// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package schema validates a FlowDoc against the vendored
// conformance/schema/flowdoc-<version>.schema.json, draft 2020-12, the way
// @flow-as-code/core's tests and @flow-as-code/cli's docs.ts do with
// `new Ajv2020({ allErrors: true, strict: false })`: every violation is
// reported, not the first.
//
// Validation is santhosh-tekuri/jsonschema/v6; the violations are then
// written as Ajv writes them (instancePath, schemaPath, keyword, message), so
// a problem the provider reports reads the same as the one the CLI prints.
// Accept and reject agree with Ajv on every case the TypeScript suite runs,
// and the recorded Ajv output in testdata/ajv-oracle.json holds the error
// lists to each other. Where the two libraries differ, and why, is documented
// on ValidateVersion.
//
// Ports: packages/cli/src/docs.ts (flowDocValidator, versionOf,
// unsupportedVersion, describeSchemaError), and the schema checks of
// packages/core/src/conformance.test.ts, whose documents schema_test.go runs.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Versions is SUPPORTED_FLOWDOC_VERSIONS (packages/core/src/migrate.ts): every
// format version this build reads, oldest first, each with a schema.
var Versions = []string{"0.1", "0.2"}

// Violation is one Ajv error object (ajv's ErrorObject), the fields the
// TypeScript reads.
type Violation struct {
	// InstancePath is a JSON pointer into the document, "" for the root.
	InstancePath string
	// SchemaPath is "#" and a JSON pointer into the schema, dereferenced.
	SchemaPath string
	// Keyword is the schema keyword that failed.
	Keyword string
	// Message is Ajv's English message, byte for byte.
	Message string
	// Property is Ajv's params.additionalProperty for additionalProperties
	// and its propertyName for propertyNames, "" otherwise.
	Property string
}

// String is docs.ts's describeSchemaError: "<where> <what>", the root
// written "(root)".
func (v Violation) String() string {
	where := v.InstancePath
	if where == "" {
		where = "(root)"
	}
	return where + " " + v.Message
}

// UnsupportedVersionError is a value whose `flowdoc` field names no version in
// Versions. Its message is docs.ts's unsupportedVersion.
type UnsupportedVersionError struct {
	// Got is the field's value, nil and Present false when absent.
	Got     any
	Present bool
}

func (e *UnsupportedVersionError) Error() string {
	known := make([]string, len(Versions))
	for i, v := range Versions {
		known[i] = string(jsonv.Encode(v, ""))
	}
	got := "nothing"
	if e.Present {
		got = string(jsonv.Encode(e.Got, ""))
	}
	return fmt.Sprintf("/flowdoc must be %s, got %s", strings.Join(known, " or "), got)
}

// VersionOf is docs.ts's versionOf: the format version a value claims, when
// it is one this build reads.
func VersionOf(value any) (string, bool) {
	got, present := flowdocField(value)
	s, ok := got.(string)
	if !present || !ok {
		return "", false
	}
	for _, v := range Versions {
		if v == s {
			return s, true
		}
	}
	return "", false
}

func flowdocField(value any) (any, bool) {
	switch o := value.(type) {
	case jsonv.Object:
		return o.Get("flowdoc")
	case map[string]any:
		v, ok := o["flowdoc"]
		return v, ok
	}
	return nil, false
}

// Validate checks a value against the schema of the version its `flowdoc`
// field names, as docs.ts's flowDocProblems does before its literal-ARN
// filtering (which is lint's, not the schema's). A version this build does not
// read is an *UnsupportedVersionError.
//
// value is a jsonv value (what jsonv.Decode returns) or the equivalent built
// from map[string]any, float64 or json.Number, and the other encoding/json
// types.
func Validate(value any) ([]Violation, error) {
	version, ok := VersionOf(value)
	if !ok {
		got, present := flowdocField(value)
		return nil, &UnsupportedVersionError{Got: got, Present: present}
	}
	return ValidateVersion(version, value)
}

// ValidateJSON decodes b as JSON.parse does (jsonv.Decode) and validates it
// with Validate.
func ValidateJSON(b []byte) ([]Violation, error) {
	v, err := jsonv.Decode(b)
	if err != nil {
		return nil, err
	}
	return Validate(v)
}

