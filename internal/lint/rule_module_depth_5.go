// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// MaxModuleDepth is rules/module-depth-5.ts's MAX_MODULE_DEPTH. "You can
// invoke modules within other modules, supporting up to five levels of
// nesting with a stack limit to prevent recursive invocations."
// https://docs.aws.amazon.com/connect/latest/adminguide/contact-flow-modules.html
const MaxModuleDepth = 5

// invokedModules is module-depth-5.ts's invokedModules: the module names an
// InvokeFlowModule action's FlowModuleId token points at, in action order.
func invokedModules(doc jsonv.Object) []string {
	var names []string
	for _, a := range actionsOf(doc) {
		if a.typ != "InvokeFlowModule" {
			continue
		}
		v, _ := a.parameters.Get("FlowModuleId")
		id, ok := v.(string)
		if !ok {
			continue
		}
		if ref, ok := flowdoc.ParseToken(id); ok && ref.Type == "module" {
			names = append(names, ref.Name)
		}
	}
	return names
}

// ModuleDepth5 is rules/module-depth-5.ts. Depth is only computable across
// a set, so it reads ctx.All; linting one document alone reports nothing
// here, which is correct rather than a false pass. Only a flow starts a
// walk, so a violation is reported once, against the flow.
var ModuleDepth5 = Rule{
	ID:          "module-depth-5",
	Description: "Flow module invocation must not nest more than five levels deep, and must not recurse.",
	Check: func(ctx RuleContext) {
		if str(ctx.Doc, "kind") != "flow" {
			return
		}
		// new Map(modules.map(d => [d.name, d])): the last of a repeated name.
		modules := map[string]jsonv.Object{}
		for _, d := range ctx.All {
			if str(d, "kind") == "module" {
				modules[docName(d)] = d
			}
		}

		var walk func(current jsonv.Object, depth int, stack []string)
		walk = func(current jsonv.Object, depth int, stack []string) {
			for _, name := range invokedModules(current) {
				path := append(append([]string{}, stack...), name)
				if contains(stack, name) {
					ctx.Report(Report{
						Severity: SeverityError,
						Message:  "Recursive module invocation: " + strings.Join(path, " -> ") + ".",
					})
					continue
				}
				if depth+1 > MaxModuleDepth {
					ctx.Report(Report{
						Severity: SeverityError,
						Message: "Module nesting reaches depth " + strconv.Itoa(depth+1) + " via " + strings.Join(path, " -> ") +
							"; Connect allows " + strconv.Itoa(MaxModuleDepth) + ".",
					})
					continue
				}
				if next, ok := modules[name]; ok {
					walk(next, depth+1, path)
				}
			}
		}
		walk(ctx.Doc, 0, []string{docName(ctx.Doc)})
	},
}
