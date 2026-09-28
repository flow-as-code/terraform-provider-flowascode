// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"context"
	"errors"
	"sort"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// exportableFlowTypes is export.ts's EXPORTABLE_FLOW_TYPES: the
// ContactFlowType values FlowDoc models. CAMPAIGN has no FlowDoc connectType.
var exportableFlowTypes = map[string]bool{
	"CONTACT_FLOW":     true,
	"CUSTOMER_QUEUE":   true,
	"CUSTOMER_HOLD":    true,
	"CUSTOMER_WHISPER": true,
	"AGENT_HOLD":       true,
	"AGENT_WHISPER":    true,
	"OUTBOUND_WHISPER": true,
	"AGENT_TRANSFER":   true,
	"QUEUE_TRANSFER":   true,
}

// ExportedFlow is export.ts's ExportedFlow, without `code`: codegen is not
// ported.
type ExportedFlow struct {
	Arn string
	ID  string
	// SourceName is the name as the instance spells it, before slugging.
	SourceName string
	// Doc is the canonical FlowDoc.
	Doc jsonv.Object
	// Saved is true when the flow was read through the $SAVED alias.
	Saved bool
}

// ExportFailure is export.ts's ExportFailure.
type ExportFailure struct {
	Arn    string
	Name   string
	Reason string
	// UnknownArns is set when the failure was an *ExportError; nil otherwise.
	UnknownArns []string
}

// ExportInstanceResult is export.ts's ExportInstanceResult.
type ExportInstanceResult struct {
	Inventory  InstanceInventory
	ReverseMap ReverseMap
	Flows      []ExportedFlow
	Warnings   []string
	Failures   []ExportFailure
}

// ExportInstanceOptions is export.ts's ExportInstanceOptions, without
// `codegen`.
type ExportInstanceOptions struct {
	CollectInventoryOptions
	// NoSavedFallback fails on a never-published flow instead of reading it
	// through the `$SAVED` alias (TypeScript: savedFallback === false).
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
	NoSavedFallback bool
	// CollectFailures records a flow that cannot be exported in Failures and
	// keeps going (TypeScript: onError "collect"); false aborts on the first,
	// which is what SPEC.md specifies.
	CollectFailures bool
	// Generator is recorded in meta.generator on every exported doc; nil is
	// ExportFlow's default.
	Generator *string
	// Inventory, when set, is used instead of listing.
	Inventory *InstanceInventory
}

// isNotPublished is export.ts's isNotPublished, for Go errors: the error
// code the AWS SDK's smithy.APIError reports.
func isNotPublished(err error) bool {
	var coded interface{ ErrorCode() string }
	return errors.As(err, &coded) && coded.ErrorCode() == "ContactFlowNotPublishedException"
}

// describe is exportInstance's describe: read, and on
// ContactFlowNotPublishedException read again through the $SAVED alias ("Use
// the $SAVED alias in the request to describe the SAVED content of a Flow").
func describe[T any](ctx context.Context, id string, fallback bool, read func(context.Context, string) (T, error)) (T, bool, error) {
	described, err := read(ctx, id)
	if err == nil {
		return described, false, nil
	}
	if !fallback || !isNotPublished(err) {
		var zero T
		return zero, false, err
	}
	described, err = read(ctx, id+":$SAVED")
	return described, true, err
}

