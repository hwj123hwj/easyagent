const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const ts = require('typescript');
const React = require('react');
const { JSDOM } = require('jsdom');

test('parent renders do not reload historical diffs; review refresh still reloads them', async () => {
  const dom = new JSDOM('<!doctype html><div id="root"></div>');
  const previous = {};
  for (const [name, value] of Object.entries({ window: dom.window, document: dom.window.document, navigator: dom.window.navigator, IS_REACT_ACT_ENVIRONMENT: true })) {
    previous[name] = Object.getOwnPropertyDescriptor(global, name);
    Object.defineProperty(global, name, { configurable: true, writable: true, value });
  }
  const state = { selectedProfile: 'qa', reviewRevision: 0 };
  const useStore = select => select(state);
  useStore.getState = () => state;
  const calls = [];
  const runs = Array.from({ length: 20 }, (_, i) => ({ run_id: 'run-' + i, complete: true, files: [{ path: 'file-' + i + '.ts', size: 12 }] }));
  const apiRequest = async (_method, url) => {
    calls.push(url);
    return url.endsWith('/run-files') ? runs : { files: [] };
  };
  const stubs = { '../store': { useStore, apiRequest }, './Icon': { Icon: () => null },
    '../client/file-review': { summarizeDiffs: () => ({ diffs: new Map() }) } };
  const filename = path.resolve(__dirname, '../src/components/RunResults.tsx');
  const component = new Module(filename, module);
  component.paths = module.paths;
  component.require = name => Object.hasOwn(stubs, name) ? stubs[name] : module.require(name);
  component._compile(ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX,
  } }).outputText, filename);
  const { createRoot } = require('react-dom/client');
  const root = createRoot(document.getElementById('root'));
  const view = { meta: { id: 's' }, run: { state: 'completed' } };
  const render = async () => React.act(async () => root.render(React.createElement(component.exports.RunResults, { view: { ...view } })));
  const fileCalls = () => calls.filter(url => url.endsWith('/files')).length;
  try {
    await render();
    assert.equal(fileCalls(), 20);
    for (let i = 0; i < 5; i++) await render();
    assert.equal(fileCalls(), 20, 'viewport/transcript updates must not refetch every historical diff');
    state.reviewRevision++;
    await render();
    assert.equal(fileCalls(), 40, 'an explicit review refresh reloads each diff once');
    const count = calls.length;
    view.meta = { id: '__new__' };
    await render();
    assert.equal(calls.length, count, 'a new conversation has no server session to load');
    assert.equal(document.querySelector('.run-results'), null);
  } finally {
    await React.act(async () => root.unmount());
    dom.window.close();
    for (const name of Object.keys(previous)) {
      if (previous[name]) Object.defineProperty(global, name, previous[name]);
      else delete global[name];
    }
  }
});
