// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The examples the registry documentation embeds are formatted, and every
// resource example plans against the fake with the lint and schema checks a
// real plan runs, so an example cannot rot. The module, version and alias
// examples refer to each other and plan together.
func TestExamplesPlan(t *testing.T) {
	terraformBinary(t)
	root := filepath.Join("..", "..", "examples")
	fmtCmd := exec.Command(os.Getenv("TF_ACC_TERRAFORM_PATH"), "fmt", "-check", "-recursive", root)
	if out, err := fmtCmd.CombinedOutput(); err != nil {
		t.Fatalf("examples are not formatted:\n%s", out)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	sets := map[string][]string{
		"flowascode_contact_flow.appointment_line": {"resources/flowascode_contact_flow/resource.tf"},
		"flowascode_contact_flow_module_alias.survey_prod": {
			"resources/flowascode_contact_flow_module/resource.tf",
			"resources/flowascode_contact_flow_module_version/resource.tf",
			"resources/flowascode_contact_flow_module_alias/resource.tf",
		},
	}
	for address, files := range sets {
		t.Run(address, func(t *testing.T) {
			var text []string
			for _, f := range files {
				text = append(text, read(f))
			}
			_, failed := planFiles(t, standIn(strings.Join(text, "\n")), address)
			if failed != "" {
				t.Fatalf("plan failed:\n%s", failed)
			}
		})
	}
}

// Every hcl block in README.md plans the same way, so the example on the
// repository's front page cannot rot either.
func TestReadmeExamplesPlan(t *testing.T) {
	terraformBinary(t)
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```hcl\n(.*?)```").FindAllStringSubmatch(string(b), -1)
	if len(blocks) == 0 {
		t.Fatal("README.md has no hcl block")
	}
	resourceLabel := regexp.MustCompile(`resource "(flowascode_[a-z_]+)" "([a-z0-9_]+)"`)
	for i, m := range blocks {
		r := resourceLabel.FindStringSubmatch(m[1])
		if r == nil {
			t.Fatalf("hcl block %d declares no flowascode resource", i)
		}
		t.Run(r[1]+"."+r[2], func(t *testing.T) {
			_, failed := planFiles(t, standIn(m[1]), r[1]+"."+r[2])
			if failed != "" {
				t.Fatalf("plan failed:\n%s", failed)
			}
		})
	}
}
