// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The action catalog, conformance/flow-language/catalog.json as typed data,
// ported from packages/core/src/catalog.ts. The file is read from the
// vendored conformance tree (conformance.FS()), the same bytes the TypeScript
// imports as src/catalog/catalog.json.
//
// Decoding is strict: a key this port does not know fails the load, so a
// catalog that grows a field cannot be read here as if it had not.

// ParameterKind is catalog.ts's ParameterKind: string, integer,
// integerString, enum, ref, jsonPath, stringOrJsonPath, map, list, object or
// json.
type ParameterKind string

// CatalogConstraint is catalog.ts's CatalogConstraint. Rule is exactlyOne,
// atMostOne or neverBoth.
type CatalogConstraint struct {
	Rule string   `json:"rule"`
	Keys []string `json:"keys"`
}

// CatalogElement is catalog.ts's CatalogElement: the shape of a value, a
// parameter without its key, as list elements are. A nil slice or pointer is
// TypeScript's undefined.
type CatalogElement struct {
	Kind ParameterKind `json:"kind"`
	// enum
	Values []string `json:"values,omitempty"`
	// ref
	Ref string `json:"ref,omitempty"`
	// object
	Fields      []CatalogParameter  `json:"fields,omitempty"`
	Constraints []CatalogConstraint `json:"constraints,omitempty"`
	// list: the element shape; map: the value shape, when it is not a string.
	Of *CatalogElement `json:"of,omitempty"`
	// integer and integerString: the value's bounds; list and map: the entry
	// count's.
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
	// map: the keys the page allows, when it lists them.
	Keys []string `json:"keys,omitempty"`
	// The page also accepts a single JSONPath identifier in this position.
	// Absent reads false, as TypeScript's `dynamic === true` tests do.
	Dynamic bool `json:"dynamic,omitempty"`
}

// CatalogParameter is catalog.ts's CatalogParameter.
type CatalogParameter struct {
	// The Flow language key.
	Key string `json:"key"`
	// The HCL attribute name, always snakeCaseKey(key).
	Attr     string `json:"attr"`
	Required bool   `json:"required"`
	CatalogElement
}

// CatalogRef is catalog.ts's CatalogRef: a catalog path (paths.go) into
// Parameters and the reference type it holds.
type CatalogRef struct {
	Path string `json:"path"`
	Ref  string `json:"ref"`
}

// CatalogError is catalog.ts's CatalogError.
type CatalogError struct {
	Type string `json:"type"`
	// Whether error-branches reports the branch as missing.
	Required bool `json:"required"`
	// Whether the builder's modeled form wires this branch.
	Builder bool `json:"builder"`
	// Free text from the action page, when the error exists only in some
	// forms; "" when absent.
	When string `json:"when,omitempty"`
	// A top-level Parameters key whose presence makes the branch required;
	// "" when absent.
	RequiredWhenKey string `json:"requiredWhenKey,omitempty"`
}

// ConditionsKind is catalog.ts's ConditionsKind: none, fixed, dtmf, enum,
// numeric or custom.
type ConditionsKind string

// CatalogTransitions is catalog.ts's CatalogTransitions.
type CatalogTransitions struct {
	// NextRule: required, none, mirrors:error:<type> or
	// mirrors:condition:<operand>.
	Next       string         `json:"next"`
	Conditions ConditionsKind `json:"conditions"`
	// The fewest conditions the service accepts, when it refuses an action
	// without any (CheckMetricData, observed 2026-09-29); absent reads 0.
	MinConditions int `json:"minConditions,omitempty"`
	// For kind fixed, the operands in builder order; for kind enum, the
	// operands the page names, when it names them.
	ConditionOperands []string `json:"conditionOperands,omitempty"`
	// In the order the builder emits them.
	Errors []CatalogError `json:"errors"`
	// The action holds the participant until something outside the flow
	// moves them on; absent reads false.
	Waits bool `json:"waits,omitempty"`
}

// FlowTypes is catalog.ts's `readonly string[] | "unrestricted"`: the
// ConnectType values an action is legal in, or the recorded absence of a
// restriction.
type FlowTypes struct {
	Unrestricted bool
	Types        []string
}

// UnmarshalJSON accepts a list of ConnectType values or the string
// "unrestricted", and nothing else.
func (f *FlowTypes) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if s != "unrestricted" {
			return fmt.Errorf("flowTypes %q is neither a list nor \"unrestricted\"", s)
		}
		*f = FlowTypes{Unrestricted: true}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*f = FlowTypes{Types: list}
	return nil
}

// ActionCategory is catalog.ts's ActionCategory: contact, participant,
// flowControl or interaction.
type ActionCategory string

