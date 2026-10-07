import { useCallback, useEffect, useRef, useState } from "react";
import { apiRequest, useStore } from "../store";

interface ComputerSettings {
  enabled: boolean;
  provider: string;
  available: boolean;
  granted: boolean;
  missing?: string[];
  approval_policy: "ask" | "auto";
  approved_apps: string[];
  lease_holder: string;
  helper_path?: string;
}

const RISK_BANNER =
  "Computer use 功能属于测试阶段，而且对模型性能要求较高，请在知晓所有风险后开启。" +
  "当前为实验性 helper 接口，安装包暂未包含原生 helper。只有主机安装兼容的 easyagent-cua-helper 后，才可截图、读取应用状态与控制键鼠。";

function StatusRow({
  label,
  description,
  value,
  action,
}: {
  label: string;
  description?: string;
  value?: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 16,
        padding: "12px 16px",
        borderBottom: "1px solid var(--border, #2a2a2a)",
      }}
    >
      <div style={{ minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 500 }}>{label}</div>
        {description && (
          <div style={{ fontSize: 12, opacity: 0.65, marginTop: 2 }}>{description}</div>
        )}
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 12, maxWidth: "55%" }}>
        {value && <div style={{ fontSize: 13, overflowWrap: "anywhere" }}>{value}</div>}
        {action}
      </div>
    </div>
  );
}

