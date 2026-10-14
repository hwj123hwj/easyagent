import { useEffect, useId, useMemo, useState } from "react";
import { apiRequest, useStore, type SessionView } from "../store";
import type { RunFileSet, RunFileChange } from "../client/file-delivery";
import { summarizeDiffs } from "../client/file-review";
import { aggregateRunFiles, workspaceFileRuns } from "../client/file-summary";
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

function AggregatedRunCard({
  runs,
  sessionId,
  source,
}: {
  runs: RunFileSet[];
  sessionId: string;
  source: "file_tool" | "observed";
}) {
  const profile = useStore((s) => s.selectedProfile);
  const [open, setOpen] = useState(source === "file_tool");
  const [all, setAll] = useState(false);
  const [diffs, setDiffs] = useState<Map<string, { additions: number; deletions: number }>>();
  const [incomplete, setIncomplete] = useState(false);
  const [partial, setPartial] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const listId = useId();

  const aggregatedFiles = useMemo(() => aggregateRunFiles(runs)
    .filter(item => (item.file.attribution === "file_tool" ? "file_tool" : "observed") === source), [runs, source]);

  const latestRunId = [...runs].reverse().find(run => aggregatedFiles.some(file => file.latestRunId === run.run_id))?.run_id || "";

  useEffect(() => {
    if (!open || !aggregatedFiles.length) return;
    let alive = true;
    setError("");
    setDiffs(undefined);
    const runIds = Array.from(new Set(runs.map((r) => r.run_id)));
    Promise.all(
      runIds.map((rid) =>
        apiRequest<RunFileSet>(
          "GET",
          `/sessions/${encodeURIComponent(sessionId)}/runs/${encodeURIComponent(rid)}/files`,
        ),
      ),
    )
      .then((snapshots) => {
        if (!alive) return;
        const files = aggregateRunFiles(snapshots)
          .filter(item => (item.file.attribution === "file_tool" ? "file_tool" : "observed") === source);
        const summary = summarizeDiffs(files.map(item => item.file));
        setIncomplete(snapshots.some(snap => snap.incomplete));
        setPartial(summary.partial);
        setDiffs(summary.diffs);
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [open, sessionId, runs, profile, attempt, source, aggregatedFiles]);

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

  if (source === "observed" && !aggregatedFiles.length) return null;

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
            {source === "file_tool"
              ? `文件工具累计修改 ${aggregatedFiles.length} 个文件`
              : `其他工作区变化：${aggregatedFiles.length} 个文件`}
          </span>
          {stats && <ChangeStats additions={stats.additions} deletions={stats.deletions} partial={partial} />}
        </button>
        <button
          className="file-change-review-btn"
          disabled={!latestRunId}
          onClick={() => review(latestRunId)}
          title="查看最近任务的变更；点击文件可查看其对应任务"
        >
          审查
          <Icon name="chevron-right" size={12} />
        </button>
      </div>
      {open && (
        <div id={listId} className="file-change-list">
          <p className="file-change-scope" role="note">
            当前工作区最近 {runs.length} 个有变更的任务，按路径去重，属于历史记录。
            {source === "file_tool" ? "只计可确认的文件工具修改；行数按首次修改前与最后修改后计算。" : "Bash、外部进程等来源未确认的变化单独展示，不计入文件工具修改数量。"}
          </p>
          {(incomplete || runs.some(run => run.incomplete)) && (
            <p className="file-change-incomplete" role="note">
              ⚠️ 工作区快照不完整：无法核对的单侧增删已过滤，统计可能遗漏实际改动。
            </p>
          )}
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
                    <span className="file-edit-count">
                      {item.file.undone ? "已撤回" : item.file.attribution === "file_tool" ? "文件工具修改" : "来源未确认"}
                    </span>
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
                        title={`在 ${item.runs.length} 个任务快照中记录到变化${item.file.undone ? "；最新任务已撤回" : ""}`}
                      >
                        {item.runs.length} 个任务
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
    if (view.meta.id === "__new__") {
      setResult(undefined);
      return;
    }
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

  // Keep this reference stable: parent scroll/stream renders must not restart
  // the card's requests and diff calculation for every completed run.
  const changed = useMemo(() => workspaceFileRuns(result?.key === key ? result.runs : [], view.meta.cwd), [result, key, view.meta.cwd]);

  if (!changed.length && !error) return null;

  return (
    <section className="run-results" aria-label="任务文件变更">
      {changed.length > 0 && (
        <AggregatedRunCard
          key={key + ":" + view.meta.cwd + ":file_tool"}
          runs={changed}
          sessionId={view.meta.id}
          source="file_tool"
        />
      )}
      {changed.some(run => run.files.some(file => file.attribution !== "file_tool")) && (
        <AggregatedRunCard key={key + ":" + view.meta.cwd + ":observed"} runs={changed} sessionId={view.meta.id} source="observed" />
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
