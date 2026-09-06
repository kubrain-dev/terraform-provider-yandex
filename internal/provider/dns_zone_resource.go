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
	_ resource.Resource                = (*dnsZoneResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsZoneResource)(nil)
	_ resource.ResourceWithImportState = (*dnsZoneResource)(nil)
)

// dnsZoneResource maps yandex_dns_zone onto a Kubrain hosted DNS zone. YC's
// `zone` argument (the DNS domain, with trailing dot) is the Kubrain zone name.
type dnsZoneResource struct {
	client *kbclient.Client
}

// NewDNSZoneResource is the resource factory.
func NewDNSZoneResource() resource.Resource { return &dnsZoneResource{} }

type dnsZoneModel struct {
	ID   types.String `tfsdk:"id"`
	Zone types.String `tfsdk:"zone"`

	// computed:
	NameServers types.List `tfsdk:"name_servers"`

	// accepted & ignored (YC compatibility):
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Public          types.Bool   `tfsdk:"public"`
	FolderID        types.String `tfsdk:"folder_id"`
	PrivateNetworks types.List   `tfsdk:"private_networks"`
	Labels          types.Map    `tfsdk:"labels"`
}

func (r *dnsZoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (r *dnsZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Kubrain hosted DNS zone. Mirrors `yandex_dns_zone`; `zone` is the domain to host.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Zone domain (identity).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone": schema.StringAttribute{
				Required:      true,
				Description:   "The DNS domain to host (e.g. `example.com.`).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name_servers": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Nameservers to delegate the domain to at your registrar.",
			},
			// accepted & ignored:
			"name":             schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"description":      schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"public":           schema.BoolAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"folder_id":        schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"private_networks": schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
			"labels":           schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
	}
}

func (r *dnsZoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *dnsZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.CreateZone(ctx, plan.Zone.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Creating DNS zone", err.Error())
		return
	}
	resp.Diagnostics.Append(r.apply(ctx, &plan, z)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.GetZone(ctx, state.Zone.ValueString())
	if err != nil {
		if kbclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading DNS zone", err.Error())
		return
	}
	resp.Diagnostics.Append(r.apply(ctx, &state, z)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dnsZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.GetZone(ctx, plan.Zone.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading DNS zone", err.Error())
		return
	}
	resp.Diagnostics.Append(r.apply(ctx, &plan, z)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteZone(ctx, state.Zone.ValueString()); err != nil && !kbclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting DNS zone", err.Error())
	}
}

func (r *dnsZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("zone"), req, resp)
}

func (r *dnsZoneResource) apply(ctx context.Context, m *dnsZoneModel, z *kbclient.Zone) (diags diagList) {
	m.ID = types.StringValue(z.Name)
	m.Zone = types.StringValue(z.Name)
	ns, d := types.ListValueFrom(ctx, types.StringType, z.NameServers)
	diags = append(diags, d...)
	m.NameServers = ns
	return diags
}