// ValidateVersion checks value against flowdoc-<version>.schema.json and
// returns every violation, none when the value is valid. The error is for a
// version with no schema or a value that is not JSON data.
//
// The violations are Ajv's error objects for the same input (same place,
// schema path, keyword, message and property, as many times as Ajv reports
// each), with these differences, each held by a test against the recorded
// Ajv output in testdata/ajv-oracle.json:
//
//   - Order. Ajv reports in the order its generated code runs; this reports
//     in document order of InstancePath (object members as the document
//     orders them, array elements by index), then SchemaPath, then the
//     Property's position in its object, then Message.
//   - Fewer errors where a keyword fails early. santhosh-tekuri stops
//     evaluating a subschema once `type`, `const` or `enum` fails; Ajv with
//     allErrors goes on to that subschema's other keywords. The value is
//     rejected either way; only Ajv's extra errors in that subschema are
//     missing here. No FlowDoc schema pairs those keywords with one a value
//     of the wrong type can fail, so on FlowDocs the lists are equal.
//   - multipleOf. Ajv divides in doubles and compares with parseInt, so it
//     rejects 1e21 against 0.5 and 0.3 against 0.1; santhosh-tekuri divides
//     exactly and accepts both. A limit (minimum and the like) that is not
//     exactly a double can compare differently too. The FlowDoc schemas have
//     no multipleOf and only integer limits.
//   - Patterns. A pattern this package cannot translate to RE2 with the same
//     meaning (see compileECMA) fails the schema's compilation instead of
//     validating differently; the FlowDoc schemas' patterns all translate.
//
// The `if` ("must match "then" schema"), propertyNames, and passing-twice
// oneOf errors santhosh-tekuri does not produce as Ajv does are rebuilt here.
func ValidateVersion(version string, value any) ([]Violation, error) {
	c, err := compiled(version)
	if err != nil {
		return nil, err
	}
	return c.validate(value)
}

func (c *compiledSchema) validate(value any) ([]Violation, error) {
	ordered, err := toOrdered(value)
	if err != nil {
		return nil, err
	}
	verr := c.root.Validate(toPlain(ordered))
	if verr == nil {
		return nil, nil
	}
	ve, ok := verr.(*jsonschema.ValidationError)
	if !ok {
		return nil, verr
	}
	f := flattener{c: c, doc: ordered}
	if err := f.walk(ve, parentCtx{}); err != nil {
		return nil, err
	}
	sortViolations(f.out, ordered)
	return f.out, nil
}

// ValidateVersionJSON is ValidateVersion over JSON text, decoded as
// JSON.parse does.
func ValidateVersionJSON(version string, b []byte) ([]Violation, error) {
	v, err := jsonv.Decode(b)
	if err != nil {
		return nil, err
	}
	return ValidateVersion(version, v)
}

// --- compilation

type compiledSchema struct {
	id       string
	raw      any // the schema as jsonv decoded it, for Ajv's message values
	compiler *jsonschema.Compiler
	root     *jsonschema.Schema

	mu  sync.Mutex
	sub map[string]*jsonschema.Schema // propertyNames subschemas by location
}

var (
	compileMu sync.Mutex
	schemas   = map[string]*compiledSchema{}
)

func compiled(version string) (*compiledSchema, error) {
	compileMu.Lock()
	defer compileMu.Unlock()
	if c, ok := schemas[version]; ok {
		return c, nil
	}
	known := false
	for _, v := range Versions {
		known = known || v == version
	}
	if !known {
		return nil, fmt.Errorf("schema: no FlowDoc schema for version %q", version)
	}
	b, err := fs.ReadFile(conformance.FS(), "schema/flowdoc-"+version+".schema.json")
	if err != nil {
		return nil, err
	}
	c, err := compileSchema(b)
	if err != nil {
		return nil, fmt.Errorf("schema: flowdoc-%s.schema.json: %w", version, err)
	}
	schemas[version] = c
	return c, nil
}