// ModeledAction is catalog.ts's ModeledAction.
// CatalogShape is catalog.ts's CatalogShape: a shape an action must take
// when one of its parameters has, or lacks, a static value. The
// conditional-shape lint rule reads it. An absent parameter counts as not
// equal to any value.
type CatalogShape struct {
	When struct {
		Key       string  `json:"key"`
		Equals    *string `json:"equals,omitempty"`
		NotEquals *string `json:"notEquals,omitempty"`
	} `json:"when"`
	Requires *struct {
		Parameters []string `json:"parameters,omitempty"`
		Errors     []string `json:"errors,omitempty"`
	} `json:"requires,omitempty"`
	Forbids *struct {
		Parameters []string `json:"parameters,omitempty"`
		Errors     []string `json:"errors,omitempty"`
		Conditions bool     `json:"conditions,omitempty"`
	} `json:"forbids,omitempty"`
	Source string `json:"source"`
}

type ModeledAction struct {
	Category ActionCategory `json:"category"`
	Doc      string         `json:"doc"`
	Modeled  bool           `json:"modeled"`
	// The HCL sub-block name, snakeCaseKey of the Type.
	Block       string              `json:"block"`
	Terminal    bool                `json:"terminal"`
	FlowTypes   FlowTypes           `json:"flowTypes"`
	Parameters  []CatalogParameter  `json:"parameters"`
	Constraints []CatalogConstraint `json:"constraints,omitempty"`
	// Parameter-dependent shapes; see CatalogShape.
	Shapes []CatalogShape `json:"shapes,omitempty"`
	Refs   []CatalogRef   `json:"refs"`
	// Paths whose string is billed prompt text (Text and SSML forms).
	TextBodies []string `json:"textBodies,omitempty"`
	// Paths whose non-blank string means the participant hears something.
	Announces []string `json:"announces,omitempty"`
	// Path to a list whose non-empty value turns recording on; "" when
	// absent.
	RecordingEnabler string             `json:"recordingEnabler,omitempty"`
	Transitions      CatalogTransitions `json:"transitions"`
}

// CatalogAction is catalog.ts's CatalogAction, ModeledAction |
// UnmodeledAction: Model is set exactly when Modeled is true.
type CatalogAction struct {
	Type     string
	Category ActionCategory
	Doc      string
	Modeled  bool
	Model    *ModeledAction
}

// CatalogCategory is one entry of catalog.ts's categories record, in file
// order.
type CatalogCategory struct {
	Name  ActionCategory
	Doc   string
	Types []string
}

// FlowTypeGroup is one entry of catalog.ts's flowTypeGroups record, in file
// order.
type FlowTypeGroup struct {
	Name  string
	Types []string
}

// ActionCatalog is catalog.ts's ActionCatalog. The TypeScript records are
// ordered slices here, in the file's order, which is the order
// Object.entries gives (no key in them is an array index); Entry looks an
// action up by type.
type ActionCatalog struct {
	Catalog      string
	Recorded     string
	FlowLanguage struct {
		Version string `json:"version"`
		Root    string `json:"root"`
		Actions string `json:"actions"`
	}
	Categories     []CatalogCategory
	RefTypes       []string
	FlowTypeGroups []FlowTypeGroup
	Actions        []CatalogAction
	byType         map[string]int
}

// Entry is the action for a type, or nil.
func (c *ActionCatalog) Entry(actionType string) *CatalogAction {
	if i, ok := c.byType[actionType]; ok {
		return &c.Actions[i]
	}
	return nil
}

// CatalogPath is the catalog's path within the conformance tree.
const CatalogPath = "flow-language/catalog.json"

var (
	catalogOnce sync.Once
	catalogData *ActionCatalog
	catalogErr  error
)

// Catalog is catalog.ts's actionCatalog, read once from the vendored
// conformance tree. It panics if the embedded file does not decode, which the
// tests in this package rule out.
func Catalog() *ActionCatalog {
	catalogOnce.Do(func() {
		var b []byte
		b, catalogErr = fs.ReadFile(conformance.FS(), CatalogPath)
		if catalogErr == nil {
			catalogData, catalogErr = ParseCatalog(b)
		}
	})
	if catalogErr != nil {
		panic(fmt.Sprintf("flowdoc: %s: %v", CatalogPath, catalogErr))
	}
	return catalogData
}

func strictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ParseCatalog decodes catalog.json, keeping the order of its records.
func ParseCatalog(b []byte) (*ActionCatalog, error) {
	v, err := jsonv.Decode(b)
	if err != nil {
		return nil, err
	}
	root, ok := v.(jsonv.Object)
	if !ok {
		return nil, fmt.Errorf("catalog is not an object")
	}
	c := &ActionCatalog{byType: map[string]int{}}
	sub := func(key string, out any) error {
		val, ok := root.Get(key)
		if !ok {
			return fmt.Errorf("catalog has no %q", key)
		}
		if err := strictUnmarshal(jsonv.Encode(val, ""), out); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		return nil
	}
	record := func(key string) (jsonv.Object, error) {
		val, _ := root.Get(key)
		o, ok := val.(jsonv.Object)
		if !ok {
			return nil, fmt.Errorf("catalog %q is not an object", key)
		}
		return o, nil
	}
	for _, m := range root {
		switch m.Key {
		case "catalog", "recorded", "flowLanguage", "categories", "refTypes", "flowTypeGroups", "actions":
		default:
			return nil, fmt.Errorf("catalog has an unknown key %q", m.Key)
		}
	}
	if err := sub("catalog", &c.Catalog); err != nil {
		return nil, err
	}
	if err := sub("recorded", &c.Recorded); err != nil {
		return nil, err
	}
	if err := sub("flowLanguage", &c.FlowLanguage); err != nil {
		return nil, err
	}
	if err := sub("refTypes", &c.RefTypes); err != nil {
		return nil, err
	}

	categories, err := record("categories")
	if err != nil {
		return nil, err
	}
	for _, m := range categories {
		var cat struct {
			Doc   string   `json:"doc"`
			Types []string `json:"types"`
		}
		if err := strictUnmarshal(jsonv.Encode(m.Value, ""), &cat); err != nil {
			return nil, fmt.Errorf("categories.%s: %w", m.Key, err)
		}
		c.Categories = append(c.Categories, CatalogCategory{Name: ActionCategory(m.Key), Doc: cat.Doc, Types: cat.Types})
	}

	groups, err := record("flowTypeGroups")
	if err != nil {
		return nil, err
	}
	for _, m := range groups {
		var types []string
		if err := strictUnmarshal(jsonv.Encode(m.Value, ""), &types); err != nil {
			return nil, fmt.Errorf("flowTypeGroups.%s: %w", m.Key, err)
		}
		c.FlowTypeGroups = append(c.FlowTypeGroups, FlowTypeGroup{Name: m.Key, Types: types})
	}

	actions, err := record("actions")
	if err != nil {
		return nil, err
	}
	for _, m := range actions {
		raw := jsonv.Encode(m.Value, "")
		var head struct {
			Modeled bool `json:"modeled"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return nil, fmt.Errorf("actions.%s: %w", m.Key, err)
		}
		entry := CatalogAction{Type: m.Key, Modeled: head.Modeled}
		if head.Modeled {
			var model ModeledAction
			if err := strictUnmarshal(raw, &model); err != nil {
				return nil, fmt.Errorf("actions.%s: %w", m.Key, err)
			}
			entry.Category, entry.Doc, entry.Model = model.Category, model.Doc, &model
		} else {
			var plain struct {
				Category ActionCategory `json:"category"`
				Doc      string         `json:"doc"`
				Modeled  bool           `json:"modeled"`
			}
			if err := strictUnmarshal(raw, &plain); err != nil {
				return nil, fmt.Errorf("actions.%s: %w", m.Key, err)
			}
			entry.Category, entry.Doc = plain.Category, plain.Doc
		}
		c.byType[m.Key] = len(c.Actions)
		c.Actions = append(c.Actions, entry)
	}
	return c, nil
}

// CatalogEntry is catalog.ts's catalogEntry: the entry for a type, or nil.
// Only the catalog's own keys match, as Object.hasOwn does.
func CatalogEntry(actionType string) *CatalogAction {
	return Catalog().Entry(actionType)
}

// ModeledEntry is catalog.ts's modeledEntry: the entry for a modeled type,
// or nil for an unmodeled or unknown one.
func ModeledEntry(actionType string) *ModeledAction {
	if e := CatalogEntry(actionType); e != nil && e.Modeled {
		return e.Model
	}
	return nil
}

// ModeledTypes is catalog.ts's modeledTypes: every modeled type, in catalog
// order.
func ModeledTypes() []string {
	out := []string{}
	for _, a := range Catalog().Actions {
		if a.Modeled {
			out = append(out, a.Type)
		}
	}
	return out
}

func catalogErrors(actionType string) []CatalogError {
	if e := ModeledEntry(actionType); e != nil {
		return e.Transitions.Errors
	}
	return nil
}

// RequiredErrors is catalog.ts's requiredErrors: the error branches a
// document must wire on an action of this type; empty when none.
func RequiredErrors(actionType string) []string {
	out := []string{}
	for _, e := range catalogErrors(actionType) {
		if e.Required {
			out = append(out, e.Type)
		}
	}
	return out
}

// RequiredErrorsFor is catalog.ts's requiredErrorsFor: the always required
// branches and those required by a parameter the action carries
// (requiredWhenKey), in catalog order. A key is carried when it is present,

// MinConditionsFor is catalog.ts's minConditionsFor: the fewest conditions an
// action of this type must carry, 0 when the catalog sets none or the type is
// not modeled.
func MinConditionsFor(typ string) int {
	if e := ModeledEntry(typ); e != nil {
		return e.Transitions.MinConditions
	}
	return 0
}

// whatever its value, as TypeScript's `!== undefined` reads a parsed null.
func RequiredErrorsFor(actionType string, parameters jsonv.Object) []string {
	out := []string{}
	for _, e := range catalogErrors(actionType) {
		carried := false
		if e.RequiredWhenKey != "" {
			_, carried = parameters.Get(e.RequiredWhenKey)
		}
		if e.Required || carried {
			out = append(out, e.Type)
		}
	}
	return out
}

// BuilderErrors is catalog.ts's builderErrors: the branches the builder's
// modeled form wires, in its order; empty when none.
func BuilderErrors(actionType string) []string {
	out := []string{}
	for _, e := range catalogErrors(actionType) {
		if e.Builder {
			out = append(out, e.Type)
		}
	}
	return out
}

// ConditionsKindOf is catalog.ts's conditionsKind; ok is false for an
// unmodeled type.
func ConditionsKindOf(actionType string) (ConditionsKind, bool) {
	if e := ModeledEntry(actionType); e != nil {
		return e.Transitions.Conditions, true
	}
	return "", false
}

// NextRule is catalog.ts's nextRule; ok is false for an unmodeled type.
func NextRule(actionType string) (string, bool) {
	if e := ModeledEntry(actionType); e != nil {
		return e.Transitions.Next, true
	}
	return "", false
}

// HoldsParticipant is catalog.ts's holdsParticipant: whether a flow may end
// in this action with nothing wired.
func HoldsParticipant(actionType string) bool {
	e := ModeledEntry(actionType)
	return e != nil && e.Transitions.Waits
}

// TextBodyPaths is catalog.ts's textBodyPaths: paths that hold billed prompt
// text; empty for actions that play nothing.
func TextBodyPaths(actionType string) []string {
	if e := ModeledEntry(actionType); e != nil && e.TextBodies != nil {
		return e.TextBodies
	}
	return []string{}
}

// AnnouncePaths is catalog.ts's announcePaths: paths whose non-blank string
// means the participant is played something.
func AnnouncePaths(actionType string) []string {
	if e := ModeledEntry(actionType); e != nil && e.Announces != nil {
		return e.Announces
	}
	return []string{}
}

// RecordingEnablerPath is catalog.ts's recordingEnablerPath: the list whose
// non-empty value enables recording; ok is false when the action has none.
func RecordingEnablerPath(actionType string) (string, bool) {
	if e := ModeledEntry(actionType); e != nil && e.RecordingEnabler != "" {
		return e.RecordingEnabler, true
	}
	return "", false
}

// The tables below are the parts of packages/core/src/actions.ts that lint
// reads and that the catalog carries; catalog.test.ts holds each table equal
// to the catalog, so they are derived here rather than transcribed.

// FlowTypeRestrictions is actions.ts's FLOW_TYPE_RESTRICTIONS[type], read by
// the action-allowed-in-flow-type rule: the ConnectType values the action is
// legal in, in table order. ok is false for an unrestricted, unmodeled or
// unknown type, which the TypeScript table has no entry for.
func FlowTypeRestrictions(actionType string) ([]string, bool) {
	e := ModeledEntry(actionType)
	if e == nil || e.FlowTypes.Unrestricted {
		return nil, false
	}
	return e.FlowTypes.Types, true
}

// IsFlowTypeUnrestricted is membership in actions.ts's
// FLOW_TYPE_UNRESTRICTED: a modeled type whose page states no restriction.
func IsFlowTypeUnrestricted(actionType string) bool {
	e := ModeledEntry(actionType)
	return e != nil && e.FlowTypes.Unrestricted
}

// IsTerminalAction is membership in actions.ts's TERMINAL_ACTIONS, read by
// the terminal-blocks rule.
func IsTerminalAction(actionType string) bool {
	e := ModeledEntry(actionType)
	return e != nil && e.Terminal
}

// IsModeledType is membership in actions.ts's ActionType, which
// catalog.test.ts holds equal to the catalog's modeled set.
func IsModeledType(actionType string) bool {
	return ModeledEntry(actionType) != nil
}
