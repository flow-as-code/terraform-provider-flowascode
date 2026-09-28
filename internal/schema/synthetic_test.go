// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"math"
	"testing"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/jsonv"
)

// Small schemas that reach what the FlowDoc schemas do not (yet): every
// branch of the error flattening, Ajv's two uniqueItems searches, $ref
// inlining, nested clauses, and the ECMA-262 pattern semantics RE2 lacks.
// Each is recorded in the oracle next to the FlowDoc cases.
type synthCase struct {
	name   string
	schema string
	doc    string
	// earlyStops is how many Ajv errors santhosh-tekuri's early stop at a
	// failing type, const or enum elides here.
	earlyStops int
	// ajvOnly marks a value Ajv rejects and this accepts: multipleOf, which
	// Ajv tests as data/schema !== parseInt(data/schema) in doubles
	// (vocabularies/validation/multipleOf.js) and santhosh-tekuri in exact
	// rationals. The FlowDoc schemas have no multipleOf
	// (TestFlowDocSchemasAvoidNumericDivergence).
	ajvOnly bool
}

func synthCases() []synthCase {
	return []synthCase{
		{name: "type then enum: Ajv goes on to enum", schema: `{"type":"string","enum":["a"]}`, doc: `5`, earlyStops: 1},
		{name: "type then const and not", schema: `{"type":"string","const":"a","not":{"const":5}}`, doc: `5`, earlyStops: 2},
		{name: "uniqueItems over scalar items: the indices scan", schema: `{"type":"array","items":{"type":"string"},"uniqueItems":true}`, doc: `["a","b","a","b"]`},
		{name: "uniqueItems over two scalar types", schema: `{"items":{"type":["string","number"]},"uniqueItems":true}`, doc: `["1",1,"1",1]`},
		{name: "uniqueItems skips items of another type", schema: `{"items":{"type":"string"},"uniqueItems":true}`, doc: `[1,1,"x"]`},
		{name: "uniqueItems over untyped items: the pairwise scan", schema: `{"uniqueItems":true}`, doc: `[{"a":[1]},2,{"a":[1]},2]`},
		{name: "uniqueItems compares objects as key sets", schema: `{"uniqueItems":true}`, doc: `[{"a":1,"b":2},{"b":2,"a":1}]`},
		{name: "a type list", schema: `{"type":["string","null"]}`, doc: `5`},
		{name: "a false schema", schema: `{"properties":{"a":false}}`, doc: `{"a":1}`},
		{name: "not", schema: `{"not":{"type":"string"}}`, doc: `"s"`},
		{name: "anyOf with every branch failing", schema: `{"anyOf":[{"type":"string"},{"minimum":3}]}`, doc: `1`},
		{name: "oneOf with no branch passing", schema: `{"oneOf":[{"required":["A"]},{"required":["B"]}]}`, doc: `{}`},
		{name: "oneOf passing twice with a failing branch between", schema: `{"oneOf":[{"required":["A"]},{"required":["B"]},{"required":["C"]},{"required":["D"]}]}`, doc: `{"A":1,"C":1}`},
		{name: "oneOf passing twice then failing", schema: `{"oneOf":[{"required":["A"]},{"required":["B"]},{"required":["C"]}]}`, doc: `{"A":1,"B":1}`},
		{name: "number limits", schema: `{"minimum":1.5,"exclusiveMaximum":10,"exclusiveMinimum":0,"multipleOf":0.5}`, doc: `[0.25,10,0]`},
		{name: "number limits on items", schema: `{"items":{"minimum":1.5,"exclusiveMaximum":10,"exclusiveMinimum":0,"maximum":9,"multipleOf":0.5}}`, doc: `[0.25,10,0,1e20]`},
		{name: "multipleOf at 1e21, which parseInt reads as 2", schema: `{"multipleOf":0.5}`, doc: `1e21`, ajvOnly: true},
		{name: "multipleOf in binary floating point", schema: `{"multipleOf":0.1}`, doc: `0.3`, ajvOnly: true},
		{name: "object counts", schema: `{"properties":{"o":{"minProperties":2,"maxProperties":0}}}`, doc: `{"o":{"a":1}}`},
		{name: "array counts", schema: `{"minItems":3,"maxItems":1}`, doc: `[1,2]`},
		{name: "string lengths count code points", schema: `{"items":{"minLength":3,"maxLength":1}}`, doc: `["\ud83d\ude00\ud83d\ude00","ab"]`},
		{name: "if with then and else, per item", schema: `{"items":{"if":{"type":"string"},"then":{"minLength":2},"else":{"type":"number","maximum":3}}}`, doc: `["a",5,"abc",true]`},
		{name: "an if nested in a then", schema: `{"if":{"required":["a"]},"then":{"if":{"required":["b"]},"then":{"required":["c"]}}}`, doc: `{"a":1,"b":1}`},
		{name: "a then two properties deep", schema: `{"if":{"required":["k"]},"then":{"properties":{"p":{"properties":{"q":{"type":"string"}}}}}}`, doc: `{"k":1,"p":{"q":1}}`},
		{name: "a property called then", schema: `{"properties":{"then":{"type":"string"}}}`, doc: `{"then":1}`},
		{name: "additionalProperties in instance order", schema: `{"properties":{"m":{}},"additionalProperties":false}`, doc: `{"z":1,"m":1,"a":1}`},
		{name: "required, several missing", schema: `{"required":["b","a","c"]}`, doc: `{"a":1}`},
		{name: "propertyNames with two failing keywords", schema: `{"propertyNames":{"maxLength":2,"pattern":"^x"}}`, doc: `{"xy":1,"abc":2}`},
		{name: "propertyNames under additionalProperties", schema: `{"additionalProperties":{"propertyNames":{"pattern":"^a"}}}`, doc: `{"x":{"b":1},"y":{"a":1,"c":2}}`},
		{name: "propertyNames under items, the same bad key twice", schema: `{"items":{"propertyNames":{"pattern":"^a"}}}`, doc: `[{"a":1},{"b":1},{"b":2}]`},
		{name: "propertyNames under a then", schema: `{"if":{"required":["t"]},"then":{"properties":{"m":{"propertyNames":{"enum":["ok"]}}}}}`, doc: `{"t":1,"m":{"ok":1,"no":2}}`},
		{name: "an inlined $ref under a compiled one", schema: `{"$defs":{"a":{"type":"string"},"b":{"$ref":"#/$defs/a"}},"properties":{"x":{"$ref":"#/$defs/b"},"y":{"$ref":"#/$defs/a"}}}`, doc: `{"x":1,"y":2}`},
		{name: "a pure $ref chain ending in a compiled schema", schema: `{"$defs":{"a":{"properties":{"z":{"$ref":"#/$defs/c"}},"required":["w"]},"b":{"description":"only a ref","$ref":"#/$defs/a"},"c":{"type":"string"}},"properties":{"x":{"$ref":"#/$defs/b"}}}`, doc: `{"x":{"z":1}}`},
		{name: "an ECMA-262 dot refuses line terminators", schema: `{"items":{"pattern":"^.$"}}`, doc: `["\r","\n","\u2028","\u2029","a","\u00e9"]`},
		{name: "an ECMA-262 \\s is Unicode white space", schema: `{"items":{"pattern":"^\\s$"}}`, doc: `["\u00a0","\ufeff","\u3000","\u000b","x"]`},
		{name: "an ECMA-262 \\S", schema: `{"items":{"pattern":"^\\S$"}}`, doc: `["\u00a0","x"]`},
		{name: "ECMA-262 empty classes", schema: `{"properties":{"none":{"pattern":"[]"},"any":{"pattern":"^[^]$"}}}`, doc: `{"none":"]","any":"\n"}`},
		{name: "a bracket in a class is literal", schema: `{"items":{"pattern":"^[[:a]+$"}}`, doc: `["[:a","b"]`},
		{name: "unicode escapes", schema: `{"items":{"pattern":"^\\u00e9\\u{1F600}\\uD83D\\uDE00\\x41$"}}`, doc: `["\u00e9\ud83d\ude00\ud83d\ude00A","e"]`},
		{name: "a leading negative lookahead", schema: `{"items":{"pattern":"^(?!a\\.b)[a-z.]+$"}}`, doc: `["a.b","a.bc","axb","ab"]`},
		{name: "a pattern searches, it does not anchor", schema: `{"pattern":"b"}`, doc: `"abc"`},
		{name: "escapes of syntax characters", schema: `{"pattern":"^\\/\\$\\{\\}\\(\\)\\|[\\-]$"}`, doc: `"/${}()|-"`},
	}
}

