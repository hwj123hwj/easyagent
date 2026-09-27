import { test } from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown } from '../internal/web/static/js/markdown.js';
import { filterModels, modelKey } from '../internal/web/static/js/model-picker.js';

test('model search matches names, IDs and providers without changing IDs', () => {
  const models = [{id:'vendor/model-v3',provider:'openai',name:'代码模型'}, {id:'coding',provider:'anthropic',name:'Coding'}];
  assert.deepEqual(filterModels(models, 'OPENAI v3'), [models[0]]);
  assert.deepEqual(filterModels(models, '代码'), [models[0]]);
  assert.deepEqual(filterModels(models, 'unknown'), []);
  assert.deepEqual(filterModels(models, '  '), models);
  assert.equal(modelKey(models[0]), 'openai/vendor/model-v3');
});

test('markdown safely renders links, inline code and fenced language', () => {
  const html = renderMarkdown('[危险](javascript:alert(1)) [文档](https://example.com/"onclick="bad) `**literal**`');
  assert.ok(!html.includes('href="javascript:'));
  assert.ok(html.includes('&quot;'));
  assert.ok(html.includes('<code>**literal**</code>'));
  assert.equal((renderMarkdown('[文档](https://example.com)').match(/<a /g) || []).length, 1);
  assert.ok(!renderMarkdown('```js"onmouseover="x\nalert(1)\n```').includes('onmouseover="'));
});
test('markdown renders final headings and preserves code exactly', () => {
  assert.equal(renderMarkdown('## 最后一行'), '<h2>最后一行</h2>');
  const html = renderMarkdown('```go\nfunc main() { fmt.Println("hi") }\n```');
  assert.ok(html.includes('func main() { fmt.Println("hi") }'));
  assert.ok(!html.match(/<code[^>]*>([\s\S]*?)<\/code>/)[1].includes('<span'));
});
test('markdown images are not transformed into links or executable attributes', () => {
  const html = renderMarkdown('![图" onload="bad](https://example.com/a.png)');
  assert.ok(html.includes('<img ')); assert.ok(!html.includes('<a '));
  assert.ok(!html.includes(' onload="bad'));
  assert.ok(!renderMarkdown('![x](data:text/html,bad)').includes('<img'));
});

test('clipboard works on HTTP and falls back when modern access is denied', async () => {
  const { readFile } = await import('node:fs/promises');
  const { runInNewContext } = await import('node:vm');
  const source = (await readFile(new URL('../internal/web/static/js/clipboard.js', import.meta.url), 'utf8')).replace('export async function', 'async function') + '\ncopyText;';
  for (const secure of [false, true]) {
    let copied = '', removed = false, restored = false;
    const field = { value:'', setAttribute(){}, focus(){}, select(){}, setSelectionRange(){}, remove(){removed=true;} };
    const copy = runInNewContext(source, {
      isSecureContext:secure,
      navigator:secure ? {clipboard:{writeText:async()=>{throw new Error('denied');}}} : {},
      window:{getSelection:()=>null},
      document:{activeElement:{focus(){restored=true;}},body:{append(){}},createElement:()=>field,execCommand:command=>{assert.equal(command,'copy');copied=field.value;return true;}},
    });
    await copy('/pair sample');
    assert.equal(copied,'/pair sample');assert.ok(removed);assert.ok(restored);
  }
  let removed = false;
  const copy = runInNewContext(source, {isSecureContext:false,navigator:{},window:{getSelection:()=>null},document:{body:{append(){}},createElement:()=>({setAttribute(){},focus(){},select(){},setSelectionRange(){},remove(){removed=true;}}),execCommand:()=>false}});
  await assert.rejects(copy('sample'), /clipboard unavailable/);
  assert.ok(removed);
});

import { commands, parseCommand, filterCommands } from '../internal/web/static/js/commands.js';
import { Drafts } from '../internal/web/static/js/drafts.js';
test('slash completion searches names and Chinese labels, not arguments or paths', () => {
  assert.equal(filterCommands('/').length, commands.length);
  assert.equal(filterCommands('/mod')[0].name, 'model');
  assert.equal(filterCommands('/压缩')[0].name, 'compact');
  assert.deepEqual(filterCommands('/unknown'), []);
  for (const text of ['hello /', '/compact 保留目标', '//literal', '/tmp/file']) assert.equal(filterCommands(text), null);
  assert.deepEqual(parseCommand('/COMPACT 保留目标\n以及约束'), {name:'compact',args:'保留目标\n以及约束'});
  assert.equal(parseCommand('//help'), null);
});
test('per-tab drafts survive recreation, stay separate, and clear after send', () => {
  const memory = new Map(), storage = {getItem:k=>memory.get(k),setItem:(k,v)=>memory.set(k,v),removeItem:k=>memory.delete(k)};
  const drafts = new Drafts(storage); drafts.set('a','甲'); drafts.set('b','乙');
  const refreshed = new Drafts(storage); assert.equal(refreshed.get('a'),'甲'); assert.equal(refreshed.get('b'),'乙');
  refreshed.delete('a'); assert.equal(new Drafts(storage).get('a'),''); assert.equal(refreshed.get('b'),'乙');
  const denied = new Drafts({getItem(){throw Error();},setItem(){throw Error();},removeItem(){throw Error();}});
  assert.equal(denied.get('a'),''); denied.set('a','保留'); assert.equal(denied.get('a'),'保留');
});
test('emphasis can contain inline code and links without leaking markup or HTML', () => {
  assert.equal(renderMarkdown('**已通过 `go test` 和 [文档](https://example.com)**'), '<p><strong>已通过 <code>go test</code> 和 <a href="https://example.com" target="_blank" rel="noopener noreferrer">文档</a></strong></p>');
  assert.ok(renderMarkdown('**<img onerror=x>**').includes('&lt;img onerror=x&gt;'));
  assert.ok(renderMarkdown('```sh\necho "hi"\n```').includes('class="copy-code"'));
});

