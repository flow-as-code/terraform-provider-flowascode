// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"errors"
	"sort"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Options is engine.ts's LintOptions.
type Options struct {
	// Rules defaults to every built-in rule when nil. A non-nil empty slice
	// runs no rule, as an empty array does in the TypeScript (it is not
	// nullish, so the ?? default does not apply).
	Rules []Rule
	// Disable names rule ids to skip. A hard rule cannot be skipped, and
	// naming one is an error: the same refusal a .flow.tf's lint block and
	// the provider make (conformance/hcl/README.md, rule 19). An id no rule
	// has is ignored.
	Disable []string
}

// compareFindings is engine.ts's order: doc, then rule, then blockId (absent
// reads as ""), then message, each with localeCompare (see collate.go for
// what that is and where the port of it stops being exact).
func compareFindings(a, b Finding) int {
	if c := localeCompare(a.Doc, b.Doc); c != 0 {
		return c
	}
	if c := localeCompare(a.Rule, b.Rule); c != 0 {
		return c
	}
	if c := localeCompare(deref(a.BlockID), deref(b.BlockID)); c != 0 {
		return c
	}
	return localeCompare(a.Message, b.Message)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Lint is engine.ts's lint: every rule over every document, findings
// sorted so output is stable regardless of rule execution order. The sort
// is stable, as Array.prototype.sort is, so findings the comparator calls
// equal keep document, then rule, then report order.
//
// Each document must pass flowdoc.AssertFlowDoc (context "lint"), checked
// before anything else, and the error is returned as that function gives
// it. Then a hard rule named in Disable is refused with engine.ts's message.
// The TypeScript's single-document overload is a one-element slice here.
func Lint(docs []any, opts Options) ([]Finding, error) {
	set := make([]jsonv.Object, 0, len(docs))
	for _, d := range docs {
		doc, err := flowdoc.AssertFlowDoc(d, "lint")
		if err != nil {
			return nil, err
		}
		set = append(set, doc)
	}

	candidates := opts.Rules
	if candidates == nil {
		candidates = allRules
	}
	disabled := map[string]bool{}
	for _, id := range opts.Disable {
		disabled[id] = true
	}
	var hard []string
	for _, r := range candidates {
		if r.Hard && disabled[r.ID] {
			hard = append(hard, r.ID)
		}
	}
	if len(hard) > 0 {
		return nil, errors.New("lint cannot disable a hard rule: " + strings.Join(hard, ", ") + ". " +
			"Hard rules block a save and are never skipped.")
	}
	var rules []Rule
	for _, r := range candidates {
		if !disabled[r.ID] {
			rules = append(rules, r)
		}
	}

	findings := []Finding{}
	for _, doc := range set {
		name := docName(doc)
		for _, rule := range rules {
			id := rule.ID
			rule.Check(RuleContext{
				Doc: doc,
				All: set,
				Report: func(r Report) {
					findings = append(findings, Finding{
						Rule:     id,
						Severity: r.Severity,
						Message:  r.Message,
						Doc:      name,
						BlockID:  r.BlockID,
					})
				},
			})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return compareFindings(findings[i], findings[j]) < 0 })
	return findings, nil
}

// HasBlockingFindings is engine.ts's hasBlockingFindings: whether any
// finding would block a studio save, that is, comes from a hard rule. rules
// defaults to every built-in rule when nil.
func HasBlockingFindings(findings []Finding, rules []Rule) bool {
	if rules == nil {
		rules = allRules
	}
	hard := map[string]bool{}
	for _, r := range rules {
		if r.Hard {
			hard[r.ID] = true
		}
	}
	for _, f := range findings {
		if hard[f.Rule] {
			return true
		}
	}
	return false
}
