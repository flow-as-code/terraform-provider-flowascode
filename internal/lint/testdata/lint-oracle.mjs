// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

// Records @flow-as-code/core's own lint output as ts-oracle.json, which
// oracle_test.go replays. Build flow-as-code first (npm run build), then:
//
//   node lint-oracle.mjs <flow-as-code checkout> ts-oracle.json
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const repo = resolve(process.argv[2]);
const core = await import(pathToFileURL(repo + "/packages/core/dist/index.js").href);

const { lint, toJson, toText, hasBlockingFindings, allRules } = core;
const root = repo + "/conformance/";

const docsOf = (parsed) =>
  parsed.docs ?? (parsed.doc === undefined ? [parsed] : [parsed.doc]);

function record(docs, opts) {
  try {
    const f = lint(docs, opts);
    return { json: toJson(f), text: toText(f), blocking: hasBlockingFindings(f) };
  } catch (e) {
    return { error: String(e.message) };
  }
}

// Every conformance/lint fixture, all rules, full output.
const fixtures = [];
for (const dir of readdirSync(root + "lint", { withFileTypes: true })
  .filter((e) => e.isDirectory())
  .map((e) => e.name)
  .sort()) {
  for (const file of readdirSync(root + "lint/" + dir).filter((f) => f.endsWith(".json")).sort()) {
    const p = `lint/${dir}/${file}`;
    fixtures.push({ file: p, ...record(docsOf(JSON.parse(readFileSync(root + p, "utf8")))) });
  }
}
fixtures.push({
  file: "demo/appointment-line.flowdoc.json",
  ...record([JSON.parse(readFileSync(root + "demo/appointment-line.flowdoc.json", "utf8"))]),
});

// Hand-built documents. `input` is JSON text of the docs array, parsed by both
// sides, so key order is whatever the text says.
const act = (Identifier, Type, Parameters = {}, Transitions = {}) => ({
  Identifier,
  Type,
  Parameters,
  Transitions,
});
const doc = (name, actions, extra = {}) => ({
  flowdoc: "0.2",
  kind: "flow",
  name,
  connectType: "CONTACT_FLOW",
  content: { Version: "2019-10-30", StartAction: actions[0]?.Identifier ?? "none", Actions: actions },
  refs: [],
  ...extra,
});
const bye = act("bye", "DisconnectParticipant");
const cases = [];
const add = (name, docsOrText, opts = {}) => {
  const input = typeof docsOrText === "string" ? docsOrText : JSON.stringify(docsOrText);
  const lintOpts = {};
  if (opts.disable) lintOpts.disable = opts.disable;
  if (opts.rules) lintOpts.rules = allRules.filter((r) => opts.rules.includes(r.id));
  cases.push({ name, input, ...opts, ...record(JSON.parse(input), lintOpts) });
};

// Sort: names, block ids and messages where localeCompare and byte order disagree.
const sortDoc = (name) =>
  doc(name, [
    act("start", "MessageParticipant", { Text: "hi" }, { NextAction: "B" }),
    act("B", "CheckHoursOfOperation"),
    act("a", "CheckHoursOfOperation"),
    act("A", "CheckHoursOfOperation"),
    act("_a", "CheckHoursOfOperation"),
    act("-a", "CheckHoursOfOperation"),
    act("a-b", "CheckHoursOfOperation"),
    act("ab", "CheckHoursOfOperation"),
    act("aB", "CheckHoursOfOperation"),
    act("Ab", "CheckHoursOfOperation"),
    act("10", "CheckHoursOfOperation"),
    act("9", "CheckHoursOfOperation"),
    act("Z", "CheckHoursOfOperation"),
    act("~", "CheckHoursOfOperation"),
    act("a b", "CheckHoursOfOperation"),
  ]);
add("sort-locale-order", ["b", "B", "a-b", "a_b", "ab", "A", "_z", "10", "9", "a"].map(sortDoc).concat([sortDoc("B")]));

