const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');

// Exercise the real pane's input handlers and layout effects. Browser QA covers
// actual row measurement; this small host lets CI reproduce the scroll races.
function fixture() {
  const hooks = [], frames = [];
  let cursor = 0, effects = [];
  const same = (a, b) => a && b && a.length === b.length && a.every((v, i) => Object.is(v, b[i]));
  const cache = (factory, deps) => {
    const index = cursor++;
    if (!hooks[index] || !same(hooks[index].deps, deps)) hooks[index] = { value: factory(), deps };
    return hooks[index].value;
  };
  const react = {
    memo: (value) => value,
    useRef: (value) => cache(() => ({ current: value }), []),
    useState: (value) => {
      const index = cursor++;
      if (!hooks[index]) hooks[index] = { value };
      return [hooks[index].value, (next) => {
        hooks[index].value = typeof next === 'function' ? next(hooks[index].value) : next;
      }];
    },
    useMemo: cache,
    useCallback: (value, deps) => cache(() => value, deps),
    useLayoutEffect: (fn, deps) => {
      const index = cursor++;
      if (!hooks[index] || !same(hooks[index].deps, deps)) effects.push(fn);
      hooks[index] = { deps };
    },
    useEffect: () => { cursor++; },
  };
  const element = (type, props) => ({ type, props });
  const store = (select) => select({ selectedProfile: 'qa' });
  store.getState = () => ({});
  const module = { exports: {} };
  const source = fs.readFileSync(path.join(__dirname, '../src/components/panes/ChatPane.tsx'), 'utf8');
  const code = ts.transpileModule(source, { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX,
  } }).outputText;
  vm.runInNewContext(code, {
    module, exports: module.exports,
    requestAnimationFrame: (fn) => frames.push(fn), cancelAnimationFrame() {},
    require: (name) => {
      if (name === 'react') return react;
      if (name === 'react/jsx-runtime') return { jsx: element, jsxs: element };
      if (name === '../../store') return { useStore: store };
      if (name === '../../client/protocol') return { isActiveRun: () => false };
      if (name === '../../client/conversation-search') return { findConversation: () => [] };
      return {};
    },
  });
  let top = 1400;
  const el = {
    scrollHeight: 2000, clientHeight: 600,
    get scrollTop() { return top; },
    set scrollTop(value) { top = Math.max(0, Math.min(value, this.scrollHeight - this.clientHeight)); },
  };
  let view = {
    meta: { id: 'qa', cwd: '/qa' }, phase: 'idle', density: 'normal',
    transcript: Array.from({ length: 20 }, (_, i) => ({ kind: 'assistant', id: 'row-' + i, text: 'answer' })),
  };
  const find = (node, predicate) => {
    if (!node || typeof node !== 'object') return undefined;
    if (predicate(node)) return node;
    for (const child of [node.props?.children].flat(Infinity)) {
      const match = find(child, predicate);
      if (match) return match;
    }
  };
  let tree, renderedRows;
  const render = () => {
    cursor = 0; effects = [];
    tree = module.exports.ChatPane({ view });
    renderedRows = [];
    const collect = node => {
      if (!node || typeof node !== "object") return;
      if (node.props?.row) renderedRows.push(node.props.row.id);
      for (const child of [node.props?.children].flat(Infinity)) collect(child);
    };
    collect(tree);
    const pane = find(tree, (node) => node.props?.className === 'pane-body personal-transcript-scroll');
    pane.props.ref.current = el;
    for (const fn of effects) fn();
    return pane.props;
  };
  render();
  return {
    el, frames, render,
    switchTo: next => { view = next; },
    renderedRows: () => renderedRows,
    hasJump: () => !!find(tree, (node) => node.props?.className === 'jump-latest'),
    jump: () => find(tree, (node) => node.props?.className === 'jump-latest').props.onClick(),
    measure: (id, height) => find(tree, (node) => node.props?.row)?.props.measure(id, height),
    grow: () => { view = { ...view, transcript: [...view.transcript, { kind: 'assistant', id: 'new', text: 'stream' }] }; },
  };
}

test('first small upward scroll keeps the reading position when a reply grows', () => {
  const f = fixture(), pane = f.render();
  pane.onWheel({ deltaY: -24 });
  f.el.scrollTop -= 24;
  pane.onScroll();
  f.render();
  assert.equal(f.hasJump(), true);
  f.el.scrollHeight += 100;
  f.grow(); f.render();
  assert.equal(f.el.scrollTop, 1376);
});

