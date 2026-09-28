// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"regexp"
	"strings"
)

// arnPattern is no-literal-arn.ts's ARN.
var arnPattern = regexp.MustCompile(`arn:aws[a-z-]*:`)

// NoLiteralArnID is no-literal-arn.ts's NO_LITERAL_ARN, this rule's id, for
// tools that report the violation before lint can run.
const NoLiteralArnID = "no-literal-arn"

// LiteralArnMessage is no-literal-arn.ts's literalArnMessage: the sentence
// every literal-ARN report ends with.
func LiteralArnMessage(path string) string {
	return `"` + path + `" contains a literal ARN. Use a ${cdref:type:name} token instead.`
}

// LiteralArnPaths is no-literal-arn.ts's literalArnPaths: the path of every
// string holding a literal ARN in an arbitrary JSON value, in walk order.
func LiteralArnPaths(value any) []string {
	out := []string{}
	for _, s := range WalkStrings(value, "") {
		if arnPattern.MatchString(s.Value) {
			out = append(out, s.Path)
		}
	}
	return out
}

// NoLiteralArn is rules/no-literal-arn.ts, a hard rule. References are
// tokens, never literal ARNs: a literal ARN pins a flow to one account,
// region and instance. content.Metadata and refs are scanned as well as
// every action's Parameters and Transitions.
var NoLiteralArn = Rule{
	ID:          NoLiteralArnID,
	Description: "Authored content must not contain a literal ARN; use a ${cdref:...} token.",
	Hard:        true,
	Check: func(ctx RuleContext) {
		refs, has := ctx.Doc.Get("refs")
		if !has || refs == nil {
			refs = []any{}
		}
		documentLevel := append(documentStrings(ctx.Doc), WalkStrings(refs, "refs")...)
		for _, s := range documentLevel {
			if arnPattern.MatchString(s.Value) {
				ctx.Report(Report{Severity: SeverityError, Message: LiteralArnMessage(s.Path)})
			}
		}

		for _, as := range findingsForActions(ctx.Doc) {
			for _, s := range as.strings {
				if !arnPattern.MatchString(s.Value) {
					continue
				}
				kind := "Parameter"
				if strings.HasPrefix(s.Path, "Transitions") {
					kind = "Transition"
				}
				ctx.Report(Report{
					Severity: SeverityError,
					BlockID:  blockID(as.action.id),
					Message:  kind + " " + LiteralArnMessage(s.Path),
				})
			}
		}
	},
}
