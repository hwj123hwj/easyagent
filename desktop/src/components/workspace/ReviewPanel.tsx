import { useEffect, useMemo, useRef, useState } from "react";
import { apiRequest, useStore } from "../../store";
import {
  exportFile,
  type RunFileChange,
  type RunFileSet,
} from "../../client/file-delivery";
import { changeLabel, fileDiff, runTime } from "../../client/file-review";
import { ChangeStats } from "../RunResults";
import { Icon } from "../Icon";

function FileReview({
  file,
  workspace,
  sessionId,
  initiallyOpen,
  revealTarget,
  busy,
  undo,
  reportError,
}: {
  file: RunFileChange;
  workspace: string;
  sessionId: string;
  initiallyOpen: boolean;
  revealTarget?: object;
  busy: boolean;
  undo: (file: RunFileChange) => Promise<void>;
  reportError: (message: string) => void;
}) {
  const row = useRef<HTMLElement>(null);
  const [open, setOpen] = useState(initiallyOpen),
    [confirming, setConfirming] = useState(false);
  useEffect(() => {
    if (initiallyOpen) setOpen(true);
  }, [initiallyOpen, revealTarget]);
  useEffect(() => {
    if (!initiallyOpen || !revealTarget) return;
    const frame = requestAnimationFrame(() =>
      row.current?.scrollIntoView({ block: "nearest" }),
    );
    return () => cancelAnimationFrame(frame);
  }, [initiallyOpen, revealTarget]);
  const diff = useMemo(() => (open ? fileDiff(file) : undefined), [open, file]);
  const currentPath = `${workspace}/${file.path}`;
  return (
    <section ref={row} className="task-file-review">
      <button
        className="task-file-toggle"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        title={file.path}
      >
        <Icon name={open ? "chevron-down" : "chevron-right"} size={13} />
        <Icon name="file" size={14} />
        <span className="task-file-path">{file.path}</span>
        <small>{changeLabel(file)}</small>
      </button>
      {open && (
        <div className="task-file-body">
          <div className="task-file-actions">
            {diff && <ChangeStats {...diff} />}
            {file.kind !== "deleted" && (
              <>
                <button
                  onClick={() => useStore.getState().openFileTab(currentPath)}
                  title="打开当前文件内容"
                >
                  打开文件
                </button>
                <button
                  onClick={() =>
                    void exportFile(sessionId, currentPath).catch((e) =>
                      reportError(e.message),
                    )
                  }
                  title="导出当前文件内容"
                >
                  导出
                </button>
              </>
            )}
            <button
              className="task-file-undo"
              disabled={busy || !file.can_undo}
              onClick={() => setConfirming(true)}
              title={
                file.conflict ||
                (file.undone
                  ? "文件已经撤回"
                  : file.can_undo
                    ? "恢复任务开始前的内容"
                    : "此文件不能安全撤回")
              }
            >
              <Icon name="rewind" size={13} />
              撤回
            </button>
          </div>
          <p className="task-file-provenance">
            {file.attribution === "file_tool"
              ? "文件工具改动"
              : "任务期间观察到的改动 · 无法确认来源，不支持撤回"}
            {file.conflict && ` · ${file.conflict}`}
          </p>
          {confirming && (
            <div
              className="task-undo-confirm"
              role="group"
              aria-label={`确认撤回 ${file.path}`}
            >
              <p>
                {file.kind === "created"
                  ? "移除本轮新建的文件？"
                  : "恢复到任务开始前的内容？"}
                任务开始前的修改和 Git 暂存区会保留。
              </p>
              <button disabled={busy} onClick={() => setConfirming(false)}>
                取消
              </button>
              <button
                disabled={busy || !file.can_undo}
                onClick={() => {
                  setConfirming(false);
                  void undo(file);
                }}
              >
                确认撤回
              </button>
            </div>
          )}
          {file.binary ? (
            <p className="task-diff-note">
              二进制或超出快照大小上限，无法显示文本差异。
            </p>
          ) : diff ? (
            <>
              <div
                className="task-diff"
                role="region"
                aria-label={`${file.path} 文本差异`}
                tabIndex={0}
              >
                {diff.lines.length ? (
                  diff.lines.map((line, i) => (
                    <div key={i} className={`task-diff-line diff-${line.kind}`}>
                      <span className="diff-line-number">
                        {line.before ?? ""}
                      </span>
                      <span className="diff-line-number">
                        {line.after ?? ""}
                      </span>
                      <span className="diff-line-sign">
                        {line.kind === "added"
                          ? "+"
                          : line.kind === "removed"
                            ? "−"
                            : ""}
                      </span>
                      <code>{line.text || " "}</code>
                    </div>
                  ))
                ) : (
                  <div className="task-diff-note">
                    文本内容相同，可能仅文件属性发生变更。
                  </div>
                )}
              </div>
              {diff.truncated && (
                <p className="task-diff-note">
                  差异较长，显示前 800 行。增删统计包含全部差异。
                </p>
              )}
            </>
          ) : (
            <div className="task-file-comparison">
              <p>
                文件较大或差异过多，无法计算行数。以下显示快照前 20 万字符。
              </p>
              <section>
                <strong>任务开始前</strong>
                <pre>
                  {file.before?.slice(0, 200_000) || "（不存在或空文件）"}
                </pre>
              </section>
              <section>
                <strong>任务结束时</strong>
                <pre>
                  {file.after?.slice(0, 200_000) || "（不存在或空文件）"}
                </pre>
              </section>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

export function ReviewPanel() {
  const id = useStore((s) => s.activeSessionId),
    profile = useStore((s) => s.selectedProfile);
  if (!id)
    return (
      <div className="ws-panel task-review">
        <div className="empty">选择会话以审阅文件变更</div>
      </div>
    );
  return <SessionReview key={`${profile}:${id}`} id={id} profile={profile} />;
}

function SessionReview({ id, profile }: { id: string; profile: string }) {
  const state = useStore((s) => s.sessions[id]?.run?.state);
  const target = useStore((s) => s.reviewTarget);
  const pin =
    target?.sessionId === id && target.profileId === profile
      ? target
      : undefined;
  const [runs, setRuns] = useState<RunFileSet[]>(),
    [selected, setSelected] = useState("");
  const [snapshot, setSnapshot] = useState<{
    runId: string;
    data: RunFileSet;
  }>();
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [revision, setRevision] = useState(0);
  useEffect(() => {
    let alive = true;
    setError("");
    void apiRequest<RunFileSet[]>(
      "GET",
      `/sessions/${encodeURIComponent(id)}/run-files`,
    )
      .then((data) => {
        if (!alive) return;
        setRuns(data.filter((run) => run.files.length));
        setSelected((previous) =>
          data.some((run) => run.run_id === previous)
            ? previous
            : data.filter((run) => run.files.length).at(-1)?.run_id || "",
        );
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [id, state, revision]);
  useEffect(() => {
    if (pin && runs?.some((run) => run.run_id === pin.runId))
      setSelected(pin.runId);
  }, [pin, runs]);
  useEffect(() => {
    if (!selected) return;
    let alive = true;
    setError("");
    void apiRequest<RunFileSet>(
      "GET",
      `/sessions/${encodeURIComponent(id)}/runs/${encodeURIComponent(selected)}/files`,
    )
      .then((data) => {
        if (alive) setSnapshot({ runId: selected, data });
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [id, selected, revision]);
  const files = snapshot?.runId === selected ? snapshot.data : undefined;
  async function undo(file: RunFileChange) {
    setBusy(true);
    setError("");
    try {
      await apiRequest(
        "POST",
        `/sessions/${encodeURIComponent(id)}/runs/${encodeURIComponent(selected)}/undo`,
        { path: file.path, after_hash: file.version },
      );
      setRevision((n) => n + 1);
      useStore.setState((s) => ({ reviewRevision: s.reviewRevision + 1 }));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="ws-panel task-review">
      <div className="ws-panel-head">
        <Icon name="review" size={15} />
        文件变更
        <button
          className="task-review-close"
          aria-label="关闭文件审阅"
          onClick={() => useStore.getState().toggleWorkspaceView("review")}
        >
          <Icon name="x" size={14} />
        </button>
      </div>
      <label className="task-review-selector">
        任务快照
        <select
          aria-label="选择审阅任务"
          value={selected}
          disabled={busy}
          onChange={(e) => {
            useStore.setState({ reviewTarget: undefined });
            setSelected(e.target.value);
          }}
        >
          {(runs ?? [])
            .slice()
            .reverse()
            .map((run) => (
              <option key={run.run_id} value={run.run_id}>
                {runTime(run.started_at)} · {run.files.length} 个文件
              </option>
            ))}
        </select>
      </label>
      {error && (
        <div className="file-change-error" role="alert">
          {error}
          <button onClick={() => setRevision((n) => n + 1)}>重试</button>
        </div>
      )}
      {!error && (runs === undefined || (selected && !files)) && (
        <p role="status">正在加载文件变更…</p>
      )}
      {runs?.length === 0 && <div className="empty">暂无任务文件变更</div>}
      {files && !files.complete && <p>任务尚未结束，完成后可撤回。</p>}
      {!!files?.skipped?.length && (
        <p className="task-diff-note">部分文件未纳入快照，不能在这里撤回。</p>
      )}
      {files?.files.map((file, i) => (
        <FileReview
          key={`${selected}:${file.path}`}
          file={file}
          workspace={files.workspace}
          sessionId={id}
          revealTarget={pin}
          initiallyOpen={
            pin?.runId === selected && pin.path
              ? pin.path === file.path
              : i === 0
          }
          busy={busy}
          undo={undo}
          reportError={setError}
        />
      ))}
    </div>
  );
}
