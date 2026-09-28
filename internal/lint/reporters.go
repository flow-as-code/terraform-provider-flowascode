// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// findingJSON is a finding as JSON.stringify writes the object engine.ts
// builds, { ...report, rule, doc }: every built-in rule reports severity,
// then blockId when it has one, then message, so that is the key order.
func findingJSON(f Finding) jsonv.Object {
	o := jsonv.Object{{Key: "severity", Value: string(f.Severity)}}
	if f.BlockID != nil {
		o = append(o, jsonv.Member{Key: "blockId", Value: *f.BlockID})
	}
	return append(o,
		jsonv.Member{Key: "message", Value: f.Message},
		jsonv.Member{Key: "rule", Value: f.Rule},
		jsonv.Member{Key: "doc", Value: f.Doc},
	)
}

func countErrors(findings []Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == SeverityError {
			n++
		}
	}
	return n
}

// ToJSON is reporters.ts's toJson: the machine-readable form the CLI's
// --format json emits, byte for byte.
func ToJSON(findings []Finding) string {
	errs := countErrors(findings)
	list := make([]any, len(findings))
	for i, f := range findings {
		list[i] = findingJSON(f)
	}
	out := jsonv.Object{
		{Key: "summary", Value: jsonv.Object{
			{Key: "total", Value: float64(len(findings))},
			{Key: "errors", Value: float64(errs)},
			{Key: "warnings", Value: float64(len(findings) - errs)},
		}},
		{Key: "findings", Value: list},
	}
	return string(jsonv.Encode(out, "  ")) + "\n"
}

// ToText is reporters.ts's toText: one finding per line, grouped under a
// document heading whenever the document changes from the previous line.
func ToText(findings []Finding) string {
	if len(findings) == 0 {
		return "No findings.\n"
	}
	var lines []string
	current := ""
	for _, f := range findings {
		if f.Doc != current {
			if current != "" {
				lines = append(lines, "")
			}
			lines = append(lines, f.Doc)
			current = f.Doc
		}
		where := ""
		if f.BlockID != nil {
			where = " (" + *f.BlockID + ")"
		}
		lines = append(lines, "  "+string(f.Severity)+where+": "+f.Message+" ["+f.Rule+"]")
	}
	errs := countErrors(findings)
	lines = append(lines, "",
		strconv.Itoa(len(findings))+" finding(s): "+strconv.Itoa(errs)+" error(s), "+strconv.Itoa(len(findings)-errs)+" warning(s).")
	return strings.Join(lines, "\n") + "\n"
}
