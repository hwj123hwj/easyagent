const { test } = require("node:test");
const assert = require("node:assert/strict");
global.localStorage = { getItem: () => null, setItem: () => {} };
global.sessionStorage = { getItem: () => null, setItem: () => {} };
global.window = {};
const { useStore, wsService } = require("../.test-output/src/store.js");
const { emptyProjection } = require("../.test-output/src/client/reducer.js");
test("update check shows pending state, prevents duplicate IPC and cleans Electron error prefixes", async () => {
  const before = useStore.getState().update, api = global.window.piAPI;
  global.__APP_VERSION__ = '0.3.0';
  let reject, calls = 0;
  global.window.piAPI = { checkForUpdate: () => { calls++; return new Promise((_resolve, fail) => { reject = fail; }); } };
  try {
    useStore.setState({ update: null });
    const pending = useStore.getState().checkUpdate();
    assert.equal(useStore.getState().update.phase, 'checking');
    await useStore.getState().checkUpdate();
    assert.equal(calls, 1);
    reject(Error("Error invoking remote method 'check-for-update': Error: 暂时无法连接更新服务"));
    await pending;
    assert.equal(useStore.getState().update.phase, 'error');
    assert.equal(useStore.getState().update.error, '暂时无法连接更新服务');
  } finally {
    global.window.piAPI = api;
    delete global.__APP_VERSION__;
    useStore.setState({ update: before });
  }
});
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
test("normal settings opens general after Feishu while explicit shortcuts preserve conversation, draft and layout", () => {
  const before = useStore.getState();
  const workspace = { ...before.workspace, rightOpen: true, rightView: "files", sidebarOpen: true };
  const drafts = { s: "尚未发送的任务" };
  const sessions = { s: view() };
  useStore.setState({ activeSessionId: "s", sessions, drafts, workspace });
  const assertConversationPreserved = () => {
    assert.equal(useStore.getState().activeSessionId, "s");
    assert.equal(useStore.getState().sessions, sessions);
    assert.equal(useStore.getState().drafts, drafts);
    assert.equal(useStore.getState().workspace, workspace);
  };
  try {
    useStore.getState().openSettings(true, "feishu");
    assert.equal(useStore.getState().settingsTab, "feishu");
    useStore.getState().openSettings(false);
    assert.equal(useStore.getState().settingsOpen, false);
    assert.equal(useStore.getState().settingsTab, "feishu");
    assertConversationPreserved();
    useStore.getState().openSettings();
    assert.equal(useStore.getState().settingsOpen, true);
    assert.equal(useStore.getState().settingsTab, "general");
    assertConversationPreserved();

    for (const tab of ["models", "mcp", "connections", "appearance", "feishu"]) {
      // Explicit quick entries and page category navigation still select their destination.
      useStore.getState().openSettings(true, tab);
      assert.equal(useStore.getState().settingsTab, tab);
      assert.equal(useStore.getState().settingsOpen, true);
      assertConversationPreserved();
      // Closing keeps the viewed category even if a caller supplied another tab.
      useStore.getState().openSettings(false, "general");
      assert.equal(useStore.getState().settingsTab, tab);
      assert.equal(useStore.getState().settingsOpen, false);
      assertConversationPreserved();
      useStore.getState().openSettings(true);
      assert.equal(useStore.getState().settingsTab, "general");
      assertConversationPreserved();
    }
  } finally {
    useStore.setState({ activeSessionId: before.activeSessionId, sessions: before.sessions, drafts: before.drafts, workspace: before.workspace, settingsOpen: before.settingsOpen, settingsTab: before.settingsTab });
  }
});

