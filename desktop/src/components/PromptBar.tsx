import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { apiRequest, useStore, type SessionView } from "../store";
import { Icon } from "./Icon";
import { ContextInspector } from "./ContextInspector";
import { ComposerStatus } from "./ComposerStatus";
import { ModelPicker } from "./ModelPicker";
import { useVoiceInput } from "../hooks/useVoiceInput";
import { filterCommands, parseCommand } from "../client/commands";
import { isActiveRun } from "../client/protocol";
import { ConversationQueue } from "./ConversationQueue";
import { PromptContext } from "./PromptContext";
import { EMPTY_INPUTS, promptInputs } from "../client/protocol";
export function PromptBar({
  view,
  onStart,
  onModelChange,
}: {
  view: SessionView;
  onStart?: () => Promise<string>;
  onModelChange?: (value: string) => void;
}) {
  const commands = useStore((s) => s.commands);
  const id = view.meta.id,
    pending = useStore((s) => !!s.pending[id]),
    connected = useStore((s) => s.connected),
    loading = useStore((s) => s.loadingSession === id);
  const text = useStore((s) => s.drafts[id] || ""),
    setDraft = useStore((s) => s.setDraft),
    setCommandOutput = useStore((s) => s.setCommandOutput);
  const draftInputs = useStore((state) => state.draftInputs[id] || EMPTY_INPUTS);
  const [inspect, setInspect] = useState(false);
  const [uploadBusy, setUploadBusy] = useState(false);
  const hasInputs = !!(draftInputs.attachments.length + draftInputs.files.length);
  const [error, setError] = useState(""),
    [localOutput, setLocalOutput] = useState(""),
    [menuOpen, setMenuOpen] = useState(false),
    [selected, setSelected] = useState(0),
    [commandBusy, setCommandBusy] = useState(false),
    [modelBusy, setModelBusy] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null),
    composing = useRef(false),
    revision = useRef(0);
  const busy = isActiveRun(view.run),
    matches = filterCommands(text, commands),
    output = view.commandOutput ?? localOutput;
  function change(value: string) {
    revision.current++;
    setDraft(id, value);
    setMenuOpen(!!filterCommands(value, commands));
    setSelected(0);
  }
  const voice = useVoiceInput({
    onText: (value) => change(text + (text ? " " : "") + value),
  });
  function fitInput() {
    const el = input.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = Math.min(200, el.scrollHeight) + "px";
      el.style.overflowY = el.scrollHeight > 200 ? "auto" : "hidden";
    }
  }
  useLayoutEffect(() => {
    fitInput();
  }, [text]);
  useEffect(() => {
    const el = input.current;
    if (!el) return;
    let width = el.getBoundingClientRect().width;
    const observer = new ResizeObserver(() => {
      const nextWidth = el.getBoundingClientRect().width;
      if (nextWidth === width) return;
      width = nextWidth;
      fitInput();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    setError("");
    setLocalOutput("");
    setInspect(false);
    setMenuOpen(false);
    input.current?.focus();
  }, [id]);
  useEffect(() => {
    if (menuOpen)
      document
        .getElementById("slash-" + selected)
        ?.scrollIntoView({ block: "nearest" });
  }, [menuOpen, selected]);
  async function execute(name: string, args: string, targetId: string, profileId: string) {
    switch (name) {
      case "help":
        return commands
          .map((c) => "/" + c.name + "  " + c.description)
          .join("\n");
      case "new":
        await useStore.getState().createSession({ cwd: view.meta.cwd });
        return;
      case "sessions":
        useStore.getState().toggleSidebar();
        return;
      case "mcp":
        if (args) return serverCommand(name, args, targetId, profileId);
        useStore.getState().openSettings(true, "mcp");
        return;
      case "settings":
        useStore.getState().openSettings(true, "connections");
        return;
      case "model":
        if (args) await useStore.getState().setModel(targetId, args);
        else {
          document.querySelector<HTMLButtonElement>(".model-trigger")?.click();
        }
        return;
      case "stop":
        await useStore.getState().cancel(targetId);
        return "已请求停止任务";
      case "tools": {
        const value = await apiRequest<any>("GET", "/tools");
        return (value.tools || []).join("\n");
      }
      case "context": {
        if (busy) throw new Error("任务完成后才能查看当前上下文");
        setInspect(true);
        return;
      }
      case "compact": {
        if (busy) throw new Error("任务完成后才能压缩上下文");
        const value = await apiRequest<any>(
          "POST",
          `/sessions/${targetId}/compact`,
          { custom_instructions: args },
        );
        await useStore.getState().refreshSessionInfo(targetId);
        return value.summary || "上下文已压缩";
      }
      default:
        return serverCommand(name, args, targetId, profileId);
    }
  }
  async function serverCommand(name: string, args: string, targetId: string, profileId: string) {
    if (!commands.some((command) => command.name === name))
      throw new Error("未知命令，请输入 / 查看可用命令");
    const result = await apiRequest<{
      output: string;
      should_query: boolean;
      query_prompt?: string;
      session_id?: string;
    }>("POST", `/sessions/${targetId}/command`, {
      command: "/" + name + (args ? " " + args : ""),
    });
    if (useStore.getState().selectedProfile !== profileId) return;
    if (result.session_id && result.session_id !== targetId) {
      await useStore.getState().refreshSessions();
      if (useStore.getState().selectedProfile !== profileId) return;
      await useStore.getState().setActive(result.session_id);
    }
    if (useStore.getState().selectedProfile !== profileId) return;
    if (result.should_query && result.query_prompt)
      await useStore
        .getState()
        .sendPrompt(result.session_id || targetId, result.query_prompt);
    else if (["profile", "confirm", "undo", "goal"].includes(name))
      await useStore.getState().setActive(result.session_id || targetId);
    return { output: result.output, sessionId: result.session_id || targetId };
  }
  async function submit() {
    if ((!text.trim() && !hasInputs) || pending || commandBusy || loading || uploadBusy) return;
    let targetId = id;
    const captured = text,
      version = revision.current,
      profileId = useStore.getState().selectedProfile,
      command = text.startsWith("//") ? null : parseCommand(text);
    const capturedInputs = draftInputs;
    setError("");
    setMenuOpen(false);
    try {
      if (
        onStart &&
        (!command ||
          (!["help", "settings", "mcp", "sessions", "new", "stop"].includes(
            command.name,
          ) &&
            !(command.name === "model" && !command.args)) ||
          (command?.name === "mcp" && !!command.args))
      )
        targetId = await onStart();
      if (useStore.getState().selectedProfile !== profileId) return;
      if (command) {
        if (hasInputs) throw new Error("附件和文件引用请随普通消息发送；移除后可执行命令。");
        setCommandBusy(true);
        const result = await execute(command.name, command.args, targetId, profileId);
        if (useStore.getState().selectedProfile !== profileId) return;
        const commandOutput = typeof result === "string" ? result : result?.output || "";
        const outputId = typeof result === "object" ? result.sessionId : targetId;
        if (outputId === "__new__") setLocalOutput(commandOutput);
        else setCommandOutput(outputId || targetId, commandOutput, profileId);
      } else {
        await useStore
          .getState()
          .sendPrompt(targetId, text.trim() ? (text.startsWith("//") ? text.slice(1) : text) : "请查看附带的文件。", promptInputs(capturedInputs));
      }
      if (useStore.getState().selectedProfile !== profileId) return;
      if (revision.current === version) {
        if (useStore.getState().drafts[id] === captured) setDraft(id, "");
        if (
          targetId !== id &&
          useStore.getState().drafts[targetId] === captured
        )
          setDraft(targetId, "");
      }
      const currentInputs = useStore.getState().draftInputs[id] || EMPTY_INPUTS;
      if (JSON.stringify(currentInputs) === JSON.stringify(capturedInputs)) useStore.getState().setDraftInputs(id, EMPTY_INPUTS);
    } catch (error) {
      if (useStore.getState().selectedProfile !== profileId) return;
      setError((error as Error).message);
      if (targetId !== id)
        useStore.setState({ connectionError: (error as Error).message });
      if (revision.current === version) setDraft(id, captured);
    } finally {
      setCommandBusy(false);
    }
  }
  return (
    <div className="promptbar personal-composer">
      <div className="promptbar-inner">
        <ConversationQueue view={view} />
        {inspect && <ContextInspector key={id} view={view} onBusy={setCommandBusy} onClose={() => { setInspect(false); input.current?.focus(); }} />}
        <div className="confirmation-list">
          {view.confirmations.map((confirmation) => (
            <ConfirmationPrompt
              key={confirmation.confirmation_id}
              id={id}
              value={confirmation}
            />
          ))}
        </div>
        {output && (
          <section className="command-output">
            <button
              className="icon-btn"
              aria-label="关闭命令结果"
              onClick={() => {
                setLocalOutput("");
                if (id !== "__new__") setCommandOutput(id, "");
                input.current?.focus();
              }}
            >
              <Icon name="x" size={14} />
            </button>
            <pre>{output}</pre>
          </section>
        )}
        {error && (
          <p className="composer-error" role="alert">
            {error}
          </p>
        )}
        <div className="personal-input-wrap">
          <PromptContext id={id} cwd={view.meta.cwd} text={text} onChange={change} onStart={onStart} onBusy={setUploadBusy} onError={setError} input={input} />
          {menuOpen && matches && (
            <div className="slash-menu">
              <div className="slash-heading">
                命令<span>↑ ↓ 选择 · Tab 补全 · Esc 关闭</span>
              </div>
              <div role="listbox" id="slash-options">
                {matches.map((command, i) => (
                  <button
                    role="option"
                    aria-selected={i === selected}
                    id={"slash-" + i}
                    key={command.name}
                    onPointerDown={(e) => e.preventDefault()}
                    onClick={() => {
                      change(command.insert || "/" + command.name + " ");
                      setMenuOpen(false);
                      input.current?.focus();
                    }}
                  >
                    <code>/{command.name}</code>
                    <span>
                      <strong>{command.label}</strong>
                      <small>{command.description}</small>
                    </span>
                  </button>
                ))}
                {!matches.length && <p>没有匹配的命令</p>}
              </div>
            </div>
          )}
          <textarea
            ref={input}
            className="prompt-input"
            rows={1}
            value={text}
            disabled={loading}
            placeholder={
              loading
                ? "正在恢复会话…"
                : busy
                  ? "追加要求，Enter 加入待发队列…"
                  : "描述你想完成的事…"
            }
            aria-label="消息输入"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={!!(menuOpen && matches)}
            aria-controls="slash-options"
            aria-activedescendant={
              menuOpen && matches?.[selected] ? "slash-" + selected : undefined
            }
            onChange={(e) => change(e.target.value)}
            onCompositionStart={() => {
              composing.current = true;
            }}
            onCompositionEnd={() => {
              composing.current = false;
            }}
            onKeyDown={(e) => {
              if (
                composing.current ||
                e.nativeEvent.isComposing ||
                e.keyCode === 229
              )
                return;
              if (menuOpen && matches) {
                if (e.key === "Escape") {
                  e.preventDefault();
                  setMenuOpen(false);
                  return;
                }
                if (["ArrowDown", "ArrowUp"].includes(e.key)) {
                  e.preventDefault();
                  setSelected(
                    (i) =>
                      (i + (e.key === "ArrowDown" ? 1 : -1) + matches.length) %
                      Math.max(matches.length, 1),
                  );
                  return;
                }
                if (
                  (e.key === "Tab" || e.key === "Enter") &&
                  !e.shiftKey &&
                  matches[selected]
                ) {
                  e.preventDefault();
                  change(
                    matches[selected].insert ||
                      "/" + matches[selected].name + " ",
                  );
                  setMenuOpen(false);
                  return;
                }
              }
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                void submit();
              }
            }}
          />
          <div className="composer-actions">
            <ComposerStatus onInspect={id === "__new__" ? undefined : () => setInspect(true)} view={view} disabled={!connected || busy || pending || loading || commandBusy} onStart={onStart} onError={setError} />
            <ModelPicker
              value={view.meta.model}
              disabled={!connected || busy || pending || modelBusy}
              onChange={async (value) => {
                setModelBusy(true);
                try {
                  if (onModelChange) onModelChange(value);
                  else await useStore.getState().setModel(id, value);
                } catch (error) {
                  setError((error as Error).message);
                } finally {
                  setModelBusy(false);
                }
              }}
            />
            <button
              className="composer-command"
              title="命令（输入 /）"
              aria-label="查看命令"
              disabled={loading}
              onClick={() => {
                if (!text.trim()) change("/");
                input.current?.focus();
                setMenuOpen(true);
              }}
            >
              <span aria-hidden="true" className="composer-command-icon">/</span>
              <span className="composer-command-label">命令</span>
            </button>
            <span className="grow composer-spacer" />
            <button
              className={"icon-btn composer-voice " + (voice.recording ? "recording" : "")}
              title={voice.recording ? "停止录音" : "语音输入"}
              aria-label={voice.recording ? "停止录音" : "语音输入"}
              aria-pressed={voice.recording}
              disabled={voice.transcribing || busy || loading}
              onClick={voice.toggle}
            >
              <Icon name="mic" size={16} />
            </button>
            {busy && (
              <button
                className="composer-stop"
                title="停止当前任务"
                aria-label="停止当前任务"
                disabled={!connected}
                onClick={() =>
                  void useStore
                    .getState()
                    .cancel(id)
                    .catch((error) => setError(error.message))
                }
              >
                <Icon name="stop" size={15} />
                <span className="composer-stop-label">停止</span>
              </button>
            )}
            {(
              <button
                className="composer-send"
                title={busy ? "加入待发队列，当前任务结束后执行" : "发送消息"}
                aria-label={busy ? "加入待发队列" : "发送消息"}
                disabled={
                  (!text.trim() && !hasInputs) ||
                  uploadBusy ||
                  pending ||
                  commandBusy ||
                  loading ||
                  (!connected && !parseCommand(text))
                }
                onClick={() => void submit()}
              >
                {pending ? (
                  <span className="spinner-xs" />
                ) : (
                  <Icon name="send" size={19} />
                )}
              </button>
            )}
          </div>
        </div>
        <div className="composer-foot">
          <span>
            {pending
              ? "等待服务确认接收，草稿仍保留"
              : busy
                ? "Agent 正在" +
                  (view.phase === "tool"
                    ? "执行工具"
                    : view.phase === "approval"
                      ? "等待确认"
                      : view.phase === "responding"
                        ? "生成回复"
                        : "思考")
                : "Enter 发送 · Shift Enter 换行"}
          </span>
          <span className="composer-host-hint">文件与工具在服务主机上运行</span>
        </div>
        {voice.error && (
          <p role="alert" className="composer-error">
            {voice.error}
          </p>
        )}
      </div>
    </div>
  );
}
function ConfirmationPrompt({
  id,
  value,
}: {
  id: string;
  value: SessionView["confirmations"][number];
}) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function reply(approved: boolean) {
    setBusy(true);
    try {
      await useStore.getState().confirm(id, value.confirmation_id, approved);
    } catch (error) {
      setError((error as Error).message);
      setBusy(false);
    }
  }
  return (
    <section className="confirmation-prompt">
      <strong>确认操作 · {value.tool_name}</strong>
      <p>{value.description}</p>
      {value.args !== undefined && (
        <pre>{JSON.stringify(value.args, null, 2)}</pre>
      )}
      <div>
        <button
          className="btn"
          disabled={busy}
          onClick={() => void reply(false)}
        >
          拒绝
        </button>
        <button
          className="btn primary"
          disabled={busy}
          onClick={() => void reply(true)}
        >
          允许这次操作
        </button>
      </div>
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
