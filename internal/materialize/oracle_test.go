// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package materialize

import (
	"errors"
	"os"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// testdata/ts-oracle.json is what @flow-as-code/core's own materialize.ts
// returns for the inputs in testdata/oracle.mjs, recorded by building
// packages/core at the commit the conformance tree is vendored from (named
// in the file's generatedFrom) and running the script over it. Every input
// is JSON text, parsed by JSON.parse there and by jsonv.Decode here.
//
// For each materialize case the oracle holds either the error (its class,
// message, and missingTokens and missingRefs for a MaterializeError) or
// JSON.stringify of the content, the binder's calls in order, and what
// serializeContent wrote for the content or threw. The serializeContent
// cases hold its output, or its error, for content given directly.

// knownDivergences are oracle cases the port does not reproduce, each with
// the reason.
var knownDivergences = map[string]string{
	// A string spread into an object splits into UTF-16 code units; an
	// astral character becomes two lone surrogates, which JSON.stringify
	// escapes as \udXXX and jsonv.Encode, holding them as generalized UTF-8,
	// writes raw.
	"metadata-astral-string": "lone surrogates are written raw, not escaped",
}

type oracleCase = jsonv.Object

func loadOracle(t *testing.T) jsonv.Object {
	t.Helper()
	b, err := os.ReadFile("testdata/ts-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, b).(jsonv.Object)
}

func str(o jsonv.Object, key string) (string, bool) {
	v, ok := o.Get(key)
	s, isString := v.(string)
	return s, ok && isString
}

func mustStr(t *testing.T, o jsonv.Object, key string) string {
	t.Helper()
	s, ok := str(o, key)
	if !ok {
		t.Fatalf("oracle case has no string %q", key)
	}
	return s
}

func enc(v any) string { return string(jsonv.Encode(v, "")) }

func refJSON(e flowdoc.RefEntry) jsonv.Object {
	o := jsonv.Object{{Key: "token", Value: e.Token}, {Key: "type", Value: e.Type}, {Key: "name", Value: e.Name}}
	if e.Alias != "" {
		o = append(o, jsonv.Member{Key: "alias", Value: e.Alias})
	}
	return o
}

func refsJSON(es []flowdoc.RefEntry) []any {
	out := []any{}
	for _, e := range es {
		out = append(out, refJSON(e))
	}
	return out
}

func stringsJSON(ss []string) []any {
	out := []any{}
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// checkError holds err to the oracle's recorded error.
func checkError(t *testing.T, err error, want jsonv.Object) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error; the TypeScript threw %s", enc(want))
	}
	name := mustStr(t, want, "name")
	if msg := mustStr(t, want, "message"); err.Error() != msg {
		t.Errorf("message\n got: %s\nwant: %s", err.Error(), msg)
	}
	var invalid *flowdoc.InvalidFlowDocError
	var typeErr *TypeError
	var matErr *MaterializeError
	switch name {
	case "InvalidFlowDocError":
		if !errors.As(err, &invalid) {
			t.Errorf("error is %T, want *flowdoc.InvalidFlowDocError", err)
		}
	case "TypeError":
		if !errors.As(err, &typeErr) {
			t.Errorf("error is %T, want *TypeError", err)
		}
	case "MaterializeError":
		if !errors.As(err, &matErr) {
			t.Fatalf("error is %T, want *MaterializeError", err)
		}
		tokens, _ := want.Get("missingTokens")
		if got := enc(stringsJSON(matErr.MissingTokens)); got != enc(tokens) {
			t.Errorf("MissingTokens = %s, want %s", got, enc(tokens))
		}
		refs, _ := want.Get("missingRefs")
		if got := enc(refsJSON(matErr.MissingRefs)); got != enc(refs) {
			t.Errorf("MissingRefs = %s, want %s", got, enc(refs))
		}
	default:
		t.Fatalf("oracle error class %q is not mapped", name)
	}
}

func TestOracleMaterialize(t *testing.T) {
	cases := oracleList(t, "materialize")
	if len(cases) < 100 {
		t.Fatalf("oracle holds %d materialize cases", len(cases))
	}
	binderFixture := stringMap(t, decode(t, readConformance(t, "materialize/binder-passthrough/binder.json")))
	for _, c := range cases {
		name := mustStr(t, c, "name")
		t.Run(name, func(t *testing.T) {
			if reason, skip := knownDivergences[name]; skip {
				t.Skip(reason)
			}
			doc := decode(t, []byte(mustStr(t, c, "doc")))
			pristine := enc(doc)
			var calls []flowdoc.RefEntry
			var content jsonv.Object
			var err error
			switch mustStr(t, c, "fn") {
			case "map":
				content, err = MaterializeWithMap(doc, stringMap(t, decode(t, []byte(mustStr(t, c, "map")))))
			case "binder":
				mode := mustStr(t, c, "binder")
				content, err = MaterializeWithBinder(doc, func(ref flowdoc.RefEntry) string {
					calls = append(calls, ref)
					if mode == "fixture" {
						return binderFixture[ref.Token]
					}
					return "B(" + ref.Token + ")"
				})
			}
			if enc(doc) != pristine {
				t.Error("the input document was mutated")
			}
			wantCalls, _ := c.Get("calls")
			if got := enc(refsJSON(calls)); got != enc(wantCalls) {
				t.Errorf("binder calls\n got: %s\nwant: %s", got, enc(wantCalls))
			}
			if want, threw := c.Get("error"); threw {
				checkError(t, err, want.(jsonv.Object))
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got, want := enc(content), mustStr(t, c, "content"); got != want {
				t.Errorf("content\n got: %s\nwant: %s", got, want)
			}
			serialized, serr := SerializeContent(content)
			if want, threw := c.Get("serializeError"); threw {
				checkError(t, serr, want.(jsonv.Object))
				return
			}
			if serr != nil {
				t.Fatalf("serializeContent: %v", serr)
			}
			if want := mustStr(t, c, "serialized"); string(serialized) != want {
				t.Errorf("serializeContent\n got: %s\nwant: %s", serialized, want)
			}
		})
	}
}

func TestOracleSerializeContent(t *testing.T) {
	cases := oracleList(t, "serializeContent")
	if len(cases) < 30 {
		t.Fatalf("oracle holds %d serializeContent cases", len(cases))
	}
	for _, c := range cases {
		name := mustStr(t, c, "name")
		t.Run(name, func(t *testing.T) {
			if reason, skip := knownDivergences[name]; skip {
				t.Skip(reason)
			}
			content := decode(t, []byte(mustStr(t, c, "content"))).(jsonv.Object)
			got, err := SerializeContent(content)
			if want, threw := c.Get("error"); threw {
				checkError(t, err, want.(jsonv.Object))
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if want := mustStr(t, c, "serialized"); string(got) != want {
				t.Errorf("serializeContent\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func oracleList(t *testing.T, key string) []oracleCase {
	t.Helper()
	v, ok := loadOracle(t).Get(key)
	if !ok {
		t.Fatalf("oracle has no %q", key)
	}
	var out []oracleCase
	for _, item := range v.([]any) {
		out = append(out, item.(jsonv.Object))
	}
	return out
}
