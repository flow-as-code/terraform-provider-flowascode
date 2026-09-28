// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Command terraform-provider-flowascode serves the flow-as-code/flowascode
// provider over the Terraform plugin protocol, version 6.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/provider"
)

// version is set by the release build (-ldflags "-X main.version=...").
var version = "dev"

// address is where both registries serve the provider. OpenTofu resolves
// flow-as-code/flowascode against registry.opentofu.org itself; the address a
// provider serves under is the Terraform Registry's, as every published
// provider's is.
const address = "registry.terraform.io/flow-as-code/flowascode"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers such as delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: address,
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
