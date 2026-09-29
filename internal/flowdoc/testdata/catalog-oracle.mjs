// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0
// Re-records the per-type error facts in ts-oracle.json from
// @flow-as-code/core's own catalog functions, keeping every other recorded
// value, and prints which types changed so the diff can be read before it is
// committed. Build flow-as-code first (npm run build), then:
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
const changed = [];
for (const [type, entry] of Object.entries(o.perType)) {
  const next = {
    ...entry,
    requiredErrors: core.requiredErrors(type),
    requiredErrorsForChat: core.requiredErrorsFor(type, { ChatBehavior: null }),
    builderErrors: core.builderErrors(type),
    minConditions: core.minConditionsFor(type),
  };
  if (JSON.stringify(next) !== JSON.stringify(entry)) changed.push(type);
  o.perType[type] = next;
}
const head = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
o.generatedFrom = `flow-as-code ${head}, packages/core/dist`;
console.log(JSON.stringify(changed));
if (write) writeFileSync(path, JSON.stringify(o, null, 2) + "\n");
