import { useEffect, useState } from "react";
import { apiRequest, useStore } from "../../store";
import type { RunFileSet } from "../../client/file-delivery";
import { Icon } from "../Icon";
export function ReviewPanel() {
  const id = useStore((s) => s.activeSessionId),
    profile = useStore((s) => s.selectedProfile),
    state = useStore((s) => (id ? s.sessions[id]?.run?.state : undefined));
  const [runs, setRuns] = useState<RunFileSet[]>([]),
    [selected, setSelected] = useState(""),
    [files, setFiles] = useState<RunFileSet>(),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [revision, setRevision] = useState(0);
  useEffect(() => {
    let alive = true;
    setRuns([]);
    setFiles(undefined);
    setSelected("");
    setError("");
    if (id)
      void apiRequest<RunFileSet[]>(
        "GET",
        `/sessions/${encodeURIComponent(id)}/run-files`,
      )
        .then((data) => {
          if (alive) {
            setRuns(data);
            setSelected(data.at(-1)?.run_id || "");
          }
        })
        .catch((e) => {
          if (alive) setError(e.message);
        });
    return () => {
      alive = false;
    };
  }, [id, profile, state]);
  useEffect(() => {
    let alive = true;
    setFiles(undefined);
    if (id && selected)
      void apiRequest<RunFileSet>(
        "GET",
        `/sessions/${encodeURIComponent(id)}/runs/${encodeURIComponent(selected)}/files`,
      )
        .then((data) => {
          if (alive) setFiles(data);
        })
        .catch((e) => {
          if (alive) setError(e.message);
        });
    return () => {
      alive = false;
    };
  }, [id, selected, profile, revision]);
  async function undo(path: string, version: string) {
    if (!id) return;
    setBusy(true);
    setError("");
    try {
      await apiRequest(
        "POST",
        `/sessions/${encodeURIComponent(id)}/runs/${encodeURIComponent(selected)}/undo`,
        { path, after_hash: version },
      );
      setRevision((x) => x + 1);
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
        任务变更审阅
      </div>
      <p>
        撤回恢复任务开始前的内容，保留原有修改及 Git
        暂存区。文件结束后被改动时会拒绝撤回。
      </p>
      <label>
        选择任务
        <select value={selected} onChange={(e) => setSelected(e.target.value)}>
          {runs
            .slice()
            .reverse()
            .map((run) => (
              <option key={run.run_id} value={run.run_id}>
                {new Date(run.started_at || "").toLocaleString()} ·{" "}
                {run.files.length} 个文件
              </option>
            ))}
        </select>
      </label>
      {error && (
        <p className="inline-error" role="alert">
          {error}
        </p>
      )}
      {!runs.length && (
        <div className="empty">
          暂无带文件快照的任务。旧会话的整个工作区差异不会作为本轮变更。
        </div>
      )}
      {files && !files.complete && <p>任务尚未结束，完成后可审阅。</p>}
      {files?.skipped.length ? (
        <p className="settings-note">
          有文件因大小、权限或扫描上限未纳入快照；此处不能撤回这些文件。
        </p>
      ) : null}
      {files?.files.map((file) => (
        <details className="task-file-review" key={file.path}>
          <summary>
            <span>{file.path}</span>
            <small>
              {file.undone
                ? "已撤回"
                : file.kind === "created"
                  ? "新增"
                  : file.kind === "deleted"
                    ? "删除"
                    : "修改"}
            </small>
          </summary>
          <p>
            {file.attribution === "file_tool"
              ? "文件工具改动"
              : "本轮期间观察到 · 来源未确认，不开放撤回"}
            {file.conflict && " · " + file.conflict}
          </p>
          {file.binary ? (
            <p>二进制文件可在文件面板预览或导出。</p>
          ) : (
            <div className="task-file-comparison">
              <section>
                <strong>任务开始前</strong>
                <pre>{file.before || "（不存在或空文件）"}</pre>
              </section>
              <section>
                <strong>任务结束时</strong>
                <pre>{file.after || "（不存在或空文件）"}</pre>
              </section>
            </div>
          )}
          <button
            className="btn"
            disabled={busy || !file.can_undo}
            onClick={() => {
              if (
                window.confirm(
                  `恢复「${file.path}」到任务开始前？${file.kind === "created" ? "本轮新建文件将被移除。" : "原有修改会保留。"}`,
                )
              )
                void undo(file.path, file.version);
            }}
          >
            撤回此文件
          </button>
        </details>
      ))}
    </div>
  );
}