test("deselecting any right feature closes the entire workspace and persists it", () => {
  const workspace = useStore.getState().workspace;
  const storage = global.localStorage;
  let saved;
  global.localStorage = { getItem: () => null, setItem: (key, value) => {
    if (key === "pi-go.workspace") saved = JSON.parse(value);
  } };
  try {
    for (const feature of ["review", "files", "plan", "tasks", "kb", "profile"]) {
      useStore.getState().openWorkspaceView(feature);
      useStore.getState().toggleWorkspaceView(feature);
      assert.equal(useStore.getState().workspace.rightOpen, false);
      assert.equal(useStore.getState().workspace.rightView, null);
      assert.equal(saved.rightOpen, false);
      assert.equal(saved.rightView, null);
    }
    useStore.getState().toggleWorkspaceRight();
    assert.equal(useStore.getState().workspace.rightOpen, true);
    assert.equal(useStore.getState().workspace.rightView, "files");
    useStore.getState().toggleWorkspaceView("plan");
    assert.equal(useStore.getState().workspace.rightView, "plan");
    useStore.getState().toggleWorkspaceRight();
    assert.equal(useStore.getState().workspace.rightOpen, false);
    assert.equal(useStore.getState().workspace.rightView, "plan");
    // A remembered but hidden feature is not an active selection.
    useStore.getState().toggleWorkspaceView("plan");
    assert.equal(useStore.getState().workspace.rightOpen, true);
    assert.equal(useStore.getState().workspace.rightView, "plan");
  } finally {
    global.localStorage = storage;
    useStore.setState({ workspace });
  }
});

test("restoring an old empty launcher does not reopen a right sidebar", () => {
  const modulePath = require.resolve("../.test-output/src/store.js");
  const cached = require.cache[modulePath];
  const storage = global.localStorage;
  try {
    for (const [input, open, selected] of [
      [{ rightOpen: true, rightView: null }, false, null],
      [{ rightOpen: true }, false, null],
      [{ rightOpen: true, rightView: "removed-feature" }, false, null],
      [{ rightOpen: true, rightView: "files" }, true, "files"],
      [{ rightOpen: false, rightView: "plan" }, false, "plan"],
    ]) {
      global.localStorage = { getItem: (key) => key === "pi-go.workspace" ? JSON.stringify(input) : null, setItem() {} };
      delete require.cache[modulePath];
      const restored = require(modulePath).useStore.getState().workspace;
      assert.equal(restored.rightOpen, open);
      assert.equal(restored.rightView, selected);
    }
  } finally {
    global.localStorage = storage;
    require.cache[modulePath] = cached;
  }
});

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

test("command feedback follows a newly created session through switching and history refresh", async () => {
  const send = wsService.send;
  wsService.send = async () => true;
  window.piAPI = {
    request: async (method, path) => {
      if (method === "POST" && path === "/sessions") return { id: "created", created_at: 1790501735 };
      if (path === "/sessions") return [
        { id: "created", workspace: "/qa", created_at: 1790501735, last_active: 1790501735 },
        { id: "other", created_at: 1790501735, last_active: 1790501735 },
      ];
      if (path.endsWith("/run")) return { type: "snapshot", seq: 0, messages: [] };
      return { workspace: "/qa", model: "qa-model" };
    },
  };
  try {
    useStore.setState({
      selectedProfile: "mini", sessions: { other: { ...view(), meta: { ...view().meta, id: "other" } } },
      order: ["other"], activeSessionId: "other",
    });
    const id = await useStore.getState().createSession({ cwd: "/qa" });
    const result = "模型：openai / qa-model\n工作区：/qa";
    useStore.getState().setCommandOutput(id, result, "mini");
    assert.equal(useStore.getState().activeSessionId, id);
    assert.equal(useStore.getState().sessions[id].commandOutput, result);
    await useStore.getState().setActive("other");
    await useStore.getState().setActive(id);
    await useStore.getState().refreshSessions();
    assert.equal(useStore.getState().sessions[id].commandOutput, result);
    assert.deepEqual(useStore.getState().sessions[id].transcript, []);
    useStore.getState().setCommandOutput(id, "");
    assert.equal(useStore.getState().sessions[id].commandOutput, "");
  } finally {
    wsService.send = send;
    delete window.piAPI;
  }
});

