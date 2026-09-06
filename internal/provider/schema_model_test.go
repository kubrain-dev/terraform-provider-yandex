package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestSchemaModelParity decodes a null object of each resource's schema into its
// model struct. The framework's reflection layer errors if the schema declares
// an attribute or block with no matching tfsdk struct field (or vice versa), so
// this catches schema/model drift for every resource at build time instead of at
// the first apply. This is exactly the class of bug that made the versioning
// block fail to decode.
func TestSchemaModelParity(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		res  resource.Resource
		model any
	}{
		{"vpc_network", NewVPCNetworkResource(), &vpcNetworkModel{}},
		{"vpc_subnet", NewVPCSubnetResource(), &vpcSubnetModel{}},
		{"kubernetes_cluster", NewK8sClusterResource(), &k8sClusterModel{}},
		{"kubernetes_node_group", NewK8sNodeGroupResource(), &k8sNodeGroupModel{}},
		{"storage_bucket", NewStorageBucketResource(), &storageBucketModel{}},
		{"iam_service_account", NewIAMServiceAccountResource(), &iamServiceAccountModel{}},
		{"iam_static_access_key", NewIAMStaticAccessKeyResource(), &iamStaticAccessKeyModel{}},
		{"dns_zone", NewDNSZoneResource(), &dnsZoneModel{}},
		{"dns_recordset", NewDNSRecordSetResource(), &dnsRecordSetModel{}},
		{"container_registry", NewContainerRegistryResource(), &containerRegistryModel{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp resource.SchemaResponse
			tc.res.Schema(ctx, resource.SchemaRequest{}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
			}
			// Build a known object whose every top-level attribute/block is null.
			// A null whole-object can't decode into a struct, but a known object
			// with null members still exercises the attribute-name → struct-field
			// matching that catches schema/model drift (the versioning bug).
			tfType := resp.Schema.Type().TerraformType(ctx)
			obj, ok := tfType.(tftypes.Object)
			if !ok {
				t.Fatalf("schema type is not an object: %T", tfType)
			}
			vals := make(map[string]tftypes.Value, len(obj.AttributeTypes))
			for name, at := range obj.AttributeTypes {
				vals[name] = tftypes.NewValue(at, nil)
			}
			state := tfsdk.State{Schema: resp.Schema, Raw: tftypes.NewValue(tfType, vals)}
			diags := state.Get(ctx, tc.model)
			if diags.HasError() {
				t.Fatalf("schema/model mismatch for %s: %v", tc.name, diags)
			}
		})
	}
}
