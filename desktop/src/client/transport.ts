import type { Envelope, PromptInputs } from "./protocol";
type Handler = (value: any) => void;
export class AgentTransport {
  private socket: WebSocket | null = null;
  private listeners = new Map<string, Set<Handler>>();
  private pending = new Map<
    string,
    {
      resolve: (value: Envelope) => void;
      reject: (error: Error) => void;
      timer: ReturnType<typeof setTimeout>;
      prompt?: string;
    }
  >();
  private removeIPC?: () => void;
  private reconnect?: ReturnType<typeof setTimeout>;
  private attempts = 0;
  private stopped = true;
  connected = false;
  url = "";
  token = "";
  on(event: string, handler: Handler): () => void {
    const set = this.listeners.get(event) || new Set();
    set.add(handler);
    this.listeners.set(event, set);
    return () => {
      set.delete(handler);
    };
  }
  private emit(event: string, value: unknown) {
    this.listeners.get(event)?.forEach((handler) => handler(value));
  }
  private route(message: Envelope) {
    const pending = message.request_id
      ? this.pending.get(message.request_id)
      : undefined;
    if (message.type === "accepted" && pending)
      message = { ...message, prompt: message.prompt || pending.prompt };
    this.emit("message", message);
    if (pending && ["accepted", "error"].includes(message.type)) {
      clearTimeout(pending.timer);
      this.pending.delete(message.request_id!);
      message.type === "accepted"
        ? pending.resolve(message)
        : pending.reject(
            new Error(message.message || message.error || "消息未被接受"),
          );
    }
  }
  private status(state: string, message?: string) {
    this.connected = state === "connected";
    this.emit("status", { state, message });
    if (this.connected) {
      this.attempts = 0;
      this.emit("open", undefined);
    } else if (state === "disconnected" || state === "error") {
      for (const value of this.pending.values()) {
        clearTimeout(value.timer);
        value.reject(new Error("连接已断开，草稿已保留；重连后可重试"));
      }
      this.pending.clear();
      if (!this.stopped && !this.reconnect)
        this.reconnect = setTimeout(
          () => {
            this.reconnect = undefined;
            void this.open();
          },
          Math.min(1000 * 2 ** this.attempts++, 15000),
        );
    }
  }
  async connect(url: string, token = "") {
    await this.disconnect();
    this.url = url;
    this.token = token;
    this.stopped = false;
    if (window.piAPI)
      this.removeIPC = window.piAPI.onAgentEvent((value) => {
        if (value.type === "message") this.route(value.data);
        else if (value.type === "transport")
          this.status(value.state, value.message);
        else this.emit(value.type, value);
      });
    await this.open();
  }
  private async open() {
    if (this.stopped) return;
    this.status("connecting");
    try {
      if (window.piAPI) {
        await window.piAPI.connect();
        return;
      }
      const url =
        this.url.replace(/^http/, "ws") +
        "/ws" +
        (this.token ? "?token=" + encodeURIComponent(this.token) : "");
      if (this.socket) {
        this.socket.onopen = null;
        this.socket.onmessage = null;
        this.socket.onclose = null;
        this.socket.onerror = null;
        this.socket.close();
      }
      const socket = new WebSocket(url);
      this.socket = socket;
      socket.onopen = () => {
        if (this.socket === socket && !this.stopped) this.status("connected");
      };
      socket.onmessage = (event) => {
        if (this.socket !== socket || this.stopped) return;
        try {
          this.route(JSON.parse(event.data));
        } catch {
          this.emit("notice", { message: "收到无法解析的服务消息" });
        }
      };
      socket.onclose = () => {
        if (this.socket === socket && !this.stopped)
          this.status("disconnected");
      };
      socket.onerror = () => {
        if (this.socket === socket && !this.stopped)
          this.status("error", "连接失败，请检查服务地址和认证");
      };
    } catch (error) {
      this.status("error", error instanceof Error ? error.message : "连接失败");
    }
  }
  async send(message: object): Promise<boolean> {
    if (!this.connected) return false;
    if (window.piAPI) return window.piAPI.send(message);
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return false;
    this.socket.send(JSON.stringify(message));
    return true;
  }
  async prompt(
    session: string,
    prompt: string,
    requestId: string,
    inputs?: PromptInputs,
  ): Promise<Envelope> {
    if (!this.connected) throw new Error("尚未连接到服务，草稿已保留");
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(new Error("服务未确认接收，草稿已保留；请重连后查看任务状态"));
      }, 12000);
      this.pending.set(requestId, { resolve, reject, timer, prompt });
      void this.send({
        type: "prompt",
        session_id: session,
        prompt,
        request_id: requestId,
        inputs,
      })
        .then((sent) => {
          if (!sent) {
            clearTimeout(timer);
            this.pending.delete(requestId);
            reject(new Error("消息没有发送，草稿已保留"));
          }
        })
        .catch((error) => {
          clearTimeout(timer);
          this.pending.delete(requestId);
          reject(error);
        });
    });
  }
  async disconnect() {
    this.stopped = true;
    clearTimeout(this.reconnect);
    this.reconnect = undefined;
    this.removeIPC?.();
    this.removeIPC = undefined;
    if (this.socket) {
      this.socket.onopen = null;
      this.socket.onmessage = null;
      this.socket.onerror = null;
      this.socket.onclose = null;
      this.socket.close();
      this.socket = null;
    }
    if (window.piAPI) await window.piAPI.disconnect();
    this.connected = false;
    for (const value of this.pending.values()) {
      clearTimeout(value.timer);
      value.reject(new Error("连接已切换，草稿已保留"));
    }
    this.pending.clear();
  }
}
