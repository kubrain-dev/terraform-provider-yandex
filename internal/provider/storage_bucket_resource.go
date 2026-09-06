package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

var (
	_ resource.Resource                = (*storageBucketResource)(nil)
	_ resource.ResourceWithConfigure   = (*storageBucketResource)(nil)
	_ resource.ResourceWithImportState = (*storageBucketResource)(nil)
)

// storageBucketResource maps yandex_storage_bucket onto a Kubrain bucket.
type storageBucketResource struct {
	client *kbclient.Client
}

// NewStorageBucketResource is the resource factory.
func NewStorageBucketResource() resource.Resource { return &storageBucketResource{} }

// storageBucketModel declares the YC yandex_storage_bucket argument surface we
// accept. Only `bucket` and `acl` map to Kubrain; the rest are accepted for
// compatibility and ignored (they never diff because they are only ever read
// back from config, never from Kubrain).
type storageBucketModel struct {
	ID     types.String `tfsdk:"id"`
	Bucket types.String `tfsdk:"bucket"`
	ACL    types.String `tfsdk:"acl"`

	// mapped-derived, computed:
	Public           types.Bool   `tfsdk:"public"`
	BucketDomainName types.String `tfsdk:"bucket_domain_name"`

	// accepted & ignored (YC compatibility):
	AccessKey types.String `tfsdk:"access_key"`
	SecretKey types.String `tfsdk:"secret_key"`
	FolderID  types.String `tfsdk:"folder_id"`
	MaxSize   types.Int64  `tfsdk:"max_size"`
	Tags      types.Map    `tfsdk:"tags"`
}

func (r *storageBucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_bucket"
}

func (r *storageBucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "S3-compatible bucket on Kubrain object storage. Mirrors `yandex_storage_bucket`; " +
			"`bucket` is the name and `acl` controls anonymous read. Other arguments are accepted and ignored.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Bucket name (identity).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"bucket": schema.StringAttribute{
				Required:      true,
				Description:   "Bucket name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"acl": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Canned ACL. A `public-read`-family value makes the bucket anonymously readable; " +
					"anything else keeps it private. Immutable on Kubrain.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the bucket allows anonymous read (derived from acl).",
			},
			"bucket_domain_name": schema.StringAttribute{
				Computed:    true,
				Description: "Public/base URL of the bucket on the Kubrain S3 gateway.",
			},
			// accepted & ignored:
			"access_key": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"secret_key": schema.StringAttribute{Optional: true, Sensitive: true, Description: "Ignored (YC compatibility)."},
			"folder_id":  schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"max_size":   schema.Int64Attribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"tags":       schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Ignored (YC compatibility)."},
		},
		Blocks: map[string]schema.Block{
			// yandex_storage_bucket writes `versioning { enabled = true }` as a
			// block; accept it (and ignore it) so such configs validate.
			"versioning": schema.ListNestedBlock{
				Description: "Ignored (YC compatibility).",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
					},
				},
			},
		},
	}
}

func (r *storageBucketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *storageBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan storageBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	public := aclToPublic(plan.ACL.ValueString())
	b, err := r.client.CreateBucket(ctx, plan.Bucket.ValueString(), public)
	if err != nil {
		resp.Diagnostics.AddError("Creating bucket", err.Error())
		return
	}
	r.apply(&plan, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *storageBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state storageBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.GetBucket(ctx, state.Bucket.ValueString())
	if err != nil {
		if kbclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading bucket", err.Error())
		return
	}
	r.apply(&state, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update never does a real mutation: bucket and acl are RequiresReplace and
// every other attribute is ignored. It just re-reads and persists the plan.
func (r *storageBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan storageBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.GetBucket(ctx, plan.Bucket.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading bucket", err.Error())
		return
	}
	r.apply(&plan, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *storageBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state storageBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteBucket(ctx, state.Bucket.ValueString()); err != nil {
		if kbclient.IsNotFound(err) {
			return
		}
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "not empty") {
			msg += "\n\nKubrain refuses to delete a non-empty bucket. Empty it with your S3 credentials first."
		}
		resp.Diagnostics.AddError("Deleting bucket", msg)
	}
}

func (r *storageBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("bucket"), req, resp)
}

// apply copies computed fields from the Kubrain bucket onto the model.
func (r *storageBucketResource) apply(m *storageBucketModel, b *kbclient.Bucket) {
	m.ID = types.StringValue(b.Name)
	m.Bucket = types.StringValue(b.Name)
	m.Public = types.BoolValue(b.Public)
	if b.Public {
		m.ACL = types.StringValue("public-read")
	} else {
		m.ACL = types.StringValue("private")
	}
	domain := b.PublicURL
	if domain == "" {
		domain = b.Endpoint
	}
	m.BucketDomainName = types.StringValue(domain)
}
