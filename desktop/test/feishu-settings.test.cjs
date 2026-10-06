const { test } = require("node:test");
const assert = require("node:assert/strict");
const { FeishuSettingsController, pairingIsValid, serviceStateLabel, autostartLabel } = require("../.test-output/src/client/feishu-settings.js");

const configured = { managed: true, app_id: "cli_qa", secret_configured: true, paired: false };
const validPairing = () => ({ command: "/pair one-use-qa", expires: new Date(Date.now() + 60000).toISOString() });
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

test("profile change discards sensitive fields and every late read, save, and pairing result", async () => {
  for (const action of ["refresh", "save", "showPairing"]) {
    const pending = deferred();
    let initial = true;
    const controller = new FeishuSettingsController(async () => {
      if (initial) { initial = false; return configured; }
      return pending.promise;
    }, async () => {});
    controller.activate(true);
    await controller.refresh();
    controller.setSecret("qa-only-secret");
    const request = controller[action]();
    controller.activate(true); // New selected profile; earlier requests cannot commit.
    pending.resolve(action === "showPairing" ? validPairing() : { ...configured, paired: true });
    await request;
    const state = controller.getSnapshot();
    assert.equal(state.secret, "");
    assert.equal(state.status, null);
    assert.equal(state.pairing, null);
    assert.equal(state.notice, "");
    assert.equal(state.error, "");
  }
});

test("unmount prevents a pending request from publishing or retaining a credential", async () => {
  const pending = deferred();
  const controller = new FeishuSettingsController(async () => pending.promise, async () => {});
  controller.activate(true);
  controller.setSecret("qa-only-secret");
  let emissions = 0;
  controller.subscribe(() => emissions++);
  const request = controller.refresh();
  controller.dispose();
  const before = emissions;
  pending.resolve(configured);
  await request;
  assert.equal(emissions, before);
  assert.equal(controller.getSnapshot().secret, "");
  assert.equal(controller.getSnapshot().status, null);
});

test("refreshes are read-only and never fetch a pairing command; disconnected requests are skipped", async () => {
  const calls = [];
  const controller = new FeishuSettingsController(async (method, path) => {
    calls.push([method, path]); return configured;
  }, async () => {});
  controller.activate(false);
  await controller.refresh();
  assert.equal(calls.length, 0);
  controller.activate(true);
  await controller.refresh();
  await controller.refresh(true);
  assert.deepEqual(calls, [["GET", "/settings/feishu"], ["GET", "/settings/feishu"]]);
  assert.equal(controller.getSnapshot().pairing, null);
});

test("configured App ID stays fixed and empty Secret preserves the host credential", async () => {
  let payload;
  const controller = new FeishuSettingsController(async (method, path, body) => {
    if (method === "PUT") { payload = body; }
    assert.equal(path, "/settings/feishu");
    return configured;
  }, async () => {});
  controller.activate(true);
  await controller.refresh();
  controller.setAppId("cli_different");
  await controller.save();
  assert.deepEqual(payload, { app_id: "cli_qa", app_secret: "" });
  assert.match(controller.getSnapshot().notice, /仅?重启飞书桥接/);
});

test("failed saving clears the typed secret and surfaces an actionable error", async () => {
  const controller = new FeishuSettingsController(async (method) => {
    if (method === "PUT") throw new Error("桥接重启失败，已恢复旧配置");
    return configured;
  }, async () => {});
  controller.activate(true);
  await controller.refresh();
  controller.setSecret("qa-only-secret");
  await controller.save();
  assert.equal(controller.getSnapshot().secret, "");
  assert.equal(controller.getSnapshot().saving, false);
  assert.match(controller.getSnapshot().error, /已恢复旧配置/);
});

test("first setup requires both fields and unmanaged hosts cannot save or expose pairing actions", async () => {
  const calls = [];
  let status = { managed: true, secret_configured: false };
  const controller = new FeishuSettingsController(async (method, path, body) => {
    calls.push([method, path, body]); return status;
  }, async () => {});
  controller.activate(true);
  await controller.refresh();
  controller.setAppId("cli_first");
  await controller.save();
  assert.equal(calls.length, 1);
  assert.match(controller.getSnapshot().error, /首次配置/);
  controller.setSecret("qa-only-secret");
  status = { managed: false };
  await controller.refresh();
  assert.equal(controller.getSnapshot().secret, "");
  await controller.save();
  await controller.showPairing();
  assert.equal(calls.length, 2);
});

