import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { useStore, type SessionView } from "../store";
import { Icon } from "./Icon";

function ComposerPopover({ label, trigger, children, disabled = false, tone = "", status }: {
  label: string; trigger: ReactNode; children: (close: () => void) => ReactNode; disabled?: boolean; tone?: string; status?: string;
}) {
  const id = useId(), button = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 12, bottom: 12, maxHeight: 520 });
  const place = () => {
    const rect = button.current?.getBoundingClientRect();
    if (rect) setPosition({ left: Math.max(12, Math.min(rect.left, innerWidth - 344)), bottom: innerHeight - rect.top + 8, maxHeight: Math.max(80, Math.min(520, rect.top - 20)) });
  };
  useEffect(() => {
    if (!open) return;
    place();
    const selected = panel.current?.querySelector<HTMLButtonElement>('[role="option"][aria-selected="true"]') || panel.current?.querySelector<HTMLButtonElement>('[role="option"]');
    selected?.focus();
    window.addEventListener("resize", place);
    return () => window.removeEventListener("resize", place);
  }, [open]);
  useEffect(() => { if (disabled) panel.current?.hidePopover(); }, [disabled]);
  const close = () => { panel.current?.hidePopover(); button.current?.focus(); };
  return <>
    <button ref={button} type="button" className={`composer-status-trigger ${tone}`} popoverTarget={id} aria-label={status ? `${label}：${status}` : label} aria-expanded={open} disabled={disabled} onClick={place}>{trigger}</button>
    <div ref={panel} id={id} popover="auto" className="composer-status-popover" role="dialog" aria-label={label}
      onToggle={event => setOpen(event.newState === "open")} style={position}>{children(close)}</div>
  </>;
}

export function ComposerStatus({ view, disabled, onError, onStart, onInspect }: { view: SessionView; disabled: boolean; onError: (error: string) => void; onStart?: () => Promise<string>; onInspect?: () => void }) {
  const [saving, setSaving] = useState(false);
  const profile = useStore(state => state.selectedProfile);
  const active = view.run?.state === "running" || view.run?.state === "waiting_confirmation";
  const previouslyActive = useRef(active);
  useEffect(() => {
    if (previouslyActive.current && !active)
      void useStore.getState().refreshSessionInfo(view.meta.id).catch(() => {});
    previouslyActive.current = active;
  }, [active, view.meta.id, profile]);
  const mode = view.accessMode, usage = view.contextUsage;
  const percent = usage && usage.context_window > 0 ? usage.estimated_tokens / usage.context_window * 100 : undefined;
  const modeLabel = saving ? "切换中…" : mode === "full" ? "完全权限" : mode === "ask" ? "每次询问" : onStart ? "工具权限" : "权限未同步";
  const format = (n: number) => n.toLocaleString();
  return <>
    <ComposerPopover label="工具权限" status={modeLabel === "工具权限" ? "尚未创建会话" : modeLabel} disabled={disabled || saving || (!mode && !onStart)} tone={mode === "full" ? "access-full" : ""}
      trigger={<><Icon name="shield" size={14} /><span>{modeLabel}</span><Icon name="chevron-down" size={12} /></>}>
      {close => <>
        <strong>工具权限</strong>
        <div role="listbox" aria-label="权限模式" onKeyDown={event => {
          if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
          event.preventDefault();
          const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button"));
          const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
          buttons[(index + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length]?.focus();
        }}>
          {(["ask", "full"] as const).map(value => <button key={value} role="option" aria-selected={mode === value} autoFocus={mode === value}
            disabled={saving} onClick={async () => {
              close();
              if (mode === value) return;
              setSaving(true);
              try {
                const targetId = onStart ? await onStart() : view.meta.id;
                if (useStore.getState().selectedProfile !== profile) throw new Error("运行主机已切换，请重新选择权限");
                await useStore.getState().setAccessMode(targetId, value);
              }
              catch (error) { onError((error as Error).message); }
              finally { setSaving(false); }
            }}>
            <Icon name={value === "ask" ? "shield" : "power"} size={16} />
            <span><b>{value === "ask" ? "每次询问" : "完全权限"}</b><small>{value === "ask" ? "有风险的操作执行前由你确认" : "自动批准普通工具的修改与命令"}</small></span>
            {mode === value && <Icon name="check" size={14} />}
          </button>)}
        </div>
        <p>只影响当前会话。MCP 的独立批准规则及工作区访问限制仍然生效；服务重启后恢复启动配置。</p>
      </>}
    </ComposerPopover>
    <ComposerPopover label="上下文用量" status={percent === undefined ? "暂不可用" : `估算 ${percent.toFixed(1)}%`} disabled={!usage} tone={percent !== undefined && percent >= 80 ? "context-high" : ""}
      trigger={<><Icon name="activity" size={14} /><span>{percent === undefined ? "上下文" : `约 ${percent.toFixed(1)}%`}</span></>}>
      {close => usage && <>
        {onInspect && <button type="button" className="btn context-inspect-link" onClick={() => { close(); onInspect(); }}>查看上下文与压缩记录 <Icon name="chevron-right" size={12} /></button>}
        <strong>上下文用量 <span>估算</span></strong>
        <div className="context-total">{format(usage.estimated_tokens)} <span>/ {format(usage.context_window)} tokens</span></div>
        <progress max={usage.context_window} value={usage.estimated_tokens} aria-label="估算上下文占用" />
        <dl className="context-breakdown">
          {([["消息", usage.messages], ["系统提示词", usage.system], ["工具定义", usage.tools]] as const).map(([label, count]) => <div key={label}><dt>{label}</dt><dd>{format(count)}</dd></div>)}
        </dl>
        <p>按当前消息、系统提示词和工具定义估算；图片等多模态内容未计入，实际用量以模型返回为准。</p>
        {!usage.window_known && <p className="context-budget-note">模型容量尚未登记，分母为当前运行时的默认预算，并非模型官方上限。</p>}
        {!!usage.last_request?.input_tokens && <>
          <strong className="context-measurement">最近一次模型请求 · 实际</strong>
          <dl className="context-breakdown">
            <div><dt>输入</dt><dd>{format(usage.last_request.input_tokens)}</dd></div>
            <div><dt>输出</dt><dd>{format(usage.last_request.output_tokens)}</dd></div>
            {usage.last_request.cached_input_tokens !== undefined && <div><dt>输入缓存命中</dt><dd>{(usage.last_request.cached_input_tokens / usage.last_request.input_tokens * 100).toFixed(1)}%</dd></div>}
          </dl>
        </>}
      </>}
    </ComposerPopover>
  </>;
}
