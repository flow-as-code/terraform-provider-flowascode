// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"regexp"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// RefPosition is contract.ts's RefPosition: where a typed sub-block's
// top-level parameter holds references in key or item position
// (flow-as-code docs/adr/0008-case-field-ids.md). Keys is the reference type
// when the catalog names every key of the map (`CaseRequestFields.*~`),
// Items when it names every item of a list of strings
// (`CaseResponseFields[]`); "" is neither. A string there is a reference of
// that type or a literal (a Cases system field id), so the writer writes a
// token as its reference key and a literal as itself, and the reader reads a
// reference key back to the token (conformance/hcl/README.md, rules 11 and
// 20).
type RefPosition struct {
	Keys  string
	Items string
}

var (
	keyPositionPath  = regexp.MustCompile(`^([A-Za-z0-9_$-]+)\.\*~$`)
	itemPositionPath = regexp.MustCompile(`^([A-Za-z0-9_$-]+)\[\]$`)
)

// RefPositions is contract.ts's refPositions: the reference positions of a
// modeled entry's top-level parameters, by Flow language key.
func RefPositions(entry *flowdoc.ModeledAction) map[string]RefPosition {
	out := map[string]RefPosition{}
	byKey := map[string]flowdoc.CatalogParameter{}
	for _, p := range entry.Parameters {
		byKey[p.Key] = p
	}
	for _, r := range entry.Refs {
		if m := keyPositionPath.FindStringSubmatch(r.Path); m != nil {
			if p, ok := byKey[m[1]]; ok && p.Kind == "map" {
				at := out[m[1]]
				at.Keys = r.Ref
				out[m[1]] = at
			}
		}
		if m := itemPositionPath.FindStringSubmatch(r.Path); m != nil {
			if p, ok := byKey[m[1]]; ok && p.Kind == "list" && (p.Of == nil || p.Of.Kind == "string") {
				at := out[m[1]]
				at.Items = r.Ref
				out[m[1]] = at
			}
		}
	}
	return out
}

// writeRefOrLiteral is contract.ts's writeRefOrLiteral: how a string at a
// reference key or item position is written (rule 11). A token of the type
// is written as its reference key, a literal holding no `${` that no reader
// would take for a reference key as itself, and anything else not at all
// (ok false), which writes the action generic.
func writeRefOrLiteral(value, refType string) (string, bool) {
	if entry, ok := flowdoc.ParseToken(value); ok {
		if entry.Type != refType {
			return "", false
		}
		return value[len(flowdoc.TokenOpen) : len(value)-1], true
	}
	if strings.Contains(value, "${") || RefKeyPattern.MatchString(value) {
		return "", false
	}
	return value, true
}
