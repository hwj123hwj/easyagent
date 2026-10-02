import { api } from './api.js';

const OAUTH_KEY = 'easyagent:mcp:oauth';
const labels = {connected:'已连接',connecting:'连接中',disconnected:'已断开',failed:'连接失败',needs_auth:'需要登录',disabled:'已停用'};
const transportLabels = {stdio:'stdio',http:'Streamable HTTP',sse:'SSE'};
const escape = value => String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
const fieldIds = ['name','scope','transport','description','command','args','url','env','cwd','headers','oauth','include','exclude','timeout','enabled','trusted'];

export function canUseWebOAuth(href) {
  const url = new URL(href);
  if (url.username || url.password) return false;
  return url.protocol === 'https:' || (url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname));
}

// Omitted fields are deliberately omitted from PUT: the API preserves stored
// credentials and command arguments instead of sending them back to the page.
export function serverPatch(values, original = null) {
  if (!/^[A-Za-z0-9_-]{1,64}$/.test(values.name)) throw Error('名称只支持 1–64 个字母、数字、下划线或短横线。');
  if (!['user','project'].includes(values.scope)) throw Error('请选择个人或项目保存位置。');
  if (!['stdio','http','sse'].includes(values.transport)) throw Error('请选择连接方式。');
  const patch = {type:values.transport,description:values.description.trim(),enabled:values.enabled,trust:values.trusted};
  const changed = original && (original.transport === 'stdio') !== (values.transport === 'stdio');
  const parse = (field, key, kind) => {
    const input = values[field]?.trim();
    if (!input) return;
    let value;
    try { value = JSON.parse(input); } catch { throw Error(`${key} 必须是有效的 JSON。`); }
    if (kind === 'array' && (!Array.isArray(value) || value.some(item => typeof item !== 'string'))) throw Error(`${key} 必须是字符串数组。`);
    if (kind === 'map' && (!value || Array.isArray(value) || typeof value !== 'object' || Object.values(value).some(item => typeof item !== 'string'))) throw Error(`${key} 必须是字符串键值对象。`);
    if (kind === 'oauth' && value !== null && (!value || Array.isArray(value) || typeof value !== 'object')) throw Error('OAuth 配置必须是 JSON 对象或 null。');
    patch[key] = value;
  };
  if (values.transport === 'stdio') {
    if (!values.command.trim() && (!original || changed)) throw Error('请输入可执行命令。');
    if (values.command.trim()) patch.command = values.command.trim();
    parse('args','args','array'); parse('env','env','map');
    if (values.cwd.trim()) patch.cwd = values.cwd.trim();
    if (changed) Object.assign(patch,{url:null,httpUrl:null,headers:null,oauth:null});
  } else {
    if (!values.url.trim() && (!original || changed)) throw Error('请输入服务地址。');
    if (values.url.trim()) {
      let url;
      try { url = new URL(values.url.trim()); } catch { throw Error('服务地址必须是完整的 HTTP(S) URL。'); }
      if (!['http:','https:'].includes(url.protocol) || url.username || url.password || url.hash) throw Error('服务地址必须是 HTTP(S) URL，不能包含用户名、密码或片段。');
      Object.assign(patch,{url:values.url.trim(),httpUrl:null});
    }
    parse('headers','headers','map'); parse('oauth','oauth','oauth');
    if (changed) Object.assign(patch,{command:null,args:null,env:null,cwd:null});
  }
  parse('include','includeTools','array'); parse('exclude','excludeTools','array');
  if (values.timeout.trim()) {
    const timeout = Number(values.timeout);
    if (!Number.isSafeInteger(timeout) || timeout < 0) throw Error('超时必须是非负整数，单位为毫秒。');
    patch.timeout = timeout;
  }
  return patch;
}

