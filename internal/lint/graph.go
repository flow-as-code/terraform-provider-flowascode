// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strconv"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Shared traversal helpers, ported from packages/core/src/lint/graph.ts.
//
// Every document reaching a rule has passed flowdoc.AssertFlowDoc, so name,
// kind, connectType, content.StartAction and each action's Identifier and
// Type are strings, and Parameters and Transitions are objects. Anything
// deeper is read as found. Where graph.ts or a rule would throw a TypeError
// on a shape the assertion lets through (Transitions.Errors that is neither
// an array nor null, a null element in it, refs that is not an array), the
// port skips the malformed part instead; the schema rejects every such
// document.

// action is one element of content.Actions, its asserted fields unpacked.
type action struct {
	id          string
	typ         string
	parameters  jsonv.Object
	transitions jsonv.Object
}

func str(o jsonv.Object, key string) string {
	v, _ := o.Get(key)
	s, _ := v.(string)
	return s
}

func docName(doc jsonv.Object) string { return str(doc, "name") }

func docContent(doc jsonv.Object) jsonv.Object {
	v, _ := doc.Get("content")
	c, _ := v.(jsonv.Object)
	return c
}

func startAction(doc jsonv.Object) string { return str(docContent(doc), "StartAction") }

func actionsOf(doc jsonv.Object) []action {
	v, _ := docContent(doc).Get("Actions")
	list, _ := v.([]any)
	out := make([]action, 0, len(list))
	for _, item := range list {
		o, _ := item.(jsonv.Object)
		p, _ := o.Get("Parameters")
		t, _ := o.Get("Transitions")
		params, _ := p.(jsonv.Object)
		trans, _ := t.(jsonv.Object)
		out = append(out, action{id: str(o, "Identifier"), typ: str(o, "Type"), parameters: params, transitions: trans})
	}
	return out
}

// listAt is `(o[key] ?? [])` for a key meant to hold an array.
func listAt(o jsonv.Object, key string) []any {
	v, _ := o.Get(key)
	list, _ := v.([]any)
	return list
}

// field is `element.key` on an arbitrary array element: undefined unless the
// element is an object that has the key.
func field(element any, key string) any {
	o, ok := element.(jsonv.Object)
	if !ok {
		return undefined
	}
	v, has := o.Get(key)
	if !has {
		return undefined
	}
	return v
}

// transitionTargets is graph.ts's transitionTargets: NextAction when present
// (a present null included), then every Errors and every Conditions
// element's NextAction, which is undefined where the element has none. The
// values are whatever the document holds; only a string can name an action.
func transitionTargets(a action) []any {
	var out []any
	if v, has := a.transitions.Get("NextAction"); has {
		out = append(out, v)
	}
	for _, e := range listAt(a.transitions, "Errors") {
		out = append(out, field(e, "NextAction"))
	}
	for _, c := range listAt(a.transitions, "Conditions") {
		out = append(out, field(c, "NextAction"))
	}
	return out
}

// isTerminal is graph.ts's isTerminal: Transitions has no keys.
func isTerminal(a action) bool { return len(a.transitions) == 0 }

// actionsByID is graph.ts's actionsById. A repeated Identifier maps to its
// last action, as new Map(entries) keeps the last value.
func actionsByID(doc jsonv.Object) map[string]action {
	out := map[string]action{}
	for _, a := range actionsOf(doc) {
		out[a.id] = a
	}
	return out
}

// reachable is graph.ts's reachable: Identifiers reachable from
// StartAction.
func reachable(doc jsonv.Object) map[string]bool {
	byID := actionsByID(doc)
	seen := map[string]bool{}
	queue := []any{startAction(doc)}
	for len(queue) > 0 {
		id, isString := queue[0].(string)
		queue = queue[1:]
		if !isString || seen[id] {
			continue
		}
		a, ok := byID[id]
		if !ok {
			continue
		}
		seen[id] = true
		queue = append(queue, transitionTargets(a)...)
	}
	return seen
}

// StringAt is one [jsonPath, value] pair from WalkStrings.
type StringAt struct {
	Path  string
	Value string
}

// WalkStrings is graph.ts's walkStrings: every string in a value with the
// path it sits at. An array element is `path[i]` (so `[0]` at the root), an
// object member `path.key` (just `key` at the root), and an object's members
// come in Object.entries order.
func WalkStrings(value any, path string) []StringAt {
	switch t := value.(type) {
	case string:
		return []StringAt{{Path: path, Value: t}}
	case []any:
		var out []StringAt
		for i, v := range t {
			out = append(out, WalkStrings(v, path+"["+strconv.Itoa(i)+"]")...)
		}
		return out
	case jsonv.Object:
		var out []StringAt
		for _, m := range entries(t) {
			child := m.Key
			if path != "" {
				child = path + "." + m.Key
			}
			out = append(out, WalkStrings(m.Value, child)...)
		}
		return out
	}
	return nil
}

// documentStrings is graph.ts's documentStrings: authored strings that
// belong to the document rather than to any one action, today
// content.Metadata.
func documentStrings(doc jsonv.Object) []StringAt {
	md, has := docContent(doc).Get("Metadata")
	if !has {
		return nil
	}
	return WalkStrings(md, "content.Metadata")
}

// actionStrings is one element of graph.ts's findingsForActions.
type actionStrings struct {
	action  action
	strings []StringAt
}

// findingsForActions is graph.ts's findingsForActions: every authored string
// in each action, Parameters then Transitions, for attributing a finding to
// a block.
func findingsForActions(doc jsonv.Object) []actionStrings {
	var out []actionStrings
	for _, a := range actionsOf(doc) {
		s := WalkStrings(a.parameters, "")
		s = append(s, WalkStrings(a.transitions, "Transitions")...)
		out = append(out, actionStrings{action: a, strings: s})
	}
	return out
}
