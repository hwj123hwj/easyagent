const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");
const { EventEmitter } = require("node:events");
function managerFixture() {
  let options, child;
  const directories = [];
  const module = { exports: {} };
  const fakeSpawn = (_binary, _args, value) => {
    options = value;
    child = new EventEmitter();
    child.exitCode = null;
    child.kill = () => {
      child.exitCode = 0;
      child.emit("exit", 0);
      return true;
    };
    return child;
  };
  vm.runInNewContext(
    fs.readFileSync(
      path.join(__dirname, "../.test-output/electron/easyagent-manager.js"),
      "utf8",
    ),
    {
      module,
      exports: module.exports,
      require: (name) =>
        name === "electron"
          ? {
              app: {
                isPackaged: false,
                getPath: (name) =>
                  name === "userData" ? "/tmp/easyagent-app-user-data" : "/tmp",
              },
            }
          : name === "child_process"
            ? { spawn: fakeSpawn }
            : name === "fs"
              ? { ...fs, mkdirSync: (...args) => directories.push(args) }
              : require(name),
      __dirname: "/tmp/desktop/dist/electron",
      process: {
        env: {
          EA_API_KEY: "upstream-secret",
          EA_DATA_DIR: "/srv/existing-service/data",
          PATH: "/usr/bin",
        },
        resourcesPath: "/tmp/resources",
      },
      fetch: async () => ({ ok: true }),
      AbortSignal,
      setTimeout,
      clearTimeout,
      Buffer,
    },
  );
  return {
    Manager: module.exports.EasyAgentManager,
    options: () => options,
    child: () => child,
    directories: () => directories,
  };
}
test("local backend uses an independent server token and preserves upstream credentials", async () => {
  const f = managerFixture(),
    manager = new f.Manager();
  await manager.start();
  assert.equal(f.options().env.EA_API_KEY, "upstream-secret");
  assert.equal(f.options().env.EA_SERVER_API_KEY, manager.token);
  assert.equal(f.options().env.EA_ALLOW_NO_AUTH, "0");
  assert.equal(manager.getStatus().state, "ready");
  await manager.stop();
  assert.equal(manager.getStatus().state, "stopped");
});
test("managed core isolates persisted runs from an inherited service data directory", async () => {
  const f = managerFixture(),
    manager = new f.Manager();
  await manager.start();
  assert.equal(
    f.options().env.EA_DATA_DIR,
    "/tmp/easyagent-app-user-data/core-data",
  );
  assert.equal(f.directories().length, 1);
  assert.equal(
    f.directories()[0][0],
    "/tmp/easyagent-app-user-data/core-data",
  );
  assert.equal(f.directories()[0][1].mode, 0o700);
  await manager.stop();
});
test("a backend crash clears readiness and reports a visible failure", async () => {
  const f = managerFixture(),
    manager = new f.Manager();
  await manager.start();
  f.child().emit("exit", 1);
  assert.equal(manager.getServerInfo(), null);
  assert.equal(manager.getStatus().state, "error");
  assert.match(manager.getStatus().message, /已退出/);
  await manager.stop();
});
const moduleUpdate = { exports: {} };
vm.runInNewContext(
  fs.readFileSync(
    path.join(__dirname, "../.test-output/electron/update-checker.js"),
    "utf8",
  ),
  {
    module: moduleUpdate,
    exports: moduleUpdate.exports,
    require: (name) =>
      name === "electron" ? { app: {}, net: {} } : require(name),
    URL,
    AbortSignal,
  },
);
const { desktopUpdate } = moduleUpdate.exports;
test("desktop updates use matching installer asset versions and ignore unrelated core releases", () => {
  const asset = (version, arch) => ({
    name: `EasyAgent-${version}-${arch}.dmg`,
    browser_download_url: `https://github.com/hwj123hwj/easyagent/releases/download/desktop-v${version}/EasyAgent-${version}-${arch}.dmg`,
  });
  assert.equal(
    desktopUpdate([{ tag_name: "v9.0.0", assets: [] }], "0.2.0", "arm64"),
    null,
  );
  const update = desktopUpdate(
    [
      { assets: [asset("0.3.0", "x64"), asset("0.4.0", "arm64")] },
      { prerelease: true, assets: [asset("0.5.0", "arm64")] },
    ],
    "0.2.0",
    "arm64",
  );
  assert.equal(update.version, "0.4.0");
  assert(update.downloadUrl.includes("arm64"));
  assert.equal(
    desktopUpdate(
      [
        {
          assets: [
            {
              ...asset("0.9.0", "arm64"),
              browser_download_url: "https://untrusted.example/download",
            },
          ],
        },
      ],
      "0.2.0",
      "arm64",
    ),
    null,
  );
});