test('scrolling down resumes following only at the bottom, not within 80 pixels', () => {
  const f = fixture();
  let pane = f.render(); pane.onWheel({ deltaY: -100 });
  f.el.scrollTop = 1300; pane.onScroll();
  pane = f.render(); f.el.scrollTop = 1360; pane.onScroll();
  f.render(); assert.equal(f.hasJump(), true);
  pane = f.render(); f.el.scrollTop = 1400; pane.onScroll();
  f.render(); assert.equal(f.hasJump(), false);
  f.el.scrollHeight += 100; f.grow(); f.render();
  assert.equal(f.el.scrollTop, 1500);
});

test('first measurement above the viewport is compensated after the new height commits', () => {
  const f = fixture(), pane = f.render(); pane.onWheel({ deltaY: -800 });
  f.el.scrollTop = 600; pane.onScroll(); f.render();
  f.measure('row-0', 350); // Replaces an unseen 100px estimate above the reader.
  assert.equal(f.el.scrollTop, 600, 'must not adjust against the old DOM scroll height');
  f.el.scrollHeight += 250; f.render();
  assert.equal(f.el.scrollTop, 850);
});

test('manual upward input cancels scheduled jump-to-latest frames', () => {
  const f = fixture(); let pane = f.render(); pane.onWheel({ deltaY: -100 });
  f.el.scrollTop = 1300; pane.onScroll(); f.render(); f.jump();
  pane = f.render(); pane.onWheel({ deltaY: -24 });
  f.el.scrollTop -= 24; const readingTop = f.el.scrollTop; pane.onScroll();
  while (f.frames.length) f.frames.shift()();
  assert.equal(f.el.scrollTop, readingTop);
});

function longView(id, count = 10000) {
  return { meta: { id, cwd: '/qa' }, phase: 'idle', density: 'normal',
    transcript: Array.from({ length: count }, (_, i) => ({ kind: 'assistant', id: 'row-' + i, text: 'answer' })) };
}

test('a cold long session mounts its tail immediately instead of rendering the previous session viewport', () => {
  const f = fixture();
  f.switchTo(longView('long'));
  f.el.scrollHeight = 1000000;
  f.render();
  assert.ok(f.renderedRows().length < 25);
  assert.ok(f.renderedRows().every(id => Number(id.slice(4)) > 9900), 'first render must start at the new session tail');
});

test('restored reading position selects the correct rows on the first render after switching', () => {
  const f = fixture();
  f.switchTo(longView('reading', 100)); f.el.scrollHeight = 10000; f.render();
  let pane = f.render(); pane.onWheel({ deltaY: -8000 });
  f.el.scrollTop = 1000; pane.onScroll(); f.render();
  f.switchTo(longView('other', 1000)); f.el.scrollHeight = 100000; f.render(); f.render();
  f.switchTo(longView('reading', 100)); f.el.scrollHeight = 10000; f.render();
  assert.ok(f.renderedRows().includes('row-10'), 'saved reading rows mount without rendering the previous tail');
  assert.equal(f.el.scrollTop, 1000);
});

test('history row IDs reused by another session do not reuse its measured heights', () => {
  const f = fixture();
  f.measure('row-0', 50000); f.render();
  f.switchTo(longView('different', 100)); f.el.scrollHeight = 10000; f.render();
  assert.ok(f.renderedRows().includes('row-99'), 'another session must use its own estimates');
});


test('saved reading intent survives an empty loading view until history arrives', () => {
  const f = fixture();
  f.switchTo(longView('restoring', 100)); f.el.scrollHeight = 10000; f.render();
  let pane = f.render(); pane.onWheel({ deltaY: -8000 });
  f.el.scrollTop = 1000; pane.onScroll(); f.render();
  f.switchTo(longView('other', 1000)); f.el.scrollHeight = 100000; f.render();
  f.switchTo(longView('restoring', 0)); f.el.scrollHeight = 600; f.render(); f.render();
  f.switchTo(longView('restoring', 100)); f.el.scrollHeight = 10000; f.render();
  assert.ok(f.renderedRows().includes('row-10'));
  assert.equal(f.el.scrollTop, 1000, 'empty loading DOM must not erase the saved reading position');
});
