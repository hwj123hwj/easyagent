const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const http = require("node:http");
function fixture() {
  const handlers = new Map(),
    requests = [],
    events = {};
  const frame = {},
    contents = {
      mainFrame: frame,
      send() {},
      setWindowOpenHandler() {},
      on() {},
      getURL: () => "http://localhost:5173",
    };
  let url = "https://first.example",
    token = "private-api-token",
    callback;
  const electron = {
    app: {
      isPackaged: false,
      requestSingleInstanceLock: () => true,
      whenReady: () => Promise.resolve(),
      on: (name, cb) => {
        events[name] = cb;
      },
      quit() {},
      getPath: () => "/tmp",
    },
    BrowserWindow: class {
      constructor() {
        this.webContents = contents;
      }
      async loadURL() {}
      async loadFile() {}
      on() {}
    },
    ipcMain: { handle: (name, fn) => handlers.set(name, fn) },
    shell: {
      openExternal: async () => {
        url = "https://other.example";
        token = "other-secret";
        if (callback) await callback();
      },
    },
    dialog: {},
    clipboard: { writeText() {} },
  };
  const module = { exports: {} };
  const fakeFetch = async (resource, options) => {
    requests.push({ resource, options });
    const body = options.body ? JSON.parse(options.body) : null;
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
  const fakes = {
    electron: electron,
    "./easyagent-manager": {
      EasyAgentManager: class {
        onStatus() {}
        async start() {
          return { url: "http://127.0.0.1:9999" };
        }
        get token() {
          return "local-secret";
        }
        getStatus() {
          return { state: "ready" };
        }
        async stop() {}
      },
    },
    "./profile-store": {
      ProfileStore: class {
        get() {
          return { kind: "remote", url };
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
    "./update-checker": { checkForUpdate: async () => null },
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
      __dirname: "/tmp/easyagent-main-test",
      process,
      Buffer,
      URL,
      FormData,
      Blob,
      AbortSignal,
      fetch: fakeFetch,
      setTimeout,
      clearTimeout,
    },
    { filename: "main.js" },
  );
  return {
    handlers,
    requests,
    event: { sender: contents, senderFrame: frame },
    ready: () => new Promise((resolve) => setImmediate(resolve)),
  };
}
test("main process supplies authentication without exposing it or accepting child-frame IPC", async () => {
  const f = fixture();
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
test("desktop OAuth callback completes on the issuing profile when the selected connection changes", async () => {
  const f = fixture();
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
