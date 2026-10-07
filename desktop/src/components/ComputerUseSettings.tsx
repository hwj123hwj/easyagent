import { useCallback, useEffect, useState } from "react";
import { apiRequest } from "../store";

interface ComputerSettings {
  enabled: boolean;
  provider: string;
  available: boolean;
  granted: boolean;
  missing?: string[];
  approved_apps: string[];
  lease_holder: string;
  helper_path?: string;
}

const RISK_BANNER =
  "Computer use 功能属于测试阶段，而且对模型性能要求较高，请在知晓所有风险后开启。" +
  "当前版本仅提供只读感知（截屏与读取应用状态），后续版本才会开放键鼠控制。";

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
      {action ?? (
        <div style={{ fontSize: 13, flexShrink: 0, opacity: 0.9 }}>{value}</div>
      )}
    </div>
  );
}

export function ComputerUseSettings() {
  const [settings, setSettings] = useState<ComputerSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [requesting, setRequesting] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      setSettings(await apiRequest<ComputerSettings>("GET", "/computer/settings"));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggleEnabled = async (enabled: boolean) => {
    setSaving(true);
    setError("");
    try {
      setSettings(
        await apiRequest<ComputerSettings>("POST", "/computer/settings", { enabled }),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const requestPermissions = async () => {
    setRequesting(true);
    setError("");
    try {
      await apiRequest("POST", "/computer/permissions");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRequesting(false);
      // 权限面板操作完回来，重新探测状态
      void load();
    }
  };

  if (!settings) {
    return (
      <div style={{ padding: "24px 0", fontSize: 13, opacity: 0.6 }}>
        {error ? `加载失败：${error}` : "正在读取 Computer use 状态…"}
      </div>
    );
  }

  const availabilityLabel = settings.available ? "可用" : "不可用（需要 macOS + easyagent-cua-helper）";
  const permissionLabel = !settings.available
    ? "无需额外权限或尚未返回"
    : settings.granted
      ? "已授予"
      : `缺少：${(settings.missing || []).join("、")}`;

  return (
    <div className="computer-use-settings">
      <div
        style={{
          border: "1px solid var(--warning, #b45309)",
          background: "rgba(180, 83, 9, 0.08)",
          borderRadius: 10,
          padding: "12px 16px",
          marginBottom: 16,
          fontSize: 13,
          lineHeight: 1.6,
          color: "var(--warning-text, #f59e0b)",
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
              disabled={saving}
              onClick={() => void toggleEnabled(!settings.enabled)}
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
        <StatusRow label="Provider" value={<code style={{ fontSize: 12 }}>{settings.provider}</code>} />
        <StatusRow label="可用性" value={availabilityLabel} />
        <StatusRow
          label="请求系统权限"
          description="macOS 会请求辅助功能和屏幕录制权限。"
          action={
            <button
              className="btn"
              onClick={() => void requestPermissions()}
              disabled={requesting || !settings.available}
              style={{ padding: "6px 14px", fontSize: 13, borderRadius: 8 }}
            >
              {requesting ? "打开系统设置…" : "请求系统权限"}
            </button>
          }
        />
        <StatusRow label="系统权限" value={permissionLabel} />
        <StatusRow
          label="已批准应用"
          value={settings.approved_apps.length ? settings.approved_apps.join("、") : "暂无"}
        />
        <StatusRow
          label="当前占用"
          value={settings.lease_holder ? `会话 ${settings.lease_holder.slice(0, 8)}…` : "空闲"}
          description={undefined}
        />
      </div>

      {error && (
        <div style={{ color: "var(--danger, #ef4444)", fontSize: 13, marginTop: 12 }}>{error}</div>
      )}

      <p style={{ fontSize: 12, opacity: 0.6, marginTop: 12, lineHeight: 1.6 }}>
        启用后，新建会话的模型将获得 computer 工具（只读：截屏 / 读取应用状态）。
        已存在的会话保持原有工具集，需要新建会话才会生效。
      </p>
    </div>
  );
}