func TestSyntheticCasesAgainstAjv(t *testing.T) {
	recs := oracleRecords(t, "synthetic")
	cases := synthCases()
	if len(recs) != len(cases) {
		t.Errorf("oracle holds %d synthetic cases, the table %d; re-record it", len(recs), len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, ok := recs[c.name]
			if !ok {
				t.Fatal("not in the oracle; re-record it")
			}
			if got := sha([]byte(c.schema + "\n" + c.doc)); got != r.sha {
				t.Fatal("the oracle was recorded on a different case; re-record it")
			}
			cs, err := compileSchema([]byte(c.schema))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := jsonv.Decode([]byte(c.doc))
			if err != nil {
				t.Fatal(err)
			}
			vs, err := cs.validate(doc)
			if err != nil {
				t.Fatal(err)
			}
			if c.ajvOnly {
				if r.valid || len(vs) != 0 {
					t.Fatalf("expected the documented divergence (Ajv rejects, this accepts); Ajv valid=%v, got %v", r.valid, vs)
				}
				return
			}
			if stops := compareWithAjv(t, r, vs); stops != c.earlyStops {
				t.Errorf("%d early stops, want %d", stops, c.earlyStops)
			}
		})
	}
}

func synthInput() []any {
	var out []any
	for _, c := range synthCases() {
		out = append(out, jsonv.Object{
			{Key: "id", Value: c.name},
			{Key: "sha256", Value: sha([]byte(c.schema + "\n" + c.doc))},
			{Key: "schema", Value: c.schema},
			{Key: "doc", Value: c.doc},
		})
	}
	return out
}