// Ties under localeCompare (a control character is ignorable) keep the
// stable order: rule order within a doc, walk order within a rule. The raw
// text puts a non-index key before an index key, which JSON.parse reorders.
add(
  "tie-key-order",
  `[{"flowdoc":"0.2","kind":"flow","name":"t","connectType":"CONTACT_FLOW","content":{"Version":"2019-10-30","StartAction":"m","Actions":[` +
    `{"Identifier":"m","Type":"MessageParticipant","Parameters":{"\\u00011":"arn:aws:x:1","b":{"z":"arn:aws-cn:q","0":"arn:aws:q:"},"1":"arn:aws:y:2","L":["x","arn:aws:z:"]},"Transitions":{"NextAction":"bye","Errors":[{"ErrorType":"NoMatchingError","NextAction":"bye"}],"Conditions":[{"NextAction":"bye","Condition":{"Operator":"Equals","Operands":["arn:aws:connect:"]}}]}},` +
    `{"Identifier":"bye","Type":"DisconnectParticipant","Parameters":{},"Transitions":{}}]},"refs":[]}]`,
);
add("tie-doc-names", [doc("a\u0001", [bye]), doc("a", [bye]), doc("\u0001a", [bye])].map((d) => {
  d.content.Actions = [act("x", "Loop")];
  d.content.StartAction = "x";
  return d;
}));

// Odd transition values that pass assertFlowDoc.
add("transition-values", [
  doc("tv", [
    act("s", "MessageParticipant", { Text: "hi" }, {
      NextAction: null,
      Errors: [{ ErrorType: "NoMatchingError" }, 5, "str", { ErrorType: "X", NextAction: 7 }, { NextAction: 1e21 }],
      Conditions: [{ NextAction: true }, { NextAction: { a: 1 } }, { NextAction: [1, null, "x", [2, 3]] }, { NextAction: "bye" }],
    }),
    bye,
    act("hold", "MessageParticipantIteratively", { Messages: [{ Text: "wait" }] }, { NextAction: null }),
  ]),
  doc("missing-start", [bye], {}),
].map((d, i) => {
  if (i === 1) d.content.StartAction = "nowhere";
  return d;
}));

// Tokens and placeholders.
add("tokens", [
  doc("tok", [
    act("m", "MessageParticipant", {
      Text: "${ cdref:queue:x}",
      A: "${ cdref:queue:x}",
      B: "${﻿cdref:queue:x}",
      C: "${\u0085cdref:queue:x}",
      D: "${CDREF:queue:x}",
      E: "${CdReF:queue:x}",
      F: "prefix ${cdref:queue:x}",
      G: "${cdref:queue:indexed}",
      H: "${cdref:queue:missing}",
      I: "${cdref:queue:indexed@blue}",
      J: "${cdref:queue:a} ${cdref:queue:b}",
      K: "Your balance is ${amount}",
      content: { Metadata: "${cdref:bad}" },
      M: "${cdref\nqueue:x}",
      N: "${cdref:queue:UPPER}",
    }, {
      NextAction: "bye",
      Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }],
      Conditions: [{ NextAction: "bye", Condition: { Operator: "Equals", Operands: ["${cdref:queue:cond}", "x${cdref:lambda:y}"] } }],
    }),
    bye,
  ], {
    refs: [
      { token: "${cdref:queue:indexed}", type: "queue", name: "indexed" },
      { token: "arn:aws:connect:us-east-1:1:instance/x", type: "queue", name: "x" },
    ],
  }),
].map((d) => {
  d.content.Metadata = { note: "${cdref:queue:meta}", deep: [{ arn: "arn:aws-us-gov:iam::1:role/r" }], bad: "${cdref:nope" };
  return d;
}));

