package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/cloudxino/terraform-provider-cloudxino/internal/provider"
)

// version is set by GoReleaser.
var version = "dev"

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.20.1 generate --provider-name cloudxino

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/cloudxino/cloudxino",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
