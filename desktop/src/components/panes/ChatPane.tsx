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
import { RunRecovery } from "../RunRecovery";
import { RunResults } from "../RunResults";
import { MusicPlayer } from "../MusicPlayer";
import { Icon } from "../Icon";
import { copyText } from "../../client/clipboard";
import { isActiveRun } from "../../client/protocol";
import { findConversation } from "../../client/conversation-search";
const positions = new Map<string, { top: number; follow: boolean }>();
type Group = {
  kind: "group";
  id: string;
  items: Extract<ChatItem, { kind: "tool" }>[];
};
type Row = ChatItem | Group;
export function ChatPane({ view }: { view: SessionView }) {
  const profile = useStore((s) => s.selectedProfile);
  const positionKey = profile + "\0" + view.meta.cwd + "\0" + view.meta.id;
  const [findOpen, setFindOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [matchIndex, setMatchIndex] = useState(0);
  const findInput = useRef<HTMLInputElement>(null);
  const matches = useMemo(
    () => findConversation(view.transcript, query),
    [view.transcript, query],
  );
  const match = matches[matchIndex % Math.max(1, matches.length)];
  const container = useRef<HTMLDivElement>(null),
    heights = useRef(new Map<string, number>()),
    follow = useRef(true),
    jumpingToLatest = useRef(false),
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
  useEffect(() => {
    const onFind = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "f") {
        if ((event.target as Element)?.closest(".files-panel")) return;
        event.preventDefault();
        setFindOpen(true);
        requestAnimationFrame(() => {
          findInput.current?.focus();
          findInput.current?.select();
        });
      }
    };
    window.addEventListener("keydown", onFind);
    return () => window.removeEventListener("keydown", onFind);
  }, []);
  useEffect(() => {
    setQuery("");
    setMatchIndex(0);
  }, [view.meta.id]);
  useLayoutEffect(() => {
    if (!findOpen || !match || !container.current) return;
    const index = rows.findIndex(
      (row) =>
        row.id === match.itemId ||
        (row.kind === "group" &&
          row.items.some((item) => item.id === match.itemId)),
    );
    if (index < 0) return;
    follow.current = false;
    container.current.scrollTop = Math.max(0, offsets[index] - 40);
    setViewport({
      top: container.current.scrollTop,
      height: container.current.clientHeight,
    });
    setJump(true);
    const frame = requestAnimationFrame(() => {
      const item = Array.from(
        container.current?.querySelectorAll<HTMLElement>(
          "[data-conversation-item]",
        ) || [],
      ).find((el) => el.dataset.conversationItem === match.itemId);
      item?.scrollIntoView({ block: "center" });
    });
    return () => cancelAnimationFrame(frame);
  }, [findOpen, match?.itemId, match?.offset]);
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
    follow.current =
      follow.current ||
      jumpingToLatest.current ||
      el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    setJump(!follow.current);
    setViewport({ top: el.scrollTop, height: el.clientHeight });
    positions.set(positionKey, { top: el.scrollTop, follow: follow.current });
  }, [positionKey]);
  useLayoutEffect(() => {
    const el = container.current;
    if (!el) return;
    if (previous.current !== positionKey) {
      jumpingToLatest.current = false;
      const saved = positions.get(positionKey);
      follow.current = saved?.follow ?? true;
      el.scrollTop = saved?.top ?? el.scrollHeight;
      previous.current = positionKey;
    }
    if (follow.current) el.scrollTop = el.scrollHeight;
    setViewport({ top: el.scrollTop, height: el.clientHeight });
    setJump(!follow.current);
  }, [positionKey, view.transcript, offsets]);
  useEffect(() => {
    const el = container.current;
    if (!el) return;
    const observer = new ResizeObserver(() => {
      if (follow.current) el.scrollTop = el.scrollHeight;
      setViewport({ top: el.scrollTop, height: el.clientHeight });
    });
    observer.observe(el);
    // Result cards and expanded tools can resize independently of text deltas.
    if (el.firstElementChild) observer.observe(el.firstElementChild);
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
      {findOpen && (
        <section className="conversation-find" aria-label="查找当前会话">
          <div>
            <Icon name="search" size={14} />
            <input
              ref={findInput}
              autoFocus
              aria-label="查找会话正文和工具结果"
              placeholder="查找正文和工具结果…"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setMatchIndex(0);
              }}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  setFindOpen(false);
                  container.current?.focus();
                }
                if (event.key === "Enter") {
                  event.preventDefault();
                  setMatchIndex(
                    (index) =>
                      (index + (event.shiftKey ? -1 : 1) + matches.length) %
                      Math.max(matches.length, 1),
                  );
                }
              }}
            />
            <span role="status">
              {matches.length ? (matchIndex % matches.length) + 1 : 0} /{" "}
              {matches.length}
              {matches.length === 5000 && "+"}
            </span>
            <button
              disabled={!matches.length}
              aria-label="上一处匹配"
              onClick={() =>
                setMatchIndex(
                  (index) => (index - 1 + matches.length) % matches.length,
                )
              }
            >
              ↑
            </button>
            <button
              disabled={!matches.length}
              aria-label="下一处匹配"
              onClick={() =>
                setMatchIndex((index) => (index + 1) % matches.length)
              }
            >
              ↓
            </button>
            <button
              aria-label="关闭会话查找"
              onClick={() => setFindOpen(false)}
            >
              ×
            </button>
          </div>
          {match && <p>{match.snippet}</p>}
        </section>
      )}
      <div
        className="pane-body personal-transcript-scroll"
        ref={container}
        onScroll={onScroll}
        onWheel={(event) => {
          if (event.deltaY < 0) follow.current = false;
        }}
        onTouchMove={() => {
          follow.current = false;
        }}
        onPointerDown={(event) => {
          const el = event.currentTarget;
          // A scrollbar drag expresses reading intent; content clicks do not.
          if (event.clientX >= el.getBoundingClientRect().right - 18)
            follow.current = false;
        }}
        onKeyDown={(event) => {
          if (["ArrowUp", "PageUp", "Home", "PageDown", "End"].includes(event.key))
            follow.current = false;
        }}
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
              foundItem={findOpen ? match?.itemId : undefined}
            />
          ))}
          <div
            style={{ height: offsets.at(-1)! - offsets[end] }}
            aria-hidden="true"
          />
          <RunResults view={view} />
          <RunRecovery view={view} />
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
            jumpingToLatest.current = true;
            const el = container.current;
            if (el) {
              el.scrollTop = el.scrollHeight;
              onScroll();
              // Final virtual rows mount after the first scroll. Settle their
              // measured heights before releasing the explicit follow intent.
              requestAnimationFrame(() => {
                if (container.current !== el || previous.current !== positionKey) return;
                el.scrollTop = el.scrollHeight;
                onScroll();
                requestAnimationFrame(() => {
                  if (container.current !== el || previous.current !== positionKey) return;
                  el.scrollTop = el.scrollHeight;
                  onScroll();
                  jumpingToLatest.current = false;
                });
              });
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
  foundItem,
}: {
  row: Row;
  measure: (id: string, height: number) => void;
  cwd: string;
  density: SessionView["density"];
  foundItem?: string;
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
    <div
      ref={ref}
      data-conversation-item={row.id}
      className={
        "transcript-row " + (row.id === foundItem ? "search-match-row" : "")
      }
    >
      {row.kind === "group" ? (
        <ToolGroup value={row} density={density} foundItem={foundItem} />
      ) : row.kind === "tool" ? null : (
        <Message
          item={row}
          cwd={cwd}
          density={
            row.id === foundItem && density === "summary" ? "normal" : density
          }
        />
      )}
    </div>
  );
}
function ToolGroup({
  value,
  density,
  foundItem,
}: {
  value: Group;
  density: SessionView["density"];
  foundItem?: string;
}) {
  const running = value.items.some((i) => i.status === "in_progress"),
    failed = value.items.some((i) => i.status === "failed");
  const completedCount = value.items.filter(
    (item) => item.status === "completed",
  ).length;
  const failedCount = value.items.filter(
    (item) => item.status === "failed",
  ).length;
  const current =
    value.items.find((item) => item.status === "in_progress") ||
    value.items.at(-1);
  const currentLabel =
    current?.rawInput?.command ||
    current?.rawInput?.path ||
    current?.rawInput?.file_path ||
    current?.title;
  const commandCount = value.items.filter(
    (item) => typeof item.rawInput?.command === "string",
  ).length;
  const fileCount = new Set(
    value.items.flatMap(
      (item) => item.locations?.map((location) => location.path) || [],
    ),
  ).size;
  const duration = value.items.reduce(
    (total, item) => total + (item.durationMs || 0),
    0,
  );
  const [shown, setShown] = useState(25);
  useEffect(() => {
    const index = value.items.findIndex((item) => item.id === foundItem);
    if (index >= 0) {
      setOpen(true);
      setShown((count) => Math.max(count, index + 1));
    }
  }, [foundItem]);
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
        <strong>
          {value.items.length} 个工具调用
          {commandCount > 0 && ` · ${commandCount} 条命令`}
          {fileCount > 0 && ` · ${fileCount} 个文件`}
        </strong>
        <span className="tool-group-current" title={String(currentLabel || "")}>
          {String(currentLabel || "")}
        </span>
        <span className="tool-group-counts">
          {running && "执行中 · "}
          {completedCount} 成功{failedCount > 0 && ` · ${failedCount} 失败`}
          {duration > 0 && (
            <small title="工具耗时累计">
              {" "}
              · {(duration / 1000).toFixed(1)}s
            </small>
          )}
        </span>
      </summary>
      {open && (
        <div>
          {value.items.slice(0, shown).map((item) => (
            <div
              key={item.id}
              data-conversation-item={item.id}
              className={item.id === foundItem ? "search-match-row" : ""}
            >
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
                forceOpen={item.id === foundItem}
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
