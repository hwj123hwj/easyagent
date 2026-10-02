import { useState, useEffect } from "react";
import type {
  AcpToolKind,
  ToolCallContent,
  ToolCallStatus,
  ToolLocation,
} from "../types";
import { Icon, toolKindIcon } from "./Icon";
import { useT, type TFunc } from "../i18n/useT";
import { copyText } from "../client/clipboard";

/** Accent tone applied to the tool icon chip, by kind. */
function kindTone(kind: AcpToolKind): string {
  if (kind === "edit" || kind === "delete" || kind === "move") return "k-edit";
  if (kind === "execute") return "k-execute";
  return "";
}

/** Last path segment, for compact file labels. */
function baseName(p: string | undefined): string {
  if (!p) return "";
  const parts = p.split(/[/\\]/);
  return parts[parts.length - 1] || p;
}

/** Pick the first string-valued key present in the raw tool arguments. */
function strArg(
  raw: Record<string, unknown> | undefined,
  keys: string[],
): string | undefined {
  if (!raw) return undefined;
  for (const k of keys) {
    const v = raw[k];
    if (typeof v === "string" && v.length > 0) return v;
  }
  return undefined;
}

// ── line diff (LCS) ─────────────────────────────────────────────────────────
//
// The ACP `diff` content gives us the full old/new file text. Rendering every
// old line as deleted + every new line as added (the old behaviour) is noisy
// for small edits, so we compute a real line-level diff and only surface the
// changed lines (plus a little surrounding context). Capped so a giant file
// can't lock the UI — beyond the cap we fall back to the naive whole-file view.

interface DiffRow {
  type: "ctx" | "add" | "del" | "gap";
  oldNo?: number;
  newNo?: number;
  text: string;
}

const DIFF_CELL_CAP = 1_500; // max old*… lines either side before we bail to naive

function naiveRows(oldLines: string[], newLines: string[]): DiffRow[] {
  const rows: DiffRow[] = [];
  oldLines.forEach((t, i) => rows.push({ type: "del", oldNo: i + 1, text: t }));
  newLines.forEach((t, i) => rows.push({ type: "add", newNo: i + 1, text: t }));
  return rows;
}

function computeRows(
  oldText: string | null | undefined,
  newText: string,
): DiffRow[] {
  const oldLines = oldText ? oldText.split("\n") : [];
  const newLines = newText.split("\n");

  // New file (write): everything is an addition.
  if (oldLines.length === 0) {
    return newLines.map((t, i) => ({
      type: "add" as const,
      newNo: i + 1,
      text: t,
    }));
  }
  if (oldLines.length > DIFF_CELL_CAP || newLines.length > DIFF_CELL_CAP) {
    return naiveRows(oldLines, newLines);
  }

  // LCS DP over lines.
  const n = oldLines.length;
  const m = newLines.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () =>
    new Array<number>(m + 1).fill(0),
  );
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] =
        oldLines[i] === newLines[j]
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }

  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (oldLines[i] === newLines[j]) {
      rows.push({ type: "ctx", oldNo: i + 1, newNo: j + 1, text: oldLines[i] });
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      rows.push({ type: "del", oldNo: i + 1, text: oldLines[i] });
      i++;
    } else {
      rows.push({ type: "add", newNo: j + 1, text: newLines[j] });
      j++;
    }
  }
  while (i < n) rows.push({ type: "del", oldNo: i + 1, text: oldLines[i++] });
  while (j < m) rows.push({ type: "add", newNo: j + 1, text: newLines[j++] });
  return rows;
}

/**
 * Collapse long runs of unchanged context to a few lines on each side of a
 * change, the way a unified diff does, so the preview stays scannable.
 */
function collapseContext(rows: DiffRow[], pad = 3): DiffRow[] {
  const keep = new Array<boolean>(rows.length).fill(false);
  rows.forEach((r, idx) => {
    if (r.type !== "ctx") {
      for (
        let k = Math.max(0, idx - pad);
        k <= Math.min(rows.length - 1, idx + pad);
        k++
      ) {
        keep[k] = true;
      }
    }
  });
  const out: DiffRow[] = [];
  let hidden = 0;
  rows.forEach((r, idx) => {
    if (keep[idx]) {
      if (hidden > 0) {
        out.push({ type: "gap", text: String(hidden) });
        hidden = 0;
      }
      out.push(r);
    } else {
      hidden++;
    }
  });
  if (hidden > 0) out.push({ type: "gap", text: String(hidden) });
  return out;
}

/**
 * Build a concise, parameter-aware result summary — the desktop equivalent of
 * the VSCode plugin's `getToolResultSummary`. Driven by the ACP tool `kind`
 * plus `rawInput` and the result `content` (we don't get the raw tool name
 * over the wire, only the kind). Returns `null` when nothing reliable can be
 * derived, so callers fall back to the plain title.
 */
