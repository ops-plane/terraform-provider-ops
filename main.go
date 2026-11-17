package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/ops-plane/terraform-provider-ops/internal/provider"
)

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.24.0 generate --provider-name ops

// version is set at release (goreleaser).
var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "run with support for debuggers such as delve")
	flag.Parse()
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/ops-plane/ops",
		Debug:   *debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
