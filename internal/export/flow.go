// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/flowdoc"
	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// jsSpace is JavaScript's \s (WhiteSpace plus LineTerminator). RE2's \s is
// ASCII only and lacks \v, so export.ts's \S cannot be written as \S here.
// https://tc39.es/ecma262/#sec-characterclassescape
const jsSpace = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// arnAccount is export.ts's ARN_ACCOUNT: digits for a customer resource, the
// literal `aws` for an AWS-managed one, such as the view
// `arn:aws:connect:<region>:aws:view/after-contact-work:1`.
// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
const arnAccount = `(?:[0-9]*|aws)`

// wholeArn is export.ts's WHOLE_ARN: a whole field value that is an ARN, the
// only replaceable shape.
var wholeArn = regexp.MustCompile(`^arn:aws[a-z0-9-]*:[a-z0-9-]*:[a-z0-9-]*:` + arnAccount + `:[^` + jsSpace + `]+$`)

// arnAnywhere is export.ts's ARN_ANYWHERE: an ARN inside a longer string,
// never replaceable. RE2's leftmost-first matching picks the same matches as
// JavaScript's backtracking for this pattern.
var arnAnywhere = regexp.MustCompile(`arn:aws[a-z0-9-]*:[a-z0-9-]*:[a-z0-9-]*:` + arnAccount + `:[^` + jsSpace + `"']+`)

// ExportError is export.ts's ExportError: export failed, with both lists
// complete and sorted, so a caller fixes the inventory once rather than one
// ARN per run.
type ExportError struct {
	// UnknownArns occupy a whole field with no entry in the reverse map.
	UnknownArns []string
	// InterpolatedArns are embedded in a longer string; a reference occupies
	// an entire field value (FlowDoc invariant 4), so these cannot become
	// tokens at all.
	InterpolatedArns []string
	// Locations maps each ARN to the content paths it was found at.
	Locations map[string][]string
	// Resource is the FlowDoc name being exported; "" is none (TypeScript:
	// undefined).
	Resource string
	message  string
}

// NewExportError is ExportError's constructor, which writes the message.
func NewExportError(unknownArns, interpolatedArns []string, locations map[string][]string, resource string) *ExportError {
	var parts []string
	if len(unknownArns) > 0 {
		parts = append(parts, strconv.Itoa(len(unknownArns))+" ARN(s) not found in the instance inventory: "+
			strings.Join(unknownArns, ", "))
	}
	if len(interpolatedArns) > 0 {
		parts = append(parts, strconv.Itoa(len(interpolatedArns))+
			" ARN(s) embedded in a longer string, which cannot hold a reference: "+
			strings.Join(interpolatedArns, ", "))
	}
	named := ""
	if resource != "" {
		named = ` "` + resource + `"`
	}
	return &ExportError{
		UnknownArns:      unknownArns,
		InterpolatedArns: interpolatedArns,
		Locations:        locations,
		Resource:         resource,
		message:          "Cannot export" + named + ": " + strings.Join(parts, "; "),
	}
}

func (e *ExportError) Error() string { return e.message }

// arnPaths is a Map<string, string[]>: insertion-ordered keys.
type arnPaths struct {
	keys  []string
	paths map[string][]string
}

func (a *arnPaths) record(arn, at string) {
	if a.paths == nil {
		a.paths = map[string][]string{}
	}
	if _, ok := a.paths[arn]; !ok {
		a.keys = append(a.keys, arn)
	}
	a.paths[arn] = append(a.paths[arn], at)
}

func (a *arnPaths) sortedKeys() []string {
	out := append([]string{}, a.keys...)
	// Array.prototype.sort with no comparator: UTF-16 code unit order.
	sort.SliceStable(out, func(i, j int) bool { return jsonv.LessUTF16(out[i], out[j]) })
	return out
}

type rewriteAccumulator struct {
	unknown      arnPaths
	interpolated arnPaths
}

