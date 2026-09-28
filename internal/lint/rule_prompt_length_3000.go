// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// "When you use text, either for text-to-speech or chat, you can use a
// maximum of 3,000 billed characters, which is 6,000 characters total."
// https://docs.aws.amazon.com/connect/latest/adminguide/play.html
// https://docs.aws.amazon.com/connect/latest/adminguide/get-customer-input.html
// Billed characters are the spoken text; SSML markup counts toward the 6,000
// total but not the 3,000 billed. Characters are UTF-16 code units, as the
// TypeScript's String length counts them.
const (
	// MaxBilledCharacters is prompt-length-3000.ts's MAX_BILLED_CHARACTERS.
	MaxBilledCharacters = 3000
	// MaxTotalCharacters is prompt-length-3000.ts's MAX_TOTAL_CHARACTERS.
	MaxTotalCharacters = 6000
)

var ssmlTag = regexp.MustCompile(`<[^>]*>`)

// stripSsmlTags is prompt-length-3000.ts's stripSsmlTags.
func stripSsmlTags(s string) string { return ssmlTag.ReplaceAllString(s, "") }

// isSsmlPath is prompt-length-3000.ts's isSsmlPath: the last dotted segment
// is SSML.
func isSsmlPath(path string) bool {
	segments := strings.Split(path, ".")
	return segments[len(segments)-1] == "SSML"
}

// PromptLength3000 is rules/prompt-length-3000.ts. Which actions carry a
// body, and where, comes from the catalog's textBodies.
var PromptLength3000 = Rule{
	ID:          "prompt-length-3000",
	Description: "Prompt text must stay within 3,000 billed characters and 6,000 total.",
	Check: func(ctx RuleContext) {
		for _, a := range actionsOf(ctx.Doc) {
			for _, p := range flowdoc.TextBodyPaths(a.typ) {
				// Catalog paths are well formed (the flowdoc tests hold every
				// one to isCatalogPath), so ReadPath cannot fail here.
				hits, _ := flowdoc.ReadPath(a.parameters, p)
				for _, hit := range hits {
					body, ok := hit.Value.(string)
					if !ok {
						continue
					}
					label := hit.Path
					length := jsLength(body)

					if !isSsmlPath(hit.Path) {
						if length > MaxBilledCharacters {
							ctx.Report(Report{
								Severity: SeverityError,
								BlockID:  blockID(a.id),
								Message:  label + " is " + strconv.Itoa(length) + " characters; Connect allows " + strconv.Itoa(MaxBilledCharacters) + " billed characters.",
							})
						}
						continue
					}

					if length > MaxTotalCharacters {
						ctx.Report(Report{
							Severity: SeverityError,
							BlockID:  blockID(a.id),
							Message:  label + " is " + strconv.Itoa(length) + " characters; Connect allows " + strconv.Itoa(MaxTotalCharacters) + " total.",
						})
					}
					spoken := jsLength(stripSsmlTags(body))
					if spoken > MaxBilledCharacters {
						ctx.Report(Report{
							Severity: SeverityError,
							BlockID:  blockID(a.id),
							Message:  label + " contains " + strconv.Itoa(spoken) + " spoken characters; Connect allows " + strconv.Itoa(MaxBilledCharacters) + " billed characters.",
						})
					}
				}
			}
		}
	},
}
