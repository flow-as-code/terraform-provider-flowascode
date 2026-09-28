// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6/tf6server"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
)

// planWithTofu runs `plan` and `show -json` on config with this provider served in
// process against a fake Connect, and returns the planned values of one
// resource's attributes. It reads the plan JSON with encoding/json:
// terraform-json cannot decode a plan whose configuration has a nested
// attribute named `references` (UpdateContactData's References parameter),
// because the name collides with its own field.
func planWithTofu(t *testing.T, config, address string) (map[string]any, string) {
	return planFiles(t, map[string]string{"main.tf": config}, address)
}

// planFiles is planWithTofu over several files (a local module, say).
func planFiles(t *testing.T, files map[string]string, address string) (map[string]any, string) {
	t.Helper()
	terraformBinary(t)
	bin := os.Getenv("TF_ACC_TERRAFORM_PATH")
	host := os.Getenv("TF_ACC_PROVIDER_HOST")
	if host == "" {
		host = "registry.terraform.io"
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reattach := make(chan *plugin.ReattachConfig, 1)
	closed := make(chan struct{})
	client := &connectapi.Client{Connect: connectapi.NewFake(), Region: "us-east-1"}
	server := providerserver.NewProtocol6(NewWithClient("test", client)())
	go func() {
		_ = tf6server.Serve(host+"/hashicorp/flowascode", func() tfprotov6.ProviderServer { return server() },
			tf6server.WithDebug(ctx, reattach, closed), tf6server.WithoutLogStderrOverride(),
			tf6server.WithGoPluginLogger(hclog.NewNullLogger()))
	}()
	rc := <-reattach
	env, _ := json.Marshal(map[string]any{
		host + "/hashicorp/flowascode": map[string]any{
			"Protocol": string(rc.Protocol), "ProtocolVersion": rc.ProtocolVersion, "Pid": rc.Pid, "Test": true,
			"Addr": map[string]string{"Network": rc.Addr.Network(), "String": rc.Addr.String()},
		},
	})
	dir := t.TempDir()
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TF_REATTACH_PROVIDERS="+string(env), "TF_IN_AUTOMATION=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	// A configuration HCL cannot parse fails at init already; that is a
	// refusal too, returned like a plan's.
	if out, err := run("init", "-input=false", "-no-color"); err != nil {
		return nil, out
	}
	if out, err := run("plan", "-input=false", "-no-color", "-out=plan"); err != nil {
		return nil, out
	}
	out, err := run("show", "-json", "plan")
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	var doc struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				After map[string]any `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("plan JSON: %v", err)
	}
	for _, rc := range doc.ResourceChanges {
		if rc.Address == address {
			return rc.Change.After, ""
		}
	}
	t.Fatal(fmt.Sprintf("no planned change for %s", address))
	return nil, ""
}