export function serverMarkup(server, pending = false, expanded = false) {
  const name = escape(server.name), key = encodeURIComponent(server.name), disabled = pending ? ' disabled' : '';
  const tools = Array.isArray(server.tools) ? server.tools : [];
  const button = (action, text, extra = '') => `<button type="button" id="mcp-${key}-${action}" class="btn ${extra}" data-action="${action}" data-server="${name}"${disabled}>${text}</button>`;
  return `<details class="mcp-server" data-server="${name}"${expanded ? ' open' : ''}>
    <summary><span class="mcp-server-title"><strong>${name}</strong><span class="mcp-server-meta"><span>${server.scope === 'project' ? '项目配置' : '个人配置'}</span><span>${transportLabels[server.transport] || '未知连接'}</span><span>${tools.filter(tool => tool.enabled).length} / ${tools.length} 工具启用</span></span></span><span class="mcp-state" data-state="${escape(server.state)}">${labels[server.state] || '状态未知'}</span></summary>
    <div class="mcp-server-content">${server.description ? `<p class="mcp-server-description">${escape(server.description)}</p>` : ''}<p class="mcp-source">${escape(server.source)}</p>
      ${server.error ? `<p class="mcp-server-error">${escape(server.error)}</p>` : ''}
      <div class="mcp-server-actions">${button(server.enabled ? 'disable' : 'enable',server.enabled ? '停用' : '启用')}${button('reconnect','重新连接')}${server.state === 'connected' ? button('refresh','刷新工具') : ''}${button('edit','编辑')}${server.transport !== 'stdio' ? button('login','登录授权') + button('logout','退出授权') : ''}${button('delete','删除','btn-danger')}</div>
      <p class="mcp-trust-label">${server.trusted ? '已信任 · 工具可直接执行' : '逐次确认 · 执行工具前需要你的授权'}</p>
      ${button(server.trusted ? 'untrust' : 'trust',server.trusted ? '恢复逐次确认' : '信任此服务','btn-ghost')}
      <h4 class="mcp-tools-title">可用工具</h4>${tools.length ? tools.map(tool => `<label class="mcp-tool"><input type="checkbox" id="mcp-${key}-tool-${encodeURIComponent(tool.name)}" data-server="${name}" data-tool="${escape(tool.name)}"${tool.enabled ? ' checked' : ''}${disabled}><span><strong>${escape(tool.name)}</strong>${tool.description ? `<small>${escape(tool.description)}</small>` : ''}</span></label>`).join('') : '<p class="settings-hint">连接成功后会在这里显示工具。服务也可能没有提供工具。</p>'}
    </div></details>`;
}

export function oauthCallback(href, pending, now = Date.now()) {
  const url = new URL(href);
  if (!url.searchParams.has('code') && !url.searchParams.has('error')) return null;
  const expires = Date.parse(pending?.expires_at);
  if (!pending || !pending.state || !Number.isFinite(expires) || url.searchParams.get('state') !== pending.state || now > expires) throw Error('授权回调已过期或不匹配，请重新登录。');
  if (url.searchParams.has('error')) throw Error('授权未完成，请重新登录。');
  const code = url.searchParams.get('code');
  if (!code) throw Error('授权回调缺少授权码，请重新登录。');
  return {state:pending.state,code};
}

