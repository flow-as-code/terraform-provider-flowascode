// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package flowdoc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Dotted paths into an action's Parameters, the notation the catalog uses to
// name a reference-bearing or text-bearing field wherever it sits. Ported from
// packages/core/src/paths.ts.
//
//	PromptId              a top-level key
//	LexV2Bot.AliasArn     a key inside an object
//	Messages[].PromptId   a key inside every element of a list
//	EventHooks.*          every value of a map

// PathHit is paths.ts's PathHit: one value found at a concrete path,
// `Messages[2].PromptId` for example.
type PathHit struct {
	Path  string
	Value any
}

var segmentPattern = regexp.MustCompile(`^(?:\*|([A-Za-z0-9_$-]+)(\[\])?)$`)

// IsCatalogPath is paths.ts's isCatalogPath: whether a string is a
// well-formed catalog path.
func IsCatalogPath(p string) bool {
	if p == "" {
		return false
	}
	for _, s := range strings.Split(p, ".") {
		if !segmentPattern.MatchString(s) {
			return false
		}
	}
	return true
}

// ReadPath is paths.ts's readPath: every value at p within value. A path the
// value does not have yields nothing; a malformed path is an error, because
// it is a catalog error rather than a document one. A `*` segment visits a
// map's keys in JavaScript's Object.entries order (array-index keys first,
// ascending, then creation order).
func ReadPath(value any, p string) ([]PathHit, error) {
	if !IsCatalogPath(p) {
		return nil, fmt.Errorf(`"%s" is not a catalog path.`, p)
	}
	hits := []PathHit{{Path: "", Value: value}}
	for _, segment := range strings.Split(p, ".") {
		next := []PathHit{}
		for _, hit := range hits {
			obj, isRecord := hit.Value.(jsonv.Object)
			if segment == "*" {
				if !isRecord {
					continue
				}
				for _, k := range jsKeyOrder(obj.Keys()) {
					v, _ := obj.Get(k)
					next = append(next, PathHit{Path: joinPath(hit.Path, k), Value: v})
				}
				continue
			}
			m := segmentPattern.FindStringSubmatchIndex(segment)
			key := segment[m[2]:m[3]]
			list := m[4] >= 0
			if !isRecord {
				continue
			}
			at, has := obj.Get(key)
			if !has {
				continue
			}
			if !list {
				next = append(next, PathHit{Path: joinPath(hit.Path, key), Value: at})
			} else if arr, ok := at.([]any); ok {
				for i, v := range arr {
					next = append(next, PathHit{
						Path:  joinPath(hit.Path, key) + "[" + strconv.Itoa(i) + "]",
						Value: v,
					})
				}
			}
		}
		hits = next
	}
	return hits, nil
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
