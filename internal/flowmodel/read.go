// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowmodel

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/lint"
)

// Phase is when the configuration is read. In PhaseValidate every reference
// and variable may still be unknown, so an unknown value is not an error; in
// PhasePlan an unknown value in a reference field means an expression the
// field cannot hold (rule 21).
type Phase int

const (
	PhaseValidate Phase = iota
	PhasePlan
)

// Problem is one refusal, carrying the contract's error code
// (conformance/hcl/README.md, "Error codes") and the attribute's path in the
// contract's spelling (action[<id>].<block>.<attr>).
type Problem struct {
	Code   string
	Path   string
	Attr   path.Path
	Detail string
}

// Summary is what a diagnostic's summary says: the code first, as the
// contract requires.
func (p Problem) Summary() string { return p.Code }

// Result is a configuration read to a FlowDoc.
type Result struct {
	// Doc is nil when a value it needs is unknown, or a problem stopped it.
	Doc jsonv.Object
	// Refs maps each key in the refs attribute to its value: a string, nil
	// for an explicit null, or Unknown.
	Refs        map[string]any
	LintDisable []string
	Problems    []Problem
	// Incomplete is true when an unknown value kept Doc from being built.
	Incomplete bool
}

// RefKeyPattern is the reference key (contract.ts REF_KEY).
var RefKeyPattern = regexp.MustCompile(`^(queue|hours|lambda|lex|prompt|flow|module|view):([a-z0-9]+(?:-[a-z0-9]+)*)(?:@([a-z0-9]+(?:-[a-z0-9]+)*))?$`)

type reader struct {
	phase      Phase
	problems   []Problem
	incomplete bool
}

func (r *reader) fail(code, p string, attr path.Path, format string, a ...any) {
	r.problems = append(r.problems, Problem{Code: code, Path: p, Attr: attr, Detail: fmt.Sprintf(format, a...)})
}

