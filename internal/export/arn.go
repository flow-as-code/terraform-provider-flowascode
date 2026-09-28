// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Package export is the pure half of @flow-as-code/core's export.ts: a live
// Amazon Connect instance's inventory and flow content in, FlowDocs out. ARN
// parsing, the reverse map from ARN to reference token, the ARN rewrite, the
// Metadata lift, and the document assembly are ported one to one; the AWS SDK
// adapter is not, so ConnectInventoryClient is an interface that a caller (or
// a test replaying conformance/export) implements.
//
// Documents are jsonv values, not structs, because the bytes are the
// contract: ExportFlow returns the canonical document flowdoc.Serialize
// writes byte for byte as the TypeScript's serialize does.
//
// Operations the client interface stands for, as export.ts lists them:
//
//	ListContactFlows           https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlows.html
//	DescribeContactFlow        https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
//	ListContactFlowModules     https://docs.aws.amazon.com/connect/latest/APIReference/API_ListContactFlowModules.html
//	DescribeContactFlowModule  https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlowModule.html
//	ListQueues                 https://docs.aws.amazon.com/connect/latest/APIReference/API_ListQueues.html
//	ListHoursOfOperations      https://docs.aws.amazon.com/connect/latest/APIReference/API_ListHoursOfOperations.html
//	ListPrompts                https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPrompts.html
//	ListLambdaFunctions        https://docs.aws.amazon.com/connect/latest/APIReference/API_ListLambdaFunctions.html
//	ListBots                   https://docs.aws.amazon.com/connect/latest/APIReference/API_ListBots.html
//	ListViews                  https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
package export

import "strings"

// ConnectArnRefTypes is export.ts's CONNECT_ARN_REF_TYPES: ARN type keyword to
// FlowDoc ref type, for resources nested under an instance. Two keywords do
// not match their IAM resource-type names: a contact-flow-module is
// `flow-module` in the ARN, and an hours-of-operation is `operating-hours`.
// https://servicereference.us-east-1.amazonaws.com/v1/connect/connect.json
var ConnectArnRefTypes = map[string]string{
	"contact-flow":    "flow",
	"flow-module":     "module",
	"queue":           "queue",
	"operating-hours": "hours",
	"prompt":          "prompt",
	"view":            "view",
}

// RefTypeArnKeywords is export.ts's REF_TYPE_ARN_KEYWORDS, the inverse of
// ConnectArnRefTypes.
var RefTypeArnKeywords = map[string]string{
	"flow":   "contact-flow",
	"module": "flow-module",
	"queue":  "queue",
	"hours":  "operating-hours",
	"prompt": "prompt",
	"view":   "view",
}

// ConnectArn is export.ts's ConnectArn. The TypeScript's optional fields are
// a value plus a Has flag, because an empty string is a value they can hold.
type ConnectArn struct {
	Partition string
	Region    string
	// Account is digits, or `aws` for an AWS-managed view.
	Account string
	// InstanceID is empty for an AWS-managed view, which belongs to no
	// instance.
	InstanceID string
	// HasResource is true when ResourceType and ResourceID are set
	// (TypeScript: resourceType !== undefined).
	HasResource bool
	// ResourceType is the ARN type keyword, e.g. `contact-flow`,
	// `flow-module`, `operating-hours`, `view`.
	ResourceType string
	ResourceID   string
	// HasQualifier is true when the ARN carried a trailing colon qualifier.
	HasQualifier bool
	// Qualifier is `$SAVED` or a version number on a flow or module ARN, the
	// view's version on a view ARN.
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
	Qualifier string
}

// ParseConnectArn is export.ts's parseConnectArn: every Connect resource ARN
// nests under the instance ARN, except an AWS-managed view,
// `arn:aws:connect:<region>:aws:view/<name>:<version>`.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
// ok is false where the TypeScript returns undefined.
func ParseConnectArn(arn string) (ConnectArn, bool) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return ConnectArn{}, false
	}
	if parts[0] != "arn" || parts[2] != "connect" {
		return ConnectArn{}, false
	}
	segments := strings.Split(parts[5], "/")
	hasQualifier := len(parts) > 6
	qualifier := ""
	if hasQualifier {
		qualifier = strings.Join(parts[6:], ":")
	}
	if segments[0] == "view" && len(segments) >= 2 && segments[1] != "" {
		// An AWS-managed view: no instance, and `aws` where an account id
		// would be.
		return ConnectArn{
			Partition:    parts[1],
			Region:       parts[3],
			Account:      parts[4],
			HasResource:  true,
			ResourceType: "view",
			ResourceID:   strings.Join(segments[1:], "/"),
			HasQualifier: hasQualifier,
			Qualifier:    qualifier,
		}, true
	}
	if segments[0] != "instance" || len(segments) < 2 {
		return ConnectArn{}, false
	}
	instanceID := segments[1]
	if instanceID == "" {
		return ConnectArn{}, false
	}
	parsed := ConnectArn{
		Partition:    parts[1],
		Region:       parts[3],
		Account:      parts[4],
		InstanceID:   instanceID,
		HasQualifier: hasQualifier,
		Qualifier:    qualifier,
	}
	if len(segments) >= 4 {
		parsed.HasResource = true
		parsed.ResourceType = segments[2]
		parsed.ResourceID = strings.Join(segments[3:], "/")
	}
	return parsed, true
}

// ParseLambdaFunctionArn is export.ts's parseLambdaFunctionArn: the function
// name out of a native Lambda ARN, any version or alias qualifier dropped.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListLambdaFunctions.html
func ParseLambdaFunctionArn(arn string) (string, bool) {
	parts := strings.Split(arn, ":")
	if len(parts) < 7 || parts[0] != "arn" || parts[2] != "lambda" {
		return "", false
	}
	if parts[5] != "function" {
		return "", false
	}
	if parts[6] == "" {
		return "", false
	}
	return parts[6], true
}

// NormalizeArn is export.ts's normalizeArn: drops the trailing qualifier a
// reference may carry, so `:$SAVED` or a version suffix resolves to the same
// reverse-map entry as the bare resource.
func NormalizeArn(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[0] != "arn" {
		return arn
	}
	if parts[2] == "connect" {
		return strings.Join(parts[:6], ":")
	}
	if parts[2] == "lambda" && parts[5] == "function" {
		// JavaScript's slice(0, 7) stops at the end of a shorter array.
		end := 7
		if len(parts) < end {
			end = len(parts)
		}
		return strings.Join(parts[:end], ":")
	}
	return arn
}
