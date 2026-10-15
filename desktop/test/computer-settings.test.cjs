const { test } = require("node:test");
const assert = require("node:assert/strict");
const { parseComputerSettings } = require("../.test-output/src/client/computer-settings.js");
const valid = { enabled: false, provider: "macos:cua", available: false,
  granted: false, approval_policy: "ask", lease_holder: "" };

test("settings rejects HTML and malformed hosts instead of rendering undefined fields", () => {
  for (const response of ["<!DOCTYPE html><html></html>", null, {},
    { ...valid, approved_apps: "editor" }, { ...valid, approved_apps: [7] }]) {
    assert.throws(() => parseComputerSettings(response), /更新 Agent 核心/);
  }
});
test("empty audit fields remain renderable and actual audit records survive", () => {
  for (const apps of [undefined, null, []]) {
    const result = parseComputerSettings({ ...valid, approved_apps: apps });
    assert.equal(result.approved_apps.length, 0);
  }
  assert.deepEqual(parseComputerSettings({ ...valid, approved_apps: ["test.editor"] }).approved_apps, ["test.editor"]);
});
