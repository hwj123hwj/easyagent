import { test } from 'node:test';
import assert from 'node:assert/strict';
import { MCPSettings, serverPatch, serverMarkup, canUseWebOAuth, oauthCallback } from '../internal/web/static/js/mcp-settings.js';

const values = overrides => ({name:'files',scope:'user',transport:'stdio',description:'',command:'',args:'',url:'',env:'',cwd:'',headers:'',oauth:'',include:'',exclude:'',timeout:'',enabled:true,trusted:false,...overrides});

test('editing status alone preserves undisplayed connection and credential fields', () => {
  const patch = serverPatch(values({transport:'http',description:'New label'}),{transport:'http'});
  assert.deepEqual(patch,{type:'http',description:'New label',enabled:true,trust:false});
  for (const key of ['headers','env','oauth','url','httpUrl','args','timeout']) assert.equal(Object.hasOwn(patch,key),false,key);
  assert.equal(Object.hasOwn(serverPatch(values(),{transport:'stdio'}),'command'),false);
});

test('explicit empty collections clear secrets and filters without changing unrelated fields', () => {
  const patch = serverPatch(values({transport:'http',headers:'{}',oauth:'null',include:'[]',exclude:'[]',timeout:'0'}),{transport:'http'});
  assert.deepEqual(patch.headers,{}); assert.equal(patch.oauth,null);
  assert.deepEqual(patch.includeTools,[]); assert.deepEqual(patch.excludeTools,[]); assert.equal(patch.timeout,0);
  assert.equal(Object.hasOwn(patch,'url'),false);
});

test('new stdio configuration preserves argument boundaries and restricts env to string pairs', () => {
  const patch = serverPatch(values({command:'npx',args:'["-y","server","/path with spaces"]',env:'{"TOKEN":"${TOKEN}"}'}));
  assert.deepEqual(patch.args,['-y','server','/path with spaces']); assert.equal(patch.env.TOKEN,'${TOKEN}');
  assert.throws(() => serverPatch(values({command:'npx',args:'[1]'})),/字符串数组/);
  assert.throws(() => serverPatch(values({command:'npx',env:'{"X":true}'})),/字符串键值对象/);
  assert.throws(() => serverPatch(values({command:'npx',args:'malformed'})),/有效的 JSON/);
});

test('changing transport requires replacement connection and clears incompatible fields', () => {
  assert.throws(() => serverPatch(values({transport:'http'}),{transport:'stdio'}),/服务地址/);
  assert.throws(() => serverPatch(values(),{transport:'http'}),/可执行命令/);
  const http = serverPatch(values({transport:'http',url:'https://example.com/mcp'}),{transport:'stdio'});
  assert.equal(http.command,null); assert.equal(http.args,null); assert.equal(http.env,null); assert.equal(http.cwd,null);
  const stdio = serverPatch(values({command:'server'}),{transport:'http'});
  assert.equal(stdio.url,null); assert.equal(stdio.httpUrl,null); assert.equal(stdio.headers,null); assert.equal(stdio.oauth,null);
  assert.equal(serverPatch(values({transport:'sse'}),{transport:'http'}).type,'sse');
});

test('invalid URLs, names, scope and tool timeout cannot be saved', () => {
  for (const url of ['javascript:alert(1)','https://token@example.com/mcp','https://example.com/mcp#fragment']) {
    assert.throws(() => serverPatch(values({transport:'http',url})),/HTTP\(S\)/);
  }
  assert.throws(() => serverPatch(values({name:'bad/name',command:'server'})),/名称/);
  assert.throws(() => serverPatch(values({scope:'global',command:'server'})),/保存位置/);
  assert.throws(() => serverPatch(values({command:'server',timeout:'1.5'})),/非负整数/);
});

test('untrusted tools and service configuration cannot inject markup', () => {
  const html = serverMarkup({name:'files',scope:'project',source:'<img onerror="bad">',transport:'http',state:'connected',enabled:true,trusted:false,description:'<script>bad()</script>',tools:[{name:'x" autofocus onfocus="bad',description:'<img src=x onerror=bad>',enabled:false}]});
  assert.ok(!html.includes('<script>')); assert.ok(!html.includes('<img '));
  assert.ok(!html.includes(' autofocus onfocus="')); assert.ok(html.includes('逐次确认'));
  assert.ok(html.includes('项目配置')); assert.ok(html.includes('0 / 1 工具启用'));
  assert.ok(serverMarkup({name:'empty',tools:null},true).includes(' disabled'));
});

test('unchanged status polls preserve existing DOM selection and disclosure state', () => {
  const previous = globalThis.document;
  let writes = 0, markup = '';
  const list = {contains:() => false,querySelectorAll:() => [{dataset:{server:'files'}}],set innerHTML(value){writes++;markup=value;}};
  const panel = Object.create(MCPSettings.prototype);
  Object.assign(panel,{list,pending:false,data:{servers:[{name:'files',transport:'stdio',state:'connected',tools:[]}]}});
  globalThis.document = {activeElement:null};
  try {
    panel.renderServers(); panel.renderServers(); panel.renderServers();
    assert.equal(writes,1,'status polling must not replace an unchanged interactive list');
    assert.ok(markup.includes(' open'),'previously opened service is retained');
    panel.data.servers[0].description = 'Changed'; panel.renderServers();
    assert.equal(writes,2); assert.ok(markup.includes('Changed')); assert.ok(markup.includes(' open'));
  } finally {globalThis.document = previous;}
});

test('web OAuth is available only over HTTPS or loopback, never LAN plaintext', () => {
  for (const url of ['https://agent.example.com','http://localhost:8080','http://127.0.0.1:8080','http://[::1]:8080']) assert.equal(canUseWebOAuth(url),true,url);
  for (const url of ['http://192.168.5.16:8080','http://127.0.0.1.example.com','https://secret@example.com','file:///settings']) assert.equal(canUseWebOAuth(url),false,url);
});

test('OAuth callback validates same-tab state and finite expiry before forwarding the code', () => {
  const now = Date.now(), pending = {state:'random-state',expires_at:new Date(now+60_000).toISOString()};
  assert.deepEqual(oauthCallback('https://agent.example.com/?code=abc&state=random-state',pending,now),{code:'abc',state:'random-state'});
  assert.equal(oauthCallback('https://agent.example.com/?page=page-settings',pending,now),null);
  assert.throws(() => oauthCallback('https://agent.example.com/?code=abc&state=wrong',pending,now),/不匹配/);
  assert.throws(() => oauthCallback('https://agent.example.com/?code=abc&state=random-state',{...pending,expires_at:'invalid'},now),/过期/);
  assert.throws(() => oauthCallback('https://agent.example.com/?code=abc&state=random-state',pending,now+90_000),/过期/);
  assert.throws(() => oauthCallback('https://agent.example.com/?error=access_denied&state=random-state',pending,now),/未完成/);
});
