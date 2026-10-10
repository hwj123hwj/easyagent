import { structuredPatch } from "diff";
import type { RunFileChange } from "./file-delivery";

export interface DiffLine {
  kind: "context" | "added" | "removed" | "hunk" | "note";
  text: string;
  before?: number;
  after?: number;
}
export interface FileDiff {
  additions: number;
  deletions: number;
  lines: DiffLine[];
  truncated: boolean;
}

// Bound both calculation and rendering. A missing result means unavailable,
// never zero changes. Large/binary snapshots remain reviewable via metadata.
export function fileDiff(file: RunFileChange): FileDiff | undefined {
  if (file.binary || (file.before === undefined && file.after === undefined))
    return;
  const before = file.before ?? "",
    after = file.after ?? "";
  if (before.length + after.length > 400_000) return;
  const patch = structuredPatch(
    file.path,
    file.path,
    before,
    after,
    undefined,
    undefined,
    {
      context: 3,
      maxEditLength: 2000,
      timeout: 20,
    },
  );
  if (!patch) return;
  let additions = 0,
    deletions = 0;
  const lines: DiffLine[] = [];
  let truncated = false;
  const push = (line: DiffLine) => {
    if (lines.length < 800) lines.push(line);
    else truncated = true;
  };
  for (const hunk of patch.hunks) {
    let oldLine = hunk.oldStart,
      newLine = hunk.newStart;
    push({
      kind: "hunk",
      text: `@@ −${hunk.oldStart},${hunk.oldLines} +${hunk.newStart},${hunk.newLines} @@`,
    });
    for (const line of hunk.lines) {
      if (line.startsWith("+")) {
        additions++;
        push({ kind: "added", text: line.slice(1), after: newLine++ });
      } else if (line.startsWith("-")) {
        deletions++;
        push({ kind: "removed", text: line.slice(1), before: oldLine++ });
      } else if (line.startsWith(" ")) {
        push({
          kind: "context",
          text: line.slice(1),
          before: oldLine++,
          after: newLine++,
        });
      } else push({ kind: "note", text: "文件末尾无换行" });
    }
  }
  return { additions, deletions, lines, truncated };
}

export function summarizeDiffs(files: RunFileChange[]) {
  const diffs = new Map<string, FileDiff>();
  const deadline = performance.now() + 80;
  for (const file of files) {
    if (performance.now() >= deadline) break;
    const diff = fileDiff(file);
    if (diff) diffs.set(file.path, diff);
  }
  return {
    diffs,
    additions: [...diffs.values()].reduce((sum, d) => sum + d.additions, 0),
    deletions: [...diffs.values()].reduce((sum, d) => sum + d.deletions, 0),
    partial: diffs.size !== files.length,
  };
}

export function changeLabel(file: RunFileChange) {
  return file.undone
    ? "已撤回"
    : file.kind === "created"
      ? "新增"
      : file.kind === "deleted"
        ? "删除"
        : "修改";
}

export function runTime(value?: string) {
  if (!value || Number.isNaN(Date.parse(value))) return "";
  return new Date(value).toLocaleString("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}
