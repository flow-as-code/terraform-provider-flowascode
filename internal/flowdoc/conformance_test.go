// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"bytes"
	"errors"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

func caseDirs(t *testing.T, family string) []string {
	t.Helper()
	entries, err := fs.ReadDir(conformance.FS(), family)
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

func readFixture(t *testing.T, p string) []byte {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeDoc(t *testing.T, p string) jsonv.Object {
	t.Helper()
	v, err := jsonv.Decode(readFixture(t, p))
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return v.(jsonv.Object)
}

// conformance/layout, as layout.test.ts runs it: autoLayout over the
// document's Actions and StartAction, written as JSON.stringify(actual, null,
// 2) plus a newline, byte for byte.
func TestLayoutConformance(t *testing.T) {
	cases := caseDirs(t, "layout")
	want := []string{"branching", "cycle", "linear", "longest-path", "single", "unreachable"}
	if !reflect.DeepEqual(cases, want) {
		t.Fatalf("layout cases %v, the README lists %v", cases, want)
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			doc := decodeDoc(t, "layout/"+name+"/doc.flowdoc.json")
			content := field(doc, "content")
			actions := field(content, "Actions").([]any)
			start := field(content, "StartAction").(string)
			got := append(jsonv.Encode(LayoutJSON(actions, AutoLayout(actions, &start)), "  "), '\n')
			if expected := readFixture(t, "layout/"+name+"/expected.layout.json"); !bytes.Equal(got, expected) {
				t.Errorf("got\n%s\nwant\n%s", got, expected)
			}
		})
	}
}

// Properties layout.test.ts holds beside the cases.
func TestLayoutProperties(t *testing.T) {
	for _, name := range caseDirs(t, "layout") {
		doc := decodeDoc(t, "layout/"+name+"/doc.flowdoc.json")
		content := field(doc, "content")
		actions := field(content, "Actions").([]any)
		start := field(content, "StartAction").(string)
		layout := AutoLayout(actions, &start)
		if len(layout) != len(actions) {
			t.Errorf("%s: %d positions for %d actions", name, len(layout), len(actions))
		}
		seen := map[Point]bool{}
		for id, p := range layout {
			if seen[p] {
				t.Errorf("%s: %s shares a position", name, id)
			}
			seen[p] = true
			if p.X != float64(int(p.X)) || p.Y != float64(int(p.Y)) {
				t.Errorf("%s: %s is not on integers", name, id)
			}
		}
		if !reflect.DeepEqual(AutoLayout(actions, &start), layout) {
			t.Errorf("%s: not deterministic", name)
		}
		// The start defaults to the first action, which every case starts at.
		if !reflect.DeepEqual(AutoLayout(actions, nil), layout) {
			t.Errorf("%s: the default start differs", name)
		}
	}
}

func TestLayoutEdges(t *testing.T) {
	if got := AutoLayout(nil, nil); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
	doc := decodeDoc(t, "layout/linear/doc.flowdoc.json")
	actions := field(field(doc, "content"), "Actions").([]any)
	nowhere := "nowhere"
	got := AutoLayout(actions, &nowhere)
	for i, id := range LayoutIDs(actions) {
		if p := got[id]; p.X != 20 || p.Y != float64(20+100*i) {
			t.Errorf("unreachable %s at %v", id, p)
		}
	}
}

