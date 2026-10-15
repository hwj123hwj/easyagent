export interface FileWorkspace {
  fileTabs: string[];
  activeFileTab?: string;
  positions: Record<string, { top: number; preview: boolean }>;
}
const KEY = "easyagent.desktop.files.v1";
const empty = (): FileWorkspace => ({ fileTabs: [], positions: {} });
function scope(host: string, workspace: string) {
  return JSON.stringify([host, workspace]);
}
export function readWorkspaceFiles(
  host: string,
  workspace: string,
): FileWorkspace {
  if (!host || !workspace) return empty();
  try {
    const entry = JSON.parse(localStorage.getItem(KEY) || "{}")[
      scope(host, workspace)
    ];
    if (!entry) return empty();
    const tabs = Array.isArray(entry.fileTabs)
      ? entry.fileTabs
          .filter(
            (p: unknown): p is string =>
              typeof p === "string" &&
              p.startsWith(workspace.replace(/\/$/, "") + "/") &&
              !p
                .split("/")
                .some((segment) => segment === ".." || segment === "."),
          )
          .slice(-20)
      : [];
    const positions: FileWorkspace["positions"] = {};
    for (const path of tabs) {
      const position = entry.positions?.[path];
      if (
        position &&
        typeof position.top === "number" &&
        Number.isFinite(position.top) &&
        position.top >= 0
      )
        positions[path] = {
          top: position.top,
          preview: position.preview !== false,
        };
    }
    return {
      fileTabs: tabs,
      activeFileTab: tabs.includes(entry.activeFileTab)
        ? entry.activeFileTab
        : tabs.at(-1),
      positions,
    };
  } catch {
    return empty();
  }
}
export function saveWorkspaceFiles(
  host: string,
  workspace: string,
  patch: Partial<FileWorkspace>,
) {
  if (!host || !workspace) return;
  try {
    const entries = JSON.parse(localStorage.getItem(KEY) || "{}");
    if (!entries || typeof entries !== "object" || Array.isArray(entries))
      return;
    entries[scope(host, workspace)] = {
      ...readWorkspaceFiles(host, workspace),
      ...patch,
    };
    const keys = Object.keys(entries);
    for (const key of keys.slice(0, Math.max(0, keys.length - 30)))
      delete entries[key];
    localStorage.setItem(KEY, JSON.stringify(entries));
  } catch {}
}
export function saveFilePosition(
  host: string,
  workspace: string,
  path: string,
  top: number,
  preview: boolean,
) {
  const entry = readWorkspaceFiles(host, workspace);
  saveWorkspaceFiles(host, workspace, {
    positions: { ...entry.positions, [path]: { top, preview } },
  });
}