export class MCPSettings {
  constructor(state) {
    this.state = state; this.workspace = ''; this.active = false; this.pending = false; this.data = null; this.requestId = 0;
    this.page = document.getElementById('settings-mcp'); this.list = document.getElementById('mcp-servers');
    this.notice = document.getElementById('mcp-notice'); this.editor = document.getElementById('mcp-editor');
    this.form = document.getElementById('mcp-server-form'); this.fields = Object.fromEntries(fieldIds.map(id => [id,document.getElementById(`mcp-${id}`)]));
    this.tabs = ['feishu','mcp'].map(name => document.getElementById(`settings-tab-${name}`));
    this.selectedTab = new URL(location.href).searchParams.get('mcp') === '1' ? 'mcp' : 'feishu';
    this.tabs.forEach((tab,index) => {
      tab.onclick = () => this.selectTab(index === 0 ? 'feishu' : 'mcp');
      tab.onkeydown = event => {
        if (!['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) return;
        event.preventDefault(); const next = event.key === 'Home' ? 0 : event.key === 'End' ? 1 : 1-index;
        this.selectTab(next === 0 ? 'feishu' : 'mcp'); this.tabs[next].focus();
      };
    });
    this.selectTab(this.selectedTab);
    document.getElementById('mcp-add').onclick = () => this.openEditor();
    document.getElementById('mcp-refresh').onclick = () => this.load(true);
    document.getElementById('mcp-editor-close').onclick = () => this.closeEditor();
    this.form.onsubmit = event => {event.preventDefault(); this.save();};
    this.fields.transport.onchange = () => this.transportFields();
    document.getElementById('mcp-workspace-form').onsubmit = event => {
      event.preventDefault(); if (this.pending) return;
      if (!this.editor.hidden && !window.confirm('切换项目会关闭当前未保存的服务配置。继续切换？')) return;
      this.closeEditor(); this.workspace = document.getElementById('mcp-workspace').value.trim(); this.customWorkspace = true; this.load(true);
    };
    document.getElementById('mcp-project-trust').onclick = () => this.projectTrust();
    this.list.onclick = event => {
      const button = event.target.closest('button[data-action]');
      if (button && !this.pending) this.action(button.dataset.server,button.dataset.action);
    };
    this.list.onchange = event => {
      const field = event.target;
      if (field.matches('input[data-tool]')) this.toggleTool(field.dataset.server,field.dataset.tool,field.checked);
    };
    document.getElementById('mcp-oauth-note').hidden = canUseWebOAuth(location.href);
  }
  selectTab(name) {
    this.selectedTab = name;
    for (const tab of this.tabs) {
      const selected = tab.id === `settings-tab-${name}`;
      tab.setAttribute('aria-selected',String(selected)); tab.tabIndex = selected ? 0 : -1;
    }
    document.getElementById('settings-feishu').hidden = name !== 'feishu'; this.page.hidden = name !== 'mcp';
    if (this.active && name === 'mcp') this.load(true);
  }
  async activate() {
    this.active = true;
    if (!this.customWorkspace) this.workspace = this.state.sessions.find(session => session.id === this.state.currentSessionId)?.workspace || '';
    if (this.selectedTab === 'mcp') await this.load(true);
    await this.completeOAuth();
    clearInterval(this.timer);
    this.timer = setInterval(() => { if (this.selectedTab === 'mcp' && !this.pending && this.editor.hidden) this.load(false); },2500);
  }
  deactivate() {
    this.active = false; this.requestId++; clearInterval(this.timer);
    // Typed credentials never linger in a hidden editor after leaving settings.
    this.closeEditor();
  }
  path(name = '', action = '', scope = '') {
    const query = new URLSearchParams(); if (this.workspace) query.set('workspace',this.workspace); if (scope) query.set('scope',scope);
    return '/mcp' + (name ? `/servers/${encodeURIComponent(name)}` : '') + (action ? '/' + action : '') + (query.size ? '?' + query : '');
  }
  message(text, error = false) {this.notice.textContent = text; this.notice.classList.toggle('is-error',error);}
  errorMessage(error) {return error.status === 409 ? '有任务正在运行，请待任务结束后再修改 MCP 配置。' : error.message;}
  async load(foreground = false) {
    if (!this.active || this.pending) return;
    const request = ++this.requestId;
    if (foreground) this.message('正在读取连接状态…');
    try {
      const data = await api.get(this.path());
      if (!this.active || request !== this.requestId) return;
      this.render(data); if (foreground || this.lastLoadError) this.message(''); this.lastLoadError = false;
    } catch (error) {if (this.active && request === this.requestId) {this.lastLoadError = true; this.message(`读取失败：${this.errorMessage(error)}`,true);}}
  }
  render(data) {
    this.data = data; this.workspace = data.workspace || this.workspace;
    const workspaceInput = document.getElementById('mcp-workspace');
    if (document.activeElement !== workspaceInput) workspaceInput.value = this.workspace;
    document.getElementById('mcp-count').textContent = (data.servers || []).length;
    document.getElementById('mcp-project-label').textContent = data.project_trusted ? '已授权此项目的 MCP 配置' : '项目配置尚未授权';
    document.getElementById('mcp-project-note').textContent = data.project_trusted ? '项目服务仅在此目录可用；取消授权会关闭这些连接。' : '个人配置始终可用。授权后才会读取此项目的 .easyagent/mcp.json。';
    document.getElementById('mcp-project-trust').textContent = data.project_trusted ? '取消项目授权' : '授权项目配置';
    this.fields.scope.querySelector('option[value="project"]').disabled = !data.project_trusted;
    const issues = document.getElementById('mcp-issues'); issues.hidden = !data.issues?.length;
    issues.innerHTML = (data.issues || []).map(issue => `<p>${escape(issue.server ? `${issue.server}：` : '')}${escape(issue.message)}<br><span class="mcp-source">${escape(issue.source)}</span></p>`).join('');
    this.renderServers();
  }
  renderServers() {
    if (!this.data) return;
    const signature = JSON.stringify({servers:this.data.servers || [],pending:this.pending});
    if (signature === this.serverSignature) return;
    this.serverSignature = signature;
    const opened = new Set([...this.list.querySelectorAll('details[open]')].map(detail => detail.dataset.server));
    const focus = this.list.contains(document.activeElement) ? document.activeElement.id : '';
    const html = (this.data.servers || []).map(server => serverMarkup(server,this.pending,opened.has(server.name))).join('') || '<p class="mcp-empty">还没有连接外部工具。添加 stdio 命令或远程 HTTP 服务，连接成功后即可选择 Agent 能使用的工具。</p>';
    this.list.innerHTML = html; if (focus) document.getElementById(focus)?.focus({preventScroll:true});
  }
  setBusy(value) {
    this.pending = value; this.requestId++;
    for (const id of ['mcp-add','mcp-refresh','mcp-project-trust','mcp-save','mcp-editor-close']) document.getElementById(id).disabled = value;
    document.querySelector('#mcp-workspace-form button').disabled = value;
    this.renderServers();
  }
  async mutate(request, success) {
    if (this.pending) return false;
    this.setBusy(true); this.message('正在更新连接…');
    try { const data = await request(); this.render(data); this.message(success); return true; }
    catch (error) {this.message(this.errorMessage(error),true); return false;}
    finally {this.setBusy(false);}
  }
  server(name) {return this.data?.servers?.find(server => server.name === name);}
  async projectTrust() {
    if (!this.data) return;
    const trusted = !this.data.project_trusted;
    if (!window.confirm(trusted ? `授权读取 ${this.workspace}/.easyagent/mcp.json？其中已启用的 stdio 服务会在服务器上启动命令。` : '取消此项目的授权并关闭它的 MCP 连接？个人服务仍可用。')) return;
    await this.mutate(() => api.put('/mcp/project-trust',{workspace:this.workspace,trusted}),trusted ? '已授权项目配置，正在连接服务。' : '已取消项目授权。');
  }
  transportFields() {
    const stdio = this.fields.transport.value === 'stdio';
    document.getElementById('mcp-stdio-fields').hidden = !stdio; document.getElementById('mcp-env-fields').hidden = !stdio;
    document.getElementById('mcp-http-fields').hidden = stdio; document.getElementById('mcp-headers-fields').hidden = stdio;
    const changed = this.editing && (this.editing.transport === 'stdio') !== stdio;
    this.fields.command.required = stdio && (!this.editing || changed); this.fields.url.required = !stdio && (!this.editing || changed);
  }
  openEditor(server = null) {
    if (this.pending) return;
    this.form.reset(); this.editing = server;
    this.fields.name.readOnly = Boolean(server); this.fields.scope.disabled = Boolean(server);
    this.fields.name.value = server?.name || ''; this.fields.scope.value = server?.scope || 'user';
    this.fields.transport.value = server?.transport || 'stdio'; this.fields.description.value = server?.description || '';
    this.fields.enabled.checked = server?.enabled ?? true; this.fields.trusted.checked = server?.trusted ?? false;
    document.getElementById('mcp-editor-title').textContent = server ? `编辑 ${server.name}` : '添加服务';
    document.getElementById('mcp-edit-hint').hidden = !server; document.getElementById('mcp-editor-error').textContent = '';
    document.getElementById('mcp-save').textContent = server ? '保存修改' : '保存并连接';
    this.editor.querySelector('details').open = false; this.editor.hidden = false; this.transportFields();
    this.editor.scrollIntoView({block:'nearest'}); (server ? this.fields.description : this.fields.name).focus();
  }
  closeEditor() {
    const name = this.editing?.name, visible = !this.editor.hidden;
    this.editor.hidden = true; this.form.reset(); this.editing = null; document.getElementById('mcp-editor-error').textContent = '';
    if (visible && this.active && this.selectedTab === 'mcp') (document.getElementById(name ? `mcp-${encodeURIComponent(name)}-edit` : 'mcp-add'))?.focus({preventScroll:true});
  }
  async save() {
    if (this.pending) return;
    const values = Object.fromEntries(fieldIds.map(id => [id,this.fields[id].type === 'checkbox' ? this.fields[id].checked : this.fields[id].value]));
    let patch;
    try {patch = serverPatch(values,this.editing);}
    catch (error) {document.getElementById('mcp-editor-error').textContent = error.message; return;}
    const saved = await this.mutate(() => api.put(this.path(values.name,'',values.scope),patch),'配置已保存，连接状态将自动更新。');
    if (saved) this.closeEditor();
  }
  async action(name, action) {
    const server = this.server(name); if (!server || this.pending) return;
    if (action === 'edit') {this.openEditor(server); return;}
    if (action === 'trust' && !window.confirm(`信任 ${name} 的工具？此服务的工具将可直接执行，请确认来源可信。`)) return;
    if (action === 'delete' && !window.confirm(`删除 ${name} 的${server.scope === 'project' ? '项目' : '个人'}配置并关闭连接？`)) return;
    if (action === 'login') {await this.login(server); return;}
    if (action === 'logout' && !window.confirm(`退出 ${name} 的 OAuth 授权并关闭连接？`)) return;
    await this.mutate(() => action === 'delete' ? api.del(this.path(name,'',server.scope)) : api.post(this.path(name,action,server.scope)), action === 'delete' ? '服务已删除。' : '连接已更新。');
  }
  async toggleTool(name, tool, enabled) {
    const server = this.server(name); if (!server) return;
    await this.mutate(() => api.post(this.path(name,`tools/${encodeURIComponent(tool)}`,server.scope),{enabled}),enabled ? `已启用 ${tool}。` : `已停用 ${tool}。`);
  }
  async login(server) {
    if (!canUseWebOAuth(location.href)) {this.message('局域网 HTTP 页面无法接收安全的 OAuth 回调，请在桌面客户端的 MCP 设置中登录。',true); return;}
    this.setBusy(true); this.message('正在准备授权…');
    try {
      const redirect = new URL('/',location.origin); redirect.searchParams.set('page','page-settings'); redirect.searchParams.set('mcp','1');
      const info = await api.post(this.path(server.name,'login',server.scope),{redirect_url:redirect.href});
      const authorization = new URL(info.authorization_url);
      if (!canUseWebOAuth(authorization.href) || !info.state || authorization.searchParams.get('state') !== info.state) throw Error('服务器返回了无效的授权地址。');
      sessionStorage.setItem(OAUTH_KEY,JSON.stringify({name:server.name,scope:server.scope,workspace:this.workspace,state:info.state,expires_at:info.expires_at}));
      location.assign(authorization.href);
    } catch (error) {this.message(this.errorMessage(error),true);}
    finally {this.setBusy(false);}
  }
  async completeOAuth() {
    const url = new URL(location.href);
    if (!url.searchParams.has('code') && !url.searchParams.has('error')) return;
    const strip = () => {for (const key of ['code','state','error','error_description']) url.searchParams.delete(key); history.replaceState(null,'',url.href);};
    let pending;
    try {pending = JSON.parse(sessionStorage.getItem(OAUTH_KEY) || 'null'); sessionStorage.removeItem(OAUTH_KEY);}
    catch {strip(); this.message('无法读取登录状态，请重新授权。',true); return;}
    try {
      const callback = oauthCallback(url.href,pending);
      this.workspace = pending.workspace; this.customWorkspace = true; this.selectTab('mcp');
      await this.mutate(() => api.post(this.path(pending.name,'login/complete',pending.scope),callback),'授权成功，服务已重新连接。');
    } catch (error) {this.selectTab('mcp'); this.message(error.message,true);}
    finally {strip();}
  }
}
