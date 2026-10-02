import { copyText } from './clipboard.js';
// Main application — initializes all modules and page navigation

import { PiWebSocket } from './websocket.js';
import { ChatPanel } from './chat.js';
import { Sidebar } from './sidebar.js';
import { DynamicWorkflowsPage } from './dynamic-workflows.js';
import { WorkflowsPage } from './workflows.js';
import { SessionsPage } from './sessions.js';
import { SettingsPage } from './settings.js';
import { MCPSettings } from './mcp-settings.js';
import { api, getToken, showLogin } from './api.js';

// Determine base URL (same host serving this page)
const baseUrl = window.location.protocol + '//' + window.location.host;

// Global state
const state = {
  baseUrl,
  currentSessionId: null,
  sessions: [],
  models: [],
  streaming: false,
  restoringSession: /^#s=.+/.test(location.hash),
};

// Initialize modules
const ws = new PiWebSocket(baseUrl);
const chat = new ChatPanel(ws, state);
const sidebar = new Sidebar(ws, state, onSessionChange);
const workflowsPage = new WorkflowsPage(state);
const dynamicPage = new DynamicWorkflowsPage(state);
const sessionsPage = new SessionsPage(state);
const settingsPage = new SettingsPage();
const mcpSettings = new MCPSettings(state);

// Connect WebSocket
ws.connect();

// Load initial data
sidebar.loadSessions();
sidebar.loadModels();
refreshAuthBadge();

// Handle session change
async function onSessionChange(sessionId) {
  const session = state.sessions.find(s => s.id === sessionId);
  document.getElementById('chat-title').textContent = session?.title || '新对话';
  setSidebar(false);
  await chat.selectSession(sessionId);
}
state.navigate = switchPage;
state.composeWorkflow = () => {
  switchPage('page-chat');
  if (!chat.input.value.trim()) { chat.input.value = '/workflow '; chat._inputChanged(); }
  chat.input.focus();
  if (!chat.input.value.startsWith('/workflow ')) chat._notice('当前草稿已保留。输入 /workflow 加任务描述可创建动态工作流。');
};
state.openModels = () => { setSidebar(true); sidebar.modelPicker.open(); };
state.createSession = () => sidebar.createSession({select:false});
state.refreshControls = () => chat._updateButtons();
state.selectSession = id => sidebar.selectSession(id);
state.onSessionUpdated = () => sidebar.loadSessions();
state.onPromptSent = (text) => {
  const s = state.sessions.find(s => s.id === state.currentSessionId);
  if (s && !s.title) { s.title = text; sidebar._renderSessions(); document.getElementById('chat-title').textContent = text; }
};
function setSidebar(open) {
  document.getElementById('app').classList.toggle('sidebar-open', open);
  document.getElementById('sidebar-backdrop').hidden = !open;
  document.getElementById('sidebar-toggle').setAttribute('aria-expanded', String(open));
}
document.getElementById('sidebar-toggle').onclick = () => setSidebar(!document.getElementById('app').classList.contains('sidebar-open'));
document.getElementById('sidebar-backdrop').onclick = () => setSidebar(false);
document.addEventListener('keydown', e => { if (e.key === 'Escape') setSidebar(false); });
fetch('/health').then(r => r.json()).then(data => { document.getElementById('build-version').textContent = data.version || 'dev'; }).catch(() => {});

// Periodic ping to keep connection alive
setInterval(() => {
  if (ws.connected) ws.sendPing();
}, 30000);

// 会话深链接：#s=<sessionId> 自动选中并打开该会话（刷新不丢当前会话）
const hashSession = location.hash.match(/^#s=(.+)$/);
if (hashSession) {
  const wanted = decodeURIComponent(hashSession[1]);
  let tries = 0;
  const trySelect = async () => {
    if (state.sessions.some(s => s.id === wanted)) {
      await sidebar.selectSession(wanted);
      state.restoringSession = false; chat._updateButtons();
    } else if (tries++ < 20) {
      setTimeout(trySelect, 300);
    } else {
      state.restoringSession = false; chat._updateButtons();
      chat._notice('未能恢复此会话，请从历史列表重新选择。');
    }
  };
  setTimeout(trySelect, 200);
}

// ─── 页面导航 ────────────────────────────────────────────────────────────────

const pages = { 'page-dynamic-workflows': dynamicPage, 'page-chat': null, 'page-workflows': workflowsPage, 'page-sessions': sessionsPage, 'page-settings': settingsPage };

document.querySelectorAll('.nav-tab').forEach(tab => {
  tab.onclick = () => switchPage(tab.dataset.page);
});

function switchPage(pageID) {
  document.querySelectorAll('.nav-tab').forEach(t => { t.classList.toggle('active', t.dataset.page === pageID); t.setAttribute('aria-current', t.dataset.page === pageID ? 'page' : 'false'); });
  setSidebar(false); chat.commandMenu.close();
  document.querySelectorAll('.page').forEach(p => {
    const active = p.id === pageID;
    p.hidden = !active;
    p.classList.toggle('active', active);
  });
  const page = pages[pageID];
  if (page && page.activate) page.activate();
  // 离开工作流页时停掉详情轮询由 deactivate 控制；此处简化：仅聊天页外的页不处理
  if (pageID !== 'page-workflows') workflowsPage.deactivate();
  if (pageID !== 'page-dynamic-workflows') dynamicPage.deactivate();
  if (pageID !== 'page-settings') settingsPage.deactivate();
  if (pageID === 'page-settings') mcpSettings.activate();
  else mcpSettings.deactivate();
  if (pageID !== 'page-sessions') sessionsPage.deactivate();
}

// 深链接：?page=page-workflows 直达指定页签
const initialPage = new URLSearchParams(location.search).get('page');
if (initialPage && pages[initialPage]) switchPage(initialPage);

// 登录浮层入口：点右上角角标可重新填写令牌
document.getElementById('nav-auth').onclick = () => showLogin();

async function refreshAuthBadge() {
  const badge = document.getElementById('nav-auth');
  if (!getToken()) {
    // 探测是否需要登录：访问一个受保护端点
    try {
      await api.get('/sessions');
      badge.textContent = '本机模式';
    } catch (e) {
      badge.textContent = '未登录';
    }
    return;
  }
  badge.textContent = '已登录';
}

// Delegation also covers Markdown in session details and workflow results.
document.addEventListener('click', async event => {
  const button = event.target.closest('.copy-code'); if (!button) return;
  try { await copyText(button.closest('.code-block').querySelector('code').textContent); button.textContent = '已复制'; }
  catch { button.textContent = '请选中代码复制'; }
  setTimeout(() => { button.textContent = '复制代码'; }, 1800);
});
