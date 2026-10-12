// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package materialize is @flow-as-code/core's materialize.ts: FlowDoc in,
// deployable Flow language content out, with every `${cdref:...}` token
// replaced by the value it stands for, and serializeContent, the exact bytes
// the TypeScript writes for that content.
//
// Two backends resolve tokens (SPEC.md, Materialization):
//   - MaterializeWithMap: strict lookup in a reference map; every missing
//     reference is reported at once in a MaterializeError.
//   - MaterializeWithBinder: the binder returns opaque strings that pass
//     through byte for byte with no validation.
//
// Both emit content only: layout, refs and meta are tool metadata and are
// dropped, after layout is projected into content.Metadata so the flow lays
// out in the Connect console. The Metadata shape follows the Flow language
// example (EntryPointPosition, ActionMetadata.<id>.Position):
// https://docs.aws.amazon.com/connect/latest/devguide/flow-language-example.html
//
// Content is held as jsonv values, in the key order JavaScript would give the
// object the TypeScript builds, so jsonv.Encode of a result is
// JSON.stringify of the TypeScript's.
package materialize

import (
	"sort"
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// TypeError is a JavaScript TypeError the TypeScript would throw on a shape
// assertFlowDoc does not check, with V8's message for it byte for byte (Node
// is what runs @flow-as-code/core), so both sides refuse the same documents
// with the same words.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

func readError(of string, property string) error {
	return &TypeError{Message: "Cannot read properties of " + of + " (reading '" + property + "')"}
}

// keyFormsSentence is materialize.ts's keyFormsSentence: the three key forms
// a reference map accepts, worked through the first missing reference.
func keyFormsSentence(missingRefs []flowdoc.RefEntry) string {
	if len(missingRefs) == 0 {
		return ""
	}
	k := flowdoc.RefMapKeys(missingRefs[0])
	return ` Key each one by its token ("` + k[0] + `"), by type and name ("` + k[1] + `"), ` +
		`or by the variable the terraform emitter writes ("` + k[2] + `").`
}

// MaterializeError is materialize.ts's MaterializeError. MissingTokens is
// sorted and complete, so a caller fixes the map once. MissingRefs carries
// the same references parsed; it is empty for the other failure this error
// reports, a token that survived materialization, where the map is not the
// problem.
type MaterializeError struct {
	MissingTokens []string
	MissingRefs   []flowdoc.RefEntry
	Message       string
}

func (e *MaterializeError) Error() string { return e.Message }

// NewMaterializeError is MaterializeError's constructor. A nil reason is the
// TypeScript's undefined: the message then lists unmapped tokens and names
// the key forms; otherwise it states the reason and the offending tokens.
func NewMaterializeError(missingTokens []string, reason *string, missingRefs []flowdoc.RefEntry) *MaterializeError {
	if missingRefs == nil {
		missingRefs = []flowdoc.RefEntry{}
	}
	var msg string
	if reason == nil {
		msg = "Cannot materialize: " + strconv.Itoa(len(missingTokens)) + " unmapped token(s): " +
			strings.Join(missingTokens, ", ") + keyFormsSentence(missingRefs)
	} else {
		msg = "Cannot materialize: " + *reason + " Offending token(s): " + strings.Join(missingTokens, ", ")
	}
	return &MaterializeError{MissingTokens: missingTokens, MissingRefs: missingRefs, Message: msg}
}

// MissingKeysError is MaterializeError.missingKeys: the error for references
// no key form of the map matched.
func MissingKeysError(missingRefs []flowdoc.RefEntry) *MaterializeError {
	tokens := make([]string, len(missingRefs))
	for i, e := range missingRefs {
		tokens[i] = e.Token
	}
	return NewMaterializeError(tokens, nil, missingRefs)
}

// resolver is the resolve callback of materialize.ts: the value a reference
// becomes, which on the map path is undefined for a reference no key form
// matches.
type resolver func(flowdoc.RefEntry) any

// resolveString is materialize.ts's resolveString: a whole-string token
// resolved, anything else returned as it was.
func resolveString(value string, resolve resolver) any {
	if entry, ok := flowdoc.ParseToken(value); ok {
		return resolve(entry)
	}
	return value
}

// keyString is a resolved value used as an object key, as JavaScript's
// property-key conversion writes it. The map path resolves only references
// it holds a string for, and a binder returns a string, so only undefined,
// which neither produces, needs spelling.
func keyString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "undefined"
}

