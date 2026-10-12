// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0
// Re-records the per-type tables in ts-oracle.json (perType and
// catalogOrder) from @flow-as-code/core's own catalog.ts helpers and
// actions.ts tables, the collect cases from its collectRefs, and the migrate
// and path cases from migrateFlowDoc and readPath: every type
// the vendored catalog names, so a type the catalog gains is recorded rather
// than hand-written and a type it loses is flagged, plus the two probes
// oracle_test.go relies on (NotAType, a type no table has; constructor, an
// Object.prototype key, which actions.ts's plain-object tables answer with
// the prototype's function, dropped by JSON.stringify as the Go side
// expects). Every field oracle_test.go checks is recorded here; the rest of
// the file is kept. It prints which types changed so the diff can be read
// before it is committed. Build flow-as-code first (npm run build), then:
//
//   node catalog-oracle.mjs <flow-as-code checkout> ts-oracle.json [--write]
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const repo = resolve(process.argv[2]);
const path = process.argv[3];
const write = process.argv.includes("--write");
const core = await import(pathToFileURL(repo + "/packages/core/dist/index.js").href);
const o = JSON.parse(readFileSync(path, "utf8"));
const probes = ["NotAType", "constructor"];
const types = [...Object.keys(core.actionCatalog.actions), ...probes];
const changed = [];

/**
 * The parameter objects requiredErrorsFor is recorded over for a type: the
 * empty object, then one per conditional error the catalog names. A
 * requiredWhenKey error is recorded with its key present (null, as the
 * chat form was); a requiredWhenValue error with its key at the value and
 * at a value that is not it, so both readings are oracled. Sorted by the
 * JSON of the parameters, so the recording is stable.
 */
function parameterCases(type) {
  const entry = core.modeledEntry(type);
  const cases = new Map([["{}", {}]]);
  for (const e of entry?.transitions.errors ?? []) {
    if (e.requiredWhenKey !== undefined) {
      cases.set(JSON.stringify({ [e.requiredWhenKey]: null }), { [e.requiredWhenKey]: null });
    }
    if (e.requiredWhenValue !== undefined) {
      const { key, equals } = e.requiredWhenValue;
      cases.set(JSON.stringify({ [key]: equals }), { [key]: equals });
      cases.set(JSON.stringify({ [key]: `not-${equals}` }), { [key]: `not-${equals}` });
    }
  }
  return [...cases.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
}

const perType = {};
for (const type of types) {
  const entry = o.perType[type] ?? {};
  const next = {
    modeled: core.modeledEntry(type) !== undefined,
    requiredErrors: core.requiredErrors(type),
    requiredErrorsByParameters: parameterCases(type).map(([, parameters]) => ({
      parameters,
      errors: core.requiredErrorsFor(type, parameters),
    })),
    builderErrors: core.builderErrors(type),
    minConditions: core.minConditionsFor(type),
    conditionsKind: core.conditionsKind(type) ?? null,
    nextRule: core.nextRule(type) ?? null,
    holdsParticipant: core.holdsParticipant(type),
    textBodyPaths: [...core.textBodyPaths(type)],
    announcePaths: [...core.announcePaths(type)],
    recordingEnablerPath: core.recordingEnablerPath(type) ?? null,
    refPaths: core.refPathsOf(type),
    // The table read as the action-allowed-in-flow-type rule reads it, a
    // plain index (packages/core/src/lint/rules/action-allowed-in-flow-type.ts),
    // so the constructor probe yields the prototype's function, which JSON
    // drops, and the Go side sees the key absent.
    restrictions: core.FLOW_TYPE_RESTRICTIONS[type] ?? null,
    unrestricted: core.FLOW_TYPE_UNRESTRICTED.includes(type),
    terminal: core.TERMINAL_ACTIONS.includes(type),
    actionType: Object.values(core.ActionType).includes(type),
    channels:
      core.channelRestriction(type) === undefined ? null : [...core.channelRestriction(type)],
  };
  if (JSON.stringify(next) !== JSON.stringify(entry)) changed.push(type);
  perType[type] = next;
}
for (const type of Object.keys(o.perType)) {
  if (!(type in perType)) changed.push(type);
}
o.perType = perType;
// migrates and paths: each case's input kept, its output recomputed from
// migrateFlowDoc and readPath, so a format bump or a new path form is
// re-recorded rather than hand-edited. A paths input is JSON text, parsed
// here as the Go test parses it.
o.migrates.forEach((c, i) => {
  let out;
  try {
    out = { ok: core.migrateFlowDoc(c.in) };
  } catch (e) {
    out = { err: e.message };
  }
  if (JSON.stringify(out) !== JSON.stringify(c.out)) changed.push(`migrates[${i}]`);
  c.out = out;
});
o.paths.forEach((c, i) => {
  let out;
  try {
    out = { ok: core.readPath(JSON.parse(c.in), c.path) };
  } catch (e) {
    out = { err: e.message };
  }
  if (JSON.stringify(out) !== JSON.stringify(c.out)) changed.push(`paths[${i}]`);
  c.out = out;
});
// collect: each case's input is JSON text, parsed here as the Go test parses
// it, so both sides scan the same bytes.
o.collect.forEach((c, i) => {
  const out = core.collectRefs(JSON.parse(c.in));
  if (JSON.stringify(out) !== JSON.stringify(c.out)) changed.push(`collect[${i}]`);
  c.out = out;
});
const catalogOrder = core.modeledTypes();
if (JSON.stringify(catalogOrder) !== JSON.stringify(o.catalogOrder)) changed.push("catalogOrder");
o.catalogOrder = catalogOrder;
const head = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
o.generatedFrom = `flow-as-code ${head}, packages/core/dist`;
console.log(JSON.stringify(changed));
if (write) writeFileSync(path, JSON.stringify(o, null, 2) + "\n");
