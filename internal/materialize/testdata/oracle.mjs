// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Records what @flow-as-code/core's materialize.ts returns for the inputs
// below, into ts-oracle.json next to this file. Run from this directory:
//
//   node oracle.mjs <path to a build of packages/core> <commit>
//
// Every input is stored as JSON text, parsed by JSON.parse here and by
// jsonv.Decode in the Go test, so both sides start from the same bytes.

import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const [distDir, commit] = process.argv.slice(2);
const core = await import(pathToFileURL(resolve(distDir, "index.js")).href);
const { materializeWithMap, materializeWithBinder, serializeContent } = core;

const vendored = (p) => readFileSync(new URL(`../../conformance/data/${p}`, import.meta.url), "utf8");
const demoText = vendored("materialize/demo-with-map/doc.flowdoc.json");
const demoMapText = vendored("materialize/demo-with-map/map.json");
const demo = () => JSON.parse(demoText);
const demoMap = () => JSON.parse(demoMapText);

const Q = "${cdref:queue:appointments}";
const H = "${cdref:hours:main-line}";
const L = "${cdref:lambda:appointment-lookup}";

// A small document the layout and identifier cases edit.
function small(ids = ["a", "b"], start = ids[0]) {
  const actions = ids.map((id, i) => ({
    Identifier: id,
    Type: i === ids.length - 1 ? "DisconnectParticipant" : "MessageParticipant",
    Parameters: i === ids.length - 1 ? {} : { Text: `step ${id}` },
    Transitions: i === ids.length - 1 ? {} : { NextAction: ids[i + 1], Errors: [], Conditions: [] },
  }));
  const layout = {};
  ids.forEach((id, i) => (layout[id] = { x: 150 + 260 * i, y: 40 + 10 * i }));
  return {
    flowdoc: "0.2",
    kind: "flow",
    name: "small",
    connectType: "CONTACT_FLOW",
    content: { Version: "2019-10-30", StartAction: start, Actions: actions },
    layout,
  };
}

const cases = [];
const mapCase = (name, doc, map) =>
  cases.push({ name, fn: "map", doc: typeof doc === "string" ? doc : JSON.stringify(doc), map: typeof map === "string" ? map : JSON.stringify(map) });
const binderCase = (name, doc, binder) =>
  cases.push({ name, fn: "binder", doc: typeof doc === "string" ? doc : JSON.stringify(doc), binder });
const edit = (fn, base = demo()) => {
  fn(base);
  return base;
};

// --- The map backend ---------------------------------------------------------
mapCase("demo-full", demo(), demoMap());
mapCase("demo-missing-two", demo(), edit((m) => { delete m[H]; delete m[Q]; }, demoMap()));
mapCase("demo-missing-all", demo(), {});
mapCase("demo-key-forms", demo(), {
  "hours:main-line": "HOURS",
  lambda_appointment_lookup_arn: "LAMBDA",
  [Q]: "",
});
mapCase("demo-token-form-wins", demo(), {
  "queue:appointments": "BY-KEY",
  queue_appointments_arn: "BY-VARIABLE",
  [Q]: "BY-TOKEN",
  "hours:main-line": "H",
  hours_main_line_arn: "H-VAR",
  [L]: "L",
});
mapCase("demo-extra-keys-ignored", demo(), { ...demoMap(), "${cdref:queue:unused}": "X", unrelated: "Y" });
mapCase("alias-missing", edit((d) => { d.content.Actions[0].Parameters.Q = "${cdref:module:survey@prod-2}"; }), demoMap());
mapCase("alias-by-variable", edit((d) => { d.content.Actions[0].Parameters.Q = "${cdref:module:survey@prod-2}"; }), { ...demoMap(), module_survey_prod_2_arn: "M" });

mapCase(
  "interpolated-leak",
  edit((d) => {
    d.content.Actions[1].Parameters.Text = "Please hold, ${cdref:prompt:greeting} is next.";
  }),
  { ...demoMap(), "${cdref:prompt:greeting}": "arn:aws:connect:us-east-1:111122223333:instance/E/x/E" },
);
mapCase(
  "leak-several-sorted",
  edit((d) => {
    d.content.Actions[1].Parameters.Text = "b ${cdref:queue:zeta} and ${cdref:hours:alpha} and ${cdref:queue:zeta}";
    d.content.Actions[0].Parameters.Note = "x ${cdref:lambda:mid}";
  }),
  { ...demoMap(), "${cdref:queue:zeta}": "Z", "${cdref:hours:alpha}": "A", "${cdref:lambda:mid}": "M" },
);
mapCase(
  "leak-unmodeled-shape",
  edit((d) => {
    d.content.Actions[1].Parameters.Text = "say ${cdref:Queue:Bad Name} now";
  }),
  demoMap(),
);

