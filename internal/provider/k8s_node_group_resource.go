package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

var (
	_ resource.Resource                = (*k8sNodeGroupResource)(nil)
	_ resource.ResourceWithConfigure   = (*k8sNodeGroupResource)(nil)
	_ resource.ResourceWithImportState = (*k8sNodeGroupResource)(nil)
)

// k8sNodeGroupResource maps yandex_kubernetes_node_group onto a Kubrain
// cluster's single worker pool: the group size sets the worker count (scale) and
// the per-node memory sets the worker RAM (resize). Kubrain has ONE worker pool
// per cluster, so multiple node groups on the same cluster collapse onto it
// (last apply wins); a warning is emitted to make that explicit.
type k8sNodeGroupResource struct {
	client *kbclient.Client
}

// NewK8sNodeGroupResource is the resource factory.
func NewK8sNodeGroupResource() resource.Resource { return &k8sNodeGroupResource{} }

type ngFixedScaleModel struct {
	Size types.Int64 `tfsdk:"size"`
}

type ngAutoScaleModel struct {
	MinSize     types.Int64 `tfsdk:"min_size"`
	MaxSize     types.Int64 `tfsdk:"max_size"`
	InitialSize types.Int64 `tfsdk:"initial_size"`
}

type ngScalePolicyModel struct {
	FixedScale *ngFixedScaleModel `tfsdk:"fixed_scale"`
	AutoScale  *ngAutoScaleModel  `tfsdk:"auto_scale"`
}

type ngResourcesModel struct {
	Memory       types.Float64 `tfsdk:"memory"`
	Cores        types.Int64   `tfsdk:"cores"`
	CoreFraction types.Int64   `tfsdk:"core_fraction"`
}

type ngBootDiskModel struct {
	Type types.String `tfsdk:"type"`
	Size types.Int64  `tfsdk:"size"`
}

type ngInstanceTemplateModel struct {
	PlatformID types.String      `tfsdk:"platform_id"`
	NAT        types.Bool        `tfsdk:"nat"`
	Resources  *ngResourcesModel `tfsdk:"resources"`
	BootDisk   *ngBootDiskModel  `tfsdk:"boot_disk"`
}

type k8sNodeGroupModel struct {
	ID               types.String             `tfsdk:"id"`
	Name             types.String             `tfsdk:"name"`
	ClusterID        types.String             `tfsdk:"cluster_id"`
	ScalePolicy      *ngScalePolicyModel      `tfsdk:"scale_policy"`
	InstanceTemplate *ngInstanceTemplateModel `tfsdk:"instance_template"`

	// accepted & ignored (YC compatibility):
	Version     types.String `tfsdk:"version"`
	Description types.String `tfsdk:"description"`
	Labels      types.Map    `tfsdk:"labels"`
}

func (r *k8sNodeGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kubernetes_node_group"
}