// FromConfig reads a flowascode_contact_flow (kind "flow") or
// flowascode_contact_flow_module (kind "module") configuration, decoded with
// Decode, to its FlowDoc (rules 18 to 21, the provider's half).
func FromConfig(cfg map[string]any, kind string, phase Phase) Result {
	r := &reader{phase: phase}
	res := Result{Refs: map[string]any{}}

	name, nameOK := r.str(cfg["name"], "name", path.Root("name"))
	if nameOK && !flowdoc.SlugPattern.MatchString(name) {
		r.fail("NON_LITERAL_VALUE", "name", path.Root("name"), "name must be a slug, got %q.", name)
	}
	connectType := "MODULE"
	if kind == "flow" {
		if cfg["settings"] != nil {
			r.fail("FLOW_WITH_SETTINGS", "settings", path.Root("settings"), "Only a module resource has settings.")
		}
		connectType, _ = r.str(cfg["type"], "type", path.Root("type"))
	} else if cfg["type"] != nil {
		r.fail("MODULE_WITH_TYPE", "type", path.Root("type"), "A module resource has no type.")
	}

	if refs, ok := cfg["refs"].(map[string]any); ok {
		for _, key := range sortedKeys(refs) {
			p := fmt.Sprintf("refs[%s]", key)
			if !RefKeyPattern.MatchString(key) {
				r.fail("REF_KEY_MALFORMED", p, path.Root("refs").AtMapKey(key), "%q is not a reference key such as \"queue:front-desk\".", key)
			}
			res.Refs[key] = refs[key]
		}
	} else if _, unknown := cfg["refs"].(Unknown); unknown {
		r.incomplete = true
	}

	if l, ok := cfg["lint"].(map[string]any); ok {
		if list, ok := l["disable"].([]any); ok {
			hard := map[string]bool{}
			known := map[string]bool{}
			for _, rule := range lint.AllRules() {
				known[rule.ID] = true
				hard[rule.ID] = rule.Hard
			}
			for i, v := range list {
				p := fmt.Sprintf("lint.disable[%d]", i)
				attr := path.Root("lint").AtName("disable").AtListIndex(i)
				id, ok := v.(string)
				switch {
				case !ok:
					if _, unknown := v.(Unknown); !unknown {
						r.fail("UNKNOWN_LINT_RULE", p, attr, "lint.disable entries are rule ids.")
					}
				case !known[id]:
					r.fail("UNKNOWN_LINT_RULE", p, attr, "%q is not a lint rule id.", id)
				case hard[id]:
					r.fail("UNKNOWN_LINT_RULE", p, attr, "%q is a hard rule and cannot be disabled.", id)
				default:
					res.LintDisable = append(res.LintDisable, id)
				}
			}
		}
	}

	var actions []any
	positions := map[string]flowdoc.Point{}
	seen := map[string]bool{}
	var ids []string
	if list, ok := cfg["action"].([]any); ok {
		for i, raw := range list {
			a, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			action, id, pos := r.action(a, i, seen)
			if action == nil {
				continue
			}
			actions = append(actions, action)
			ids = append(ids, id)
			if pos != nil {
				positions[id] = *pos
			}
		}
	}

	start := ""
	if len(ids) > 0 {
		start = ids[0]
	}
	if s, ok := cfg["start"].(string); ok {
		start = s
		if !seen[s] {
			r.fail("START_UNKNOWN", "start", path.Root("start"), "start names no action: %q.", s)
		}
	} else if _, unknown := cfg["start"].(Unknown); unknown {
		r.incomplete = true
	}

	var settings any
	if kind == "module" {
		settings = jsonv.Object{}
		if s, ok := cfg["settings"].(string); ok {
			v, err := jsonv.Decode([]byte(s))
			if err != nil {
				r.fail("NON_LITERAL_VALUE", "settings", path.Root("settings"), "settings must be jsonencode({...}): %v.", err)
			} else if o, ok := v.(jsonv.Object); ok {
				settings = o
			} else {
				r.fail("NON_LITERAL_VALUE", "settings", path.Root("settings"), "settings must encode an object.")
			}
		} else if _, unknown := cfg["settings"].(Unknown); unknown {
			r.incomplete = true
		}
	}

	res.Problems = r.problems
	res.Incomplete = r.incomplete
	if r.incomplete || len(r.problems) > 0 || !nameOK {
		return res
	}

	content := jsonv.Object{
		{Key: "Version", Value: flowdoc.FlowLanguageVersion},
		{Key: "StartAction", Value: start},
	}
	if settings != nil {
		content = append(content, jsonv.Member{Key: "Settings", Value: settings})
	}
	content = append(content, jsonv.Member{Key: "Actions", Value: actions})

	layout := flowdoc.AutoLayout(actions, &start)
	for id, p := range positions {
		layout[id] = p
	}
	refs := []any{}
	for _, e := range flowdoc.CollectRefs(content) {
		o := jsonv.Object{{Key: "token", Value: e.Token}, {Key: "type", Value: e.Type}, {Key: "name", Value: e.Name}}
		if e.Alias != "" {
			o = append(o, jsonv.Member{Key: "alias", Value: e.Alias})
		}
		refs = append(refs, o)
	}
	doc := jsonv.Object{
		{Key: "flowdoc", Value: flowdoc.Version},
		{Key: "kind", Value: kind},
		{Key: "name", Value: name},
	}
	if d, ok := cfg["description"].(string); ok {
		doc = append(doc, jsonv.Member{Key: "description", Value: d})
	} else if _, unknown := cfg["description"].(Unknown); unknown {
		res.Incomplete = true
		return res
	}
	doc = append(doc,
		jsonv.Member{Key: "connectType", Value: connectType},
		jsonv.Member{Key: "content", Value: content},
		jsonv.Member{Key: "layout", Value: flowdoc.LayoutJSON(actions, layout)},
		jsonv.Member{Key: "refs", Value: refs},
	)
	res.Doc = doc
	return res
}

