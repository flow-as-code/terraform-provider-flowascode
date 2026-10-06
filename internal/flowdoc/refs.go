// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Reference tokens, ported from packages/core/src/refs.ts. A reference
// occupies an entire field value and is never interpolated into a longer
// string: Connect requires the fields that hold references to be "either
// fully static or a single valid JSONPath identifier". See
// conformance/flow-language/actions.md.

// RefEntry is flowdoc.ts's RefEntry. Alias is "" when the token has none: a
// parsed alias is a slug and never empty, so "" stands for TypeScript's
// undefined.
type RefEntry struct {
	Token string
	Type  string
	Name  string
	Alias string
}

// TokenPattern is refs.ts's TOKEN_PATTERN.
var TokenPattern = regexp.MustCompile(`^\$\{cdref:(queue|hours|lambda|lex|prompt|flow|module|view):([a-z0-9]+(?:-[a-z0-9]+)*)(?:@([a-z0-9]+(?:-[a-z0-9]+)*))?\}$`)

// TokenOpen is refs.ts's TOKEN_OPEN: where a token can begin; ParseToken
// decides whether what follows is one.
const TokenOpen = "${cdref:"

// indexFrom is JavaScript's text.indexOf(sub, from).
func indexFrom(text, sub string, from int) int {
	if from > len(text) {
		return -1
	}
	i := strings.Index(text[from:], sub)
	if i == -1 {
		return -1
	}
	return from + i
}

// tokenCandidates is refs.ts's tokenCandidates: every candidate token in a
// string, for building the refs index, each `${cdref:` up to the next `}`.
// A hand-written scan rather than a regex, as the TypeScript's is since
// flow-as-code 78b0a87: one pass, each opening visited once, and ParseToken
// is the only judge of what is a token. Both searches only ever move
// forward: the next opening starts after this one, and the closing brace is
// looked up again only once the opening has passed it, so a run of openings
// sharing one close costs one search. Offsets are bytes here and UTF-16
// code units there; the delimiters are ASCII, so the slices agree.
func tokenCandidates(text string) []string {
	var out []string
	open := strings.Index(text, TokenOpen)
	closing := -1
	for open != -1 {
		body := open + len(TokenOpen)
		if closing < body {
			closing = indexFrom(text, "}", body)
		}
		if closing == -1 {
			return out
		}
		out = append(out, text[open:closing+1])
		open = indexFrom(text, TokenOpen, open+1)
	}
	return out
}

var jsonPathPattern = regexp.MustCompile(`^\$\.[A-Za-z0-9_$.[\]'-]+$`)

// IsToken is refs.ts's isToken.
func IsToken(value any) bool {
	s, ok := value.(string)
	return ok && TokenPattern.MatchString(s)
}

// ParseToken is refs.ts's parseToken; ok is false where TypeScript returns
// undefined.
func ParseToken(value string) (RefEntry, bool) {
	m := TokenPattern.FindStringSubmatch(value)
	if m == nil {
		return RefEntry{}, false
	}
	return RefEntry{Token: value, Type: m[1], Name: m[2], Alias: m[3]}, true
}

// Token is refs.ts's token: the token for a reference, built from its parts.
// Both names must be slugs; alias "" means none.
func Token(refType, name, alias string) (string, error) {
	if err := assertSlug(refType+" name", name); err != nil {
		return "", err
	}
	suffix := ""
	if alias != "" {
		if err := assertSlug("module alias", alias); err != nil {
			return "", err
		}
		suffix = "@" + alias
	}
	return "${cdref:" + refType + ":" + name + suffix + "}", nil
}

func assertSlug(kind, value string) error {
	if !SlugPattern.MatchString(value) {
		return errors.New("Invalid " + kind + ` "` + value + `". Names must be lowercase words separated by single hyphens.`)
	}
	return nil
}

// JSONPath is refs.ts's jsonPath: it checks that p is a single JSONPath
// identifier, the dynamic alternative to a reference.
func JSONPath(p string) (string, error) {
	if !jsonPathPattern.MatchString(p) {
		return "", errors.New(`Invalid JSONPath "` + p + `". Expected a single identifier such as $.Attributes.queueId.`)
	}
	return p, nil
}

