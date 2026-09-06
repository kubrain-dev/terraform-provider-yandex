// Package provider implements a Terraform provider that presents Yandex Cloud
// resource types (yandex_*) but provisions them on the Kubrain cloud. It is a
// drop-in mirror: an existing YC config runs against Kubrain by only swapping
// the source in required_providers. Arguments with no Kubrain equivalent are
// declared (so plans validate) and silently ignored.
package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

// defaultAPI is the public Kubrain api-server, used when neither the provider
// `endpoint`, $KUBRAIN_API, nor ~/.kubrain/config supplies one.
const defaultAPI = "https://api.kubrain.dev"

// yandexProvider is the provider implementation.
type yandexProvider struct {
	version string
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &yandexProvider{version: version} }
}

// providerModel mirrors the yandex provider's configuration arguments. Only
// `token` and `endpoint` are honored; the rest are accepted and ignored so
// existing YC provider blocks parse unchanged.
type providerModel struct {
	Token                 types.String `tfsdk:"token"`
	Endpoint              types.String `tfsdk:"endpoint"`
	CloudID               types.String `tfsdk:"cloud_id"`
	FolderID              types.String `tfsdk:"folder_id"`
	Zone                  types.String `tfsdk:"zone"`
	ServiceAccountKeyFile types.String `tfsdk:"service_account_key_file"`
	StorageEndpoint       types.String `tfsdk:"storage_endpoint"`
	StorageAccessKey      types.String `tfsdk:"storage_access_key"`
	StorageSecretKey      types.String `tfsdk:"storage_secret_key"`
	MaxRetries            types.Int64  `tfsdk:"max_retries"`
	Insecure              types.Bool   `tfsdk:"insecure"`
}

func (p *yandexProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	// TypeName "yandex" makes every resource type yandex_*.
	resp.TypeName = "yandex"
	resp.Version = p.version
}

func (p *yandexProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Kubrain cloud, presented as the Yandex Cloud provider. " +
			"`token` is your Kubrain tenant token; `endpoint` is the Kubrain api-server URL. " +
			"All other arguments are accepted for compatibility and ignored.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Kubrain tenant bearer token. Falls back to $KUBRAIN_TOKEN, then ~/.kubrain/config.",
			},
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Kubrain api-server base URL. Falls back to $KUBRAIN_API, then ~/.kubrain/config, then " + defaultAPI + ".",
			},
			// --- accepted & ignored (YC compatibility) ---
			"cloud_id":                 schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"folder_id":                schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"zone":                     schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"service_account_key_file": schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"storage_endpoint":         schema.StringAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"storage_access_key":       schema.StringAttribute{Optional: true, Sensitive: true, Description: "Ignored (YC compatibility)."},
			"storage_secret_key":       schema.StringAttribute{Optional: true, Sensitive: true, Description: "Ignored (YC compatibility)."},
			"max_retries":              schema.Int64Attribute{Optional: true, Description: "Ignored (YC compatibility)."},
			"insecure":                 schema.BoolAttribute{Optional: true, Description: "Ignored (YC compatibility)."},
		},
	}
}

func (p *yandexProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fileAPI, fileToken := loadFileConfig()

	// Precedence: provider block > env > ~/.kubrain/config > built-in default.
	base := firstNonEmpty(cfg.Endpoint.ValueString(), os.Getenv("KUBRAIN_API"), fileAPI, defaultAPI)
	token := firstNonEmpty(cfg.Token.ValueString(), os.Getenv("KUBRAIN_TOKEN"), fileToken)

	if token == "" {
		resp.Diagnostics.AddError(
			"Missing Kubrain token",
			"Set `token` in the provider block, $KUBRAIN_TOKEN, or run `kubrain auth login` to populate ~/.kubrain/config.",
		)
		return
	}

	c := kbclient.New(base, token)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *yandexProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewVPCNetworkResource,
		NewVPCSubnetResource,
		NewK8sClusterResource,
		NewK8sNodeGroupResource,
		NewStorageBucketResource,
		NewIAMServiceAccountResource,
		NewIAMStaticAccessKeyResource,
		NewDNSZoneResource,
		NewDNSRecordSetResource,
		NewContainerRegistryResource,
	}
}

func (p *yandexProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// loadFileConfig reads api/token from ~/.kubrain/config (or $KUBRAIN_CONFIG),
// matching the CLI's on-disk format. A missing/unparseable file yields empties.
func loadFileConfig() (api, token string) {
	path := os.Getenv("KUBRAIN_CONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		path = filepath.Join(home, ".kubrain", "config")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var c struct {
		API   string `json:"api"`
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &c) != nil {
		return "", ""
	}
	return c.API, c.Token
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
