import { useEffect, useId, useMemo, useState } from "react";
import { apiRequest, useStore, type SessionView } from "../store";
import type { RunFileSet, RunFileChange } from "../client/file-delivery";
import { summarizeDiffs } from "../client/file-review";
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
      {additions > 0 && <span className="change-added">+{additions}</span>}
      {deletions > 0 && <span className="change-removed">−{deletions}</span>}
      {partial && <span className="change-partial">部分</span>}
    </span>
  );
}

const EXT_BADGES: Record<string, { bg: string; color: string; label: string }> = {
  go: { bg: "rgba(0, 172, 215, 0.16)", color: "#29b6f6", label: "go" },
  ts: { bg: "rgba(49, 120, 198, 0.16)", color: "#42a5f5", label: "ts" },
  tsx: { bg: "rgba(49, 120, 198, 0.16)", color: "#42a5f5", label: "tsx" },
  js: { bg: "rgba(247, 223, 30, 0.16)", color: "#fbc02d", label: "js" },
  jsx: { bg: "rgba(247, 223, 30, 0.16)", color: "#fbc02d", label: "jsx" },
  py: { bg: "rgba(55, 118, 171, 0.16)", color: "#64b5f6", label: "py" },
  rs: { bg: "rgba(222, 90, 54, 0.16)", color: "#ff8a65", label: "rs" },
  json: { bg: "rgba(255, 255, 255, 0.08)", color: "#b0bec5", label: "json" },
  md: { bg: "rgba(120, 144, 156, 0.16)", color: "#90a4ae", label: "md" },
  css: { bg: "rgba(66, 165, 245, 0.16)", color: "#64b5f6", label: "css" },
  html: { bg: "rgba(239, 83, 80, 0.16)", color: "#ef5350", label: "html" },
  yaml: { bg: "rgba(239, 83, 80, 0.16)", color: "#ef5350", label: "yml" },
  yml: { bg: "rgba(239, 83, 80, 0.16)", color: "#ef5350", label: "yml" },
  sh: { bg: "rgba(78, 205, 196, 0.16)", color: "#4ecdc4", label: "sh" },
  sql: { bg: "rgba(255, 167, 38, 0.16)", color: "#ffa726", label: "sql" },
};

function FileTypeBadge({ path }: { path: string }) {
  const dot = path.lastIndexOf(".");
  const ext = dot >= 0 ? path.slice(dot + 1).toLowerCase() : "";
  const item = EXT_BADGES[ext];
  if (item) {
    return (
      <span
        className="file-type-badge"
        style={{ backgroundColor: item.bg, color: item.color }}
      >
        {item.label}
      </span>
    );
  }
  return (
    <span className="file-type-badge fallback">
      <Icon name="file" size={13} />
    </span>
  );
}

function splitFilePath(path: string): { name: string; dir: string } {
  const normalized = path.replace(/\\/g, "/");
  const lastSlash = normalized.lastIndexOf("/");
  if (lastSlash === -1) {
    return { name: normalized, dir: "" };
  }
  return {
    name: normalized.slice(lastSlash + 1),
    dir: normalized.slice(0, lastSlash + 1),
  };
}

interface AggregatedFile {
  path: string;
  file: RunFileChange;
  latestRunId: string;
  runs: string[];
}

