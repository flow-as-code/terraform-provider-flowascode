// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package provider

// What the flow and module resources share: reading a configuration to its
// FlowDoc, what the plan says about it, materializing it, and tags.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/connectapi"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowmodel"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/lint"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/materialize"
	fdschema "github.com/flow-as-code/terraform-provider-flowascode/internal/schema"
)

// connectTypes are the ConnectType values a flow resource takes; MODULE is
// the module resource's.
var connectTypes = []string{
	"CONTACT_FLOW", "CUSTOMER_QUEUE", "CUSTOMER_HOLD", "CUSTOMER_WHISPER", "AGENT_HOLD",
	"AGENT_WHISPER", "OUTBOUND_WHISPER", "AGENT_TRANSFER", "QUEUE_TRANSFER",
}

func keep() []planmodifier.String {
	return []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
}

// read decodes a whole configuration or plan and reads it to a FlowDoc.
func read(raw tftypes.Value, kind string, phase flowmodel.Phase) (flowmodel.Result, error) {
	v, err := flowmodel.Decode(raw)
	if err != nil {
		return flowmodel.Result{}, err
	}
	m, _ := v.(map[string]any)
	return flowmodel.FromConfig(m, kind, phase), nil
}

func addProblems(diags *diag.Diagnostics, problems []flowmodel.Problem) {
	for _, p := range problems {
		diags.AddAttributeError(p.Attr, p.Summary(), p.Detail)
	}
}

// planned is what ModifyPlan computes from a configuration: the FlowDoc, and
// the content once every reference is bound to a known value.
type planned struct {
	flowdoc     string
	content     string
	contentKnow bool
	complete    bool
}

// plan reads a configuration at plan time and reports what lint, the schema
// and the refs bindings say about it.
func plan(raw tftypes.Value, kind string, diags *diag.Diagnostics) planned {
	res, err := read(raw, kind, flowmodel.PhasePlan)
	if err != nil {
		diags.AddError("Cannot read the configuration", err.Error())
		return planned{}
	}
	// Problems found in the validate walk stop the run before a plan; here
	// the only new ones are expressions in reference fields.
	for _, p := range res.Problems {
		if p.Code == "REF_EXPRESSION_REFUSED" {
			diags.AddAttributeError(p.Attr, p.Summary(), p.Detail)
		}
	}
	if res.Doc == nil || diags.HasError() {
		return planned{}
	}
	doc := res.Doc
	violations, err := fdschema.Validate(doc)
	if err != nil {
		diags.AddError("Cannot validate the FlowDoc", err.Error())
		return planned{}
	}
	for _, v := range violations {
		diags.AddError("FlowDoc schema", fmt.Sprintf("%s %s", pointerOrRoot(v.InstancePath), v.Message))
	}
	findings, err := lint.Lint([]any{doc}, lint.Options{Disable: res.LintDisable})
	if err != nil {
		diags.AddError("Cannot lint the FlowDoc", err.Error())
		return planned{}
	}
	hard := map[string]bool{}
	for _, rule := range lint.AllRules() {
		hard[rule.ID] = rule.Hard
	}
	for _, f := range findings {
		detail := f.Message
		if f.BlockID != nil {
			detail = fmt.Sprintf("action %q: %s", *f.BlockID, f.Message)
		}
		// Settled decision 8: a hard rule fails the plan; every other finding
		// is a warning, whatever its severity, since the lint panel and
		// flow-cli lint already say it.
		if hard[f.Rule] {
			diags.AddError(f.Rule, detail)
		} else {
			diags.AddWarning(f.Rule, detail)
		}
	}

	// Every reference the flow uses needs a binding (rule 6); a binding no
	// action uses is a warning (rule 22).
	bindings := map[string]string{}
	known := true
	used := map[string]bool{}
	for _, e := range flowdoc.CollectRefs(doc) {
		key := flowdoc.RefKey(e)
		used[key] = true
		switch v := res.Refs[key].(type) {
		case string:
			bindings[key] = v
		case flowmodel.Unknown:
			known = false
		default:
			diags.AddAttributeError(path.Root("refs").AtMapKey(key), "unbound reference",
				fmt.Sprintf("No terraform address for %s. Bind it in refs: %q = <expression>.", e.Token, key))
		}
	}
	for key := range res.Refs {
		if !used[key] {
			diags.AddAttributeWarning(path.Root("refs").AtMapKey(key), "unused reference",
				fmt.Sprintf("refs[%q] is referenced by no action.", key))
		}
	}
	out := planned{flowdoc: string(flowdoc.Serialize(doc)), complete: true}
	if diags.HasError() || !known {
		return out
	}
	content, err := contentOf(doc, bindings)
	if err != nil {
		diags.AddError("Cannot materialize the flow", err.Error())
		return out
	}
	out.content, out.contentKnow = content, true
	return out
}

