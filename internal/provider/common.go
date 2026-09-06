package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"kubrain.dev/terraform-provider-yandex/internal/kbclient"
)

// diagList is a short alias for the framework's diagnostics collection, used by
// helper methods that accumulate and return diagnostics.
type diagList = diag.Diagnostics

// clientFromProvider extracts the shared *kbclient.Client that Configure stashed
// in ProviderData. req.ProviderData is nil during the framework's early
// validation walk, which is not an error — the resource just isn't wired yet.
func clientFromProvider(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *kbclient.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*kbclient.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("expected *kbclient.Client, got %T. This is a provider bug.", req.ProviderData),
		)
		return nil
	}
	return c
}