test("late command feedback neither recreates deleted sessions nor overwrites another host", async () => {
  window.piAPI = { request: async () => ({}) };
  try {
    useStore.setState({ selectedProfile: "mini", sessions: { s: view() }, order: ["s"], activeSessionId: "s" });
    await useStore.getState().deleteSession("s");
    useStore.getState().setCommandOutput("s", "late deleted result", "mini");
    assert.equal(useStore.getState().sessions.s, undefined);
    assert.deepEqual(useStore.getState().order, []);
    useStore.setState({ selectedProfile: "other-host", sessions: { s: { ...view(), commandOutput: "current result" } } });
    useStore.getState().setCommandOutput("s", "late old host result", "mini");
    assert.equal(useStore.getState().sessions.s.commandOutput, "current result");
  } finally { delete window.piAPI; }
});

test("switching hosts during new-session creation discards the old host's completion", async () => {
  const connect = wsService.connect, disconnect = wsService.disconnect;
  wsService.connect = async () => {};
  wsService.disconnect = async () => {};
  let resolveCreate, issued;
  const createdRequest = new Promise((resolve) => { issued = resolve; });
  window.piAPI = {
    selectProfile: async () => ({ profiles: [] }),
    getServerUrl: async () => "http://new-host",
    request: async (method, path) => {
      if (method === "POST" && path === "/sessions") {
        issued();
        return new Promise((resolve) => { resolveCreate = resolve; });
      }
      if (path === "/models") return { models: [], source: "unconfigured" };
      if (path === "/sessions") return [];
      return { commands: [] };
    },
  };
  try {
    useStore.setState({ selectedProfile: "old-host", sessions: {}, order: [], activeSessionId: undefined });
    const creating = useStore.getState().createSession({ cwd: "/old-project" });
    await createdRequest;
    await useStore.getState().connectProfile("new-host");
    resolveCreate({ id: "old-created", created_at: 1790501735 });
    await assert.rejects(creating, /主机已切换/);
    assert.equal(useStore.getState().sessions["old-created"], undefined);
    assert.deepEqual(useStore.getState().order, []);
    assert.equal(useStore.getState().activeSessionId, undefined);
  } finally {
    wsService.connect = connect;
    wsService.disconnect = disconnect;
    delete window.piAPI;
  }
});

test("session activation and creation clear file tabs only when the project changes", async () => {
  const send = wsService.send;
  wsService.send = async () => true;
  const sessionAt = (id, cwd) => ({ ...view(), meta: { ...view().meta, id, cwd } });
  const sessions = { a: sessionAt("a", "/one"), same: sessionAt("same", "/one"), other: sessionAt("other", "/two") };
  let created = 0;
  window.piAPI = {
    request: async (method, path) => {
      if (method === "POST" && path === "/sessions") return { id: `created-${++created}`, created_at: 1790501735 };
      if (path.endsWith("/run")) return { type: "snapshot", seq: 0, messages: [] };
      return { workspace: sessions[path.split("/")[2]]?.meta.cwd, model: "qa-model" };
    },
  };
  try {
    useStore.setState({ sessions, order: Object.keys(sessions), activeSessionId: "a",
      workspace: { ...useStore.getState().workspace, fileTabs: ["/one/a.go"], activeFileTab: "/one/a.go" } });
    await useStore.getState().setActive("same");
    assert.deepEqual(useStore.getState().workspace.fileTabs, ["/one/a.go"]);
    assert.equal(useStore.getState().workspace.activeFileTab, "/one/a.go");
    await useStore.getState().createSession({ cwd: "/one" });
    assert.deepEqual(useStore.getState().workspace.fileTabs, ["/one/a.go"]);
    await useStore.getState().setActive("other");
    assert.deepEqual(useStore.getState().workspace.fileTabs, []);
    assert.equal(useStore.getState().workspace.activeFileTab, undefined);
    useStore.setState({ workspace: { ...useStore.getState().workspace, fileTabs: ["/two/b.go"], activeFileTab: "/two/b.go" } });
    await useStore.getState().createSession({ cwd: "/one" });
    assert.deepEqual(useStore.getState().workspace.fileTabs, []);
    assert.equal(useStore.getState().workspace.activeFileTab, undefined);
  } finally {
    wsService.send = send;
    delete window.piAPI;
  }
});