mapCase(
  "metadata-merge",
  `{"flowdoc":"0.2","kind":"flow","name":"m","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"welcome","Metadata":{"zeta":1,"EntryPointPosition":{"x":1},"note":"${Q}","7":"seven","ActionMetadata":{"welcome":{"Position":{"x":1,"y":1},"isFriendlyName":true,"ref":"${Q}"},"stale-id-not-in-actions":{"Position":{"x":9,"y":9}},"12":{"a":1},"bye":"not an object","__proto__":{"own":true}}},"Actions":[{"Identifier":"welcome","Type":"MessageParticipant","Parameters":{"Text":"hi"},"Transitions":{"NextAction":"bye","Errors":[],"Conditions":[]}},{"Identifier":"bye","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}},{"Identifier":"__proto__","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"welcome":{"x":300,"y":80},"bye":{"x":560,"y":80},"__proto__":{"x":1,"y":2}}}`,
  demoMap(),
);
for (const [name, metadata] of [
  ["metadata-token-string", `"${Q}"`],
  ["metadata-plain-string", `"ab"`],
  ["metadata-array", `[1,"${Q}",{"k":"${H}"}]`],
  ["metadata-number", `5`],
  ["metadata-true", `true`],
  ["metadata-null", `null`],
  ["metadata-empty", `{}`],
  ["metadata-am-array", `{"ActionMetadata":[1,2]}`],
  ["metadata-am-null", `{"ActionMetadata":null}`],
  ["metadata-am-string", `{"ActionMetadata":"${Q}"}`],
  ["metadata-am-token-object", `{"ActionMetadata":{"a":"${Q}","b":{"Position":"${H}","k":1}}}`],
]) {
  const doc = small();
  doc.content.Metadata = "__METADATA__";
  mapCase(name, JSON.stringify(doc).replace('"__METADATA__"', metadata), demoMap());
}

mapCase("module-no-settings", edit((d) => { d.kind = "module"; }, small()), {});
mapCase("flow-no-settings", small(), {});
mapCase("flow-settings-null", edit((d) => { d.content.Settings = null; }, small()), {});
mapCase("module-settings-null", edit((d) => { d.kind = "module"; d.content.Settings = null; }, small()), {});
mapCase(
  "module-settings-tokens",
  `{"flowdoc":"0.2","kind":"module","name":"m","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Settings":{"b":"${Q}","a":[1,"${H}"],"3":{"z":1,"y":2},"InputParameters":[]},"Actions":[{"Identifier":"a","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"a":{"x":20,"y":20}}}`,
  demoMap(),
);
mapCase("flow-settings-token-string", edit((d) => { d.content.Settings = Q; }, small()), demoMap());
mapCase("flow-settings-array", edit((d) => { d.content.Settings = [Q, 2]; }, small()), demoMap());
mapCase("flow-settings-missing-token", edit((d) => { d.content.Settings = { q: "${cdref:queue:nope}" }; }, small()), {});