// action reads one action block. It returns the Flow language action, its id
// and its explicit position, or a nil action when a problem stopped it.
func (r *reader) action(a map[string]any, i int, seen map[string]bool) (jsonv.Object, string, *flowdoc.Point) {
	base := path.Root("action").AtListIndex(i)
	id, ok := r.str(a["id"], "action.id", base.AtName("id"))
	if !ok {
		return nil, "", nil
	}
	at := fmt.Sprintf("action[%s]", id)
	if !flowdoc.IsValidIdentifier(id) {
		r.fail("INVALID_IDENTIFIER", at, base.AtName("id"), "%q is not an identifier the Flow language allows.", id)
		return nil, "", nil
	}
	if seen[id] {
		r.fail("DUPLICATE_ACTION_ID", at, base.AtName("id"), "Two actions have id %q.", id)
		return nil, "", nil
	}
	seen[id] = true

	var typ string
	var params jsonv.Object
	chosen := ""
	for _, block := range TypeBlocks() {
		v := a[block]
		if v == nil {
			continue
		}
		if chosen != "" {
			r.fail("MULTIPLE_TYPE_BLOCKS", at, base.AtName(block), "%s has both %s and %s; an action is one type.", at, chosen, block)
			return nil, "", nil
		}
		chosen = block
		body, ok := v.(map[string]any)
		if !ok {
			r.incomplete = true
			return nil, "", nil
		}
		p := fmt.Sprintf("%s.%s", at, block)
		if block == "generic" {
			typ, params, ok = r.generic(body, p, base.AtName(block))
		} else {
			typ, params, ok = r.typed(block, body, p, base.AtName(block))
		}
		if !ok {
			return nil, "", nil
		}
	}
	if chosen == "" {
		r.fail("NO_TYPE_BLOCK", at, base, "%s has no action type block.", at)
		return nil, "", nil
	}

	var conditions, errs []any
	if list, ok := a["condition"].([]any); ok {
		for k, c := range list {
			m, _ := c.(map[string]any)
			p := fmt.Sprintf("%s.condition", at)
			attr := base.AtName("condition").AtListIndex(k)
			op, ok1 := r.str(m["operator"], p+".operator", attr.AtName("operator"))
			next, ok2 := r.str(m["next"], p+".next", attr.AtName("next"))
			operands := []any{}
			if list, ok := m["operands"].([]any); ok {
				for j, o := range list {
					s, ok := r.str(o, fmt.Sprintf("%s.operands[%d]", p, j), attr.AtName("operands").AtListIndex(j))
					if !ok {
						return nil, "", nil
					}
					operands = append(operands, s)
				}
			} else {
				r.fail("NON_LITERAL_VALUE", p+".operands", attr.AtName("operands"), "condition needs operands.")
				return nil, "", nil
			}
			if !ok1 || !ok2 {
				return nil, "", nil
			}
			conditions = append(conditions, jsonv.Object{
				{Key: "NextAction", Value: next},
				{Key: "Condition", Value: jsonv.Object{{Key: "Operator", Value: op}, {Key: "Operands", Value: operands}}},
			})
		}
	}
	if list, ok := a["error"].([]any); ok {
		for k, e := range list {
			m, _ := e.(map[string]any)
			p := fmt.Sprintf("%s.error", at)
			attr := base.AtName("error").AtListIndex(k)
			et, ok1 := r.str(m["type"], p+".type", attr.AtName("type"))
			next, ok2 := r.str(m["next"], p+".next", attr.AtName("next"))
			if !ok1 || !ok2 {
				return nil, "", nil
			}
			errs = append(errs, jsonv.Object{{Key: "ErrorType", Value: et}, {Key: "NextAction", Value: next}})
		}
	}
	var pos *flowdoc.Point
	if m, ok := a["position"].(map[string]any); ok {
		x, okx := r.integer(m["x"], at+".position.x", base.AtName("position").AtName("x"))
		y, oky := r.integer(m["y"], at+".position.y", base.AtName("position").AtName("y"))
		if !okx || !oky {
			return nil, "", nil
		}
		pos = &flowdoc.Point{X: x, Y: y}
	}

	transitions := jsonv.Object{}
	next, hasNext := a["next"].(string)
	if _, unknown := a["next"].(Unknown); unknown {
		r.incomplete = true
		return nil, "", nil
	}
	entry := flowdoc.ModeledEntry(typ)
	bare := !hasNext && len(conditions) == 0 && len(errs) == 0
	// Rule 18: nothing wired reads as {} on a terminal or unmodeled type and
	// as empty lists on any other.
	if !bare || (entry != nil && !entry.Terminal) {
		if hasNext {
			transitions = append(transitions, jsonv.Member{Key: "NextAction", Value: next})
		}
		if errs == nil {
			errs = []any{}
		}
		if conditions == nil {
			conditions = []any{}
		}
		transitions = append(transitions, jsonv.Member{Key: "Errors", Value: errs}, jsonv.Member{Key: "Conditions", Value: conditions})
	}
	return jsonv.Object{
		{Key: "Identifier", Value: id},
		{Key: "Type", Value: typ},
		{Key: "Parameters", Value: params},
		{Key: "Transitions", Value: transitions},
	}, id, pos
}

