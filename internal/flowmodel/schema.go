// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package flowmodel is the flowascode resource's view of a FlowDoc: the
// `action` block schema, built from the vendored action catalog at run time
// (so it cannot drift from the catalog the rest of the provider reads), and
// the conversion between a resource's configuration and a FlowDoc by the
// rules in conformance/hcl/README.md.
package flowmodel

import (
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
)

// ActionBlock is the repeated `action` block (rule 9): id and next, one
// sub-block per modeled action type named by the catalog's `block`, the
// `generic` block for any other type, condition and error blocks, and
// position. Every attribute inside is optional in the schema; what the
// contract requires is checked when the configuration is read, so the
// diagnostics carry the contract's codes and paths.
func ActionBlock() schema.ListNestedBlock {
	blocks := map[string]schema.Block{
		"generic": schema.SingleNestedBlock{
			Description: "An action type the tooling does not model, as its Flow language Type and Parameters.",
			Attributes: map[string]schema.Attribute{
				"type":       schema.StringAttribute{Optional: true, Description: "The Flow language action Type."},
				"parameters": schema.StringAttribute{Optional: true, Description: "The Parameters object, as jsonencode({...}). Omitted means {}."},
			},
		},
		"condition": schema.ListNestedBlock{
			Description: "One entry of the action's Conditions, in order.",
			NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
				"operator": schema.StringAttribute{Optional: true},
				"operands": schema.ListAttribute{Optional: true, ElementType: types.StringType},
				"next":     schema.StringAttribute{Optional: true},
			}},
		},
		"error": schema.ListNestedBlock{
			Description: "One entry of the action's Errors, in order.",
			NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
				"type": schema.StringAttribute{Optional: true},
				"next": schema.StringAttribute{Optional: true},
			}},
		},
		"position": schema.SingleNestedBlock{
			Description: "Where the action sits on the canvas, when that is not where the layout puts it.",
			Attributes: map[string]schema.Attribute{
				"x": schema.NumberAttribute{Optional: true},
				"y": schema.NumberAttribute{Optional: true},
			},
		},
	}
	for _, typ := range flowdoc.ModeledTypes() {
		entry := flowdoc.ModeledEntry(typ)
		attrs := map[string]schema.Attribute{}
		for _, p := range entry.Parameters {
			attrs[p.Attr] = attribute(p.CatalogElement, fmt.Sprintf("Flow language parameter %s.", p.Key))
		}
		blocks[entry.Block] = schema.SingleNestedBlock{
			Description: fmt.Sprintf("A %s action: %s", typ, entry.Doc),
			Attributes:  attrs,
		}
	}
	return schema.ListNestedBlock{
		Description: "One action of the flow, in order.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"id":   schema.StringAttribute{Required: true, Description: "The action's Identifier."},
				"next": schema.StringAttribute{Optional: true, Description: "The action's NextAction."},
			},
			Blocks: blocks,
		},
	}
}

// attribute is one catalog element as a schema attribute (rule 11, read as a
// type). integerString is a string attribute because Terraform converts a
// number to a string. integer is a number: the framework has no dynamic
// attribute inside a block list, so an integer parameter holding a JSONPath
// is written generic (rule 11).
func attribute(e flowdoc.CatalogElement, description string) schema.Attribute {
	switch e.Kind {
	case "integer":
		return schema.NumberAttribute{Optional: true, Description: description}
	case "object":
		return schema.SingleNestedAttribute{Optional: true, Description: description, Attributes: fields(e.Fields)}
	case "list":
		if e.Of != nil && e.Of.Kind == "object" {
			return schema.ListNestedAttribute{Optional: true, Description: description, NestedObject: schema.NestedAttributeObject{Attributes: fields(e.Of.Fields)}}
		}
		return schema.ListAttribute{Optional: true, Description: description, ElementType: types.StringType}
	case "map":
		if e.Of != nil && e.Of.Kind == "object" {
			return schema.MapNestedAttribute{Optional: true, Description: description, NestedObject: schema.NestedAttributeObject{Attributes: fields(e.Of.Fields)}}
		}
		return schema.MapAttribute{Optional: true, Description: description, ElementType: types.StringType}
	case "json":
		return schema.StringAttribute{Optional: true, Description: description + " Written as jsonencode(...)."}
	default:
		return schema.StringAttribute{Optional: true, Description: description}
	}
}

func fields(ps []flowdoc.CatalogParameter) map[string]schema.Attribute {
	out := map[string]schema.Attribute{}
	for _, p := range ps {
		out[p.Attr] = attribute(p.CatalogElement, fmt.Sprintf("Flow language field %s.", p.Key))
	}
	return out
}

// TypeBlocks are the sub-block names that choose an action's type: every
// modeled type's block, then generic.
func TypeBlocks() []string {
	var out []string
	for _, typ := range flowdoc.ModeledTypes() {
		out = append(out, flowdoc.ModeledEntry(typ).Block)
	}
	sort.Strings(out)
	return append(out, "generic")
}
