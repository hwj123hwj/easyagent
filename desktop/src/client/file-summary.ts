import type { RunFileChange, RunFileSet } from "./file-delivery";

export interface AggregatedFile {
  path: string;
  file: RunFileChange;
  latestRunId: string;
  runs: string[];
}

// Historical task changes, not the workspace's current Git status.
// Keep first-before → final-after; discontinuous content has no net stat.
export function aggregateRunFiles(runs: RunFileSet[]): AggregatedFile[] {
  const files = new Map<string, AggregatedFile>();
  for (const run of runs) {
    for (const file of run.files) {
      const key = `${run.workspace}\0${file.path}`;
      const existing = files.get(key);
      if (!existing) {
        files.set(key, { path: file.path, file: { ...file }, latestRunId: run.run_id, runs: [run.run_id] });
        continue;
      }
      const discontinuous = existing.file.after !== file.before || existing.file.attribution !== file.attribution;
      existing.file = { ...file, before: existing.file.before, binary: existing.file.binary || file.binary || discontinuous };
      existing.latestRunId = run.run_id;
      if (!existing.runs.includes(run.run_id)) existing.runs.push(run.run_id);
    }
  }
  return [...files.values()];
}

export function workspaceFileRuns(runs: RunFileSet[], workspace: string): RunFileSet[] {
  const normalized = (path: string) => path.replace(/\\/g, "/").replace(/\/+$/, "");
  return runs.filter(run => run.complete && normalized(run.workspace) === normalized(workspace))
    .map(run => {
      // Older servers filtered details, but still returned phantom single-sided
      // files in summaries. Apply the same rule before displaying their count.
      const incomplete = run.incomplete || run.skipped?.some(reason => reason === "扫描达到上限" || reason === "快照达到文件或总大小上限");
      return incomplete ? { ...run, incomplete: true, files: run.files.filter(file => file.kind === "modified") } : run;
    })
    .filter(run => run.files.length).slice(-20);
}
