// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Every document packages/core/src/conformance.test.ts builds gets Ajv's
// verdict here.
func TestDocCases(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range docCases() {
		if seen[c.id()] {
			t.Fatalf("duplicate case %q", c.id())
		}
		seen[c.id()] = true
		t.Run(c.id(), func(t *testing.T) {
			vs, err := ValidateVersion(c.version, c.doc(t))
			if err != nil {
				t.Fatal(err)
			}
			if c.valid && len(vs) > 0 {
				t.Fatalf("rejected: %v", vs)
			}
			if !c.valid && len(vs) == 0 {
				t.Fatal("accepted")
			}
		})
	}
}

// "all conformance fixtures are schema-valid": every *.flowdoc.json and lint
// pass-*.json outside schema/ and migrate/, each document in it (its docs,
// its doc, or itself) valid at 0.2.
func TestConformanceFixturesAreSchemaValid(t *testing.T) {
	root := conformance.FS()
	var files []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(p, "schema/") || strings.HasPrefix(p, "migrate/") {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".flowdoc.json") || strings.HasPrefix(d.Name(), "pass-") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) <= 15 {
		t.Fatalf("found only %d fixtures", len(files))
	}
	for _, p := range files {
		t.Run(p, func(t *testing.T) {
			parsed := load(t, p)
			obj, _ := parsed.(jsonv.Object)
			var docs []any
			if v, ok := obj.Get("docs"); ok {
				docs, _ = v.([]any)
			} else if v, ok := obj.Get("doc"); ok {
				docs = []any{v}
			} else if _, ok := obj.Get("flowdoc"); ok {
				docs = []any{parsed}
			}
			if len(docs) == 0 {
				t.Fatal("no document in the file")
			}
			for _, d := range docs {
				vs, err := ValidateVersion("0.2", d)
				if err != nil {
					t.Fatal(err)
				}
				if len(vs) > 0 {
					t.Errorf("%v", vs)
				}
			}
		})
	}
}

// Validate picks the schema from the flowdoc field, as docs.ts does, and
// words an unreadable version as unsupportedVersion does.
func TestValidateDispatchesOnVersion(t *testing.T) {
	demo := load(t, demoFixture)
	if vs, err := Validate(demo); err != nil || len(vs) != 0 {
		t.Fatalf("demo: %v %v", vs, err)
	}
	old := load(t, "migrate/minimal-0.1/input.flowdoc.json")
	if vs, err := Validate(old); err != nil || len(vs) != 0 {
		t.Fatalf("0.1 input: %v %v", vs, err)
	}
	for _, tc := range []struct {
		in   string
		want string
	}{
		{`{"flowdoc":"0.3"}`, `/flowdoc must be "0.1" or "0.2", got "0.3"`},
		{`{"flowdoc":1}`, `/flowdoc must be "0.1" or "0.2", got 1`},
		{`{"flowdoc":null}`, `/flowdoc must be "0.1" or "0.2", got null`},
		{`{"name":"x"}`, `/flowdoc must be "0.1" or "0.2", got nothing`},
		{`[]`, `/flowdoc must be "0.1" or "0.2", got nothing`},
		{`null`, `/flowdoc must be "0.1" or "0.2", got nothing`},
		{`{"flowdoc":["0.2"]}`, `/flowdoc must be "0.1" or "0.2", got ["0.2"]`},
	} {
		_, err := ValidateJSON([]byte(tc.in))
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: got %v, want %s", tc.in, err, tc.want)
		}
	}
	if _, err := ValidateVersion("0.3", demo); err == nil {
		t.Error("a version with no schema validated")
	}
	if _, err := ValidateJSON([]byte(`{`)); err == nil {
		t.Error("malformed JSON validated")
	}
}

// A map[string]any document (encoding/json's form) validates like its jsonv
// twin.
func TestPlainMapInput(t *testing.T) {
	d := toPlain(load(t, demoFixture))
	if vs, err := Validate(d); err != nil || len(vs) != 0 {
		t.Fatalf("%v %v", vs, err)
	}
	for _, k := range []string{"zf", "zb", "ze", "za", "zd", "zc"} {
		d.(map[string]any)[k] = 1
	}
	vs, err := Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, v := range vs {
		got = append(got, v.Property)
	}
	if strings.Join(got, ",") != "za,zb,zc,zd,ze,zf" {
		t.Fatalf("got %v", vs)
	}
	if _, err := ValidateVersion("0.2", map[string]any{"x": struct{}{}}); err == nil {
		t.Error("a non-JSON value validated")
	}
}