// conformance/migrate, as conformance.test.ts's "FlowDoc migration" runs it:
// each case's input migrates to the expected bytes through Serialize. The
// schema half of that suite (the input valid at 0.1 and not 0.2, the output
// the reverse) is the schema package's to hold.
func TestMigrateConformance(t *testing.T) {
	cases := caseDirs(t, "migrate")
	if want := []string{"minimal-0.1", "with-meta-0.1"}; !reflect.DeepEqual(cases, want) {
		t.Fatalf("migrate cases %v, want %v", cases, want)
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			input := decodeDoc(t, "migrate/"+c+"/input.flowdoc.json")
			if field(input, "flowdoc") != "0.1" {
				t.Fatalf("input is at %v", field(input, "flowdoc"))
			}
			migrated, err := MigrateFlowDoc(input)
			if err != nil {
				t.Fatal(err)
			}
			if field(input, "flowdoc") != "0.1" {
				t.Error("migration changed its input")
			}
			if got, want := Serialize(migrated), readFixture(t, "migrate/"+c+"/expected.flowdoc.json"); !bytes.Equal(got, want) {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestMigrateReturnsACurrentDocumentUnchanged(t *testing.T) {
	demo := decodeDoc(t, "demo/appointment-line.flowdoc.json")
	got, err := MigrateFlowDoc(demo)
	if err != nil {
		t.Fatal(err)
	}
	if &got[0] != &demo[0] || len(got) != len(demo) {
		t.Error("a current document came back as a copy")
	}
}

func TestMigrateRefusesEveryVersionItDoesNotRead(t *testing.T) {
	versions := field(decodeDoc(t, "migrate/invalid.json"), "versions").([]any)
	if len(versions) <= 3 {
		t.Fatalf("only %d versions", len(versions))
	}
	for _, v := range versions {
		doc := append(jsonv.Object{}, decodeDoc(t, "demo/appointment-line.flowdoc.json")...)
		doc.Set("flowdoc", v)
		_, err := MigrateFlowDoc(doc)
		var inv *InvalidFlowDocError
		if err == nil || !errors.As(err, &inv) || !strings.Contains(err.Error(), "is not supported") {
			t.Errorf("version %s: %v", enc(v), err)
		}
	}
	doc := decodeDoc(t, "demo/appointment-line.flowdoc.json")
	doc.Set("flowdoc", "0.3")
	_, err := MigrateFlowDocIn(doc, "flow-cli lint")
	if want := `flow-cli lint: FlowDoc version "0.3" is not supported; this build reads "0.1" and "0.2".`; err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

// Every roundtrip document's refs index is exactly collectRefs(content): the
// "derived index, regenerated on save" promise of refs.ts, which the demo
// invariant in conformance.test.ts holds for the demo.
func TestRoundtripRefsAreCollectRefsOfContent(t *testing.T) {
	cases := caseDirs(t, "roundtrip")
	if len(cases) < 12 {
		t.Fatalf("only %d roundtrip cases", len(cases))
	}
	paths := []string{"demo/appointment-line.flowdoc.json"}
	for _, c := range cases {
		paths = append(paths, "roundtrip/"+c+"/doc.flowdoc.json")
	}
	withRefs := 0
	for _, p := range paths {
		doc := decodeDoc(t, p)
		refs, ok := doc.Get("refs")
		if !ok {
			refs = []any{}
		}
		if len(refs.([]any)) > 0 {
			withRefs++
		}
		got := refEntriesJSON(CollectRefs(field(doc, "content")))
		if enc(got) != enc(refs) {
			t.Errorf("%s: collectRefs(content) = %s, refs = %s", p, enc(got), enc(refs))
		}
	}
	if withRefs < 5 {
		t.Errorf("only %d documents carry references", withRefs)
	}
}

// The catalog loads, and the facts catalog.test.ts checks of its shape hold
// for the Go structs too.
func TestCatalogLoads(t *testing.T) {
	c := Catalog()
	if c.Catalog != "0.1" || c.FlowLanguage.Version != FlowLanguageVersion {
		t.Errorf("catalog %q, flow language %q", c.Catalog, c.FlowLanguage.Version)
	}
	counts := map[ActionCategory]int{"contact": 27, "participant": 6, "flowControl": 15, "interaction": 8}
	total := 0
	for _, cat := range c.Categories {
		total += len(cat.Types)
		if len(cat.Types) != counts[cat.Name] {
			t.Errorf("category %s lists %d types, want %d", cat.Name, len(cat.Types), counts[cat.Name])
		}
		for _, typ := range cat.Types {
			if e := CatalogEntry(typ); e == nil || e.Category != cat.Name {
				t.Errorf("%s is listed under %s", typ, cat.Name)
			}
		}
	}
	if total != len(c.Actions) {
		t.Errorf("%d entries, categories list %d", len(c.Actions), total)
	}
	var groups []string
	for _, g := range c.FlowTypeGroups {
		groups = append(groups, g.Types...)
	}
	sort.Strings(groups)
	all := []string{"AGENT_HOLD", "AGENT_TRANSFER", "AGENT_WHISPER", "CONTACT_FLOW", "CUSTOMER_HOLD", "CUSTOMER_QUEUE", "CUSTOMER_WHISPER", "MODULE", "OUTBOUND_WHISPER", "QUEUE_TRANSFER"}
	if !reflect.DeepEqual(groups, all) {
		t.Errorf("flowTypeGroups cover %v", groups)
	}
	if len(ModeledTypes()) != 35 {
		t.Errorf("%d modeled types", len(ModeledTypes()))
	}
	for _, a := range c.Actions {
		if a.Modeled != (a.Model != nil) {
			t.Errorf("%s: modeled %v with model %v", a.Type, a.Modeled, a.Model != nil)
		}
		if a.Model == nil {
			continue
		}
		paths := append(append([]string{}, a.Model.TextBodies...), a.Model.Announces...)
		for _, r := range a.Model.Refs {
			paths = append(paths, r.Path)
		}
		if a.Model.RecordingEnabler != "" {
			paths = append(paths, a.Model.RecordingEnabler)
		}
		for _, p := range paths {
			if !IsCatalogPath(p) {
				t.Errorf("%s: %s is not a catalog path", a.Type, p)
			}
		}
	}
	lex := ModeledEntry("ConnectParticipantWithLexBot")
	for _, p := range lex.Parameters {
		if p.Key == "LexTimeoutSeconds" {
			f := p.Fields[0]
			if f.Kind != "integerString" || *f.Min != 60 || *f.Max != 604800 || !f.Required || f.Attr != "text" {
				t.Errorf("LexTimeoutSeconds.Text decoded as %+v", f)
			}
		}
	}
}

func TestCatalogDecodingIsStrict(t *testing.T) {
	raw := readFixture(t, CatalogPath)
	if _, err := ParseCatalog(raw); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string][2]string{
		"an unknown parameter key": {`"attr": "prompt_id",`, `"attr": "prompt_id", "surprise": 1,`},
		"an unknown top-level key": {`"catalog": "0.1",`, `"catalog": "0.1", "extra": 1,`},
		"a flowTypes word":         {`"flowTypes": "unrestricted"`, `"flowTypes": "sometimes"`},
		"an unknown error key":     {`"builder": true`, `"builder": true, "why": "x"`},
	} {
		mutated := strings.Replace(string(raw), edit[0], edit[1], 1)
		if mutated == string(raw) {
			t.Fatalf("%s: the edit did not apply", name)
		}
		if _, err := ParseCatalog([]byte(mutated)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