// resolveDeep is materialize.ts's resolveDeep. A token occupies an entire
// field value, or an entire map key for a casefield (FlowDoc invariant 4,
// docs/adr/0008-case-field-ids.md), so only whole strings are swapped, keys
// and values alike. Objects are rebuilt as Object.fromEntries rebuilds
// Object.entries: each key is resolved before its value, in JavaScript's
// own-key order, so a binder sees its calls in that order; two keys that
// resolve to one keep the first one's place and the last one's value; and
// the result is in own-key order again, since a resolved key may be an
// array index.
func resolveDeep(value any, resolve resolver) any {
	switch t := value.(type) {
	case string:
		return resolveString(t, resolve)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = resolveDeep(item, resolve)
		}
		return out
	case jsonv.Object:
		built := jsonv.Object{}
		for _, k := range jsKeyOrder(t.Keys()) {
			v, _ := t.Get(k)
			key := keyString(resolveString(k, resolve))
			built.Set(key, resolveDeep(v, resolve))
		}
		out := jsonv.Object{}
		for _, k := range jsKeyOrder(built.Keys()) {
			v, _ := built.Get(k)
			out = append(out, jsonv.Member{Key: k, Value: v})
		}
		return out
	}
	return value
}

// entryPointOffsetX is how far left of the StartAction block the console
// entry point marker sits (materialize.ts's ENTRY_POINT_OFFSET_X).
const entryPointOffsetX = 100

// member reads an own member, or undefined.
func member(o jsonv.Object, key string) any {
	if v, ok := o.Get(key); ok {
		return v
	}
	return undefined
}

// protoRecord is an ordinary object the TypeScript builds with `{}` and then
// assigns into by identifier: an own member list plus the prototype an
// assignment to "__proto__" installs, which later reads fall back to.
type protoRecord struct {
	own   jsonv.Object
	proto any // nil: Object.prototype
}

// read is record[key].
func (r *protoRecord) read(key string) any {
	if v, ok := r.own.Get(key); ok {
		return v
	}
	if key == "__proto__" {
		if r.proto == nil {
			return objectPrototype{}
		}
		return r.proto
	}
	if r.proto != nil {
		return get(r.proto, key)
	}
	return inherited(key)
}

// assign is record[key] = value: an own member, except that "__proto__"
// without an own member of that name sets the prototype (an object) or is
// ignored (a primitive).
func (r *protoRecord) assign(key string, value any) {
	if _, ok := r.own.Get(key); ok || key != "__proto__" {
		r.own.Set(key, value)
		return
	}
	if isObject(value) {
		r.proto = value
	}
}

// autoLayoutPreflight throws what layout.ts's edgesOf throws before
// flowdoc.AutoLayout, which skips what it cannot read, is asked: edgesOf
// runs over each distinct Identifier's first action, in document order, and
// maps Transitions.Errors then Transitions.Conditions after `?? []`.
func autoLayoutPreflight(actions []any) error {
	seen := map[string]bool{}
	for _, a := range actions {
		action, _ := a.(jsonv.Object)
		id, _ := member(action, "Identifier").(string)
		if seen[id] {
			continue
		}
		seen[id] = true
		transitions := member(action, "Transitions")
		for _, key := range []string{"Errors", "Conditions"} {
			list := get(transitions, key)
			if nullish(list) {
				continue
			}
			items, ok := list.([]any)
			if !ok {
				return &TypeError{Message: "(t." + key + " ?? []).map is not a function or its return value is not iterable"}
			}
			for _, item := range items {
				if item == nil {
					return readError("null", "NextAction")
				}
			}
		}
	}
	return nil
}

