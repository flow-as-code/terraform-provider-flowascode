// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0
// Re-records the per-type catalog facts in ts-oracle.json (the error
// branches, conditions and NextAction rule, and the catalog.ts helpers over
// prompt text, announcements, recording and holding the participant) from
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
    conditionsKind: core.conditionsKind(type) ?? null,
    nextRule: core.nextRule(type) ?? null,
    holdsParticipant: core.holdsParticipant(type),
    textBodyPaths: [...core.textBodyPaths(type)],
    announcePaths: [...core.announcePaths(type)],
    recordingEnablerPath: core.recordingEnablerPath(type) ?? null,
  };
  if (JSON.stringify(next) !== JSON.stringify(entry)) changed.push(type);
  o.perType[type] = next;
}
const head = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
o.generatedFrom = `flow-as-code ${head}, packages/core/dist`;
console.log(JSON.stringify(changed));
if (write) writeFileSync(path, JSON.stringify(o, null, 2) + "\n");
