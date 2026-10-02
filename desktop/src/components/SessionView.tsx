import { useEffect, useState } from "react";
import { useStore, type SessionView as SV, type ViewDensity } from "../store";
import { ChatPane } from "./panes/ChatPane";
import { PromptBar } from "./PromptBar";
import { Icon } from "./Icon";
import { WorkspaceToggles } from "./workspace/WorkspaceToggles";
import { useT } from "../i18n/useT";
import { BottomTerminal } from "./workspace/BottomTerminal";
import { Resizer } from "./workspace/Resizer";
import { isElectron } from "../platform";
import { copyText } from "../client/clipboard";
import { isActiveRun } from "../client/protocol";

export function SessionView() {
  const activeId = useStore((s) => s.activeSessionId);
  const view = useStore((s) => (activeId ? s.sessions[activeId] : undefined));
  const setDensity = useStore((s) => s.setDensity);
  const bottomOpen = useStore((s) => s.workspace.bottomOpen);
  const bottomHeight = useStore((s) => s.workspace.bottomHeight);
  const setWorkspaceSize = useStore((s) => s.setWorkspaceSize);
  const t = useT();

  if (!view) {
    return <EmptyState />;
  }

  const meta = view.meta;
  async function copyConversation() {
    try {
      await copyText(
        view!.transcript
          .map((item) =>
            item.kind === "tool"
              ? `工具 · ${item.title}\n${JSON.stringify(item.rawInput || {})}\n${item.terminalOutput || item.content.map((c) => c.text || "").join("\n")}`
              : `${item.kind === "user" ? "你" : item.kind === "assistant" ? "EasyAgent" : item.kind}\n${item.text}`,
          )
          .join("\n\n"),
      );
      useStore.setState({ connectionError: undefined });
    } catch (error) {
      useStore.setState({ connectionError: (error as Error).message });
    }
  }

  return (
    <main className="main">
      <div className="toolbar">
        <span className="toolbar-title">{meta.title}</span>
        {meta.cwd && (
          <span className="toolbar-cwd" title={meta.cwd}>
            <Icon name="folder" size={13} />
            {baseName(meta.cwd)}
          </span>
        )}
        <span className="toolbar-status">
          <span className={`status-dot ${meta.status}`} />
          {isActiveRun(view.run) && view.phase === "responding"
            ? t("chat.aiResponding")
            : isActiveRun(view.run) && view.phase === "tool"
              ? t("tool.status.in_progress")
              : isActiveRun(view.run) && view.phase === "approval"
                ? t("status.needs_approval")
                : t(`status.${meta.status}` as any)}
        </span>
        <span className="grow" />

        <button
          className="icon-btn"
          aria-label="复制完整会话"
          title="复制完整会话"
          disabled={!view.transcript.length}
          onClick={() => void copyConversation()}
        >
          <Icon name="copy" size={14} />
        </button>
        <WorkspaceToggles />

        {/* Density toggle (summary / normal / verbose) — desktop only on mobile */}
        <div
          className={`views-menu ${isElectron ? "toolbar-density" : "toolbar-density-mobile"}`}
        >
          {(["summary", "normal", "verbose"] as ViewDensity[]).map((d) => (
            <button
              key={d}
              className={view.density === d ? "active" : ""}
              onClick={() => setDensity(meta.id, d)}
              title={t("density.title")}
            >
              {t(`density.${d}`)}
            </button>
          ))}
        </div>
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

      {bottomOpen && (
        <>
          <Resizer
            axis="y"
            sign={1}
            title={t("terminal.resize")}
            getValue={() => useStore.getState().workspace.bottomHeight}
            onChange={(v) => setWorkspaceSize("bottomHeight", v)}
          />
          <BottomTerminal view={view} height={bottomHeight} />
        </>
      )}

      <PromptBar view={view} />
    </main>
  );
}

function baseName(cwd: string): string {
  const parts = cwd.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || cwd;
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
    <main className="main">
      <div className="toolbar">
        <span className="toolbar-title">新对话</span>
        <span className="grow" />
        <button
          className="btn"
          disabled={!connected}
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
          {cwd ? baseName(cwd) : "选择项目"}
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
      </div>
      <ChatPane view={view} />
      <PromptBar view={view} onStart={start} onModelChange={setModel} />
    </main>
  );
}