// projectLayout is materialize.ts's projectLayout: layout projected into the
// Metadata shape the console reads. A position comes from the document's
// layout, else from the auto-layout synth would assign, else (0, 0).
// Metadata the document already carries is resolved like the rest of the
// content and kept; within ActionMetadata only Position is overwritten.
func projectLayout(doc jsonv.Object, resolve resolver) (jsonv.Object, error) {
	content := member(doc, "content").(jsonv.Object)
	actions := member(content, "Actions").([]any)
	start := member(content, "StartAction").(string)

	var layout any = member(doc, "layout")
	if nullish(layout) {
		layout = jsonv.Object{}
	}
	missing := false
	for _, a := range actions {
		id := member(a.(jsonv.Object), "Identifier").(string)
		if _, isUndefined := get(layout, id).(undefinedValue); isUndefined {
			missing = true
			break
		}
	}
	auto := &protoRecord{own: jsonv.Object{}}
	if missing {
		if err := autoLayoutPreflight(actions); err != nil {
			return nil, err
		}
		// layout.ts builds its record by assignment, so an Identifier of
		// "__proto__" lands in the prototype, where a read still finds it.
		positions := flowdoc.AutoLayout(actions, &start)
		for _, id := range flowdoc.LayoutIDs(actions) {
			auto.assign(id, positions[id])
		}
	}

	metadata := member(content, "Metadata")
	if nullish(metadata) {
		metadata = jsonv.Object{}
	}
	existing := resolveDeep(metadata, resolve)
	if _, isUndefined := existing.(undefinedValue); isUndefined {
		return nil, readError("undefined", "ActionMetadata")
	}
	existingActionMetadata := jsonv.Object{}
	if raw := get(existing, "ActionMetadata"); isRecord(raw) {
		existingActionMetadata = spread(raw)
	}

	actionMetadata := &protoRecord{own: existingActionMetadata}
	positions := &protoRecord{own: jsonv.Object{}}
	for _, a := range actions {
		id := member(a.(jsonv.Object), "Identifier").(string)
		var p any = get(layout, id)
		if nullish(p) {
			p = auto.read(id)
		}
		if nullish(p) {
			p = flowdoc.Point{}
		}
		positions.assign(id, p)
		priorEntry := jsonv.Object{}
		if prior := actionMetadata.read(id); isRecord(prior) {
			priorEntry = spread(prior)
		}
		priorEntry.Set("Position", jsonv.Object{
			{Key: "x", Value: get(p, "x")},
			{Key: "y", Value: get(p, "y")},
		})
		actionMetadata.assign(id, priorEntry)
	}

	// The example places the entry point marker left of the first block
	// (EntryPointPosition x=88 vs first Action x=270). Math.max(NaN, 0) is
	// NaN, which JSON.stringify writes as null.
	startPosition := positions.read(start)
	if nullish(startPosition) {
		startPosition = flowdoc.Point{}
	}
	x := toNumber(get(startPosition, "x")) - entryPointOffsetX
	if x < 0 {
		x = 0
	}
	out := spread(existing)
	out.Set("EntryPointPosition", jsonv.Object{
		{Key: "x", Value: x},
		{Key: "y", Value: get(startPosition, "y")},
	})
	out.Set("ActionMetadata", actionMetadata.own)
	return out, nil
}

// materialize is materialize.ts's materialize. The document is held to
// FlowDoc invariant 4 first (refuseInterpolated), on both backends. A module
// requires a top-level
// Settings in its deployable content; Connect rejects the create without it.
// It defaults to {} and is resolved like the rest of the content. Settings,
// Metadata and Actions are resolved in that order, which is the order a
// binder sees its calls in.
func materialize(doc jsonv.Object, resolve resolver) (jsonv.Object, error) {
	content := member(doc, "content").(jsonv.Object)
	if err := refuseInterpolated(content); err != nil {
		return nil, err
	}
	settings := any(undefined)
	settingsValue, hasSettings := content.Get("Settings")
	if kind, _ := member(doc, "kind").(string); kind == "module" || hasSettings {
		if nullish(settingsValue) {
			settingsValue = jsonv.Object{}
		}
		settings = resolveDeep(settingsValue, resolve)
	}
	metadata, err := projectLayout(doc, resolve)
	if err != nil {
		return nil, err
	}
	list := member(content, "Actions").([]any)
	actions := make([]any, len(list))
	for i, a := range list {
		actions[i] = resolveDeep(a, resolve)
	}
	out := jsonv.Object{
		{Key: "Version", Value: member(content, "Version")},
		{Key: "StartAction", Value: member(content, "StartAction")},
	}
	if _, isUndefined := settings.(undefinedValue); !isUndefined {
		out = append(out, jsonv.Member{Key: "Settings", Value: settings})
	}
	out = append(out,
		jsonv.Member{Key: "Metadata", Value: metadata},
		jsonv.Member{Key: "Actions", Value: actions},
	)
	return finish(out).(jsonv.Object), nil
}

// embeddedTokens is materialize.ts's embeddedTokens: every `${cdref:...}`
// inside one string, added to found: an opening followed by a closing brace
// before any double quote. One forward pass, as the TypeScript's is since
// flow-as-code 78b0a87 (it replaced a regex code scanning flagged as
// polynomial): the closing position is looked up again only once an opening
// has moved past it, and a search that ends at a quote moves on by one
// opening, so the next opening after a match is sought past the match, never
// inside it. Offsets are bytes here and UTF-16 code units there; the
// delimiters are ASCII, so the slices agree.
func embeddedTokens(text string, found map[string]bool) {
	open := strings.Index(text, flowdoc.TokenOpen)
	closing := -1
	for open != -1 {
		body := open + len(flowdoc.TokenOpen)
		if closing < body {
			closing = body
			for closing < len(text) && text[closing] != '}' && text[closing] != '"' {
				closing++
			}
		}
		if closing == len(text) {
			break
		}
		if text[closing] == '}' {
			found[text[open:closing+1]] = true
			open = indexFrom(text, flowdoc.TokenOpen, closing+1)
		} else {
			open = indexFrom(text, flowdoc.TokenOpen, open+1)
		}
	}
}

