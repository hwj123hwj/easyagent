const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");
function fixture(available = true) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "easyagent-profiles-"));
  const fake = {
    app: { getPath: () => dir },
    safeStorage: {
      isEncryptionAvailable: () => available,
      getSelectedStorageBackend: () => "keychain",
      encryptString: (value) => Buffer.from(value.split("").reverse().join("")),
      decryptString: (value) => value.toString().split("").reverse().join(""),
    },
  };
  const module = { exports: {} };
  vm.runInNewContext(
    fs.readFileSync(
      path.join(__dirname, "../.test-output/electron/profile-store.js"),
      "utf8",
    ),
    {
      module,
      exports: module.exports,
      require: (name) => (name === "electron" ? fake : require(name)),
      Buffer,
      process,
      URL,
    },
  );
  return {
    dir,
    ...module.exports,
    cleanup: () => fs.rmSync(dir, { recursive: true, force: true }),
  };
}
test("saved token is encrypted and never exposed by the public profile list", () => {
  const f = fixture();
  try {
    const store = new f.ProfileStore();
    store.save({
      id: "mini",
      name: "主机",
      url: "http://192.168.5.16:8080",
      token: "secret-token-123",
    });
    const text = fs.readFileSync(path.join(f.dir, "connections.json"), "utf8");
    assert(!text.includes("secret-token-123"));
    assert(!JSON.stringify(store.list()).includes("secret-token-123"));
    assert.equal(store.token("mini"), "secret-token-123");
    assert(store.list().profiles.find((p) => p.id === "mini").hasToken);
  } finally {
    f.cleanup();
  }
});
test("unavailable secure storage refuses token persistence", () => {
  const f = fixture(false);
  try {
    const store = new f.ProfileStore();
    assert.throws(
      () =>
        store.save({
          id: "x",
          name: "x",
          url: "https://example.com",
          token: "secret",
        }),
      /系统安全存储不可用/,
    );
    assert(!fs.existsSync(path.join(f.dir, "connections.json")));
  } finally {
    f.cleanup();
  }
});
test("server address disallows embedded credentials, extra paths and unsafe schemes", () => {
  const f = fixture();
  try {
    for (const value of [
      "file:///tmp/a",
      "https://name:secret@example.com",
      "http://server/path",
      "http://server?token=secret",
    ])
      assert.throws(() => f.normalizeServerURL(value));
    assert.equal(
      f.normalizeServerURL("https://example.com/"),
      "https://example.com",
    );
  } finally {
    f.cleanup();
  }
});
