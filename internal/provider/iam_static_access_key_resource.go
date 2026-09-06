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
	_ resource.Resource              = (*iamStaticAccessKeyResource)(nil)
	_ resource.ResourceWithConfigure = (*iamStaticAccessKeyResource)(nil)
)

// iamStaticAccessKeyResource maps yandex_iam_service_account_static_access_key
// onto the tenant's Kubrain S3 credentials. Kubrain issues ONE key pair per
// tenant (stable across calls), so every static access key resolves to the same
// access_key/secret_key. That still lets a storage_bucket config reference
// `...static_access_key.x.access_key` and get working credentials.
type iamStaticAccessKeyResource struct {
	client *kbclient.Client
}

// NewIAMStaticAccessKeyResource is the resource factory.
func NewIAMStaticAccessKeyResource() resource.Resource { return &iamStaticAccessKeyResource{} }

type iamStaticAccessKeyModel struct {
	ID               types.String `tfsdk:"id"`
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	AccessKey        types.String `tfsdk:"access_key"`
	SecretKey        types.String `tfsdk:"secret_key"`
	Description      types.String `tfsdk:"description"`
	PGPKey           types.String `tfsdk:"pgp_key"`
}

func (r *iamStaticAccessKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_service_account_static_access_key"
}

func (r *iamStaticAccessKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The tenant's Kubrain S3 key pair, presented as " +
			"`yandex_iam_service_account_static_access_key`. Kubrain issues one pair per tenant, so all keys " +
			"resolve to the same credentials.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Identity (the access key).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"service_account_id": schema.StringAttribute{
				Optional:      true,
				Description:   "Ignored (YC compatibility); the key is tenant-wide.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"access_key": schema.StringAttribute{
				Computed:    true,
				Description: "S3 access key id.",
			},
			"secret_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "S3 secret access key.",
			},
			// accepted & ignored:
			"description": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"pgp_key":     schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
		},
	}
}

func (r *iamStaticAccessKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *iamStaticAccessKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamStaticAccessKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	creds, err := r.client.GetBucketCredentials(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Fetching S3 credentials", err.Error())
		return
	}
	r.apply(&plan, creds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *iamStaticAccessKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamStaticAccessKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	creds, err := r.client.GetBucketCredentials(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Fetching S3 credentials", err.Error())
		return
	}
	r.apply(&state, creds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *iamStaticAccessKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan iamStaticAccessKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	creds, err := r.client.GetBucketCredentials(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Fetching S3 credentials", err.Error())
		return
	}
	r.apply(&plan, creds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the state; Kubrain credentials are tenant-wide and not revoked.
func (r *iamStaticAccessKeyResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *iamStaticAccessKeyResource) apply(m *iamStaticAccessKeyModel, c *kbclient.BucketCredentials) {
	m.ID = types.StringValue(c.AccessKey)
	m.AccessKey = types.StringValue(c.AccessKey)
	m.SecretKey = types.StringValue(c.SecretKey)
}
