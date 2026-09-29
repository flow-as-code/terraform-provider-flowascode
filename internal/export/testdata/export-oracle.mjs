// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0
// Re-records ts-oracle.json from @flow-as-code/core's own export.ts, keeping
// every recorded input and recomputing every output, and prints which entries
// changed so the diff can be read before it is committed. Build flow-as-code
// first (npm run build), then:
//
//   node export-oracle.mjs <flow-as-code checkout> ts-oracle.json [--write]
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const repo = resolve(process.argv[2]);
const path = process.argv[3];
const write = process.argv.includes("--write");
const core = await import(pathToFileURL(repo + "/packages/core/dist/index.js").href);
const o = JSON.parse(readFileSync(path, "utf8"));
const changed = {};
const note = (section, i, before, after) => {
  if (JSON.stringify(before) !== JSON.stringify(after)) (changed[section] ??= []).push(i);
};
const errorOf = (e) => ({ name: e.name, message: e.message, ...Object.fromEntries(Object.entries(e)) });
const byArn = (map) =>
  [...map.byArn.entries()]
    .map(([arn, e]) => ({ arn, ...e }))
    .sort((a, b) => (a.arn < b.arn ? -1 : a.arn > b.arn ? 1 : 0));

for (const [section, fn] of [
  ["parseConnectArn", core.parseConnectArn],
  ["parseLambdaFunctionArn", core.parseLambdaFunctionArn],
  ["normalizeArn", core.normalizeArn],
  ["slugifyResourceName", core.slugifyResourceName],
]) {
  o[section].forEach((c, i) => {
    const out = fn(c.input);
    const next = out === undefined ? null : out;
    note(section, i, c.output ?? null, next);
    c.output = next;
  });
}

o.buildReverseMap.forEach((c, i) => {
  const rm = core.buildReverseMap(JSON.parse(c.inventory));
  const next = { byArn: byArn(rm), warnings: rm.warnings };
  note("buildReverseMap", i, { byArn: c.byArn, warnings: c.warnings }, next);
  Object.assign(c, next);
});

o.reverseMapOfResourceMap.forEach((c, i) => {
  const rm = core.reverseMapOfResourceMap(c.map);
  const next = { byArn: byArn(rm), warnings: rm.warnings };
  note("reverseMapOfResourceMap", i, { byArn: c.byArn, warnings: c.warnings }, next);
  Object.assign(c, next);
});

o.exportFlow.forEach((c, i) => {
  const rm = c.resourceMap
    ? core.reverseMapOfResourceMap(c.resourceMap)
    : core.buildReverseMap(JSON.parse(c.inv));
  let result;
  try {
    const doc = core.exportFlow(c.content, rm, c.options);
    result = { doc: core.serialize(doc), refs: JSON.stringify(doc.refs) };
  } catch (e) {
    result = { error: errorOf(e) };
  }
  note("exportFlow", i, c.result, result);
  c.result = result;
});

// The scripted client oracle_test.go builds from the same spec.
function scripted(spec, calls) {
  const inv = spec.inventory;
  const describe = async (id) => {
    calls.push(id);
    const saved = id.endsWith(":$SAVED");
    const base = saved ? id.slice(0, -":$SAVED".length) : id;
    const f = spec.flows[base];
    if (f === undefined) {
      const e = new Error(`No fixture for ${id}`);
      e.name = "ResourceNotFoundException";
      throw e;
    }
    if (f.draft && !saved) {
      const e = new Error(`Flow ${base} has not been published.`);
      e.name = "ContactFlowNotPublishedException";
      throw e;
    }
    return { arn: "", id: id.replace(":$SAVED", ""), name: f.name ?? "", content: f.content, ...f.extra };
  };
  return {
    listContactFlows: async () => inv.contactFlows,
    describeContactFlow: describe,
    listContactFlowModules: async () => inv.contactFlowModules,
    describeContactFlowModule: describe,
    listQueues: async () => inv.queues,
    listHoursOfOperations: async () => inv.hoursOfOperations,
    listPrompts: async () => inv.prompts,
    listLambdaFunctions: async () => inv.lambdaFunctions,
    listBots: async () => inv.lexBots,
    listViews: async () => inv.views,
  };
}

for (const [i, c] of o.exportInstance.entries()) {
  const spec = JSON.parse(c.spec);
  const calls = [];
  let result;
  try {
    const r = await core.exportInstance(scripted(spec, calls), c.options);
    result = {
      flows: r.flows.map((f) => ({
        arn: f.arn,
        id: f.id,
        sourceName: f.sourceName,
        saved: f.saved,
        doc: core.serialize(f.doc),
      })),
      warnings: r.warnings,
      failures: r.failures.map((f) => ({
        arn: f.arn,
        name: f.name,
        reason: f.reason,
        ...(f.unknownArns === undefined ? {} : { unknownArns: f.unknownArns }),
      })),
    };
  } catch (e) {
    result = { error: errorOf(e) };
  }
  note("exportInstance", i, { calls: c.calls, result: c.result }, { calls, result });
  c.calls = calls;
  c.result = result;
}

o.generatedFrom = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
console.log(JSON.stringify(changed));
if (write) writeFileSync(path, JSON.stringify(o, null, 2) + "\n");