function AggregatedRunCard({
  runs,
  sessionId,
}: {
  runs: RunFileSet[];
  sessionId: string;
}) {
  const profile = useStore((s) => s.selectedProfile);
  const revision = useStore((s) => s.reviewRevision);
  const [open, setOpen] = useState(true);
  const [all, setAll] = useState(false);
  const [diffs, setDiffs] = useState<Map<string, { additions: number; deletions: number }>>();
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const listId = useId();

  // Aggregate files across all completed runs: deduplicate by path, keeping latest metadata and run tracking
  const aggregatedFiles = useMemo(() => {
    const map = new Map<string, AggregatedFile>();
    for (const run of runs) {
      for (const f of run.files) {
        const existing = map.get(f.path);
        if (existing) {
          existing.file = f;
          existing.latestRunId = run.run_id;
          if (!existing.runs.includes(run.run_id)) {
            existing.runs.push(run.run_id);
          }
        } else {
          map.set(f.path, {
            path: f.path,
            file: f,
            latestRunId: run.run_id,
            runs: [run.run_id],
          });
        }
      }
    }
    return [...map.values()];
  }, [runs]);

  const latestRunId = runs.at(-1)?.run_id || "";

  useEffect(() => {
    if (!open || !runs.length) return;
    let alive = true;
    setError("");
    const runIds = Array.from(new Set(runs.map((r) => r.run_id)));
    Promise.all(
      runIds.map((rid) =>
        apiRequest<RunFileSet>(
          "GET",
          `/sessions/${encodeURIComponent(sessionId)}/runs/${encodeURIComponent(rid)}/files`,
        ).catch(() => null),
      ),
    )
      .then((snapshots) => {
        if (!alive) return;
        const diffMap = new Map<string, { additions: number; deletions: number }>();
        for (const snap of snapshots) {
          if (!snap) continue;
          const summary = summarizeDiffs(snap.files);
          for (const [p, d] of summary.diffs.entries()) {
            const prev = diffMap.get(p) || { additions: 0, deletions: 0 };
            diffMap.set(p, {
              additions: prev.additions + d.additions,
              deletions: prev.deletions + d.deletions,
            });
          }
        }
        setDiffs(diffMap);
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [open, sessionId, runs, profile, revision, attempt]);

  const stats = useMemo(() => {
    if (!diffs) return undefined;
    let additions = 0;
    let deletions = 0;
    for (const d of diffs.values()) {
      additions += d.additions;
      deletions += d.deletions;
    }
    return { additions, deletions };
  }, [diffs]);

  const review = (runId: string, path?: string) =>
    useStore.getState().openRunReview(sessionId, runId, path);

  return (
    <article className="file-change-card">
      <div className="file-change-head">
        <button
          className="file-change-toggle"
          aria-expanded={open}
          aria-controls={listId}
          onClick={() => setOpen(!open)}
        >
          <Icon
            name="chevron-right"
            size={14}
            className={`file-change-chevron ${open ? "open" : ""}`}
          />
          <span className="file-change-title">
            {aggregatedFiles.length} 个文件已更改
          </span>
          {stats && (stats.additions > 0 || stats.deletions > 0) && (
            <span className="file-change-head-stats">
              {stats.additions > 0 && (
                <span className="change-added">+{stats.additions}</span>
              )}
              {stats.deletions > 0 && (
                <span className="change-removed">−{stats.deletions}</span>
              )}
            </span>
          )}
        </button>
        <button
          className="file-change-review-btn"
          onClick={() => review(latestRunId)}
          title="在工作区查看完整变更"
        >
          审查
          <Icon name="chevron-right" size={12} />
        </button>
      </div>
      {open && (
        <div id={listId} className="file-change-list">
          {aggregatedFiles
            .slice(0, all ? aggregatedFiles.length : 3)
            .map((item) => {
              const diff = diffs?.get(item.path);
              const { name, dir } = splitFilePath(item.path);
              return (
                <div
                  key={item.path}
                  className="file-change-row"
                  title={item.path}
                  onClick={() => review(item.latestRunId, item.path)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      review(item.latestRunId, item.path);
                    }
                  }}
                >
                  <div className="file-change-main">
                    <FileTypeBadge path={item.path} />
                    <span className="file-change-name">{name}</span>
                    {dir && <span className="file-change-dir">{dir}</span>}
                    {diff ? (
                      <span className="file-change-row-stats">
                        {diff.additions > 0 && (
                          <span className="change-added">+{diff.additions}</span>
                        )}
                        {diff.deletions > 0 && (
                          <span className="change-removed">−{diff.deletions}</span>
                        )}
                      </span>
                    ) : (
                      <span className="file-change-size">
                        {item.file.binary
                          ? "二进制"
                          : `${(item.file.size / 1024).toFixed(1)} KiB`}
                      </span>
                    )}
                    {item.runs.length > 1 && (
                      <span
                        className="file-edit-count"
                        title={`在此对话中修改了 ${item.runs.length} 次`}
                      >
                        {item.runs.length} 次修改
                      </span>
                    )}
                  </div>
                  <div className="file-change-actions">
                    <button
                      type="button"
                      className="file-row-action-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        review(item.latestRunId, item.path);
                      }}
                    >
                      审查
                    </button>
                  </div>
                </div>
              );
            })}
          {aggregatedFiles.length > 3 && (
            <button className="file-change-more" onClick={() => setAll(!all)}>
              {all
                ? "收起文件列表"
                : `展开另外 ${aggregatedFiles.length - 3} 个文件`}
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
  const profile = useStore((s) => s.selectedProfile);
  const revision = useStore((s) => s.reviewRevision);
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
      {changed.length > 0 && (
        <AggregatedRunCard
          key={key}
          runs={changed}
          sessionId={view.meta.id}
        />
      )}
      {error && (
        <div className="file-change-error" role="alert">
          文件变更加载失败
          <button onClick={() => setAttempt((n) => n + 1)}>重试</button>
        </div>
      )}
    </section>
  );
}