// The two places this validator's numbers can disagree with Ajv's are
// multipleOf (above) and a limit that is not exactly a double. Neither occurs
// in the FlowDoc schemas: no multipleOf, and every numeric limit an integer a
// double holds exactly, which a double compares to the same way in both.
func TestFlowDocSchemasAvoidNumericDivergence(t *testing.T) {
	limits := map[string]bool{"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
		"minLength": true, "maxLength": true, "minItems": true, "maxItems": true, "minProperties": true, "maxProperties": true}
	for _, v := range Versions {
		c, err := compiled(v)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		var walk func(x any)
		walk = func(x any) {
			switch o := x.(type) {
			case jsonv.Object:
				for _, m := range o {
					if m.Key == "multipleOf" {
						t.Errorf("%s: multipleOf, which Ajv evaluates in doubles", v)
					}
					if limits[m.Key] {
						if f, ok := m.Value.(float64); ok {
							n++
							if f != math.Trunc(f) || math.Abs(f) > 1<<53 {
								t.Errorf("%s: %s %v is not an exact integer", v, m.Key, f)
							}
						}
					}
					walk(m.Value)
				}
			case []any:
				for _, e := range o {
					walk(e)
				}
			}
		}
		walk(c.raw)
		if n == 0 {
			t.Errorf("%s: no numeric limits found; is the walk right?", v)
		}
	}
}
