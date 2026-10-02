import {
  Fragment,
  useState,
  useCallback,
  useMemo,
  type ReactNode,
} from "react";
import { Icon } from "./Icon";
import { Mermaid } from "./Mermaid";
import { copyText } from "../client/clipboard";

/**
 * A compact, dependency-free Markdown renderer. Handles the subset that shows up
 * in agent transcripts: fenced code blocks, inline code, headings, bold/italic,
 * links, ordered/unordered lists, and paragraphs. Everything is escaped by
 * React (we only build elements, never dangerouslySetInnerHTML).
 *
 * `basePath` — when provided, relative links like `[text](file.md)` are
 * resolved against this directory and dispatched as `open-file` events so they
 * open in the file viewer instead of a new browser window.
 */
export function Markdown({
  text,
  basePath,
}: {
  text: string;
  basePath?: string;
}) {
  const blocks = useMemo(() => renderBlocks(text, basePath), [text, basePath]);
  return <div className="md">{blocks}</div>;
}

/** Resolve a possibly-relative href to an absolute path. */
function resolveHref(href: string, basePath?: string): string {
  // Already absolute path or an external URL — return as-is.
  if (
    href.startsWith("/") ||
    /^https?:\/\//i.test(href) ||
    href.startsWith("#")
  ) {
    return href;
  }
  if (!basePath) return href;
  // Resolve relative to the markdown file's directory.
  const dir = basePath.replace(/\/[^/]*$/, "");
  // Strip any leading ./
  const clean = href.replace(/^\.\//, "");
  // Handle ../
  const parts = dir.split("/");
  for (const seg of clean.split("/")) {
    if (seg === "..") parts.pop();
    else if (seg !== ".") parts.push(seg);
  }
  return parts.join("/");
}

/**
 * Code block with copy button and tap-to-expand for long blocks on mobile.
 * Keeps the same DOM structure (`pre > code`) for CSS compatibility, but wraps
 * it in a positioned container with a copy button and optional collapse.
 */
const COLLAPSE_THRESHOLD = 12; // lines before we offer expand/collapse on mobile

function CodeBlock({ lang, code }: { lang: string; code: string }) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const [expanded, setExpanded] = useState(false);
  const lineCount = code.split("\n").length;
  const shouldCollapse = lineCount > COLLAPSE_THRESHOLD;

  const onCopy = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      void copyText(code)
        .then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1400);
        })
        .catch(() => {
          setCopyError("复制失败，请选中代码手动复制");
        });
    },
    [code],
  );

  return (
    <div
      className={`md-code-block ${shouldCollapse && !expanded ? "collapsed" : ""}`}
    >
      <button
        className="md-code-copy"
        onClick={onCopy}
        aria-label="Copy code"
        title="Copy"
      >
        <Icon name={copied ? "check" : "copy"} size={13} />
      </button>
      {copyError && (
        <span className="inline-error" role="alert">
          {copyError}
        </span>
      )}
      <pre>
        <code data-lang={lang}>{code}</code>
      </pre>
      {shouldCollapse && !expanded && (
        <button
          className="md-code-expand"
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(true);
          }}
        >
          +{lineCount - COLLAPSE_THRESHOLD} more lines
        </button>
      )}
    </div>
  );
}

