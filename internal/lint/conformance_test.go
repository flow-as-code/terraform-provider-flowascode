// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The conformance/lint runner, as packages/core/src/lint.test.ts runs it.

func readJSON(t *testing.T, p string) any {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), p)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonv.Decode(b)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return v
}

// docsOf is lint.test.ts's docsOf: parsed.docs, else parsed.doc, else the
// file itself is the document.
func docsOf(parsed any) []any {
	o, _ := parsed.(jsonv.Object)
	if docs, ok := o.Get("docs"); ok && docs != nil {
		return docs.([]any)
	}
	if doc, ok := o.Get("doc"); ok {
		return []any{doc}
	}
	return []any{parsed}
}

func ruleDirs(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(conformance.FS(), "lint")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func ids(rules []Rule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.ID
	}
	return out
}

// "A02 acceptance: every rule has fixtures".
func TestRegistry(t *testing.T) {
	registered := ids(AllRules())
	sorted := append([]string(nil), registered...)
	sort.Strings(sorted)
	if dirs := ruleDirs(t); !reflect.DeepEqual(dirs, sorted) {
		t.Fatalf("conformance/lint has %v, the registry %v", dirs, sorted)
	}
	// Spelled out: deleting a rule and its fixtures together must not pass.
	want := []string{
		"action-allowed-in-flow-type", "action-count", "conditional-shape", "error-branches", "module-depth-5",
		"no-literal-arn", "no-unresolved-token", "prompt-length-3000", "reachable-blocks",
		"recording-consent-before-record", "terminal-blocks", "unique-names",
	}
	if !reflect.DeepEqual(sorted, want) {
		t.Fatalf("registry %v, want %v", sorted, want)
	}
	// Registry order is rules/index.ts's.
	order := []string{
		"no-literal-arn", "no-unresolved-token", "reachable-blocks", "error-branches",
		"terminal-blocks", "module-depth-5", "prompt-length-3000", "recording-consent-before-record",
		"unique-names", "action-allowed-in-flow-type", "action-count", "conditional-shape",
	}
	if !reflect.DeepEqual(registered, order) {
		t.Fatalf("registry order %v, want %v", registered, order)
	}
	var hard []string
	for _, r := range AllRules() {
		if r.Hard {
			hard = append(hard, r.ID)
		}
	}
	if !reflect.DeepEqual(hard, []string{"no-literal-arn", "no-unresolved-token"}) {
		t.Fatalf("hard rules %v", hard)
	}
	for _, id := range order {
		if r, ok := RuleByID(id); !ok || r.ID != id {
			t.Fatalf("RuleByID(%q) = %v, %v", id, r.ID, ok)
		}
	}
	if _, ok := RuleByID("nope"); ok {
		t.Fatal("RuleByID found a rule that does not exist")
	}
}

func TestLintConformance(t *testing.T) {
	total := 0
	for _, rule := range ruleDirs(t) {
		entries, err := fs.ReadDir(conformance.FS(), "lint/"+rule)
		if err != nil {
			t.Fatal(err)
		}
		var passes, fails []string
		for _, e := range entries {
			switch name := e.Name(); {
			case e.IsDir() || !strings.HasSuffix(name, ".json"):
			case strings.HasPrefix(name, "pass-"):
				passes = append(passes, name)
			case strings.HasPrefix(name, "fail-"):
				fails = append(fails, name)
			}
		}
		if len(passes) == 0 || len(fails) == 0 {
			t.Errorf("%s needs a pass and a fail fixture, has %v and %v", rule, passes, fails)
		}
		for _, file := range passes {
			total++
			p := "lint/" + rule + "/" + file
			found, err := Lint(docsOf(readJSON(t, p)), Options{})
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			if got := only(found, rule); len(got) != 0 {
				t.Errorf("%s: want no %s finding, got %+v", p, rule, got)
			}
		}
		for _, file := range fails {
			total++
			p := "lint/" + rule + "/" + file
			fixture := readJSON(t, p).(jsonv.Object)
			found, err := Lint(docsOf(fixture), Options{})
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			got := only(found, rule)
			expect, _ := fixture.Get("expect")
			want := expect.([]any)
			if len(got) != len(want) {
				t.Errorf("%s: expected %d finding(s), got %d: %s", p, len(want), len(got), ToJSON(got))
				continue
			}
			for i, w := range want {
				wo := w.(jsonv.Object)
				if r := str(wo, "rule"); got[i].Rule != r {
					t.Errorf("%s[%d]: rule %q, want %q", p, i, got[i].Rule, r)
				}
				if b, has := wo.Get("blockId"); has && deref(got[i].BlockID) != b.(string) {
					t.Errorf("%s[%d]: blockId %q, want %q", p, i, deref(got[i].BlockID), b)
				}
				if m := str(wo, "messageIncludes"); !strings.Contains(got[i].Message, m) {
					t.Errorf("%s[%d]: message %q does not contain %q", p, i, got[i].Message, m)
				}
			}
		}
	}
	if total != 83 {
		t.Errorf("ran %d lint fixtures, the vendored tree has 83", total)
	}
}

func only(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// "the demo flow is clean under every rule".
func TestDemoClean(t *testing.T) {
	found, err := Lint([]any{readJSON(t, "demo/appointment-line.flowdoc.json")}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("demo: %s", ToJSON(found))
	}
}
