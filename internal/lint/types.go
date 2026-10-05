// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package lint is @flow-as-code/core's lint engine and its fifteen rules,
// ported one to one from packages/core/src/lint. Rule ids, severities and
// messages are the TypeScript's byte for byte, and findings come back in the
// same order, so the provider and the CLI report a document identically.
//
// Documents are jsonv values, as internal/flowdoc holds them. Lint runs
// flowdoc.AssertFlowDoc over each first, as engine.ts does.
package lint

import "github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"

// Severity is types.ts's Severity.
type Severity string

// The two severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding is types.ts's Finding.
type Finding struct {
	// Rule is the stable rule id. Never renamed; the provider keys off these.
	Rule     string
	Severity Severity
	Message  string
	// Doc is the name of the FlowDoc the finding belongs to.
	Doc string
	// BlockID is the action Identifier when the finding is about one action,
	// nil when it is not. An empty Identifier is a present "" (unique-names
	// reports one), which is why this is a pointer.
	BlockID *string
}

// Report is what a rule reports, types.ts's Omit<Finding, "rule" | "doc">.
type Report struct {
	Severity Severity
	BlockID  *string
	Message  string
}

// RuleContext is types.ts's RuleContext.
type RuleContext struct {
	Doc jsonv.Object
	// All is every doc being linted, for rules that follow module references
	// or compare names across the set.
	All    []jsonv.Object
	Report func(Report)
}

// Rule is types.ts's Rule.
type Rule struct {
	ID          string
	Description string
	// Hard marks a rule that blocks a save in the studio rather than merely
	// reporting (docs/02-studio-design.md: no-literal-arn and
	// no-unresolved-token). Options.Disable refuses to skip one.
	Hard  bool
	Check func(ctx RuleContext)
}

func blockID(id string) *string { return &id }