// rewriteArns is export.ts's rewriteArns: every string that is wholly an ARN
// with a reverse-map entry becomes its token; one without is recorded as
// unknown, and an ARN inside a longer string as interpolated. Objects are
// walked and rebuilt in JavaScript's own-key order (Object.entries then
// Object.fromEntries), so the recorded paths come in the TypeScript's order.
func rewriteArns(value any, at string, reverseMap ReverseMap, acc *rewriteAccumulator) any {
	switch t := value.(type) {
	case string:
		if wholeArn.MatchString(t) {
			if entry, ok := LookupArn(reverseMap, t); ok {
				if entry.Type == "view" {
					return viewToken(entry, t, at, acc)
				}
				return entry.Token
			}
			acc.unknown.record(t, at)
			return t
		}
		for _, match := range arnAnywhere.FindAllString(t, -1) {
			acc.interpolated.record(match, at)
		}
		return t
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = rewriteArns(v, at+"["+strconv.Itoa(i)+"]", reverseMap, acc)
		}
		return out
	case jsonv.Object:
		out := make(jsonv.Object, 0, len(t))
		for _, m := range jsOrderedObject(t) {
			child := m.Key
			if at != "" {
				child = at + "." + m.Key
			}
			out = append(out, jsonv.Member{Key: m.Key, Value: rewriteArns(m.Value, child, reverseMap, acc)})
		}
		return out
	default:
		return value
	}
}

// viewToken is export.ts's viewToken: a view reference keeps the version the
// ARN carried, `${cdref:view:after-contact-work@1}`. A qualifier that is not
// a slug (`$LATEST`, say) cannot ride in the alias slot, so the ARN stays
// unknown rather than losing the version silently.
func viewToken(entry flowdoc.RefEntry, arn, at string, acc *rewriteAccumulator) string {
	parsed, ok := ParseConnectArn(arn)
	if !ok || !parsed.HasQualifier {
		return entry.Token
	}
	if flowdoc.SlugPattern.MatchString(parsed.Qualifier) {
		// token("view", entry.name, qualifier): both parts are slugs here, so
		// token() cannot throw.
		return "${cdref:view:" + entry.Name + "@" + parsed.Qualifier + "}"
	}
	acc.unknown.record(arn, at)
	return arn
}

func isRecord(v any) bool {
	_, ok := v.(jsonv.Object)
	return ok
}

// normalizeAction is export.ts's normalizeAction. Connect omits `Parameters`
// from an action that takes none rather than writing an empty map (observed
// live on TransferContactToQueue and DistributeByPercentage), and FlowDoc
// requires the key on every action, so the empty map Connect means by the
// absence is filled in; `Transitions` likewise, defensively.
//
// The rest is `{ ...action, Parameters, Transitions }` as JavaScript
// evaluates it for an element that is not an object: null or an absent
// element throws the TypeError reading `.Parameters` does, and anything else
// is spread (see spread).
func normalizeAction(action any) (any, error) {
	if action == nil {
		return nil, errors.New("Cannot read properties of null (reading 'Parameters')")
	}
	o, isObject := action.(jsonv.Object)
	var params, transitions any
	if isObject {
		params, _ = o.Get("Parameters")
		transitions, _ = o.Get("Transitions")
		if isRecord(params) && isRecord(transitions) {
			return o, nil
		}
	}
	out := spread(action)
	if isRecord(params) {
		out.Set("Parameters", params)
	} else {
		out.Set("Parameters", jsonv.Object{})
	}
	if isRecord(transitions) {
		out.Set("Transitions", transitions)
	} else {
		out.Set("Transitions", jsonv.Object{})
	}
	return out, nil
}

// asPoint is export.ts's isPoint: an object with numeric x and y.
func asPoint(v any) (flowdoc.Point, bool) {
	o, ok := v.(jsonv.Object)
	if !ok {
		return flowdoc.Point{}, false
	}
	x, xok := o.Get("x")
	y, yok := o.Get("y")
	xf, xnum := x.(float64)
	yf, ynum := y.(float64)
	if !xok || !yok || !xnum || !ynum {
		return flowdoc.Point{}, false
	}
	return flowdoc.Point{X: xf, Y: yf}, true
}

// lifted is liftMetadata's result.
type lifted struct {
	// layout holds the own properties of the TypeScript's layout record.
	layout map[string]flowdoc.Point
	// rest is the Metadata left in content; hasRest false means none.
	rest    jsonv.Object
	hasRest bool
}

// copyObject is a shallow copy with its own backing array, so Set and Delete
// on it leave the original alone.
func copyObject(o jsonv.Object) jsonv.Object {
	return append(jsonv.Object{}, o...)
}

