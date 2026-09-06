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
	_ resource.Resource                = (*vpcNetworkResource)(nil)
	_ resource.ResourceWithConfigure   = (*vpcNetworkResource)(nil)
	_ resource.ResourceWithImportState = (*vpcNetworkResource)(nil)
)

// vpcNetworkResource maps yandex_vpc_network onto a Kubrain VPC.
type vpcNetworkResource struct {
	client *kbclient.Client
}

// NewVPCNetworkResource is the resource factory.
func NewVPCNetworkResource() resource.Resource { return &vpcNetworkResource{} }

type vpcNetworkModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`

	// computed status:
	CIDR types.String `tfsdk:"cidr"`

	// accepted & ignored (YC compatibility):
	Description types.String `tfsdk:"description"`
	FolderID    types.String `tfsdk:"folder_id"`
	Labels      types.Map    `tfsdk:"labels"`
}

func (r *vpcNetworkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_network"
}

func (r *vpcNetworkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Kubrain VPC (regional, tenant-isolated network). Mirrors `yandex_vpc_network`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Network name (identity).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Network name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cidr": schema.StringAttribute{
				Computed:    true,
				Description: "The CIDR Kubrain allocated to this VPC.",
			},
			// accepted & ignored:
			"description": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"folder_id":   schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"labels":      schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
	}
}

func (r *vpcNetworkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *vpcNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.EnsureVPC(ctx, plan.Name.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError("Creating VPC", err.Error())
		return
	}
	r.apply(&plan, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpcNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.GetVPC(ctx, state.Name.ValueString())
	if err != nil {
		if kbclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading VPC", err.Error())
		return
	}
	r.apply(&state, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op mutation: name is RequiresReplace and the rest are ignored.
func (r *vpcNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vpcNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.GetVPC(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading VPC", err.Error())
		return
	}
	r.apply(&plan, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpcNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpcNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVPC(ctx, state.Name.ValueString()); err != nil && !kbclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting VPC", err.Error())
	}
}

func (r *vpcNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func (r *vpcNetworkResource) apply(m *vpcNetworkModel, v *kbclient.VPC) {
	m.ID = types.StringValue(v.Name)
	m.Name = types.StringValue(v.Name)
	m.CIDR = types.StringValue(v.CIDR)
}