// --- Layout projection -------------------------------------------------------
mapCase("no-layout", edit((d) => { delete d.layout; }), demoMap());
mapCase("partial-layout", edit((d) => { delete d.layout["hang-up"]; delete d.layout["welcome"]; }), demoMap());
mapCase("layout-null", edit((d) => { d.layout = null; }, small()), {});
mapCase("layout-array", edit((d) => { d.layout = [{ x: 500, y: 7 }]; }, small(["0", "1"])), {});
mapCase("layout-string", edit((d) => { d.layout = "xyz"; }, small(["0", "length"])), {});
mapCase("layout-number", edit((d) => { d.layout = 5; }, small()), {});
mapCase("layout-entry-null", edit((d) => { d.layout.a = null; }, small()), {});
mapCase("layout-entry-null-and-missing", edit((d) => { d.layout.a = null; delete d.layout.b; }, small()), {});
mapCase("layout-entry-partial", edit((d) => { d.layout.b = { x: 3 }; d.layout.a = { y: 4, extra: 1 }; }, small()), {});
mapCase("layout-entry-odd", edit((d) => { d.layout.b = 7; d.layout.a = [1, 2]; }, small()), {});
mapCase("layout-token-x", edit((d) => { d.layout.b = { x: Q, y: 1 }; }, small()), demoMap());
for (const [name, x] of [
  ["start-x-string", "150"],
  ["start-x-padded", " \n 150\t"],
  ["start-x-hex", "0x100"],
  ["start-x-octal", "0o777"],
  ["start-x-binary", "0b11111111"],
  ["start-x-exponent", "1.5e2"],
  ["start-x-leading-dot", ".25e3"],
  ["start-x-trailing-dot", "250."],
  ["start-x-infinity", "Infinity"],
  ["start-x-neg-infinity", "-Infinity"],
  ["start-x-junk", "abc"],
  ["start-x-hex-signed", "-0x10"],
  ["start-x-empty", ""],
  ["start-x-null", null],
  ["start-x-true", true],
  ["start-x-empty-array", []],
  ["start-x-one-array", [250]],
  ["start-x-nested-array", [["175"]]],
  ["start-x-null-array", [null]],
  ["start-x-two-array", [1, 2]],
  ["start-x-object", {}],
  ["start-x-small", 30],
  ["start-x-exact", 100],
  ["start-x-negative", -5],
  ["start-x-fraction", 100.5],
  ["start-x-huge", 1e300],
]) {
  mapCase(name, edit((d) => { d.layout.a = { x, y: 3 }; }, small()), {});
}
mapCase("start-x-missing", edit((d) => { d.layout.a = { y: 3 }; }, small()), {});
mapCase("start-not-an-action", small(["a", "b"], "zzz"), {});
mapCase("start-inherited-name", small(["a", "b"], "hasOwnProperty"), {});

// --- Identifiers JavaScript reads specially ----------------------------------
mapCase("id-constructor-no-layout", edit((d) => { delete d.layout.constructor; }, small(["a", "constructor", "b"])), {});
mapCase("id-constructor-with-layout", small(["a", "constructor", "b"]), {});
mapCase("id-constructor-start", edit((d) => { delete d.layout.constructor; }, small(["constructor", "b"])), {});
mapCase("id-tostring-null-layout", edit((d) => { d.layout.toString = null; }, small(["a", "toString"])), {});
mapCase("id-tostring-null-and-missing", edit((d) => { d.layout.toString = null; delete d.layout.a; }, small(["a", "toString"])), {});
mapCase(
  "id-proto-own-layout",
  `{"flowdoc":"0.2","kind":"flow","name":"p","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"MessageParticipant","Parameters":{},"Transitions":{"NextAction":"__proto__"}},{"Identifier":"__proto__","Type":"MessageParticipant","Parameters":{},"Transitions":{"NextAction":"Position"}},{"Identifier":"Position","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"a":{"x":150,"y":1},"__proto__":{"x":410,"y":2},"Position":{"x":670,"y":3}}}`,
  {},
);
mapCase(
  "id-proto-inherited-layout",
  `{"flowdoc":"0.2","kind":"flow","name":"p","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"__proto__","Actions":[{"Identifier":"__proto__","Type":"MessageParticipant","Parameters":{},"Transitions":{"NextAction":"b"}},{"Identifier":"b","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"b":{"x":670,"y":3}}}`,
  {},
);
mapCase(
  "id-proto-start-own-layout",
  `{"flowdoc":"0.2","kind":"flow","name":"p","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"__proto__","Actions":[{"Identifier":"__proto__","Type":"MessageParticipant","Parameters":{},"Transitions":{"NextAction":"b"}},{"Identifier":"b","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"b":{"x":670,"y":3},"__proto__":{"x":333,"y":4}}}`,
  {},
);
mapCase(
  "id-proto-primitive-layout",
  `{"flowdoc":"0.2","kind":"flow","name":"p","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"__proto__","Actions":[{"Identifier":"__proto__","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"__proto__":5}}`,
  {},
);
mapCase(
  "start-inherited-through-proto",
  `{"flowdoc":"0.2","kind":"flow","name":"p","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"x","Actions":[{"Identifier":"__proto__","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"__proto__":{"x":{"x":260,"y":9},"y":2}}}`,
  {},
);
mapCase(
  "id-proto-auto-layout",
  `{"flowdoc":"0.2","kind":"flow","name":"p","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"MessageParticipant","Parameters":{},"Transitions":{"NextAction":"__proto__"}},{"Identifier":"__proto__","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"layout":{"__proto__":null}}`,
  {},
);
mapCase("index-ids", edit((d) => { delete d.layout["2"]; }, small(["10", "a", "9", "2"])), {});
mapCase("duplicate-ids", edit((d) => { d.content.Actions[1].Identifier = "a"; d.content.Actions.push({ Identifier: "b", Type: "DisconnectParticipant", Parameters: {}, Transitions: {} }); }, small(["a", "x"])), {});
mapCase("version-missing", edit((d) => { delete d.content.Version; }, small()), {});
mapCase("version-number", edit((d) => { d.content.Version = 3; }, small()), {});
mapCase(
  "content-extra-keys-dropped",
  `{"flowdoc":"0.2","kind":"flow","name":"x","connectType":"CONTACT_FLOW","description":"d","content":{"Extra":1,"Actions":[{"Identifier":"a","Type":"DisconnectParticipant","Parameters":{"10":1,"9":2,"b":{"2":3,"1":4}},"Transitions":{},"Extra":true}],"StartAction":"a","Version":"2019-10-30"},"layout":{"a":{"x":150,"y":20}},"refs":[],"meta":{"k":1}}`,
  {},
);
mapCase(
  "unicode-keys",
  `{"flowdoc":"0.2","kind":"flow","name":"u","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"Generic","Parameters":{"\\uffff":1,"\\ud83d\\ude00":2,"\\u00e9":3,"Z":4,"a":5},"Transitions":{}}]},"layout":{"a":{"x":150,"y":20}}}`,
  {},
);