function toolSummary(
  t: TFunc,
  kind: AcpToolKind,
  raw: Record<string, unknown> | undefined,
  content: ToolCallContent[],
  status: ToolCallStatus,
  terminalOutput?: string,
): string | null {
  if (status !== "completed") return null;
  const text = content
    .map((c) => c.text)
    .filter((s): s is string => !!s)
    .join("\n");
  const diff = content.find((c) => c.diff)?.diff;

  switch (kind) {
    // NOTE: read_file / read_many_files / glob / ls / grep all collapse to the
    // ACP `search` kind (their core `Icon` maps there — see acpUtils
    // `iconToAcpKind`), so a single branch handles read- and search-style
    // results, disambiguated by the result text and which args are present.
    case "read":
    case "search": {
      const name = baseName(
        strArg(raw, ["file_path", "absolute_path", "path", "filename"]),
      );
      const q = strArg(raw, ["pattern", "regex", "query"]) ?? "";

      // read_file: "(N lines)" or "read lines: X-Y".
      const lines = text.match(/\((\d+)\s+lines?\)/i);
      if (lines)
        return t("tool.sum.read", { name: name || "file", n: lines[1] });
      const range = text.match(/read\s+lines:\s*(\d+-\d+)/i);
      if (range)
        return t("tool.sum.readLines", {
          name: name || "file",
          range: range[1],
        });

      // glob: "Found N matching file(s)". Checked before the grep branch
      // because "Found N matching" also satisfies a loose "match" regex.
      const files =
        text.match(/Found\s+(\d+)\s+matching\s+file/i) ??
        text.match(/Found\s+(\d+)\s+files?\b/i);
      if (files) return t("tool.sum.foundFiles", { n: files[1], q });
      // grep / search_file_content: "Found N match(es)". The `\b` keeps this
      // from also firing on glob's "matching".
      const matches = text.match(/Found\s+(\d+)\s+match(?:es)?\b/i);
      if (matches) {
        return q
          ? t("tool.sum.found", { n: matches[1], q })
          : t("tool.sum.foundNoQuery", { n: matches[1] });
      }
      if (/No matches found|No files found/i.test(text)) {
        return q
          ? t("tool.sum.noMatches", { q })
          : t("tool.sum.noMatchesNoQuery");
      }
      // ls: "Listed N item(s)".
      const listed = text.match(/Listed\s+(\d+)\s+item/i);
      if (listed) return t("tool.sum.listed", { n: listed[1] });

      // read_many_files: "--- path ---" section markers.
      const fileCount = (text.match(/--- .*? ---/g) || []).length;
      if (fileCount > 1) return t("tool.sum.readFiles", { n: fileCount });

      // Fallback. A search-style call (grep/glob) carries a pattern/query arg —
      // never label it "Read X" (that's only right for an actual file read).
      // The title already shows the pattern, so just omit the summary here.
      if (q) return null;
      return name ? t("tool.sum.readName", { name }) : null;
    }
    case "edit":
    case "move": {
      const name = baseName(diff?.path || strArg(raw, ["file_path", "path"]));
      if (!name) return null;
      if (diff && diff.oldText) {
        const { added, removed } = diffStats(diff.oldText, diff.newText);
        return `${t("tool.sum.edited", { name })}  +${added} −${removed}`;
      }
      if (diff && !diff.oldText) {
        return `${t("tool.sum.created", { name })}  +${diff.newText.split("\n").length}`;
      }
      return t("tool.sum.edited", { name });
    }
    case "delete": {
      const name = baseName(diff?.path || strArg(raw, ["file_path", "path"]));
      return name ? t("tool.sum.deleted", { name }) : null;
    }
    case "execute": {
      const src = `${text}\n${terminalOutput ?? ""}`;
      const exit = src.match(/exit\s*code:?\s*(\d+)/i);
      if (exit) return t("tool.sum.exit", { code: exit[1] });
      return null;
    }
    default:
      return null;
  }
}

/** Count changed lines for the edit summary chip. */
function diffStats(
  oldText: string,
  newText: string,
): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const r of computeRows(oldText, newText)) {
    if (r.type === "add") added++;
    else if (r.type === "del") removed++;
  }
  return { added, removed };
}

export interface ToolCallProps {
  title: string;
  toolKind: AcpToolKind;
  status: ToolCallStatus;
  locations?: ToolLocation[];
  content: ToolCallContent[];
  terminalOutput?: string;
  rawInput?: Record<string, unknown>;
  defaultOpen: boolean;
  details?: Record<string, unknown>;
  onOpenFile?: (path: string) => void;
}

