import { authFetch } from './api.js';
import { renderMarkdown } from './markdown.js';

export class ChatPanel {
  constructor(ws, state) {
    this.ws = ws; this.state = state;
    this.messageList = document.getElementById('message-list');
    this.input = document.getElementById('message-input');
    this.sendBtn = document.getElementById('send-btn');
    this.stopBtn = document.getElementById('stop-btn');
    this.emptyState = document.getElementById('empty-state');
    this.chatView = document.getElementById('chat-view');
    this.panel = document.getElementById('chat-panel');
    this.notice = document.getElementById('chat-notice');
    this.status = document.getElementById('chat-status');
    this.jump = document.getElementById('jump-latest');
    this.drafts = new Map(); this.busy = new Set(); this.views = new Map(); this.follow = true; this.inputRevision = 0;
    this.clear(); this._bindEvents(); this._bindWS(); this._updateButtons();
  }
  _bindEvents() {
    this.sendBtn.addEventListener('click', () => this._send());
    this.input.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && e.keyCode !== 229) {
        e.preventDefault(); this._send();
      }
    });
    this.input.addEventListener('input', () => { this.inputRevision++; this._resizeInput(); this._updateButtons(); });
    this.stopBtn.addEventListener('click', () => {
      if (!this.ws.sendCancel(this.state.currentSessionId)) this._notice('连接已断开，重新连接后可停止任务。');
      else this.status.textContent = '正在停止…';
    });
    this.messageList.addEventListener('scroll', () => {
      this.follow = this.messageList.scrollHeight - this.messageList.scrollTop - this.messageList.clientHeight < 80;
      this.jump.hidden = this.follow;
    });
    this.jump.onclick = () => { this.follow = true; this._scrollToBottom(); };
    document.querySelectorAll('[data-prompt]').forEach(button => button.onclick = () => {
      this.inputRevision++; this.input.value = button.dataset.prompt; this.input.focus(); this._resizeInput(); this._updateButtons();
    });
  }
  _bindWS() {
    this.ws.on('open', () => { this._updateButtons(); this._notice(''); });
    this.ws.on('close', () => {
      this.busy.clear(); this.views.clear(); this._finalizeStream();
      this._notice('连接已断开，正在重连。草稿已保留；重新连接后请查看任务结果。');
    });
    this.streamHandlers = {
      text_delta: event => {
        if (!this._currentStreamEl) this._startAssistantStream();
        this._currentStreamText += event.text_delta || '';
        this._currentStreamEl.querySelector('.stream-text').innerHTML = renderMarkdown(this._currentStreamText) + '<span class="streaming-cursor"></span>';
        this._scrollToBottom();
      },
      tool_start: event => {
        if (!this._currentStreamEl) this._startAssistantStream();
        this._addToolCall(event.tool_call_id, event.tool_name, 'running', event.tool_args);
      },
      tool_end: event => this._updateToolCall(event.tool_call_id, event.is_error ? 'error' : 'done', event.tool_result),
      done: event => this._finalizeStream(event.final_message),
      error: event => this._finalizeWithError(event.error),
    };
    for (const [type, handler] of Object.entries(this.streamHandlers)) {
      this.ws.on('event:' + type, (event, id) => {
        if (type === 'done' || type === 'error') { this.busy.delete(id); this.state.onSessionUpdated?.(); }
        if (id !== this.state.currentSessionId) this.views.get(id)?.events.push({type, event});
        else if (this.loading) this.loadEvents.push({type, event});
        else handler(event);
        this._updateButtons();
      });
    }
    this.ws.on('error', data => {
      const id = data.session_id || this.state.currentSessionId;
      this.busy.delete(id);
      const event = {error: data.message || data.error || '请求失败，请重试。'};
      if (id === this.state.currentSessionId) this._finalizeWithError(event.error);
      else this.views.get(id)?.events.push({type:'error', event});
    });
    this.ws.on('status', data => {
      if (data.streaming) this.busy.add(data.session_id); else this.busy.delete(data.session_id);
      if (data.session_id === this.state.currentSessionId) this.state.streaming = !!data.streaming;
      this._updateButtons();
    });
  }
  async selectSession(id) {
    if (this.visibleSession && this.busy.has(this.visibleSession)) {
      this.views.set(this.visibleSession, {nodes:[...this.messageList.childNodes], streamEl:this._currentStreamEl, text:this._currentStreamText, tools:this._currentToolCalls, events:[]});
    }
    this.drafts.set(this.visibleSession || '', this.input.value);
    this.visibleSession = id; this.input.value = this.drafts.get(id || '') || '';
    this.loading = false; this._resizeInput(); this.clear(); this._notice('');
    this.state.streaming = this.busy.has(id); this._updateButtons();
    if (id) {
      this.show();
      const saved = this.views.get(id);
      if (saved) {
        this.messageList.replaceChildren(...saved.nodes); this._currentStreamEl = saved.streamEl; this._currentStreamText = saved.text; this._currentToolCalls = saved.tools;
        this.views.delete(id);
        for (const {type,event} of saved.events) this.streamHandlers[type](event);
        this._scrollToBottom();
      } else await this.loadHistory(id);
    } else this.hide();
  }
  show() { this.emptyState.hidden = true; this.messageList.hidden = false; this.panel.classList.add('has-session'); }
  hide() { this.emptyState.hidden = false; this.messageList.hidden = true; this.panel.classList.remove('has-session'); }
  clear() {
    this.messageList.replaceChildren(); this._currentStreamEl = null; this._currentStreamText = '';
    this._currentToolCalls = {}; this.follow = true; this.jump.hidden = true;
  }
  async loadHistory(id) {
    this.loading = true; this.loadEvents = []; this._updateButtons();
    this.messageList.innerHTML = '<div class="loading-history" role="status">正在加载对话…</div>';
    try {
      const resp = await authFetch(`${this.state.baseUrl}/sessions/${encodeURIComponent(id)}/messages`);
      if (!resp.ok) throw new Error('暂时无法加载对话');
      const messages = await resp.json();
      if (id !== this.state.currentSessionId) return;
      this.clear();
      for (const msg of messages || []) {
        if (msg.role === 'user') this._addUserMessage(msg.content || '');
        else if (msg.role === 'assistant') {
          if (msg.content) this._addAssistantMessage(msg.content);
          if (msg.tool_calls?.length) {
            this._startAssistantStream();
            for (const tc of msg.tool_calls) this._addToolCall(tc.id, tc.name, 'done', tc.args);
          }
        } else if (msg.role === 'toolResult' || msg.role === 'tool') this._updateToolCall(msg.tool_call_id, msg.is_error ? 'error' : 'done', msg.content);
      }
      this._finishTools(); this._currentStreamEl = null;
      if (!this.messageList.children.length) this.messageList.innerHTML = '<p class="empty-conversation">对话已准备好。写下第一个想法吧。</p>';
      this._scrollToBottom();
    } catch (e) {
      if (id !== this.state.currentSessionId) return;
      this.messageList.replaceChildren(); this._notice(e.message + '，可重新选择此对话重试。');
    } finally { if (id === this.state.currentSessionId) { this.loading = false; for (const {type,event} of this.loadEvents) this.streamHandlers[type](event); this.loadEvents = []; this._updateButtons(); } }
  }
  async _send() {
    const text = this.input.value.trim();
    if (!text || this.state.streaming || this.creating || this.loading || this.state.modelChanging || this.state.modelInfoLoading) return;
    if (this.busy.size) { this._notice('另一个对话正在执行，请等待完成或返回该对话停止。'); return; }
    if (!this.ws.connected) { this._notice('尚未连接到服务，消息已保留。'); return; }
    let sessionId = this.state.currentSessionId;
    if (!sessionId) {
      const revision = this.inputRevision;
      this.creating = true; this._updateButtons();
      sessionId = await this.state.createSession?.();
      if (sessionId) this.drafts.set(sessionId, text);
      if (!sessionId || this.state.currentSessionId || revision !== this.inputRevision) {
        this.creating = false; this._updateButtons();
        if (sessionId) this._notice('原消息已保留在新对话草稿中，尚未发送。');
        return;
      }
      await this.state.selectSession(sessionId);
      this.creating = false; this._updateButtons();
      if (this.state.currentSessionId !== sessionId || revision !== this.inputRevision) return;
    }
    if (!this.ws.sendPrompt(sessionId, text)) { this._notice('发送失败，消息已保留。'); this._updateButtons(); return; }
    this.show(); this.messageList.querySelector('.empty-conversation')?.remove();
    this.follow = true; this._addUserMessage(text); this._startAssistantStream();
    this.input.value = ''; this.drafts.delete(this.visibleSession || ''); this.drafts.delete('');
    this._resizeInput(); this.busy.add(this.state.currentSessionId); this.state.streaming = true;
    this._notice(''); this._updateButtons(); this._scrollToBottom();
    this.state.onPromptSent?.(text);
  }
  _addUserMessage(text) {
    const el = document.createElement('div'); el.className = 'user-message';
    const bubble = document.createElement('div'); bubble.className = 'user-bubble'; bubble.textContent = text;
    el.append(bubble); this.messageList.append(el); this._scrollToBottom();
  }
  _assistantElement() {
    const el = document.createElement('article'); el.className = 'assistant-message';
    el.innerHTML = '<div class="assistant-avatar" aria-hidden="true">ea·</div><div class="assistant-content"><div class="message-label"><span>EasyAgent</span></div><div class="markdown-body stream-text"></div><div class="tool-calls-container"></div></div>';
    this.messageList.append(el); return el;
  }
  _copyButton(el, text) {
    const button = document.createElement('button'); button.className = 'copy-reply'; button.textContent = '复制回复';
    button.onclick = async () => {
      try {
        if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(text);
        else {
          const field = document.createElement('textarea'); field.value = text; field.className = 'clipboard-buffer'; document.body.append(field); field.select();
          const copied = document.execCommand('copy'); field.remove(); if (!copied) throw new Error('copy unavailable');
        }
        button.textContent = '已复制'; setTimeout(() => { button.textContent = '复制回复'; }, 1600);
      } catch { this._notice('浏览器不允许自动复制，请选中回复文字后复制。'); }
    };
    el.querySelector('.message-label').append(button);
  }
  _addAssistantMessage(text) {
    const el = this._assistantElement(); el.querySelector('.stream-text').innerHTML = renderMarkdown(text);
    this._copyButton(el, text); this._scrollToBottom();
  }
  _startAssistantStream() {
    this._currentStreamEl = this._assistantElement(); this._currentStreamText = ''; this._currentToolCalls = {};
    this._currentStreamEl.querySelector('.stream-text').innerHTML = '<span class="streaming-cursor"></span>';
  }
  _addToolCall(id, name, status, args) {
    if (!this._currentStreamEl) return;
    const container = this._currentStreamEl.querySelector('.tool-calls-container');
    let group = container.querySelector('.tool-group');
    if (!group) { group = document.createElement('details'); group.className = 'tool-group'; group.open = true; group.innerHTML = '<summary>工具执行</summary>'; container.append(group); }
    const el = document.createElement('details'); el.className = `tool-call ${status}`;
    el.innerHTML = '<summary><span class="tool-name"></span><span class="tool-status"></span></summary><pre class="tool-args"></pre><pre class="tool-result"></pre>';
    el.querySelector('.tool-name').textContent = name || '工具';
    el.querySelector('.tool-status').textContent = status === 'running' ? '执行中' : '已完成';
    const argEl = el.querySelector('.tool-args'); argEl.textContent = args ? (typeof args === 'string' ? args : JSON.stringify(args, null, 2)) : ''; argEl.hidden = !args;
    el.querySelector('.tool-result').textContent = status === 'running' ? '等待执行结果…' : '暂无输出';
    group.append(el); this._currentToolCalls[id] = el;
    group.querySelector('summary').textContent = `工具执行 · ${group.querySelectorAll('.tool-call').length} 项`;
    this._scrollToBottom();
  }
  _updateToolCall(id, status, result) {
    const el = this._currentToolCalls[id]; if (!el) return;
    el.classList.remove('running', 'done', 'error'); el.classList.add(status);
    el.querySelector('.tool-status').textContent = status === 'error' ? '执行失败' : '已完成';
    el.querySelector('.tool-result').textContent = typeof result === 'string' ? result : JSON.stringify(result ?? '无输出', null, 2);
    if (status === 'error') { el.open = true; el.parentElement.open = true; }
    this._scrollToBottom();
  }
  _finishTools() {
    this.messageList.querySelectorAll('.streaming-cursor').forEach(cursor => cursor.remove());
    this.messageList.querySelectorAll('.tool-group').forEach(group => {
      const pending = group.querySelectorAll('.running');
      pending.forEach(el => { el.classList.remove('running'); el.querySelector('.tool-status').textContent = '已结束'; });
      if (!group.querySelector('.error') && !group.querySelector('.tool-call[open]')) group.open = false;
    });
  }
  _finalizeStream(finalMessage) {
    if (this._currentStreamEl) {
      const text = finalMessage?.text || this._currentStreamText || '';
      this._currentStreamEl.querySelector('.stream-text').innerHTML = renderMarkdown(text);
      if (text) this._copyButton(this._currentStreamEl, text);
    }
    this._finishTools(); this._currentStreamEl = null; this._currentStreamText = ''; this._currentToolCalls = {};
    this.state.streaming = false; this._updateButtons(); this._scrollToBottom();
  }
  _finalizeWithError(error) { this._notice(error || '任务失败，请重试。'); this._finalizeStream(); }
  _updateButtons() {
    this.sendBtn.style.display = this.state.streaming ? 'none' : 'flex'; this.stopBtn.style.display = this.state.streaming ? 'flex' : 'none';
    this.sendBtn.disabled = !this.input.value.trim() || !this.ws.connected || this.creating || this.loading || this.busy.size > 0 || this.state.modelChanging || this.state.modelInfoLoading;
    this.stopBtn.disabled = !this.ws.connected;
    document.getElementById('model-select').disabled = !!(this.state.streaming || this.state.modelChanging || this.state.modelInfoLoading || this.creating || !this.ws.connected || !this.state.models?.length);
    this.status.textContent = this.state.streaming ? '正在处理' : this.ws.connected ? '准备就绪' : '等待连接';
  }
  _resizeInput() { this.input.style.height = 'auto'; this.input.style.height = Math.min(this.input.scrollHeight, 200) + 'px'; }
  _notice(text) { this.notice.textContent = text; this.notice.hidden = !text; }
  _scrollToBottom() { if (this.follow) requestAnimationFrame(() => { this.messageList.scrollTop = this.messageList.scrollHeight; this.jump.hidden = true; }); }
}