// --- A whole-value token the completeness scan never saw ---------------------
mapCase(
  "swallowed-token-in-object",
  edit((d) => { d.content.Actions[0].Parameters = { A: "${cdref:queue:x", B: "${cdref:queue:y}", C: "keep" }; }, small()),
  {},
);
mapCase(
  "swallowed-token-in-array",
  edit((d) => { d.content.Actions[0].Parameters = { L: ["${cdref:queue:x", "${cdref:queue:y}", "z"] }; }, small()),
  {},
);
mapCase(
  "swallowed-metadata",
  `{"flowdoc":"0.2","kind":"flow","name":"s","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"\${cdref:a:b","Metadata":"\${cdref:queue:x}","Actions":[]}}`,
  {},
);
mapCase(
  "swallowed-settings",
  `{"flowdoc":"0.2","kind":"flow","name":"s","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"\${cdref:a:b","Settings":"\${cdref:queue:x}","Actions":[]}}`,
  {},
);
mapCase(
  "swallowed-metadata-key",
  `{"flowdoc":"0.2","kind":"flow","name":"s","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"\${cdref:a:b","Metadata":{"EntryPointPosition":"\${cdref:queue:x}","z":1},"Actions":[]}}`,
  {},
);

// --- Auto-layout and serialization over malformed transitions ----------------
for (const [name, transitions] of [
  ["autolayout-errors-object", { Errors: {} }],
  ["autolayout-errors-string", { Errors: "ab" }],
  ["autolayout-errors-null", { Errors: null }],
  ["autolayout-errors-null-element", { Errors: [null] }],
  ["autolayout-errors-string-element", { Errors: ["ab"], NextAction: "b" }],
  ["autolayout-conditions-object", { Conditions: { a: 1 } }],
  ["autolayout-conditions-null-element", { NextAction: "b", Conditions: [{ NextAction: "b" }, null] }],
]) {
  mapCase(name, edit((d) => { delete d.layout.b; d.content.Actions[0].Transitions = transitions; }, small()), {});
}
mapCase("autolayout-second-duplicate-ignored", edit((d) => { delete d.layout.b; d.content.Actions.push({ Identifier: "a", Type: "X", Parameters: {}, Transitions: { Errors: [null] } }); }, small()), {});
mapCase("serialize-errors-null", edit((d) => { d.content.Actions[0].Transitions = { Errors: null }; }, small()), {});
mapCase("serialize-condition-shapes", edit((d) => {
  d.content.Actions[0].Transitions = {
    NextAction: "b",
    Errors: [null, "ab", [1], { NextAction: "b", ErrorType: "E", z: 1 }],
    Conditions: [{ NextAction: "b" }, { NextAction: null, Condition: null }, { Condition: "ab" }, "s", 5, { Condition: { Operands: ["x"], Operator: "Equals", z: 0 }, NextAction: "b", extra: 1 }],
    zz: 1,
    "3": 3,
  };
}, small()), {});

