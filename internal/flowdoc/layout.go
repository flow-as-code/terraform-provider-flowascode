// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"sort"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Deterministic auto-layout, ported from packages/core/src/layout.ts. The
// algorithm is specified in conformance/layout/README.md and pinned by the
// cases beside it, because a `.flow.tf` omits an action's position block
// exactly when the action sits where auto-layout puts it.

// Layout constants, layout.ts's NODE_WIDTH, NODE_HEIGHT, RANK_GAP, NODE_GAP
// and LAYOUT_MARGIN.
const (
	NodeWidth    = 180
	NodeHeight   = 60
	RankGap      = 80
	NodeGap      = 40
	LayoutMargin = 20
)

// Point is flowdoc.ts's Point. JavaScript numbers are float64; auto-layout
// only ever produces integers.
type Point struct {
	X float64
	Y float64
}

// JSON is the point as JSON.stringify writes a Point: {"x": ..., "y": ...}.
func (p Point) JSON() jsonv.Object {
	return jsonv.Object{{Key: "x", Value: p.X}, {Key: "y", Value: p.Y}}
}

func stringAt(o jsonv.Object, key string) (string, bool) {
	v, ok := o.Get(key)
	s, isString := v.(string)
	return s, ok && isString
}

// edgesOf is layout.ts's edgesOf: an action's transition targets in order,
// each at most once, actions only. A target that is not a string cannot name
// an action, as ids.has() finds nothing for it in TypeScript.
func edgesOf(action jsonv.Object, ids map[string]bool) []string {
	t, _ := action.Get("Transitions")
	trans, _ := t.(jsonv.Object)
	var targets []string
	if s, ok := stringAt(trans, "NextAction"); ok {
		targets = append(targets, s)
	}
	for _, key := range []string{"Errors", "Conditions"} {
		list, _ := trans.Get(key)
		items, _ := list.([]any)
		for _, item := range items {
			o, _ := item.(jsonv.Object)
			if s, ok := stringAt(o, "NextAction"); ok {
				targets = append(targets, s)
			}
		}
	}
	out := []string{}
	for _, target := range targets {
		// Dangling targets are a lint finding, not a layout crash.
		if !ids[target] || contains(out, target) {
			continue
		}
		out = append(out, target)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// AutoLayout is layout.ts's autoLayout: left-to-right layered layout, per
// conformance/layout/README.md, deterministic for a given action array.
// actions is content.Actions as decoded (each a jsonv.Object with a string
// Identifier; any other element is skipped). start is the document's
// StartAction; nil means the first action's Identifier, which is
// TypeScript's default when the argument is undefined.
//
// The result is a map; LayoutIDs gives the order TypeScript's record keys
// have.
func AutoLayout(actions []any, start *string) map[string]Point {
	var order []string
	byID := map[string]jsonv.Object{}
	for _, a := range actions {
		o, ok := a.(jsonv.Object)
		if !ok {
			continue
		}
		id, ok := stringAt(o, "Identifier")
		if !ok {
			continue
		}
		if _, seen := byID[id]; !seen {
			byID[id] = o
			order = append(order, id)
		}
	}
	if start == nil && len(actions) > 0 {
		o, _ := actions[0].(jsonv.Object)
		if first, ok := stringAt(o, "Identifier"); ok {
			start = &first
		}
	}
	ids := map[string]bool{}
	for _, id := range order {
		ids[id] = true
	}
	edges := map[string][]string{}
	for _, id := range order {
		edges[id] = edgesOf(byID[id], ids)
	}

	// Depth-first from the start action: discovery order, kept edges (back
	// edges dropped), and post-order, whose reverse is a topological order.
	discovery := map[string]int{}
	kept := map[string][]string{}
	var postOrder []string
	onStack := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		discovery[id] = len(discovery)
		onStack[id] = true
		out := []string{}
		for _, target := range edges[id] {
			if onStack[target] {
				continue // back edge
			}
			out = append(out, target)
			if _, seen := discovery[target]; !seen {
				visit(target)
			}
		}
		kept[id] = out
		delete(onStack, id)
		postOrder = append(postOrder, id)
	}
	if start != nil && ids[*start] {
		visit(*start)
	}

	// Longest kept path from the start, one pass in topological order.
	rank := map[string]int{}
	for i := len(postOrder) - 1; i >= 0; i-- {
		id := postOrder[i]
		r := rank[id]
		rank[id] = r
		for _, target := range kept[id] {
			if cur, ok := rank[target]; !ok || r+1 > cur {
				rank[target] = r + 1
			}
		}
	}

	// Reachable actions in discovery order, then the rest in document order.
	deepest := -1
	for _, r := range rank {
		if r > deepest {
			deepest = r
		}
	}
	ordered := append([]string(nil), order...)
	sort.SliceStable(ordered, func(i, j int) bool {
		da, oka := discovery[ordered[i]]
		db, okb := discovery[ordered[j]]
		switch {
		case oka && okb:
			return da < db
		case oka:
			return true
		default:
			return false
		}
	})

	positions := map[string]Point{}
	nextIndex := map[int]int{}
	for _, id := range ordered {
		r, ok := rank[id]
		if !ok {
			r = deepest + 1
		}
		index := nextIndex[r]
		nextIndex[r] = index + 1
		positions[id] = Point{
			X: float64(LayoutMargin + r*(NodeWidth+RankGap)),
			Y: float64(LayoutMargin + index*(NodeHeight+NodeGap)),
		}
	}
	return positions
}

// LayoutIDs is the key order of the record TypeScript's autoLayout returns:
// each Identifier once, in document order, then put in JavaScript's own-key
// order (an Identifier that is an array index, such as "7", comes first).
func LayoutIDs(actions []any) []string {
	var order []string
	seen := map[string]bool{}
	for _, a := range actions {
		o, _ := a.(jsonv.Object)
		if id, ok := stringAt(o, "Identifier"); ok && !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	return jsKeyOrder(order)
}

// LayoutJSON is the layout as JSON.stringify writes autoLayout's record.
func LayoutJSON(actions []any, positions map[string]Point) jsonv.Object {
	out := jsonv.Object{}
	for _, id := range LayoutIDs(actions) {
		out = append(out, jsonv.Member{Key: id, Value: positions[id].JSON()})
	}
	return out
}
