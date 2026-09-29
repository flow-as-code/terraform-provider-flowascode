// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"sort"
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// ReverseMap is export.ts's ReverseMap.
type ReverseMap struct {
	// ByArn maps a normalized ARN to the ref entry that replaces it.
	ByArn map[string]flowdoc.RefEntry
	// Warnings are names that collided, bots with no ARN, and anything else
	// lossy, in the order the TypeScript pushes them.
	Warnings []string
}

type candidate struct {
	arn  string
	typ  string
	name string
	// alias "" is none. buildReverseMap never sets one.
	alias string
}

// makeRefEntry is export.ts's makeRefEntry.
func makeRefEntry(typ, name, alias string) (flowdoc.RefEntry, bool) {
	suffix := ""
	if alias != "" {
		suffix = "@" + alias
	}
	return flowdoc.ParseToken("${cdref:" + typ + ":" + name + suffix + "}")
}

// assignNames is export.ts's assignNames: one slug per resource, per ref type,
// candidates sorted by (type, name, ARN) and a collision taking a numeric
// suffix. The sort compares with JavaScript's < (jsonv.LessUTF16), stably, as
// Array.prototype.sort is.
func assignNames(candidates []candidate, warnings *[]string) map[string]flowdoc.RefEntry {
	byArn := map[string]flowdoc.RefEntry{}
	taken := map[string]map[string]bool{}
	sorted := append([]candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.typ != b.typ {
			return jsonv.LessUTF16(a.typ, b.typ)
		}
		if a.name != b.name {
			return jsonv.LessUTF16(a.name, b.name)
		}
		return jsonv.LessUTF16(a.arn, b.arn)
	})

	for _, c := range sorted {
		arn := NormalizeArn(c.arn)
		if _, ok := byArn[arn]; ok {
			continue
		}
		used := taken[c.typ]
		if used == nil {
			used = map[string]bool{}
			taken[c.typ] = used
		}

		name := c.name
		if used[name] {
			n := 2
			for used[name+"-"+strconv.Itoa(n)] {
				n++
			}
			renamed := name + "-" + strconv.Itoa(n)
			*warnings = append(*warnings,
				"Two "+c.typ+` resources slug to "`+name+`"; `+arn+` exported as "`+renamed+`".`)
			name = renamed
		}

		entry, ok := makeRefEntry(c.typ, name, c.alias)
		if !ok {
			*warnings = append(*warnings, "Cannot name "+c.typ+" "+arn+`: "`+name+`" is not a valid slug.`)
			continue
		}
		used[name] = true
		byArn[arn] = entry
	}
	return byArn
}

// candidateName is export.ts's candidateName.
func candidateName(raw, arn, typ string, warnings *[]string) (string, bool) {
	slug := SlugifyResourceName(raw)
	if flowdoc.SlugPattern.MatchString(slug) {
		return slug, true
	}
	*warnings = append(*warnings, "Skipping "+typ+" "+arn+`: name "`+raw+`" has no slug form.`)
	return "", false
}

