package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

// clusterCreateTimeout bounds how long Create waits for provisioning->running.
const (
	clusterCreateTimeout  = 30 * time.Minute
	clusterUpgradeTimeout = 45 * time.Minute
	clusterPollInterval   = 10 * time.Second
)

var (
	_ resource.Resource                = (*k8sClusterResource)(nil)
	_ resource.ResourceWithConfigure   = (*k8sClusterResource)(nil)
	_ resource.ResourceWithImportState = (*k8sClusterResource)(nil)
)

// k8sClusterResource maps yandex_kubernetes_cluster onto a Kubrain cluster's
// control plane. Kubrain couples CP + workers in one object, so this resource
// creates the cluster (tier from the master block, default ram, 0 workers) and
// the companion yandex_kubernetes_node_group sets the worker shape.
type k8sClusterResource struct {
	client *kbclient.Client
}

// NewK8sClusterResource is the resource factory.
func NewK8sClusterResource() resource.Resource { return &k8sClusterResource{} }

type k8sMasterModel struct {
	Version            types.String        `tfsdk:"version"`
	PublicIP           types.Bool          `tfsdk:"public_ip"`
	Zonal              *k8sMasterZoneModel `tfsdk:"zonal"`
	Regional           *k8sMasterRegModel  `tfsdk:"regional"`
	ExternalV4Endpoint types.String        `tfsdk:"external_v4_endpoint"`
	ClusterCA          types.String        `tfsdk:"cluster_ca_certificate"`
}

type k8sMasterZoneModel struct {
	Zone     types.String `tfsdk:"zone"`
	SubnetID types.String `tfsdk:"subnet_id"`
}

type k8sMasterRegModel struct {
	Region types.String `tfsdk:"region"`
}

type k8sClusterModel struct {
	ID        types.String    `tfsdk:"id"`
	Name      types.String    `tfsdk:"name"`
	NetworkID types.String    `tfsdk:"network_id"`
	Master    *k8sMasterModel `tfsdk:"master"`

	// computed status:
	Status types.String `tfsdk:"status"`
	Health types.String `tfsdk:"health"`

	// accepted & ignored (YC compatibility):
	Description          types.String `tfsdk:"description"`
	FolderID             types.String `tfsdk:"folder_id"`
	ServiceAccountID     types.String `tfsdk:"service_account_id"`
	NodeServiceAccountID types.String `tfsdk:"node_service_account_id"`
	ReleaseChannel       types.String `tfsdk:"release_channel"`
	Labels               types.Map    `tfsdk:"labels"`
}

func (r *k8sClusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kubernetes_cluster"
}

func (r *k8sClusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Kubrain Kubernetes cluster (control plane). Mirrors `yandex_kubernetes_cluster`. " +
			"A zonal master maps to Kubrain tier `normal`, a regional master to `ha`. Worker shape is set by a " +
			"companion `yandex_kubernetes_node_group`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Cluster name (identity).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Cluster name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"network_id": schema.StringAttribute{
				Required:      true,
				Description:   "The network (Kubrain VPC name) to place the cluster in. Immutable.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{Computed: true, Description: "Cluster state (provisioning/running/…)."},
			"health": schema.StringAttribute{Computed: true, Description: "Cluster health message."},
			// accepted & ignored:
			"description":             schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"folder_id":               schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"service_account_id":      schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"node_service_account_id": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"release_channel":         schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"labels":                  schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
		Blocks: map[string]schema.Block{
			"master": schema.SingleNestedBlock{
				Description: "Master (control plane) spec. `version` maps to the Kubernetes version; a `regional` " +
					"block selects HA (3 control planes), otherwise a single control plane (`normal`).",
				Attributes: map[string]schema.Attribute{
					// Optional+Computed: the provider fills these from the live
					// cluster after apply (version normalization, public_ip default),
					// which a plain Optional attribute would reject as an
					// inconsistent result.
					"version":                schema.StringAttribute{Optional: true, Computed: true, Description: "Kubernetes version."},
					"public_ip":              schema.BoolAttribute{Optional: true, Computed: true, Description: "Ignored (Kubrain always exposes a public endpoint)."},
					"external_v4_endpoint":   schema.StringAttribute{Computed: true, Description: "Public API endpoint."},
					"cluster_ca_certificate": schema.StringAttribute{Computed: true, Description: "Not exposed by Kubrain; always empty."},
				},
				Blocks: map[string]schema.Block{
					"zonal": schema.SingleNestedBlock{
						Description: "Zonal master → Kubrain tier `normal`.",
						Attributes: map[string]schema.Attribute{
							"zone":      schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
							"subnet_id": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
						},
					},
					"regional": schema.SingleNestedBlock{
						Description: "Regional master → Kubrain tier `ha`.",
						Attributes: map[string]schema.Attribute{
							"region": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
						},
					},
				},
			},
		},
	}
}

