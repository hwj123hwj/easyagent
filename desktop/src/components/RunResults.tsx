import { useEffect, useState } from "react";
import { apiRequest, useStore, type SessionView } from "../store";
import { exportFile, type RunFileSet } from "../client/file-delivery";
import { Icon } from "./Icon";
export function RunResults({ view }: { view: SessionView }) {
  const profile = useStore((s) => s.selectedProfile);
  const [runs, setRuns] = useState<RunFileSet[]>([]),
    [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    setRuns([]);
    void apiRequest<RunFileSet[]>(
      "GET",
      `/sessions/${encodeURIComponent(view.meta.id)}/run-files`,
    )
      .then((data) => {
        if (alive) setRuns(data);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [profile, view.meta.id, view.run?.state]);
  const changed = runs.filter(
    (run) => run.complete && run.files.some((file) => file.kind !== "deleted"),
  );
  if (!changed.length) return null;
  return (
    <section className="run-results" aria-label="任务生成的文件">
      {changed.slice(-20).map((run) => (
        <details key={run.run_id} open={run.run_id === view.run?.run_id}>
          <summary>
            <Icon name="file" size={15} />
            本轮文件 · {run.files.filter((f) => f.kind !== "deleted").length}
            <small>
              {run.started_at && new Date(run.started_at).toLocaleTimeString()}
            </small>
          </summary>
          {run.files
            .filter((file) => file.kind !== "deleted")
            .map((file) => (
              <div className="run-file-row" key={file.path}>
                <button
                  className="run-file-name"
                  title={run.workspace + "/" + file.path}
                  onClick={() =>
                    useStore
                      .getState()
                      .openFileTab(run.workspace + "/" + file.path)
                  }
                >
                  <Icon name="file" size={15} />
                  {file.path}
                </button>
                <small>
                  {file.kind === "created" ? "新增" : "修改"} ·{" "}
                  {(file.size / 1024).toFixed(1)} KiB
                </small>
                <button
                  className="btn"
                  onClick={() =>
                    void exportFile(
                      view.meta.id,
                      run.workspace + "/" + file.path,
                    ).catch((e) => setError(e.message))
                  }
                >
                  导出
                </button>
              </div>
            ))}
          <p className="settings-note">
            {run.files.some((f) => f.attribution === "observed")
              ? "包含任务期间观察到的文件，来源以审阅页为准。"
              : "来自文件工具的执行记录。"}
          </p>
          <button
            className="btn"
            onClick={() => useStore.getState().openWorkspaceView("review")}
          >
            审阅与撤回
          </button>
        </details>
      ))}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
