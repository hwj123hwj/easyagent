import { useEffect, useRef, useState } from "react";
import { apiRequest, useStore, type SessionView } from "../store";
import type { ContextSnapshot } from "../client/protocol";
import { isActiveRun } from "../client/protocol";
import { Icon } from "./Icon";

// Avoid displaying enormous inline image encodings. The authenticated API keeps
// the original payload; the UI labels omissions instead of pretending it is raw.
function inspectJSON(value: unknown) {
  return JSON.stringify(value, (key, value) => key === "data" && typeof value === "string" ? `[图片数据：${value.length} 个 Base64 字符，界面省略]` : value, 2);
}
export function ContextInspector({ view, onClose, onBusy }: { view: SessionView; onClose: () => void; onBusy: (busy: boolean) => void }) {
  const profile = useStore(state => state.selectedProfile), id = view.meta.id;
  const active = isActiveRun(view.run);
  const [snapshot, setSnapshot] = useState<ContextSnapshot>(), [error, setError] = useState("");
  const [loading, setLoading] = useState(true), [compacting, setCompacting] = useState(false);
  const [instructions, setInstructions] = useState(""), [revision, setRevision] = useState(0);
  const generation = useRef(0), heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus(); return () => onBusy(false); }, []);
  useEffect(() => {
    const current = ++generation.current;
    setSnapshot(undefined); setError(""); setLoading(!active);
    if (!active) void apiRequest<ContextSnapshot>("GET", `/sessions/${encodeURIComponent(id)}/context`)
      .then(value => { if (generation.current === current) setSnapshot(value); })
      .catch(error => { if (generation.current === current) setError((error as Error).message); })
      .finally(() => { if (generation.current === current) setLoading(false); });
    return () => { generation.current++; };
  }, [id, profile, active, revision]);
  async function compact() {
    const current = generation.current;
    setError(""); setCompacting(true); onBusy(true);
    try {
      await apiRequest("POST", `/sessions/${encodeURIComponent(id)}/compact`, { custom_instructions: instructions.trim() });
      if (generation.current !== current) return;
      await useStore.getState().refreshSessionInfo(id);
      if (generation.current === current) { setInstructions(""); setRevision(value => value + 1); }
    } catch (error) { if (generation.current === current) setError((error as Error).message); }
    finally { setCompacting(false); onBusy(false); }
  }
  return <section className="context-inspector" aria-label="上下文检查" onKeyDown={event => { if (event.key === "Escape" && !compacting) { event.stopPropagation(); onClose(); } }}>
    <header><h3 ref={heading} tabIndex={-1}>当前上下文</h3><button type="button" className="icon-btn" disabled={compacting} aria-label="关闭上下文检查" onClick={onClose}><Icon name="x" size={14} /></button></header>
    <div className="context-inspector-body" aria-busy={loading || compacting}>
      <p className="context-inspector-note">当前会话的逻辑输入，包含系统提示词、消息和工具定义。模型供应商还会转换请求格式；未发送的草稿与队列消息未计入，图片数据在此省略。</p>
      {active && <p role="status">任务正在运行，结束后自动刷新上下文。</p>}
      {loading && <p role="status">正在读取上下文…</p>}
      {error && <div className="composer-error" role="alert">{error}<button className="btn" disabled={active || compacting} onClick={() => setRevision(value => value + 1)}>重新读取</button></div>}
      {snapshot && <>
        <div className="context-inspector-metrics"><span>{snapshot.usage.model}</span><span>{snapshot.messages.length} 条消息</span><span>估算 {snapshot.usage.estimated_tokens.toLocaleString()} tokens</span><button className="btn" disabled={compacting} onClick={() => setRevision(value => value + 1)}>刷新</button></div>
        <details><summary>系统提示词 · 估算 {snapshot.usage.system.toLocaleString()} tokens</summary><pre>{snapshot.system || "（无）"}</pre></details>
        <details><summary>模型输入消息 · 估算 {snapshot.usage.messages.toLocaleString()} tokens</summary>
          {snapshot.messages.map((message, index) => <details key={index}><summary>{index + 1} · {message.role}</summary><pre>{inspectJSON(message.message)}</pre></details>)}
        </details>
        <details><summary>工具定义 · {snapshot.tools.length} 个 · 估算 {snapshot.usage.tools.toLocaleString()} tokens</summary>
          {snapshot.tools.map(tool => <details key={tool.name}><summary>{tool.name}</summary><pre>{inspectJSON(tool)}</pre></details>)}
        </details>
        <details><summary>压缩记录 · {snapshot.compactions.length} 次</summary>
          {!snapshot.compactions.length && <p>此会话还没有全量压缩记录。</p>}
          {[...snapshot.compactions].reverse().map(record => <details key={record.id}><summary>{new Date(record.timestamp).toLocaleString()} · {record.info?.trigger === "manual" ? "手动压缩" : record.info ? "自动压缩" : "历史压缩"}</summary>
            {record.info && <p>{record.info.messages_before} → {record.info.messages_after} 条消息 · 消息估算 {record.info.tokens_before.toLocaleString()} → {record.info.tokens_after.toLocaleString()} tokens</p>}
            {record.info?.instructions && <p>压缩要求：{record.info.instructions}</p>}<pre>{record.summary}</pre>
          </details>)}
        </details>
        <form onSubmit={event => { event.preventDefault(); void compact(); }}>
          <label htmlFor="compact-instructions">手动压缩要求 <span>可选</span></label>
          <textarea id="compact-instructions" rows={2} maxLength={8000} value={instructions} disabled={compacting} onChange={event => setInstructions(event.target.value)} placeholder="例如：保留接口约定、未完成的测试和下一步计划" />
          <div><p>较早消息会归纳成摘要，近期完整消息会保留。</p><button type="submit" className="btn btn-primary" disabled={compacting || active || snapshot.messages.length < 2}>{compacting ? "正在生成摘要…" : "压缩上下文"}</button></div>
        </form>
      </>}
    </div>
  </section>;
}
