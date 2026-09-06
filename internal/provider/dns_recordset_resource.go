package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

var (
	_ resource.Resource              = (*dnsRecordSetResource)(nil)
	_ resource.ResourceWithConfigure = (*dnsRecordSetResource)(nil)
)

// dnsRecordSetResource maps yandex_dns_recordset onto a Kubrain DNS RRset. The
// Kubrain RRset (name, type) is the unit; `set_record` is an idempotent replace,
// so create and update are the same call.
type dnsRecordSetResource struct {
	client *kbclient.Client
}

// NewDNSRecordSetResource is the resource factory.
func NewDNSRecordSetResource() resource.Resource { return &dnsRecordSetResource{} }

type dnsRecordSetModel struct {
	ID     types.String `tfsdk:"id"`
	ZoneID types.String `tfsdk:"zone_id"`
	Name   types.String `tfsdk:"name"`
	Type   types.String `tfsdk:"type"`
	TTL    types.Int64  `tfsdk:"ttl"`
	Data   types.List   `tfsdk:"data"`
}

func (r *dnsRecordSetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_recordset"
}

func (r *dnsRecordSetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DNS record set in a Kubrain zone. Mirrors `yandex_dns_recordset`; `zone_id` is the " +
			"zone's domain, and `data` are the record values.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Composite id: zone/name/type.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				Description:   "The zone's domain (the id of a yandex_dns_zone).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Record name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "Record type (A, AAAA, CNAME, TXT, MX, …).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ttl": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "TTL in seconds. 0/omitted uses the server default.",
			},
			"data": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Record values (the RRset).",
			},
		},
	}
}

func (r *dnsRecordSetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProvider(req, resp)
}

func (r *dnsRecordSetResource) set(ctx context.Context, m *dnsRecordSetModel) diagList {
	var diags diagList
	var values []string
	diags.Append(m.Data.ElementsAs(ctx, &values, false)...)
	if diags.HasError() {
		return diags
	}
	ttl := 0
	if !m.TTL.IsNull() && !m.TTL.IsUnknown() {
		ttl = int(m.TTL.ValueInt64())
	}
	rec, err := r.client.SetRecord(ctx, m.ZoneID.ValueString(), m.Name.ValueString(), m.Type.ValueString(), values, ttl)
	if err != nil {
		diags.AddError("Setting DNS record", err.Error())
		return diags
	}
	diags.Append(r.apply(ctx, m, rec)...)
	return diags
}

func (r *dnsRecordSetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsRecordSetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.GetRecord(ctx, state.ZoneID.ValueString(), state.Name.ValueString(), state.Type.ValueString())
	if err != nil {
		if kbclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading DNS record", err.Error())
		return
	}
	resp.Diagnostics.Append(r.apply(ctx, &state, rec)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dnsRecordSetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsRecordSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsRecordSetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRecord(ctx, state.ZoneID.ValueString(), state.Name.ValueString(), state.Type.ValueString())
	if err != nil && !kbclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting DNS record", err.Error())
	}
}

func (r *dnsRecordSetResource) apply(ctx context.Context, m *dnsRecordSetModel, rec *kbclient.DNSRecord) diagList {
	var diags diagList
	m.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", m.ZoneID.ValueString(), rec.Name, rec.Type))
	m.Name = types.StringValue(rec.Name)
	m.Type = types.StringValue(rec.Type)
	m.TTL = types.Int64Value(int64(rec.TTL))
	data, d := types.ListValueFrom(ctx, types.StringType, rec.Values)
	diags.Append(d...)
	m.Data = data
	return diags
}