func (r *k8sClusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *k8sClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan k8sClusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasZonal, hasRegional, version := masterInfo(plan.Master)
	params := kbclient.ClusterParams{
		Name: plan.Name.ValueString(),
		VPC:  plan.NetworkID.ValueString(),
		Tier: masterToTier(hasZonal, hasRegional),
		K8s:  version,
		// ram + workers take Kubrain defaults; the node group sets the real shape.
	}
	rec, err := r.client.CreateCluster(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Creating cluster", err.Error())
		return
	}

	rec, err = r.waitForRunning(ctx, rec.Name, clusterCreateTimeout)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for cluster to become ready", err.Error())
		return
	}
	r.apply(&plan, rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *k8sClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state k8sClusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.GetCluster(ctx, state.Name.ValueString())
	if err != nil {
		if kbclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading cluster", err.Error())
		return
	}
	r.apply(&state, rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update handles the one in-place transition: a Kubernetes version bump →
// upgrade. network_id is RequiresReplace; everything else is ignored.
func (r *k8sClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state k8sClusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, _, wantVersion := masterInfo(plan.Master)
	rec, err := r.client.GetCluster(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading cluster", err.Error())
		return
	}
	if wantVersion != "" && wantVersion != rec.K8sVersion {
		if _, err := r.client.UpgradeCluster(ctx, plan.Name.ValueString(), wantVersion); err != nil {
			resp.Diagnostics.AddError("Upgrading cluster", err.Error())
			return
		}
		rec, err = r.waitForRunning(ctx, plan.Name.ValueString(), clusterUpgradeTimeout)
		if err != nil {
			resp.Diagnostics.AddError("Waiting for upgrade", err.Error())
			return
		}
	}
	r.apply(&plan, rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *k8sClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state k8sClusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteCluster(ctx, state.Name.ValueString()); err != nil && !kbclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting cluster", err.Error())
	}
}

func (r *k8sClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// waitForRunning polls GetCluster until the cluster is running (or failed, or
// the timeout/ctx elapses). It returns the last record seen.
func (r *k8sClusterResource) waitForRunning(ctx context.Context, name string, timeout time.Duration) (*kbclient.Cluster, error) {
	deadline := time.Now().Add(timeout)
	var last *kbclient.Cluster
	for {
		rec, err := r.client.GetCluster(ctx, name)
		if err != nil {
			return last, err
		}
		last = rec
		switch rec.State {
		case "running":
			return rec, nil
		case "failed":
			return rec, fmt.Errorf("cluster entered failed state: %s", rec.Message)
		}
		if time.Now().After(deadline) {
			return rec, fmt.Errorf("timed out after %s in state %q", timeout, rec.State)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(clusterPollInterval):
		}
	}
}

func (r *k8sClusterResource) apply(m *k8sClusterModel, rec *kbclient.Cluster) {
	m.ID = types.StringValue(rec.Name)
	m.Name = types.StringValue(rec.Name)
	m.Status = types.StringValue(rec.State)
	m.Health = types.StringValue(rec.Message)
	if m.Master == nil {
		m.Master = &k8sMasterModel{}
	}
	m.Master.Version = types.StringValue(rec.K8sVersion)
	endpoint := rec.Hostname
	if endpoint == "" {
		endpoint = rec.EndpointIP
	}
	m.Master.ExternalV4Endpoint = types.StringValue(endpoint)
	m.Master.ClusterCA = types.StringValue("")
	if m.Master.PublicIP.IsNull() || m.Master.PublicIP.IsUnknown() {
		m.Master.PublicIP = types.BoolValue(true)
	}
}

// masterInfo extracts whether zonal/regional blocks are present and the desired
// version from a (possibly nil) master block.
func masterInfo(m *k8sMasterModel) (hasZonal, hasRegional bool, version string) {
	if m == nil {
		return false, false, ""
	}
	if !m.Version.IsNull() && !m.Version.IsUnknown() {
		version = m.Version.ValueString()
	}
	return m.Zonal != nil, m.Regional != nil, version
}
