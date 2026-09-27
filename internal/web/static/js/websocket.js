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
    this.onStatusChange = null; // callback(connected)
  }

  connect() {
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
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        this._dispatch('message', data);

        // Dispatch by type
        if (data.type) {
          this._dispatch(data.type, data);
        }

        // Dispatch by event type for stream events
        if (data.type === 'event' && data.event?.type) {
          this._dispatch('event:' + data.event.type, data.event, data.session_id);
        }
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

  sendPrompt(sessionId, prompt) {
    return this.send({ type: 'prompt', session_id: sessionId, prompt });
  }

  sendCancel(sessionId) {
    return this.send({ type: 'cancel', session_id: sessionId });
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
    setTimeout(() => this.connect(), delay);
  }

  disconnect() {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.connected = false;
    this.connecting = false;
  }
}
