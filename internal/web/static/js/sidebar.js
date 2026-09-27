import { api, authFetch } from './api.js';
import { ModelPicker } from './model-picker.js';
// Sidebar: session list, model selector, connection status

export class Sidebar {
  constructor(ws, state, onSessionChange) {
    this.ws = ws;
    this.state = state;
    this.onSessionChange = onSessionChange;

    this.sessionList = document.getElementById('session-list');
    this.modelSelect = document.getElementById('model-select');
    this.modelPicker = new ModelPicker(this.modelSelect, model => this.chooseModel(model));
    this.newSessionBtn = document.getElementById('new-session-btn');
    this.connectionStatus = document.getElementById('connection-status');

    this.search = document.getElementById('session-search');
    this.search.addEventListener('input', () => this._renderSessions());
    this._bindEvents();
    this._bindWS();
  }

  _bindEvents() {
    this.newSessionBtn.addEventListener('click', () => this.createSession());

    // Session list click delegation
    this.sessionList.addEventListener('click', (e) => {
      const item = e.target.closest('.session-item');
      if (!item) return;

      const deleteBtn = e.target.closest('.delete-btn');
      if (deleteBtn) {
        e.stopPropagation();
        this.deleteSession(item.dataset.sessionId);
        return;
      }

      this.selectSession(item.dataset.sessionId);
    });
  }

  _bindWS() {
    this.ws.onStatusChange = (connected) => {
      this._updateConnectionStatus(connected);
    };
  }

  _updateConnectionStatus(connected) {
    const dot = this.connectionStatus.querySelector('.status-dot');
    const text = this.connectionStatus.querySelector('.status-text');

    if (connected) {
      dot.className = 'status-dot online';
      text.textContent = '已连接';
    } else {
      dot.className = 'status-dot connecting';
      text.textContent = '重新连接中…';
    }
  }

  async loadSessions() {
    try {
      const resp = await authFetch(`${this.state.baseUrl}/sessions`);
      if (!resp.ok) throw new Error('请求失败（' + resp.status + '）');
      this.state.sessions = await resp.json() || [];
      this._renderSessions();
    } catch (e) {
      this.sessionList.textContent = '对话加载失败，请刷新重试。';
    }
  }

  async loadModels() {
    try {
      const resp = await authFetch(`${this.state.baseUrl}/models`);
      if (!resp.ok) throw new Error('请求失败（' + resp.status + '）');
      const data = await resp.json();
      this.state.models = data.models || [];
      this.state.selectedModel ||= data.current;
      this.modelPicker.setModels(this.state.models, this.state.selectedModel);
      this.state.refreshControls?.();
    } catch (e) {
      document.getElementById('model-selected-name').textContent = '模型加载失败';
    }
  }

  async chooseModel(model) {
    if (this.state.streaming || this.state.modelChanging) return;
    const id = this.state.currentSessionId;
    this.state.modelChanging = true; this.state.refreshControls?.();
    try {
      if (id) await api.post(`/sessions/${encodeURIComponent(id)}/model`, {model:model.id, provider:model.provider});
      if (id === this.state.currentSessionId) {
        this.state.selectedModel = model;
        this.modelPicker.setSelected(model);
      }
    } catch (error) {
      const notice = document.getElementById('chat-notice');
      notice.textContent = '模型切换失败：' + error.message; notice.hidden = false;
    } finally { this.state.modelChanging = false; this.state.refreshControls?.(); }
  }

  async createSession({select = true} = {}) {
    if (this.creating || this.state.modelChanging) return null;
    this.creating = true; this.newSessionBtn.disabled = true;
    try {
      const resp = await authFetch(`${this.state.baseUrl}/sessions`, { method: 'POST' });
      if (!resp.ok) throw new Error('请求失败（' + resp.status + '）');
      const data = await resp.json();
      const model = this.state.selectedModel;
      if (model?.id) await api.post(`/sessions/${encodeURIComponent(data.id)}/model`, {model:model.id, provider:model.provider});
      this.state.sessions.unshift({
        id: data.id,
        created_at: data.created_at,
        message_count: 0,
        last_active: data.created_at,
      });
      this._renderSessions();
      if (select) await this.selectSession(data.id);
      return data.id;
    } catch (e) {
      const notice = document.getElementById('chat-notice'); notice.textContent = '新建失败：' + e.message; notice.hidden = false;
      return null;
    } finally { this.creating = false; this.newSessionBtn.disabled = false; }
  }