// liftMetadata is export.ts's liftMetadata: positions out of
// content.Metadata and into `layout`, FlowDoc's single source of truth for
// position; everything else Metadata carries stays in content. Both
// `Position` (the Flow language example) and `position` (console exports)
// are lifted.
// https://docs.aws.amazon.com/connect/latest/devguide/flow-language-example.html
//
// An ActionMetadata key of `__proto__` is dropped from both, as the
// TypeScript's plain-object assignments drop it (they set a prototype and
// create no property).
func liftMetadata(metadata any, actionIDs map[string]bool) lifted {
	out := lifted{layout: map[string]flowdoc.Point{}}
	m, ok := metadata.(jsonv.Object)
	if !ok {
		return out
	}
	rest := copyObject(m)
	rest.Delete("EntryPointPosition")
	rest.Delete("entryPointPosition")

	if rawActionMetadata, ok := rest.Get("ActionMetadata"); ok {
		if actionMetadata, ok := rawActionMetadata.(jsonv.Object); ok {
			remaining := jsonv.Object{}
			for _, member := range jsOrderedObject(actionMetadata) {
				id := member.Key
				raw, ok := member.Value.(jsonv.Object)
				if !ok {
					if id != "__proto__" {
						remaining = append(remaining, jsonv.Member{Key: id, Value: member.Value})
					}
					continue
				}
				entry := copyObject(raw)
				position, has := entry.Get("Position")
				if !has || position == nil {
					position, _ = entry.Get("position")
				}
				if point, isPoint := asPoint(position); actionIDs[id] && isPoint {
					if id != "__proto__" {
						out.layout[id] = point
					}
					entry.Delete("Position")
					entry.Delete("position")
				}
				if len(entry) > 0 && id != "__proto__" {
					remaining = append(remaining, jsonv.Member{Key: id, Value: entry})
				}
			}
			if len(remaining) > 0 {
				rest.Set("ActionMetadata", remaining)
			} else {
				rest.Delete("ActionMetadata")
			}
		}
	}
	if len(rest) > 0 {
		out.rest = rest
		out.hasRest = true
	}
	return out
}

// ExportFlowOptions is export.ts's ExportFlowOptions.
type ExportFlowOptions struct {
	// Name is the FlowDoc name; it must be a slug (see SlugifyResourceName).
	Name        string
	ConnectType string
	// Kind is "flow" or "module"; "" defaults to "module" when ConnectType is
	// MODULE and "flow" otherwise.
	Kind string
	// Description is the flow's description; "" is none, as in the
	// TypeScript.
	Description string
	// Generator is recorded in meta.generator; nil defaults to
	// "core@<FlowDoc version>", the TypeScript's default.
	Generator *string
	// Meta is merged into meta, after generator.
	Meta jsonv.Object
	// OmitMeta emits no `meta` block (TypeScript: includeMeta === false).
	OmitMeta bool
}

