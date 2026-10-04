import { useEffect, useRef, useState } from "react";
import { useStore, type SessionView as SV, type ViewDensity } from "../store";
import { ChatPane } from "./panes/ChatPane";
import { PromptBar } from "./PromptBar";
import { Icon } from "./Icon";
import { useT } from "../i18n/useT";
import { BottomTerminal } from "./workspace/BottomTerminal";
import { Resizer } from "./workspace/Resizer";
import { copyText } from "../client/clipboard";
import { isActiveRun } from "../client/protocol";

export function SessionView() {
  const activeId = useStore((s) => s.activeSessionId);
  const profile = useStore((s) => s.selectedProfile);
  const view = useStore((s) => (activeId ? s.sessions[activeId] : undefined));
  const bottomOpen = useStore((s) => s.workspace.bottomOpen);
  const bottomHeight = useStore((s) => s.workspace.bottomHeight);
  const setWorkspaceSize = useStore((s) => s.setWorkspaceSize);
  const t = useT();

  if (!view) {
    return <EmptyState key={profile} />;
  }

  const meta = view.meta;
  return (
    <main className="main session-main">
      <div className="toolbar">
        <div className="toolbar-heading">
          <span className="toolbar-title" title={meta.title}>{meta.title}</span>
          {meta.cwd && (
            <span className="toolbar-cwd" title={meta.cwd}>
              <Icon name="folder" size={12} />
              <span>{baseName(meta.cwd)}</span>
            </span>
          )}
        </div>
        <span className="toolbar-status" role="status">
          <span className={`status-dot ${meta.status}`} />
          {isActiveRun(view.run) && view.phase === "responding"
            ? t("chat.aiResponding")
            : isActiveRun(view.run) && view.phase === "tool"
              ? t("tool.status.in_progress")
              : isActiveRun(view.run) && view.phase === "approval"
                ? t("status.needs_approval")
                : t(`status.${meta.status}` as any)}
        </span>
        <SessionActions view={view} cwd={meta.cwd} />
      </div>

      <div className={`workspace ${bottomOpen ? "with-bottom" : ""}`}>
        <div className="pane">
          <div className="pane-head">
            <Icon name="chat" size={15} />
            <span>{t("pane.chat")}</span>
            <span className="grow" />
          </div>
          <ChatPane view={view} />
        </div>
      </div>

      <PromptBar view={view} />

      {bottomOpen && (
          <Resizer
            axis="y"
            sign={-1}
            title={t("terminal.resize")}
            getValue={() => useStore.getState().workspace.bottomHeight}
            onChange={(v) => setWorkspaceSize("bottomHeight", v)}
          />
      )}
      <BottomTerminal key={`${profile}:${view.meta.id}`} view={view} height={bottomHeight} open={bottomOpen} />
    </main>
  );
}

function baseName(cwd: string): string {
  const parts = cwd.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || cwd;
}

function SessionActions({ view, cwd }: { view?: SV; cwd?: string }) {
  const t = useT();
  const setDensity = useStore((s) => s.setDensity);
  const details = useRef<HTMLDetailsElement>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const dismiss = (event: PointerEvent) => {
      if (details.current && !details.current.contains(event.target as Node)) details.current.open = false;
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || !details.current?.open) return;
      event.preventDefault();
      event.stopPropagation();
      details.current.open = false;
      details.current.querySelector("summary")?.focus();
    };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("keydown", onKey);
    };
  }, []);

  useEffect(() => {
    if (!copied) return;
    const timeout = window.setTimeout(() => setCopied(false), 2400);
    return () => window.clearTimeout(timeout);
  }, [copied]);

  async function copyConversation() {
    if (!view) return;
    try {
      await copyText(view.transcript.map((item) => item.kind === "tool"
        ? `工具 · ${item.title}\n${JSON.stringify(item.rawInput || {})}\n${item.terminalOutput || item.content.map((c) => c.text || "").join("\n")}`
        : `${item.kind === "user" ? "你" : item.kind === "assistant" ? "EasyAgent" : item.kind}\n${item.text}`,
      ).join("\n\n"));
      setCopied(true);
      useStore.setState({ connectionError: undefined });
    } catch (error) {
      useStore.setState({ connectionError: (error as Error).message });
    }
  }

  return (
    <details
      className="session-actions-menu"
      ref={details}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) event.currentTarget.open = false;
      }}
    >
      <summary className="icon-btn" title="更多会话选项" aria-label="更多会话选项">
        <span className="session-menu-dots" aria-hidden="true">···</span>
      </summary>
      <div className="session-actions-popover">
        {cwd && (
          <div className="session-menu-path">
            <Icon name="folder" size={13} />
            <span title={cwd}>{cwd}</span>
          </div>
        )}
        {view && (
          <>
            <button className="session-menu-action" disabled={!view.transcript.length} onClick={() => void copyConversation()}>
              <Icon name={copied ? "check" : "copy"} size={15} />
              <span role="status">{copied ? "已复制完整会话" : "复制完整会话"}</span>
            </button>
            <div className="session-menu-section" role="group" aria-label={t("density.title")}>
              <span className="session-menu-label">{t("density.title")}</span>
              {(["summary", "normal", "verbose"] as ViewDensity[]).map((density) => (
                <button
                  key={density}
                  className={`session-menu-action ${view.density === density ? "selected" : ""}`}
                  aria-pressed={view.density === density}
                  onClick={() => setDensity(view.meta.id, density)}
                >
                  <span>{t(`density.${density}`)}</span>
                  {view.density === density && <Icon name="check" size={14} />}
                </button>
              ))}
            </div>
          </>
        )}
      </div>
    </details>
  );
}

function EmptyState() {
  const currentModel = useStore((s) => s.currentModel),
    connected = useStore((s) => s.connected);
  const [cwd, setCwd] = useState(""),
    [model, setModel] = useState("");
  useEffect(() => {
    if (!model && currentModel) setModel(currentModel);
  }, [currentModel, model]);
  const view: SV = {
    meta: {
      id: "__new__",
      title: "新对话",
      cwd,
      status: "idle",
      model,
      availableModels: [],
      createdAt: 0,
      updatedAt: 0,
    },
    transcript: [],
    seq: 0,
    confirmations: [],
    phase: "idle",
    plan: [],
    diffs: [],
    density: "normal",
    panes: ["chat"],
    activePane: "chat",
  };
  async function start() {
    const text = useStore.getState().drafts.__new__ || "";
    const id = await useStore
      .getState()
      .createSession({ cwd: cwd || undefined, model: model || undefined });
    useStore.getState().setDraft(id, text);
    return id;
  }
  return (
    <main className="main session-main">
      <div className="toolbar">
        <div className="toolbar-heading">
          <span className="toolbar-title">新对话</span>
        </div>
        <button
          className="btn toolbar-project-picker"
          disabled={!connected}
          title={cwd || "选择项目"}
          onClick={() =>
            void useStore
              .getState()
              .pickFolder()
              .then((path) => {
                if (path) setCwd(path);
              })
              .catch((error) =>
                useStore.setState({ connectionError: error.message }),
              )
          }
        >
          <Icon name="folder" size={14} />
          <span>{cwd ? baseName(cwd) : "选择项目"}</span>
        </button>
        {cwd && (
          <button
            className="icon-btn"
            aria-label="清除项目"
            onClick={() => setCwd("")}
          >
            <Icon name="x" size={13} />
          </button>
        )}
        <SessionActions cwd={cwd} />
      </div>
      <ChatPane view={view} />
      <PromptBar view={view} onStart={start} onModelChange={setModel} />
    </main>
  );
}
