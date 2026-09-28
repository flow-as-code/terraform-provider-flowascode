// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"reflect"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

func mustParse(t *testing.T, s string) RefEntry {
	t.Helper()
	e, ok := ParseToken(s)
	if !ok {
		t.Fatalf("%s did not parse", s)
	}
	return e
}

func TestParseToken(t *testing.T) {
	if got := mustParse(t, "${cdref:module:survey@prod}"); got != (RefEntry{"${cdref:module:survey@prod}", "module", "survey", "prod"}) {
		t.Errorf("got %+v", got)
	}
	if got := mustParse(t, "${cdref:queue:front-desk}"); got.Alias != "" || got.Name != "front-desk" {
		t.Errorf("got %+v", got)
	}
	for _, bad := range []string{
		"${cdref:queue:front-desk}\n", // JavaScript's $ does not match before a final newline
		"x${cdref:queue:a}",
		"${cdref:bogus:a}",
		"${cdref:queue:a--b}",
		"${cdref:queue:a@}",
		"${cdref:queue:A}",
		"",
	} {
		if _, ok := ParseToken(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if IsToken(3.0) || IsToken(nil) || !IsToken("${cdref:view:v@1}") {
		t.Error("IsToken")
	}
}

func TestRefKeyForms(t *testing.T) {
	plain := mustParse(t, "${cdref:queue:front-desk}")
	if got := RefMapKeys(plain); got != [3]string{"${cdref:queue:front-desk}", "queue:front-desk", "queue_front_desk_arn"} {
		t.Errorf("got %v", got)
	}
	module := mustParse(t, "${cdref:module:survey@prod}")
	if got := RefMapKeys(module); got != [3]string{"${cdref:module:survey@prod}", "module:survey@prod", "module_survey_prod_arn"} {
		t.Errorf("got %v", got)
	}
	if got := RefVariableName(mustParse(t, "${cdref:flow:2fa-line}")); got != "flow__2fa_line_arn" {
		t.Errorf("got %s", got)
	}
	if got := RefVariableName(mustParse(t, "${cdref:view:acw@1}")); got != "view_acw__1_arn" {
		t.Errorf("got %s", got)
	}
	if got := DescribeMissingRefKey(module); got != `${cdref:module:survey@prod} (key it as "${cdref:module:survey@prod}", "module:survey@prod", or "module_survey_prod_arn")` {
		t.Errorf("got %s", got)
	}
}

func TestLookupRefValue(t *testing.T) {
	ref := mustParse(t, "${cdref:queue:front-desk}")
	for _, k := range RefMapKeys(ref) {
		if v, ok := LookupRefValue(map[string]string{k: "arn:example"}, ref); !ok || v != "arn:example" {
			t.Errorf("%s: %q %v", k, v, ok)
		}
	}
	m := map[string]string{RefKey(ref): "body", RefVariableName(ref): "variable", ref.Token: "token"}
	if v, _ := LookupRefValue(m, ref); v != "token" {
		t.Errorf("preferred %q", v)
	}
	delete(m, ref.Token)
	if v, _ := LookupRefValue(m, ref); v != "body" {
		t.Errorf("preferred %q", v)
	}
	if v, ok := LookupRefValue(map[string]string{"queue:front-desk": ""}, ref); !ok || v != "" {
		t.Error("an empty value is not mapped")
	}
	if _, ok := LookupRefValue(map[string]string{}, ref); ok {
		t.Error("found in an empty map")
	}
}

func TestCollectRefsSortsByCodeUnit(t *testing.T) {
	v, _ := jsonv.Decode([]byte(`{"b":"${cdref:queue:b}","a":["${cdref:queue:a-2}","${cdref:queue:a}","${cdref:hours:z}"]}`))
	var got []string
	for _, e := range CollectRefs(v) {
		got = append(got, e.Token)
	}
	want := []string{"${cdref:hours:z}", "${cdref:queue:a-2}", "${cdref:queue:a}", "${cdref:queue:b}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := CollectRefs(nil); got == nil || len(got) != 0 {
		t.Errorf("nil: %v", got)
	}
}

func TestRefPathsOf(t *testing.T) {
	got := RefPathsOf("ConnectParticipantWithLexBot")
	want := []RefPath{{"PromptId", "prompt"}, {"LexV2Bot.AliasArn", "lex"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := RefPathsOf("Unknown"); got == nil || len(got) != 0 {
		t.Errorf("unknown: %v", got)
	}
	params, _ := jsonv.Decode([]byte(`{"EventHooks":{"CustomerQueue":"${cdref:flow:q}","AgentWhisper":"${cdref:flow:w}"}}`))
	hits, err := ReadRefPath(params.(jsonv.Object), RefPathsOf("UpdateContactEventHooks")[0].Path)
	if err != nil || len(hits) != 2 || hits[0].Path != "EventHooks.CustomerQueue" || hits[1].Value != "${cdref:flow:w}" {
		t.Errorf("hits %v, %v", hits, err)
	}
}

func TestIsCatalogPath(t *testing.T) {
	for _, p := range []string{"PromptId", "LexV2Bot.AliasArn", "Messages[].PromptId", "EventHooks.*", "A.B[].C.*"} {
		if !IsCatalogPath(p) {
			t.Errorf("rejects %s", p)
		}
	}
	for _, p := range []string{"", ".", "A.", "A[0]", "A[", "A b", "*.[]", "A\n", "*[]"} {
		if IsCatalogPath(p) {
			t.Errorf("accepts %q", p)
		}
	}
}

func TestReadPathOnNonObjects(t *testing.T) {
	for _, v := range []any{"not an object", nil, []any{jsonv.Object{{Key: "A", Value: 1.0}}}, 1.0} {
		hits, err := ReadPath(v, "A")
		if err != nil || len(hits) != 0 {
			t.Errorf("%v: %v %v", v, hits, err)
		}
		if hits, _ := ReadPath(v, "*"); len(hits) != 0 {
			t.Errorf("%v: * found %v", v, hits)
		}
	}
}

func TestJSKeyOrder(t *testing.T) {
	got := jsKeyOrder([]string{"b", "10", "2", "02", "0", "4294967295", "4294967294", "-1", "1.5"})
	want := []string{"0", "2", "10", "4294967294", "b", "02", "4294967295", "-1", "1.5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestIsValidIdentifier(t *testing.T) {
	for _, c := range ForbiddenIdentifierChars {
		if IsValidIdentifier("a" + c + "b") {
			t.Errorf("accepts %q", c)
		}
	}
	for _, id := range ForbiddenIdentifiers {
		if IsValidIdentifier(id) {
			t.Errorf("accepts %s", id)
		}
	}
	if !SlugPattern.MatchString("a-1") || SlugPattern.MatchString("a--1") || SlugPattern.MatchString("-a") {
		t.Error("SlugPattern")
	}
}
