export interface FeishuStatus {
  managed: boolean;
  local_mode?: boolean;
  app_id?: string;
  secret_configured?: boolean;
  paired?: boolean;
  pairing_unavailable?: boolean;
  pairing_expires?: string;
  services?: Record<string, { state: string; autostart: string }>;
  linger?: string;
}

export interface FeishuPairing {
  command: string;
  expires: string;
}

export interface FeishuQR {
  device_code: string;
  qr_url: string;
  user_code: string;
  interval: number;
  expire_in: number;
}

interface SettingsState {
  status: FeishuStatus | null;
  appId: string;
  secret: string;
  pairing: FeishuPairing | null;
  qr: FeishuQR | null;
  qrLoading: boolean;
  qrPolling: boolean;
  loading: boolean;
  saving: boolean;
  pairingLoading: boolean;
  copying: boolean;
  error: string;
  errorSource: "read" | "action" | null;
  notice: string;
}

type Request = <T>(method: string, path: string, body?: unknown) => Promise<T>;
const emptyState = (): SettingsState => ({
  status: null, appId: "", secret: "", pairing: null,
  qr: null, qrLoading: false, qrPolling: false,
  loading: false, saving: false, pairingLoading: false, copying: false,
  error: "", errorSource: null, notice: "",
});

export function pairingIsValid(pairing: FeishuPairing | null, now = Date.now()): boolean {
  if (!pairing || !/^\/pair [A-Za-z0-9_-]+$/.test(pairing.command)) return false;
  const expiry = Date.parse(pairing.expires);
  return Number.isFinite(expiry) && now < expiry;
}

export function serviceStateLabel(state?: string): string {
  return ({ active: "进程运行中", activating: "正在启动", inactive: "已停止",
    failed: "启动失败", deactivating: "正在停止", unknown: "状态未知" } as Record<string, string>)[state || ""] || "状态未知";
}

export function autostartLabel(value?: string): string {
  return ({ enabled: "已启用", disabled: "未启用", masked: "已禁用",
    static: "由其他服务启动", "enabled-runtime": "仅本次运行启用" } as Record<string, string>)[value || ""] || "状态未知";
}

/** Per-profile transient state. Credentials and pairing codes are never persisted. */
export class FeishuSettingsController {
  private state = emptyState();
  private listeners = new Set<() => void>();
  private epoch = 0;
  private alive = false;
  private connected = false;
  private statusRevision = 0;

  constructor(private request: Request, private copy: (text: string) => Promise<void>) {}

  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  // Called when the selected profile or its connection changes, and on mount.
  activate(connected: boolean) {
    this.epoch++;
    this.statusRevision++;
    this.alive = true;
    this.connected = connected;
    this.set(emptyState());
  }

  dispose() {
    this.epoch++;
    this.alive = false;
    this.connected = false;
    // Drop sensitive values without notifying an unmounted component.
    this.state = emptyState();
  }

  setAppId(value: string) { this.set({ appId: value }); }
  setSecret(value: string) { this.set({ secret: value }); }
  hidePairing() { this.set({ pairing: null, notice: "" }); }

  private set(update: Partial<SettingsState>) {
    if (!this.alive) return;
    this.state = { ...this.state, ...update };
    this.listeners.forEach((listener) => listener());
  }
  private current(epoch: number) { return this.alive && this.connected && epoch === this.epoch; }
  private applyStatus(status: FeishuStatus) {
    this.set({ status, ...(!status.managed ? { secret: "" } : {}), ...(status.paired || status.pairing_unavailable || !status.managed
      ? { pairing: null } : {}) });
  }

  async refresh(quiet = false) {
    if (!this.alive || !this.connected || this.state.loading || this.state.saving) return;
    const epoch = this.epoch, revision = ++this.statusRevision;
    this.set({ loading: true, ...(!quiet ? { error: "", errorSource: null } : {}) });
    try {
      const status = await this.request<FeishuStatus>("GET", "/settings/feishu");
      if (!this.current(epoch) || revision !== this.statusRevision) return;
      this.applyStatus(status);
      // A healthy status read does not mean a failed save/pair/copy recovered.
      if (this.state.errorSource !== "action") this.set({ error: "", errorSource: null });
    } catch (error) {
      if (this.current(epoch) && revision === this.statusRevision && this.state.errorSource !== "action")
        this.set({ error: (error as Error).message || "无法读取飞书配置", errorSource: "read" });
    } finally {
      if (this.current(epoch) && revision === this.statusRevision) this.set({ loading: false });
    }
  }

