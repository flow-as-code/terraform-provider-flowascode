// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package materialize

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The assertions of materialize.test.ts, restated over the vendored demo,
// plus the Go-only edges (a nil map or binder, the error constructors).

func demo(t *testing.T) jsonv.Object {
	t.Helper()
	return decode(t, readConformance(t, "materialize/demo-with-map/doc.flowdoc.json")).(jsonv.Object)
}

func demoMap(t *testing.T) map[string]string {
	t.Helper()
	return stringMap(t, decode(t, readConformance(t, "materialize/demo-with-map/map.json")))
}

func actionByID(t *testing.T, content jsonv.Object, id string) jsonv.Object {
	t.Helper()
	actions, _ := content.Get("Actions")
	for _, a := range actions.([]any) {
		if v, _ := a.(jsonv.Object).Get("Identifier"); v == id {
			return a.(jsonv.Object)
		}
	}
	t.Fatalf("no action %q", id)
	return nil
}

func param(t *testing.T, action jsonv.Object, key string) any {
	t.Helper()
	p, _ := action.Get("Parameters")
	v, _ := p.(jsonv.Object).Get(key)
	return v
}

func TestBinderPassesOutputThroughUnvalidated(t *testing.T) {
	sentinel := `{"Fn::GetAtt": ["Queue", "Arn"]} arn:aws:not-even-close ${}`
	content, err := MaterializeWithBinder(demo(t), func(flowdoc.RefEntry) string { return sentinel })
	if err != nil {
		t.Fatal(err)
	}
	if got := param(t, actionByID(t, content, "set-working-queue"), "QueueId"); got != sentinel {
		t.Errorf("QueueId = %v", got)
	}
}

func TestBinderRefusesNil(t *testing.T) {
	_, err := MaterializeWithBinder(demo(t), nil)
	var invalid *flowdoc.InvalidFlowDocError
	if !errors.As(err, &invalid) || err.Error() != "materializeWithBinder expects a binder function as its second argument." {
		t.Errorf("err = %v", err)
	}
}

func TestMapRefusesNil(t *testing.T) {
	_, err := MaterializeWithMap(demo(t), nil)
	var invalid *flowdoc.InvalidFlowDocError
	if !errors.As(err, &invalid) || err.Error() != "materializeWithMap expects a token to value map object as its second argument." {
		t.Errorf("err = %v", err)
	}
	// An empty map is a map: the document's references are what is missing.
	_, err = MaterializeWithMap(demo(t), map[string]string{})
	var matErr *MaterializeError
	if !errors.As(err, &matErr) || len(matErr.MissingTokens) != 3 {
		t.Errorf("err = %v", err)
	}
}

