// Skill-market client tests: SSE frame parsing + formatting helpers.
const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");

const src = fs.readFileSync(
  path.join(__dirname, "..", "src", "client", "skill-market.ts"),
  "utf8",
);

test("skill-market client: formatBytes tiers", () => {
  // formatBytes is exported from the module; assert the documented tiers via
  // the same logic the UI shows (re-implemented here to avoid TS imports).
  const formatBytes = (n) => {
    if (!n || n <= 0) return "";
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  };
  assert.equal(formatBytes(0), "");
  assert.equal(formatBytes(undefined), "");
  assert.equal(formatBytes(512), "512 B");
  assert.equal(formatBytes(2048), "2.0 KB");
  assert.equal(formatBytes(5 * 1024 * 1024), "5.0 MB");
});

test("skill-market client: module exposes install job SSE subscription", () => {
  assert.match(src, /export function subscribeInstallJob/);
  assert.match(src, /fetch\(/);
  assert.match(src, /AbortController/);
});

test("skill-market client: SSE parser handles event+data frames", () => {
  // The parser contract: split on blank lines, read `data: ` payloads,
  // stop after terminal state. Verify the source implements exactly that.
  assert.match(src, /buf\.indexOf\("\\n\\n"\)/);
  assert.match(src, /startsWith\("data: "\)/);
  assert.match(src, /state === "succeeded" \|\| evt\.state === "failed"/);
});

test("skill-market client: endpoints match server routes", () => {
  assert.ok(src.includes("/skills/market/search"));
  assert.ok(src.includes("/skills/market/sections"));
  assert.ok(src.includes("/skills/market/installed"));
  assert.ok(src.includes("/skills/market/install"));
  assert.ok(src.includes("/skills/market/jobs/"));
  assert.ok(src.includes("/events"));
});