export function ToolCall({
  title,
  toolKind,
  status,
  locations,
  content,
  terminalOutput,
  rawInput,
  defaultOpen,
  details,
  onOpenFile,
}: ToolCallProps) {
  const [open, setOpen] = useState(defaultOpen);
  const [copyStatus, setCopyStatus] = useState("");
  useEffect(() => {
    if (status === "failed") setOpen(true);
  }, [status]);
  const t = useT();
  const hasBody =
    !!rawInput ||
    !!details ||
    content.some((c) => c.text || c.diff) ||
    !!terminalOutput ||
    (locations?.length ?? 0) > 0;
  const running = status === "pending" || status === "in_progress";
  const summary = toolSummary(
    t,
    toolKind,
    rawInput,
    content,
    status,
    terminalOutput,
  );

  return (
    <div className={`tool tool-${status}`}>
      <button
        type="button"
        className="tool-head"
        aria-expanded={open}
        disabled={!hasBody}
        onClick={() => setOpen(!open)}
      >
        <span className={`tool-ico ${kindTone(toolKind)}`}>
          <Icon name={toolKindIcon(toolKind)} size={15} />
        </span>
        <span className="tool-title">{title}</span>
        {summary && <span className="tool-summary">{summary}</span>}
        <span className={`tool-status ${status}`}>
          {running && <Icon name="loader" size={11} spin />}
          {t(`tool.status.${status}` as any)}
        </span>
        {hasBody && (
          <span className="tool-chevron">
            <Icon name={open ? "chevron-down" : "chevron-right"} size={15} />
          </span>
        )}
      </button>
      {open && hasBody && (
        <div className="tool-body">
          {rawInput && (
            <details className="tool-arguments">
              <summary>调用参数</summary>
              <pre>{JSON.stringify(rawInput, null, 2)}</pre>
            </details>
          )}
          {details && (
            <details className="tool-arguments">
              <summary>执行详情</summary>
              <pre>{JSON.stringify(details, null, 2)}</pre>
            </details>
          )}
          {locations && locations.length > 0 && (
            <div className="tool-locations">
              {locations.map((l, i) => (
                <span
                  key={i}
                  className="tool-loc"
                  style={{ cursor: onOpenFile ? "pointer" : "default" }}
                  onClick={() => onOpenFile?.(l.path)}
                >
                  <Icon name="file" size={12} />
                  {l.path}
                  {l.line ? `:${l.line}` : ""}
                </span>
              ))}
            </div>
          )}
          {content.map((c, i) => {
            if (c.diff) {
              return (
                <DiffPreview
                  key={i}
                  path={c.diff.path}
                  oldText={c.diff.oldText}
                  newText={c.diff.newText}
                />
              );
            }
            if (c.text && !terminalOutput) {
              return (
                <div key={i} className="tool-text">
                  {c.text}
                </div>
              );
            }
            return null;
          })}
          {terminalOutput && <div className="console">{terminalOutput}</div>}
          <button
            className="tool-output-copy btn"
            onClick={() =>
              void copyText(
                terminalOutput || content.map((c) => c.text || "").join("\n"),
              )
                .then(() => setCopyStatus("已复制"))
                .catch((error) => setCopyStatus(error.message))
            }
          >
            <Icon name="copy" size={12} />
            复制输出
          </button>
          {copyStatus && <span role="status">{copyStatus}</span>}
        </div>
      )}
    </div>
  );
}

function DiffPreview({
  path,
  oldText,
  newText,
}: {
  path: string;
  oldText?: string | null;
  newText: string;
}) {
  const t = useT();
  const isEdit = !!oldText;
  const rows = collapseContext(computeRows(oldText, newText));
  const added = rows.filter((r) => r.type === "add").length;
  const removed = rows.filter((r) => r.type === "del").length;

  return (
    <div className="diff-preview">
      <div className="diff-preview-head">
        <Icon name={isEdit ? "edit" : "file"} size={13} />
        <span className="diff-preview-verb">
          {isEdit ? t("diff.edit") : t("diff.write")}
        </span>
        <span className="diff-preview-path">{path}</span>
        <span className="diff-preview-stat">
          {added > 0 && <span className="add">+{added}</span>}
          {removed > 0 && <span className="del">−{removed}</span>}
        </span>
      </div>
      <div className="diff-preview-body">
        {rows.map((r, i) => {
          if (r.type === "gap") {
            return (
              <div key={i} className="diff-line gap">
                <span className="diff-gut" />
                <span className="diff-gut" />
                <span className="diff-mark" />
                <span className="diff-code">
                  {t("diff.moreLines", { n: r.text })}
                </span>
              </div>
            );
          }
          return (
            <div key={i} className={`diff-line ${r.type}`}>
              <span className="diff-gut">{r.oldNo ?? ""}</span>
              <span className="diff-gut">{r.newNo ?? ""}</span>
              <span className="diff-mark">
                {r.type === "add" ? "+" : r.type === "del" ? "-" : " "}
              </span>
              <span className="diff-code">{r.text}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