func TestMapListsEveryMissingTokenSorted(t *testing.T) {
	m := demoMap(t)
	delete(m, "${cdref:queue:appointments}")
	delete(m, "${cdref:hours:main-line}")
	_, err := MaterializeWithMap(demo(t), m)
	var matErr *MaterializeError
	if !errors.As(err, &matErr) {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Join(matErr.MissingTokens, ","); got != "${cdref:hours:main-line},${cdref:queue:appointments}" {
		t.Errorf("MissingTokens = %s", got)
	}
	want := `Cannot materialize: 2 unmapped token(s): ${cdref:hours:main-line}, ${cdref:queue:appointments}` +
		` Key each one by its token ("${cdref:hours:main-line}"), by type and name ("hours:main-line"),` +
		` or by the variable the terraform emitter writes ("hours_main_line_arn").`
	if err.Error() != want {
		t.Errorf("message\n got: %s\nwant: %s", err.Error(), want)
	}
}

func TestMapCountsAnEmptyValueAsMapped(t *testing.T) {
	m := demoMap(t)
	m["${cdref:queue:appointments}"] = ""
	content, err := MaterializeWithMap(demo(t), m)
	if err != nil {
		t.Fatal(err)
	}
	if got := param(t, actionByID(t, content, "set-working-queue"), "QueueId"); got != "" {
		t.Errorf("QueueId = %v", got)
	}
}

func TestMapSubstitutesVerbatimAndDropsToolMetadata(t *testing.T) {
	doc := demo(t)
	pristine := enc(doc)
	content, err := MaterializeWithMap(doc, demoMap(t))
	if err != nil {
		t.Fatal(err)
	}
	if enc(doc) != pristine {
		t.Error("the input document was mutated")
	}
	if got := param(t, actionByID(t, content, "set-working-queue"), "QueueId"); got != "arn:aws:connect:us-east-1:111122223333:instance/EXAMPLE/queue/EXAMPLE" {
		t.Errorf("QueueId = %v", got)
	}
	out, err := SerializeContent(content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(content.Keys(), ",") != "Version,StartAction,Metadata,Actions" {
		t.Errorf("keys = %v", content.Keys())
	}
	for _, s := range []string{"${cdref:", `"layout"`, `"refs"`, `"meta"`} {
		if strings.Contains(string(out), s) {
			t.Errorf("output contains %s", s)
		}
	}
	again, _ := MaterializeWithMap(demo(t), demoMap(t))
	if out2, _ := SerializeContent(again); string(out2) != string(out) {
		t.Error("two runs differ")
	}
}

func TestProjectsLayoutAndFillsAutoLayout(t *testing.T) {
	doc := demo(t)
	layoutValue, _ := doc.Get("layout")
	layout := layoutValue.(jsonv.Object)
	layout.Delete("hang-up")
	doc.Set("layout", layout)
	content, err := MaterializeWithMap(doc, demoMap(t))
	if err != nil {
		t.Fatal(err)
	}
	md, _ := content.Get("Metadata")
	am, _ := md.(jsonv.Object).Get("ActionMetadata")
	if _, ok := am.(jsonv.Object).Get("hang-up"); !ok {
		t.Error("hang-up has no ActionMetadata")
	}
	welcome, _ := am.(jsonv.Object).Get("welcome")
	pos, _ := welcome.(jsonv.Object).Get("Position")
	want, _ := layout.Get("welcome")
	if enc(pos) != enc(want) {
		t.Errorf("welcome Position = %s, want %s", enc(pos), enc(want))
	}
	epp, _ := md.(jsonv.Object).Get("EntryPointPosition")
	if enc(epp) != `{"x":0,"y":20}` {
		t.Errorf("EntryPointPosition = %s", enc(epp))
	}
}

func TestInterpolatedTokenIsRefused(t *testing.T) {
	doc := demo(t)
	contentValue, _ := doc.Get("content")
	a := actionByID(t, contentValue.(jsonv.Object), "welcome")
	p, _ := a.Get("Parameters")
	params := p.(jsonv.Object)
	params.Set("Text", "Please hold, ${cdref:prompt:greeting} is next.")
	a.Set("Parameters", params)
	m := demoMap(t)
	m["${cdref:prompt:greeting}"] = "arn:aws:connect:us-east-1:111122223333:instance/E/x/E"
	_, err := MaterializeWithMap(doc, m)
	var matErr *MaterializeError
	if !errors.As(err, &matErr) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "entire field value") || strings.Join(matErr.MissingTokens, ",") != "${cdref:prompt:greeting}" {
		t.Errorf("err = %v, tokens %v", err, matErr.MissingTokens)
	}
	if len(matErr.MissingRefs) != 0 || matErr.MissingRefs == nil {
		t.Errorf("MissingRefs = %#v, want empty", matErr.MissingRefs)
	}
}

func TestErrorConstructors(t *testing.T) {
	reason := "Because."
	if got := NewMaterializeError([]string{"a", "b"}, &reason, nil).Error(); got != "Cannot materialize: Because. Offending token(s): a, b" {
		t.Errorf("with reason: %s", got)
	}
	if got := NewMaterializeError([]string{"a"}, nil, nil).Error(); got != "Cannot materialize: 1 unmapped token(s): a" {
		t.Errorf("without refs: %s", got)
	}
	e, _ := flowdoc.ParseToken("${cdref:module:survey@prod}")
	got := MissingKeysError([]flowdoc.RefEntry{e})
	want := `Cannot materialize: 1 unmapped token(s): ${cdref:module:survey@prod} Key each one by its token ("${cdref:module:survey@prod}"), by type and name ("module:survey@prod"), or by the variable the terraform emitter writes ("module_survey_prod_arn").`
	if got.Error() != want || got.MissingTokens[0] != e.Token {
		t.Errorf("missingKeys: %s", got.Error())
	}
}

func TestModuleSettingsDefault(t *testing.T) {
	doc := decode(t, []byte(`{"flowdoc":"0.2","kind":"module","name":"m","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]}}`))
	content, err := MaterializeWithMap(doc, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := content.Get("Settings"); !ok || enc(s) != "{}" {
		t.Errorf("Settings = %v, %v", s, ok)
	}
	obj := doc.(jsonv.Object)
	obj.Set("kind", "flow")
	content, _ = MaterializeWithMap(doc, map[string]string{})
	if _, ok := content.Get("Settings"); ok {
		t.Error("a flow without Settings got one")
	}
}

func TestStringToNumber(t *testing.T) {
	for in, want := range map[string]float64{
		"": 0, " 12 ": 12, "0x1F": 31, "0o17": 15, "0b101": 5, "1e3": 1000, ".5": 0.5, "5.": 5,
		"+7": 7, "-Infinity": math.Inf(-1), "  3　": 3, "1e400": math.Inf(1),
	} {
		if got := stringToNumber(in); got != want {
			t.Errorf("stringToNumber(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"abc", "0x", "-0x10", "1_000", "infinity", "Inf", "NaN", "1e", "0x1p3", "++1"} {
		if got := stringToNumber(in); !math.IsNaN(got) {
			t.Errorf("stringToNumber(%q) = %v, want NaN", in, got)
		}
	}
}

func TestJSKeyOrder(t *testing.T) {
	got := jsKeyOrder([]string{"b", "10", "a", "9", "4294967295", "4294967294", "01", "0"})
	if strings.Join(got, ",") != "0,9,10,4294967294,b,a,4294967295,01" {
		t.Errorf("jsKeyOrder = %v", got)
	}
}
