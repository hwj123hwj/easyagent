const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const http = require("node:http");
const os = require("node:os");
const { EventEmitter } = require("node:events");
function fixture(options = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "easyagent-main-provider-"));
  const handlers = new Map(),
    requests = [],
    terminalEvents = [],
    terminalSockets = [],
    dockIcons = [],
    events = {};
  const frame = {},
    contents = {
      mainFrame: frame,
      send(channel, value) { if (channel === "terminal-event") terminalEvents.push(value); },
      setWindowOpenHandler() {},
      on() {},
      getURL: () => "http://localhost:5173",
    };
  let url = "https://first.example",
    token = "private-api-token",
    callback;
  let providerStore, manager, windowOptions, currentWindow;
  const electron = {
    app: {
      isPackaged: options.packaged === true,
      dock: { setIcon: (icon) => dockIcons.push(icon) },
      requestSingleInstanceLock: () => true,
      whenReady: () => Promise.resolve(),
      on: (name, cb) => {
        events[name] = cb;
      },
      quit() {},
      getPath: () => directory,
    },
    BrowserWindow: class extends EventEmitter {
      constructor(options) {
        super();
        windowOptions = options;
        currentWindow = this;
        this.webContents = contents;
        this.buttonPositions = [];
        this.destroyed = false;
        this.fullScreen = false;
      }
      async loadURL() {}
      async loadFile() {}
      setWindowButtonPosition(position) { this.buttonPositions.push({ ...position }); }
      isDestroyed() { return this.destroyed; }
      isFullScreen() { return this.fullScreen; }
    },
    ipcMain: { handle: (name, fn) => handlers.set(name, fn) },
    shell: {
      openExternal: async () => {
        url = "https://other.example";
        token = "other-secret";
        if (callback) await callback();
      },
    },
    dialog: {showSaveDialog: async()=>({canceled:!!options.cancelExport,filePath:path.join(directory,"exported.md")})},
    clipboard: { writeText() {} },
    safeStorage: {
      isEncryptionAvailable: () => true,
      getSelectedStorageBackend: () => "keychain",
      encryptString: (value) => Buffer.from(value.split("").reverse().join("")),
      decryptString: (value) => value.toString().split("").reverse().join(""),
    },
  };
  const module = { exports: {} };
  const fakeFetch = async (resource, requestOptions) => {
    requests.push({ resource, options: requestOptions });
    const body = requestOptions.body ? JSON.parse(requestOptions.body) : null;
    if (resource.includes("/workspace/file-data")) return {ok:true,text:async()=>JSON.stringify({data:Buffer.from("exported-file").toString("base64"),mimeType:"text/plain",name:"report.md"})};
    if (resource.endsWith("/admin/deploy")) {
      return { ok: !options.busy, status: options.busy ? 409 : 200,
        text: async () => JSON.stringify(options.busy ? { error: "sensitive-upstream-error" } : { lease: "owned-lease" }) };
    }
    if (resource.includes("/login") && !resource.includes("/complete")) {
      callback = () =>
        new Promise((resolve, reject) => {
          const target = new URL(body.redirect_url);
          target.searchParams.set("state", "expected-state");
          target.searchParams.set("code", "temporary-test-code");
          http
            .get(target, (res) => {
              res.resume();
              res.on("end", () => resolve());
            })
            .on("error", reject);
        });
      return {
        ok: true,
        text: async () =>
          JSON.stringify({
            state: "expected-state",
            authorization_url: "https://oauth.example/authorize",
          }),
      };
    }
    return { ok: true, text: async () => JSON.stringify({ ok: true }) };
  };
  const providerModule = { exports: {} };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../.test-output/electron/provider-store.js"), "utf8"), {
    module: providerModule, exports: providerModule.exports,
    require: (name) => name === "electron" ? electron : require(name),
    Buffer, process, URL, AbortSignal, fetch: fakeFetch,
  });
  const fakes = {
    electron: electron,
    "./easyagent-manager": {
      EasyAgentManager: class {
        constructor(environment) { this.environment = environment; this.restarts = 0; this.stops = 0; this.url = "http://127.0.0.1:9999"; manager = this; }
        onStatus() {}
        async start() {
          return { url: this.url };
        }
        get token() {
          return "local-secret";
        }
        getStatus() {
          return { state: "ready" };
        }
        getServerInfo() { return { url: this.url, port: 9999 }; }
        async restart() {
          this.restarts++;
          this.appliedEnvironment = this.environment();
          if (this.restarts <= (options.failedRestarts || 0)) throw new Error("private-provider-secret-in-restart-error");
          this.url = `http://127.0.0.1:${9999 + this.restarts}`;
          return { url: this.url };
        }
        async stop() { this.stops++; }
      },
    },
    "./profile-store": {
      ProfileStore: class {
        get() {
          return { id: "remote", kind: options.local ? "local" : "remote", url };
        }
        token() {
          return token;
        }
        list() {
          return { profiles: [], selected: "remote" };
        }
        save() {}
        select() {}
      },
    },
    "./provider-store": {
      ProviderStore: class extends providerModule.exports.ProviderStore {
        constructor() { super(); providerStore = this; }
      },
    },
    "./update-checker": { checkForUpdate: async () => null },
  };
  if (options.terminals) fakes.ws = class extends EventEmitter {
    static OPEN = 1;
    constructor(url, options) { super(); this.url = url; this.options = options; this.sent = []; this.readyState = 1; terminalSockets.push(this); }
    send(data) { this.sent.push(data); }
    close() { this.readyState = 3; this.emit("close"); }
  };
  vm.runInNewContext(
    fs.readFileSync(
      path.join(__dirname, "../.test-output/electron/main.js"),
      "utf8",
    ),
    {
      module,
      exports: module.exports,
      require: (name) => fakes[name] || require(name),
      __dirname: options.electronDirectory || "/tmp/easyagent-main-test",
      process: { ...process, platform: options.platform || process.platform },
      Buffer,
      URL,
      URLSearchParams,
      FormData,
      Blob,
      AbortSignal,
      fetch: fakeFetch,
      setTimeout,
      clearTimeout,
      setImmediate,
    },
    { filename: "main.js" },
  );
  return {
    handlers,
    terminalEvents,
    terminalSockets,
    exported:()=>fs.readFileSync(path.join(directory,"exported.md"),"utf8"),
    requests,
    dockIcons,
    store: () => providerStore,
    manager: () => manager,
    windowOptions: () => windowOptions,
    window: () => currentWindow,
    cleanup: () => fs.rmSync(directory, { recursive: true, force: true }),
    event: { sender: contents, senderFrame: frame },
    ready: () => new Promise((resolve) => setImmediate(resolve)),
  };
}
test("only unpackaged macOS uses the source PNG for its Dock icon", async (t) => {
  for (const platform of ["darwin", "win32", "linux"]) {
    for (const packaged of [false, true]) {
      const f = fixture({
        platform,
        packaged,
        electronDirectory: path.resolve(__dirname, "../dist/electron"),
      });
      t.after(f.cleanup);
      await f.ready();
      const expected = platform === "darwin" && !packaged
        ? [path.resolve(__dirname, "../src/assets/app-icon.png")]
        : [];
      assert.deepEqual(f.dockIcons, expected, `${platform}, packaged=${packaged}`);
      assert.ok(f.window(), "icon setup does not interrupt window creation");
    }
  }
});
test("macOS integrates native window controls without changing other platforms or renderer isolation", async (t) => {
  for (const platform of ["darwin", "win32", "linux"]) {
    const f = fixture({ platform });
    t.after(f.cleanup);
    await f.ready();
    const options = f.windowOptions();
    assert.equal(options.titleBarStyle, platform === "darwin" ? "hiddenInset" : undefined);
    assert.notEqual(options.frame, false, "native window controls remain available");
    assert.equal(options.webPreferences.contextIsolation, true);
    assert.equal(options.webPreferences.nodeIntegration, false);
    assert.equal(options.webPreferences.sandbox, true);
  }
});
test("completed macOS resize, zoom, unzoom and fullscreen exit restore the native button position", async (t) => {
  const f = fixture({ platform: "darwin" });
  t.after(f.cleanup);
  await f.ready();
  const window = f.window();
  for (const event of ["resized", "maximize", "unmaximize", "leave-full-screen"]) {
    window.buttonPositions.length = 0;
    window.emit(event);
    assert.equal(window.buttonPositions.length, 0, "restore waits until after the native layout event");
    await f.ready();
    assert.deepEqual(window.buttonPositions, [{ x: 14, y: 16 }], event);
  }
});
test("macOS fullscreen transition resize and fullscreen events do not reposition native controls", async (t) => {
  const f = fixture({ platform: "darwin" });
  t.after(f.cleanup);
  await f.ready();
  const window = f.window();
  // Native entry is asynchronous: isFullScreen can still be false during resize.
  window.emit("resize");
  await f.ready();
  assert.equal(window.buttonPositions.length, 0);
  window.fullScreen = true;
  window.emit("enter-full-screen");
  for (const event of ["resized", "maximize", "unmaximize", "leave-full-screen"])
    window.emit(event);
  await f.ready();
  assert.equal(window.buttonPositions.length, 0);
  window.fullScreen = false;
  window.emit("leave-full-screen");
  await f.ready();
  assert.deepEqual(window.buttonPositions, [{ x: 14, y: 16 }]);
});
test("a queued macOS button restore is skipped when the window enters fullscreen before it runs", async (t) => {
  const f = fixture({ platform: "darwin" });
  t.after(f.cleanup);
  await f.ready();
  const window = f.window();
  window.emit("resized");
  window.fullScreen = true;
  await f.ready();
  assert.equal(window.buttonPositions.length, 0);
});
test("a queued macOS button restore is skipped when its window is destroyed before it runs", async (t) => {
  const f = fixture({ platform: "darwin" });
  t.after(f.cleanup);
  await f.ready();
  const window = f.window();
  window.emit("maximize");
  window.destroyed = true;
  await f.ready();
  assert.equal(window.buttonPositions.length, 0);
});
test("destroyed macOS windows ignore native button restoration events", async (t) => {
  const f = fixture({ platform: "darwin" });
  t.after(f.cleanup);
  await f.ready();
  const window = f.window();
  window.destroyed = true;
  for (const event of ["resized", "maximize", "unmaximize", "leave-full-screen"])
    window.emit(event);
  await f.ready();
  assert.equal(window.buttonPositions.length, 0);
});
test("Windows and Linux never reposition native window buttons", async (t) => {
  for (const platform of ["win32", "linux"]) {
    const f = fixture({ platform });
    t.after(f.cleanup);
    await f.ready();
    const window = f.window();
    for (const event of ["resize", "resized", "maximize", "unmaximize", "enter-full-screen", "leave-full-screen"])
      window.emit(event);
    await f.ready();
    assert.equal(window.buttonPositions.length, 0, platform);
  }
});
test("main process supplies authentication without exposing it or accepting child-frame IPC", async (t) => {
  const f = fixture();
  t.after(f.cleanup);
  await f.ready();
  const request = f.handlers.get("agent-request");
  await request(f.event, "GET", "/mcp");
  assert.equal(
    f.requests[0].options.headers.Authorization,
    "Bearer private-api-token",
  );
  assert.equal(f.requests[0].options.headers.Origin, undefined);
  assert.equal(f.requests[0].options.redirect, "error");
  assert.throws(
    () => request({ sender: f.event.sender, senderFrame: {} }, "GET", "/mcp"),
    /不允许此窗口/,
  );
  await assert.rejects(
    request(f.event, "GET", "//outside.example"),
    /请求路径/,
  );
});
test("desktop OAuth callback completes on the issuing profile when the selected connection changes", async (t) => {
  const f = fixture();
  t.after(f.cleanup);
  await f.ready();
  await f.handlers.get("mcp-login")(f.event, "service", "/project");
  const [login, complete] = f.requests;
  assert(login.resource.startsWith("https://first.example/"));
  assert(complete.resource.startsWith("https://first.example/"));
  assert.equal(
    complete.options.headers.Authorization,
    "Bearer private-api-token",
  );
  const body = JSON.parse(complete.options.body);
  assert.equal(body.state, "expected-state");
  assert.equal(body.code, "temporary-test-code");
  assert.equal(f.requests.length, 2);
});