import { ChatPanel } from '../internal/web/static/js/chat.js';
import { api } from '../internal/web/static/js/api.js';
test('commands execute separately from prompts, preserving failed or edited drafts', async () => {
  const oldDocument = globalThis.document, oldGet = api.get;
  const nodes = new Map();
  globalThis.document = {getElementById:id=>{if (!nodes.has(id)) nodes.set(id,{hidden:true,textContent:''});return nodes.get(id);}};
  const panel = Object.create(ChatPanel.prototype);
  Object.assign(panel, {
    input:{value:'/tools '}, inputRevision:1, state:{currentSessionId:'a'}, busy:new Set(),
    commandMenu:{close(){}}, _updateButtons(){}, _notice(text){this.lastNotice=text;},
    _inputChanged(){this.inputRevision++;}, ws:{sendPrompt(){throw Error('command sent as prompt');}},
  });
  try {
    let release;
    api.get = async () => new Promise(resolve=>{release=resolve;});
    const pending = panel._send();
    assert.ok(panel.commandRunning);
    panel.input.value='下一条草稿';panel.inputRevision++;
    release({tools:['read','write']});await pending;
    assert.equal(panel.input.value,'下一条草稿');
    assert.equal(nodes.get('command-result-body').textContent,'read\nwrite');
    panel.input.value='/context ';
    api.get=async()=>{throw Error('服务断开');};
    await panel._send();assert.equal(panel.input.value,'/context ');assert.match(panel.lastNotice,/服务断开/);
    panel.input.value='/unknown';await panel._send();assert.match(panel.lastNotice,/未知命令/);
    panel.input.value='/compact ';panel.busy.add('other');await panel._send();assert.match(panel.lastNotice,/等待任务结束/);
    panel.busy.clear();panel.input.value='/tools ';
    api.get=async()=>new Promise(resolve=>{release=resolve;});
    const switched=panel._send();panel.state.currentSessionId='b';panel.input.value='B 草稿';release({tools:['ls']});await switched;
    assert.equal(panel.input.value,'B 草稿');assert.equal(nodes.get('command-result-body').textContent,'read\nwrite');
  } finally {globalThis.document=oldDocument;api.get=oldGet;}
});

test('a failed first send retains slash escaping in the new session draft', async () => {
  const panel = Object.create(ChatPanel.prototype), drafts = new Drafts();
  Object.assign(panel, {input:{value:'//help'},inputRevision:0,drafts,busy:new Set(),ws:{connected:true,sendPrompt:(_id,text)=>{assert.equal(text,'/help');return false;}},_updateButtons(){},_notice(text){this.lastNotice=text;}});
  panel.state={currentSessionId:null,createSession:async()=>'new',selectSession:async id=>{panel.state.currentSessionId=id;panel.input.value=drafts.get(id);}};
  await panel._send();
  assert.equal(panel.input.value,'//help');assert.equal(drafts.get('new'),'//help');assert.match(panel.lastNotice,/发送失败/);
});
test('touch users can send commands while streaming and still stop generation', () => {
  const oldDocument=globalThis.document;
  globalThis.document={getElementById:()=>({})};
  const panel=Object.create(ChatPanel.prototype);
  Object.assign(panel,{state:{streaming:true},input:{value:'/help '},sendBtn:{style:{}},stopBtn:{style:{}},ws:{connected:true},busy:new Set(['a']),status:{}});
  try {
    panel._updateButtons();assert.equal(panel.sendBtn.style.display,'flex');assert.equal(panel.sendBtn.disabled,false);assert.equal(panel.stopBtn.style.display,'flex');
    panel.input.value='下一条消息';panel._updateButtons();assert.equal(panel.sendBtn.style.display,'none');
  } finally {globalThis.document=oldDocument;}
});
test('a delayed /new does not navigate away after the user switches or edits', async () => {
  for (const change of ['session','draft']) {
    let resolve, selected=false;
    const panel=Object.create(ChatPanel.prototype);
    Object.assign(panel,{input:{value:'/new '},inputRevision:1,busy:new Set(),commandMenu:{close(){}},_updateButtons(){},_notice(){},_inputChanged(){this.inputRevision++;},state:{currentSessionId:'a',createSession:()=>new Promise(r=>{resolve=r;}),selectSession:()=>{selected=true;}}});
    const pending=panel._command('/new');
    if (change==='session') panel.state.currentSessionId='b'; else panel.inputRevision++;
    panel.input.value='保留新草稿';resolve('new');await pending;
    assert.equal(selected,false);assert.equal(panel.input.value,'保留新草稿');
  }
});

test('workflow task sends through the agent and empty workflow opens the run directory', async () => {
  assert.equal(filterCommands('/work')[0].name,'workflow');
  const panel=Object.create(ChatPanel.prototype);let sent='',page='';
  Object.assign(panel,{state:{currentSessionId:'a',navigate:p=>{page=p;}},input:{value:'/workflow 研究方案',style:{},scrollHeight:20},drafts:new Drafts(),busy:new Set(),ws:{connected:true,sendPrompt:(_id,text)=>{sent=text;return false;}},_updateButtons(){},_notice(){},commandMenu:{close(){}},_inputChanged(){}});
  await panel._send();assert.equal(sent,'/workflow 研究方案');assert.equal(panel.input.value,'/workflow 研究方案');
  panel.input.value='/workflow';await panel._send();assert.equal(page,'page-dynamic-workflows');assert.equal(panel.input.value,'');
});
