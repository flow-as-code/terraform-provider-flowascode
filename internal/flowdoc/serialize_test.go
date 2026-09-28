// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// vendoredDocs returns every FlowDoc-shaped JSON file in the vendored tree.
func vendoredDocs(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	root := conformance.FS()
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		v, err := jsonv.Decode(b)
		if err != nil {
			return nil
		}
		o, ok := v.(jsonv.Object)
		if !ok {
			return nil
		}
		if _, ok := o.Get("flowdoc"); !ok {
			return nil
		}
		if _, ok := o.Get("content"); !ok {
			return nil
		}
		out[p] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every vendored document the TypeScript serializer writes byte-canonically
// (59 at the vendored commit) the Go serializer writes identically, and the
// rest reach a fixed point after one pass, as they do in TypeScript.
func TestSerializeMatchesTheTypeScriptBytes(t *testing.T) {
	docs := vendoredDocs(t)
	canonical := 0
	for p, raw := range docs {
		v, _ := jsonv.Decode(raw)
		got := Serialize(v.(jsonv.Object))
		if bytes.Equal(got, raw) {
			canonical++
		}
		again, _ := jsonv.Decode(got)
		if !bytes.Equal(Serialize(again.(jsonv.Object)), got) {
			t.Errorf("%s: serialize is not a fixed point", p)
		}
	}
	if canonical < 59 {
		t.Errorf("only %d of %d vendored documents re-serialize byte-identically; TypeScript manages 59", canonical, len(docs))
	}
}

// Checked against @flow-as-code/core's serialize with node.
func TestCanonicalActionEdgesMatchTypeScript(t *testing.T) {
	in := `{"flowdoc":"0.2","kind":"flow","name":"x","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Type":"Compare","Identifier":"a","Parameters":{"b":1,"10":2,"9":3},"Transitions":{"Conditions":[{"NextAction":null},{"Condition":{"Operands":["1"],"Operator":"Equals"}}]}}]}}`
	v, err := jsonv.Decode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	got := string(jsonv.Encode(Canonicalize(v.(jsonv.Object)), ""))
	want := `{"flowdoc":"0.2","kind":"flow","name":"x","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"Compare","Parameters":{"9":3,"10":2,"b":1},"Transitions":{"Conditions":[{"NextAction":null,"Condition":{}},{"Condition":{"Operator":"Equals","Operands":["1"]}}]}}]}}`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