const providerInput = { mode: "override", provider: "openai", baseUrl: "http://localhost:4001", model: "selected-model", apiKey: "private-provider-token-123" };
test("local model IPC rejects remote profiles and child-frame callers", async (t) => {
  const f = fixture(); t.after(f.cleanup); await f.ready();
  assert.throws(() => f.handlers.get("provider-config")(f.event), /只适用于/);
  assert.throws(() => f.handlers.get("provider-check")(f.event, providerInput), /只适用于/);
  await assert.rejects(f.handlers.get("provider-save")(f.event, providerInput), /只适用于/);
  assert.throws(() => f.handlers.get("provider-config")({ sender: f.event.sender, senderFrame: {} }), /不允许此窗口/);
  assert.equal(f.manager().restarts, 0);
  assert.equal(f.requests.length, 0);
});
test("saving local upstream settings locks only the managed core and returns no credentials", async (t) => {
  const f = fixture({ local: true }); t.after(f.cleanup); await f.ready();
  const result = await f.handlers.get("provider-save")(f.event, providerInput);
  assert.equal(result.mode, "override");
  assert.equal(result.hasKey, true);
  assert(!JSON.stringify(result).includes(providerInput.apiKey));
  assert.equal(f.manager().restarts, 1);
  assert.equal(f.manager().appliedEnvironment.EA_API_KEY, providerInput.apiKey);
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].resource, "http://127.0.0.1:9999/admin/deploy");
  assert.equal(f.requests[0].options.method, "POST");
  assert.equal(f.requests[0].options.headers.Authorization, "Bearer local-secret");
});
test("active local runs reject model save before persistence or managed-process restart", async (t) => {
  const f = fixture({ local: true, busy: true }); t.after(f.cleanup); await f.ready();
  await assert.rejects(f.handlers.get("provider-save")(f.event, providerInput), /任务运行|无法确认空闲/);
  assert.equal(f.store().configuration().mode, "inherit");
  assert.equal(f.manager().restarts, 0);
});
test("invalid settings release the acquired idle lease without interrupting the core", async (t) => {
  const f = fixture({ local: true }); t.after(f.cleanup); await f.ready();
  await assert.rejects(f.handlers.get("provider-save")(f.event, { ...providerInput, apiKey: "" }), /必须填写/);
  assert.equal(f.manager().restarts, 0);
  assert.equal(f.requests.length, 2);
  assert.equal(f.requests[1].options.method, "DELETE");
  assert.equal(JSON.parse(f.requests[1].options.body).lease, "owned-lease");
});
test("failed apply restores the old encrypted configuration and attempts to recover its core", async (t) => {
  const f = fixture({ local: true, failedRestarts: 1 }); t.after(f.cleanup); await f.ready();
  f.store().save(providerInput);
  await assert.rejects(f.handlers.get("provider-save")(f.event, { ...providerInput, model: "replacement-model", apiKey: "replacement-private-token" }), (error) => /恢复原模型配置/.test(error.message) && !error.message.includes("private-provider-secret"));
  assert.equal(f.manager().restarts, 2);
  assert.equal(f.store().configuration().model, "selected-model");
  assert.equal(f.store().environment().EA_API_KEY, providerInput.apiKey);
  assert.equal(f.manager().appliedEnvironment.EA_MODEL, "selected-model");
});

