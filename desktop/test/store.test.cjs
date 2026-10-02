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