// ExportFlow is export.ts's exportFlow: live Flow language in, FlowDoc out,
// returned canonical (flowdoc.Canonicalize). content is the Flow language
// JSON as a string or []byte, or a value already decoded by jsonv. Every ARN
// occupying a whole field value becomes its `${cdref:type:name}` token; any
// ARN with no reverse-map entry, and any ARN inside a longer string, is a
// hard *ExportError listing every one at once.
func ExportFlow(content any, reverseMap ReverseMap, options ExportFlowOptions) (jsonv.Object, error) {
	parsed := content
	switch c := content.(type) {
	case string:
		v, err := jsonv.Decode([]byte(c))
		if err != nil {
			return nil, err
		}
		parsed = v
	case []byte:
		v, err := jsonv.Decode(c)
		if err != nil {
			return nil, err
		}
		parsed = v
	}
	raw, ok := parsed.(jsonv.Object)
	if !ok {
		return nil, errors.New("Cannot export: flow content is not a JSON object.")
	}
	version, hasVersion := raw.Get("Version")
	if s, ok := version.(string); !ok || s != flowdoc.FlowLanguageVersion {
		return nil, errors.New("Cannot export: flow content Version is " + stringify(version, hasVersion) +
			`, expected "` + flowdoc.FlowLanguageVersion + `".`)
	}
	startValue, _ := raw.Get("StartAction")
	start, ok := startValue.(string)
	if !ok || start == "" {
		return nil, errors.New("Cannot export: flow content has no StartAction.")
	}
	actionsValue, _ := raw.Get("Actions")
	rawActions, ok := actionsValue.([]any)
	if !ok || len(rawActions) == 0 {
		return nil, errors.New("Cannot export: flow content has no Actions.")
	}
	if !flowdoc.SlugPattern.MatchString(options.Name) {
		return nil, errors.New(`Cannot export: "` + options.Name +
			`" is not a valid FlowDoc name. Names are lowercase words separated by single hyphens.`)
	}

	acc := &rewriteAccumulator{}
	rewritten := rewriteArns(rawActions, "Actions", reverseMap, acc).([]any)
	actions := make([]any, len(rewritten))
	for i, a := range rewritten {
		normalized, err := normalizeAction(a)
		if err != nil {
			return nil, err
		}
		actions[i] = normalized
	}
	metadataValue, _ := raw.Get("Metadata")
	metadata := rewriteArns(metadataValue, "Metadata", reverseMap, acc)

	if len(acc.unknown.keys) > 0 || len(acc.interpolated.keys) > 0 {
		locations := map[string][]string{}
		for _, arn := range acc.unknown.keys {
			locations[arn] = acc.unknown.paths[arn]
		}
		for _, arn := range acc.interpolated.keys {
			locations[arn] = acc.interpolated.paths[arn]
		}
		return nil, NewExportError(acc.unknown.sortedKeys(), acc.interpolated.sortedKeys(), locations, options.Name)
	}

	// new Set(actions.map((a) => a.Identifier)), queried only with string
	// keys, so only string Identifiers can ever match.
	actionIDs := map[string]bool{}
	for _, a := range actions {
		if id, ok := identifierOf(a); ok {
			actionIDs[id] = true
		}
	}
	lift := liftMetadata(metadata, actionIDs)

	flowContent := jsonv.Object{
		{Key: "Version", Value: flowdoc.FlowLanguageVersion},
		{Key: "StartAction", Value: start},
		{Key: "Actions", Value: actions},
	}
	if lift.hasRest {
		flowContent = append(flowContent, jsonv.Member{Key: "Metadata", Value: lift.rest})
	}

	// Actions the instance never gave a position get the same deterministic
	// auto-layout synth would have assigned, so the studio can open the
	// result.
	var auto map[string]flowdoc.Point
	if len(lift.layout) != len(actions) {
		auto = flowdoc.AutoLayout(actions, &start)
	}
	layout := jsonv.Object{}
	seen := map[string]bool{}
	var keys []string
	values := map[string]flowdoc.Point{}
	for _, a := range actions {
		idValue, hasID := a.(jsonv.Object).Get("Identifier")
		key := propertyKey(idValue, hasID)
		if seen[key] {
			continue // the same key again gets the same value
		}
		seen[key] = true
		// layout[id] = lifted.layout[id] ?? auto[id] ?? { x: 0, y: 0 }
		if p, ok := lift.layout[key]; ok {
			values[key] = p
		} else if objectPrototypeKeys[key] {
			// The inherited Object.prototype member is not nullish, so the
			// record gets a function (dropped by JSON.stringify) or, for
			// __proto__, a new prototype and no property.
			continue
		} else if p, ok := auto[key]; ok && isString(idValue) {
			values[key] = p
		} else {
			values[key] = flowdoc.Point{}
		}
		keys = append(keys, key)
	}
	for _, key := range jsKeyOrder(keys) {
		layout = append(layout, jsonv.Member{Key: key, Value: values[key].JSON()})
	}

	refs := []any{}
	for _, e := range flowdoc.CollectRefs(flowContent) {
		ref := jsonv.Object{
			{Key: "token", Value: e.Token},
			{Key: "type", Value: e.Type},
			{Key: "name", Value: e.Name},
		}
		if e.Alias != "" {
			ref = append(ref, jsonv.Member{Key: "alias", Value: e.Alias})
		}
		refs = append(refs, ref)
	}

	kind := options.Kind
	if kind == "" {
		kind = "flow"
		if options.ConnectType == "MODULE" {
			kind = "module"
		}
	}
	doc := jsonv.Object{
		{Key: "flowdoc", Value: flowdoc.Version},
		{Key: "kind", Value: kind},
		{Key: "name", Value: options.Name},
	}
	if options.Description != "" {
		doc = append(doc, jsonv.Member{Key: "description", Value: options.Description})
	}
	doc = append(doc,
		jsonv.Member{Key: "connectType", Value: options.ConnectType},
		jsonv.Member{Key: "content", Value: flowContent},
		jsonv.Member{Key: "layout", Value: layout},
		jsonv.Member{Key: "refs", Value: refs},
	)
	if !options.OmitMeta {
		generator := "core@" + flowdoc.Version
		if options.Generator != nil {
			generator = *options.Generator
		}
		meta := jsonv.Object{{Key: "generator", Value: generator}}
		for _, m := range jsOrderedObject(options.Meta) {
			meta.Set(m.Key, m.Value)
		}
		doc = append(doc, jsonv.Member{Key: "meta", Value: meta})
	}
	return flowdoc.Canonicalize(doc), nil
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

func identifierOf(action any) (string, bool) {
	o, _ := action.(jsonv.Object)
	v, _ := o.Get("Identifier")
	s, ok := v.(string)
	return s, ok
}