// ExportInstance is export.ts's exportInstance: inventory, reverse map, then
// every flow and module in ARN order (JavaScript <, stable). Returns an error
// and no result where the TypeScript rejects.
func ExportInstance(ctx context.Context, client ConnectInventoryClient, options ExportInstanceOptions) (ExportInstanceResult, error) {
	var inventory InstanceInventory
	if options.Inventory != nil {
		inventory = *options.Inventory
	} else {
		var err error
		if inventory, err = CollectInventory(ctx, client, options.CollectInventoryOptions); err != nil {
			return ExportInstanceResult{}, err
		}
	}
	reverseMap := BuildReverseMap(inventory)
	warnings := append([]string{}, reverseMap.Warnings...)
	failures := []ExportFailure{}
	flows := []ExportedFlow{}

	fail := func(arn, name string, err error) error {
		if !options.CollectFailures {
			return err
		}
		failure := ExportFailure{Arn: arn, Name: name, Reason: err.Error()}
		var exportErr *ExportError
		if errors.As(err, &exportErr) {
			failure.UnknownArns = exportErr.UnknownArns
		}
		failures = append(failures, failure)
		return nil
	}

	emit := func(summary ResourceSummary, described DescribedContactFlow, connectType string, saved bool) error {
		entry, ok := LookupArn(reverseMap, summary.Arn)
		if !ok {
			return errors.New("Cannot export " + summary.Arn +
				": the instance inventory has no entry for it, so it has no name.")
		}
		// Provenance, deliberately without the ARN: an exported FlowDoc is an
		// authored document, so it must not carry a literal ARN anywhere.
		source := jsonv.Object{}
		if parsed, ok := ParseConnectArn(summary.Arn); ok {
			source = append(source, jsonv.Member{Key: "instanceId", Value: parsed.InstanceID})
		}
		source = append(source,
			jsonv.Member{Key: "id", Value: described.ID},
			jsonv.Member{Key: "name", Value: described.Name},
		)
		if described.Version != nil {
			source = append(source, jsonv.Member{Key: "version", Value: *described.Version})
		}
		if described.ContentSha256 != nil {
			source = append(source, jsonv.Member{Key: "contentSha256", Value: *described.ContentSha256})
		}
		if saved {
			source = append(source, jsonv.Member{Key: "alias", Value: "$SAVED"})
		}
		description := ""
		if described.Description != nil {
			description = *described.Description
		}
		doc, err := ExportFlow(described.Content, reverseMap, ExportFlowOptions{
			Name:        entry.Name,
			ConnectType: connectType,
			Description: description,
			Generator:   options.Generator,
			Meta:        jsonv.Object{{Key: "source", Value: source}},
		})
		if err != nil {
			return err
		}
		flows = append(flows, ExportedFlow{
			Arn:        summary.Arn,
			ID:         described.ID,
			SourceName: described.Name,
			Doc:        doc,
			Saved:      saved,
		})
		return nil
	}

	contactFlows := append([]ContactFlowSummary(nil), inventory.ContactFlows...)
	sort.SliceStable(contactFlows, func(i, j int) bool {
		return jsonv.LessUTF16(contactFlows[i].Arn, contactFlows[j].Arn)
	})
	for _, summary := range contactFlows {
		typ := "CONTACT_FLOW"
		if summary.ContactFlowType != nil {
			typ = *summary.ContactFlowType
		}
		if !exportableFlowTypes[typ] {
			warnings = append(warnings,
				"Skipping "+summary.Arn+": ContactFlowType "+typ+" has no FlowDoc connectType.")
			continue
		}
		id := summary.Arn
		if summary.ID != nil {
			id = *summary.ID
		}
		described, saved, err := describe(ctx, id, !options.NoSavedFallback, client.DescribeContactFlow)
		if err == nil {
			err = emit(summary.ResourceSummary, described, typ, saved)
		}
		if err != nil {
			if err := fail(summary.Arn, summary.Name, err); err != nil {
				return ExportInstanceResult{}, err
			}
		}
	}

	modules := append([]ContactFlowModuleSummary(nil), inventory.ContactFlowModules...)
	sort.SliceStable(modules, func(i, j int) bool {
		return jsonv.LessUTF16(modules[i].Arn, modules[j].Arn)
	})
	for _, summary := range modules {
		id := summary.Arn
		if summary.ID != nil {
			id = *summary.ID
		}
		module, saved, err := describe(ctx, id, !options.NoSavedFallback, client.DescribeContactFlowModule)
		if err == nil {
			if module.Settings != nil || module.ExternalInvocationEnabled != nil {
				// Neither field has a FlowDoc home yet. Warn rather than drop
				// silently.
				warnings = append(warnings, "Module "+summary.Arn+
					" carries Settings or ExternalInvocationConfiguration, which FlowDoc does not model; they are not exported.")
			}
			err = emit(summary.ResourceSummary, module.DescribedContactFlow, "MODULE", saved)
		}
		if err != nil {
			if err := fail(summary.Arn, summary.Name, err); err != nil {
				return ExportInstanceResult{}, err
			}
		}
	}

	return ExportInstanceResult{
		Inventory:  inventory,
		ReverseMap: reverseMap,
		Flows:      flows,
		Warnings:   warnings,
		Failures:   failures,
	}, nil
}