func (r *reader) generic(body map[string]any, p string, attr path.Path) (string, jsonv.Object, bool) {
	typ, ok := r.str(body["type"], p+".type", attr.AtName("type"))
	if !ok {
		return "", nil, false
	}
	params := jsonv.Object{}
	switch v := body["parameters"].(type) {
	case nil:
	case Unknown:
		r.incomplete = true
		return "", nil, false
	case string:
		decoded, err := jsonv.Decode([]byte(v))
		o, isObj := decoded.(jsonv.Object)
		if err != nil || !isObj {
			r.fail("NON_LITERAL_VALUE", p+".parameters", attr.AtName("parameters"), "parameters must be jsonencode({...}) of an object.")
			return "", nil, false
		}
		params = o
	}
	return typ, params, true
}

func (r *reader) typed(block string, body map[string]any, p string, attr path.Path) (string, jsonv.Object, bool) {
	var typ string
	var entry *flowdoc.ModeledAction
	for _, t := range flowdoc.ModeledTypes() {
		if e := flowdoc.ModeledEntry(t); e.Block == block {
			typ, entry = t, e
		}
	}
	params := jsonv.Object{}
	ok := true
	for _, param := range entry.Parameters {
		v := body[param.Attr]
		if v == nil {
			// Terraform reads null as unset; so does the document.
			continue
		}
		val, good := r.value(v, param.CatalogElement, p+"."+param.Attr, attr.AtName(param.Attr))
		if !good {
			ok = false
			continue
		}
		params = append(params, jsonv.Member{Key: param.Key, Value: val})
	}
	return typ, params, ok
}

