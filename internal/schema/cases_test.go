// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/conformance"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// The documents packages/core/src/conformance.test.ts builds by mutating a
// fixture, one table entry per it.each row or it() there, under the same
// labels. Each edit is the TypeScript's, written against a jsonv tree.

const (
	demoFixture        = "demo/appointment-line.flowdoc.json"
	routingFixture     = "roundtrip/contact-routing/doc.flowdoc.json"
	flowControlFixture = "roundtrip/flow-control/doc.flowdoc.json"
	contactDataFixture = "roundtrip/contact-data/doc.flowdoc.json"
	participantFixture = "roundtrip/participant/doc.flowdoc.json"
	recordingFixture   = "roundtrip/recording-analytics/doc.flowdoc.json"
	menuFixture        = "roundtrip/dtmf-menu/doc.flowdoc.json"
	moduleFixture      = "roundtrip/after-call-survey/doc.flowdoc.json"
)

// docCase is one document and the verdict Ajv gives it.
type docCase struct {
	group   string // the describe block
	name    string // the it label
	fixture string
	version string // the schema validated against
	edit    func(doc *any)
	valid   bool
}

func (c docCase) id() string { return c.group + ": " + c.name }

// load reads a conformance file as jsonv.
func load(t testing.TB, p string) any {
	t.Helper()
	b, err := fs.ReadFile(conformance.FS(), p)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonv.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (c docCase) doc(t testing.TB) any {
	d := load(t, c.fixture)
	if c.edit != nil {
		c.edit(&d)
	}
	return d
}

// j is a JSON literal.
func j(s string) any {
	v, err := jsonv.Decode([]byte(s))
	if err != nil {
		panic(err)
	}
	return v
}

// ref is a pointer to the value at a dotted path; array steps are indices.
func ref(v *any, path string) *any {
	if path == "" {
		return v
	}
	for _, tok := range strings.Split(path, ".") {
		switch n := (*v).(type) {
		case jsonv.Object:
			found := false
			for i := range n {
				if n[i].Key == tok {
					v = &n[i].Value
					found = true
					break
				}
			}
			if !found {
				panic("no member " + tok)
			}
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil {
				panic(err)
			}
			v = &n[i]
		default:
			panic("cannot step into " + tok)
		}
	}
	return v
}

// set assigns path's last key on the object its prefix names, as a JavaScript
// assignment does.
func set(v *any, path string, value any) {
	parent, key := "", path
	if i := strings.LastIndex(path, "."); i >= 0 {
		parent, key = path[:i], path[i+1:]
	}
	p := ref(v, parent)
	o := (*p).(jsonv.Object)
	o.Set(key, value)
	*p = o
}

// del is JavaScript's delete.
func del(v *any, path string) {
	parent, key := "", path
	if i := strings.LastIndex(path, "."); i >= 0 {
		parent, key = path[:i], path[i+1:]
	}
	p := ref(v, parent)
	o := (*p).(jsonv.Object)
	o.Delete(key)
	*p = o
}

// setMeta is `doc.meta = { ...doc.meta, [key]: value }`: meta keeps its
// place and members, or is appended when the document has none.
func setMeta(d *any, key string, value any) {
	o := (*d).(jsonv.Object)
	m, ok := o.Get("meta")
	meta, _ := m.(jsonv.Object)
	if !ok {
		meta = jsonv.Object{}
	}
	meta = append(jsonv.Object{}, meta...)
	meta.Set(key, value)
	o.Set("meta", meta)
	*d = o
}

// push is Array.prototype.push.
func push(v *any, path string, value any) {
	p := ref(v, path)
	*p = append((*p).([]any), value)
}

// action is the action with an Identifier, as the TypeScript's at().
func action(d *any, id string) *any {
	actions := ref(d, "content.Actions")
	for i, a := range (*actions).([]any) {
		if got, _ := a.(jsonv.Object).Get("Identifier"); got == id {
			return &(*actions).([]any)[i]
		}
	}
	panic("no action " + id)
}

// on edits one action.
func on(id string, f func(a *any)) func(d *any) {
	return func(d *any) { f(action(d, id)) }
}

// setP is on() with a single assignment.
func setP(id, path string, value any) func(d *any) {
	return on(id, func(a *any) { set(a, path, value) })
}

func docCases() []docCase {
	var cs []docCase
	add := func(group, fixture string, valid bool, rows ...any) {
		for i := 0; i < len(rows); i += 2 {
			edit, _ := rows[i+1].(func(*any))
			cs = append(cs, docCase{group: group, name: rows[i].(string), fixture: fixture, version: "0.2", edit: edit, valid: valid})
		}
	}

	add("FlowDoc schema", demoFixture, true, "validates the demo flow", nil)

	g := "FlowDoc schema rejections"
	add(g, demoFixture, false,
		"rejects a literal ARN in a queue reference", setP("set-working-queue", "Parameters.QueueId", "arn:aws:connect:us-east-1:111122223333:instance/a/queue/b"),
		"rejects a literal ARN in a lambda reference", setP("look-up-appointment", "Parameters.LambdaFunctionARN", "arn:aws:lambda:us-east-1:111122223333:function:f"),
		"rejects a token interpolated into a longer string", setP("set-working-queue", "Parameters.QueueId", "prefix-${cdref:queue:appointments}"),
		"rejects QueueId and AgentId set together", setP("set-working-queue", "Parameters.AgentId", "${cdref:queue:overflow}"),
		"rejects MessageParticipant with both Text and SSML", setP("welcome", "Parameters.SSML", "<speak>hi</speak>"),
		"rejects a Lambda timeout above the documented maximum of 8", setP("look-up-appointment", "Parameters.InvocationTimeLimitSeconds", 30.0),
		"rejects an Identifier containing a character Connect reserves", setP("welcome", "Identifier", "bad/id"),
	)
	add(g, demoFixture, true,
		"still accepts a single JSONPath identifier in a reference field", setP("set-working-queue", "Parameters.QueueId", "$.Attributes.queueId"),
	)

	g = "FlowDoc schema rejections: contact routing"
	add(g, routingFixture, true, "accepts the fixture as committed", nil)
	add(g, routingFixture, false,
		"rejects a callback delay written as a JSON number, which the console never writes", setP("offer-callback", "Parameters.InitialCallDelaySeconds", 60.0),
		"rejects zero connection attempts", setP("offer-callback", "Parameters.MaximumConnectionAttempts", "0"),
		"rejects a retry delay with a leading zero", setP("offer-callback", "Parameters.RetryDelaySeconds", "0600"),
		"rejects a callback with both a queue and an agent queue", setP("offer-callback", "Parameters.AgentId", "${cdref:queue:agents}"),
		"rejects a literal ARN as the callback flow", setP("offer-callback", "Parameters.ContactFlowId", "arn:aws:connect:us-east-1:111122223333:instance/a/contact-flow/b"),
		"rejects a queue priority of zero", setP("bump-priority", "Parameters.QueuePriority", "0"),
		"rejects a queue priority written as a JSON number", setP("bump-priority", "Parameters.QueuePriority", 1.0),
		"rejects a static callback number", setP("set-callback-number", "Parameters.CallbackNumber", "+15555550100"),
		"rejects a priority and a time adjustment together", setP("bump-priority", "Parameters.QueueTimeAdjustmentSeconds", "30"),
		"rejects a queue-to-queue transfer naming both a queue and an agent queue", setP("move-to-priority-queue", "Parameters.AgentId", "${cdref:queue:agents}"),
	)

	g = "FlowDoc schema rejections: flow control"
	add(g, flowControlFixture, true, "accepts the fixture as committed", nil)
	add(g, flowControlFixture, false,
		"rejects a loop count above 100", setP("again", "Parameters.LoopCount", "101"),
		"rejects a loop count written as a JSON number, which the console never writes", setP("again", "Parameters.LoopCount", 2.0),
		"rejects a loop count with a leading zero", setP("again", "Parameters.LoopCount", "02"),
		"rejects a percentage threshold above 100", setP("split", "Transitions.Conditions.0.Condition.Operands", j(`["101"]`)),
		"rejects a percentage branch with an operator other than NumberLessThan", setP("split", "Transitions.Conditions.0.Condition.Operator", "NumberGreaterThan"),
		"rejects a percentage threshold written as a JSON number", setP("split", "Transitions.Conditions.0.Condition.Operands", j(`[20]`)),
		"rejects a percentage branch with two operands", setP("split", "Transitions.Conditions.0.Condition.Operands", j(`["20","30"]`)),
		"rejects a flow attribute as a flat string, which the console never writes", setP("remember", "Parameters.FlowAttributes", j(`{"lastPrompt":"greet"}`)),
		"rejects a flow attribute wrapper with a key other than Value", setP("remember", "Parameters.FlowAttributes", j(`{"lastPrompt":{"Value":"greet","Type":"string"}}`)),
		"rejects a wait of zero seconds", setP("wait-for-customer", "Parameters.TimeLimitSeconds", "0"),
		"rejects a wait written as a JSON number, which the console never writes", setP("wait-for-customer", "Parameters.TimeLimitSeconds", 300.0),
		"rejects a wait event the page does not list", setP("wait-for-customer", "Parameters.Events", j(`["CustomerReturned","LambdaReturned"]`)),
		"rejects a wait event listed twice", setP("wait-for-customer", "Parameters.Events", j(`["CustomerReturned","CustomerReturned"]`)),
		"rejects a metric the page does not list", setP("staffed", "Parameters.MetricType", "NumberOfAgentsHappy"),
		"rejects a metric check naming both a queue and an agent queue", setP("queue-depth", "Parameters.AgentId", "${cdref:queue:agents}"),
		"rejects a metric load for a channel the page does not list", setP("load-metrics", "Parameters.QueueChannel", "Email"),
		"rejects a metric load naming both a queue and an agent queue", setP("load-metrics", "Parameters.AgentId", "${cdref:queue:agents}"),
	)
	add(g, flowControlFixture, true,
		"still accepts a JSONPath loop count and wait timeout", setP("wait-for-customer", "Parameters.TimeLimitSeconds", "$.Attributes.holdSeconds"),
	)

	g = "FlowDoc schema rejections: contact data"
	add(g, contactDataFixture, true, "accepts the fixture as committed", nil)
	add(g, contactDataFixture, false,
		"rejects seven tags", setP("tag", "Parameters.Tags", j(`{"a":"a","b":"b","c":"c","d":"d","e":"e","f":"f","g":"g"}`)),
		"rejects a system tag key", setP("tag", "Parameters.Tags", j(`{"aws:connect:instanceId":"x"}`)),
		"rejects a non-string tag value", setP("tag", "Parameters.Tags", j(`{"count":1}`)),
		"rejects removing a system tag", setP("untag", "Parameters.TagKeys", j(`["aws:connect:instanceId"]`)),
		"rejects an empty key list, which the service refuses", setP("untag", "Parameters.TagKeys", j(`[]`)),
		"rejects a text-to-speech engine the pages do not list", setP("set-voice", "Parameters.TextToSpeechEngine", "premium"),
		"rejects a text-to-speech engine in the admin guide's lower case, which the console never writes", setP("set-voice", "Parameters.TextToSpeechEngine", "neural"),
		"rejects an empty voice name", setP("set-voice", "Parameters.TextToSpeechVoice", ""),
		"rejects a voice authentication threshold above 100", setP("set-data", "Parameters.VoiceAuthenticationThreshold", "101"),
		"rejects a response time below 5 seconds", setP("set-data", "Parameters.VoiceAuthenticationResponseTime", "4"),
		"rejects a lowercase Voice ID flag", setP("set-data", "Parameters.IsVoiceAuthenticationEnabled", "true"),
		"rejects a target the page does not list", setP("set-data", "Parameters.TargetContact", "Flow"),
		"rejects two event hooks in one action", setP("set-queue-flow", "Parameters.EventHooks", j(`{"CustomerQueue":"${cdref:flow:a}","CustomerHold":"${cdref:flow:b}"}`)),
		"rejects an event hook the page does not list", setP("set-queue-flow", "Parameters.EventHooks", j(`{"AgentQueue":"${cdref:flow:a}"}`)),
		"rejects a literal ARN as an event hook's flow", setP("set-queue-flow", "Parameters.EventHooks", j(`{"CustomerQueue":"arn:aws:connect:us-east-1:111122223333:instance/a/contact-flow/b"}`)),
	)

	g = "FlowDoc schema rejections: participant"
	add(g, participantFixture, true, "accepts the fixture as committed", nil)
	add(g, participantFixture, false,
		"rejects a loop message with two bodies", setP("hold-music", "Parameters.Messages", j(`[{"Text":"hi","SSML":"<speak>hi</speak>"}]`)),
		"rejects a loop with no messages", setP("hold-music", "Parameters.Messages", j(`[]`)),
		"rejects an interrupt frequency written as a JSON number", setP("hold-music", "Parameters.InterruptFrequencySeconds", 30.0),
		"rejects a media message from somewhere other than S3", setP("hold-music", "Parameters.Messages", j(`[{"Media":{"Uri":"s3://b/x","SourceType":"HTTP","MediaType":"Audio"}}]`)),
		"rejects a Lex bot with both a text and an SSML body", setP("ask-intent", "Parameters.SSML", "<speak>hi</speak>"),
		"rejects a Lex bot alias as a literal ARN", setP("ask-intent", "Parameters.LexV2Bot", j(`{"AliasArn":"arn:aws:lex:us-east-1:111122223333:bot-alias/BOT/ALIAS"}`)),
		"rejects a Lex timeout written as a JSON number", setP("ask-intent", "Parameters.LexTimeoutSeconds", j(`{"Text":300}`)),
		"rejects a Lex timeout below the console's one minute", setP("ask-intent", "Parameters.LexTimeoutSeconds", j(`{"Text":"59"}`)),
		"rejects a Lex timeout above the console's seven days", setP("ask-intent", "Parameters.LexTimeoutSeconds", j(`{"Text":"604801"}`)),
		"rejects a Lex action with no bot at all", on("ask-intent", func(a *any) { del(a, "Parameters.LexV2Bot") }),
		"rejects a view resource with no id", setP("show-form", "Parameters.ViewResource", j(`{"Version":"1"}`)),
		"rejects a view id as a literal ARN", setP("show-form", "Parameters.ViewResource", j(`{"Id":"arn:aws:connect:us-west-2:aws:view/form:1"}`)),
		"rejects a view time limit written as a JSON number", setP("show-form", "Parameters.InvocationTimeLimitSeconds", 300.0),
		"rejects a view without a time limit, which the service refuses", on("show-form", func(a *any) { del(a, "Parameters.InvocationTimeLimitSeconds") }),
	)

	g = "FlowDoc schema rejections: recording and analytics"
	voice := "Parameters.VoiceBehavior.VoiceRecordingBehavior."
	add(g, recordingFixture, true, "accepts the fixture as committed, chat form included", nil)
	add(g, recordingFixture, false,
		"rejects a recorded participant other than Agent or Customer", setP("record-voice-ivr", voice+"RecordedParticipants", j(`["Supervisor"]`)),
		"rejects a participant recorded twice", setP("record-voice-ivr", voice+"RecordedParticipants", j(`["Agent","Agent"]`)),
		"rejects an IVR recording value other than Enabled or Disabled", setP("record-voice-ivr", voice+"IVRRecordingBehavior", "On"),
		"rejects a screen recorded participant other than Agent", setP("record-screen", "Parameters.ScreenRecordingBehavior", j(`{"ScreenRecordedParticipants":["Customer"]}`)),
		"rejects a chat and a voice behavior on one block", setP("record-voice-ivr", "Parameters.ChatBehavior", j(`{"ChatAnalyticsBehavior":{"Enabled":"True"}}`)),
		"rejects a voice and a screen behavior on one block, which the service refuses", setP("record-voice-ivr", "Parameters.ScreenRecordingBehavior", j(`{"ScreenRecordedParticipants":["Agent"]}`)),
		"rejects a voice behavior with no recording object", setP("record-voice-ivr", "Parameters.VoiceBehavior", j(`{}`)),
		"rejects a screen behavior with no participant list", setP("record-screen", "Parameters.ScreenRecordingBehavior", j(`{}`)),
		"rejects no behavior at all, which the service refuses", setP("record-screen", "Parameters", j(`{}`)),
	)

	g = "FlowDoc schema rejections: GetParticipantInput"
	add(g, menuFixture, false,
		"rejects StoreInput that is neither True nor False", setP("menu", "Parameters.StoreInput", "Maybe"),
		"rejects a timeout of zero, as a string", setP("menu", "Parameters.InputTimeLimitSeconds", "0"),
		"rejects a timeout of zero, as an integer", setP("menu", "Parameters.InputTimeLimitSeconds", 0.0),
		"rejects a timeout with a leading zero", setP("menu", "Parameters.InputTimeLimitSeconds", "05"),
		"rejects a fractional timeout", setP("menu", "Parameters.InputTimeLimitSeconds", 2.5),
		"rejects Text and SSML together", setP("menu", "Parameters.SSML", "<speak>hi</speak>"),
		"rejects Text and PromptId together", setP("menu", "Parameters.PromptId", "${cdref:prompt:p}"),
		"rejects a literal ARN in PromptId", on("menu", func(a *any) {
			del(a, "Parameters.Text")
			set(a, "Parameters.PromptId", "arn:aws:connect:us-east-1:111122223333:instance/a/prompt/b")
		}),
	)
	add(g, menuFixture, true,
		"still accepts an integer timeout, which Connect's own exports may carry", setP("menu", "Parameters.InputTimeLimitSeconds", 5.0),
		"still accepts no StoreInput, which the action page makes optional", on("menu", func(a *any) { del(a, "Parameters.StoreInput") }),
		"still accepts a JSONPath identifier in PromptId", on("menu", func(a *any) {
			del(a, "Parameters.Text")
			set(a, "Parameters.PromptId", "$.Attributes.menuPrompt")
		}),
		"still accepts no prompt at all", on("menu", func(a *any) { del(a, "Parameters.Text") }),
	)

	// { ...base, kind, connectType } keeps base's key order, overwriting in place.
	g = "kind and connectType agree"
	add(g, demoFixture, true, "accepts a flow with a non-module connectType", func(d *any) {
		set(d, "kind", "flow")
		set(d, "connectType", "CONTACT_FLOW")
	})
	add(g, moduleFixture, true, "accepts a module with connectType MODULE", nil)
	add(g, demoFixture, false,
		"rejects a module whose connectType is not MODULE", func(d *any) {
			set(d, "kind", "module")
			set(d, "connectType", "CONTACT_FLOW")
		},
		"rejects a flow whose connectType is MODULE", func(d *any) {
			set(d, "kind", "flow")
			set(d, "connectType", "MODULE")
		},
	)

	// Not in the TypeScript: inputs chosen to reach every message form and
	// the ECMA-262 pattern semantics RE2 does not share, held to Ajv by the
	// oracle.
	g = "Go port: Ajv parity"
	add(g, contactDataFixture, false,
		"a tag key that is a lone carriage return, which ECMA-262's . refuses", setP("untag", "Parameters.TagKeys", j(`["\r"]`)),
		"a tag key that is U+2028", setP("untag", "Parameters.TagKeys", j(`["\u2028"]`)),
		"two system tag keys and a non-string value", setP("tag", "Parameters.Tags", j(`{"aws:a":"x","ok":1,"aws:b":"y"}`)),
		"a text-to-speech engine that is a number", setP("set-voice", "Parameters.TextToSpeechEngine", 5.0),
	)
	add(g, contactDataFixture, true,
		"a tag key with a newline inside, which ECMA-262's . allows after the first character", setP("untag", "Parameters.TagKeys", j(`["a\nb"]`)),
		"a tag key that only contains aws: later", setP("untag", "Parameters.TagKeys", j(`["x-aws:y"]`)),
		"a tag key ending in a carriage return", setP("untag", "Parameters.TagKeys", j(`["a\r"]`)),
	)
	add(g, flowControlFixture, false,
		"a wait event list with two repeated pairs", setP("wait-for-customer", "Parameters.Events", j(`["CustomerReturned","BotParticipantDisconnected","CustomerReturned","BotParticipantDisconnected"]`)),
		"a percentage branch with no operands", setP("split", "Transitions.Conditions.0.Condition.Operands", j(`[]`)),
	)
	add(g, demoFixture, false,
		"an Identifier Connect reserves as a prototype name", setP("welcome", "Identifier", "__proto__"),
		"an Identifier over 50 characters", setP("welcome", "Identifier", strings.Repeat("x", 51)),
		"an Identifier that is a number", setP("welcome", "Identifier", 7.0),
		"an empty Identifier", setP("welcome", "Identifier", ""),
		"two unknown top-level keys", func(d *any) {
			set(d, "zeta", 1.0)
			set(d, "alpha", 2.0)
		},
		"a missing name and content", func(d *any) {
			del(d, "name")
			del(d, "content")
		},
		"an unknown key in an action and in its transitions", on("welcome", func(a *any) {
			set(a, "Extra", true)
			set(a, "Transitions.Bogus", nil)
		}),
		"a layout point with a string coordinate and an extra key", func(d *any) {
			set(d, "layout.welcome", j(`{"x":"1","y":2,"z":3}`))
		},
		"a malformed source hash", func(d *any) { setMeta(d, "sourceHash", "sha256:XYZ") },
		"a description over 500 characters", func(d *any) { set(d, "description", strings.Repeat("\u00e9", 501)) },
		"a wrong content version", func(d *any) { set(d, "content.Version", "2020-01-01") },
		"no actions", func(d *any) { set(d, "content.Actions", j(`[]`)) },
	)
	add(g, demoFixture, true,
		"a description of exactly 500 astral characters", func(d *any) { set(d, "description", strings.Repeat("\U0001F600", 500)) },
	)

	g = "FlowDoc migration"
	withView := func(d *any) {
		set(d, "description", "The demo appointment line.")
		setMeta(d, "sourceKind", "tf")
		push(d, "content.Actions", j(`{"Identifier":"show-acw","Type":"ShowView","Parameters":{"ViewResource":{"Id":"${cdref:view:after-contact-work@1}"},"InvocationTimeLimitSeconds":"300"},"Transitions":{}}`))
		push(d, "refs", j(`{"token":"${cdref:view:after-contact-work@1}","type":"view","name":"after-contact-work","alias":"1"}`))
	}
	add(g, demoFixture, true, "0.2 accepts what 0.1 could not say: a view token, a version pin on it, sourceKind, description", withView)
	cs = append(cs, docCase{group: g, name: "0.1 refuses the same document", fixture: demoFixture, version: "0.1", edit: func(d *any) {
		withView(d)
		set(d, "flowdoc", "0.1")
	}})
	add(g, demoFixture, false, "0.2 still refuses a sourceKind it does not know", func(d *any) { setMeta(d, "sourceKind", "yaml") })
	for _, m := range []string{"minimal-0.1", "with-meta-0.1"} {
		in, out := "migrate/"+m+"/input.flowdoc.json", "migrate/"+m+"/expected.flowdoc.json"
		cs = append(cs,
			docCase{group: g, name: m + ": the input is valid at its own version", fixture: in, version: "0.1", valid: true},
			docCase{group: g, name: m + ": the input is not valid at 0.2", fixture: in, version: "0.2"},
			docCase{group: g, name: m + ": the migrated document is valid at 0.2", fixture: out, version: "0.2", valid: true},
			docCase{group: g, name: m + ": the migrated document is not valid at 0.1", fixture: out, version: "0.1"},
		)
	}
	return cs
}