  async save() {
    const state = this.state;
    if (!this.alive || !this.connected || !state.status?.managed || state.saving || state.pairingLoading) return;
    const appId = (state.status.app_id || state.appId).trim(), secret = state.secret.trim();
    if (!appId || (!secret && !state.status.secret_configured)) {
      this.set({ error: "首次配置请同时填写 App ID 和 App Secret", errorSource: "action" });
      return;
    }
    const epoch = this.epoch;
    this.statusRevision++;
    this.set({ saving: true, loading: false, pairing: null, error: "", errorSource: null, notice: "" });
    try {
      const status = await this.request<FeishuStatus>("PUT", "/settings/feishu", {
        app_id: appId, app_secret: secret,
      });
      if (!this.current(epoch)) return;
      this.applyStatus(status);
      this.set({ notice: "已保存并重启飞书桥接。对话服务保持运行；长连接状态需另行确认。" });
    } catch (error) {
      if (this.current(epoch)) this.set({ error: (error as Error).message || "保存失败", errorSource: "action" });
    } finally {
      if (this.current(epoch)) this.set({ saving: false, secret: "" });
    }
  }

  async showPairing() {
    if (!this.alive || !this.connected || !this.state.status?.managed || this.state.status.paired || this.state.status.pairing_unavailable || this.state.saving || this.state.pairingLoading) return;
    const epoch = this.epoch;
    this.set({ pairing: null, pairingLoading: true, error: "", errorSource: null, notice: "" });
    try {
      const pairing = await this.request<FeishuPairing>("GET", "/settings/feishu/pairing");
      if (!this.current(epoch) || !this.state.status?.managed || this.state.status.paired || this.state.status.pairing_unavailable) return;
      if (!pairingIsValid(pairing)) throw new Error("配对码已过期。保存配置并重启桥接后，再获取新的指令。");
      this.set({ pairing });
    } catch (error) {
      if (this.current(epoch)) this.set({ error: (error as Error).message || "无法获取配对指令", errorSource: "action" });
    } finally {
      if (this.current(epoch)) this.set({ pairingLoading: false });
    }
  }

  async copyPairing() {
    const pairing = this.state.pairing;
    if (!this.alive || !this.connected || this.state.copying || !pairingIsValid(pairing)) return;
    const epoch = this.epoch;
    this.set({ copying: true, error: "", errorSource: null, notice: "" });
    try {
      await this.copy(pairing!.command);
      if (this.current(epoch) && this.state.pairing === pairing)
        this.set({ notice: pairingIsValid(pairing)
          ? "已复制。请私聊当前机器人发送，不要发到群聊。"
          : "配对码已过期，请保存配置并重启桥接后，获取新的指令。" });
    } catch (error) {
      if (this.current(epoch) && this.state.pairing === pairing)
        this.set({ error: (error as Error).message || "复制失败，请选中指令后按 Ctrl/Cmd+C", errorSource: "action" });
    } finally {
      if (this.current(epoch)) this.set({ copying: false });
    }
  }

  hideQR() {
    this.set({ qr: null, qrLoading: false, qrPolling: false });
  }

  async startQR() {
    if (!this.alive || !this.connected || this.state.qrLoading || this.state.qrPolling) return;
    const epoch = this.epoch;
    this.set({ qr: null, qrLoading: true, error: "", errorSource: null, notice: "" });
    try {
      const qr = await this.request<FeishuQR>("POST", "/settings/feishu/qr/begin");
      if (!this.current(epoch)) return;
      this.set({ qr, qrLoading: false, qrPolling: true });
      this.pollQR(qr, epoch);
    } catch (error) {
      if (this.current(epoch)) {
        this.set({ error: (error as Error).message || "获取飞书二维码失败", errorSource: "action", qrLoading: false });
      }
    }
  }

  private async pollQR(qr: FeishuQR, epoch: number) {
    try {
      const res = await this.request<{ success: boolean; app_id: string; bot_name?: string }>(
        "POST",
        "/settings/feishu/qr/poll",
        {
          device_code: qr.device_code,
          interval: qr.interval || 3,
          expire_in: qr.expire_in || 120,
        }
      );
      if (!this.current(epoch)) return;
      if (res.success) {
        this.set({
          qr: null,
          qrPolling: false,
          notice: `飞书机器人授权成功！App ID: ${res.app_id}${res.bot_name ? ` (${res.bot_name})` : ""}`,
        });
        await this.refresh();
      }
    } catch (error) {
      if (this.current(epoch)) {
        this.set({
          qr: null,
          qrPolling: false,
          error: (error as Error).message || "扫码授权超时或失败",
          errorSource: "action",
        });
      }
    }
  }
}
