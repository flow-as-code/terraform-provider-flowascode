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
// each case's input, at the version its directory name ends with, migrates
// to the expected bytes through Serialize, and the output names Version. The
// schema half of that suite (the input valid at its own version and not at
// the current one, the output the reverse) is the schema package's to hold.
func TestMigrateConformance(t *testing.T) {
	cases := caseDirs(t, "migrate")
	if want := []string{"minimal-0.1", "minimal-0.2", "with-meta-0.1", "with-meta-0.2"}; !reflect.DeepEqual(cases, want) {
		t.Fatalf("migrate cases %v, want %v", cases, want)
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			from := c[strings.LastIndex(c, "-")+1:]
			input := decodeDoc(t, "migrate/"+c+"/input.flowdoc.json")
			if field(input, "flowdoc") != from {
				t.Fatalf("input is at %v, want %s", field(input, "flowdoc"), from)
			}
			migrated, err := MigrateFlowDoc(input)
			if err != nil {
				t.Fatal(err)
			}
			if field(input, "flowdoc") != from {
				t.Error("migration changed its input")
			}
			if field(migrated, "flowdoc") != Version {
				t.Errorf("migrated to %v, want %s", field(migrated, "flowdoc"), Version)
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
	doc.Set("flowdoc", "0.4")
	_, err := MigrateFlowDocIn(doc, "flow-cli lint")
	if want := `flow-cli lint: FlowDoc version "0.4" is not supported; this build reads "0.1", "0.2", "0.3".`; err == nil || err.Error() != want {
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
	counts := map[ActionCategory]int{"contact": 27, "participant": 6, "flowControl": 15, "interaction": 8, "other": 5}
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
	// The count comes from the vendored file read on its own, not from the
	// port that ModeledTypes reads through.
	modeled := 0
	for _, m := range field(decodeDoc(t, CatalogPath), "actions").(jsonv.Object) {
		if field(m.Value, "modeled") == true {
			modeled++
		}
	}
	if modeled == 0 || len(ModeledTypes()) != modeled {
		t.Errorf("%d modeled types, the vendored catalog has %d", len(ModeledTypes()), modeled)
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
	// The channel restrictions are the vendored file's `channels`, read on
	// their own (C03 and the Phase D groups add to them), and Wait and
	// ShowView are among them. The ChannelRestriction helper reads nil as
	// "none", as channelRestriction reads undefined.
	restricted := map[string][]Channel{}
	for _, a := range c.Actions {
		if a.Model != nil && a.Model.Channels != nil {
			restricted[a.Type] = a.Model.Channels
		}
	}
	want := map[string][]Channel{}
	for _, m := range field(decodeDoc(t, CatalogPath), "actions").(jsonv.Object) {
		if list, ok := field(m.Value, "channels").([]any); ok && field(m.Value, "modeled") == true {
			for _, c := range list {
				want[m.Key] = append(want[m.Key], Channel(c.(string)))
			}
		}
	}
	if !reflect.DeepEqual(restricted["Wait"], []Channel{"CHAT"}) || !reflect.DeepEqual(restricted["ShowView"], []Channel{"CHAT"}) {
		t.Errorf("Wait and ShowView are not CHAT only: %v", restricted)
	}
	if !reflect.DeepEqual(restricted, want) {
		t.Errorf("channel restrictions %v, want %v", restricted, want)
	}
	if got, ok := ChannelRestriction("Wait"); !ok || !reflect.DeepEqual(got, []Channel{"CHAT"}) {
		t.Errorf("ChannelRestriction(Wait) = %v, %v", got, ok)
	}
	if got, ok := ChannelRestriction("Compare"); ok || got != nil {
		t.Errorf("ChannelRestriction(Compare) = %v, %v", got, ok)
	}
	// catalog.test.ts's docProblem: every entry under other carries a source
	// (D00) and nothing under the four Developer Guide categories does,
	// modeled or not (D09 models two types from outside the Developer
	// Guide); a source says where doc points, and a console-export doc that
	// is not an API Reference page is a file kept in the vendored tree.
	for _, a := range c.Actions {
		if (a.Category == "other") != (a.Source != "") {
			t.Errorf("%s: category %s with source %q", a.Type, a.Category, a.Source)
		}
		page := func(prefix string) bool { return strings.HasPrefix(a.Doc, prefix) && strings.HasSuffix(a.Doc, ".html") }
		switch a.Source {
		case "":
			if !page("https://docs.aws.amazon.com/connect/latest/devguide/") {
				t.Errorf("%s: doc %q is not a developer guide page", a.Type, a.Doc)
			}
		case "adminguide":
			if !page("https://docs.aws.amazon.com/connect/latest/adminguide/") {
				t.Errorf("%s: doc %q is not an admin guide page", a.Type, a.Doc)
			}
		case "console-export":
			kept := false
			if rest, ok := strings.CutPrefix(a.Doc, "conformance/"); ok {
				_, err := fs.Stat(conformance.FS(), rest)
				kept = err == nil
			}
			if !page("https://docs.aws.amazon.com/connect/latest/APIReference/") && !kept {
				t.Errorf("%s: doc %q is neither an API Reference page nor a kept console export", a.Type, a.Doc)
			}
		default:
			t.Errorf("%s: source %q is unknown", a.Type, a.Source)
		}
	}
	if e := CatalogEntry("TransferParticipantToThirdParty"); e == nil || e.Source != "console-export" {
		t.Errorf("TransferParticipantToThirdParty decoded as %+v", e)
	}
	if e := CatalogEntry("RouteContactToAgent"); e == nil || e.Source != "adminguide" {
		t.Errorf("RouteContactToAgent decoded as %+v", e)
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
		"an unknown unmodeled key": {`"source": "adminguide"`, `"source": "adminguide", "note": "x"`},
		"a channels word": {`"channels": [
        "CHAT"
      ]`, `"channels": "CHAT"`},
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

// phaseDCatalog is the vendored catalog with one of each FlowDoc 0.3
// vocabulary field written in, the way a group task's catalog will carry
// them: channels on an unmodeled entry, requiredWhenValue on an error,
// keyPatterns on a map, groups on a constraint, dynamic on a list and a `*~`
// ref path. extra, when not nil, adds a key the port does not know to the
// structure it names.
func phaseDCatalog(t *testing.T, extra *string) []byte {
	t.Helper()
	root := decodeDoc(t, CatalogPath)
	actions := field(root, "actions").(jsonv.Object)
	entry := func(typ string) jsonv.Object { return field(actions, typ).(jsonv.Object) }
	// setParam writes a member into a parameter in place: Set appends a new
	// key to a copy of the slice, so the element is stored back.
	setParam := func(typ, key, member string, value any) {
		params := field(entry(typ), "parameters").([]any)
		for i, p := range params {
			if field(p, "key") == key {
				o := p.(jsonv.Object)
				o.Set(member, value)
				params[i] = o
				return
			}
		}
		t.Fatalf("%s has no %s", typ, key)
	}
	surprise := func(name string, o jsonv.Object) jsonv.Object {
		if extra != nil && *extra == name {
			o = append(o, jsonv.Member{Key: "surprise", Value: 1.0})
		}
		return o
	}

	unmodeled := entry("AuthenticateParticipant")
	unmodeled.Set("channels", []any{"VOICE", "CHAT"})
	actions.Set("AuthenticateParticipant", surprise("unmodeled", unmodeled))

	hooks := entry("UpdateContactEventHooks")
	transitions := field(hooks, "transitions").(jsonv.Object)
	errs := field(transitions, "errors").([]any)
	first := errs[0].(jsonv.Object)
	first.Set("requiredWhenValue", surprise("requiredWhenValue", jsonv.Object{{Key: "key", Value: "LinkContactToCase"}, {Key: "equals", Value: "true"}}))
	errs[0] = first

	setParam("UpdateContactAttributes", "Attributes", "keyPatterns", []any{`^Attributes\.[^.]+$`})
	setParam("MessageParticipantIteratively", "Messages", "dynamic", true)

	mp := entry("MessageParticipant")
	mp.Set("constraints", []any{surprise("groups", jsonv.Object{
		{Key: "rule", Value: "exactlyOne"},
		{Key: "groups", Value: []any{[]any{"PromptId"}, []any{"Text", "SSML"}}},
	})})
	return jsonv.Encode(root, "")
}

// The FlowDoc 0.3 catalog vocabulary is read, and decoding stays strict
// inside each new structure.
func TestCatalogReadsThePhaseDVocabulary(t *testing.T) {
	c, err := ParseCatalog(phaseDCatalog(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if e := c.Entry("AuthenticateParticipant"); e.Modeled || !reflect.DeepEqual(e.Channels, []Channel{"VOICE", "CHAT"}) {
		t.Errorf("AuthenticateParticipant channels %v", e.Channels)
	}
	if e := c.Entry("Wait"); !reflect.DeepEqual(e.Channels, []Channel{"CHAT"}) {
		t.Errorf("a modeled entry's channels on CatalogAction: %v", e.Channels)
	}
	hooks := c.Entry("UpdateContactEventHooks").Model
	if w := hooks.Transitions.Errors[0].RequiredWhenValue; w == nil || w.Key != "LinkContactToCase" || w.Equals != "true" {
		t.Errorf("requiredWhenValue %+v", w)
	}
	for _, p := range c.Entry("UpdateContactAttributes").Model.Parameters {
		if p.Key == "Attributes" && !reflect.DeepEqual(p.KeyPatterns, []string{`^Attributes\.[^.]+$`}) {
			t.Errorf("keyPatterns %v", p.KeyPatterns)
		}
	}
	for _, p := range c.Entry("MessageParticipantIteratively").Model.Parameters {
		if p.Key == "Messages" && (!p.Dynamic || p.Kind != "list") {
			t.Errorf("dynamic list %+v", p.CatalogElement)
		}
	}
	got := c.Entry("MessageParticipant").Model.Constraints
	if len(got) != 1 || got[0].Keys != nil || !reflect.DeepEqual(got[0].Groups, [][]string{{"PromptId"}, {"Text", "SSML"}}) {
		t.Errorf("groups %+v", got)
	}
	for _, name := range []string{"unmodeled", "requiredWhenValue", "groups"} {
		if _, err := ParseCatalog(phaseDCatalog(t, &name)); err == nil {
			t.Errorf("an unknown key in %s was accepted", name)
		}
	}
}

// requiredErrorsOf: a requiredWhenValue error is required when its key holds
// exactly the string it names, and not for another value, a JSONPath, a
// non-string or an absent key; requiredWhenKey keeps reading presence.
func TestRequiredErrorsOfReadsAValue(t *testing.T) {
	errs := []CatalogError{
		{Type: "Always", Required: true},
		{Type: "ByKey", RequiredWhenKey: "K"},
		{Type: "ByValue", RequiredWhenValue: &RequiredWhenValue{Key: "LinkContactToCase", Equals: "true"}},
		{Type: "Never"},
	}
	for _, c := range []struct {
		params string
		want   []string
	}{
		{`{}`, []string{"Always"}},
		{`{"LinkContactToCase":"true"}`, []string{"Always", "ByValue"}},
		{`{"LinkContactToCase":"false"}`, []string{"Always"}},
		{`{"LinkContactToCase":"$.Attributes.link"}`, []string{"Always"}},
		{`{"LinkContactToCase":true}`, []string{"Always"}},
		{`{"K":null,"LinkContactToCase":"true"}`, []string{"Always", "ByKey", "ByValue"}},
	} {
		v, err := jsonv.Decode([]byte(c.params))
		if err != nil {
			t.Fatal(err)
		}
		if got := RequiredErrorsOf(errs, v.(jsonv.Object)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.params, got, c.want)
		}
	}
}