test('permission selection uses the server response and preserves drafts when switching is rejected', async () => {
 const before=useStore.getState(), api=window.piAPI;
 const calls=[];
 window.piAPI={request:async(method,path,body)=>{
  calls.push({method,path,body});
  if(path.endsWith('/permissions')) {
   if(body.mode==='ask') throw Error('task is running');
   return {access_mode:'full'};
  }
  return {access_mode:'full',context_usage:{estimated_tokens:125,context_window:128000}};
 }};
 try {
  useStore.setState({sessions:{s:{...view(),accessMode:'ask'}},drafts:{s:'keep draft'}});
  await useStore.getState().setAccessMode('s','full');
  assert.equal(useStore.getState().sessions.s.accessMode,'full');
  assert.deepEqual(calls[0],{method:'POST',path:'/sessions/s/permissions',body:{mode:'full'}});
  await assert.rejects(useStore.getState().setAccessMode('s','ask'),/task is running/);
  assert.equal(useStore.getState().sessions.s.accessMode,'full');
  assert.equal(useStore.getState().drafts.s,'keep draft');
  await useStore.getState().refreshSessionInfo('s');
  assert.equal(useStore.getState().sessions.s.contextUsage.estimated_tokens,125);
 } finally {window.piAPI=api;useStore.setState(before);}
});

test("stale confirmation refreshes authoritative approvals without replaying the decision", async () => {
  const before = useStore.getState(), api = window.piAPI;
  const pending = { confirmation_id: "new", tool_name: "edit" };
  const initial = { ...view(), run: { run_id: "r", state: "waiting_confirmation" }, phase: "approval", confirmations: [{ confirmation_id: "old", tool_name: "edit" }] };
  const calls = [];
  window.piAPI = { request: async (method, path) => {
    calls.push([method, path]);
    if (method === "POST") throw Error("Error invoking remote method 'agent-request': Error: HTTP 409: 已过期");
    return { type: "snapshot", reset: true, run: initial.run, pending_confirmations: [pending], messages: [] };
  }};
  try {
    useStore.setState({ sessions: { s: initial } });
    await assert.rejects(useStore.getState().confirm("s", "old", true), /HTTP 409/);
    assert.deepEqual(useStore.getState().sessions.s.confirmations, [pending]);
    assert.equal(useStore.getState().sessions.s.meta.cwd, "/qa");
    assert.equal(calls.filter(c => c[0] === "POST").length, 1);
  } finally { window.piAPI = api; useStore.setState(before); }
});

test('question replies send structured answers and failed submission preserves the question', async () => {
 const before = useStore.getState(), api = global.window.piAPI;
 const confirmation = {confirmation_id:'c',tool_name:'ask_user_question',tool_call_id:'q',args:{questions:[]}};
 const s = {...view(), run:{run_id:'r',state:'waiting_confirmation'},confirmations:[confirmation],phase:'approval'};
 const answers = [{selected:['React']},{selected:[],text:'自定义'}];
 let payload, fail = true;
 global.window.piAPI = { request:async (method, path, body) => {payload={method,path,body}; if(fail) throw Error('network offline');return {confirmed:true};} };
 useStore.setState({sessions:{s}});
 try {
  await assert.rejects(useStore.getState().confirm('s','c',true,answers),/offline/);
  assert.equal(useStore.getState().sessions.s.confirmations.length,1);
  fail=false;
  await useStore.getState().confirm('s','c',true,answers);
  assert.deepEqual(payload.body.answers,answers);
  assert.equal(payload.body.run_id,'r');
  assert.equal(payload.path,'/sessions/s/run/confirm');
  assert.equal(useStore.getState().sessions.s.confirmations.length,0);
 } finally {global.window.piAPI=api;useStore.setState(before);}
});
