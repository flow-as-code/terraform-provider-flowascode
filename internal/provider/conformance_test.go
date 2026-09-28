// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// terraformBinary finds the CLI the plugin-testing harness drives:
// TF_ACC_TERRAFORM_PATH when set, else tofu, else terraform. The unit lane
// runs with whichever is on PATH; CI runs it against both.
func terraformBinary(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC_TERRAFORM_PATH") != "" {
		return
	}
	for _, name := range []string{"tofu", "terraform"} {
		if p, err := exec.LookPath(name); err == nil {
			t.Setenv("TF_ACC_TERRAFORM_PATH", p)
			if name == "tofu" {
				t.Setenv("TF_ACC_PROVIDER_HOST", "registry.opentofu.org")
			}
			return
		}
	}
	t.Skip("no tofu or terraform binary on PATH")
}

func factories(fake *connectapi.Fake) map[string]func() (tfprotov6.ProviderServer, error) {
	client := &connectapi.Client{Connect: fake, Region: "us-east-1"}
	return map[string]func() (tfprotov6.ProviderServer, error){
		"flowascode": providerserver.NewProtocol6WithError(NewWithClient("test", client)()),
	}
}

func readVendored(t *testing.T, p string) []byte {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// expectedFlowDoc is the case's document as the provider's flowdoc
// attribute holds it: canonical, without meta.
func expectedFlowDoc(t *testing.T, docPath string) string {
	t.Helper()
	v, err := jsonv.Decode(readVendored(t, docPath))
	if err != nil {
		t.Fatal(err)
	}
	o := v.(jsonv.Object)
	o.Delete("meta")
	return string(flowdoc.Serialize(o))
}

var (
	refsBlock   = regexp.MustCompile(`(?s)\n  refs = \{\n(.*?)\n  \}\n`)
	refsEntry   = regexp.MustCompile(`(?m)^(\s+)("[^"]+")(\s*)= .*$`)
	instanceArg = regexp.MustCompile(`(?m)^(  instance_id\s*=\s*).*$`)
	resourceHdr = regexp.MustCompile(`resource "(flowascode_[a-z_]+)" "([a-z0-9_]+)"`)
)

// standalone makes a golden plannable on its own: the refs values and the
// instance become literals (the flowdoc does not depend on either), since the
// addresses they name belong to providers this test does not load.
func standalone(tf string) string {
	tf = refsBlock.ReplaceAllStringFunc(tf, func(block string) string {
		block = regexp.MustCompile(`(?m)^\s+# TODO: .*\n`).ReplaceAllString(block, "")
		return refsEntry.ReplaceAllStringFunc(block, func(line string) string {
			m := refsEntry.FindStringSubmatch(line)
			key := strings.Trim(m[2], `"`)
			return m[1] + m[2] + m[3] + `= "arn:aws:connect:us-east-1:111122223333:instance/i/bound/` + strings.ReplaceAll(key, ":", "/") + `"`
		})
	})
	return instanceArg.ReplaceAllString(tf, `${1}"11111111-2222-3333-4444-555555555555"`)
}

// Every hcl/roundtrip golden plans to its document byte for byte: the
// contract's parity oracle (rule 26, and decision record 0001).
func TestHCLRoundtripGoldensPlanToTheirDocuments(t *testing.T) {
	terraformBinary(t)
	for _, name := range caseNames(t, "hcl/roundtrip") {
		t.Run(name, func(t *testing.T) {
			dir := "hcl/roundtrip/" + name
			var spec struct{ Doc string }
			if err := json.Unmarshal(readVendored(t, dir+"/case.json"), &spec); err != nil {
				t.Fatal(err)
			}
			golden := string(readVendored(t, dir+"/expected.flow.tf"))
			m := resourceHdr.FindStringSubmatch(golden)
			after, failed := planWithTofu(t, standalone(golden), m[1]+"."+m[2])
			if failed != "" {
				t.Fatalf("plan failed:\n%s", failed)
			}
			if got, want := after["flowdoc"], expectedFlowDoc(t, path.Clean(dir+"/"+spec.Doc)); got != want {
				t.Errorf("planned flowdoc differs from %s\ngot:\n%v\nwant:\n%s", spec.Doc, got, want)
			}
		})
	}
}

func caseNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := fs.ReadDir(conformance.FS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// Every hcl/roundtrip golden also applies against the fake and plans clean
// afterwards, which is Create and Read agreeing on the flow. One case is
// skipped here and only planned above: terraform-plugin-testing decodes plan
// JSON with terraform-json, which cannot read a configuration holding a
// nested attribute named `references`.
func TestHCLRoundtripGoldensApplyAndReadBack(t *testing.T) {
	terraformBinary(t)
	entries, err := fs.ReadDir(conformance.FS(), "hcl/roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := "hcl/roundtrip/" + name
			var spec struct{ Doc string }
			if err := json.Unmarshal(readVendored(t, dir+"/case.json"), &spec); err != nil {
				t.Fatal(err)
			}
			golden := string(readVendored(t, dir+"/expected.flow.tf"))
			m := resourceHdr.FindStringSubmatch(golden)
			if strings.Contains(golden, "references = {") {
				t.Skip("terraform-json cannot decode a plan with an attribute named references")
			}
			address := m[1] + "." + m[2]
			want := expectedFlowDoc(t, path.Clean(dir+"/"+spec.Doc))
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories(connectapi.NewFake()),
				Steps: []resource.TestStep{{
					Config: standalone(golden),
					// Held on the state the plan produced rather than on the
					// plan itself: terraform-plugin-testing cannot parse part
					// of OpenTofu's plan JSON (configuration references), and
					// Create stores the planned flowdoc unchanged.
					ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownValue(address, tfjsonpath.New("flowdoc"), knownvalue.StringExact(want)),
					},
				}},
			})
		})
	}
}
