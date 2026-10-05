// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

// allRules is rules/index.ts's allRules, in its order. Ids are stable and
// never renamed: the conformance fixtures, the CLI, the studio and this
// provider all key off them. The order matters in two places: rules run in
// it, so findings the sort cannot tell apart keep it, and the hard-rule
// refusal names rules in it.
var allRules = []Rule{
	NoLiteralArn,
	NoUnresolvedToken,
	ReachableBlocks,
	ErrorBranches,
	TerminalBlocks,
	ModuleDepth5,
	PromptLength3000,
	RecordingConsentBeforeRecord,
	UniqueNames,
	ActionAllowedInFlowType,
	ActionCount,
	ConditionalShape,
	NextActionRequired,
	ChannelRestrictedAction,
	AttributeSetBeforeRead,
}

// AllRules is rules/index.ts's allRules: every built-in rule, in registry
// order. The slice is a copy.
func AllRules() []Rule { return append([]Rule(nil), allRules...) }

// RuleByID is rules/index.ts's ruleById; ok is false where it returns
// undefined.
func RuleByID(id string) (Rule, bool) {
	for _, r := range allRules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}
