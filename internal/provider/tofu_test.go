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
// resource's attributes.
func planWithTofu(t *testing.T, config, address string) (map[string]any, string) {
	return planFiles(t, map[string]string{"main.tf": config}, address)
}

// planFiles is planWithTofu over several files (a local module, say).
func planFiles(t *testing.T, files map[string]string, address string) (map[string]any, string) {
	t.Helper()
	p, failed := planFilesWith(t, connectapi.NewFake(), files, address)
	return p.After, failed
}

// planOutcome is what a plan says about one resource and the root outputs.
type planOutcome struct {
	// After is the resource's planned attributes.
	After map[string]any
	// Actions is the change's actions: create, update, delete, no-op or a
	// replace pair.
	Actions []string
	// Outputs is planned_values.outputs: the value of every root output the
	// plan already knows; an output unknown until apply is absent.
	Outputs map[string]any
}

// planFilesWith is planFiles against a given fake, so a test can seed the
// instance (or a prior state file, passed as terraform.tfstate) first. It
// reads the plan JSON with encoding/json: terraform-json cannot decode a plan
// whose configuration has a nested attribute named `references`
// (UpdateContactData's References parameter), because the name collides
// with its own field.
func planFilesWith(t *testing.T, fake *connectapi.Fake, files map[string]string, address string) (planOutcome, string) {
	t.Helper()
	env := startTofu(t, fake, files)
	// A configuration HCL cannot parse fails at init already; that is a
	// refusal too, returned like a plan's.
	if out, err := env.run("init", "-input=false", "-no-color"); err != nil {
		return planOutcome{}, out
	}
	if out, err := env.run("plan", "-input=false", "-no-color", "-out=plan"); err != nil {
		return planOutcome{}, out
	}
	out, err := env.run("show", "-json", "plan")
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	var doc struct {
		PlannedValues struct {
			Outputs map[string]struct {
				Value any `json:"value"`
			} `json:"outputs"`
		} `json:"planned_values"`
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string       `json:"actions"`
				After   map[string]any `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("plan JSON: %v", err)
	}
	outputs := map[string]any{}
	for name, o := range doc.PlannedValues.Outputs {
		if o.Value != nil {
			outputs[name] = o.Value
		}
	}
	for _, rc := range doc.ResourceChanges {
		if rc.Address == address {
			return planOutcome{After: rc.Change.After, Actions: rc.Change.Actions, Outputs: outputs}, ""
		}
	}
	t.Fatal(fmt.Sprintf("no planned change for %s", address))
	return planOutcome{}, ""
}

// applyFiles runs `apply` on files against fake and returns the state file
// it wrote, for a test that needs a state as this provider version writes it
// before editing it into another version's shape.
func applyFiles(t *testing.T, fake *connectapi.Fake, files map[string]string) string {
	t.Helper()
	env := startTofu(t, fake, files)
	for _, args := range [][]string{
		{"init", "-input=false", "-no-color"},
		{"apply", "-input=false", "-no-color", "-auto-approve"},
	} {
		if out, err := env.run(args...); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
	state, err := os.ReadFile(filepath.Join(env.dir, "terraform.tfstate"))
	if err != nil {
		t.Fatal(err)
	}
	return string(state)
}

// tofuEnv is a working directory the harness made and the CLI run in it.
type tofuEnv struct {
	dir string
	run func(args ...string) (string, error)
}

// startTofu serves this provider in process against fake, writes files into
// a fresh working directory, and returns the CLI in it with the provider
// reattached.
func startTofu(t *testing.T, fake *connectapi.Fake, files map[string]string) tofuEnv {
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
	client := &connectapi.Client{Connect: fake, Region: "us-east-1"}
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
	return tofuEnv{dir: dir, run: func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TF_REATTACH_PROVIDERS="+string(env), "TF_IN_AUTOMATION=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}}
}