func TestViolationString(t *testing.T) {
	if got := (Violation{Message: "must be object"}).String(); got != "(root) must be object" {
		t.Error(got)
	}
	if got := (Violation{InstancePath: "/name", Message: "x"}).String(); got != "/name x" {
		t.Error(got)
	}
}

// --- the Ajv oracle

// testdata/ajv-oracle.json is what Ajv2020({ allErrors: true, strict: false })
// (packages/core's ajv) returns for every docCase, recorded by
// testdata/ajv-oracle.mjs:
//
//	FLOWASCODE_AJV_INPUT=/tmp/in.json go test ./internal/schema -run TestWriteAjvInput
//	node internal/schema/testdata/ajv-oracle.mjs /tmp/in.json <flow-as-code checkout> > internal/schema/testdata/ajv-oracle.json
//
// Each case carries the sha256 of the document it was run on, so an edit to
// a case or a fixture that is not re-recorded fails TestAjvOracle.

type oracleError struct {
	InstancePath string `json:"instancePath"`
	SchemaPath   string `json:"schemaPath"`
	Keyword      string `json:"keyword"`
	Message      string `json:"message"`
	Property     string `json:"property"`
}

func docBytes(t testing.TB, c docCase) []byte { return jsonv.Encode(c.doc(t), "") }

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestWriteAjvInput(t *testing.T) {
	out := os.Getenv("FLOWASCODE_AJV_INPUT")
	if out == "" {
		t.Skip("FLOWASCODE_AJV_INPUT not set")
	}
	var cases []any
	for _, c := range docCases() {
		b := docBytes(t, c)
		cases = append(cases, jsonv.Object{
			{Key: "id", Value: c.id()},
			{Key: "version", Value: c.version},
			{Key: "sha256", Value: sha(b)},
			{Key: "doc", Value: string(b)},
		})
	}
	input := jsonv.Object{{Key: "cases", Value: cases}, {Key: "synthetic", Value: synthInput()}}
	if err := os.WriteFile(out, jsonv.Encode(input, ""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func key(e oracleError) string {
	return e.InstancePath + "\x00" + e.SchemaPath + "\x00" + e.Keyword + "\x00" + e.Message + "\x00" + e.Property
}

// earlyStop reports whether an error Ajv gives and this package does not is
// the documented difference: santhosh-tekuri stopped at a failing type, const
// or enum in the same subschema at the same instance location.
func earlyStop(missing oracleError, got []Violation) bool {
	slash := strings.LastIndex(missing.SchemaPath, "/")
	base := missing.SchemaPath[:slash]
	for _, v := range got {
		if v.InstancePath != missing.InstancePath {
			continue
		}
		switch v.Keyword {
		case "type", "const", "enum":
			vs := strings.LastIndex(v.SchemaPath, "/")
			if v.SchemaPath[:vs] == base {
				return true
			}
		}
	}
	return false
}

// oracleRecord is one recorded Ajv run.
type oracleRecord struct {
	sha    string
	valid  bool
	errors []oracleError
}

// oracleRecords reads one list of testdata/ajv-oracle.json by case id.
func oracleRecords(t *testing.T, list string) map[string]oracleRecord {
	t.Helper()
	b, err := os.ReadFile("testdata/ajv-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonv.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	str := func(o jsonv.Object, k string) string { v, _ := o.Get(k); s, _ := v.(string); return s }
	recs := map[string]oracleRecord{}
	listV, _ := raw.(jsonv.Object).Get(list)
	for _, cv := range listV.([]any) {
		o := cv.(jsonv.Object)
		validV, _ := o.Get("valid")
		r := oracleRecord{sha: str(o, "sha256"), valid: validV == true}
		errsV, _ := o.Get("errors")
		for _, ev := range errsV.([]any) {
			eo := ev.(jsonv.Object)
			r.errors = append(r.errors, oracleError{str(eo, "instancePath"), str(eo, "schemaPath"), str(eo, "keyword"), str(eo, "message"), str(eo, "property")})
		}
		if _, dup := recs[str(o, "id")]; dup {
			t.Fatalf("oracle repeats %q", str(o, "id"))
		}
		recs[str(o, "id")] = r
	}
	return recs
}

// compareWithAjv holds violations to Ajv's errors for the same input: the
// same verdict; every violation one of Ajv's errors (place, schema location,
// keyword, message, property), as many times as Ajv reports it; and every
// Ajv error not reported an early stop. It returns the early stops.
func compareWithAjv(t *testing.T, r oracleRecord, vs []Violation) int {
	t.Helper()
	if (len(vs) == 0) != r.valid {
		t.Fatalf("Ajv valid=%v, got %v", r.valid, vs)
	}
	ajv := map[string]int{}
	for _, e := range r.errors {
		ajv[key(e)]++
	}
	mine := map[string]int{}
	for _, v := range vs {
		k := key(oracleError{v.InstancePath, v.SchemaPath, v.Keyword, v.Message, v.Property})
		mine[k]++
		if mine[k] > ajv[k] {
			t.Errorf("not an Ajv error: %+v", v)
		}
	}
	stops := 0
	var missing []string
	for _, e := range r.errors {
		k := key(e)
		if mine[k] > 0 {
			mine[k]--
			continue
		}
		if earlyStop(e, vs) {
			stops++
			continue
		}
		missing = append(missing, strings.ReplaceAll(k, "\x00", " | "))
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("Ajv reports and this does not: %s", m)
	}
	return stops
}

func TestAjvOracle(t *testing.T) {
	recs := oracleRecords(t, "cases")
	cases := docCases()
	if len(recs) != len(cases) {
		t.Errorf("oracle holds %d cases, the table %d; re-record it", len(recs), len(cases))
	}
	for _, c := range cases {
		t.Run(c.id(), func(t *testing.T) {
			r, ok := recs[c.id()]
			if !ok {
				t.Fatal("not in the oracle; re-record it")
			}
			if got := sha(docBytes(t, c)); got != r.sha {
				t.Fatal("the oracle was recorded on a different document; re-record it")
			}
			if r.valid != c.valid {
				t.Fatalf("the table says valid=%v, Ajv said %v", c.valid, r.valid)
			}
			vs, err := ValidateVersion(c.version, c.doc(t))
			if err != nil {
				t.Fatal(err)
			}
			// The FlowDoc schemas pair type, const or enum with no keyword a
			// value of the wrong type can fail, so on them the lists are equal.
			if stops := compareWithAjv(t, r, vs); stops != 0 {
				t.Errorf("%d early stops", stops)
			}
		})
	}
}

// Violations come in document order: array elements by index, not as
// strings, object members in the document's order, then schema path.
func TestViolationOrder(t *testing.T) {
	d := load(t, demoFixture)
	actions := ref(&d, "content.Actions")
	n := len((*actions).([]any))
	for len((*actions).([]any)) < 11 {
		push(&d, "content.Actions", j(`{"Identifier":"pad-`+strings.Repeat("x", len((*actions).([]any)))+`","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}`))
	}
	set(ref(&d, "content.Actions.10"), "Identifier", "bad/ten")
	set(ref(&d, "content.Actions.2"), "Identifier", "bad/two")
	set(&d, "zeta", 1.0)
	set(&d, "alpha", 1.0)
	set(&d, "name", "Bad")
	vs, err := ValidateVersion("0.2", d)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.InstancePath+" "+v.Keyword+" "+v.Property)
	}
	want := []string{
		" additionalProperties zeta",
		" additionalProperties alpha",
		"/name pattern ",
		"/content/Actions/2/Identifier pattern ",
		"/content/Actions/10/Identifier pattern ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("demo has %d actions; got\n%s\nwant\n%s", n, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// earlyStop excuses an Ajv error only when a type, const or enum failed in
// the same subschema at the same place.
func TestEarlyStopClassifier(t *testing.T) {
	got := []Violation{{InstancePath: "/x", SchemaPath: "#/properties/x/type", Keyword: "type"}}
	for _, tc := range []struct {
		missing oracleError
		want    bool
	}{
		{oracleError{InstancePath: "/x", SchemaPath: "#/properties/x/enum"}, true},
		{oracleError{InstancePath: "/y", SchemaPath: "#/properties/x/enum"}, false},
		{oracleError{InstancePath: "/x", SchemaPath: "#/properties/x/not/enum"}, false},
		{oracleError{InstancePath: "/x", SchemaPath: "#/properties/y/enum"}, false},
	} {
		if earlyStop(tc.missing, got) != tc.want {
			t.Errorf("%+v: want %v", tc.missing, tc.want)
		}
	}
	if earlyStop(oracleError{InstancePath: "/x", SchemaPath: "#/properties/x/enum"}, []Violation{{InstancePath: "/x", SchemaPath: "#/properties/x/pattern", Keyword: "pattern"}}) {
		t.Error("a pattern failure excused an Ajv error")
	}
}
