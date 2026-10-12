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
//
// Added for FlowDoc 0.3, each optional on the client (TaskTemplateLister,
// PhoneNumberLister, CasesDomainDescriber, AssistantLister):
//
//	ListTaskTemplates           https://docs.aws.amazon.com/connect/latest/APIReference/API_ListTaskTemplates.html
//	ListPhoneNumbersV2          https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPhoneNumbersV2.html
//	ListIntegrationAssociations https://docs.aws.amazon.com/connect/latest/APIReference/API_ListIntegrationAssociations.html
//	Cases ListTemplates         https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListTemplates.html
//	Cases ListFields            https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_ListFields.html
//	Q in Connect ListAssistants https://docs.aws.amazon.com/amazon-q-connect/latest/APIReference/API_ListAssistants.html
package export

import "strings"

// ConnectArnRefTypes is export.ts's CONNECT_ARN_REF_TYPES: ARN type keyword to
// FlowDoc ref type, for every Connect resource ARN a reference can stand for:
// the instance-nested ones, the AWS-managed view and the phone number.
// Lambda, Lex, Cases and Q in Connect resources are other services' ARNs and
// have parsers of their own. Two keywords do not match their IAM
// resource-type names: a contact-flow-module is `flow-module` in the ARN,
// and an hours-of-operation is `operating-hours`.
// https://servicereference.us-east-1.amazonaws.com/v1/connect/connect.json
var ConnectArnRefTypes = map[string]string{
	"contact-flow":    "flow",
	"flow-module":     "module",
	"queue":           "queue",
	"operating-hours": "hours",
	"prompt":          "prompt",
	"view":            "view",
	"task-template":   "tasktemplate",
	"phone-number":    "phonenumber",
}

// RefTypeArnKeywords is export.ts's REF_TYPE_ARN_KEYWORDS, the inverse of
// ConnectArnRefTypes.
var RefTypeArnKeywords = map[string]string{
	"flow":         "contact-flow",
	"module":       "flow-module",
	"queue":        "queue",
	"hours":        "operating-hours",
	"prompt":       "prompt",
	"view":         "view",
	"tasktemplate": "task-template",
	"phonenumber":  "phone-number",
}

// ConnectArn is export.ts's ConnectArn. The TypeScript's optional fields are
// a value plus a Has flag, because an empty string is a value they can hold.
type ConnectArn struct {
	Partition string
	Region    string
	// Account is digits, or `aws` for an AWS-managed view.
	Account string
	// InstanceID is empty for an AWS-managed view or a phone number, which
	// belong to no instance.
	InstanceID string
	// HasResource is true when ResourceType and ResourceID are set
	// (TypeScript: resourceType !== undefined).
	HasResource bool
	// ResourceType is the ARN type keyword, e.g. `contact-flow`,
	// `flow-module`, `operating-hours`, `view`, `phone-number`.
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
// nests under the instance ARN, except two. An AWS-managed view,
// `arn:aws:connect:<region>:aws:view/<name>:<version>`, belongs to no
// instance and no account
// (https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html).
// A claimed phone number, `arn:aws:connect:<region>:<account>:phone-number/<id>`,
// belongs to the account and is routed to an instance or a traffic
// distribution group (the service reference's ARN format; ListPhoneNumbersV2
// returns it as PhoneNumberArn).
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListPhoneNumbersV2.html
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
	if segments[0] == "phone-number" && len(segments) == 2 && segments[1] != "" {
		// A claimed phone number: the account's, nested under no instance.
		return ConnectArn{
			Partition:    parts[1],
			Region:       parts[3],
			Account:      parts[4],
			HasResource:  true,
			ResourceType: "phone-number",
			ResourceID:   segments[1],
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

// CasesArn is export.ts's CasesArn: a Cases resource ARN, the domain or a
// template or field inside it. ResourceType is "" for the domain itself
// (TypeScript: undefined), else "template" or "field".
type CasesArn struct {
	Partition    string
	Region       string
	Account      string
	DomainID     string
	ResourceType string
	ResourceID   string
}

// ParseCasesArn is export.ts's parseCasesArn. Cases is its own service,
// `cases`, and its resources nest under a domain rather than a Connect
// instance (the service reference's ARN formats, read 2026-10-05 by the
// TypeScript):
//
//	arn:aws:cases:<region>:<account>:domain/<domainId>
//	arn:aws:cases:<region>:<account>:domain/<domainId>/template/<templateId>
//	arn:aws:cases:<region>:<account>:domain/<domainId>/field/<fieldId>
//
// The instance's domain is named by its CASES_DOMAIN integration association
// (IntegrationArn), which is how the inventory finds it.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListIntegrationAssociations.html
func ParseCasesArn(arn string) (CasesArn, bool) {
	parts := strings.Split(arn, ":")
	if len(parts) != 6 || parts[0] != "arn" || parts[2] != "cases" {
		return CasesArn{}, false
	}
	segments := strings.Split(parts[5], "/")
	if segments[0] != "domain" || len(segments) < 2 || segments[1] == "" {
		return CasesArn{}, false
	}
	parsed := CasesArn{Partition: parts[1], Region: parts[3], Account: parts[4], DomainID: segments[1]}
	if len(segments) == 2 {
		return parsed, true
	}
	if len(segments) != 4 || (segments[2] != "template" && segments[2] != "field") || segments[3] == "" {
		return CasesArn{}, false
	}
	parsed.ResourceType = segments[2]
	parsed.ResourceID = segments[3]
	return parsed, true
}

// ParseAssistantArn is export.ts's parseAssistantArn: the assistant id out of
// an Amazon Q in Connect assistant ARN. The service keeps its original name
// in the ARN, `wisdom`:
//
//	arn:aws:wisdom:<region>:<account>:assistant/<assistantId>
//
// CreateWisdomSession.WisdomAssistantArn holds exactly this.
// https://docs.aws.amazon.com/amazon-q-connect/latest/APIReference/API_ListAssistants.html
func ParseAssistantArn(arn string) (string, bool) {
	parts := strings.Split(arn, ":")
	if len(parts) != 6 || parts[0] != "arn" || parts[2] != "wisdom" {
		return "", false
	}
	segments := strings.Split(parts[5], "/")
	if segments[0] != "assistant" || len(segments) != 2 || segments[1] == "" {
		return "", false
	}
	return segments[1], true
}

// NormalizeArn is export.ts's normalizeArn: drops the trailing qualifier a
// reference may carry, so `:$SAVED` or a version suffix resolves to the same
// reverse-map entry as the bare resource. A Cases or Q in Connect ARN
// carries none and is returned as is.
//
// A phone number has two ARN forms. The service reference
// (https://servicereference.us-east-1.amazonaws.com/v1/connect/connect.json)
// lists `phone-number`, `arn:<partition>:connect:<region>:<account>:phone-number/<id>`,
// the form ListPhoneNumbersV2 returns, and `legacy-phone-number`,
// `arn:<partition>:connect:<region>:<account>:instance/<instanceId>/phone-number/<id>`,
// the form tied to the V1 ListPhoneNumbers. The legacy form folds onto the
// un-nested one, so a flow saved with either resolves to the same entry.
func NormalizeArn(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[0] != "arn" {
		return arn
	}
	if parts[2] == "connect" {
		segments := strings.Split(parts[5], "/")
		if len(segments) == 4 && segments[0] == "instance" && segments[2] == "phone-number" &&
			segments[1] != "" && segments[3] != "" {
			return strings.Join(parts[:5], ":") + ":phone-number/" + segments[3]
		}
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