test("native export uses the selected authenticated host and cancellation performs no request",async t=>{
 const f=fixture();t.after(f.cleanup);await f.ready();
 await assert.rejects(f.handlers.get("export-file")(f.event,"s","/report.md","wrong-host"),/主机已切换/);
 assert.equal(f.requests.length,0);
 await f.handlers.get("export-file")(f.event,"s","/report.md","remote");
 assert.equal(f.exported(),"exported-file");assert.match(f.requests[0].resource,/session_id=s/);
 assert.equal(f.requests[0].options.headers.Authorization,"Bearer private-api-token");
 const canceled=fixture({cancelExport:true});t.after(canceled.cleanup);await canceled.ready();
 await canceled.handlers.get("export-file")(canceled.event,"s","/report.md","remote");assert.equal(canceled.requests.length,0);
});
test("session preference PATCH is allowed through authenticated native transport",async t=>{
 const f=fixture();t.after(f.cleanup);await f.ready();await f.handlers.get("agent-request")(f.event,"PATCH","/sessions/s",{title:"renamed"});
 assert.equal(f.requests[0].options.method,"PATCH");assert.equal(JSON.parse(f.requests[0].options.body).title,"renamed");
});

test("interactive terminal uses the selected authenticated host and never sends its key to renderer", async (t) => {
  const f = fixture({ terminals: true }); t.after(f.cleanup); await f.ready();
  await f.handlers.get("terminal-open")(f.event, "terminal-1", "session/project", "remote", 90, 20);
  const socket = f.terminalSockets[0];
  assert.equal(socket.url.host, "first.example");
  assert.equal(socket.url.pathname, "/terminal");
  assert.equal(socket.url.searchParams.get("session_id"), "session/project");
  assert.equal(socket.url.searchParams.has("token"), false);
  assert.equal(socket.options.headers.Authorization, "Bearer private-api-token");
  socket.emit("open"); socket.emit("message", Buffer.from("中文\x1b[31m"), true);
  assert.equal(Buffer.from(f.terminalEvents[1].data, "base64").toString(), "中文\x1b[31m");
  assert.equal(JSON.stringify(f.terminalEvents).includes("private-api-token"), false);
  await f.handlers.get("terminal-send")(f.event, "terminal-1", { type: "input", data: "pwd\r" });
  assert.deepEqual(JSON.parse(socket.sent[0]), { type: "input", data: "pwd\r" });
  await f.handlers.get("profiles-select")(f.event, "other");
  assert.equal(socket.readyState, 3);
  await f.handlers.get("terminal-send")(f.event, "terminal-1", { type: "input", data: "old-host" });
  assert.equal(socket.sent.length, 1);
  await assert.rejects(f.handlers.get("terminal-open")(f.event, "terminal-2", "session", "wrong", 80, 24), /主机已切换/);
});

test("closing a terminal while the endpoint is resolving does not start an orphan shell", async (t) => {
  const f = fixture({ terminals: true, local: true }); t.after(f.cleanup); await f.ready();
  let release;
  f.manager().start = () => new Promise((resolve) => { release = resolve; });
  const pending = f.handlers.get("terminal-open")(f.event, "pending", "session", "remote", 80, 24);
  await f.handlers.get("terminal-close")(f.event, "pending");
  release({ url: "http://127.0.0.1:9999" }); await pending;
  assert.equal(f.terminalSockets.length, 0);
});