// --- Refused inputs ----------------------------------------------------------
mapCase("invalid-array-doc", "[]", {});
mapCase("invalid-null-doc", "null", {});
mapCase("invalid-no-name", edit((d) => { delete d.name; }, small()), {});
mapCase("invalid-action-parameters", edit((d) => { d.content.Actions[1].Parameters = []; }, small()), {});
mapCase("map-null", small(), "null");

// --- The binder backend ------------------------------------------------------
binderCase("binder-demo-echo", demo(), "echo");
binderCase("binder-passthrough-fixture", vendored("materialize/binder-passthrough/doc.flowdoc.json"), "fixture");
binderCase(
  "binder-order",
  `{"flowdoc":"0.2","kind":"module","name":"m","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"a","Actions":[{"Identifier":"a","Type":"X","Parameters":{"z":"\${cdref:queue:z}","7":"\${cdref:queue:seven}","a":["\${cdref:hours:h@v1}"]},"Transitions":{}}],"Metadata":{"n":"\${cdref:lambda:meta}","ActionMetadata":{"a":{"t":"\${cdref:lambda:am}"}}},"Settings":{"s":"\${cdref:flow:set}"}},"layout":{"a":{"x":150,"y":20}}}`,
  "echo",
);
binderCase(
  "binder-no-strictness",
  edit((d) => { d.content.Actions[1].Parameters.Text = "Please hold, ${cdref:prompt:greeting} is next."; }),
  "echo",
);
binderCase("binder-swallowed", edit((d) => { d.content.Actions[0].Parameters = { A: "${cdref:queue:x", B: "${cdref:queue:y}" }; }, small()), "echo");
binderCase("binder-invalid-doc", "{}", "echo");

// --- serializeContent on its own ---------------------------------------------
const serializeCases = [];
const ser = (name, content) =>
  serializeCases.push({ name, content: typeof content === "string" ? content : JSON.stringify(content) });