func compileSchema(b []byte) (*compiledSchema, error) {
	raw, err := jsonv.Decode(b)
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(jsonv.Object)
	if !ok {
		return nil, fmt.Errorf("not a JSON object")
	}
	idv, _ := obj.Get("$id")
	id, _ := idv.(string)
	if id == "" {
		id = "urn:flowascode:schema"
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	comp := jsonschema.NewCompiler()
	comp.DefaultDraft(jsonschema.Draft2020)
	comp.UseRegexpEngine(compileECMA)
	if err := comp.AddResource(id, doc); err != nil {
		return nil, err
	}
	root, err := comp.Compile(id)
	if err != nil {
		return nil, err
	}
	return &compiledSchema{id: id, raw: raw, compiler: comp, root: root, sub: map[string]*jsonschema.Schema{}}, nil
}

// subschema compiles the schema at an absolute location of this resource.
func (c *compiledSchema) subschema(loc string) (*jsonschema.Schema, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.sub[loc]; ok {
		return s, nil
	}
	s, err := c.compiler.Compile(loc)
	if err != nil {
		return nil, err
	}
	c.sub[loc] = s
	return s, nil
}

// fragment is the JSON pointer part of an absolute schema location of this
// resource, unescaped from its URL form.
func (c *compiledSchema) fragment(loc string) string {
	_, frag, _ := strings.Cut(loc, "#")
	if u, err := url.PathUnescape(frag); err == nil {
		return u
	}
	return frag
}

// at is the raw schema value at a JSON pointer.
func (c *compiledSchema) at(ptr string) any {
	v := c.raw
	if ptr == "" {
		return v
	}
	for _, tok := range strings.Split(ptr[1:], "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		switch n := v.(type) {
		case jsonv.Object:
			v, _ = n.Get(tok)
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(n) {
				return nil
			}
			v = n[i]
		default:
			return nil
		}
	}
	return v
}

func (c *compiledSchema) keywordValue(schemaPtr, kw string) any {
	o, _ := c.at(schemaPtr).(jsonv.Object)
	v, _ := o.Get(kw)
	return v
}

// --- instance values

// toOrdered brings a value to jsonv's form: Object for objects (a Go map's
// keys in LessUTF16 order, since it has no order of its own), float64 for
// numbers.
func toOrdered(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, string, float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case json.Number:
		return strconv.ParseFloat(string(t), 64)
	case jsonv.Object:
		out := make(jsonv.Object, len(t))
		for i, m := range t {
			mv, err := toOrdered(m.Value)
			if err != nil {
				return nil, err
			}
			out[i] = jsonv.Member{Key: m.Key, Value: mv}
		}
		return out, nil
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return jsonv.LessUTF16(keys[i], keys[j]) })
		out := make(jsonv.Object, len(keys))
		for i, k := range keys {
			mv, err := toOrdered(t[k])
			if err != nil {
				return nil, err
			}
			out[i] = jsonv.Member{Key: k, Value: mv}
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			ev, err := toOrdered(e)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	}
	return nil, fmt.Errorf("schema: %T is not a JSON value", v)
}

// toPlain is the form santhosh-tekuri validates: map[string]any objects.
func toPlain(v any) any {
	switch t := v.(type) {
	case jsonv.Object:
		m := make(map[string]any, len(t))
		for _, mem := range t {
			m[mem.Key] = toPlain(mem.Value)
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = toPlain(e)
		}
		return out
	}
	return v
}

// valueAt is the ordered value at an instance location.
func valueAt(v any, loc []string) any {
	for _, tok := range loc {
		switch n := v.(type) {
		case jsonv.Object:
			v, _ = n.Get(tok)
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(n) {
				return nil
			}
			v = n[i]
		default:
			return nil
		}
	}
	return v
}