test("pairing codes are requested explicitly, copied through the supplied clipboard, then removed on successful pairing", async () => {
  let paired = false;
  let copies = [];
  const pairing = validPairing();
  const controller = new FeishuSettingsController(async (_, path) =>
    path.endsWith("/pairing") ? pairing : { ...configured, paired }, async (text) => { copies.push(text); });
  controller.activate(true);
  await controller.refresh();
  await controller.showPairing();
  await controller.copyPairing();
  assert.deepEqual(copies, [pairing.command]);
  assert.match(controller.getSnapshot().notice, /不要发到群聊/);
  paired = true;
  await controller.refresh(true);
  assert.equal(controller.getSnapshot().pairing, null);
  await controller.copyPairing();
  assert.equal(copies.length, 1);
});

test("a pairing response arriving after pairing success never restores its one-use code", async () => {
  const pending = deferred();
  let paired = false;
  const controller = new FeishuSettingsController(async (_, path) =>
    path.endsWith("/pairing") ? pending.promise : { ...configured, paired }, async () => {});
  controller.activate(true);
  await controller.refresh();
  const request = controller.showPairing();
  paired = true;
  await controller.refresh(true);
  pending.resolve(validPairing());
  await request;
  assert.equal(controller.getSnapshot().pairing, null);
  assert.equal(controller.getSnapshot().pairingLoading, false);
});

test("a pairing response arriving after management becomes unavailable never reveals its code", async () => {
  const pending = deferred();
  let managed = true;
  const controller = new FeishuSettingsController(async (_, path) =>
    path.endsWith("/pairing") ? pending.promise : { ...configured, managed }, async () => {});
  controller.activate(true);
  await controller.refresh();
  const request = controller.showPairing();
  managed = false;
  await controller.refresh(true);
  pending.resolve(validPairing());
  await request;
  assert.equal(controller.getSnapshot().pairing, null);
  assert.equal(controller.getSnapshot().pairingLoading, false);
});

test("a failed status read can be retried without retaining its stale error", async () => {
  let fail = true;
  const controller = new FeishuSettingsController(async () => {
    if (fail) throw new Error("服务暂不可用");
    return configured;
  }, async () => {});
  controller.activate(true);
  await controller.refresh();
  assert.match(controller.getSnapshot().error, /服务暂不可用/);
  assert.equal(controller.getSnapshot().loading, false);
  fail = false;
  await controller.refresh();
  assert.equal(controller.getSnapshot().error, "");
  assert.equal(controller.getSnapshot().status, configured);
});

test("quiet status polling neither clears nor replaces a failed save, pairing, or copy action", async () => {
  for (const action of ["save", "showPairing", "copyPairing"]) {
    let failRead = false;
    const controller = new FeishuSettingsController(async (method, path) => {
      if (method === "PUT" || (path.endsWith("/pairing") && action === "showPairing"))
        throw new Error("操作未完成");
      if (path.endsWith("/pairing")) return validPairing();
      if (failRead) throw new Error("状态读取失败");
      return configured;
    }, async () => { throw new Error("操作未完成"); });
    controller.activate(true);
    await controller.refresh();
    if (action === "copyPairing") await controller.showPairing();
    await controller[action]();
    await controller.refresh(true);
    assert.equal(controller.getSnapshot().error, "操作未完成");
    assert.equal(controller.getSnapshot().errorSource, "action");
    failRead = true;
    await controller.refresh(true);
    assert.equal(controller.getSnapshot().error, "操作未完成");
    failRead = false;
    await controller.refresh(); // Explicit refresh acknowledges the old action error.
    assert.equal(controller.getSnapshot().error, "");
  }
});

test("quiet status recovery clears a read failure, and retrying an action clears its own error", async () => {
  let failRead = true, failSave = true;
  const controller = new FeishuSettingsController(async (method) => {
    if (method === "PUT" && failSave) throw new Error("保存失败");
    if (method === "GET" && failRead) throw new Error("状态读取失败");
    return configured;
  }, async () => {});
  controller.activate(true);
  await controller.refresh(true);
  assert.equal(controller.getSnapshot().errorSource, "read");
  failRead = false;
  await controller.refresh(true);
  assert.equal(controller.getSnapshot().error, "");
  await controller.save();
  assert.equal(controller.getSnapshot().errorSource, "action");
  failSave = false;
  await controller.save();
  assert.equal(controller.getSnapshot().error, "");
});

test("a status read started before an action fails cannot clear the later action error", async () => {
  const pending = deferred();
  let delay = false;
  const controller = new FeishuSettingsController(async (method, path) => {
    if (path.endsWith("/pairing")) throw new Error("配对失败");
    return delay ? pending.promise : configured;
  }, async () => {});
  controller.activate(true);
  await controller.refresh();
  delay = true;
  const refresh = controller.refresh();
  await controller.showPairing();
  pending.resolve(configured);
  await refresh;
  assert.equal(controller.getSnapshot().error, "配对失败");
});

