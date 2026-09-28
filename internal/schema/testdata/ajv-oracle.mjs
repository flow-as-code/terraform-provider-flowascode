// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0
//
// Records what Ajv2020({ allErrors: true, strict: false }), the validator
// packages/core/src/conformance.test.ts and packages/cli/src/docs.ts use,
// returns for each document schema_test.go's TestWriteAjvInput writes.
//
//   node ajv-oracle.mjs <input.json> <flow-as-code checkout> > ajv-oracle.json
//
// ajv is loaded from the checkout's packages/core; the schemas are the ones
// vendored beside this package (internal/conformance/data/schema), so the
// oracle and the Go validator read the same bytes.

import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const [inputPath, checkout] = process.argv.slice(2);
if (!inputPath || !checkout) {
  console.error("usage: node ajv-oracle.mjs <input.json> <flow-as-code checkout>");
  process.exit(2);
}
const require = createRequire(join(checkout, "packages/core/package.json"));
const { Ajv2020 } = require("ajv/dist/2020.js");
const ajvVersion = require("ajv/package.json").version;

const here = dirname(fileURLToPath(import.meta.url));
const schemaDir = join(here, "../../conformance/data/schema");
const validators = {};
for (const version of ["0.1", "0.2"]) {
  const schema = JSON.parse(readFileSync(join(schemaDir, `flowdoc-${version}.schema.json`), "utf8"));
  validators[version] = new Ajv2020({ allErrors: true, strict: false }).compile(schema);
}
const commit = readFileSync(join(here, "../../conformance/COMMIT"), "utf8").trim();

const record = (validate, value) => {
  const valid = validate(value);
  const errors = (validate.errors ?? []).map((e) => ({
    instancePath: e.instancePath,
    schemaPath: e.schemaPath,
    keyword: e.keyword,
    message: e.message,
    property:
      e.params?.additionalProperty ??
      (e.keyword === "propertyNames" ? e.params?.propertyName : undefined) ??
      e.propertyName ??
      "",
  }));
  return { valid, errors };
};

const input = JSON.parse(readFileSync(inputPath, "utf8"));
const out = { generatedFrom: commit, ajv: ajvVersion, cases: [], synthetic: [] };
for (const c of input.cases) {
  const r = record(validators[c.version], JSON.parse(c.doc));
  out.cases.push({ id: c.id, version: c.version, sha256: c.sha256, ...r });
}
for (const c of input.synthetic) {
  const validate = new Ajv2020({ allErrors: true, strict: false }).compile(JSON.parse(c.schema));
  out.synthetic.push({ id: c.id, sha256: c.sha256, ...record(validate, JSON.parse(c.doc)) });
}
process.stdout.write(JSON.stringify(out, null, 1) + "\n");