export function ComputerUseSettings() {
  const profileId = useStore((s) => s.selectedProfile);
  const epoch = useRef(0);
  const [settings, setSettings] = useState<ComputerSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [requesting, setRequesting] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    const current = epoch.current;
    try {
      const next = await apiRequest<ComputerSettings>("GET", "/computer/settings");
      if (current !== epoch.current) return;
      setSettings(next);
      setError("");
    } catch (err) {
      if (current === epoch.current) setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    epoch.current++;
    setSettings(null);
    setError("");
    setSaving(false);
    setRequesting(false);
    void load();
    return () => { epoch.current++; };
  }, [load, profileId]);

  const patch = async (body: Record<string, unknown>) => {
    if (saving || requesting) return;
    const current = ++epoch.current;
    setSaving(true);
    setError("");
    try {
      const next = await apiRequest<ComputerSettings>("POST", "/computer/settings", body);
      if (current === epoch.current) setSettings(next);
    } catch (err) {
      if (current === epoch.current) setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (current === epoch.current) setSaving(false);
    }
  };

  const requestPermissions = async () => {
    if (saving || requesting) return;
    const current = ++epoch.current;
    setRequesting(true);
    setError("");
    try {
      await apiRequest("POST", "/computer/permissions");
    } catch (err) {
      if (current === epoch.current) setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (current === epoch.current) {
        setRequesting(false);
        void load();
      }
    }
  };

  if (!settings) {
    return (
      <div style={{ padding: "24px 0", fontSize: 13, opacity: 0.6 }}>
        {error ? `加载失败：${error}` : "正在读取 Computer use 状态…"}
        {error && <button className="btn" onClick={() => void load()} style={{ marginLeft: 12 }}>重试</button>}
      </div>
    );
  }

  const availabilityLabel = settings.available ? "可用" : "不可用（需要 macOS + easyagent-cua-helper）";
  const permissionLabel = !settings.available
    ? "helper 未安装，无法检查权限"
    : settings.granted
      ? "已授予"
      : `缺少：${(settings.missing || []).join("、")}`;

  return (
    <div className="computer-use-settings">
      <button className="btn" disabled={saving || requesting} onClick={() => void load()} style={{ marginBottom: 16 }}>刷新状态</button>
      <div
        style={{
          border: "1px solid var(--amber)",
          background: "rgba(180, 83, 9, 0.08)",
          borderRadius: 10,
          padding: "12px 16px",
          marginBottom: 16,
          fontSize: 13,
          lineHeight: 1.6,
          color: "var(--amber)",
        }}
      >
        {RISK_BANNER}
      </div>

      <div style={{ border: "1px solid var(--border, #2a2a2a)", borderRadius: 10, overflow: "hidden" }}>
        <StatusRow
          label="启用电脑控制"
          description="默认关闭。关闭时新会话不会暴露电脑控制工具。"
          action={
            <button
              role="switch"
              aria-checked={settings.enabled}
              disabled={saving || requesting}
              onClick={() => void patch({ enabled: !settings.enabled })}
              style={{
                width: 40,
                height: 22,
                borderRadius: 11,
                border: "none",
                cursor: saving ? "wait" : "pointer",
                background: settings.enabled ? "var(--accent, #3b82f6)" : "var(--bg-hover, #3a3a3a)",
                position: "relative",
                transition: "background 0.15s ease",
              }}
            >
              <span
                style={{
                  position: "absolute",
                  top: 2,
                  left: settings.enabled ? 20 : 2,
                  width: 18,
                  height: 18,
                  borderRadius: "50%",
                  background: "#fff",
                  transition: "left 0.15s ease",
                }}
              />
            </button>
          }
        />
        <StatusRow
          label="审批策略"
          description="逐批询问：每次键鼠操作都需要你批准。自动执行：普通操作自动执行，退出、注销、锁屏、强制退出组合键仍会询问。"
          action={
            <div style={{ display: "flex", gap: 6 }}>
              {(["ask", "auto"] as const).map((policy) => (
                <button
                  key={policy}
                  disabled={saving || requesting}
                  onClick={() => void patch({ approval_policy: policy })}
                  style={{
                    padding: "5px 12px",
                    fontSize: 12,
                    borderRadius: 7,
                    border: "1px solid var(--border, #2a2a2a)",
                    cursor: "pointer",
                    background:
                      settings.approval_policy === policy
                        ? "var(--accent, #3b82f6)"
                        : "transparent",
                    color: settings.approval_policy === policy ? "var(--accent-text)" : "inherit",
                  }}
                >
                  {policy === "ask" ? "逐批询问" : "自动执行"}
                </button>
              ))}
            </div>
          }
        />
        <StatusRow label="Provider" value={<code style={{ fontSize: 12 }}>{settings.provider}</code>} />
        <StatusRow label="可用性" value={availabilityLabel} />
        <StatusRow
          label="请求系统权限"
          description="macOS 会请求辅助功能和屏幕录制权限。"
          action={
            <button
              className="btn"
              onClick={() => void requestPermissions()}
              disabled={saving || requesting || !settings.available}
              style={{ padding: "6px 14px", fontSize: 13, borderRadius: 8 }}
            >
              {requesting ? "打开系统设置…" : "请求系统权限"}
            </button>
          }
        />
        <StatusRow label="系统权限" value={permissionLabel} />
        <StatusRow
          label="已控制应用（审计）"
          description="实际被模型执行过键鼠操作的应用（自动记录，用于审计）。"
          value={settings.approved_apps.length ? settings.approved_apps.join("、") : "暂无"}
          action={
            settings.approved_apps.length ? (
              <button
                className="btn"
                disabled={saving || requesting}
                onClick={() => void patch({ clear_approved_apps: true })}
                style={{ padding: "4px 10px", fontSize: 12, borderRadius: 7 }}
              >
                清空
              </button>
            ) : undefined
          }
        />
        <StatusRow
          label="当前占用"
          value={settings.lease_holder ? `会话 ${settings.lease_holder.slice(0, 8)}…` : "空闲"}
          description={undefined}
        />
      </div>

      {error && (
        <div style={{ color: "var(--red)", fontSize: 13, marginTop: 12 }}>{error}</div>
      )}

      <p style={{ fontSize: 12, opacity: 0.6, marginTop: 12, lineHeight: 1.6 }}>
        启用并安装兼容 helper 后，新建会话的模型可使用 computer 工具（截屏 / 读取应用状态 / 键鼠控制）。
        键鼠操作按审批策略把关：默认逐批询问，每批最多 20 个动作；退出、注销、锁屏、强制退出组合键在任何策略下都会询问。
        已加载会话保持原有工具集与策略，新建或重新加载会话后生效。
      </p>
    </div>
  );
}