// value is one parameter or field by its catalog kind (rule 11, read
// backwards, on an evaluated value).
func (r *reader) value(v any, e flowdoc.CatalogElement, p string, attr path.Path) (any, bool) {
	if _, unknown := v.(Unknown); unknown {
		if e.Kind == "ref" && r.phase == PhasePlan {
			ex := fmt.Sprintf("%q", e.Ref+":<name>")
			r.fail("REF_EXPRESSION_REFUSED", p, attr,
				"%s is computed from an expression; write a reference key such as %s here and bind the expression in refs: %s = <expression>.", p, ex, ex)
			return nil, false
		}
		r.incomplete = true
		return nil, false
	}
	switch e.Kind {
	case "ref":
		s, ok := r.str(v, p, attr)
		if !ok {
			return nil, false
		}
		return r.ref(s, e.Ref, p, attr)
	case "json":
		s, ok := r.str(v, p, attr)
		if !ok {
			return nil, false
		}
		decoded, err := jsonv.Decode([]byte(s))
		if err != nil {
			r.fail("NON_LITERAL_VALUE", p, attr, "%s must be jsonencode(...) of a value: %v.", p, err)
			return nil, false
		}
		return decoded, true
	case "integer":
		if t, ok := v.(*big.Float); ok {
			f, _ := t.Float64()
			return f, true
		}
		r.fail("NON_LITERAL_VALUE", p, attr, "%s must be a number.", p)
		return nil, false
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			r.fail("NON_LITERAL_VALUE", p, attr, "%s must be an object literal.", p)
			return nil, false
		}
		out := jsonv.Object{}
		good := true
		for _, f := range e.Fields {
			fv := m[f.Attr]
			if fv == nil {
				continue
			}
			val, ok := r.value(fv, f.CatalogElement, p+"."+f.Attr, attr.AtName(f.Attr))
			if !ok {
				good = false
				continue
			}
			out = append(out, jsonv.Member{Key: f.Key, Value: val})
		}
		return out, good
	case "list":
		list, ok := v.([]any)
		if !ok {
			r.fail("NON_LITERAL_VALUE", p, attr, "%s must be a list literal.", p)
			return nil, false
		}
		out := make([]any, 0, len(list))
		good := true
		for k, item := range list {
			of := flowdoc.CatalogElement{Kind: "string"}
			if e.Of != nil {
				of = *e.Of
			}
			val, ok := r.value(item, of, fmt.Sprintf("%s[%d]", p, k), attr.AtListIndex(k))
			if !ok {
				good = false
				continue
			}
			out = append(out, val)
		}
		return out, good
	case "map":
		m, ok := v.(map[string]any)
		if !ok {
			r.fail("NON_LITERAL_VALUE", p, attr, "%s must be a map literal.", p)
			return nil, false
		}
		out := jsonv.Object{}
		good := true
		for _, k := range sortedKeys(m) {
			if m[k] == nil {
				continue
			}
			of := flowdoc.CatalogElement{Kind: "string"}
			if e.Of != nil {
				of = *e.Of
			}
			val, ok := r.value(m[k], of, p+"."+k, attr.AtMapKey(k))
			if !ok {
				good = false
				continue
			}
			out = append(out, jsonv.Member{Key: k, Value: val})
		}
		return out, good
	default:
		return r.str(v, p, attr)
	}
}

// ref is a reference field's value (rule 20): a JSONPath, the full token, or
// a key of the field's type.
func (r *reader) ref(s, refType, p string, attr path.Path) (any, bool) {
	example := fmt.Sprintf("%q", refType+":<name>")
	if strings.HasPrefix(s, "$.") {
		return s, true
	}
	if strings.HasPrefix(s, "arn:") {
		r.fail("LITERAL_ARN", p, attr, "%s holds a literal ARN; write a reference key such as %s and bind it in refs.", p, example)
		return nil, false
	}
	key := s
	if _, ok := flowdoc.ParseToken(s); ok {
		key = s[len("${cdref:") : len(s)-1]
	}
	m := RefKeyPattern.FindStringSubmatch(key)
	if m == nil || m[1] != refType {
		r.fail("REF_KEY_MALFORMED", p, attr,
			"%s must hold a %s reference key such as %s, a JSONPath, or the full token; got %q.", p, refType, example, s)
		return nil, false
	}
	return "${cdref:" + key + "}", true
}

func (r *reader) str(v any, p string, attr path.Path) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case Unknown:
		r.incomplete = true
		return "", false
	case nil:
		r.fail("NON_LITERAL_VALUE", p, attr, "%s is required.", p)
		return "", false
	}
	r.fail("NON_LITERAL_VALUE", p, attr, "%s must be a string.", p)
	return "", false
}

func (r *reader) integer(v any, p string, attr path.Path) (float64, bool) {
	switch t := v.(type) {
	case *big.Float:
		if !t.IsInt() {
			r.fail("POSITION_NOT_INTEGER", p, attr, "%s must be an integer.", p)
			return 0, false
		}
		f, _ := t.Float64()
		return f, true
	case Unknown:
		r.incomplete = true
		return 0, false
	}
	r.fail("NON_LITERAL_VALUE", p, attr, "%s needs a number.", p)
	return 0, false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
