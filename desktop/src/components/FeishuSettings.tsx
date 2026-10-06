import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { apiRequest, useStore } from "../store";
import { copyText } from "../client/clipboard";
import {
  autostartLabel, FeishuSettingsController, pairingIsValid, serviceStateLabel,
} from "../client/feishu-settings";
import { Icon } from "./Icon";

export function FeishuSettings() {
  const profileId = useStore((s) => s.selectedProfile);
  const profile = useStore((s) => s.profiles.find((p) => p.id === profileId));
  const connected = useStore((s) => s.connected);
  const controller = useMemo(() => new FeishuSettingsController(apiRequest, copyText), [profileId, connected]);
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    controller.activate(connected);
    if (connected && !document.hidden) void controller.refresh();
    const refresh = () => {
      if (!document.hidden && connected) void controller.refresh(true);
    };
    const timer = window.setInterval(refresh, 5000);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", refresh);
      controller.dispose();
    };
  }, [controller, profileId, connected]);
  useEffect(() => {
    if (!state.pairing) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [state.pairing]);

  const status = state.status;
  const disabled = !connected || state.saving || state.pairingLoading;
  const validPairing = pairingIsValid(state.pairing, now);
  return (
    <div className="settings-section feishu-settings">
      <div className="feishu-connection-summary">
        <span className="feishu-host-label">配置所在主机</span>
        <strong>{profile?.name || "当前运行主机"}</strong>
        <p>配置与配对均针对这台主机。启用飞书后，消息由这里的 Agent 处理。</p>
        <button className="btn" disabled={!connected || state.loading || state.saving}
          onClick={() => void controller.refresh()}>
          <Icon name="refresh" size={14} />{state.loading ? "正在刷新…" : "刷新状态"}
        </button>
      </div>

      {!connected ? (
        <div className="feishu-empty"><h2>先连接运行主机</h2>
          <p>连接后才能读取该主机的飞书配置与配对状态。</p>
          <button className="btn" onClick={() => useStore.getState().openSettings(true, "connections")}>
            选择运行主机
          </button>
        </div>
      ) : !status ? (
        <div className="feishu-empty" role="status">
          <p>{state.loading ? "正在读取飞书配置…" : "飞书配置暂不可用。"}</p>
          {!state.loading && <button className="btn" onClick={() => void controller.refresh()}>重试</button>}
        </div>
      ) : (
        <>
          {/* 扫码绑定区域：快捷零门槛 */}
          <section className="feishu-section" aria-labelledby="feishu-qr-title">
            <h2 id="feishu-qr-title">快速绑定（扫码登录）</h2>
            <p className="feishu-hint">使用移动端飞书 App 扫描二维码，即可自动创建并绑定机器人，免手动申请 App ID 与事件回调。</p>
            {status.app_id && (
              <p style={{ color: "var(--color-text-secondary, #888)", fontSize: "13px" }}>
                当前已配置机器人 App ID: <code>{status.app_id}</code>
              </p>
            )}
            <div style={{ marginTop: "12px", display: "flex", gap: "10px", alignItems: "center" }}>
              {!state.qr && (
                <button
                  type="button"
                  className="btn primary"
                  disabled={disabled || state.qrLoading || state.qrPolling}
                  onClick={() => void controller.startQR()}
                >
                  <Icon name="sparkle" size={14} />
                  {state.qrLoading ? "正在生成二维码…" : status.app_id ? "重新扫码绑定" : "扫码绑定飞书机器人"}
                </button>
              )}
              {state.qr && (
                <button type="button" className="btn" onClick={() => controller.hideQR()}>
                  取消扫码
                </button>
              )}
            </div>

            {state.qr && (
              <div className="feishu-pairing" style={{ marginTop: "16px" }}>
                <p><strong>请使用飞书手机客户端扫描下方链接完成授权：</strong></p>
                <div style={{ margin: "12px 0" }}>
                  <a
                    href={state.qr.qr_url}
                    target="_blank"
                    rel="noreferrer"
                    className="btn"
                    style={{ wordBreak: "break-all", display: "inline-block", maxWidth: "100%" }}
                  >
                    🔗 点击在浏览器中打开授权链接
                  </a>
                </div>
                <div className="feishu-pair-command">
                  <input readOnly value={state.qr.qr_url} aria-label="飞书授权链接" onFocus={(e) => e.target.select()} />
                  <button
                    type="button"
                    className="btn"
                    onClick={() => {
                      navigator.clipboard?.writeText(state.qr!.qr_url);
                    }}
                  >
                    复制链接
                  </button>
                </div>
                <p className="feishu-hint" style={{ marginTop: "8px" }}>
                  {state.qrPolling ? "⏳ 等待扫码确认中（授权完成后将自动生效）…" : "授权就绪"}
                </p>
              </div>
            )}
          </section>

          {/* 远程 / Systemd 托管状态 */}
          {status.managed && (
            <section className="feishu-section" aria-labelledby="feishu-runtime-title">
              <h2 id="feishu-runtime-title">服务状态</h2>
              <div className="feishu-status-grid">
                {[ ["core", "对话服务"], ["bridge", "飞书桥接"] ].map(([key, title]) => (
                  <div className="feishu-status-item" key={key}>
                    <span>{title}</span>
                    <strong>{serviceStateLabel(status.services?.[key]?.state)}</strong>
                    <small>开机启动 · {autostartLabel(status.services?.[key]?.autostart)}</small>
                  </div>
                ))}
                <div className="feishu-status-item"><span>无需登录启动</span>
                  <strong>{status.linger === "yes" ? "已启用" : status.linger === "no" ? "未启用" : "状态未知"}</strong>
                  <small>主机用户服务的 linger 状态</small>
                </div>
              </div>
              <p className="feishu-hint">进程运行不等于飞书长连接已就绪。机器人无回复时，请检查服务日志、应用发布和事件权限。</p>
            </section>
          )}

          {/* 手动自建应用凭据 */}
          {status.managed && (
            <form className="feishu-section" onSubmit={(event) => {
              event.preventDefault(); void controller.save();
            }}>
              <h2>自建应用凭据（高级）</h2>
              <p className="feishu-hint">使用飞书开放平台的自建企业应用，并启用机器人与长连接事件订阅。</p>
              <label className="feishu-field">App ID
                <input value={status.app_id || state.appId} readOnly={!!status.app_id} disabled={disabled}
                  onChange={(event) => controller.setAppId(event.target.value)}
                  placeholder="cli_…" autoComplete="off" spellCheck={false} maxLength={256} required />
              </label>
              <label className="feishu-field">App Secret
                <input type="password" value={state.secret} disabled={disabled}
                  onChange={(event) => controller.setSecret(event.target.value)}
                  placeholder={status.secret_configured ? "已保存，留空保留现有密钥" : "填写应用密钥"}
                  autoComplete="new-password" maxLength={256} required={!status.secret_configured} />
              </label>
              <p className="feishu-hint">密钥只在当前服务主机保存，不会回显。首次配置后 App ID 固定，更换机器人需要重新配置使用者绑定。</p>
              <div className="feishu-actions">
                <button type="submit" className="btn primary" disabled={disabled}>
                  {state.saving ? "正在保存并重启…" : "保存并重启桥接"}
                </button>
                <span>仅重启飞书桥接，对话服务保持运行。</span>
              </div>
            </form>
          )}

          {/* 使用者配对 */}
          {status.managed && (
            <section className="feishu-section" aria-labelledby="feishu-owner-title">
              <h2 id="feishu-owner-title">使用者配对</h2>
              <p>{status.paired ? "已绑定使用者" : status.pairing_unavailable ? "配对状态暂不可用" : "尚未配对"}</p>
              <p className="feishu-hint">{status.paired
                ? "只有绑定账号的消息和卡片操作会被接受。"
                : "配对前不会执行开发指令。启动桥接后，用有效指令私聊当前机器人完成绑定。"}</p>
              {!status.paired && !status.pairing_unavailable && (
                <button className="btn" disabled={disabled} onClick={() => void controller.showPairing()}>
                  {state.pairingLoading ? "正在获取…" : "查看私聊配对指令"}
                </button>
              )}
              {state.pairing && (
                <div className="feishu-pairing">
                  <label className="feishu-field">私聊机器人发送以下指令
                    <div className="feishu-pair-command">
                      <input readOnly value={state.pairing.command} aria-label="私聊配对指令" spellCheck={false}
                        autoComplete="off" onFocus={(event) => event.target.select()} />
                      <button className="btn" disabled={!connected || !validPairing || state.copying}
                        onClick={() => void controller.copyPairing()}>{state.copying ? "正在复制…" : "复制"}</button>
                    </div>
                  </label>
                  <p className="feishu-hint">{validPairing
                    ? `有效至 ${new Date(state.pairing.expires).toLocaleString()}，仅可使用一次。请勿发送到群聊。`
                    : "配对码已过期。保存配置并重启桥接后，再获取新的指令。"}</p>
                  <button className="btn" onClick={() => controller.hidePairing()}>隐藏指令</button>
                </div>
              )}
            </section>
          )}
        </>
      )}
      {state.error && <p className="feishu-error" role="alert">{state.error}</p>}
      {state.notice && <p className="feishu-notice" role="status" aria-live="polite">{state.notice}</p>}
    </div>
  );
}
