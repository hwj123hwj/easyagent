const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");
const http = require("node:http");

function fixture(options = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "easyagent-provider-"));
  const module = { exports: {} };
  const electron = {
    app: { getPath: () => dir },
    safeStorage: {
      isEncryptionAvailable: () => options.available !== false,
      getSelectedStorageBackend: () => options.backend || "keychain",
      encryptString: (value) => Buffer.from(value.split("").reverse().join("")),
      decryptString: (value) => value.toString().split("").reverse().join(""),
    },
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../.test-output/electron/provider-store.js"), "utf8"), {
    module, exports: module.exports,
    require: (name) => name === "electron" ? electron : name === "fs" && options.failWrite ? { ...fs, renameSync() { throw new Error("private-key-in-low-level-error"); } } : require(name),
    Buffer, process: { platform: options.platform || process.platform }, URL, AbortSignal, fetch: options.fetch || fetch,
  });
  return { dir, ...module.exports, cleanup: () => fs.rmSync(dir, { recursive: true, force: true }) };
}
const input = (baseUrl = "http://localhost:4001") => ({ mode: "override", provider: "openai", baseUrl, model: "actual-model", apiKey: "private-provider-token-123" });

test("provider key is encrypted privately and only hasKey reaches the public configuration", () => {
  const f = fixture();
  try {
    const store = new f.ProviderStore();
    assert.equal(store.configuration().mode, "inherit");
    const saved = store.save(input());
    assert.equal(saved.hasKey, true);
    assert(!JSON.stringify(saved).includes(input().apiKey));
    assert(!Object.hasOwn(saved, "encryptedKey"));
    const file = path.join(f.dir, "provider-config.json");
    assert(!fs.readFileSync(file, "utf8").includes(input().apiKey));
    assert.equal(fs.statSync(file).mode & 0o777, 0o600);
    assert.equal(store.environment().EA_API_KEY, input().apiKey);
    assert.equal(store.environment().EA_MODEL, "actual-model");
    assert.equal(store.environment().EA_BASE_URL, "http://localhost:4001");
    assert.equal(store.environment().EA_SERVER_API_KEY, undefined);
  } finally { f.cleanup(); }
});
test("blank key keeps the saved credential, provider changes need their own key and inherit removes overrides", () => {
  const f = fixture();
  try {
    const store = new f.ProviderStore();
    store.save(input());
    store.save({ ...input(), model: "second-model", apiKey: "  " });
    assert.equal(store.environment().EA_API_KEY, input().apiKey);
    assert.equal(store.configuration().model, "second-model");
    assert.throws(() => store.save({ ...input(), provider: "anthropic", apiKey: "" }), /必须填写/);
    store.save({ ...input(), provider: "anthropic" });
    assert.equal(store.environment().ANTHROPIC_API_KEY, input().apiKey);
    assert.equal(store.environment().EA_PROVIDER, "anthropic");
    assert.equal(store.environment().EA_API_KEY, undefined);
    store.save({ mode: "inherit" });
    assert.equal(store.configuration().hasKey, false);
    assert.equal(Object.keys(store.environment()).length, 0);
    assert(!fs.existsSync(path.join(f.dir, "provider-config.json")));
  } finally { f.cleanup(); }
});
test("unsafe URLs and invalid credentials are rejected without guessing whether a key is real", () => {
  const f = fixture();
  try {
    const store = new f.ProviderStore();
    for (const baseUrl of ["file:///tmp/model", "https://user:private@example.com", "http://localhost:4001?key=private", "http://localhost/#private", "not a URL"])
      assert.throws(() => store.save(input(baseUrl)), /地址/);
    for (const apiKey of ["bad\nkey", "x".repeat(8193)])
      assert.throws(() => store.save({ ...input(), apiKey }), /无效/);
    assert.throws(() => store.save({ ...input(), apiKey: "" }), /必须填写/);
    assert.equal(f.normalizeProviderURL("http://127.0.0.1:4001/v1/"), "http://127.0.0.1:4001");
    assert.equal(f.normalizeProviderURL("https://example.com/proxy/v1"), "https://example.com/proxy");
    assert(!fs.existsSync(path.join(f.dir, "provider-config.json")));
    store.save({ ...input(), apiKey: "test-key" });
    assert.equal(store.environment().EA_API_KEY, "test-key");
  } finally { f.cleanup(); }
});
test("secure-storage and persistence failures are safe and never save plaintext", () => {
  for (const options of [{ available: false }, { platform: "linux", backend: "basic_text" }, { failWrite: true }]) {
    const f = fixture(options);
    try {
      const store = new f.ProviderStore();
      assert.throws(() => store.save(input()), (error) => !error.message.includes(input().apiKey) && !error.message.includes("private-key-in-low-level-error"));
      assert(!fs.existsSync(path.join(f.dir, "provider-config.json")));
      assert.equal(fs.readdirSync(f.dir).length, 0);
    } finally { f.cleanup(); }
  }
});
test("connection check performs only authenticated model-list GET and never persists an unsaved key", async () => {
  const requests = [];
  const server = http.createServer((req, res) => {
    requests.push({ method: req.method, path: req.url, authorization: req.headers.authorization, anthropic: req.headers["x-api-key"] });
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ data: [{ id: "advertised-model", display_name: "Advertised" }] }));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const f = fixture();
  try {
    const store = new f.ProviderStore(), baseUrl = `http://127.0.0.1:${server.address().port}/v1`;
    for (const provider of ["openai", "anthropic"]) {
      const result = await store.check({ ...input(baseUrl), provider });
      assert.equal(result.ok, true);
      assert.equal(result.models[0].id, "advertised-model");
      assert(!JSON.stringify(result).includes(input().apiKey));
    }
    assert.equal(requests.length, 2);
    for (const request of requests) {
      assert.equal(request.method, "GET");
      assert.equal(request.path, "/v1/models");
      assert.equal(request.authorization, `Bearer ${input().apiKey}`);
    }
    assert.equal(requests[1].anthropic, input().apiKey);
    assert(!fs.existsSync(path.join(f.dir, "provider-config.json")));
    assert.equal((await store.check({ mode: "inherit" })).ok, false);
    assert.equal(requests.length, 2);
  } finally {
    f.cleanup();
    await new Promise((resolve) => server.close(resolve));
  }
});
test("connection failures and malicious model metadata cannot echo upstream secrets", async () => {
  for (const response of [
    { ok: false, status: 401, text: async () => input().apiKey },
    { ok: true, text: async () => JSON.stringify({ data: [{ id: input().apiKey }] }) },
    { ok: true, text: async () => "not-json:" + input().apiKey },
  ]) {
    const f = fixture({ fetch: async () => response });
    try {
      const result = await new f.ProviderStore().check(input());
      assert.equal(result.ok, false);
      assert(!JSON.stringify(result).includes(input().apiKey));
      assert.equal(result.models.length, 0);
    } finally { f.cleanup(); }
  }
  const f = fixture({ fetch: async () => { throw new Error(input().apiKey); } });
  try { assert(!JSON.stringify(await new f.ProviderStore().check(input())).includes(input().apiKey)); }
  finally { f.cleanup(); }
});