test("a code expiring while an asynchronous copy waits is not reported as usable", async () => {
  const originalNow = Date.now;
  let now = originalNow();
  Date.now = () => now;
  try {
    const pending = deferred();
    const pairing = { command: "/pair qa", expires: new Date(now + 1000).toISOString() };
    const controller = new FeishuSettingsController(async (_, path) =>
      path.endsWith("/pairing") ? pairing : configured, async () => pending.promise);
    controller.activate(true);
    await controller.refresh();
    await controller.showPairing();
    const copy = controller.copyPairing();
    now += 1000;
    pending.resolve();
    await copy;
    assert.match(controller.getSnapshot().notice, /配对码已过期/);
    assert.doesNotMatch(controller.getSnapshot().notice, /已复制/);
    assert.equal(controller.getSnapshot().copying, false);
  } finally {
    Date.now = originalNow;
  }
});

test("expired, invalid, and malformed pairing codes cannot be copied", async () => {
  const now = Date.now();
  const pairing = { command: "/pair qa", expires: new Date(now + 1).toISOString() };
  assert.equal(pairingIsValid(pairing, now), true);
  assert.equal(pairingIsValid(pairing, now + 1), false);
  assert.equal(pairingIsValid({ ...pairing, expires: "invalid" }, now), false);
  assert.equal(pairingIsValid({ ...pairing, command: "/pair " }, now), false);
  let copies = 0;
  const controller = new FeishuSettingsController(async (_, path) => path.endsWith("/pairing")
    ? { ...pairing, expires: new Date(now - 1).toISOString() } : configured, async () => { copies++; });
  controller.activate(true);
  await controller.refresh();
  await controller.showPairing();
  await controller.copyPairing();
  assert.equal(controller.getSnapshot().pairing, null);
  assert.match(controller.getSnapshot().error, /已过期/);
  assert.equal(copies, 0);
});

test("service labels describe process and autostart state without claiming WebSocket readiness", () => {
  assert.equal(serviceStateLabel("active"), "进程运行中");
  assert.equal(serviceStateLabel("not-supported"), "状态未知");
  assert.equal(autostartLabel("enabled-runtime"), "仅本次运行启用");
  assert.equal(autostartLabel(undefined), "状态未知");
});

const qr = { device_code: "issued", qr_url: "https://open.feishu.cn/page/launcher?user_code=qa", interval: 3, expire_in: 60 };
test("cancelled or disconnected QR polls cannot save credentials or publish late results", async () => {
  for (const cancel of ["hideQR", "dispose", "activate"]) {
    const pending = deferred(), calls = [];
    const controller = new FeishuSettingsController(async (_, path) => {
      calls.push(path);
      if (path.endsWith("/begin")) return qr;
      if (path.endsWith("/poll")) return pending.promise;
      return { managed: false, local_mode: true };
    }, async () => {});
    controller.activate(true); await controller.refresh(); await controller.startQR();
    controller[cancel](true);
    pending.resolve({ authorized: true });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(controller.getSnapshot().qr, null);
    assert.equal(controller.getSnapshot().qrAuthorized, false);
    assert.equal(calls.some(path => path.endsWith("/confirm")), false);
  }
});

test("QR authorization requires explicit confirmation and never claims the bridge is running", async () => {
  const calls = [];
  const controller = new FeishuSettingsController(async (_, path) => {
    calls.push(path);
    if (path.endsWith("/begin")) return qr;
    if (path.endsWith("/poll")) return { authorized: true };
    if (path.endsWith("/confirm")) return { success: true };
    return { managed: false, local_mode: true };
  }, async () => {});
  controller.activate(true); await controller.refresh(); await controller.startQR();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(controller.getSnapshot().qrAuthorized, true);
  assert.equal(calls.some(path => path.endsWith("/confirm")), false);
  await controller.confirmQR();
  assert.equal(calls.filter(path => path.endsWith("/confirm")).length, 1);
  assert.equal(controller.getSnapshot().qr, null);
  assert.match(controller.getSnapshot().notice, /另行启动或重启/);
});

test("superseded begin response and malicious authorization URLs are rejected", async () => {
  const pending = deferred();
  const controller = new FeishuSettingsController(async () => pending.promise, async () => {});
  controller.activate(true);
  const begin = controller.startQR(); controller.hideQR(); pending.resolve(qr); await begin;
  assert.equal(controller.getSnapshot().qr, null);
  const invalid = new FeishuSettingsController(async () => ({ ...qr, qr_url: "https://attacker.test" }), async () => {});
  invalid.activate(true); await invalid.startQR();
  assert.equal(invalid.getSnapshot().qr, null);
  assert.match(invalid.getSnapshot().error, /无效/);
});