  async deleteSession(sessionId) {
    if (!confirm("确认删除这条对话？此操作不可恢复。")) return;
    try {
      const resp = await authFetch(`${this.state.baseUrl}/sessions/${sessionId}`, { method: 'DELETE' });
      if (!resp.ok) throw new Error('请求失败（' + resp.status + '）');
      this.state.sessions = this.state.sessions.filter(s => s.id !== sessionId);
      if (this.state.currentSessionId === sessionId) {
        history.replaceState(null, '', location.pathname + location.search);
        this.state.currentSessionId = null;
        this.onSessionChange(null);
      }
      this._renderSessions();
    } catch (e) {
      console.error('Failed to delete session:', e);
    }
  }

  async selectSession(sessionId) {
    this.state.currentSessionId = sessionId;
    this._renderSessions();
    history.replaceState(null, "", "#s=" + encodeURIComponent(sessionId));
    const messages = this.onSessionChange(sessionId);
    this.state.modelInfoLoading = true; this.state.refreshControls?.();
    try {
      const info = await api.get(`/sessions/${encodeURIComponent(sessionId)}/info`);
      if (sessionId === this.state.currentSessionId) {
        this.state.selectedModel = {id:info.model, provider:info.provider};
        this.modelPicker.setSelected(this.state.selectedModel);
      }
    } catch {
      if (sessionId === this.state.currentSessionId) {
        this.state.selectedModel = null; this.modelPicker.setSelected(null);
      }
    } finally {
      if (sessionId === this.state.currentSessionId) { this.state.modelInfoLoading = false; this.state.refreshControls?.(); }
    }
    await messages;
  }

  _renderSessions() {
    const query = this.search.value.trim().toLocaleLowerCase();
    const sessions = this.state.sessions.filter(s => (s.title || '新对话').toLocaleLowerCase().includes(query));
    document.getElementById('session-count').textContent = this.state.sessions.length;
    this.sessionList.replaceChildren();
    if (!sessions.length) {
      const empty = document.createElement('p'); empty.className = 'empty-sessions';
      empty.textContent = query ? '没有找到匹配的对话' : '从一个新对话开始'; this.sessionList.append(empty); return;
    }
    for (const s of sessions) {
      const item = document.createElement('div'); item.className = 'session-item' + (s.id === this.state.currentSessionId ? ' active' : ''); item.dataset.sessionId = s.id;
      const open = document.createElement('button'); open.className = 'session-open';
      const title = document.createElement('span'); title.className = 'session-title'; title.textContent = (s.title || '').trim() || '新对话'; open.title = title.textContent;
      const meta = document.createElement('span'); meta.className = 'session-meta'; meta.textContent = this._formatSessionMeta(s); open.append(title, meta);
      const remove = document.createElement('button'); remove.className = 'delete-btn'; remove.textContent = '×'; remove.setAttribute('aria-label', '删除对话：' + title.textContent);
      item.append(open, remove); this.sessionList.append(item);
    }
  }
  _formatSessionMeta(s) {
    const msgCount = s.message_count || 0;
    return `${msgCount} 条消息 · ${this._formatTime(s.last_active)}`;
  }

  _formatTime(ts) {
    if (!ts) return '';
    const date = new Date(ts * 1000);
    const now = new Date();
    const diff = now - date;

    if (diff < 60000) return '刚刚';
    if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`;
    if (diff < 172800000) return '昨天';
    return date.toLocaleDateString();
  }
}