func pointerOrRoot(p string) string {
	if p == "" {
		return "(document)"
	}
	return p
}

// contentOf is the Flow language content Connect receives: every token bound
// to its ARN and the layout projected into Metadata.
func contentOf(doc jsonv.Object, bindings map[string]string) (string, error) {
	content, err := materialize.MaterializeWithMap(doc, bindings)
	if err != nil {
		return "", err
	}
	b, err := materialize.SerializeContent(content)
	return string(b), err
}

// hashOf is content_hash: the sha256 of the content in canonical form without
// Metadata, so it names what the flow does rather than how Connect or the
// canvas laid it out, and a module version resource can compare it with the
// module's live content however Connect stored it.
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(normalizedContent(content)))
	return hex.EncodeToString(sum[:])
}
func setPlanned(ctx context.Context, p planned, plan interface {
	SetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) {
	if p.complete {
		diags.Append(plan.SetAttribute(ctx, path.Root("flowdoc"), types.StringValue(p.flowdoc))...)
	} else {
		diags.Append(plan.SetAttribute(ctx, path.Root("flowdoc"), types.StringUnknown())...)
	}
	if p.contentKnow {
		diags.Append(plan.SetAttribute(ctx, path.Root("content"), types.StringValue(p.content))...)
		diags.Append(plan.SetAttribute(ctx, path.Root("content_hash"), types.StringValue(hashOf(p.content)))...)
	} else {
		diags.Append(plan.SetAttribute(ctx, path.Root("content"), types.StringUnknown())...)
		diags.Append(plan.SetAttribute(ctx, path.Root("content_hash"), types.StringUnknown())...)
	}
}

func tagsOf(ctx context.Context, m types.Map) map[string]string {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out
	}
	_ = m.ElementsAs(ctx, &out, false)
	return out
}

type attrValue = interface{}

// userTags drops the tags AWS manages itself (aws: prefix).
func userTags(tags map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range tags {
		if !strings.HasPrefix(k, "aws:") {
			out[k] = v
		}
	}
	return out
}

// sameContent compares two Flow language documents without Metadata, in
// canonical form.
func sameContent(a, b string) bool {
	return normalizedContent(a) == normalizedContent(b)
}

// normalizedContent is a Flow language document in canonical form without
// Metadata, or the text itself when it does not parse.
func normalizedContent(s string) string {
	v, err := jsonv.Decode([]byte(s))
	o, ok := v.(jsonv.Object)
	if err != nil || !ok {
		return s
	}
	o.Delete("Metadata")
	c, err := materialize.SerializeContent(o)
	if err != nil {
		return s
	}
	return string(c)
}

// syncTags applies the difference between two tag maps.
func syncTags(ctx context.Context, api connectapi.API, arn string, before, after map[string]string) error {
	var remove []string
	for k := range before {
		if _, ok := after[k]; !ok {
			remove = append(remove, k)
		}
	}
	sort.Strings(remove)
	add := map[string]string{}
	for k, v := range after {
		if before[k] != v {
			add[k] = v
		}
	}
	if len(remove) > 0 {
		if _, err := api.UntagResource(ctx, &connect.UntagResourceInput{ResourceArn: aws.String(arn), TagKeys: remove}); err != nil {
			return err
		}
	}
	if len(add) > 0 {
		if _, err := api.TagResource(ctx, &connect.TagResourceInput{ResourceArn: aws.String(arn), Tags: add}); err != nil {
			return err
		}
	}
	return nil
}