// interpolatedTokens is materialize.ts's interpolatedTokens: every token
// embedded in a longer key or value anywhere in value, sorted by
// JavaScript's <. A string that is a whole token is what substitution swaps;
// any other string holding one would survive substitution as literal text,
// on either backend, so the check runs over the document before anything is
// resolved rather than over the output, where a binder that returns the
// token itself is legitimate.
func interpolatedTokens(values ...any) []string {
	found := map[string]bool{}
	visit := func(s string) {
		if _, ok := flowdoc.ParseToken(s); !ok {
			embeddedTokens(s, found)
		}
	}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			visit(t)
		case []any:
			for _, x := range t {
				walk(x)
			}
		case jsonv.Object:
			for _, m := range t {
				visit(m.Key)
				walk(m.Value)
			}
		}
	}
	for _, v := range values {
		walk(v)
	}
	out := make([]string, 0, len(found))
	for tok := range found {
		out = append(out, tok)
	}
	sort.Slice(out, func(i, j int) bool { return jsonv.LessUTF16(out[i], out[j]) })
	return out
}

const interpolatedReason = "Token(s) would survive materialization because they are embedded in a longer string. " +
	"A reference must be the entire field value, or the entire map key for a casefield " +
	"(FlowDoc invariant 4)."

// refuseInterpolated is materialize.ts's refuseInterpolated: the refusal
// both backends raise for a token that is not a whole key or value, over
// content's Settings, Metadata and Actions.
func refuseInterpolated(content jsonv.Object) error {
	settings, _ := content.Get("Settings")
	metadata, _ := content.Get("Metadata")
	actions, _ := content.Get("Actions")
	if leaked := interpolatedTokens(settings, metadata, actions); len(leaked) > 0 {
		reason := interpolatedReason
		return NewMaterializeError(leaked, &reason, nil)
	}
	return nil
}

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

// MaterializeWithMap is materialize.ts's materializeWithMap: strict
// materialization against a reference map. Every reference in the content
// must have an entry under one of the three key forms (refs.ts's
// refMapKeys, looked up with flowdoc.LookupRefValue, which checks presence,
// so an empty value counts as mapped); otherwise a MaterializeError lists
// every missing reference, sorted. Mapped values are not validated. A nil
// map is the TypeScript's null and is refused as it is.
//
// Completeness is not enough: collectRefs finds a token anywhere in a string
// while substitution only replaces a whole string, so materialize refuses an
// interpolated token first rather than shipping it as text Connect would
// read aloud.
func MaterializeWithMap(doc any, resourceMap map[string]string) (jsonv.Object, error) {
	d, err := flowdoc.AssertFlowDoc(doc, "materializeWithMap")
	if err != nil {
		return nil, err
	}
	if resourceMap == nil {
		return nil, &flowdoc.InvalidFlowDocError{
			Message: "materializeWithMap expects a token to value map object as its second argument.",
		}
	}
	var missing []flowdoc.RefEntry
	for _, entry := range flowdoc.CollectRefs(member(d, "content")) {
		if _, ok := flowdoc.LookupRefValue(resourceMap, entry); !ok {
			missing = append(missing, entry)
		}
	}
	if len(missing) > 0 {
		return nil, MissingKeysError(missing)
	}

	return materialize(d, func(entry flowdoc.RefEntry) any {
		if v, ok := flowdoc.LookupRefValue(resourceMap, entry); ok {
			return v
		}
		return undefined
	})
}

// MaterializeWithBinder is materialize.ts's materializeWithBinder, for IaC
// backends: the binder returns an opaque string per reference, typically a
// CDK token CloudFormation resolves later, and it passes through exactly as
// returned, with no validation. The document is held to invariant 4 before
// the binder runs, as on the map path. A nil binder is refused as the
// TypeScript refuses a non-function.
func MaterializeWithBinder(doc any, binder func(flowdoc.RefEntry) string) (jsonv.Object, error) {
	d, err := flowdoc.AssertFlowDoc(doc, "materializeWithBinder")
	if err != nil {
		return nil, err
	}
	if binder == nil {
		return nil, &flowdoc.InvalidFlowDocError{
			Message: "materializeWithBinder expects a binder function as its second argument.",
		}
	}
	return materialize(d, func(entry flowdoc.RefEntry) any { return binder(entry) })
}
