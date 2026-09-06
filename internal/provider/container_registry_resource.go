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
	_ resource.Resource              = (*containerRegistryResource)(nil)
	_ resource.ResourceWithConfigure = (*containerRegistryResource)(nil)
)

// containerRegistryResource is a no-op virtual resource. Kubrain provisions one
// registry namespace per tenant automatically; there is nothing to create or
// delete. It exists so `yandex_container_registry` declarations validate, and it
// exposes the tenant's registry host as a computed attribute.
type containerRegistryResource struct {
	client *kbclient.Client
}

// NewContainerRegistryResource is the resource factory.
func NewContainerRegistryResource() resource.Resource { return &containerRegistryResource{} }

type containerRegistryModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Host     types.String `tfsdk:"host"`
	FolderID types.String `tfsdk:"folder_id"`
	Labels   types.Map    `tfsdk:"labels"`
}

func (r *containerRegistryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_registry"
}

func (r *containerRegistryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "No-op virtual `yandex_container_registry`. Kubrain auto-provisions one registry per " +
			"tenant, so this creates nothing; it exposes the registry host and lets registry declarations work.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Registry namespace (identity).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Registry name (informational; Kubrain uses a fixed per-tenant namespace).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"host": schema.StringAttribute{
				Computed:    true,
				Description: "The tenant's registry host to push to.",
			},
			// accepted & ignored:
			"folder_id": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"labels":    schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
	}
}

func (r *containerRegistryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *containerRegistryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerRegistryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.fill(ctx, &plan, resp.Diagnostics.AddWarning)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerRegistryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerRegistryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.fill(ctx, &state, resp.Diagnostics.AddWarning)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *containerRegistryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan containerRegistryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.fill(ctx, &plan, resp.Diagnostics.AddWarning)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerRegistryResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// fill sets id + host from the tenant registry. A registry that isn't configured
// on this deployment is not fatal — the resource still exists as a handle.
func (r *containerRegistryResource) fill(ctx context.Context, m *containerRegistryModel, warn func(summary, detail string)) {
	m.ID = m.Name
	reg, err := r.client.GetRegistry(ctx)
	if err != nil {
		warn("Registry unavailable", "Could not read the tenant registry: "+err.Error())
		m.Host = types.StringNull()
		return
	}
	m.Host = types.StringValue(reg.Host)
	if reg.Namespace != "" {
		m.ID = types.StringValue(reg.Namespace)
	}
}