const action = (over = {}) => ({ Identifier: "a", Type: "T", Parameters: {}, Transitions: {}, ...over });
ser("minimal", { Version: "2019-10-30", StartAction: "a", Actions: [action()] });
ser("key-order", `{"Actions":[],"Extra":1,"Metadata":{"b":1,"ActionMetadata":{"z":{"Position":{"y":1,"x":2}},"10":{},"9":{}},"a":2,"EntryPointPosition":{"y":0,"x":1},"3":0},"Settings":{"b":1,"10":2,"9":3,"a":{"d":1,"c":2}},"StartAction":"a","Version":"v"}`);
ser("no-version", { StartAction: "a", Actions: [] });
ser("settings-null", { Version: "v", StartAction: "a", Settings: null, Actions: [] });
ser("settings-string", { Version: "v", StartAction: "a", Settings: "ab", Actions: [] });
ser("metadata-null", { Version: "v", StartAction: "a", Metadata: null, Actions: [] });
ser("metadata-array", { Version: "v", StartAction: "a", Metadata: [3, { b: 1, a: 2 }], Actions: [] });
ser("metadata-string", { Version: "v", StartAction: "a", Metadata: "hé", Actions: [] });
ser("metadata-accent-string", { Version: "v", StartAction: "a", Metadata: "a\u00e9b", Actions: [] });
ser("metadata-astral-string", { Version: "v", StartAction: "a", Metadata: "\ud83d\ude00", Actions: [] });
ser("metadata-number", { Version: "v", StartAction: "a", Metadata: 5, Actions: [] });
ser("actions-missing", { Version: "v", StartAction: "a" });
ser("actions-null", { Version: "v", StartAction: "a", Actions: null });
ser("actions-object", { Version: "v", StartAction: "a", Actions: {} });
ser("actions-string", { Version: "v", StartAction: "a", Actions: "ab" });
ser("action-null", { Version: "v", StartAction: "a", Actions: [action(), null] });
ser("action-string", { Version: "v", StartAction: "a", Actions: ["ab"] });
ser("action-no-transitions", { Version: "v", StartAction: "a", Actions: [{ Identifier: "a", Type: "T", Parameters: {} }] });
ser("action-transitions-null", { Version: "v", StartAction: "a", Actions: [action({ Transitions: null })] });
ser("action-transitions-string", { Version: "v", StartAction: "a", Actions: [action({ Transitions: "ab" })] });
ser("action-transitions-number", { Version: "v", StartAction: "a", Actions: [action({ Transitions: 5 })] });
ser("action-transitions-array", { Version: "v", StartAction: "a", Actions: [action({ Transitions: [{ NextAction: "x" }] })] });
ser("action-no-parameters", { Version: "v", StartAction: "a", Actions: [{ Identifier: "a", Transitions: {}, Type: "T" }] });
ser("action-extra-keys", { Version: "v", StartAction: "a", Actions: [action({ Extra: 1, Parameters: { b: [{ d: 1, c: 2 }], a: null } })] });
ser("errors-null", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Errors: null } })] });
ser("errors-object", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Errors: {} } })] });
ser("errors-string", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Errors: "ab" } })] });
ser("conditions-null", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Conditions: null } })] });
ser("conditions-number", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Conditions: 5 } })] });
ser("condition-null", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Conditions: [null] } })] });
ser("condition-shapes", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Conditions: [{}, { NextAction: null }, { Condition: [1, 2] }, [], "xy", true] } })] });
ser("error-shapes", { Version: "v", StartAction: "a", Actions: [action({ Transitions: { Errors: [{}, null, 5, "xy", [1], { z: 1, NextAction: "n", ErrorType: "e" }] } })] });
ser("transitions-extra-keys", `{"Version":"v","StartAction":"a","Actions":[{"Identifier":"a","Type":"T","Parameters":{},"Transitions":{"z":1,"Conditions":[],"10":1,"2":2,"NextAction":"n","Errors":[],"b":0}}]}`);
ser("unicode-sort", `{"Version":"v","StartAction":"a","Settings":{"\\uffff":1,"\\ud83d\\ude00":2,"\\u00e9":3},"Metadata":{"\\uffff":1,"\\ud83d\\ude00":2},"Actions":[]}`);

function errorOf(e) {
  const out = { name: e.name ?? e.constructor?.name, message: e.message };
  if (e.missingTokens !== undefined) out.missingTokens = e.missingTokens;
  if (e.missingRefs !== undefined) out.missingRefs = e.missingRefs;
  return out;
}

function run(c) {
  const doc = JSON.parse(c.doc);
  const calls = [];
  let content;
  try {
    if (c.fn === "map") {
      content = materializeWithMap(doc, JSON.parse(c.map));
    } else {
      const fixture = JSON.parse(vendored("materialize/binder-passthrough/binder.json"));
      content = materializeWithBinder(doc, (ref) => {
        calls.push({ ...ref });
        return c.binder === "fixture" ? fixture[ref.token] : `B(${ref.token})`;
      });
    }
  } catch (e) {
    return { ...c, calls, error: errorOf(e) };
  }
  const out = { ...c, calls, content: JSON.stringify(content) };
  try {
    out.serialized = serializeContent(content);
  } catch (e) {
    out.serializeError = errorOf(e);
  }
  if (JSON.stringify(JSON.parse(c.doc)) !== JSON.stringify(doc)) throw new Error(`${c.name}: input mutated`);
  return out;
}

function runSerialize(c) {
  try {
    return { ...c, serialized: serializeContent(JSON.parse(c.content)) };
  } catch (e) {
    return { ...c, error: errorOf(e) };
  }
}

for (const list of [cases, serializeCases]) {
  const names = new Set();
  for (const c of list) {
    if (names.has(c.name)) throw new Error(`duplicate case ${c.name}`);
    names.add(c.name);
  }
}

writeFileSync(
  new URL("ts-oracle.json", import.meta.url),
  JSON.stringify(
    {
      generatedFrom: `flow-as-code ${commit}, packages/core/src/materialize.ts built with tsc`,
      materialize: cases.map(run),
      serializeContent: serializeCases.map(runSerialize),
    },
    null,
    1,
  ) + "\n",
);