func pointer(loc []string) string {
	var b strings.Builder
	for _, tok := range loc {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(tok, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// --- flattening santhosh-tekuri's error tree into Ajv's error list

// flattener walks a santhosh-tekuri error tree and writes Ajv's error list.
type flattener struct {
	c   *compiledSchema
	doc any
	out []Violation
	// prefix is the instance location of the value a sub-validation ran on
	// (a oneOf branch), which its error locations are relative to.
	prefix []string
	// override, when set, replaces every instance location: the errors of a
	// property name, which Ajv reports at the object.
	override []string
	// property is the name under validation inside propertyNames.
	property string
	ref      refContext
	// used counts the objects already matched to a propertyNames error.
	used map[string]bool
}

// parentCtx is what a node knows of the node above it.
type parentCtx struct {
	url  string   // its SchemaURL; "" above a tree's root
	base string   // the schema fragment this node's location is reached from
	loc  []string // its instance location
}

// refContext is where Ajv's schemaPath restarts after the last $ref crossed.
// Ajv inlines a $ref whose target holds no $ref of its own and then writes
// paths under the $ref as written ("#/$defs/token/pattern"); any other target
// it compiles as a function of its own, whose paths start again at "#"
// ("#/allOf/0/if" inside #/$defs/action). compile/resolve.js inlineRef and
// vocabularies/core/ref.js.
//
// A $ref whose target is itself nothing but a $ref is followed through at
// resolution (compile/index.js getJsonPointer), so Ajv never sees the inner
// hop: its paths keep the outer $ref as written. through holds the locations
// of such hops, whose Reference nodes change nothing here.
type refContext struct {
	target  string // the target's fragment
	prefix  string // what Ajv writes in its place; "" before any $ref
	through map[string]bool
}

// ajvRuleKeywords are the keywords Ajv 2020 compiles code for, the ones
// util.js schemaHasRulesButRef counts; annotations ($defs, title,
// description and the like) are not among them.
var ajvRuleKeywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`type enum const multipleOf maximum exclusiveMaximum
		minimum exclusiveMinimum maxLength minLength pattern maxItems minItems
		uniqueItems maxContains minContains maxProperties minProperties required
		dependentRequired dependencies allOf anyOf oneOf not if then else
		dependentSchemas prefixItems items additionalItems contains properties
		patternProperties additionalProperties propertyNames unevaluatedItems
		unevaluatedProperties format $dynamicRef $dynamicAnchor $recursiveRef
		$recursiveAnchor discriminator`) {
		ajvRuleKeywords[k] = true
	}
}

// followPureRefs resolves a local $ref target the way Ajv does: while the
// target holds a $ref and no other rule, it is replaced by that $ref's
// target. It returns the final target and the hops passed through.
func (c *compiledSchema) followPureRefs(frag string) (string, map[string]bool) {
	hops := map[string]bool{}
	for !hops[frag] {
		o, ok := c.at(frag).(jsonv.Object)
		if !ok {
			break
		}
		refV, has := o.Get("$ref")
		ref, isStr := refV.(string)
		if !has || !isStr || !strings.HasPrefix(ref, "#") {
			break
		}
		pure := true
		for _, m := range o {
			if m.Key != "$ref" && ajvRuleKeywords[m.Key] {
				pure = false
			}
		}
		if !pure {
			break
		}
		hops[frag] = true
		next, err := url.PathUnescape(ref[1:])
		if err != nil {
			break
		}
		frag = next
	}
	return frag, hops
}

// ajvPath is a schema fragment as Ajv's schemaPath writes it.
func (f *flattener) ajvPath(frag string) string {
	if f.ref.prefix == "" {
		return "#" + frag
	}
	return f.ref.prefix + strings.TrimPrefix(frag, f.ref.target)
}

// hasRef is resolve.js's hasRef: a $ref-like key anywhere inside.
func hasRef(v any) bool {
	switch n := v.(type) {
	case jsonv.Object:
		for _, m := range n {
			switch m.Key {
			case "$ref", "$recursiveRef", "$recursiveAnchor", "$dynamicRef", "$dynamicAnchor":
				return true
			}
			if hasRef(m.Value) {
				return true
			}
		}
	case []any:
		for _, e := range n {
			if hasRef(e) {
				return true
			}
		}
	}
	return false
}

// locOf is a node's instance location in the whole document.
func (f *flattener) locOf(e *jsonschema.ValidationError) []string {
	if f.override != nil {
		return f.override
	}
	return append(append([]string{}, f.prefix...), e.InstanceLocation...)
}

// node is one error being written: the error, its schema fragment, and its
// instance location.
type node struct {
	e    *jsonschema.ValidationError
	frag string
	loc  []string
}

func (f *flattener) add(n node, keyword, message, property string) {
	if property == "" {
		property = f.property
	}
	f.out = append(f.out, Violation{
		InstancePath: pointer(n.loc),
		SchemaPath:   f.ajvPath(n.frag) + "/" + keyword,
		Keyword:      keyword,
		Message:      message,
		Property:     property,
	})
}

func (f *flattener) walk(e *jsonschema.ValidationError, p parentCtx) error {
	n := node{e: e, frag: f.c.fragment(e.SchemaURL), loc: f.locOf(e)}
	if pn, ok := e.ErrorKind.(*kind.PropertyNames); ok && f.override == nil {
		n.loc = f.locate(n, pn.Property, p)
	}
	if err := f.node(n, p); err != nil {
		return err
	}
	f.clauses(n, p)
	return nil
}

// locate finds the object a propertyNames error is about. santhosh-tekuri
// stores that error's instance location without copying it, so later
// siblings overwrite it; only its length survives. The object is found again
// from the parent's location by following the schema path from the parent
// down to the propertyNames keyword: a `properties` step names its key, any
// other step that descends is tried on every member or element. Of the
// objects of the right depth that hold the name, the first not yet claimed
// by another error of the same schema and name is this one.
func (f *flattener) locate(n node, name string, p parentCtx) []string {
	obj := strings.TrimSuffix(n.frag, "/propertyNames")
	if f.used == nil {
		f.used = map[string]bool{}
	}
	depth := len(f.prefix) + len(n.e.InstanceLocation)
	if !strings.HasPrefix(obj, p.base) {
		return n.loc
	}
	for _, cand := range f.expand([][]string{p.loc}, splitPointer(strings.TrimPrefix(obj, p.base))) {
		if len(cand) != depth {
			continue
		}
		o, ok := valueAt(f.doc, cand).(jsonv.Object)
		if !ok {
			continue
		}
		if _, has := o.Get(name); !has {
			continue
		}
		k := n.e.SchemaURL + "\x00" + name + "\x00" + pointer(cand)
		if f.used[k] {
			continue
		}
		f.used[k] = true
		return cand
	}
	return n.loc
}

// expand follows schema path tokens through the instance from each location.
func (f *flattener) expand(locs [][]string, toks []string) [][]string {
	each := func(fn func(loc []string, v any) [][]string) {
		var next [][]string
		for _, loc := range locs {
			next = append(next, fn(loc, valueAt(f.doc, loc))...)
		}
		locs = next
	}
	child := func(loc []string, tok string) []string {
		return append(append([]string{}, loc...), tok)
	}
	members := func(loc []string, v any) [][]string {
		var out [][]string
		if o, ok := v.(jsonv.Object); ok {
			for _, m := range o {
				out = append(out, child(loc, m.Key))
			}
		}
		return out
	}
	elements := func(loc []string, v any) [][]string {
		var out [][]string
		if a, ok := v.([]any); ok {
			for i := range a {
				out = append(out, child(loc, strconv.Itoa(i)))
			}
		}
		return out
	}
	for i := 0; i < len(toks); {
		switch kw := toks[i]; kw {
		case "properties", "prefixItems":
			if i+1 >= len(toks) {
				return nil
			}
			key := toks[i+1]
			each(func(loc []string, v any) [][]string {
				if !hasChild(v, key) {
					return nil
				}
				return [][]string{child(loc, key)}
			})
			i += 2
		case "patternProperties":
			each(members)
			i += 2
		case "additionalProperties", "unevaluatedProperties":
			each(members)
			i++
		case "items", "contains", "unevaluatedItems":
			each(elements)
			i++
		default:
			_, args := keywordShape(kw)
			i += 1 + args
		}
	}
	return locs
}

// hasChild reports whether an object has the member, or an array the index.
func hasChild(v any, tok string) bool {
	switch n := v.(type) {
	case jsonv.Object:
		_, ok := n.Get(tok)
		return ok
	case []any:
		i, err := strconv.Atoi(tok)
		return err == nil && i >= 0 && i < len(n)
	}
	return false
}

// clauses adds Ajv's `must match "then" schema` (or "else") error for each
// failing then or else clause this node is the first error of.
//
// santhosh-tekuri has no error of its own for a failing clause: the clause's
// errors take its place in the tree, and when there is only one it is that
// error itself, possibly several properties deeper. So a node enters a clause
// when its schema location lies inside <base>/then (or /else), <base> holding
// an `if`, and its parent's does not. The if-error belongs at the instance
// <base> was evaluated at: the node's instance location less the steps the
// schema path from the clause to the node descends.
func (f *flattener) clauses(n node, p parentCtx) {
	toks := splitPointer(n.frag)
	type clause struct {
		at    int // index of the then/else token
		steps int // instance steps taken after it
	}
	var found []clause
	for i := 0; i < len(toks); {
		kw := toks[i]
		if kw == "then" || kw == "else" {
			found = append(found, clause{at: i})
			i++
			continue
		}
		step, args := keywordShape(kw)
		for c := range found {
			found[c].steps += step
		}
		i += 1 + args
	}
	parentFrag := f.c.fragment(p.url)
	for _, c := range found {
		base := pointer(toks[:c.at])
		prefix := pointer(toks[:c.at+1])
		if p.url != "" && (parentFrag == prefix || strings.HasPrefix(parentFrag, prefix+"/")) {
			continue
		}
		o, ok := f.c.at(base).(jsonv.Object)
		if !ok {
			continue
		}
		if _, hasIf := o.Get("if"); !hasIf {
			continue
		}
		at := n.loc
		if f.override == nil && c.steps <= len(at) {
			at = at[:len(at)-c.steps]
		}
		f.out = append(f.out, Violation{
			InstancePath: pointer(at),
			SchemaPath:   f.ajvPath(base) + "/if",
			Keyword:      "if",
			Message:      `must match "` + toks[c.at] + `" schema`,
			Property:     f.property,
		})
	}
}

// keywordShape is how an applicator keyword in a schema location moves
// through the instance: step is the instance levels it descends, args the
// location tokens it takes after itself (a property name, an index).
func keywordShape(kw string) (step, args int) {
	switch kw {
	case "properties", "patternProperties", "prefixItems":
		return 1, 1
	case "additionalProperties", "items", "unevaluatedProperties", "unevaluatedItems", "contains":
		return 1, 0
	case "allOf", "anyOf", "oneOf", "dependentSchemas", "$defs", "definitions":
		return 0, 1
	}
	return 0, 0 // not, if, then, else, propertyNames
}

func (f *flattener) causes(n node, base string) error {
	p := parentCtx{url: n.e.SchemaURL, base: base, loc: n.loc}
	for _, c := range n.e.Causes {
		if err := f.walk(c, p); err != nil {
			return err
		}
	}
	return nil
}

func (f *flattener) node(n node, p parentCtx) error {
	num := func(kw string) string {
		if v, ok := f.c.keywordValue(n.frag, kw).(float64); ok {
			return jsonv.FormatNumber(v)
		}
		return fmt.Sprint(f.c.keywordValue(n.frag, kw))
	}
	switch k := n.e.ErrorKind.(type) {
	case *kind.Schema, *kind.Group, *kind.AllOf:
		return f.causes(n, n.frag)
	case *kind.Reference:
		target := f.c.fragment(k.URL)
		if f.ref.through[n.frag] {
			return f.causes(n, target)
		}
		final, hops := f.c.followPureRefs(target)
		next := refContext{target: final, prefix: "#", through: hops}
		if !hasRef(f.c.at(final)) {
			written, _ := f.c.keywordValue(n.frag, k.Keyword).(string)
			next.prefix = written
		}
		saved := f.ref
		f.ref = next
		err := f.causes(n, target)
		f.ref = saved
		return err
	case *kind.AnyOf:
		if err := f.causes(n, n.frag); err != nil {
			return err
		}
		f.add(n, "anyOf", "must match a schema in anyOf", "")
	case *kind.OneOf:
		if k.Subschemas == nil {
			if err := f.causes(n, n.frag); err != nil {
				return err
			}
		} else if err := f.oneOfBranches(n); err != nil {
			return err
		}
		f.add(n, "oneOf", "must match exactly one schema in oneOf", "")
	case *kind.Not:
		f.add(n, "not", "must NOT be valid", "")
	case *kind.PropertyNames:
		return f.propertyName(n, k.Property)
	case *kind.FalseSchema:
		f.add(n, "false schema", "boolean schema is false", "")
	case *kind.Type:
		var s string
		switch w := f.c.keywordValue(n.frag, "type").(type) {
		case string:
			s = w
		case []any:
			parts := make([]string, len(w))
			for i, t := range w {
				parts[i] = fmt.Sprint(t)
			}
			s = strings.Join(parts, ",")
		}
		f.add(n, "type", "must be "+s, "")
	case *kind.Const:
		f.add(n, "const", "must be equal to constant", "")
	case *kind.Enum:
		f.add(n, "enum", "must be equal to one of the allowed values", "")
	case *kind.Required:
		for _, prop := range k.Missing {
			f.add(n, "required", "must have required property '"+prop+"'", "")
		}
	case *kind.AdditionalProperties:
		// One error per property, in the object's order.
		obj, _ := valueAt(f.doc, n.loc).(jsonv.Object)
		extra := map[string]bool{}
		for _, prop := range k.Properties {
			extra[prop] = true
		}
		for _, m := range obj {
			if extra[m.Key] {
				f.add(n, "additionalProperties", "must NOT have additional properties", m.Key)
			}
		}
	case *kind.MinLength:
		f.add(n, "minLength", "must NOT have fewer than "+num("minLength")+" characters", "")
	case *kind.MaxLength:
		f.add(n, "maxLength", "must NOT have more than "+num("maxLength")+" characters", "")
	case *kind.MinItems:
		f.add(n, "minItems", "must NOT have fewer than "+num("minItems")+" items", "")
	case *kind.MaxItems:
		f.add(n, "maxItems", "must NOT have more than "+num("maxItems")+" items", "")
	case *kind.MinProperties:
		f.add(n, "minProperties", "must NOT have fewer than "+num("minProperties")+" properties", "")
	case *kind.MaxProperties:
		f.add(n, "maxProperties", "must NOT have more than "+num("maxProperties")+" properties", "")
	case *kind.Minimum:
		f.add(n, "minimum", "must be >= "+num("minimum"), "")
	case *kind.Maximum:
		f.add(n, "maximum", "must be <= "+num("maximum"), "")
	case *kind.ExclusiveMinimum:
		f.add(n, "exclusiveMinimum", "must be > "+num("exclusiveMinimum"), "")
	case *kind.ExclusiveMaximum:
		f.add(n, "exclusiveMaximum", "must be < "+num("exclusiveMaximum"), "")
	case *kind.MultipleOf:
		f.add(n, "multipleOf", "must be multiple of "+num("multipleOf"), "")
	case *kind.Pattern:
		pat, _ := f.c.keywordValue(n.frag, "pattern").(string)
		f.add(n, "pattern", `must match pattern "`+pat+`"`, "")
	case *kind.UniqueItems:
		arr, _ := valueAt(f.doc, n.loc).([]any)
		if i, j, found := ajvDuplicate(arr, f.c.keywordValue(n.frag, "items")); found {
			f.add(n, "uniqueItems", fmt.Sprintf("must NOT have duplicate items (items ## %d and %d are identical)", j, i), "")
		}
	default:
		return fmt.Errorf("schema: no Ajv form for a %T error at %s", n.e.ErrorKind, n.e.SchemaURL)
	}
	return nil
}

// oneOfBranches writes the errors of the failing branches of a oneOf that
// failed because more than one branch passed. Ajv's generated code
// (vocabularies/applicator/oneOf.js) evaluates branches in order until a
// second one passes, nesting the rest in an else it never enters, and keeps
// the errors of every failing branch it evaluated, including those between
// the first and second passing ones. santhosh-tekuri keeps none of them, so
// the branches are validated again, in order, on their own.
func (f *flattener) oneOfBranches(n node) error {
	branches, _ := f.c.keywordValue(n.frag, "oneOf").([]any)
	value := toPlain(valueAt(f.doc, n.loc))
	savedPrefix := f.prefix
	defer func() { f.prefix = savedPrefix }()
	passed := 0
	for i := range branches {
		if passed == 2 {
			break
		}
		sub, err := f.c.subschema(n.e.SchemaURL + "/oneOf/" + strconv.Itoa(i))
		if err != nil {
			return err
		}
		verr, ok := sub.Validate(value).(*jsonschema.ValidationError)
		if !ok {
			passed++
			continue
		}
		f.prefix = n.loc
		if err := f.walk(verr, parentCtx{url: n.e.SchemaURL, base: n.frag, loc: n.loc}); err != nil {
			return err
		}
	}
	return nil
}

// propertyName reports a property name that fails propertyNames as Ajv does:
// the subschema's own errors at the object, then "property name must be
// valid". santhosh-tekuri keeps only the latter when there is one inner
// error, so the name is validated again against the subschema alone.
func (f *flattener) propertyName(n node, name string) error {
	sub, err := f.c.subschema(n.e.SchemaURL)
	if err != nil {
		return err
	}
	inner := flattener{c: f.c, doc: f.doc, override: n.loc, property: name, ref: f.ref}
	if verr, ok := sub.Validate(name).(*jsonschema.ValidationError); ok {
		if err := inner.walk(verr, parentCtx{url: n.e.SchemaURL, base: n.frag, loc: n.loc}); err != nil {
			return err
		}
	}
	f.out = append(f.out, inner.out...)
	f.out = append(f.out, Violation{
		InstancePath: pointer(n.loc),
		SchemaPath:   f.ajvPath(n.frag),
		Keyword:      "propertyNames",
		Message:      "property name must be valid",
		Property:     name,
	})
	return nil
}

// ajvDuplicate is Ajv's uniqueItems search (vocabularies/validation/
// uniqueItems.js), which decides which pair a message names. When the items
// schema declares only scalar types it scans from the end keeping the last
// index of each value, skipping items of another type (loopN); otherwise it
// compares every pair from the end with deep equality (loopN2).
func ajvDuplicate(arr []any, items any) (i, j int, found bool) {
	var types []string
	if o, ok := items.(jsonv.Object); ok {
		tv, _ := o.Get("type")
		switch tt := tv.(type) {
		case string:
			types = []string{tt}
		case []any:
			for _, x := range tt {
				if s, ok := x.(string); ok {
					types = append(types, s)
				}
			}
		}
	}
	optimize := len(types) > 0
	for _, t := range types {
		if t == "object" || t == "array" {
			optimize = false
		}
	}
	if optimize {
		indices := map[string]int{}
		for i := len(arr) - 1; i >= 0; i-- {
			item := arr[i]
			if !ofTypes(item, types) {
				continue
			}
			key := jsKey(item)
			if s, ok := item.(string); ok && len(types) > 1 {
				key = s + "_"
			}
			if j, ok := indices[key]; ok {
				return i, j, true
			}
			indices[key] = i
		}
		return 0, 0, false
	}
	for i := len(arr) - 1; i >= 0; i-- {
		for j := i - 1; j >= 0; j-- {
			if deepEqual(arr[i], arr[j]) {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// ofTypes is Ajv's checkDataTypes with strictNumbers off.
func ofTypes(v any, types []string) bool {
	for _, t := range types {
		switch t {
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "number":
			if _, ok := v.(float64); ok {
				return true
			}
		case "integer":
			if n, ok := v.(float64); ok && n == math.Trunc(n) {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "null":
			if v == nil {
				return true
			}
		}
	}
	return false
}

// jsKey is the property key JavaScript makes of a scalar used as an index.
func jsKey(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return jsonv.FormatNumber(t)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return "null"
	}
	return fmt.Sprint(v)
}

// deepEqual is fast-deep-equal on JSON values: objects equal as key sets.
func deepEqual(a, b any) bool {
	switch x := a.(type) {
	case jsonv.Object:
		y, ok := b.(jsonv.Object)
		if !ok || len(x) != len(y) {
			return false
		}
		for _, m := range x {
			yv, ok := y.Get(m.Key)
			if !ok || !deepEqual(m.Value, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// --- ordering

// sortViolations puts violations in document order of their instance path,
// then schema path, then the named property's position, then message.
func sortViolations(vs []Violation, doc any) {
	type key struct {
		path []int
		prop int
	}
	keys := make(map[int]key, len(vs))
	for i, v := range vs {
		p := docOrder(doc, v.InstancePath)
		prop := -1
		if v.Property != "" {
			if o, ok := valueAtPointer(doc, v.InstancePath).(jsonv.Object); ok {
				for idx, m := range o {
					if m.Key == v.Property {
						prop = idx
					}
				}
			}
		}
		keys[i] = key{p, prop}
	}
	idx := make([]int, len(vs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ka, kb := keys[idx[a]], keys[idx[b]]
		for i := 0; i < len(ka.path) && i < len(kb.path); i++ {
			if ka.path[i] != kb.path[i] {
				return ka.path[i] < kb.path[i]
			}
		}
		if len(ka.path) != len(kb.path) {
			return len(ka.path) < len(kb.path)
		}
		va, vb := vs[idx[a]], vs[idx[b]]
		if va.SchemaPath != vb.SchemaPath {
			return va.SchemaPath < vb.SchemaPath
		}
		if ka.prop != kb.prop {
			return ka.prop < kb.prop
		}
		return va.Message < vb.Message
	})
	sorted := make([]Violation, len(vs))
	for i, j := range idx {
		sorted[i] = vs[j]
	}
	copy(vs, sorted)
}

func splitPointer(p string) []string {
	if p == "" {
		return nil
	}
	toks := strings.Split(p[1:], "/")
	for i, t := range toks {
		toks[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return toks
}

func valueAtPointer(doc any, p string) any { return valueAt(doc, splitPointer(p)) }

// docOrder is the position of each step of a path within its parent.
func docOrder(doc any, p string) []int {
	var out []int
	v := doc
	for _, tok := range splitPointer(p) {
		pos := -1
		switch n := v.(type) {
		case jsonv.Object:
			for i, m := range n {
				if m.Key == tok {
					pos = i
					v = m.Value
				}
			}
		case []any:
			if i, err := strconv.Atoi(tok); err == nil && i >= 0 && i < len(n) {
				pos = i
				v = n[i]
			}
		}
		out = append(out, pos)
	}
	return out
}
