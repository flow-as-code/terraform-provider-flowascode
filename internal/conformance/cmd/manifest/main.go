// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Command manifest prints MANIFEST.json for a vendored conformance/ tree.
package main

import (
	"fmt"
	"os"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: manifest <dir>")
		os.Exit(2)
	}
	m, err := conformance.Compute(os.DirFS(os.Args[1]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(m.JSON())
}