func (r *k8sNodeGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The worker pool of a Kubrain cluster, expressed as `yandex_kubernetes_node_group`. " +
			"`scale_policy.fixed_scale.size` sets the worker count; `instance_template.resources.memory` sets the " +
			"per-worker RAM. Kubrain has one worker pool per cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Node group id (name).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Node group name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cluster_id": schema.StringAttribute{
				Required:      true,
				Description:   "The cluster (Kubrain cluster name) whose worker pool this group drives.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			// accepted & ignored:
			"version":     schema.StringAttribute{Optional: true, Description: "Ignored (set on the cluster)."},
			"description": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"labels":      schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
		Blocks: map[string]schema.Block{
			"scale_policy": schema.SingleNestedBlock{
				Description: "Worker count. `fixed_scale.size` maps to the Kubrain worker count; for `auto_scale`, " +
					"`initial_size` (else `min_size`) is used.",
				Blocks: map[string]schema.Block{
					"fixed_scale": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"size": schema.Int64Attribute{Optional: true, Description: "Worker count."},
						},
					},
					"auto_scale": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"min_size":     schema.Int64Attribute{Optional: true, Description: "Used as worker count if no initial_size."},
							"max_size":     schema.Int64Attribute{Optional: true, Description: "Ignored (Kubrain has no autoscaler)."},
							"initial_size": schema.Int64Attribute{Optional: true, Description: "Used as the worker count."},
						},
					},
				},
			},
			"instance_template": schema.SingleNestedBlock{
				Description: "Per-worker shape. `resources.memory` (GiB) maps to the Kubrain worker RAM.",
				Attributes: map[string]schema.Attribute{
					"platform_id": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
					"nat":         schema.BoolAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
				},
				Blocks: map[string]schema.Block{
					"resources": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"memory":        schema.Float64Attribute{Optional: true, Description: "Per-worker RAM in GiB."},
							"cores":         schema.Int64Attribute{Optional: true, Description: "Ignored (Kubrain derives vCPU from RAM)."},
							"core_fraction": schema.Int64Attribute{Optional: true, Description: "Ignored (YC compatibility)."},
						},
					},
					"boot_disk": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"type": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
							"size": schema.Int64Attribute{Optional: true, Description: "Ignored (YC compatibility)."},
						},
					},
				},
			},
		},
	}
}

func (r *k8sNodeGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

// reconcile applies the group's desired worker count and RAM to the cluster.
func (r *k8sNodeGroupResource) reconcile(ctx context.Context, m *k8sNodeGroupModel, diags *diagList) {
	cluster := m.ClusterID.ValueString()
	workers := desiredWorkers(m.ScalePolicy)
	ram := desiredRAMGiB(m.InstanceTemplate)

	if _, err := r.client.ScaleCluster(ctx, cluster, workers); err != nil {
		diags.AddError("Scaling worker pool", err.Error())
		return
	}
	if ram > 0 {
		if _, err := r.client.ResizeCluster(ctx, cluster, ram); err != nil {
			diags.AddError("Resizing worker pool", err.Error())
			return
		}
	}
	diags.AddWarning(
		"Node group maps to a single worker pool",
		"Kubrain has one worker pool per cluster. This node group set the cluster's worker count"+
			" and RAM directly; multiple node groups on the same cluster collapse onto that pool (last apply wins).",
	)
}

func (r *k8sNodeGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan k8sNodeGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.reconcile(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read leaves the group's declared shape in state; the authoritative worker
// count/RAM live on the cluster resource. There is no separate object to fetch.
func (r *k8sNodeGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state k8sNodeGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *k8sNodeGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan k8sNodeGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.reconcile(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete scales the cluster's worker pool to zero.
func (r *k8sNodeGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state k8sNodeGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.ScaleCluster(ctx, state.ClusterID.ValueString(), 0); err != nil {
		if kbclient.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddWarning("Scaling worker pool to zero", err.Error())
	}
}

func (r *k8sNodeGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// desiredWorkers extracts the target worker count from a scale policy.
func desiredWorkers(sp *ngScalePolicyModel) int {
	if sp == nil {
		return 0
	}
	if sp.FixedScale != nil && !sp.FixedScale.Size.IsNull() {
		return int(sp.FixedScale.Size.ValueInt64())
	}
	if sp.AutoScale != nil {
		if !sp.AutoScale.InitialSize.IsNull() && sp.AutoScale.InitialSize.ValueInt64() > 0 {
			return int(sp.AutoScale.InitialSize.ValueInt64())
		}
		if !sp.AutoScale.MinSize.IsNull() {
			return int(sp.AutoScale.MinSize.ValueInt64())
		}
	}
	return 0
}

// desiredRAMGiB extracts the target per-worker RAM (GiB) from an instance
// template, or 0 if unset (leave the cluster's current RAM).
func desiredRAMGiB(it *ngInstanceTemplateModel) int {
	if it == nil || it.Resources == nil || it.Resources.Memory.IsNull() {
		return 0
	}
	return memoryToRAMGiB(it.Resources.Memory.ValueFloat64())
}
