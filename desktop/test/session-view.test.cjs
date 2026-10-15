const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const Module = require("node:module");
const ts = require("typescript");
const { JSDOM } = require("jsdom");
const React = require("react");

test("switching sessions and hosts leaves one composer, preserves drafts and resets local state", async () => {
  const dom = new JSDOM('<!doctype html><div id="root"></div>');
  const previous = {};
  for (const [name, value] of Object.entries({ window: dom.window, document: dom.window.document, navigator: dom.window.navigator, IS_REACT_ACT_ENVIRONMENT: true })) {
    previous[name] = Object.getOwnPropertyDescriptor(global, name);
    Object.defineProperty(global, name, { configurable: true, writable: true, value });
  }
  const { createRoot } = require("react-dom/client");
  const { act } = React;
  const drafts = { a: "会话 A 的未发送草稿", b: "会话 B 的未发送草稿" };
  const sessions = Object.fromEntries(["a", "b"].map(id => [id, {
    meta: { id, title: id, cwd: "/qa", status: "idle" }, transcript: [], phase: "idle", density: "normal",
  }]));
  const state = { activeSessionId: "a", selectedProfile: "local", sessions, drafts,
    workspace: { bottomOpen: false, bottomHeight: 240 }, setWorkspaceSize() {}, setDensity() {} };
  const useStore = selector => selector(state);
  useStore.getState = () => state;
  const mounted = new Set();
  // Exercise the real SessionView reconciliation; host-specific children are fixtures.
  function PromptBar({ view }) {
    const [busy, setBusy] = React.useState(false);
    React.useEffect(() => {
      const token = {};
      mounted.add(token);
      return () => mounted.delete(token);
    }, []);
    return React.createElement("div", { className: "prompt-bar", "data-busy": busy },
      React.createElement("textarea", { "aria-label": "消息输入", "data-session": view.meta.id, value: drafts[view.meta.id] || "", readOnly: true }),
      React.createElement("button", { onClick: () => setBusy(true) }, "开始本地命令"));
  }
  const stubs = {
    "../store": { useStore },
    "./panes/ChatPane": { ChatPane: ({ view }) => React.createElement("div", { "data-chat-session": view.meta.id }) },
    "./PromptBar": { PromptBar },
    "./Icon": { Icon: () => null },
    "../i18n/useT": { useT: () => key => key },
    "./workspace/BottomTerminal": { BottomTerminal: ({ view, open }) => React.createElement("section", { "data-terminal-session": view.meta.id, hidden: !open }) },
    "./workspace/Resizer": { Resizer: () => null },
    "../client/clipboard": { copyText: async () => {} },
    "../client/protocol": { isActiveRun: () => false },
  };
  const filename = path.resolve(__dirname, "../src/components/SessionView.tsx");
  const source = ts.transpileModule(fs.readFileSync(filename, "utf8"), { compilerOptions: {
    module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022,
  } }).outputText;
  const component = new Module(filename, module);
  component.paths = module.paths;
  component.require = request => Object.hasOwn(stubs, request) ? stubs[request] : module.require(request);
  component._compile(source, filename);
  const { SessionView } = component.exports;
  const host = document.getElementById("root");
  const root = createRoot(host);
  const errors = [];
  const originalError = console.error;
  console.error = (...args) => errors.push(args.join(" "));
  try {
    for (const [id, profile, bottomOpen] of [
      ["a", "local", false], ["b", "local", false], ["a", "local", true],
      ["b", "local", true], ["a", "remote", false], ["b", "remote", false],
      [null, "remote", false], ["a", "local", false],
    ]) {
      state.activeSessionId = id;
      state.selectedProfile = profile;
      state.workspace.bottomOpen = bottomOpen;
      await act(async () => root.render(React.createElement(SessionView)));
      const inputs = host.querySelectorAll('textarea[aria-label="消息输入"]');
      assert.equal(inputs.length, 1, `one composer after switching to ${profile}:${id}`);
      assert.equal(inputs[0].dataset.session, id || "__new__");
      assert.equal(inputs[0].value, drafts[id] || "");
      assert.equal(mounted.size, 1, "previous composer was unmounted");
      assert.equal(host.querySelector(".prompt-bar").dataset.busy, "false", "local command state is reset");
      if (id) {
        assert.equal(host.querySelector("[data-chat-session]").dataset.chatSession, id);
        assert.equal(host.querySelector("[data-terminal-session]").dataset.terminalSession, id);
      }
      await act(async () => host.querySelector(".prompt-bar button").click());
      assert.equal(host.querySelector(".prompt-bar").dataset.busy, "true");
    }
    assert.deepEqual(errors, [], "React emits no duplicate-key or reconciliation warnings");
  } finally {
    await act(async () => root.unmount());
    console.error = originalError;
    dom.window.close();
    for (const name of Object.keys(previous)) {
      if (previous[name]) Object.defineProperty(global, name, previous[name]);
      else delete global[name];
    }
  }
  assert.equal(mounted.size, 0, "all composers unmount when the view closes");
});
