// 会话管理页：列表、消息回看、删除

import { api } from './api.js';
import { renderMarkdown } from './markdown.js';

export class SessionsPage {
  constructor(state) {
    this.state = state;
    this.sessions = [];
    this.detailRequest = 0;
    this.$overview = document.getElementById('sess-overview');
    this.$close = document.getElementById('sess-close-btn');

    this.$table = document.getElementById('sess-table');
    this.$messages = document.getElementById('sess-messages');
    this.$msgList = document.getElementById('sess-msg-list');
    this.$msgTitle = document.getElementById('sess-msg-title');

    document.getElementById('sess-refresh-btn').onclick = () => this.load();
    this.$close.onclick = () => this.closeMessages();
    this.$messages.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); this.closeMessages(); }
    });
  }

  activate() {
    this.load();
  }

  closeMessages(restoreFocus = true) {
    this.detailRequest++;
    this.$messages.hidden = true;
    this.$overview.hidden = false;
    this.$overview.scrollTop = this.listScrollTop || 0;
    if (restoreFocus) {
      const target = this.returnFocus?.isConnected ? this.returnFocus : document.getElementById('sess-refresh-btn');
      target.focus({preventScroll:true});
    }
  }

  deactivate() { this.closeMessages(false); }

  async load() {
    let sessions;
    try {
      sessions = await api.get('/sessions');
    } catch (e) {
      return;
    }
    this.sessions = sessions || [];
    this.render();
  }

  render() {
    this.$table.innerHTML = '';
    if (!this.sessions.length) {
      this.$table.innerHTML = '<div class="wf-empty">没有会话</div>';
      return;
    }
    const table = document.createElement('table');
    table.className = 'data-table';
    table.innerHTML = `
      <thead><tr>
        <th>标题</th><th class="col-nowrap">消息数</th><th class="col-nowrap">最近活跃</th><th class="col-nowrap">操作</th>
      </tr></thead>`;
    const tbody = document.createElement('tbody');
    for (const s of this.sessions) {
      const tr = document.createElement('tr');
      const title = (s.title || '').trim() || '新对话';
      const short = title.length > 40 ? title.slice(0, 40) + '…' : title;
      tr.innerHTML = `
        <td class="sess-title-cell"><span title="${escapeHTML(title)}">${escapeHTML(short)}</span></td>
        <td class="col-nowrap">${s.message_count ?? '-'}</td>
        <td class="col-nowrap">${formatTime(s.last_active || s.created_at)}</td>`;
      const actions = document.createElement('td');
      actions.className = 'sess-actions col-nowrap';
      const viewBtn = document.createElement('button');
      viewBtn.className = 'btn btn-ghost btn-sm';
      viewBtn.textContent = '查看';
      viewBtn.onclick = () => this.viewMessages(s.id);
      const delBtn = document.createElement('button');
      delBtn.className = 'btn btn-danger btn-sm';
      delBtn.textContent = '删除';
      delBtn.onclick = () => this.deleteSession(s.id);
      actions.append(viewBtn, delBtn);
      tr.appendChild(actions);
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    this.$table.appendChild(table);
  }

  async viewMessages(sessionID) {
    const request = ++this.detailRequest;
    this.returnFocus = document.activeElement;
    this.listScrollTop = this.$overview.scrollTop;
    this.$overview.hidden = true;
    this.$messages.hidden = false;
    this.$msgTitle.textContent = '会话消息';
    this.$msgList.innerHTML = '<p class="wf-empty" role="status">正在加载消息…</p>';
    this.$msgList.scrollTop = 0;
    this.$close.focus({preventScroll:true});
    let messages;
    try {
      messages = await api.get(`/sessions/${sessionID}/messages`);
    } catch (e) {
      if (request !== this.detailRequest) return;
      this.$msgList.innerHTML = '';
      const error = document.createElement('p');
      error.className = 'wf-empty';
      error.setAttribute('role', 'alert');
      error.textContent = '加载失败：' + e.message + '。请返回会话后重试。';
      this.$msgList.appendChild(error);
      return;
    }
    if (request !== this.detailRequest) return;
    this.$msgTitle.textContent = `会话消息 · ${shortID(sessionID)}`;
    this.$msgList.innerHTML = '';
    if (!messages?.length) this.$msgList.innerHTML = '<p class="wf-empty">这段会话还没有消息</p>';
    for (const msg of messages || []) {
      const item = document.createElement('div');
      item.className = 'sess-msg sess-msg-' + msg.role;

      const head = document.createElement('div');
      head.className = 'sess-msg-head';
      head.textContent = msg.role + (msg.is_error ? ' (error)' : '');
      item.appendChild(head);

      const body = document.createElement('div');
      body.className = 'sess-msg-body';
      if (msg.role === 'assistant' && msg.content) {
        body.innerHTML = renderMarkdown(msg.content);
      } else if (msg.tool_calls?.length) {
        const pre = document.createElement('pre');
        pre.textContent = msg.tool_calls.map(tc => `🔧 ${tc.name}(${tc.args})`).join('\n');
        body.appendChild(pre);
      } else if (msg.content) {
        const pre = document.createElement('pre');
        pre.textContent = msg.content;
        body.appendChild(pre);
      }
      if (body.childNodes.length) item.appendChild(body);
      this.$msgList.appendChild(item);
    }
    this.$messages.hidden = false;
  }

  async deleteSession(sessionID) {
    if (!confirm('确认删除会话 ' + shortID(sessionID) + '？此操作不可恢复。')) return;
    try {
      await api.del('/sessions/' + sessionID);
      await this.load();
    } catch (e) {
      alert('删除失败：' + e.message);
    }
  }
}

function escapeHTML(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function shortID(id) {
  return String(id || '').slice(0, 12);
}

function formatTime(unix) {
  if (!unix) return '-';
  // 兼容秒/毫秒两种口径
  const ms = unix > 1e12 ? unix : unix * 1000;
  const d = new Date(ms);
  return isNaN(d) ? '-' : d.toLocaleString('zh-CN', { hour12: false });
}
