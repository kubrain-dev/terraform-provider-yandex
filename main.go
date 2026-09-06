// Command terraform-provider-yandex is a Terraform provider that presents the
// Yandex Cloud resource surface (yandex_*) but provisions resources on the
// Kubrain cloud. Point required_providers at source "kubrain.dev/kubrain/yandex"
// to run an existing YC configuration against Kubrain.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"kubrain.dev/terraform-provider-yandex/internal/provider"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// The registry address whose local name is "yandex"; a config selects it
		// via required_providers { yandex = { source = "kubrain.dev/kubrain/yandex" } }.
		Address: "kubrain.dev/kubrain/yandex",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