// BuildReverseMap is export.ts's buildReverseMap: ARN to ref entry for one
// instance. Every ARN a flow can hold gets an entry, so anything left over
// after export is genuinely unknown and is reported.
func BuildReverseMap(inventory InstanceInventory) ReverseMap {
	warnings := []string{}
	var candidates []candidate

	add := func(arn string, rawName *string, typ string) {
		if arn == "" || rawName == nil {
			return
		}
		name, ok := candidateName(*rawName, arn, typ, &warnings)
		if !ok {
			return
		}
		candidates = append(candidates, candidate{arn: arn, typ: typ, name: name})
	}
	named := func(s string) *string { return &s }

	for _, q := range inventory.Queues {
		add(q.Arn, named(q.Name), "queue")
	}
	for _, h := range inventory.HoursOfOperations {
		add(h.Arn, named(h.Name), "hours")
	}
	for _, p := range inventory.Prompts {
		add(p.Arn, named(p.Name), "prompt")
	}
	for _, f := range inventory.ContactFlows {
		add(f.Arn, named(f.Name), "flow")
	}
	for _, m := range inventory.ContactFlowModules {
		add(m.Arn, named(m.Name), "module")
	}
	// A view ARN in flow content carries the version as a qualifier; the map
	// is keyed on the bare ARN and rewriteArns puts the version back as the
	// alias.
	for _, v := range inventory.Views {
		add(v.Arn, named(v.Name), "view")
	}

	for _, arn := range inventory.LambdaFunctions {
		fn, ok := ParseLambdaFunctionArn(arn)
		if !ok {
			warnings = append(warnings, "Skipping Lambda ARN with no function segment: "+arn)
			continue
		}
		add(arn, named(fn), "lambda")
	}

	for _, bot := range inventory.LexBots {
		if bot.AliasArn == nil || *bot.AliasArn == "" {
			// A V1 bot has no ARN at all, so nothing in flow content can be
			// matched back to it.
			label := "(unnamed)"
			if bot.Name != nil {
				label = *bot.Name
			}
			warnings = append(warnings,
				"Amazon Lex "+bot.LexVersion+` bot "`+label+`" has no ARN and is not reverse-mapped.`)
			continue
		}
		// ListBots gives a V2 bot an AliasArn and no name, so the slug comes
		// from the ARN's own identifiers unless the entry carries a name.
		raw := ""
		if bot.Name != nil {
			raw = *bot.Name
		} else if parts := strings.Split(*bot.AliasArn, ":"); len(parts) > 5 {
			raw = strings.Join(parts[5:], "-")
		}
		add(*bot.AliasArn, &raw, "lex")
	}

	byArn := assignNames(candidates, &warnings)
	// An invocation through an alias names it by id; key it so the document
	// reads module:<name>@<alias name>. An alias name that is not a slug
	// keeps the id.
	for _, a := range inventory.ModuleAliases {
		module, ok := byArn[NormalizeArn(a.ModuleArn)]
		if !ok {
			continue
		}
		if !flowdoc.SlugPattern.MatchString(a.Name) {
			warnings = append(warnings, `Module alias "`+a.Name+`" of `+a.ModuleArn+" is not a slug; a flow invoking it exports with its alias id.")
			continue
		}
		byArn[NormalizeArn(a.ModuleArn)+":"+a.AliasID] = flowdoc.RefEntry{
			Token: "${cdref:module:" + module.Name + "@" + a.Name + "}", Type: "module", Name: module.Name, Alias: a.Name}
	}
	return ReverseMap{ByArn: byArn, Warnings: warnings}
}

// ReverseMapOfResourceMap is export.ts's reverseMapOfResourceMap: the reverse
// map from a materialization resource map (token to value), keys taken in
// JavaScript < order.
func ReverseMapOfResourceMap(resourceMap map[string]string) ReverseMap {
	warnings := []string{}
	byArn := map[string]flowdoc.RefEntry{}
	keys := make([]string, 0, len(resourceMap))
	for k := range resourceMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return jsonv.LessUTF16(keys[i], keys[j]) })
	for _, token := range keys {
		entry, ok := flowdoc.ParseToken(token)
		if !ok {
			warnings = append(warnings, "Skipping map key that is not a reference token: "+token)
			continue
		}
		// An entry that pins an alias or version is keyed by the ARN exactly
		// as bound, so two aliases of one module stay two entries; LookupArn
		// still falls back to the bare ARN.
		arn := resourceMap[token]
		if entry.Alias == "" {
			arn = NormalizeArn(arn)
		}
		if existing, ok := byArn[arn]; ok {
			warnings = append(warnings,
				arn+" is mapped by both "+existing.Token+" and "+token+"; keeping the first.")
			continue
		}
		byArn[arn] = entry
	}
	return ReverseMap{ByArn: byArn, Warnings: warnings}
}

// LookupArn is export.ts's lookupArn: tolerant of a `:$SAVED` or version
// qualifier.
func LookupArn(reverseMap ReverseMap, arn string) (flowdoc.RefEntry, bool) {
	if e, ok := reverseMap.ByArn[arn]; ok {
		return e, true
	}
	e, ok := reverseMap.ByArn[NormalizeArn(arn)]
	return e, ok
}