function renderBlocks(src: string, basePath?: string): ReactNode[] {
  const out: ReactNode[] = [];
  const lines = src.split("\n");
  let i = 0;
  let key = 0;

  while (i < lines.length) {
    const line = lines[i];

    // fenced code block
    const fence = line.match(/^```([^`]*)$/);
    if (fence) {
      const lang = fence[1].trim().split(/\s+/)[0];
      const buf: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        buf.push(lines[i]);
        i++;
      }
      i++; // skip closing fence
      if (lang.toLowerCase() === "mermaid") {
        out.push(<Mermaid key={key++} code={buf.join("\n")} />);
      } else {
        out.push(<CodeBlock key={key++} lang={lang} code={buf.join("\n")} />);
      }
      continue;
    }

    // heading
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      const level = heading[1].length;
      const Tag = `h${Math.min(level, 3)}` as "h1" | "h2" | "h3";
      out.push(<Tag key={key++}>{renderInline(heading[2], basePath)}</Tag>);
      i++;
      continue;
    }

    // unordered list
    if (/^\s*[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*]\s+/, ""));
        i++;
      }
      out.push(
        <ul key={key++}>
          {items.map((it, idx) => (
            <li key={idx}>{renderInline(it, basePath)}</li>
          ))}
        </ul>,
      );
      continue;
    }

    // ordered list
    if (/^\s*\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*\d+\.\s+/, ""));
        i++;
      }
      out.push(
        <ol key={key++}>
          {items.map((it, idx) => (
            <li key={idx}>{renderInline(it, basePath)}</li>
          ))}
        </ol>,
      );
      continue;
    }

    // GFM table: a header row immediately followed by a delimiter row.
    if (isTableStart(lines, i)) {
      const header = splitTableRow(lines[i]);
      const aligns = splitTableRow(lines[i + 1]).map(parseAlign);
      i += 2;
      const rows: string[][] = [];
      while (
        i < lines.length &&
        lines[i].trim() !== "" &&
        lines[i].includes("|")
      ) {
        rows.push(splitTableRow(lines[i]));
        i++;
      }
      const align = (col: number) => aligns[col] ?? undefined;
      out.push(
        <div className="md-table-wrap" key={key++}>
          <table>
            <thead>
              <tr>
                {header.map((cell, idx) => (
                  <th key={idx} style={{ textAlign: align(idx) }}>
                    {renderInline(cell, basePath)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, ridx) => (
                <tr key={ridx}>
                  {header.map((_, idx) => (
                    <td key={idx} style={{ textAlign: align(idx) }}>
                      {renderInline(row[idx] ?? "", basePath)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }

    // blank line
    if (line.trim() === "") {
      i++;
      continue;
    }

    // paragraph (consume consecutive non-blank, non-special lines)
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !/^```/.test(lines[i]) &&
      !/^(#{1,4})\s+/.test(lines[i]) &&
      !/^\s*[-*]\s+/.test(lines[i]) &&
      !/^\s*\d+\.\s+/.test(lines[i]) &&
      !isTableStart(lines, i)
    ) {
      para.push(lines[i]);
      i++;
    }
    if (!para.length) para.push(lines[i++]); // Incomplete streaming syntax must always advance.
    out.push(<p key={key++}>{renderInline(para.join("\n"), basePath)}</p>);
  }

  return out;
}

/**
 * Split a markdown table row into trimmed cells. Strips one optional leading
 * and trailing pipe and respects escaped pipes (`\|`).
 */
function splitTableRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|")) s = s.slice(0, -1);
  const cells: string[] = [];
  let cur = "";
  for (let k = 0; k < s.length; k++) {
    if (s[k] === "\\" && s[k + 1] === "|") {
      cur += "|";
      k++;
      continue;
    }
    if (s[k] === "|") {
      cells.push(cur);
      cur = "";
      continue;
    }
    cur += s[k];
  }
  cells.push(cur);
  return cells.map((c) => c.trim());
}

/** A GFM delimiter row, e.g. `|---|:--:|` — every cell is `:?-+:?`. */
function isDelimiterRow(line: string): boolean {
  if (!line.includes("-") || !line.includes("|")) return false;
  const cells = splitTableRow(line);
  return cells.length > 0 && cells.every((c) => /^:?-{1,}:?$/.test(c));
}

/** Column alignment from a delimiter cell. */
function parseAlign(cell: string): "left" | "center" | "right" | undefined {
  const left = cell.startsWith(":");
  const right = cell.endsWith(":");
  if (left && right) return "center";
  if (right) return "right";
  if (left) return "left";
  return undefined;
}

/** True when `lines[i]` is a table header followed by a delimiter row. */
function isTableStart(lines: string[], i: number): boolean {
  return (
    i + 1 < lines.length &&
    lines[i].includes("|") &&
    lines[i].trim() !== "" &&
    isDelimiterRow(lines[i + 1])
  );
}

/** Inline: `code`, **bold**, *italic*, [text](url). */
function renderInline(text: string, basePath?: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  // Split on inline code first to avoid formatting inside it.
  const parts = text.split(/(`[^`]+`)/g);
  let key = 0;
  for (const part of parts) {
    if (part.startsWith("`") && part.endsWith("`") && part.length > 1) {
      const codeContent = part.slice(1, -1);
      // Check if the code content is a file path
      if (isFilePath(codeContent)) {
        nodes.push(
          <code
            key={key++}
            className="file-path-link"
            onClick={(e) => {
              e.preventDefault();
              const resolved = resolveHref(codeContent, basePath);
              window.dispatchEvent(
                new CustomEvent("open-file", { detail: { path: resolved } }),
              );
            }}
            style={{ cursor: "pointer", textDecoration: "underline" }}
          >
            {codeContent}
          </code>,
        );
      } else {
        nodes.push(<code key={key++}>{codeContent}</code>);
      }
    } else {
      nodes.push(
        <Fragment key={key++}>{renderEmphasis(part, basePath)}</Fragment>,
      );
    }
  }
  return nodes;
}

// Check if a string looks like a file path.
// Matches: paths with known extensions, directory paths (ending with /),
// and paths with ≥2 segments but no extension (e.g. /Users/.../work/something).
function isFilePath(text: string): boolean {
  // Strip trailing slash for analysis
  const stripped = text.replace(/\/+$/, "");
  // Must start with / or ~/
  if (!/^~?\//.test(stripped)) return false;
  // Exclude URLs
  if (/^https?:\/\//i.test(text)) return false;
  // Known extensions (covers most source/config/doc files)
  const extRe =
    /^~?\/[^\s]+\.(md|txt|json|js|ts|go|py|yaml|yml|toml|xml|html|css|sh|bash|rs|java|c|cpp|h|rb|php|sql|graphql|proto|tf|vue|svelte|jsx|tsx|mdx|csv|log|cfg|conf|ini|env|lock|sum|mod)$/i;
  if (extRe.test(text)) return true;
  // Directory path (ends with / and has >=2 segments)
  if (/\/$/.test(text) && stripped.split("/").filter(Boolean).length >= 2)
    return true;
  // File/dir with >=2 segments, no extension in last segment
  const lastSegment = stripped.split("/").pop() || "";
  if (
    stripped.split("/").filter(Boolean).length >= 2 &&
    !lastSegment.includes(".")
  )
    return true;
  return false;
}

function renderEmphasis(text: string, basePath?: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  // Image `![alt](src)` must come before the link alternative so the leading
  // `!` is consumed as part of the image rather than left as literal text.
  const re =
    /(!\[[^\]]*\]\([^)]+\)|\*\*[^*]+\*\*|\*[^*]+\*|\[[^\]]+\]\([^)]+\))/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let key = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last)
      nodes.push(
        ...renderTextWithFilePaths(text.slice(last, m.index), key, basePath),
      );
    key += 100; // reserve keyspace for path nodes
    const token = m[0];
    if (token.startsWith("![")) {
      const im = token.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
      if (im) {
        // The URL part may carry an optional CommonMark title: `(src "title")`.
        const inner = im[2].trim();
        const titleMatch = inner.match(/^(\S+)\s+["'(](.*)["']$/);
        const src = titleMatch ? titleMatch[1] : inner;
        const title = titleMatch ? titleMatch[2] : undefined;
        nodes.push(
          <img
            key={key++}
            className="md-img"
            src={src}
            alt={im[1]}
            title={title}
          />,
        );
      } else {
        nodes.push(<Fragment key={key++}>{token}</Fragment>);
      }
    } else if (token.startsWith("**")) {
      nodes.push(<strong key={key++}>{token.slice(2, -2)}</strong>);
    } else if (token.startsWith("*")) {
      nodes.push(<em key={key++}>{token.slice(1, -1)}</em>);
    } else {
      const lm = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      if (lm) {
        const href = lm[2];
        const isExternal = /^https?:\/\//i.test(href);
        const isAnchor = href.startsWith("#");

        if (isExternal) {
          // External link: open in system browser
          nodes.push(
            <a
              key={key++}
              href={href}
              onClick={(e) => {
                e.preventDefault();
                if (window.piAPI) void window.piAPI.openExternal(href);
                else window.open(href, "_blank", "noopener,noreferrer");
              }}
            >
              {lm[1]}
            </a>,
          );
        } else if (isAnchor) {
          // Anchor link: just render as-is (in-page navigation)
          nodes.push(
            <a key={key++} href={href}>
              {lm[1]}
            </a>,
          );
        } else {
          // Relative file link: resolve and open in file viewer
          const resolved = resolveHref(href, basePath);
          nodes.push(
            <a
              key={key++}
              href="#"
              className="md-file-link"
              title={resolved}
              onClick={(e) => {
                e.preventDefault();
                window.dispatchEvent(
                  new CustomEvent("open-file", { detail: { path: resolved } }),
                );
              }}
            >
              {lm[1]}
            </a>,
          );
        }
      } else {
        nodes.push(<Fragment key={key++}>{token}</Fragment>);
      }
    }
    last = m.index + token.length;
  }
  if (last < text.length)
    nodes.push(...renderTextWithFilePaths(text.slice(last), key, basePath));
  return nodes;
}

// Scan plain text for file paths and make them clickable.
const PATH_RE = /(\/(?:[^\s\n]*\/)+[^\s\n/.]+)/g;

function renderTextWithFilePaths(
  text: string,
  baseKey: number,
  basePath?: string,
): ReactNode[] {
  const nodes: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  let key = baseKey;
  PATH_RE.lastIndex = 0;
  while ((m = PATH_RE.exec(text)) !== null) {
    const path = m[0];
    if (!isFilePath(path)) continue;
    if (m.index > last) nodes.push(text.slice(last, m.index));
    nodes.push(
      <code
        key={key++}
        className="file-path-link"
        onClick={(e) => {
          e.preventDefault();
          const resolved = resolveHref(path, basePath);
          window.dispatchEvent(
            new CustomEvent("open-file", { detail: { path: resolved } }),
          );
        }}
        style={{ cursor: "pointer", textDecoration: "underline" }}
      >
        {path}
      </code>,
    );
    last = m.index + path.length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  if (nodes.length === 0) return [text]; // no paths found, return plain string
  return nodes;
}
