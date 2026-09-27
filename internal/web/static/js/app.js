// Main application — initializes all modules and page navigation

import { PiWebSocket } from './websocket.js';
import { ChatPanel } from './chat.js';
import { Sidebar } from './sidebar.js';
import { WorkflowsPage } from './workflows.js';
import { SessionsPage } from './sessions.js';
import { SettingsPage } from './settings.js';
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
};

// Initialize modules
const ws = new PiWebSocket(baseUrl);
const chat = new ChatPanel(ws, state);
const sidebar = new Sidebar(ws, state, onSessionChange);
const workflowsPage = new WorkflowsPage(state);
const sessionsPage = new SessionsPage(state);
const settingsPage = new SettingsPage();

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
  const trySelect = () => {
    if (state.sessions.some(s => s.id === wanted)) {
      sidebar.selectSession(wanted);
    } else if (tries++ < 20) {
      setTimeout(trySelect, 300);
    }
  };
  setTimeout(trySelect, 200);
}

// ─── 页面导航 ────────────────────────────────────────────────────────────────

const pages = { 'page-chat': null, 'page-workflows': workflowsPage, 'page-sessions': sessionsPage, 'page-settings': settingsPage };

document.querySelectorAll('.nav-tab').forEach(tab => {
  tab.onclick = () => switchPage(tab.dataset.page);
});

function switchPage(pageID) {
  document.querySelectorAll('.nav-tab').forEach(t => { t.classList.toggle('active', t.dataset.page === pageID); t.setAttribute('aria-current', t.dataset.page === pageID ? 'page' : 'false'); });
  setSidebar(false);
  document.querySelectorAll('.page').forEach(p => {
    const active = p.id === pageID;
    p.hidden = !active;
    p.classList.toggle('active', active);
  });
  const page = pages[pageID];
  if (page && page.activate) page.activate();
  // 离开工作流页时停掉详情轮询由 deactivate 控制；此处简化：仅聊天页外的页不处理
  if (pageID !== 'page-workflows') workflowsPage.deactivate();
  if (pageID !== 'page-settings') settingsPage.deactivate();
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
