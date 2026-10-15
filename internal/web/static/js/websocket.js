// WebSocket client with auto-reconnect and event dispatch

import { wsURL } from './api.js';

export class PiWebSocket {
  constructor(baseUrl) {
    this.baseUrl = baseUrl;
    this.ws = null;
    this.handlers = {};
    this.reconnectAttempts = 0;
    this.maxReconnectAttempts = 20;
    this.baseDelay = 1000;
    this.maxDelay = 30000;
    this.connected = false;
    this.connecting = false;
    this.subscriptions = new Set();
    this.sequences = new Map(); this.runIds = new Map();
    this.stopped = false; this.reconnectTimer = null;
    this.onStatusChange = null; // callback(connected)
  }

  connect() {
    this.stopped = false;
    clearTimeout(this.reconnectTimer);
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }

    const wsUrl = wsURL(this.baseUrl, '/ws');
    this.connecting = true;
    this._notifyStatus();

    try {
      this.ws = new WebSocket(wsUrl);
    } catch (e) {
      console.error('WebSocket connection failed:', e);
      this._scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      console.log('WebSocket connected');
      this.connected = true;
      this.connecting = false;
      this.reconnectAttempts = 0;
      this._notifyStatus();
      this._dispatch('open');
      for (const id of this.subscriptions) this.subscribe(id);
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        this._receive(data);
      } catch (e) {
        console.error('Failed to parse WebSocket message:', e);
      }
    };

    this.ws.onclose = () => {
      console.log('WebSocket closed');
      this.connected = false;
      this.connecting = false;
      this._notifyStatus();
      this._dispatch('close');
      this._scheduleReconnect();
    };

    this.ws.onerror = (e) => {
      console.error('WebSocket error:', e);
    };
  }

  send(msg) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
      return true;
    } else {
      return false;
    }
  }

  sendPrompt(sessionId, prompt, requestId) {
    return this.send({ type: 'prompt', session_id: sessionId, prompt, request_id: requestId });
  }

  subscribe(id, reset = false) {
    if (!id) return false;
    this.subscriptions.add(id);
    return this.send({type:'subscribe', session_id:id, after_seq:reset ? 0 : this.sequences.get(id) || 0, run_id:this.runIds.get(id)});
  }
  unsubscribe(id) { this.subscriptions.delete(id); this.send({type:'unsubscribe',session_id:id}); }
  confirm(id, confirmationId, approved) {
    return this.send({type:'confirm', session_id:id, run_id:this.runIds.get(id), confirmation_id:confirmationId, approved});
  }
  _receive(data) {
    const id = data.session_id;
    if (data.type === 'replay') {
      for (const event of data.events || []) this._receive(event);
      this.sequences.set(id, data.seq || 0);
      if (data.run) this.runIds.set(id,data.run.run_id);
      this._dispatch('replay',data); return;
    }
    if (data.type === 'snapshot') this.sequences.set(id,data.seq || 0);
    else if (data.seq) {
      const seq=this.sequences.get(id) || 0;
      if (data.seq <= seq) return;
      if (seq && data.seq !== seq+1) { this.subscribe(id); return; }
      this.sequences.set(id,data.seq);
    }
    if (data.run_id) this.runIds.set(id,data.run_id);
    this._dispatch('message',data);
    if (data.type) this._dispatch(data.type,data);
    if (data.type === 'event' && data.event?.type) this._dispatch('event:' + data.event.type,data.event,id);
  }

  sendCancel(sessionId) {
    return this.send({ type: 'cancel', session_id: sessionId, run_id:this.runIds.get(sessionId) });
  }

  sendSwitchModel(sessionId, model, provider) {
    return this.send({ type: 'switch_model', session_id: sessionId, model, provider });
  }

  sendPing() {
    return this.send({ type: 'ping' });
  }

  on(event, handler) {
    if (!this.handlers[event]) this.handlers[event] = [];
    this.handlers[event].push(handler);
  }

  off(event, handler) {
    if (!this.handlers[event]) return;
    this.handlers[event] = this.handlers[event].filter(h => h !== handler);
  }

  _dispatch(event, ...args) {
    const handlers = this.handlers[event] || [];
    handlers.forEach(h => {
      try { h(...args); } catch (e) { console.error(`Handler error for ${event}:`, e); }
    });
  }

  _notifyStatus() {
    if (this.onStatusChange) {
      this.onStatusChange(this.connected);
    }
  }

  _scheduleReconnect() {
    if (this.stopped || this.reconnectTimer) return;
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error('Max reconnect attempts reached');
      return;
    }

    const delay = Math.min(
      this.baseDelay * Math.pow(2, this.reconnectAttempts),
      this.maxDelay
    );
    this.reconnectAttempts++;
    console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
    this.reconnectTimer = setTimeout(() => { this.reconnectTimer=null; this.connect(); }, delay);
  }

  disconnect() {
    this.stopped = true; clearTimeout(this.reconnectTimer); this.reconnectTimer=null;
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.connected = false;
    this.connecting = false;
  }
}
