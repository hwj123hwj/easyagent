import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useStore, type ChatItem, type SessionView } from "../../store";
import { Markdown } from "../Markdown";
import { ToolCall } from "../ToolCall";
import { MusicPlayer } from "../MusicPlayer";
import { Icon } from "../Icon";
import { copyText } from "../../client/clipboard";
import { isActiveRun } from "../../client/protocol";
const positions = new Map<string, { top: number; follow: boolean }>();
type Group = {
  kind: "group";
  id: string;
  items: Extract<ChatItem, { kind: "tool" }>[];
};
type Row = ChatItem | Group;
export function ChatPane({ view }: { view: SessionView }) {
  const container = useRef<HTMLDivElement>(null),
    heights = useRef(new Map<string, number>()),
    follow = useRef(true),
    previous = useRef("");
  const [viewport, setViewport] = useState({ top: 0, height: 600 }),
    [version, setVersion] = useState(0),
    [jump, setJump] = useState(false);
  const rows = useMemo(() => {
    const result: Row[] = [];
    for (const item of view.transcript) {
      const last = result.at(-1);
      if (item.kind === "tool") {
        if (last?.kind === "group") last.items.push(item);
        else result.push({ kind: "group", id: item.id, items: [item] });
      } else result.push(item);
    }
    return result;
  }, [view.transcript]);
  const offsets = useMemo(() => {
    const result = [0];
    for (const row of rows)
      result.push(
        result.at(-1)! +
          (heights.current.get(row.id) ||
            (row.kind === "group"
              ? Math.min(420, 80 + row.items.length * 40)
              : row.kind === "assistant"
                ? Math.max(100, Math.min(600, row.text.length / 3))
                : 84)),
      );
    return result;
  }, [rows, version]);
  let start = 0;
  while (start < rows.length && offsets[start + 1] < viewport.top - 600)
    start++;
  let end = start;
  while (
    end < rows.length &&
    offsets[end] < viewport.top + viewport.height + 600
  )
    end++;
  const onScroll = useCallback(() => {
    const el = container.current;
    if (!el) return;
    follow.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    setJump(!follow.current);
    setViewport({ top: el.scrollTop, height: el.clientHeight });
    positions.set(view.meta.id, { top: el.scrollTop, follow: follow.current });
  }, [view.meta.id]);
  useLayoutEffect(() => {
    const el = container.current;
    if (!el) return;
    if (previous.current !== view.meta.id) {
      const saved = positions.get(view.meta.id);
      follow.current = saved?.follow ?? true;
      el.scrollTop = saved?.top ?? el.scrollHeight;
      previous.current = view.meta.id;
    }
    if (follow.current) el.scrollTop = el.scrollHeight;
    setViewport({ top: el.scrollTop, height: el.clientHeight });
    setJump(!follow.current);
  }, [view.meta.id, view.transcript, offsets]);
  useEffect(() => {
    const el = container.current;
    if (!el) return;
    const observer = new ResizeObserver(() =>
      setViewport((v) => ({ ...v, height: el.clientHeight })),
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const listener = (event: Event) => {
      const path = (event as CustomEvent).detail?.path;
      if (path) useStore.getState().openFileTab(path);
    };
    window.addEventListener("open-file", listener);
    return () => window.removeEventListener("open-file", listener);
  }, []);
  const measure = useCallback(
    (id: string, height: number) => {
      const old = heights.current.get(id);
      if (old && Math.abs(old - height) < 1) return;
      const el = container.current;
      const i = rows.findIndex((r) => r.id === id);
      if (el && !follow.current && i >= 0 && offsets[i] < el.scrollTop && old)
        el.scrollTop += height - old;
      heights.current.set(id, height);
      setVersion((v) => v + 1);
    },
    [rows, offsets],
  );
  const loading = useStore((s) => s.loadingSession === view.meta.id);
  return (
    <div className="chat-viewport">
      <div
        className="pane-body personal-transcript-scroll"
        ref={container}
        onScroll={onScroll}
        tabIndex={0}
        aria-label="会话消息"
      >
        <div className="transcript personal-transcript">
          {loading && !rows.length && (
            <div className="history-loading" role="status">
              正在恢复会话…
              <div />
              <div />
              <div />
            </div>
          )}
          {!loading && !rows.length && (
            <div className="chat-introduction">
              <span className="welcome-mark">ea·</span>
              <h1>今天，想一起完成什么？</h1>
              <p>
                说说你的任务。回复、工具执行和每一步结果，都留在这段对话里。
              </p>
              <div className="prompt-suggestions">
                {["梳理一个想法", "一起看看代码", "排查一个问题"].map(
                  (text) => (
                    <button
                      key={text}
                      onClick={() => {
                        useStore.getState().setDraft(view.meta.id, text);
                        document
                          .querySelector<HTMLTextAreaElement>(".prompt-input")
                          ?.focus();
                      }}
                    >
                      {text}
                      <Icon name="arrow-right" size={14} />
                    </button>
                  ),
                )}
              </div>
            </div>
          )}
          <div style={{ height: offsets[start] }} aria-hidden="true" />
          {rows.slice(start, end).map((row) => (
            <MeasuredRow
              key={row.id}
              row={row}
              measure={measure}
              cwd={view.meta.cwd}
              density={view.density}
            />
          ))}
          <div
            style={{ height: offsets.at(-1)! - offsets[end] }}
            aria-hidden="true"
          />
          {isActiveRun(view.run) && (
            <div className="run-progress" role="status">
              <span className="connection-dot online" />
              {view.phase === "tool"
                ? "正在执行工具"
                : view.phase === "approval"
                  ? "等待你的确认"
                  : view.phase === "responding"
                    ? "正在生成回复"
                    : "正在思考"}
              <span>回复会持续更新</span>
            </div>
          )}
          {view.phase === "interrupted" && (
            <div className="interrupted-action">
              <span>
                任务已中断，已执行的操作可能保留；检查结果后可重新发起。
              </span>
              <button
                className="btn"
                disabled={!!useStore.getState().pending[view.meta.id]}
                onClick={() =>
                  void useStore
                    .getState()
                    .retryRun(view.meta.id)
                    .catch((error) =>
                      useStore.setState({ connectionError: error.message }),
                    )
                }
              >
                重新执行
              </button>
            </div>
          )}
          {view.phase === "cancelled" && (
            <div className="run-progress">任务已停止 · 已生成的内容已保留</div>
          )}
        </div>
      </div>
      {jump && (
        <button
          className="jump-latest"
          onClick={() => {
            follow.current = true;
            const el = container.current;
            if (el) {
              el.scrollTop = el.scrollHeight;
              onScroll();
            }
          }}
        >
          回到最新
          <Icon name="chevron-down" size={14} />
        </button>
      )}
    </div>
  );
}
function MeasuredRow({
  row,
  measure,
  cwd,
  density,
}: {
  row: Row;
  measure: (id: string, height: number) => void;
  cwd: string;
  density: SessionView["density"];
}) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(() =>
      measure(row.id, el.getBoundingClientRect().height),
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [row.id, measure]);
  return (
    <div ref={ref} className="transcript-row">
      {row.kind === "group" ? (
        <ToolGroup value={row} density={density} />
      ) : row.kind === "tool" ? null : (
        <Message item={row} cwd={cwd} density={density} />
      )}
    </div>
  );
}
function ToolGroup({
  value,
  density,
}: {
  value: Group;
  density: SessionView["density"];
}) {
  const running = value.items.some((i) => i.status === "in_progress"),
    failed = value.items.some((i) => i.status === "failed");
  const [shown, setShown] = useState(25);
  const [open, setOpen] = useState(running || failed || density === "verbose"),
    touched = useRef(false),
    wasRunning = useRef(running);
  useEffect(() => {
    if (failed) setOpen(true);
    else if (wasRunning.current && !running && !touched.current) setOpen(false);
    wasRunning.current = running;
  }, [running, failed]);
  return (
    <details
      className="conversation-tools"
      open={open}
      onToggle={(e) => {
        setOpen(e.currentTarget.open);
      }}
    >
      <summary
        onClick={() => {
          touched.current = true;
        }}
      >
        <Icon name="wrench" size={14} />
        <strong>{value.items.length} 个工具调用</strong>
        <span>{running ? "执行中" : failed ? "执行失败" : "已完成"}</span>
      </summary>
      {open && (
        <div>
          {value.items.slice(0, shown).map((item) => (
            <div key={item.id}>
              {item.title === "music_play" && item.status === "completed" && (
                <MusicPlayer
                  details={item.details}
                  resultText={item.content.map((c) => c.text || "").join("\n")}
                />
              )}
              <ToolCall
                key={item.id}
                title={item.title}
                toolKind={item.toolKind}
                status={item.status}
                locations={item.locations}
                content={item.content}
                rawInput={item.rawInput}
                details={item.details}
                terminalOutput={item.terminalOutput}
                defaultOpen={item.status === "failed" || density === "verbose"}
                onOpenFile={(path) => useStore.getState().openFileTab(path)}
              />
            </div>
          ))}
          {shown < value.items.length && (
            <button className="btn" onClick={() => setShown((n) => n + 25)}>
              显示更多调用（还剩 {value.items.length - shown} 个）
            </button>
          )}
        </div>
      )}
    </details>
  );
}
const Message = memo(function Message({
  item,
  cwd,
  density,
}: {
  item: Exclude<ChatItem, { kind: "tool" }>;
  cwd: string;
  density: SessionView["density"];
}) {
  const [copied, setCopied] = useState(false),
    [error, setError] = useState("");
  if (item.kind === "thought")
    return density === "summary" ? null : (
      <details className="message-thought">
        <summary>思考过程</summary>
        <p>{item.text}</p>
      </details>
    );
  if (item.kind === "system")
    return <div className="msg-system">{item.text}</div>;
  if (item.kind === "error")
    return (
      <div className="msg-error" role="alert">
        {item.text}
      </div>
    );
  if (item.kind === "user")
    return (
      <div className="personal-user">
        <div>{item.text}</div>
      </div>
    );
  return (
    <article className="personal-reply">
      <span className="reply-author">
        ea· <span>EasyAgent</span>
      </span>
      <Markdown text={item.text} basePath={cwd ? cwd + "/" : undefined} />
      <button
        className="reply-copy"
        onClick={() =>
          void copyText(item.text)
            .then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            })
            .catch((error) => setError(error.message))
        }
      >
        <Icon name={copied ? "check" : "copy"} size={13} />
        {copied ? "已复制" : "复制回复"}
      </button>
      {error && <span className="inline-error">{error}</span>}
    </article>
  );
});
