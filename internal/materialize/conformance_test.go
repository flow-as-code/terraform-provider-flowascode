// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package materialize

import (
	"bytes"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The conformance/materialize family, as materialize.test.ts's "A05
// conformance fixtures" runs it: each case directory holds doc.flowdoc.json,
// either map.json (materializeWithMap) or binder.json (materializeWithBinder
// with a binder that looks the token up in it), and expected.content.json,
// which serializeContent must reproduce byte for byte.

func readConformance(t *testing.T, p string) []byte {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t *testing.T, b []byte) any {
	t.Helper()
	v, err := jsonv.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// stringMap is a JSON object of strings as a Go map; JSON null is a nil map.
func stringMap(t *testing.T, v any) map[string]string {
	t.Helper()
	if v == nil {
		return nil
	}
	out := map[string]string{}
	for _, m := range v.(jsonv.Object) {
		out[m.Key] = m.Value.(string)
	}
	return out
}

func conformanceCases(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(conformance.FS(), "materialize")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestConformanceMaterialize(t *testing.T) {
	names := conformanceCases(t)
	want := []string{"binder-passthrough", "demo-with-map", "view-with-version"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("materialize cases = %v, want %v; a new case needs a look at its format", names, want)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := path.Join("materialize", name)
			doc := decode(t, readConformance(t, path.Join(dir, "doc.flowdoc.json")))
			var content jsonv.Object
			var err error
			if b, readErr := fs.ReadFile(conformance.FS(), path.Join(dir, "map.json")); readErr == nil {
				content, err = MaterializeWithMap(doc, stringMap(t, decode(t, b)))
			} else {
				bound := stringMap(t, decode(t, readConformance(t, path.Join(dir, "binder.json"))))
				content, err = MaterializeWithBinder(doc, func(ref flowdoc.RefEntry) string {
					v, ok := bound[ref.Token]
					if !ok {
						t.Fatalf("binder.json has no %s", ref.Token)
					}
					return v
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := SerializeContent(content)
			if err != nil {
				t.Fatal(err)
			}
			if want := readConformance(t, path.Join(dir, "expected.content.json")); !bytes.Equal(got, want) {
				t.Errorf("serializeContent differs from expected.content.json\ngot:\n%s\nwant:\n%s", got, want)
			}
			if bytes.Contains(got, []byte("cdref")) {
				t.Error("a token survived")
			}
		})
	}
}

// materialize.test.ts: "keeps the demo-with-map doc in sync with the
// canonical demo".
func TestConformanceDemoInSync(t *testing.T) {
	if !bytes.Equal(
		readConformance(t, "materialize/demo-with-map/doc.flowdoc.json"),
		readConformance(t, "demo/appointment-line.flowdoc.json"),
	) {
		t.Error("materialize/demo-with-map/doc.flowdoc.json differs from demo/appointment-line.flowdoc.json")
	}
}

// view-with-version: the map is keyed by type and name with the version in
// the alias slot, and the value is the versioned ARN.
func TestConformanceViewWithVersion(t *testing.T) {
	doc := decode(t, readConformance(t, "materialize/view-with-version/doc.flowdoc.json"))
	content, err := MaterializeWithMap(doc, map[string]string{
		"view:after-contact-work@1": "arn:aws:connect:us-east-1:aws:view/after-contact-work:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := SerializeContent(content)
	if !bytes.Contains(got, []byte("arn:aws:connect:us-east-1:aws:view/after-contact-work:1")) {
		t.Error("the versioned ARN is not in the content")
	}
}
