// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"regexp"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// attributeRead is attribute-set-before-read.ts's ATTRIBUTE_READ. The class
// is ASCII, so Go's UTF-8 matching finds what JavaScript's UTF-16 matching
// finds.
var attributeRead = regexp.MustCompile(`\$\.Attributes\.([A-Za-z0-9_-]+)`)

// attributesSet is attribute-set-before-read.ts's attributesSet: the names
// every UpdateContactAttributes action in the set writes on the current
// contact. A TargetContact that is present and not "Current" (Related, or a
// null the schema would refuse) writes the related contact's attributes,
// which leaves the current contact's as empty as before, so it does not
// count; an absent key is the default, Current.
func attributesSet(docs []jsonv.Object) map[string]bool {
	names := map[string]bool{}
	for _, doc := range docs {
		for _, a := range actionsOf(doc) {
			if a.typ != "UpdateContactAttributes" {
				continue
			}
			if target, present := a.parameters.Get("TargetContact"); present && target != "Current" {
				continue
			}
			v, _ := a.parameters.Get("Attributes")
			attributes, ok := v.(jsonv.Object)
			if !ok {
				continue
			}
			for _, m := range attributes {
				names[m.Key] = true
			}
		}
	}
	return names
}

// AttributeSetBeforeRead is rules/attribute-set-before-read.ts. A prompt
// that reads `$.Attributes.<name>` speaks the attribute's value, and an
// attribute nothing has set is empty: the contact hears "Welcome back, ."
// and no error is raised anywhere. The read is in message text, which the
// catalog locates (textBodies); the write is UpdateContactAttributes, whose
// Attributes map keys are the names it sets.
// https://docs.aws.amazon.com/connect/latest/adminguide/connect-attrib-list.html#user-defined-attributes
// https://docs.aws.amazon.com/connect/latest/devguide/contact-actions-updatecontactattributes.html
//
// Attributes cross flows: a whisper reads what the main flow set, a module
// reads what its caller set. So this is a set-wide rule, and a lone document
// reports nothing, as module-depth-5 does: one document cannot know what the
// rest of the deployment sets. It is a warning rather than an error because
// an attribute can also arrive from outside any flow, set on the contact by
// the API that started it, by a chat widget, or by an agent; a set that
// reads such attributes disables the rule.
//
// Added 2026-10-05 (flow-as-code tasks/C15).
var AttributeSetBeforeRead = Rule{
	ID:          "attribute-set-before-read",
	Description: "A contact attribute read in message text must be set by some document in the linted set.",
	Check: func(ctx RuleContext) {
		if len(ctx.All) < 2 {
			return
		}
		set := attributesSet(ctx.All)
		for _, a := range actionsOf(ctx.Doc) {
			paths := flowdoc.TextBodyPaths(a.typ)
			if len(paths) == 0 {
				// An unmodeled type: the two keys a message body uses.
				paths = []string{"Text", "SSML"}
			}
			reported := map[string]bool{}
			for _, p := range paths {
				// Catalog paths and the two literals are well formed, so
				// ReadPath cannot fail here.
				hits, _ := flowdoc.ReadPath(a.parameters, p)
				for _, hit := range hits {
					body, ok := hit.Value.(string)
					if !ok {
						continue
					}
					for _, m := range attributeRead.FindAllStringSubmatch(body, -1) {
						name := m[1]
						if set[name] || reported[name] {
							continue
						}
						reported[name] = true
						ctx.Report(Report{
							Severity: SeverityWarning,
							BlockID:  blockID(a.id),
							Message:  hit.Path + " reads $.Attributes." + name + ", which no document in the set sets (UpdateContactAttributes); the contact hears an empty value.",
						})
					}
				}
			}
		}
	},
}
