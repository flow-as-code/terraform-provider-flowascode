// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// ConditionalShape is rules/conditional-shape.ts: the parameters, error
// branches and conditions an action must or must not carry given another
// parameter's static value, from the catalog's shapes. A deciding value lint
// cannot know (not a plain string, or a JSONPath) is not checked.
var ConditionalShape = Rule{
	ID:          "conditional-shape",
	Description: "An action whose shape depends on a parameter's value carries the parameters, error branches and conditions that value requires, and none it forbids.",
	Check: func(ctx RuleContext) {
		for _, a := range actionsOf(ctx.Doc) {
			entry := flowdoc.ModeledEntry(a.typ)
			if entry == nil || len(entry.Shapes) == 0 {
				continue
			}
			wired := map[string]bool{}
			for _, e := range listAt(a.transitions, "Errors") {
				if s, ok := field(e, "ErrorType").(string); ok {
					wired[s] = true
				}
			}
			conditions := listAt(a.transitions, "Conditions")
			has := func(key string) bool {
				_, ok := a.parameters.Get(key)
				return ok
			}
			for _, shape := range entry.Shapes {
				phrase, ok := shapeApplies(shape, a.parameters)
				if !ok {
					continue
				}
				say := func(what string) {
					ctx.Report(Report{Severity: SeverityError, BlockID: blockID(a.id), Message: a.typ + " " + phrase + " " + what + "."})
				}
				if r := shape.Requires; r != nil {
					for _, key := range r.Parameters {
						if !has(key) {
							say("needs " + key)
						}
					}
					for _, typ := range r.Errors {
						if !wired[typ] {
							say(`needs its "` + typ + `" branch`)
						}
					}
				}
				if f := shape.Forbids; f != nil {
					for _, key := range f.Parameters {
						if has(key) {
							say("must not carry " + key)
						}
					}
					for _, typ := range f.Errors {
						if wired[typ] {
							say(`must not wire the "` + typ + `" branch`)
						}
					}
					if f.Conditions && len(conditions) > 0 {
						say("takes no conditions")
					}
				}
			}
		}
	},
}

// shapeApplies is conditional-shape.ts's applies: how the finding names the
// shape's condition, and whether it holds. An absent parameter is not equal
// to any value; a value that is present but not a string (JSON null
// included, typeof "object" in JavaScript) or a JSONPath cannot be judged.
func shapeApplies(shape flowdoc.CatalogShape, params interface{ Get(string) (any, bool) }) (string, bool) {
	key := shape.When.Key
	v, present := params.Get(key)
	value, isString := v.(string)
	if present && !isString {
		return "", false
	}
	if isString && strings.HasPrefix(value, "$") {
		return "", false
	}
	if eq := shape.When.Equals; eq != nil {
		if present && value == *eq {
			return "with " + key + ` "` + *eq + `"`, true
		}
		return "", false
	}
	if ne := shape.When.NotEquals; ne != nil {
		if !present || value != *ne {
			return "without " + key + ` "` + *ne + `"`, true
		}
		return "", false
	}
	return "", false
}
