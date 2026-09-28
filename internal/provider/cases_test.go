// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Stand-ins for what a case refers to but this test does not load. The
// provider sees values, not expressions (decision record 0001), so what
// matters is whether a value is known at plan: an address of a resource that
// does not exist yet is unknown, which terraform_data's id is until apply.
var (
	addressInField = regexp.MustCompile(`(?m)^(\s+[a-z_0-9]+\s*=\s*.*?)\b((?:data\.)?(?:aws|flowascode)_[a-z_0-9]+\.([a-z_0-9]+)\.arn)\b`)
	localRef       = regexp.MustCompile(`\blocal\.([a-z_0-9]+)\b`)
	varRef         = regexp.MustCompile(`\bvar\.([a-z_0-9]+)\b`)
	moduleRef      = regexp.MustCompile(`\bmodule\.([a-z_0-9]+)\.([a-z_0-9]+)\b`)
)

// standIn makes a case input plannable here: refs values and the instance
// become literals (standalone), resource addresses in action fields become
// terraform_data ids, and every local, variable and module output it names is
// declared, the first two as stand-ins for a resource address and an ARN.
func standIn(input string) map[string]string {
	text := standalone(input)
	stubs := map[string]bool{}
	text = addressInField.ReplaceAllStringFunc(text, func(line string) string {
		m := addressInField.FindStringSubmatch(line)
		name := "stub_" + strings.ReplaceAll(strings.TrimPrefix(m[2], "data."), ".", "_")
		stubs[name] = true
		return m[1] + "terraform_data." + name + ".id"
	})
	extra := []string{}
	for _, m := range localRef.FindAllStringSubmatch(text, -1) {
		name := "stub_local_" + m[1]
		if !stubs[name] {
			stubs[name] = true
			extra = append(extra, fmt.Sprintf("locals {\n  %s = terraform_data.%s.id\n}\n", m[1], name))
		}
	}
	seenVar := map[string]bool{}
	for _, m := range varRef.FindAllStringSubmatch(text, -1) {
		if !seenVar[m[1]] {
			seenVar[m[1]] = true
			extra = append(extra, fmt.Sprintf("variable %q {\n  default = \"arn:aws:connect:us-east-1:111122223333:instance/i/queue/%s\"\n}\n", m[1], m[1]))
		}
	}
	files := map[string]string{}
	seenModule := map[string]bool{}
	for _, m := range moduleRef.FindAllStringSubmatch(text, -1) {
		if !seenModule[m[1]] {
			seenModule[m[1]] = true
			extra = append(extra, fmt.Sprintf("module %q {\n  source = \"./%s\"\n}\n", m[1], m[1]))
			files[m[1]+"/main.tf"] = fmt.Sprintf("resource \"terraform_data\" \"stub\" {}\noutput %q {\n  value = terraform_data.stub.id\n}\n", m[2])
		}
	}
	names := make([]string, 0, len(stubs))
	for n := range stubs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		extra = append(extra, fmt.Sprintf("resource \"terraform_data\" %q {}\n", n))
	}
	files["main.tf"] = text + "\n" + strings.Join(extra, "\n")
	return files
}

// caseSpec is a parse or refuse case's case.json, as far as the provider
// reads it.
type caseSpec struct {
	Description    string `json:"description"`
	TypescriptOnly string `json:"typescriptOnly"`
	TerraformError string `json:"terraformError"`
	ProviderCode   string `json:"providerCode"`
}

func readSpec(t *testing.T, dir string) caseSpec {
	t.Helper()
	var s caseSpec
	if err := json.Unmarshal(readVendored(t, dir+"/case.json"), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Every refuse case the provider can see is refused with the case's code in
// the diagnostic summary.
func TestHCLRefuseCases(t *testing.T) {
	terraformBinary(t)
	for _, name := range caseNames(t, "hcl/refuse") {
		t.Run(name, func(t *testing.T) {
			dir := "hcl/refuse/" + name
			spec := readSpec(t, dir)
			if spec.TypescriptOnly != "" {
				t.Skip(spec.TypescriptOnly)
			}
			var want struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(readVendored(t, dir+"/expected-error.json"), &want); err != nil {
				t.Fatal(err)
			}
			expect := "Error: " + want.Code
			switch {
			case spec.TerraformError != "":
				expect = "Error: " + spec.TerraformError
			case spec.ProviderCode != "":
				expect = "Error: " + spec.ProviderCode
			}
			input := string(readVendored(t, dir+"/input.flow.tf"))
			m := resourceHdr.FindStringSubmatch(input)
			if m != nil && m[1] != "flowascode_contact_flow" {
				t.Skipf("%s is not implemented yet (B04h)", m[1])
			}
			_, failed := planFiles(t, standIn(input), m[1]+"."+m[2])
			if failed == "" {
				t.Fatalf("planned without error; want %q", expect)
			}
			if !strings.Contains(failed, expect) {
				t.Errorf("want %q in the plan's errors, got:\n%s", expect, failed)
			}
		})
	}
}

// Every parse case plans to its expected document; the sugar-* cases, which
// only the TypeScript reader rewrites, are refused with
// REF_EXPRESSION_REFUSED (conformance/hcl/README.md, "Families").
func TestHCLParseCases(t *testing.T) {
	terraformBinary(t)
	for _, name := range caseNames(t, "hcl/parse") {
		t.Run(name, func(t *testing.T) {
			dir := "hcl/parse/" + name
			if spec := readSpec(t, dir); spec.TypescriptOnly != "" {
				t.Skip(spec.TypescriptOnly)
			}
			input := string(readVendored(t, dir+"/input.flow.tf"))
			m := resourceHdr.FindStringSubmatch(input)
			if m[1] != "flowascode_contact_flow" {
				t.Skipf("%s is not implemented yet (B04h)", m[1])
			}
			after, failed := planFiles(t, standIn(input), m[1]+"."+m[2])
			if strings.HasPrefix(name, "sugar-") {
				if !strings.Contains(failed, "Error: REF_EXPRESSION_REFUSED") {
					t.Fatalf("want REF_EXPRESSION_REFUSED, got:\n%s", failed)
				}
				return
			}
			if failed != "" {
				t.Fatalf("plan failed:\n%s", failed)
			}
			if got, want := after["flowdoc"], expectedFlowDoc(t, dir+"/expected.flowdoc.json"); got != want {
				t.Errorf("planned flowdoc differs\ngot:\n%v\nwant:\n%s", got, want)
			}
		})
	}
}
