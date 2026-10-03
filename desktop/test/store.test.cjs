const { test } = require("node:test");
const assert = require("node:assert/strict");
global.localStorage = { getItem: () => null, setItem: () => {} };
global.sessionStorage = { getItem: () => null, setItem: () => {} };
global.window = {};
const { useStore, wsService } = require("../.test-output/src/store.js");
const { emptyProjection } = require("../.test-output/src/client/reducer.js");
function view() {
  return {
    ...emptyProjection(),
    meta: { id: "s", title: "Session s", cwd: "/qa", status: "idle" },
    plan: [],
    diffs: [],
    density: "normal",
    panes: ["chat"],
    activePane: "chat",
  };
}
test("retry after an uncertain acknowledgement keeps request identity and draft", async () => {
  const requests = [];
  let fail = true;
  const original = wsService.prompt;
  wsService.prompt = async (id, text, request) => {
    requests.push(request);
    if (fail) throw new Error("ACK timeout");
    return { type: "accepted", request_id: request };
  };
  useStore.setState({
    selectedProfile: "mini",
    connected: true,
    sessions: { s: view() },
    drafts: { s: "保留任务" },
    pending: {},
  });
  try {
    await assert.rejects(
      useStore.getState().sendPrompt("s", "保留任务"),
      /ACK timeout/,
    );
    assert.equal(useStore.getState().drafts.s, "保留任务");
    assert.equal(useStore.getState().pending.s, false);
    fail = false;
    await useStore.getState().sendPrompt("s", "保留任务");
    assert.equal(requests[0], requests[1]);
    await useStore.getState().sendPrompt("s", "保留任务");
    assert.notEqual(requests[1], requests[2]);
  } finally {
    wsService.prompt = original;
  }
});
test("a disconnected send fails without clearing its draft or pretending to run", async () => {
  useStore.setState({
    connected: false,
    sessions: { s: view() },
    drafts: { s: "草稿" },
  });
  await assert.rejects(useStore.getState().sendPrompt("s", "草稿"), /尚未连接/);
  assert.equal(useStore.getState().drafts.s, "草稿");
  assert.equal(useStore.getState().sessions.s.run, undefined);
});
test("remote projects use an in-app picker and never resolve into another host", async () => {
  useStore.setState({
    selectedProfile: "mini",
    profiles: [
      { id: "mini", kind: "remote", name: "Mini", url: "http://mini" },
    ],
  });
  const path = useStore.getState().pickFolder();
  assert.equal(useStore.getState().pathPicker.profileId, "mini");
  useStore.getState().resolvePathPicker("/home/q/project");
  assert.equal(await path, "/home/q/project");
  const stale = useStore.getState().pickFolder();
  useStore.setState({ selectedProfile: "local" });
  useStore.getState().resolvePathPicker("/home/q/project");
  assert.equal(await stale, null);
  assert.equal(useStore.getState().pathPicker, undefined);
});

test("empty model discovery clears stale models and does not invent choices", async () => {
  window.piAPI = { request: async () => ({ models: [], source: "unconfigured" }) };
  try {
    useStore.setState({ models: [{ modelId: "stale", name: "Stale" }], currentModel: "stale" });
    await useStore.getState().refreshModels();
    assert.deepEqual(useStore.getState().models, []);
    assert.equal(useStore.getState().currentModel, undefined);
    assert.equal(useStore.getState().modelSource, "unconfigured");
  } finally { delete window.piAPI; }
});

test("model discovery failure does not block session restoration", async () => {
  const connect = wsService.connect, disconnect = wsService.disconnect;
  wsService.connect = async () => {};
  wsService.disconnect = async () => {};
  window.piAPI = {
    selectProfile: async () => ({ profiles: [] }),
    getServerUrl: async () => "http://local",
    request: async (_method, path) => {
      if (path === "/models") throw new Error("HTTP 503");
      if (path === "/sessions") return [];
      return { commands: [] };
    },
  };
  try {
    await useStore.getState().connectProfile("local");
    assert.equal(useStore.getState().connectionError, undefined);
    assert.match(useStore.getState().modelsNotice, /模型列表暂不可用/);
    assert.deepEqual(useStore.getState().models, []);
    assert.deepEqual(useStore.getState().order, []);
  } finally {
    wsService.connect = connect;
    wsService.disconnect = disconnect;
    delete window.piAPI;
  }
});

test("a late model catalog from another host cannot overwrite the active host", async () => {
  const connect = wsService.connect, disconnect = wsService.disconnect;
  wsService.connect = async () => {};
  wsService.disconnect = async () => {};
  let resolveOld;
  let issuedOld;
  const oldIssued = new Promise((resolve) => { issuedOld = resolve; });
  window.piAPI = {
    selectProfile: async () => ({ profiles: [] }),
    getServerUrl: async () => "http://local",
    request: async (_method, path) => {
      if (path === "/models") {
        if (useStore.getState().selectedProfile === "old") {
          issuedOld();
          return new Promise((resolve) => { resolveOld = resolve; });
        }
        return { models: [{ id: "new-model", name: "New" }], current: { id: "new-model" }, source: "gateway" };
      }
      if (path === "/sessions") return [];
      return { commands: [] };
    },
  };
  try {
    const old = useStore.getState().connectProfile("old");
    await oldIssued;
    await useStore.getState().connectProfile("new");
    resolveOld({ models: [{ id: "old-model", name: "Old" }], current: { id: "old-model" } });
    await old;
    assert.equal(useStore.getState().selectedProfile, "new");
    assert.deepEqual(useStore.getState().models, [{ modelId: "new-model", name: "New" }]);
    assert.equal(useStore.getState().currentModel, "new-model");
  } finally {
    wsService.connect = connect;
    wsService.disconnect = disconnect;
    delete window.piAPI;
  }
});
