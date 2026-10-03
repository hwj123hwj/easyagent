import { useEffect, useRef, useState } from "react";
import { isActiveRun } from "../client/protocol";
import { useStore } from "../store";
import type { ProviderCheckResult, ProviderConfig, ProviderConfigInput } from "../types";
import { Icon } from "./Icon";

export function ModelSettings() {
  const selected = useStore((s) => s.selectedProfile);
  const profile = useStore((s) => s.profiles.find((p) => p.id === s.selectedProfile));
  const currentModel = useStore((s) => s.currentModel);
  const modelSource = useStore((s) => s.modelSource);
  const modelsNotice = useStore((s) => s.modelsNotice);
  const running = useStore((s) => Object.values(s.sessions).some((v) => isActiveRun(v.run)));
  const nativeLocal = !!window.piAPI && profile?.kind === "local";
  const [config, setConfig] = useState<ProviderConfig | null>(null);
  const [mode, setMode] = useState<ProviderConfig["mode"]>("inherit");
  const [provider, setProvider] = useState<"openai" | "anthropic">("openai");
  const [baseUrl, setBaseUrl] = useState("");
  const [model, setModel] = useState("");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [result, setResult] = useState<ProviderCheckResult | null>(null);
  const context = useRef(selected);
  context.current = selected;

  useEffect(() => {
    let cancelled = false;
    setConfig(null);
    setKey("");
    setNotice("");
    setResult(null);
    if (nativeLocal) {
      void window.piAPI!.providerConfig().then((value) => {
        if (cancelled) return;
        setConfig(value);
        setMode(value.mode);
        setProvider(value.provider || "openai");
        setBaseUrl(value.baseUrl || "");
        setModel(value.model || "");
      }).catch(() => {
        if (!cancelled) setNotice("模型配置读取失败，请重新连接此 Mac 后重试。");
      });
    }
    return () => { cancelled = true; };
  }, [selected, nativeLocal]);

  const input = (): ProviderConfigInput => mode === "inherit" ? { mode } : {
    mode, provider, baseUrl: baseUrl.trim(), model: model.trim(), apiKey: key || undefined,
  };
  const incomplete = mode === "override" && (!baseUrl.trim() || !model.trim());

  async function check() {
    const issued = selected;
    setBusy(true);
    setNotice("");
    setResult(null);
    try {
      const value = await window.piAPI!.checkProviderConfig(input());
      if (context.current === issued) setResult(value);
    } catch {
      if (context.current === issued) setNotice("连接检测失败，请检查地址与凭据后重试。");
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    const issued = selected;
    setBusy(true);
    setNotice("");
    try {
      const value = await window.piAPI!.saveProviderConfig(input());
      if (context.current !== issued) return;
      setConfig(value);
      setKey("");
      setResult(null);
      await useStore.getState().connectProfile(issued);
      if (context.current === issued) setNotice("模型配置已应用，本地 Agent 已重新连接。会话历史已保留，重新打开的会话和新会话使用更新后的模型配置。");
    } catch (error) {
      if (context.current === issued) setNotice(error instanceof Error ? error.message : "模型配置保存失败，请重试。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-section model-settings">
      <div className="settings-section-title">
        <div>
          <h2>当前模型</h2>
          <p>{profile?.name || "当前 Agent"} · 当前模型：{currentModel || "尚未配置"}</p>
        </div>
        <button className="btn" disabled={busy} onClick={() => void useStore.getState().refreshModels()}>
          <Icon name="refresh" size={14} />刷新模型
        </button>
      </div>
      {modelsNotice && <p className="model-notice" role="status">{modelsNotice}</p>}
      {modelSource === "configured" && <p className="settings-note">当前仅显示配置中的模型，尚未从服务获取模型目录。模型目录检测不代表推理验证。</p>}
      {!nativeLocal ? (
        <div className="settings-empty">
          <h3>模型配置属于服务主机</h3>
          <p>{window.piAPI ? "要配置此 Mac 的模型，请先在运行主机中选择「此 Mac」。远程模型需在对应主机的 EasyAgent 配置中设置。" : "网页版沿用所连接服务的模型配置。请在桌面客户端配置本地模型，或在服务主机更新 EasyAgent 配置。"}</p>
          <button className="btn" onClick={() => useStore.getState().openSettings(true, "connections")}>选择运行主机</button>
        </div>
      ) : (
        <>
          <fieldset className="model-config-mode" disabled={busy || !config}>
            <legend>本地模型来源</legend>
            <label><input type="radio" name="provider-mode" checked={mode === "inherit"} onChange={() => { setMode("inherit"); setResult(null); }} />沿用命令行配置</label>
            <label><input type="radio" name="provider-mode" checked={mode === "override"} onChange={() => { setMode("override"); setResult(null); }} />在桌面客户端配置</label>
          </fieldset>
          {mode === "inherit" ? (
            <p className="settings-note">沿用 easyagent.yaml、个人配置文件和 EA_* 环境变量。若修改这些配置，点击应用即可重启应用管理的本地 Agent。</p>
          ) : (
            <fieldset className="model-config-fields" disabled={busy || !config}>
              <div className="settings-fields">
                <label>模型服务
                  <select value={provider} onChange={(e) => { setProvider(e.target.value as "openai" | "anthropic"); setKey(""); setResult(null); }}>
                    <option value="openai">OpenAI 兼容服务 / 网关</option>
                    <option value="anthropic">Anthropic</option>
                  </select>
                </label>
                <label>服务地址
                  <input value={baseUrl} onChange={(e) => { setBaseUrl(e.target.value); setResult(null); }} placeholder={provider === "openai" ? "http://localhost:4001" : "https://api.anthropic.com"} spellCheck={false} autoComplete="off" />
                </label>
                <label>默认模型
                  <input value={model} onChange={(e) => setModel(e.target.value)} placeholder="填写服务中的模型 ID" spellCheck={false} autoComplete="off" />
                </label>
                <label>API Key
                  <input type="password" value={key} onChange={(e) => { setKey(e.target.value); setResult(null); }} placeholder={config?.hasKey && provider === config.provider ? "已保存；留空保留现有密钥" : "填写模型服务的密钥"} autoComplete="new-password" />
                </label>
              </div>
              <p className="settings-note">密钥由系统安全存储加密，保存在此 Mac。此处填写模型服务的密钥，连接远程 Agent 的 API 令牌在「运行主机」设置。</p>
            </fieldset>
          )}
          <div className="model-config-actions">
            <button className="btn" disabled={busy || !config || incomplete} onClick={() => void check()}><Icon name="link" size={14} />{busy ? "正在处理…" : "检测连接"}</button>
            <button className="btn primary" disabled={busy || !config || incomplete || running} onClick={() => void save()}>应用并重新连接</button>
          </div>
          {running && <p className="settings-note" role="status">当前有任务正在执行，完成或停止任务后即可应用配置。</p>}
          {result && <div className={"model-check-result " + (result.ok ? "success" : "failure")} role="status">
            <p><Icon name={result.ok ? "circle-check" : "alert-circle"} size={16} />{result.message}</p>
            {result.models.length > 0 && <div className="detected-models" aria-label="检测到的模型">
              {result.models.map((m) => <button className="btn" key={m.id} disabled={busy || mode !== "override"} title={m.id} onClick={() => setModel(m.id)}>{m.name || m.id}{model === m.id && <Icon name="check" size={14} />}</button>)}
            </div>}
          </div>}
        </>
      )}
      {notice && <p className="settings-notice" role="status">{notice}</p>}
    </section>
  );
}
