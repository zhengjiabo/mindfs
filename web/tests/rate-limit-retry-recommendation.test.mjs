import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import vm from "node:vm";

const sourcePath = path.resolve("src/services/rateLimitRetryRecommendation.ts");
const source = fs.readFileSync(sourcePath, "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2020,
  },
}).outputText;

const sandbox = { exports: {}, module: { exports: {} } };
vm.runInNewContext(compiled, sandbox, { filename: sourcePath });

const {
  rateLimitRetryAttemptSchedule,
  recommendRateLimitRetryBaseDelay,
} = sandbox.exports;

assert.equal(recommendRateLimitRetryBaseDelay(62, 5, 5), 12);
assert.deepEqual([...rateLimitRetryAttemptSchedule(12, 5, 5)], [12, 34, 61]);

assert.equal(recommendRateLimitRetryBaseDelay(62, 0, 5), 15);
assert.deepEqual([...rateLimitRetryAttemptSchedule(15, 5, 0)], [15, 35, 60]);

assert.equal(recommendRateLimitRetryBaseDelay(20, 20, 20), 10);
assert.equal(recommendRateLimitRetryBaseDelay(100000, 0, 0), 3600);
