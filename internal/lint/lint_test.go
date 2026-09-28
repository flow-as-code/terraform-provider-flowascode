// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	v, err := jsonv.Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The class used to translate JavaScript's \s and the set jsTrim strips are
// one set, and it is neither Go's \s nor unicode.IsSpace.
func TestJSWhitespace(t *testing.T) {
	class := regexp.MustCompile(`^[` + jsSpaceClass + `]$`)
	for r := rune(0); r <= 0x10ffff; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if class.MatchString(string(r)) != isJSSpace(r) {
			t.Fatalf("U+%04X: class and isJSSpace disagree", r)
		}
	}
	for _, c := range []struct {
		in, want string
	}{
		{"\ufeff x \u3000", "x"},
		{"\u0085", "\u0085"},
		{"\u2028\u00a0\v", ""},
	} {
		if got := jsTrim(c.in); got != c.want {
			t.Errorf("jsTrim(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestJSString(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{undefined, "undefined"},
		{nil, "null"},
		{true, "true"},
		{7.0, "7"},
		{1e21, "1e+21"},
		{0.1, "0.1"},
		{"s", "s"},
		{[]any{1.0, nil, "x", []any{2.0, 3.0}, undefined}, "1,,x,2,3,"},
		{jsonv.Object{}, "[object Object]"},
	} {
		if got := jsString(c.in); got != c.want {
			t.Errorf("jsString(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// graph.ts's path syntax: [i] for elements (from the root too), dotted keys,
// and Object.entries order, which puts array-index keys first.
func TestWalkStrings(t *testing.T) {
	v := decode(t, `{"b":{"z":"1","0":"2"},"10":"3","2":"4","L":["5",{"k":"6"},7,null,["8"]],"01":"9"}`)
	var got []string
	for _, s := range WalkStrings(v, "") {
		got = append(got, s.Path+"="+s.Value)
	}
	want := []string{"2=4", "10=3", "b.0=2", "b.z=1", "L[0]=5", "L[1].k=6", "L[4][0]=8", "01=9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if got := WalkStrings(decode(t, `["a"]`), ""); len(got) != 1 || got[0].Path != "[0]" {
		t.Fatalf("root array: %+v", got)
	}
	if got := WalkStrings(decode(t, `{"a":"x"}`), "refs"); got[0].Path != "refs.a" {
		t.Fatalf("prefixed: %+v", got)
	}
	arns := LiteralArnPaths(decode(t, `{"a":"arn:aws-cn:x","b":["no","arn:aws:y"],"c":"arn:awsX:"}`))
	if !reflect.DeepEqual(arns, []string{"a", "b[1]"}) {
		t.Fatalf("LiteralArnPaths = %v", arns)
	}
	if got := LiteralArnPaths("clean"); got == nil || len(got) != 0 {
		t.Fatalf("LiteralArnPaths(clean) = %#v, want an empty list", got)
	}
}

func TestLocaleCompare(t *testing.T) {
	ordered := []string{
		"", "\t", " ", "_", "-", ",", "!", "\"", "(", "@", "*", "/", "\\", "&", "#", "%", "`", "^", "+", "<", "=", ">", "|", "~",
		"\U0001F600", "$", "\u20ac", "0", "9", "a", "A", "ab", "aB", "Ab", "AB", "b", "z", "Z", "\u4e2d",
	}
	for i := range ordered {
		for j := range ordered {
			want := sign(i - j)
			if got := localeCompare(ordered[i], ordered[j]); got != want {
				t.Errorf("localeCompare(%q, %q) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
	for _, pair := range [][2]string{{"a\x01b", "ab"}, {"a\u200bb", "ab"}, {"\x7f", ""}} {
		if localeCompare(pair[0], pair[1]) != 0 {
			t.Errorf("%q and %q should collate equal", pair[0], pair[1])
		}
	}
}

// engine.ts's order: an absent blockId reads as "", and each key only
// decides when the ones before it tie.
func TestCompareFindings(t *testing.T) {
	empty := ""
	a := Finding{Doc: "d", Rule: "r", Message: "m"}
	b := Finding{Doc: "d", Rule: "r", Message: "m", BlockID: &empty}
	if compareFindings(a, b) != 0 {
		t.Fatal("an absent blockId must equal an empty one")
	}
	x, y := "x", "Y"
	for _, c := range []struct {
		a, b Finding
		want int
	}{
		{Finding{Doc: "b", Rule: "a"}, Finding{Doc: "B", Rule: "a"}, -1},
		{Finding{Doc: "a", Rule: "z"}, Finding{Doc: "B", Rule: "a"}, -1},
		{Finding{Doc: "a", Rule: "r", BlockID: &y}, Finding{Doc: "a", Rule: "r", BlockID: &x}, 1},
		{Finding{Doc: "a", Rule: "r", Message: "a-b"}, Finding{Doc: "a", Rule: "r", Message: "a_b"}, 1},
	} {
		if got := compareFindings(c.a, c.b); got != c.want {
			t.Errorf("compareFindings(%+v, %+v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

const minimal = `{"flowdoc":"0.2","kind":"flow","name":"%s","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"bye","Actions":[{"Identifier":"bye","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"refs":[]}`

func TestEngineOptions(t *testing.T) {
	doc := decode(t, strings.Replace(minimal, "%s", "one", 1))
	var seenAll int
	probe := Rule{ID: "probe", Hard: true, Check: func(ctx RuleContext) {
		seenAll = len(ctx.All)
		ctx.Report(Report{Severity: SeverityWarning, Message: "second"})
		ctx.Report(Report{Severity: SeverityError, Message: "first"})
		// Equal under localeCompare: the stable sort keeps report order.
		ctx.Report(Report{Severity: SeverityError, Message: "tie\x01"})
		ctx.Report(Report{Severity: SeverityWarning, Message: "tie"})
	}}
	if _, err := Lint([]any{doc, doc}, Options{Rules: []Rule{probe}}); err != nil || seenAll != 2 {
		t.Fatalf("ctx.All held %d docs (%v)", seenAll, err)
	}
	found, err := Lint([]any{doc}, Options{Rules: []Rule{probe}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		got = append(got, f.Message+"/"+string(f.Severity))
	}
	if want := []string{"first/error", "second/warning", "tie\x01/error", "tie/warning"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %q, want %q", got, want)
	}
	if !HasBlockingFindings(found, []Rule{probe}) {
		t.Fatal("a finding from a hard rule blocks")
	}
	if HasBlockingFindings(found, nil) || HasBlockingFindings(found, []Rule{}) {
		t.Fatal("probe is not a built-in rule, and an empty rule list has no hard rule")
	}
	if _, err := Lint([]any{doc}, Options{Rules: []Rule{probe}, Disable: []string{"probe"}}); err == nil ||
		err.Error() != "lint cannot disable a hard rule: probe. Hard rules block a save and are never skipped." {
		t.Fatalf("disabling a hard custom rule: %v", err)
	}
	if found, err := Lint(nil, Options{}); err != nil || found == nil || len(found) != 0 {
		t.Fatalf("an empty set lints to an empty, non-nil list: %#v, %v", found, err)
	}
	if found, err := Lint([]any{doc}, Options{Rules: []Rule{}}); err != nil || len(found) != 0 {
		t.Fatalf("no rules, no findings: %v, %v", found, err)
	}
	// nil Rules is every built-in rule: a duplicate name is reported.
	found, err = Lint([]any{doc, doc}, Options{})
	if err != nil || len(found) != 2 || found[0].Rule != "unique-names" {
		t.Fatalf("default rules: %+v, %v", found, err)
	}
	found, err = Lint([]any{doc, doc}, Options{Disable: []string{"unique-names"}})
	if err != nil || len(found) != 0 {
		t.Fatalf("disabled unique-names: %+v, %v", found, err)
	}
}

func TestReporters(t *testing.T) {
	id := "blk"
	findings := []Finding{
		{Rule: "r1", Severity: SeverityError, Message: "m1", Doc: "d1", BlockID: &id},
		{Rule: "r2", Severity: SeverityWarning, Message: "m2", Doc: "d1"},
		{Rule: "r3", Severity: SeverityWarning, Message: "m3", Doc: "d2"},
	}
	wantText := "d1\n  error (blk): m1 [r1]\n  warning: m2 [r2]\n\nd2\n  warning: m3 [r3]\n\n3 finding(s): 1 error(s), 2 warning(s).\n"
	if got := ToText(findings); got != wantText {
		t.Fatalf("ToText:\n%s", got)
	}
	if got := ToText(nil); got != "No findings.\n" {
		t.Fatalf("ToText(nil) = %q", got)
	}
	wantJSON := "{\n  \"summary\": {\n    \"total\": 0,\n    \"errors\": 0,\n    \"warnings\": 0\n  },\n  \"findings\": []\n}\n"
	if got := ToJSON(nil); got != wantJSON {
		t.Fatalf("ToJSON(nil) = %q", got)
	}
}

// Findings the comparator calls equal keep report order, as
// Array.prototype.sort (stable since ES2019) keeps them. Sixty of them, so
// an unstable sort would reorder some: sort.Slice is insertion sort only up
// to twelve elements. Each message is unique in its bytes (a run of U+0001,
// which collation ignores) and equal under localeCompare to half the rest.
func TestStableSort(t *testing.T) {
	doc := decode(t, strings.Replace(minimal, "%s", "one", 1))
	var reported []string
	for i := 0; i < 60; i++ {
		base := "b"
		if i%3 == 1 {
			base = "a"
		}
		reported = append(reported, base+strings.Repeat("\x01", i))
	}
	var want []string
	for _, base := range []string{"a", "b"} {
		for _, m := range reported {
			if m[:1] == base {
				want = append(want, m)
			}
		}
	}
	probe := Rule{ID: "probe", Check: func(ctx RuleContext) {
		for _, m := range reported {
			ctx.Report(Report{Severity: SeverityWarning, Message: m})
		}
	}}
	found, err := Lint([]any{doc}, Options{Rules: []Rule{probe}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		got = append(got, f.Message)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ties reordered:\n got %q\nwant %q", got, want)
	}
}
