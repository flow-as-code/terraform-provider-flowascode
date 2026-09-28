// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// testdata/ts-oracle.json is what @flow-as-code/core itself returns for the
// inputs below, recorded by running its built packages/core/dist (at the
// commit the conformance tree is vendored from, named in the file's
// generatedFrom) over them. Inputs that depend on JavaScript's key order are
// JSON texts, parsed by JSON.parse on the TypeScript side and by jsonv.Decode
// here, so the two sides start from the same bytes.
//
// The per-type tables are the actions.ts tables the lint rules read
// (FLOW_TYPE_RESTRICTIONS, FLOW_TYPE_UNRESTRICTED, TERMINAL_ACTIONS,
// ActionType, REFERENCE_FIELDS through refPathsOf) and every catalog.ts
// helper, so the catalog-derived Go functions are held to the TypeScript's
// hand-written tables as catalog.test.ts holds the TypeScript's.

func loadOracle(t *testing.T) jsonv.Object {
	t.Helper()
	b, err := os.ReadFile("testdata/ts-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonv.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return v.(jsonv.Object)
}

func field(o any, key string) any {
	v, _ := o.(jsonv.Object).Get(key)
	return v
}

func strs(v any) []string {
	out := []string{}
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

func enc(v any) string { return string(jsonv.Encode(v, "")) }

func refEntryJSON(e RefEntry) jsonv.Object {
	o := jsonv.Object{{Key: "token", Value: e.Token}, {Key: "type", Value: e.Type}, {Key: "name", Value: e.Name}}
	if e.Alias != "" {
		o = append(o, jsonv.Member{Key: "alias", Value: e.Alias})
	}
	return o
}

func refEntriesJSON(es []RefEntry) []any {
	out := []any{}
	for _, e := range es {
		out = append(out, refEntryJSON(e))
	}
	return out
}

func TestOraclePerTypeTables(t *testing.T) {
	perType := field(loadOracle(t), "perType").(jsonv.Object)
	if len(perType) != len(Catalog().Actions)+2 {
		t.Fatalf("oracle holds %d types", len(perType))
	}
	chat := jsonv.Object{{Key: "ChatBehavior", Value: nil}}
	for _, m := range perType {
		typ, want := m.Key, m.Value
		check := func(name string, got any) {
			t.Helper()
			if enc(got) != enc(field(want, name)) {
				t.Errorf("%s %s: got %s, TypeScript %s", typ, name, enc(got), enc(field(want, name)))
			}
		}
		anyStrs := func(s []string) []any {
			out := []any{}
			for _, x := range s {
				out = append(out, x)
			}
			return out
		}
		orNull := func(s string, ok bool) any {
			if !ok {
				return nil
			}
			return s
		}
		check("modeled", ModeledEntry(typ) != nil)
		check("requiredErrors", anyStrs(RequiredErrors(typ)))
		check("requiredErrorsForChat", anyStrs(RequiredErrorsFor(typ, chat)))
		check("builderErrors", anyStrs(BuilderErrors(typ)))
		k, ok := ConditionsKindOf(typ)
		check("conditionsKind", orNull(string(k), ok))
		check("nextRule", orNull(NextRule(typ)))
		check("holdsParticipant", HoldsParticipant(typ))
		check("textBodyPaths", anyStrs(TextBodyPaths(typ)))
		check("announcePaths", anyStrs(AnnouncePaths(typ)))
		check("recordingEnablerPath", orNull(RecordingEnablerPath(typ)))
		refs := []any{}
		for _, r := range RefPathsOf(typ) {
			refs = append(refs, jsonv.Object{{Key: "path", Value: r.Path}, {Key: "ref", Value: r.Ref}})
		}
		check("refPaths", refs)
		if list, ok := FlowTypeRestrictions(typ); ok {
			check("restrictions", anyStrs(list))
		} else {
			check("restrictions", nil)
		}
		check("unrestricted", IsFlowTypeUnrestricted(typ))
		check("terminal", IsTerminalAction(typ))
		check("actionType", IsModeledType(typ))
	}
}

func TestOracleModeledTypesInCatalogOrder(t *testing.T) {
	want := strs(field(loadOracle(t), "catalogOrder"))
	if got := ModeledTypes(); !reflect.DeepEqual(got, want) {
		t.Errorf("ModeledTypes() = %v, TypeScript %v", got, want)
	}
}

func TestOracleCollectRefs(t *testing.T) {
	for _, c := range field(loadOracle(t), "collect").([]any) {
		in := field(c, "in").(string)
		v, err := jsonv.Decode([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := enc(refEntriesJSON(CollectRefs(v))), enc(field(c, "out")); got != want {
			t.Errorf("CollectRefs(%s) = %s, TypeScript %s", in, got, want)
		}
	}
}

func TestOracleAssertFlowDoc(t *testing.T) {
	for _, c := range field(loadOracle(t), "asserts").([]any) {
		in := field(c, "in")
		if in == "__undefined__" {
			continue // Go has no undefined; nil is null
		}
		_, err := AssertFlowDoc(in, "ctx")
		want := field(c, "out")
		if msg, ok := field(want, "err").(string); ok {
			var inv *InvalidFlowDocError
			if err == nil || !errors.As(err, &inv) || err.Error() != msg {
				t.Errorf("AssertFlowDoc(%s) = %v, TypeScript throws %q", enc(in), err, msg)
			}
		} else if err != nil {
			t.Errorf("AssertFlowDoc(%s) = %v, TypeScript passes", enc(in), err)
		}
	}
}

func TestOracleMigrateFlowDoc(t *testing.T) {
	for _, c := range field(loadOracle(t), "migrates").([]any) {
		in := field(c, "in")
		got, err := MigrateFlowDoc(in)
		want := field(c, "out")
		if msg, ok := field(want, "err").(string); ok {
			if err == nil || err.Error() != msg {
				t.Errorf("MigrateFlowDoc(%s) = %v, TypeScript throws %q", enc(in), err, msg)
			}
			continue
		}
		if err != nil {
			t.Errorf("MigrateFlowDoc(%s): %v", enc(in), err)
			continue
		}
		if enc(jsOrdered(got)) != enc(field(want, "ok")) {
			t.Errorf("MigrateFlowDoc(%s) = %s, TypeScript %s", enc(in), enc(got), enc(field(want, "ok")))
		}
	}
}

func TestOracleReadPath(t *testing.T) {
	for _, c := range field(loadOracle(t), "paths").([]any) {
		in, p := field(c, "in").(string), field(c, "path").(string)
		v, _ := jsonv.Decode([]byte(in))
		hits, err := ReadPath(v, p)
		want := field(c, "out")
		if msg, ok := field(want, "err").(string); ok {
			if err == nil || err.Error() != msg {
				t.Errorf("ReadPath(%s, %q) = %v, TypeScript throws %q", in, p, err, msg)
			}
			continue
		}
		got := []any{}
		for _, h := range hits {
			got = append(got, jsonv.Object{{Key: "path", Value: h.Path}, {Key: "value", Value: h.Value}})
		}
		if enc(got) != enc(field(want, "ok")) {
			t.Errorf("ReadPath(%s, %q) = %s, TypeScript %s", in, p, enc(got), enc(field(want, "ok")))
		}
	}
}

func TestOracleNamesAndTokens(t *testing.T) {
	o := loadOracle(t)
	for _, c := range field(o, "idents").([]any) {
		in := field(c, "in").(string)
		if got := IsValidIdentifier(in); got != field(c, "out").(bool) {
			t.Errorf("IsValidIdentifier(%q) = %v", in, got)
		}
	}
	for _, c := range field(o, "slugs").([]any) {
		if got := SlugIdentifier(field(c, "in").(string)); got != field(c, "out") {
			t.Errorf("SlugIdentifier(%v) = %q", field(c, "in"), got)
		}
	}
	for _, c := range field(o, "tokens").([]any) {
		args := strs(field(c, "in"))
		got, err := Token(args[0], args[1], args[2])
		if msg, ok := field(field(c, "out"), "err").(string); ok {
			if err == nil || err.Error() != msg {
				t.Errorf("Token%v = %v, TypeScript throws %q", args, err, msg)
			}
		} else if err != nil || got != field(field(c, "out"), "ok") {
			t.Errorf("Token%v = %q, %v", args, got, err)
		}
	}
	for _, c := range field(o, "jsonPaths").([]any) {
		in := field(c, "in").(string)
		got, err := JSONPath(in)
		if msg, ok := field(field(c, "out"), "err").(string); ok {
			if err == nil || err.Error() != msg {
				t.Errorf("JSONPath(%q) = %v, TypeScript throws %q", in, err, msg)
			}
		} else if err != nil || got != in {
			t.Errorf("JSONPath(%q) = %q, %v", in, got, err)
		}
	}
}

func TestOracleAutoLayout(t *testing.T) {
	for _, c := range field(loadOracle(t), "layouts").([]any) {
		actions := field(c, "in").([]any)
		var start *string
		if s, ok := field(c, "start").(string); ok {
			start = &s
		}
		got := enc(LayoutJSON(actions, AutoLayout(actions, start)))
		if want := field(c, "out").(string); got != want {
			t.Errorf("AutoLayout(%s) = %s, TypeScript %s", enc(actions), got, want)
		}
	}
}
