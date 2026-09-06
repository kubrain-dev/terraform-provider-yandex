package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

var (
	_ resource.Resource              = (*vpcSubnetResource)(nil)
	_ resource.ResourceWithConfigure = (*vpcSubnetResource)(nil)
)

// vpcSubnetResource is a passthrough: Kubrain fuses network+subnet into one VPC
// with an auto-assigned CIDR, so a subnet has no distinct backing object. The
// resource exists so that YC configs which declare a subnet and reference its id
// (e.g. from a node group) validate and wire up the dependency graph. It holds
// only Terraform state — it creates nothing server-side.
type vpcSubnetResource struct {
	client *kbclient.Client
}

// NewVPCSubnetResource is the resource factory.
func NewVPCSubnetResource() resource.Resource { return &vpcSubnetResource{} }

type vpcSubnetModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	NetworkID    types.String `tfsdk:"network_id"`
	V4CIDRBlocks types.List   `tfsdk:"v4_cidr_blocks"`
	Zone         types.String `tfsdk:"zone"`
	Description  types.String `tfsdk:"description"`
	FolderID     types.String `tfsdk:"folder_id"`
	Labels       types.Map    `tfsdk:"labels"`
	RouteTableID types.String `tfsdk:"route_table_id"`
}

func (r *vpcSubnetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_subnet"
}

func (r *vpcSubnetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Passthrough for `yandex_vpc_subnet`. Kubrain fuses network and subnet into one " +
			"auto-CIDR VPC, so this resource holds only Terraform state (it references its network but creates " +
			"nothing). It exists so subnet-referencing configs work unchanged.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Subnet id (its name).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Subnet name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"network_id": schema.StringAttribute{
				Required:      true,
				Description:   "The network (Kubrain VPC name) this subnet belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"v4_cidr_blocks": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Echoed back as-is. Kubrain assigns the VPC CIDR automatically; this is informational.",
			},
			// accepted & ignored:
			"zone":           schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"description":    schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"folder_id":      schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"route_table_id": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"labels":         schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
	}
}

func (r *vpcSubnetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *vpcSubnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcSubnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.Name
	// v4_cidr_blocks is optional+computed: if the user left it null, settle it to
	// an empty list so the plan stays consistent.
	if plan.V4CIDRBlocks.IsNull() || plan.V4CIDRBlocks.IsUnknown() {
		plan.V4CIDRBlocks = types.ListNull(types.StringType)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read is a no-op — there is no backing object to reconcile against.
func (r *vpcSubnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcSubnetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vpcSubnetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vpcSubnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete just drops the state; nothing to release server-side.
func (r *vpcSubnetResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}