// RefPath is refs.ts's RefPath: a reference-bearing field of an action type,
// by catalog path.
type RefPath struct {
	Path string
	Ref  string
}

// RefPathsOf is refs.ts's refPathsOf over actions.ts's REFERENCE_FIELDS: the
// reference-bearing paths of an action type in table order, empty when it has
// none. REFERENCE_FIELDS is read from the catalog's refs, which
// catalog.test.ts holds equal to the table path by path and in order.
func RefPathsOf(actionType string) []RefPath {
	out := []RefPath{}
	if entry := ModeledEntry(actionType); entry != nil {
		for _, r := range entry.Refs {
			out = append(out, RefPath(r))
		}
	}
	return out
}

// ReadRefPath is refs.ts's readRefPath: every value at a reference-bearing
// path within an action's Parameters.
func ReadRefPath(params jsonv.Object, p string) ([]PathHit, error) {
	return ReadPath(params, p)
}

// SlugIdentifier is refs.ts's slugIdentifier: hyphens become underscores,
// and a leading digit takes an underscore prefix.
func SlugIdentifier(slug string) string {
	underscored := strings.ReplaceAll(slug, "-", "_")
	if underscored != "" && underscored[0] >= '0' && underscored[0] <= '9' {
		return "_" + underscored
	}
	return underscored
}

// RefKey is refs.ts's refKey: `queue:front-desk`, `module:survey@prod`.
func RefKey(e RefEntry) string {
	alias := ""
	if e.Alias != "" {
		alias = "@" + e.Alias
	}
	return e.Type + ":" + e.Name + alias
}

// RefVariableName is refs.ts's refVariableName: `queue_front_desk_arn`,
// `module_survey_prod_arn`.
func RefVariableName(e RefEntry) string {
	alias := ""
	if e.Alias != "" {
		alias = "_" + SlugIdentifier(e.Alias)
	}
	return e.Type + "_" + SlugIdentifier(e.Name) + alias + "_arn"
}

// RefMapKeys is refs.ts's refMapKeys: the three key forms a reference map may
// use for one reference, in lookup order.
func RefMapKeys(e RefEntry) [3]string {
	return [3]string{e.Token, RefKey(e), RefVariableName(e)}
}

// LookupRefValue is refs.ts's lookupRefValue: the value a reference map
// holds for a reference, whichever form keys it. Presence, not truthiness.
func LookupRefValue(m map[string]string, e RefEntry) (string, bool) {
	for _, k := range RefMapKeys(e) {
		if v, ok := m[k]; ok {
			return v, true
		}
	}
	return "", false
}

// DescribeMissingRefKey is refs.ts's describeMissingRefKey.
func DescribeMissingRefKey(e RefEntry) string {
	k := RefMapKeys(e)
	return k[0] + ` (key it as "` + k[0] + `", "` + k[1] + `", or "` + k[2] + `")`
}

// CollectRefs is refs.ts's collectRefs: every token in a value, sorted by
// token with JavaScript's < (jsonv.LessUTF16). Like the TypeScript, it scans
// the compact JSON text of the value rather than walking its strings, so it
// agrees in the two places a walk would not: a token spelled as an object key
// is collected, and a candidate that runs from an unterminated `${cdref:`
// across a string boundary is cut at the next `}` and refused by ParseToken,
// while a token that begins inside that span is still seen, because the next
// candidate starts one character after the last opening, not after its
// close. The text is written in JavaScript's key order (jsOrdered) because
// that is what JSON.stringify writes for a parsed object.
func CollectRefs(value any) []RefEntry {
	text := string(jsonv.Encode(jsOrdered(value), ""))
	found := map[string]RefEntry{}
	for _, raw := range tokenCandidates(text) {
		if e, ok := ParseToken(raw); ok {
			found[e.Token] = e
		}
	}
	out := make([]RefEntry, 0, len(found))
	for _, e := range found {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return jsonv.LessUTF16(out[i].Token, out[j].Token) })
	return out
}
