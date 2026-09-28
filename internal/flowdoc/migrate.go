// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Reading an older FlowDoc, ported from packages/core/src/migrate.ts.
//
// 0.1 to 0.2 is a version bump and nothing else. 0.2 added the `view`
// reference type, `meta.sourceKind`, and an optional `description`, all of
// which a 0.1 document simply lacks.

// SupportedFlowDocVersions is migrate.ts's SUPPORTED_FLOWDOC_VERSIONS: every
// format version this build reads, oldest first.
var SupportedFlowDocVersions = []string{"0.1", Version}

// IsSupportedFlowDocVersion is migrate.ts's isSupportedFlowDocVersion.
func IsSupportedFlowDocVersion(version any) bool {
	s, ok := version.(string)
	return ok && contains(SupportedFlowDocVersions, s)
}

// MigrateFlowDoc is migrate.ts's migrateFlowDoc with its default context,
// "migrateFlowDoc".
func MigrateFlowDoc(value any) (jsonv.Object, error) {
	return MigrateFlowDocIn(value, "migrateFlowDoc")
}

// MigrateFlowDocIn is migrate.ts's migrateFlowDoc with an explicit context:
// a document at the current format version, whatever supported version it
// was read at. A current document comes back as is (the same Object); a 0.1
// document comes back as a copy with only `flowdoc` changed, in place, as
// `{ ...value, flowdoc }` writes it; any other version is an
// *InvalidFlowDocError naming it and the versions this build reads.
func MigrateFlowDocIn(value any, context string) (jsonv.Object, error) {
	doc, err := AssertFlowDoc(value, context)
	if err != nil {
		return nil, err
	}
	version, has := doc.Get("flowdoc")
	if s, ok := version.(string); has && ok {
		switch s {
		case Version:
			return doc, nil
		case "0.1":
			out := append(jsonv.Object{}, doc...)
			out.Set("flowdoc", Version)
			return out, nil
		}
	}
	// JSON.stringify(undefined) is undefined, which the template literal
	// writes as the word.
	shown := "undefined"
	if has {
		shown = string(jsonv.Encode(jsOrdered(version), ""))
	}
	quoted := make([]string, len(SupportedFlowDocVersions))
	for i, v := range SupportedFlowDocVersions {
		quoted[i] = string(jsonv.Encode(v, ""))
	}
	return nil, invalid(context + ": FlowDoc version " + shown + " is not supported; this build reads " + strings.Join(quoted, " and ") + ".")
}
