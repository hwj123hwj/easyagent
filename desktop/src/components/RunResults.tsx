import { useEffect, useId, useMemo, useState } from "react";
import { apiRequest, useStore, type SessionView } from "../store";
import type { RunFileSet } from "../client/file-delivery";
import { changeLabel, runTime, summarizeDiffs } from "../client/file-review";
import { Icon } from "./Icon";

export function ChangeStats({
  additions,
  deletions,
  partial = false,
}: {
  additions: number;
  deletions: number;
  partial?: boolean;
}) {
  return (
    <span
      className="change-stats"
      title={
        partial
          ? "仅已统计的文本文件；部分文件无法计算行数"
          : "新增与删除的行数"
      }
    >
      <span className="change-added">+{additions}</span>
      <span className="change-removed">−{deletions}</span>
      {partial && <span className="change-partial">部分</span>}
    </span>
  );
}

function RunCard({
  run,
  sessionId,
  latest,
}: {
  run: RunFileSet;
  sessionId: string;
  latest: boolean;
}) {
  const profile = useStore((s) => s.selectedProfile);
  const revision = useStore((s) => s.reviewRevision);
  const [open, setOpen] = useState(latest),
    [all, setAll] = useState(false);
  const [snapshot, setSnapshot] = useState<RunFileSet>(),
    [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const listId = useId();
  useEffect(() => {
    setOpen(latest);
  }, [latest]);
  useEffect(() => {
    if (!open) return;
    let alive = true;
    setError("");
    void apiRequest<RunFileSet>(
      "GET",
      `/sessions/${encodeURIComponent(sessionId)}/runs/${encodeURIComponent(run.run_id)}/files`,
    )
      .then((data) => {
        if (alive) setSnapshot(data);
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [open, sessionId, run.run_id, profile, revision, attempt]);
  const stats = useMemo(
    () => (snapshot ? summarizeDiffs(snapshot.files) : undefined),
    [snapshot],
  );
  const files = snapshot?.files ?? run.files;
  const review = (path?: string) =>
    useStore.getState().openRunReview(sessionId, run.run_id, path);
  return (
    <article className="file-change-card">
      <div className="file-change-head">
        <button
          className="file-change-toggle"
          aria-expanded={open}
          aria-controls={listId}
          onClick={() => setOpen(!open)}
        >
          <Icon name={open ? "chevron-down" : "chevron-right"} size={14} />
          <Icon name="diff" size={16} />
          <span>{files.length} 个文件已更改</span>
        </button>
        {stats && stats.diffs.size > 0 && <ChangeStats {...stats} />}
        <time className="file-change-time" dateTime={run.started_at}>
          {runTime(run.started_at)}
        </time>
        <button className="file-change-review" onClick={() => review()}>
          查看变更
          <Icon name="chevron-right" size={13} />
        </button>
      </div>
      {open && (
        <div id={listId} className="file-change-list">
          {files.slice(0, all ? files.length : 3).map((file) => {
            const diff = stats?.diffs.get(file.path);
            return (
              <button
                key={file.path}
                className="file-change-row"
                title={file.path}
                onClick={() => review(file.path)}
              >
                <Icon name="file" size={15} />
                <span className="file-change-path">{file.path}</span>
                <span className={`file-change-kind kind-${file.kind}`}>
                  {changeLabel(file)}
                </span>
                {diff ? (
                  <ChangeStats {...diff} />
                ) : (
                  <span className="file-change-size">
                    {file.binary
                      ? "二进制"
                      : `${(file.size / 1024).toFixed(1)} KiB`}
                  </span>
                )}
                <Icon name="chevron-right" size={12} />
              </button>
            );
          })}
          {files.length > 3 && (
            <button className="file-change-more" onClick={() => setAll(!all)}>
              {all ? "收起文件列表" : `再显示 ${files.length - 3} 个文件`}
              <Icon name={all ? "chevron-down" : "chevron-right"} size={12} />
            </button>
          )}
          {error && (
            <div className="file-change-error" role="alert">
              变更详情加载失败
              <button onClick={() => setAttempt((n) => n + 1)}>重试</button>
            </div>
          )}
        </div>
      )}
    </article>
  );
}

export function RunResults({ view }: { view: SessionView }) {
  const profile = useStore((s) => s.selectedProfile),
    revision = useStore((s) => s.reviewRevision);
  const [result, setResult] = useState<{ key: string; runs: RunFileSet[] }>();
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const key = `${profile}:${view.meta.id}`;
  useEffect(() => {
    let alive = true;
    setError("");
    void apiRequest<RunFileSet[]>(
      "GET",
      `/sessions/${encodeURIComponent(view.meta.id)}/run-files`,
    )
      .then((runs) => {
        if (alive) setResult({ key, runs });
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [key, view.meta.id, view.run?.state, revision, attempt]);
  const changed = (result?.key === key ? result.runs : [])
    .filter((run) => run.complete && run.files.length)
    .slice(-20);
  if (!changed.length && !error) return null;
  return (
    <section className="run-results" aria-label="任务文件变更">
      {changed.map((run, i) => (
        <RunCard
          key={`${key}:${run.run_id}`}
          run={run}
          sessionId={view.meta.id}
          latest={i === changed.length - 1}
        />
      ))}
      {error && (
        <div className="file-change-error" role="alert">
          文件变更加载失败
          <button onClick={() => setAttempt((n) => n + 1)}>重试</button>
        </div>
      )}
    </section>
  );
}