// Prompt lengths in UTF-16 code units.
const emoji = "\u{1F600}";
add("prompt-length", [
  doc("pl", [
    act("t", "MessageParticipant", { Text: "x".repeat(3001) }, { NextAction: "e", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    act("e", "MessageParticipant", { Text: emoji.repeat(1501) }, { NextAction: "ok", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    act("ok", "MessageParticipant", { Text: emoji.repeat(1500) }, { NextAction: "s", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    act("s", "MessageParticipant", { SSML: "<speak>" + "<b>".repeat(1000) + "y".repeat(3001) + "</speak>" }, { NextAction: "it", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    act("it", "MessageParticipantIteratively", { Messages: [{ Text: "a" }, { SSML: "<a>".repeat(2001) }, { Text: 5 }, { SSML: "q".repeat(3001) }] }, {}),
    bye,
  ]),
]);

// Recording consent and JS whitespace.
const rec = (id, next) =>
  act(id, "UpdateContactRecordingBehavior", { RecordingBehavior: { RecordedParticipants: ["Agent"] } }, { NextAction: next });
add("recording", [
  doc("r-feff", [act("say", "MessageParticipant", { Text: "﻿ 　" }, { NextAction: "rec", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }), rec("rec", "bye"), bye]),
  doc("r-0085", [act("say", "MessageParticipant", { Text: "\u0085" }, { NextAction: "rec", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }), rec("rec", "bye"), bye]),
  doc("r-media", [act("say", "MessageParticipant", { Media: { Uri: " x " } }, { NextAction: "rec", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }), rec("rec", "bye"), bye]),
  doc("r-empty-list", [act("rec", "UpdateContactRecordingBehavior", { RecordingBehavior: { RecordedParticipants: [] } }, { NextAction: "bye" }), bye]),
  doc("r-start", [rec("rec", "bye"), bye]),
  doc("r-iter", [act("it", "MessageParticipantIteratively", { Messages: [{ Text: "" }, { SSML: " " }, { PromptId: "p" }] }, { NextAction: "rec" }), rec("rec", "bye"), bye]),
  doc("r-branch", [
    act("cmp", "Compare", { ComparisonValue: "$.x" }, {
      NextAction: "rec",
      Conditions: [{ NextAction: "say", Condition: { Operator: "Equals", Operands: ["1"] } }],
      Errors: [{ ErrorType: "NoMatchingCondition", NextAction: "rec" }],
    }),
    act("say", "MessageParticipant", { Text: "recorded" }, { NextAction: "rec", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    rec("rec", "bye"),
    bye,
  ]),
]);

// Error branches, terminal blocks, flow types.
add("branches-and-terminals", [
  doc("eb", [
    act("r", "UpdateContactRecordingAndAnalyticsBehavior", { ChatBehavior: null }, { NextAction: "hold", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }, 3] }),
    act("hold", "MessageParticipantIteratively", { Messages: [{ PromptId: "p" }] }, { Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    act("dead", "UpdateFlowLoggingBehavior", { FlowLoggingBehavior: "Enabled" }, {}),
    act("unk", "SomethingNew", {}, { NextAction: "bye" }),
    bye,
  ]),
  doc("whisper", [act("q", "TransferToQueue", {}, {}), act("cb", "CreateCallbackContact", {}, { NextAction: "q" })], { connectType: "AGENT_WHISPER" }),
  doc("no-end", [act("l", "Loop", { LoopCount: "2" }, { NextAction: "l" })]),
  doc("hold-null-next", [act("h", "MessageParticipantIteratively", { Messages: [{ Text: "x" }] }, { NextAction: null })]),
]);

// Module depth.
const invoke = (id, token, next) => act(id, "InvokeFlowModule", { FlowModuleId: token }, next ? { NextAction: next, Errors: [{ ErrorType: "NoMatchingError", NextAction: next }] } : {});
const mod = (name, target) =>
  doc(name, target ? [invoke("i", `\${cdref:module:${target}}`, "r"), act("r", "EndFlowModuleExecution")] : [act("r", "EndFlowModuleExecution")], { kind: "module", connectType: "MODULE" });
add("modules", [
  doc("main", [invoke("i1", "${cdref:module:m1@blue}", "i2"), invoke("i2", "${cdref:module:main}", "i3"), invoke("i3", "${cdref:flow:m1}", "i4"), invoke("i4", 5, "i5"), invoke("i5", "${cdref:module:m-x}", "bye"), bye]),
  mod("m1", "m2"), mod("m2", "m3"), mod("m3", "m4"), mod("m4", "m5"), mod("m5", "m6"), mod("m6", "m7"), mod("m7"),
  mod("m-x", "m-y"), mod("m-y", "m-x"),
  mod("m-x"),
]);

// Names.
add("names", [
  doc("n", [
    act("", "DisconnectParticipant"),
    act("x".repeat(51), "DisconnectParticipant"),
    act(emoji.repeat(25), "DisconnectParticipant"),
    act(emoji.repeat(26), "DisconnectParticipant"),
    act("a:b", "DisconnectParticipant"),
    act("constructor", "DisconnectParticipant"),
    act("dup", "DisconnectParticipant"),
    act("dup", "DisconnectParticipant"),
  ]),
  doc("n", [bye]),
  doc("N", [bye]),
]);

// A repeated Identifier: actionsById keeps the last action under it.
add("duplicate-identifiers", [
  doc("dup", [
    act("s", "MessageParticipant", { Text: "hi" }, { NextAction: "a", Errors: [{ ErrorType: "NoMatchingError", NextAction: "a" }] }),
    act("a", "MessageParticipant", { Text: "one" }, { NextAction: "x1", Errors: [{ ErrorType: "NoMatchingError", NextAction: "x1" }] }),
    act("a", "MessageParticipant", { Text: "two" }, { NextAction: "bye", Errors: [{ ErrorType: "NoMatchingError", NextAction: "bye" }] }),
    act("x1", "DisconnectParticipant"),
    bye,
  ]),
]);

// Engine options.
const orphan = [doc("o", [bye, act("lost", "DisconnectParticipant")])];
add("disable", orphan, { disable: ["reachable-blocks", "no-such-rule"] });
add("disable-hard", orphan, { disable: ["no-unresolved-token", "reachable-blocks", "no-literal-arn"] });
add("disable-hard-one", orphan, { disable: ["no-literal-arn"] });
add("rules-subset", orphan, { rules: ["reachable-blocks", "unique-names"] });
add("rules-subset-hard-absent", orphan, { rules: ["reachable-blocks"], disable: ["no-literal-arn"] });
add("rules-empty", orphan, { rules: [] });
add("assert-first", `[{"name":1}]`, { disable: ["no-literal-arn"] });
add("assert-action", `[{"name":"x","kind":"flow","connectType":"CONTACT_FLOW","content":{"StartAction":"a","Actions":[{"Identifier":"a","Type":"T","Parameters":{}}]}}]`);
add("empty-set", `[]`);

// Collation: pairs over an alphabet that exercises every ASCII class.
let seed = 42;
const rand = (n) => {
  seed = (seed * 1103515245 + 12345) % 2147483648;
  return seed % n;
};
const alphabet = [];
for (let c = 0; c < 128; c++) alphabet.push(String.fromCharCode(c));
const pick = () => {
  const r = rand(10);
  if (r < 4) return "aAbBzZ"[rand(6)];
  if (r < 6) return "-_ .:\"'(){}0123456789"[rand(21)];
  if (r < 7) return String.fromCharCode(rand(32));
  return alphabet[rand(128)];
};
const word = () => {
  let s = "";
  const n = rand(6);
  for (let i = 0; i < n; i++) s += pick();
  return s;
};
const pairs = [];
for (let i = 0; i < 4000; i++) {
  const a = word();
  let b = rand(3) === 0 ? a.replace(/./, (c) => (c === c.toLowerCase() ? c.toUpperCase() : c.toLowerCase())) : word();
  if (rand(4) === 0) b = a + pick();
  pairs.push([a, b, Math.sign(a.localeCompare(b))]);
}
const pool = [];
for (let i = 0; i < 300; i++) pool.push(word());
const sorted = [...pool].sort((a, b) => a.localeCompare(b));

writeFileSync(
  process.argv[3],
  JSON.stringify(
    {
      source: "packages/core/dist built from flow-as-code " + execFileSync("git", ["-C", repo, "rev-parse", "--short", "HEAD"], { encoding: "utf8" }).trim() + ", Node " + process.version + ", ICU " + process.versions.icu + ", locale " + new Intl.Collator().resolvedOptions().locale,
      fixtures,
      cases,
      collation: { pairs, pool, sorted },
    },
    null,
    1,
  ) + "\n",
);
console.log(fixtures.length, "fixtures", cases.length, "cases");
