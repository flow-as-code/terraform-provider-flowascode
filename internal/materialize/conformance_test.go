// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package materialize

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path"
	"reflect"
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
	want := []string{"binder-passthrough", "casefield-key-with-map", "demo-with-map", "phonenumber-with-map", "view-with-version"}
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

// casefield-key-with-map: a casefield token stands as a whole map key of
// CaseRequestFields and as a list item of CaseResponseFields (invariant 4 as
// docs/adr/0008-case-field-ids.md restates it); both resolve through the one
// token rule. The conformance run above holds the map path to the golden.
func TestConformanceCasefieldKeyResolvesOnTheMapPath(t *testing.T) {
	doc := decode(t, readConformance(t, "materialize/casefield-key-with-map/doc.flowdoc.json"))
	m := stringMap(t, decode(t, readConformance(t, "materialize/casefield-key-with-map/map.json")))
	content, err := MaterializeWithMap(doc, m)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := SerializeContent(content)
	if !bytes.Contains(got, []byte(`"bbbbbbbb-1111-2222-3333-444444444444": "high"`)) {
		t.Errorf("the casefield key did not resolve:\n%s", got)
	}
}

// materialize.test.ts: "keeps the casefield-key-with-map doc in sync with the
// HCL golden".
func TestConformanceCasefieldDocInSyncWithHCLGolden(t *testing.T) {
	if !bytes.Equal(
		readConformance(t, "materialize/casefield-key-with-map/doc.flowdoc.json"),
		readConformance(t, "hcl/roundtrip/casefield-key/casefield-key.flowdoc.json"),
	) {
		t.Error("materialize/casefield-key-with-map/doc.flowdoc.json differs from the HCL golden's document")
	}
}

// materialize.test.ts: "resolves a casefield map key through the binder as it
// resolves a value".
func TestConformanceCasefieldKeyResolvesThroughTheBinder(t *testing.T) {
	doc := decode(t, readConformance(t, "materialize/casefield-key-with-map/doc.flowdoc.json"))
	content, err := MaterializeWithBinder(doc, func(r flowdoc.RefEntry) string { return "BOUND(" + r.Type + ":" + r.Name + ")" })
	if err != nil {
		t.Fatal(err)
	}
	actions, _ := content.Get("Actions")
	params := func(i int) any {
		p, _ := actions.([]any)[i].(jsonv.Object).Get("Parameters")
		return p
	}
	want := `{"CaseRequestFields":{"BOUND(casefield:priority)":"high","BOUND(casefield:summary)":"$.Attributes.summary"},"CaseTemplateId":"BOUND(casetemplate:billing-dispute)","LinkContactToCase":"true"}`
	if got := string(jsonv.Encode(params(0), "")); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	if got, _ := params(1).(jsonv.Object).Get("CaseResponseFields"); string(jsonv.Encode(got, "")) != `["BOUND(casefield:priority)"]` {
		t.Errorf("CaseResponseFields = %s", jsonv.Encode(got, ""))
	}
	if bytes.Contains(jsonv.Encode(content, ""), []byte("cdref")) {
		t.Error("a token survived")
	}
}

// materialize.test.ts: "phonenumber-with-map matches its golden and the
// actions the export read" and "keeps the phonenumber-with-map doc in sync
// with the export golden" (flow-as-code D04). The document is
// conformance/export/outbound-sms's exported missed-call flow; materialized
// with its map it is the content the export read, compared as values, and
// the SMS source is the number's un-nested ARN.
func TestConformancePhoneNumberRoundTripsTheExport(t *testing.T) {
	if !bytes.Equal(
		readConformance(t, "materialize/phonenumber-with-map/doc.flowdoc.json"),
		readConformance(t, "export/outbound-sms/expected/missed-call-text.flowdoc.json"),
	) {
		t.Error("materialize/phonenumber-with-map/doc.flowdoc.json differs from the export golden")
	}
	doc := decode(t, readConformance(t, "materialize/phonenumber-with-map/doc.flowdoc.json"))
	m := stringMap(t, decode(t, readConformance(t, "materialize/phonenumber-with-map/map.json")))
	content, err := MaterializeWithMap(doc, m)
	if err != nil {
		t.Fatal(err)
	}
	var got, want struct{ Actions []any }
	if err := json.Unmarshal(jsonv.Encode(content, ""), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readConformance(t, "export/outbound-sms/flows/cccc3333-0000-4000-8000-000000000051.json"), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Actions, want.Actions) {
		t.Errorf("materialized actions differ from the exported flow's:\n%s\n---\n%s", jsonv.Encode(content, ""), readConformance(t, "export/outbound-sms/flows/cccc3333-0000-4000-8000-000000000051.json"))
	}
	if !bytes.Contains(jsonv.Encode(content, ""), []byte(`"arn:aws:connect:us-east-1:111122223333:phone-number/pppp7777-0000-4000-8000-000000000051"`)) {
		t.Error("the SMS source is not the number's ARN")
	}
}
