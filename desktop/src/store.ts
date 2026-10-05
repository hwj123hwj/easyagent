import { sanitizeInputDrafts } from "./client/input-drafts";
import {
  readWorkspaceFiles,
  saveWorkspaceFiles,
} from "./client/workspace-files";
/**
 * store.ts — Single source of truth for the easyagent desktop renderer.
 *
 * Electron proxies authenticated REST + WebSocket through the main process.
 * Browser preview uses the same typed projection with an in-memory token.
 */

import { create } from "zustand";
import { type Lang, loadStoredLang, persistLang, translate } from "./i18n/i18n";
import { type ThemeMode, loadStoredTheme, persistTheme } from "./theme";
import { deriveTitleFromMessage } from "./sessionTitle";
import { getStoredServerUrl, setStoredServerUrl } from "./platform";
import { commands as localCommands, type Command } from "./client/commands";
import { AgentTransport } from "./client/transport";
import {
  emptyProjection,
  historyProjection,
  reduceEnvelope,
} from "./client/reducer";
import type {
  ChatItem,
  AccessMode,
  SessionInfo,
  ConnectionProfile,
  Envelope,
  RunProjection,
  MessageQueue,
  DraftInputs,
  PromptInputs,
} from "./client/protocol";
import { isActiveRun } from "./client/protocol";
import { timestampMillis } from "./client/timestamps";
export type { ChatItem } from "./client/protocol";
import type {
  AcpToolKind,
  DesktopSessionEvent,
  GitFileDiff,
  ModelInfo,
  ModelCatalog,
  PlanEntry,
  SessionMeta,
  SessionRunStatus,
  ToolCallContent,
  ToolCallStatus,
  ToolLocation,
  UpdateInfo,
  UpdateState,
} from "./types";

// ── REST API helpers ──────────────────────────────────────────────────────

let baseUrl = "http://127.0.0.1:8080";
let browserToken = "";
export const wsService = new AgentTransport();

export function setBaseUrl(url: string): void {
  baseUrl = url;
}

export function authHeaders(): Record<string, string> {
  return browserToken ? { Authorization: "Bearer " + browserToken } : {};
}
export function getBaseUrl(): string {
  return baseUrl;
}

// Extract file paths from tool result text for clickable locations.
// Matches: paths with known extensions, paths after "文件:" prefix,
// directory paths (ending with /), and paths with ≥2 segments but no extension.
function extractLocationsFromText(text: string): ToolLocation[] {
  const locations: ToolLocation[] = [];
  const seen = new Set<string>();

  const knownExt =
    "(?:md|txt|json|js|ts|go|py|yaml|yml|toml|xml|html|css|sh|bash|rs|java|c|cpp|h|rb|php|sql|graphql|proto|vue|svelte|jsx|tsx|mdx|csv|log|cfg|conf|ini|env|lock|sum|mod)";

  // Pattern 1: paths explicitly labeled (文件: /path/to/file)
  // This catches both files and directories after a label prefix.
  const pathPatterns = [
    /(?:文件|File|路径|Path)[:：]\s*(\/[^\s\n]+)/gi,
    // Paths with known extensions (broad match)
    new RegExp(`(\\/[^\\s\\n]+\\.${knownExt})`, "gi"),
    // Directory paths ending with / (e.g. /Users/weijian/agent-lessons/doubao-knowledge/work/)
    /(\/(?:[^\s\n]+\/){2,})/g,
    // Paths with ≥3 segments, no extension in last segment
    // (e.g. /Users/weijian/agent-lessons/doubao-knowledge/other)
    // Requires ≥3 segments to avoid matching short fragments like "/foo/bar"
    /(\/[^\s\n]*\/[^\s\n]*\/[^\s\n/.]+(?:\/[^\s\n/.]+)*)/g,
  ];

  for (const pattern of pathPatterns) {
    let match;
    while ((match = pattern.exec(text)) !== null) {
      let path = match[1].trim();
      // Skip URLs
      if (/^https?:\/\//i.test(path)) continue;
      // Skip very short matches (avoid false positives like "/a")
      if (path.length < 10) continue;
      // Normalize: remove trailing / for dedup, but keep it for display
      const normalized = path.replace(/\/+$/, "");
      if (!seen.has(normalized)) {
        seen.add(normalized);
        locations.push({ path });
      }
    }
  }

  // Pattern 2: paths without extensions that have ≥2 segments
  // (e.g. /Users/weijian/agent-lessons/doubao-knowledge/work/something)
  // Only match if it follows a label prefix to avoid false positives.
  const noExtPattern =
    /(?:文件|File|路径|Path)[:：]\s*(\/(?:[^\s\n]+\/)*[^\s\n/.]+)/gi;
  let match;
  while ((match = noExtPattern.exec(text)) !== null) {
    const path = match[1].trim();
    if (path.length < 10) continue;
    const normalized = path.replace(/\/+$/, "");
    if (!seen.has(normalized)) {
      seen.add(normalized);
      locations.push({ path });
    }
  }

  return locations;
}

export async function apiRequest<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  if (window.piAPI)
    return window.piAPI.request(method, path, body) as Promise<T>;
  const opts: RequestInit = {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(browserToken ? { Authorization: "Bearer " + browserToken } : {}),
    },
    signal: AbortSignal.timeout(path.endsWith("/compact") ? 300000 : 20000),
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(`${baseUrl}${path}`, opts);
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(`HTTP ${res.status}: ${err.error || res.statusText}`);
  }
  return res.json();
}

// ── View models ────────────────────────────────────────────────────────────

export type ViewDensity = "normal" | "verbose" | "summary";

export type PaneKind = "chat" | "diff" | "plan" | "tasks" | "terminal" | "file";

/** Which feature the right workspace sidebar is showing. */
export type RightView =
  | "review"
  | "files"
  | "plan"
  | "tasks"
  | "kb"
  | "profile";

/**
 * Global workspace layout — the right feature sidebar + the bottom terminal.
 * App-level (NOT per-session) so the chosen layout survives session switches.
 * Persisted to localStorage.
 */
export interface WorkspaceUiState {
  /** Whether the left session-list sidebar is expanded (vs. collapsed). */
  sidebarOpen: boolean;
  /** Width (px) of the left session-list sidebar. */
  sidebarWidth: number;
  rightOpen: boolean;
  /** Selected feature, or null when the entire right sidebar is closed. */
  rightView: RightView | null;
  bottomOpen: boolean;
  rightWidth: number;
  bottomHeight: number;
  fileTreeWidth: number;
  /** Absolute paths of files open in the Files panel (VSCode-style tabs). */
  fileTabs: string[];
  activeFileTab?: string;
}

/** Clamp ranges for the draggable regions. */
export const WORKSPACE_SIZE_LIMITS = {
  sidebarWidth: { min: 200, max: 480, default: 224 },
  rightWidth: { min: 340, max: 900, default: 440 },
  bottomHeight: { min: 120, max: 720, default: 280 },
  fileTreeWidth: { min: 160, max: 480, default: 220 },
} as const;

export interface SessionView extends RunProjection {
  accessMode?: AccessMode;
  queue?: MessageQueue;
  meta: SessionMeta;
  transcript: ChatItem[];
  plan: PlanEntry[];
  diffs: GitFileDiff[];
  density: ViewDensity;
  panes: PaneKind[];
  activePane: PaneKind;
  draftAssistantId?: string;
  openFile?: { path: string; content: string };
  /** Client-only slash command feedback, retained across session switches. */
  commandOutput?: string;
}

export type SettingsTab =
  | "general"
  | "appearance"
  | "connections"
  | "models"
  | "mcp"
  | "feishu"
  | "capabilities";

interface StoreState {
  ready: boolean;
  connected: boolean;
  connectionState: string;
  connectionError?: string;
  profiles: ConnectionProfile[];
  selectedProfile: string;
  settingsOpen: boolean;
  settingsTab: SettingsTab;
  loadingSession?: string;
  drafts: Record<string, string>;
  draftInputs: Record<string, DraftInputs>;
  setDraftInputs: (id: string, value: DraftInputs, profileId?: string) => void;
  pending: Record<string, boolean>;
  setDraft: (id: string, text: string) => void;
  setCommandOutput: (id: string, output: string, profileId?: string) => void;
  connectProfile: (id: string, token?: string) => Promise<void>;
  saveProfile: (input: {
    id: string;
    name: string;
    url: string;
    token?: string;
  }) => Promise<void>;
  openSettings: (open?: boolean, tab?: SettingsTab) => void;
  confirm: (
    id: string,
    confirmation: string,
    approved: boolean,
  ) => Promise<void>;
  sessions: Record<string, SessionView>;
  order: string[];
  activeSessionId?: string;
  lang: Lang;
  setLang: (lang: Lang) => void;
  theme: ThemeMode;
  setTheme: (theme: ThemeMode) => void;

  // Models fetched dynamically from backend
  models: ModelInfo[];
  modelSource?: ModelCatalog["source"];
  modelsNotice?: string;
  refreshModels: () => Promise<void>;
  currentModel?: string;
  commands: Command[];
  pickFolder: () => Promise<string | null>;
  pathPicker?: { profileId: string; resolve: (path: string | null) => void };
  resolvePathPicker: (path: string | null) => void;

  update: UpdateState | null;
  checkUpdate: () => Promise<void>;
  downloadUpdate: () => Promise<void>;
  snoozeUpdate: () => void;

  // ── Workspace layout ──
  workspace: WorkspaceUiState;
  toggleSidebar: () => void;
  toggleWorkspaceRight: () => void;
  toggleWorkspaceView: (view: RightView) => void;
  toggleWorkspaceBottom: () => void;
  openWorkspaceView: (view: RightView) => void;
  setWorkspaceSize: (
    key: keyof typeof WORKSPACE_SIZE_LIMITS,
    value: number,
  ) => void;
  openFileTab: (path: string) => void;
  closeFileTab: (path: string) => void;
  setActiveFileTab: (path: string) => void;

  init: () => Promise<void>;
  refreshSessions: () => Promise<void>;
  updateSession: (
    id: string,
    patch: { title?: string; pinned?: boolean; archived?: boolean },
  ) => Promise<void>;
  changeQueue: (
    id: string,
    action: string,
    messageId?: string,
    prompt?: string,
    inputs?: PromptInputs,
  ) => Promise<void>;
  setActive: (id: string) => Promise<void>;
  createSession: (opts?: {
    cwd?: string;
    model?: string;
    application?: string;
  }) => Promise<string>;
  forkSession: (
    sourceId: string,
    opts?: { entryId?: string; beforeMessageIndex?: number },
  ) => Promise<{
    id: string;
    source_id: string;
    kept_messages: number;
    kept_user_messages: number;
    dropped_messages: number;
  }>;
  deleteSession: (id: string) => Promise<void>;
  sendPrompt: (
    id: string,
    text: string,
    inputs?: PromptInputs,
  ) => Promise<void>;
  retryRun: (id: string) => Promise<void>;
  cancel: (id: string) => Promise<void>;
  setModel: (id: string, modelId: string) => Promise<void>;
  setAccessMode: (id: string, mode: AccessMode) => Promise<void>;
  refreshSessionInfo: (id: string) => Promise<void>;
  setDensity: (id: string, density: ViewDensity) => void;
  togglePane: (id: string, pane: PaneKind) => void;
  refreshDiff: (id: string) => Promise<void>;
  openFile: (id: string, path: string) => Promise<void>;
  saveFile: (id: string, path: string, content: string) => Promise<boolean>;

  // ── Global music player ──
  music: MusicState;
  playMusic: (song: MusicTrack) => void;
  setMusicPlaying: (playing: boolean) => void;
  setMusicTime: (time: number) => void;
  setMusicDuration: (duration: number) => void;
  setMusicError: (error: boolean) => void;
  toggleMusic: () => void;
  clearMusic: () => void;
}

export interface MusicTrack {
  songName: string;
  artist: string;
  audioURL: string;
  duration?: number; // seconds
  sessionId?: string;
}

export interface MusicState {
  current: MusicTrack | null;
  playing: boolean;
  currentTime: number;
  duration: number;
  error: boolean;
}

// Cached models from backend (shared across all sessions)
let cachedModels: ModelInfo[] = [];
let cachedCurrentModel: string | undefined;

let initialized = false;
let connectionEpoch = 0;
let storedDrafts: Record<string, Record<string, string>> = {};
let storedInputs: Record<string, Record<string, DraftInputs>> = {};
const pendingRequests = new Map<string, { id: string; text: string }>();

const newId = (() => {
  let n = 0;
  return () =>
    globalThis.crypto?.randomUUID?.() ||
    `r${Date.now().toString(36)}-${(n++).toString(36)}`;
})();

function emptyView(meta: SessionMeta): SessionView {
  return {
    ...emptyProjection(),
    meta,
    plan: [],
    diffs: [],
    density: "normal",
    panes: ["chat"],
    activePane: "chat",
  };
}

function defaultModels(): ModelInfo[] {
  return cachedModels;
}

async function fetchModels(): Promise<ModelCatalog> {
  return apiRequest<ModelCatalog>("GET", "/models");
}

// ── Workspace layout persistence ────────────────────────────────────────────

const WORKSPACE_KEY = "pi-go.workspace";

function clampSize(
  v: unknown,
  key: keyof typeof WORKSPACE_SIZE_LIMITS,
): number {
  const { min, max, default: dflt } = WORKSPACE_SIZE_LIMITS[key];
  if (typeof v !== "number" || !Number.isFinite(v)) return dflt;
  return Math.min(max, Math.max(min, v));
}

function loadWorkspaceUi(): WorkspaceUiState {
  const base: WorkspaceUiState = {
    sidebarOpen: true,
    sidebarWidth: WORKSPACE_SIZE_LIMITS.sidebarWidth.default,
    rightOpen: false,
    rightView: null,
    bottomOpen: false,
    rightWidth: WORKSPACE_SIZE_LIMITS.rightWidth.default,
    bottomHeight: WORKSPACE_SIZE_LIMITS.bottomHeight.default,
    fileTreeWidth: WORKSPACE_SIZE_LIMITS.fileTreeWidth.default,
    fileTabs: [],
  };
  try {
    const raw = localStorage.getItem(WORKSPACE_KEY);
    if (raw) {
      const p = JSON.parse(raw) as Partial<WorkspaceUiState>;
      const rightView =
        p.rightView &&
        ["review", "files", "plan", "tasks", "kb", "profile"].includes(
          p.rightView,
        )
          ? p.rightView
          : null;
      return {
        ...base,
        sidebarOpen: p.sidebarOpen ?? true,
        sidebarWidth: clampSize(p.sidebarWidth, "sidebarWidth"),
        rightOpen: !!p.rightOpen && rightView != null,
        rightView,
        bottomOpen: !!p.bottomOpen,
        rightWidth: clampSize(p.rightWidth, "rightWidth"),
        bottomHeight: clampSize(p.bottomHeight, "bottomHeight"),
        fileTreeWidth: clampSize(p.fileTreeWidth, "fileTreeWidth"),
      };
    }
  } catch {
    /* localStorage unavailable / malformed */
  }
  return base;
}

function persistWorkspaceUi(w: WorkspaceUiState): void {
  try {
    localStorage.setItem(
      WORKSPACE_KEY,
      JSON.stringify({
        sidebarOpen: w.sidebarOpen,
        sidebarWidth: w.sidebarWidth,
        rightOpen: w.rightOpen,
        rightView: w.rightView,
        bottomOpen: w.bottomOpen,
        rightWidth: w.rightWidth,
        bottomHeight: w.bottomHeight,
        fileTreeWidth: w.fileTreeWidth,
      }),
    );
  } catch {
    /* best-effort */
  }
}

export const useStore = create<StoreState>((set, get) => ({
  ready: false,
  connected: false,
  connectionState: "disconnected",
  profiles: [],
  selectedProfile: "",
  settingsOpen: false,
  settingsTab: "general",
  drafts: {},
  draftInputs: {},
  setDraftInputs: (id, value, profileId = get().selectedProfile) => {
    if (profileId === get().selectedProfile)
      set((state) => ({ draftInputs: { ...state.draftInputs, [id]: value } }));
    storedInputs[profileId] = {
      ...(storedInputs[profileId] || {}),
      [id]: value,
    };
    try {
      sessionStorage.setItem(
        "easyagent.desktop.inputs.v1",
        JSON.stringify(storedInputs),
      );
    } catch {}
  },
  pending: {},
  setDraft: (id, text) => {
    set((s) => ({ drafts: { ...s.drafts, [id]: text } }));
    try {
      storedDrafts[get().selectedProfile] = get().drafts;
      sessionStorage.setItem(
        "easyagent.desktop.drafts.v2",
        JSON.stringify(storedDrafts),
      );
    } catch {
      /* Memory retains draft when storage is unavailable. */
    }
  },
  openSettings: (open = true, tab) =>
    set((s) => ({
      settingsOpen: open,
      settingsTab: open ? (tab ?? "general") : s.settingsTab,
    })),
  setCommandOutput: (id, output, profileId = get().selectedProfile) => {
    if (profileId !== get().selectedProfile) return;
    updateView(set, id, (view) => ({ ...view, commandOutput: output }));
  },
  saveProfile: async (input) => {
    if (window.piAPI) {
      const result = await window.piAPI.saveProfile(input);
      set({ profiles: result.profiles });
    } else {
      setStoredServerUrl(input.url);
      browserToken = input.token || browserToken;
      set({
        profiles: [
          {
            id: "browser",
            kind: "remote",
            name: input.name,
            url: input.url,
            hasToken: !!browserToken,
          },
        ],
      });
    }
  },
  connectProfile: async (id, token) => {
    const epoch = ++connectionEpoch;
    get().resolvePathPicker(null);
    const previousState = get();
    saveWorkspaceFiles(
      previousState.selectedProfile,
      previousState.activeSessionId
        ? previousState.sessions[previousState.activeSessionId]?.meta.cwd || ""
        : "",
      previousState.workspace,
    );
    storedDrafts[get().selectedProfile] = get().drafts;
    storedInputs[get().selectedProfile] = get().draftInputs;
    set({
      connected: false,
      connectionState: "connecting",
      connectionError: undefined,
      selectedProfile: id,
    });
    await wsService.disconnect();
    if (epoch !== connectionEpoch) return;
    if (token !== undefined) browserToken = token;
    try {
      if (window.piAPI) {
        const result = await window.piAPI.selectProfile(id);
        set({ profiles: result.profiles });
        const url = await window.piAPI.getServerUrl();
        if (epoch !== connectionEpoch) return;
        setBaseUrl(url || "");
      } else {
        const profile = get().profiles.find((p) => p.id === id);
        if (!profile) throw new Error("请选择服务连接");
        setBaseUrl(profile.url);
      }
      if (epoch !== connectionEpoch) return;
      cachedModels = [];
      cachedCurrentModel = undefined;
      set({
        drafts: storedDrafts[id] || {},
        draftInputs: storedInputs[id] || {},
        pending: {},
        models: [],
        currentModel: undefined,
        modelSource: undefined,
        modelsNotice: undefined,
        selectedProfile: id,
        sessions: {},
        order: [],
        activeSessionId: undefined,
        workspace: {
          ...get().workspace,
          fileTabs: [],
          activeFileTab: undefined,
        },
      });
      await wsService.connect(baseUrl, browserToken);
      await get().refreshModels();
      if (epoch !== connectionEpoch) return;
      try {
        const catalog = await apiRequest<{
          commands: Array<{
            name: string;
            description: string;
            subcommands?: Array<{ name: string; description: string }>;
          }>;
        }>("GET", "/commands");
        if (epoch !== connectionEpoch) return;
        const merged = [...localCommands];
        for (const item of catalog.commands || []) {
          const existing = merged.find((c) => c.name === item.name);
          if (existing) {
            existing.subcommands = item.subcommands;
          } else
            merged.push({
              name: item.name,
              label: item.name,
              description: item.description,
              subcommands: item.subcommands,
            });
        }
        set({ commands: merged });
      } catch {
        /* Older servers retain usable local commands. */
      }
      await get().refreshSessions();
      if (epoch !== connectionEpoch) return;
      const first = get().order[0];
      if (first) await get().setActive(first);
    } catch (error) {
      if (epoch !== connectionEpoch) return;
      set({
        connectionState: "error",
        connectionError: error instanceof Error ? error.message : "连接失败",
      });
    }
  },
  confirm: async (id, confirmation, approved) => {
    const run = get().sessions[id]?.run,
      epoch = connectionEpoch;
    if (!run) throw new Error("任务已结束，请刷新会话");
    try {
      await apiRequest(
        "POST",
        `/sessions/${encodeURIComponent(id)}/run/confirm`,
        { run_id: run.run_id, confirmation_id: confirmation, approved },
      );
    } catch (error) {
      if (epoch === connectionEpoch && /HTTP 409\b/.test(String(error))) {
        const snapshot = await apiRequest<Envelope>("GET", `/sessions/${encodeURIComponent(id)}/run`);
        if (epoch === connectionEpoch)
          updateView(set, id, (v) => ({ ...v, ...reduceEnvelope(v, snapshot) }));
      }
      throw error;
    }
    if (epoch === connectionEpoch)
      updateView(set, id, (v) => ({
        ...v,
        confirmations: v.confirmations.filter(
          (c) => c.confirmation_id !== confirmation,
        ),
        phase: v.confirmations.some((c) => c.confirmation_id !== confirmation) ? "approval" : "thinking",
      }));
  },
  sessions: {},
  order: [],
  models: [],
  currentModel: undefined,
  refreshModels: async () => {
    const epoch = connectionEpoch;
    try {
      const catalog = await fetchModels();
      if (epoch !== connectionEpoch) return;
      cachedModels = (catalog.models || []).map((m) => ({
        modelId: m.id,
        name: m.name || m.id,
      }));
      cachedCurrentModel = catalog.current?.id;
      set({
        models: cachedModels,
        currentModel: cachedCurrentModel,
        modelSource: catalog.source,
        modelsNotice: catalog.discovery_error,
      });
    } catch {
      if (epoch !== connectionEpoch) return;
      set({ modelsNotice: "模型列表暂不可用，可在设置中检查模型连接。" });
    }
  },
  commands: localCommands,
  workspace: loadWorkspaceUi(),
  pickFolder: async () => {
    if (
      get().profiles.find((p) => p.id === get().selectedProfile)?.kind !==
      "local"
    ) {
      get().resolvePathPicker(null);
      return new Promise((resolve) => {
        set({ pathPicker: { profileId: get().selectedProfile, resolve } });
      });
    }
    return (await window.piAPI?.pickFolder()) ?? null;
  },
  resolvePathPicker: (path) => {
    const picker = get().pathPicker;
    set({ pathPicker: undefined });
    picker?.resolve(picker.profileId === get().selectedProfile ? path : null);
  },
  lang: loadStoredLang(),
  setLang: (lang) => {
    persistLang(lang);
    set({ lang });
  },
  theme: loadStoredTheme(),
  setTheme: (theme) => {
    persistTheme(theme);
    set({ theme });
  },

  // ── Global music player ──
  music: {
    current: null,
    playing: false,
    currentTime: 0,
    duration: 0,
    error: false,
  },
  playMusic: (song) => {
    set((s) => ({
      music: {
        ...s.music,
        current: song,
        playing: true,
        currentTime: 0,
        duration: song.duration || 0,
        error: false,
      },
    }));
  },
  setMusicPlaying: (playing) =>
    set((s) => ({ music: { ...s.music, playing } })),
  setMusicTime: (time) =>
    set((s) => ({ music: { ...s.music, currentTime: time } })),
  setMusicDuration: (duration) =>
    set((s) => ({ music: { ...s.music, duration } })),
  setMusicError: (error) => set((s) => ({ music: { ...s.music, error } })),
  toggleMusic: () =>
    set((s) => ({ music: { ...s.music, playing: !s.music.playing } })),
  clearMusic: () =>
    set((s) => ({
      music: {
        current: null,
        playing: false,
        currentTime: 0,
        duration: 0,
        error: false,
      },
    })),

  update: null,
  checkUpdate: async () => {
    if (get().update?.phase === "checking") return;
    set({ update: { supported: true, phase: "checking", currentVersion: __APP_VERSION__ } });
    try {
      const info = await window.piAPI?.checkForUpdate();
      if (info) {
        set({
          update: {
            supported: true,
            phase: "available",
            info,
            currentVersion: __APP_VERSION__,
          },
        });
      } else {
        set({
          update: {
            supported: true,
            phase: "idle",
            info: null,
            currentVersion: __APP_VERSION__,
          },
        });
      }
    } catch (error) {
      set({
        update: {
          supported: true,
          phase: "error",
          error: error instanceof Error
            ? error.message.replace(/^Error invoking remote method '[^']+':\s*(?:Error:\s*)?/, "")
            : "检查桌面更新失败",
          currentVersion: __APP_VERSION__,
        },
      });
    }
  },
  downloadUpdate: async () => {
    const u = get().update;
    if (u?.phase === "error") {
      await get().checkUpdate();
      return;
    }
    if (u?.info?.downloadUrl) {
      await window.piAPI?.openDownloadPage(u.info.downloadUrl);
    }
    set((s) => ({ update: s.update ? { ...s.update, phase: "idle" } : null }));
  },
  snoozeUpdate: () =>
    set((s) => (s.update ? { update: { ...s.update, snoozed: true } } : {})),

  // ── Workspace layout actions ──
  toggleSidebar: () => {
    set((s) => {
      const workspace = {
        ...s.workspace,
        sidebarOpen: !s.workspace.sidebarOpen,
      };
      persistWorkspaceUi(workspace);
      return { workspace };
    });
  },
  toggleWorkspaceRight: () => {
    set((s) => {
      // When opening, default to 'files' if no view is selected yet.
      const rightOpen = !s.workspace.rightOpen;
      const rightView = rightOpen
        ? (s.workspace.rightView ?? "files")
        : s.workspace.rightView;
      const workspace: WorkspaceUiState = {
        ...s.workspace,
        rightOpen,
        rightView,
      };
      persistWorkspaceUi(workspace);
      return { workspace };
    });
  },
  toggleWorkspaceView: (view) => {
    set((s) => {
      // Clicking the visible selected feature closes the entire sidebar.
      if (s.workspace.rightOpen && s.workspace.rightView === view) {
        const workspace = { ...s.workspace, rightOpen: false, rightView: null };
        persistWorkspaceUi(workspace);
        return { workspace };
      }
      const workspace = { ...s.workspace, rightOpen: true, rightView: view };
      persistWorkspaceUi(workspace);
      return { workspace };
    });
  },
  toggleWorkspaceBottom: () => {
    set((s) => {
      const workspace = { ...s.workspace, bottomOpen: !s.workspace.bottomOpen };
      persistWorkspaceUi(workspace);
      return { workspace };
    });
  },
  openWorkspaceView: (view) => {
    set((s) => {
      const workspace = { ...s.workspace, rightOpen: true, rightView: view };
      persistWorkspaceUi(workspace);
      return { workspace };
    });
  },
  setWorkspaceSize: (key, value) => {
    set((s) => {
      const clamped = clampSize(value, key);
      const workspace = { ...s.workspace, [key]: clamped };
      persistWorkspaceUi(workspace);
      return { workspace };
    });
  },
  openFileTab: (path) => {
    set((s) => {
      const tabs = s.workspace.fileTabs.includes(path)
        ? s.workspace.fileTabs
        : [...s.workspace.fileTabs, path];
      const workspace: WorkspaceUiState = {
        ...s.workspace,
        fileTabs: tabs,
        activeFileTab: path,
        rightOpen: true,
        rightView: "files" as RightView,
      };
      persistWorkspaceUi(workspace);
      saveWorkspaceFiles(
        s.selectedProfile,
        s.activeSessionId ? s.sessions[s.activeSessionId]?.meta.cwd || "" : "",
        workspace,
      );
      return { workspace };
    });
  },
  closeFileTab: (path) => {
    set((s) => {
      const tabs = s.workspace.fileTabs.filter((t) => t !== path);
      const activeFileTab =
        s.workspace.activeFileTab === path
          ? (tabs[tabs.length - 1] ?? undefined)
          : s.workspace.activeFileTab;
      const workspace: WorkspaceUiState = {
        ...s.workspace,
        fileTabs: tabs,
        activeFileTab,
      };
      persistWorkspaceUi(workspace);
      saveWorkspaceFiles(
        s.selectedProfile,
        s.activeSessionId ? s.sessions[s.activeSessionId]?.meta.cwd || "" : "",
        workspace,
      );
      return { workspace };
    });
  },
  setActiveFileTab: (path) => {
    set((s) => {
      const workspace = { ...s.workspace, activeFileTab: path };
      persistWorkspaceUi(workspace);
      saveWorkspaceFiles(
        s.selectedProfile,
        s.activeSessionId ? s.sessions[s.activeSessionId]?.meta.cwd || "" : "",
        workspace,
      );
      return { workspace };
    });
  },

  init: async () => {
    if (initialized) return;
    initialized = true;
    let queue: Envelope[] = [],
      scheduled = 0;
    const flush = () => {
      if (scheduled) cancelAnimationFrame(scheduled);
      scheduled = 0;
      const batch = queue;
      queue = [];
      set((s) => {
        const sessions = { ...s.sessions };
        for (const message of batch) {
          const id = message.session_id;
          if (!id || !sessions[id]) continue;
          const v = sessions[id];
          const projection = reduceEnvelope(v, message);
          const status: SessionRunStatus = isActiveRun(projection.run)
            ? "thinking"
            : projection.phase === "error"
              ? "error"
              : "idle";
          const nextQueue =
            message.queue && message.queue.revision >= (v.queue?.revision || 0)
              ? message.queue
              : v.queue;
          sessions[id] = {
            ...v,
            ...projection,
            queue: nextQueue,
            meta: { ...v.meta, status },
          };
        }
        return { sessions };
      });
    };
    wsService.on("status", (value) =>
      set({
        connected: value.state === "connected",
        connectionState: value.state,
        connectionError: value.message,
      }),
    );
    wsService.on("backend", (value) => {
      if (value.state === "error") set({ connectionError: value.message });
    });
    wsService.on("open", () => {
      const s = get();
      for (const [id, view] of Object.entries(s.sessions))
        if (id === s.activeSessionId || isActiveRun(view.run))
          void wsService.send({
            type: "subscribe",
            session_id: id,
            run_id: view.run?.run_id,
            after_seq: view.seq || undefined,
          });
    });
    wsService.on("message", (message: Envelope) => {
      if (message.type === "error") {
        flush();
        set({
          connectionError: message.message || message.error || "请求未完成",
        });
        return;
      }
      queue.push(message);
      // The first visible chunk and authoritative transitions render immediately.
      const view = message.session_id
        ? get().sessions[message.session_id]
        : undefined;
      if (
        message.type !== "event" ||
        message.event?.type !== "text_delta" ||
        !view?.transcript.some(
          (item) =>
            item.kind === "assistant" &&
            item.id.startsWith(view.run?.run_id || "\0"),
        )
      )
        flush();
      else if (!scheduled) scheduled = requestAnimationFrame(flush);
    });
    let id: string;
    if (window.piAPI) {
      const saved = await window.piAPI.profiles();
      set({ profiles: saved.profiles });
      id = saved.selected;
    } else {
      const url = getStoredServerUrl() || "http://127.0.0.1:8080";
      set({
        profiles: [
          {
            id: "browser",
            name: "服务连接",
            kind: "remote",
            url,
            hasToken: false,
          },
        ],
      });
      id = "browser";
    }
    try {
      const drafts = JSON.parse(
        sessionStorage.getItem("easyagent.desktop.drafts.v2") || "{}",
      );
      if (drafts && typeof drafts === "object" && !Array.isArray(drafts)) {
        for (const [profile, values] of Object.entries(drafts)) {
          if (values && typeof values === "object" && !Array.isArray(values))
            storedDrafts[profile] = Object.fromEntries(
              Object.entries(values).filter(
                ([, text]) => typeof text === "string",
              ),
            ) as Record<string, string>;
        }
      }
    } catch {}
    try {
      const saved = JSON.parse(
        sessionStorage.getItem("easyagent.desktop.inputs.v1") || "{}",
      );
      storedInputs = sanitizeInputDrafts(saved);
    } catch {}
    set({ ready: true });
    await get().connectProfile(id);
  },

  refreshSessions: async () => {
    const epoch = connectionEpoch;
    try {
      const raw = await apiRequest<any[]>("GET", "/sessions");
      if (epoch !== connectionEpoch) return;
      const sessions: any[] = Array.isArray(raw) ? raw : [];
      set((s) => {
        const newSessions: Record<string, SessionView> = {};
        for (const sess of sessions) {
          // Preserve existing cwd if the backend doesn't provide one
          const existingCwd = s.sessions[sess.id]?.meta.cwd;
          const cwd = sess.workspace || existingCwd || "";
          // Derive title: prefer backend title, then existing local title, then fallback
          const backendTitle = sess.title
            ? deriveTitleFromMessage(sess.title)
            : undefined;
          const existingTitle = s.sessions[sess.id]?.meta.title;
          // If existing title is the auto-generated fallback, prefer backend title
          const isFallback =
            !existingTitle || existingTitle.startsWith("Session ");
          const title =
            backendTitle ||
            (isFallback ? `Session ${sess.id.slice(-6)}` : existingTitle!);
          // Read application from backend, fallback to existing
          const application =
            sess.application || s.sessions[sess.id]?.meta.application;
          const meta: SessionMeta = {
            id: sess.id,
            title,
            cwd,
            status: "idle" as SessionRunStatus,
            model: s.sessions[sess.id]?.meta.model,
            application,
            pinned: !!sess.pinned,
            archived: !!sess.archived,
            forked_from: sess.forked_from,
            availableModels: defaultModels(),
            createdAt: timestampMillis(sess.created_at),
            updatedAt: timestampMillis(sess.last_active),
          };
          newSessions[sess.id] = s.sessions[sess.id] ?? emptyView(meta);
          // Update meta for existing sessions (in case cwd/title/application was loaded from backend)
          if (s.sessions[sess.id]) {
            newSessions[sess.id] = {
              ...s.sessions[sess.id],
              meta: {
                ...s.sessions[sess.id].meta,
                cwd,
                title,
                application,
                pinned: !!sess.pinned,
                archived: !!sess.archived,
                forked_from: sess.forked_from,
                updatedAt: timestampMillis(sess.last_active),
              },
            };
          }
        }
        return { sessions: newSessions, order: sessions.map((s) => s.id) };
      });
    } catch (err) {
      if (epoch !== connectionEpoch) return;
      set({
        connectionError:
          err instanceof Error ? err.message : "无法读取会话列表",
      });
    }
  },

  updateSession: async (id, patch) => {
    const epoch = connectionEpoch;
    await apiRequest("PATCH", `/sessions/${encodeURIComponent(id)}`, patch);
    if (epoch !== connectionEpoch) return;
    updateView(set, id, (v) => ({ ...v, meta: { ...v.meta, ...patch } }));
  },

  setActive: async (id) => {
    const epoch = connectionEpoch;
    set((s) => ({
      activeSessionId: id,
      settingsOpen: false,
      loadingSession: id,
      workspace: workspaceForSession(s, s.sessions[id]?.meta.cwd),
    }));
    try {
      const [snapshot, info] = await Promise.all([
        apiRequest<Envelope>("GET", `/sessions/${encodeURIComponent(id)}/run`),
        apiRequest<any>("GET", `/sessions/${encodeURIComponent(id)}/info`),
      ]);
      if (epoch !== connectionEpoch) return;
      updateView(set, id, (v) => {
        const projection = reduceEnvelope(v, snapshot);
        return {
          ...projection,
          accessMode: info.access_mode,
          contextUsage: isActiveRun(projection.run) ? projection.contextUsage || info.context_usage : info.context_usage,
          queue: snapshot.queue || v.queue,
          meta: {
            ...v.meta,
            cwd: info.workspace || v.meta.cwd,
            model: info.model || v.meta.model,
            status: isActiveRun(projection.run)
              ? "thinking"
              : projection.phase === "error"
                ? "error"
                : "idle",
          },
        };
      });
      const view = get().sessions[id];
      if (
        !(await wsService.send({
          type: "subscribe",
          session_id: id,
          run_id: view?.run?.run_id,
          after_seq: view?.seq || undefined,
        }))
      )
        set({ connectionError: "连接恢复后会同步任务状态" });
    } catch (error) {
      if (epoch === connectionEpoch)
        set({
          connectionError:
            error instanceof Error ? error.message : "读取会话失败",
        });
    } finally {
      if (epoch === connectionEpoch && get().loadingSession === id)
        set({ loadingSession: undefined });
    }
  },

  createSession: async (opts) => {
    const epoch = connectionEpoch;
    const body: Record<string, string> = {};
    if (opts?.cwd) body.cwd = opts.cwd;
    if (opts?.model) body.model = opts.model;
    if (opts?.application) body.application = opts.application;
    const result = await apiRequest<{ id: string; created_at: number }>(
      "POST",
      "/sessions",
      body,
    );
    if (epoch !== connectionEpoch)
      throw new Error("运行主机已切换，原服务的会话创建结果已忽略");
    const lang = get().lang;
    const meta: SessionMeta = {
      id: result.id,
      title:
        opts?.application === "music"
          ? translate(lang, "session.musicTitle")
          : opts?.cwd
            ? projectName(opts.cwd)
            : translate(lang, "session.defaultTitle"),
      cwd: opts?.cwd || "",
      status: "idle" as SessionRunStatus,
      model: opts?.model || cachedCurrentModel,
      application: opts?.application,
      availableModels: defaultModels(),
      createdAt: timestampMillis(result.created_at, Date.now()),
      updatedAt: Date.now(),
    };
    set((s) => ({
      sessions: { ...s.sessions, [result.id]: emptyView(meta) },
      order: [result.id, ...s.order],
      activeSessionId: result.id,
      workspace: workspaceForSession(s, meta.cwd),
    }));
    await get().refreshSessionInfo(result.id).catch(error => {
      if (epoch === connectionEpoch) set({ connectionError: error.message });
    });
    return result.id;
  },

  forkSession: async (sourceId, opts) => {
    const epoch = connectionEpoch;
    const body: Record<string, any> = {};
    if (opts?.entryId) body.entry_id = opts.entryId;
    if (typeof opts?.beforeMessageIndex === "number") {
      body.before_message_index = opts.beforeMessageIndex;
    }
    const result = await apiRequest<{
      id: string;
      source_id: string;
      kept_messages: number;
      kept_user_messages: number;
      dropped_messages: number;
    }>("POST", `/sessions/${encodeURIComponent(sourceId)}/fork`, body);
    if (epoch !== connectionEpoch) {
      throw new Error("运行主机已切换，原服务的分叉结果已忽略");
    }
    await get().refreshSessions();
    if (epoch !== connectionEpoch) throw new Error("运行主机已切换，原服务的分叉结果已忽略");
    await get().setActive(result.id);
    return result;
  },

  deleteSession: async (id) => {
    await apiRequest("DELETE", `/sessions/${id}`);
    set((s) => {
      const sessions = { ...s.sessions };
      delete sessions[id];
      const order = s.order.filter((x) => x !== id);
      const activeSessionId =
        s.activeSessionId === id ? (order[0] ?? undefined) : s.activeSessionId;
      return { sessions, order, activeSessionId };
    });
  },

  changeQueue: async (id, action, messageId, prompt, inputs) => {
    const epoch = connectionEpoch;
    const queue = await apiRequest<MessageQueue>(
      "POST",
      `/sessions/${encodeURIComponent(id)}/queue`,
      { action, id: messageId, prompt, inputs },
    );
    if (epoch !== connectionEpoch) return;
    updateView(set, id, (v) => ({
      ...v,
      queue: queue.revision >= (v.queue?.revision || 0) ? queue : v.queue,
    }));
  },

  sendPrompt: async (id, text, inputs) => {
    if (get().pending[id]) throw new Error("正在等待服务接收消息");
    if (!get().connected) throw new Error("尚未连接到服务，草稿已保留");
    const key = get().selectedProfile + ":" + id,
      epoch = connectionEpoch;
    const identity = JSON.stringify({ text, inputs: inputs || {} });
    const requestId =
      pendingRequests.get(key)?.text === identity
        ? pendingRequests.get(key)!.id
        : newId();
    pendingRequests.set(key, { id: requestId, text: identity });
    set((s) => ({
      pending: { ...s.pending, [id]: true },
      connectionError: undefined,
    }));
    try {
      if (isActiveRun(get().sessions[id]?.run)) {
        await get().changeQueue(id, "add", requestId, text, inputs);
        pendingRequests.delete(key);
        return;
      }
      const accepted = await wsService.prompt(id, text, requestId, inputs);
      if (accepted.state === "interrupted")
        throw new Error(
          "此任务因服务重启已中断；请检查已执行结果，再点击重新执行。",
        );
      pendingRequests.delete(key);
      if (epoch !== connectionEpoch) return;
      updateView(set, id, (v) => ({
        ...v,
        meta: {
          ...v.meta,
          title:
            v.transcript.filter((i) => i.kind === "user").length <= 1
              ? deriveTitleFromMessage(text)
              : v.meta.title,
        },
      }));
    } finally {
      if (epoch === connectionEpoch)
        set((s) => ({ pending: { ...s.pending, [id]: false } }));
    }
  },
  retryRun: async (id) => {
    const view = get().sessions[id];
    if (view?.run?.state !== "interrupted")
      throw new Error("此任务不是中断状态");
    const latestUser = view.transcript
      .slice()
      .reverse()
      .find((item) => item.kind === "user");
    const prompt =
      view.run.prompt || (latestUser?.kind === "user" ? latestUser.text : "");
    if (!prompt) throw new Error("无法恢复原始消息，请手动输入新任务");
    const key = get().selectedProfile + ":" + id,
      previous = pendingRequests.get(key);
    if (previous?.id === view.run.request_id) pendingRequests.delete(key);
    get().setDraft(id, prompt);
    await get().sendPrompt(id, prompt, view.run.inputs);
    if (get().drafts[id] === prompt) get().setDraft(id, "");
  },
  cancel: async (id) => {
    const run = get().sessions[id]?.run;
    if (!run) throw new Error("当前没有正在运行的任务");
    await apiRequest("POST", `/sessions/${encodeURIComponent(id)}/run/cancel`, {
      run_id: run.run_id,
    });
  },

  refreshSessionInfo: async (id) => {
    const epoch = connectionEpoch;
    const info = await apiRequest<SessionInfo>("GET", `/sessions/${encodeURIComponent(id)}/info`);
    if (epoch !== connectionEpoch) return;
    updateView(set, id, () => ({ accessMode: info.access_mode, contextUsage: info.context_usage }));
  },
  setAccessMode: async (id, mode) => {
    const epoch = connectionEpoch;
    const result = await apiRequest<{ access_mode: AccessMode }>("POST", `/sessions/${encodeURIComponent(id)}/permissions`, { mode });
    if (epoch !== connectionEpoch) return;
    updateView(set, id, () => ({ accessMode: result.access_mode }));
  },
  setModel: async (id, modelId) => {
    try {
      await apiRequest("POST", `/sessions/${id}/model`, { model: modelId });
      updateView(set, id, (v) => ({
        ...v,
        meta: { ...v.meta, model: modelId },
      }));
      await get().refreshSessionInfo(id);
    } catch (err) {
      throw err;
    }
  },

  setDensity: (id, density) => updateView(set, id, (v) => ({ ...v, density })),

  togglePane: (id, pane) =>
    updateView(set, id, (v) => {
      const has = v.panes.includes(pane);
      const panes = has
        ? v.panes.filter((p) => p !== pane)
        : [...v.panes, pane];
      return {
        ...v,
        panes: panes.length ? panes : ["chat"],
        activePane: has ? v.activePane : pane,
      };
    }),

  refreshDiff: async (id) => {
    const view = get().sessions[id];
    if (!view || !view.meta.cwd) return;
    try {
      const resp = await apiRequest<{ files: GitFileDiff[] }>(
        "GET",
        `/sessions/${id}/diff`,
      );
      updateView(set, id, (v) => ({ ...v, diffs: resp.files || [] }));
    } catch (err) {
      console.error("Failed to fetch diff", err);
    }
  },

  openFile: async (id, path) => {
    try {
      const resp = await apiRequest<{ content: string }>(
        "GET",
        `/sessions/${id}/file?path=${encodeURIComponent(path)}`,
      );
      updateView(set, id, (v) => ({
        ...v,
        openFile: { path, content: resp.content },
        activePane: "file",
      }));
    } catch (err) {
      console.error("Failed to read file", err);
    }
  },

  saveFile: async (id, path, content) => {
    try {
      console.log("[saveFile] Saving file:", {
        id,
        path,
        contentLength: content.length,
      });
      await apiRequest<{ status: string }>(
        "PUT",
        `/sessions/${id}/file?path=${encodeURIComponent(path)}`,
        { content },
      );
      console.log("[saveFile] File saved successfully");
      // Update the view with new content
      updateView(set, id, (v) => ({
        ...v,
        openFile: v.openFile?.path === path ? { path, content } : v.openFile,
      }));
      return true;
    } catch (err) {
      console.error("Failed to save file", err);
      return false;
    }
  },
}));

// ── Helpers ───────────────────────────────────────────────────────────────

type SetFn = (
  partial: Partial<StoreState> | ((s: StoreState) => Partial<StoreState>),
) => void;

function workspaceForSession(s: StoreState, cwd?: string): WorkspaceUiState {
  const previousCwd = s.activeSessionId
    ? s.sessions[s.activeSessionId]?.meta.cwd
    : undefined;
  if (previousCwd === cwd) return s.workspace;
  if (previousCwd)
    saveWorkspaceFiles(s.selectedProfile, previousCwd, s.workspace);
  const saved = readWorkspaceFiles(s.selectedProfile, cwd || "");
  return {
    ...s.workspace,
    fileTabs: saved.fileTabs,
    activeFileTab: saved.activeFileTab,
  };
}

function updateView(
  setFn: SetFn,
  id: string,
  fn: (v: SessionView) => Partial<SessionView>,
): void {
  setFn((s) => {
    const v = s.sessions[id];
    if (!v) return {};
    return { sessions: { ...s.sessions, [id]: { ...v, ...fn(v) } } };
  });
}

function inferToolKind(name: string): AcpToolKind {
  const lower = name.toLowerCase();
  // KB agent tools — must check before generic patterns
  if (lower === "kb_search" || lower === "kb_list" || lower === "kb_maintain")
    return "search";
  if (lower === "kb_read") return "read";
  if (lower === "kb_save") return "edit";
  if (lower.includes("read") || lower.includes("cat") || lower.includes("view"))
    return "read";
  if (
    lower.includes("edit") ||
    lower.includes("write") ||
    lower.includes("replace")
  )
    return "edit";
  if (
    lower.includes("delete") ||
    lower.includes("remove") ||
    lower.includes("rm")
  )
    return "delete";
  if (lower.includes("move") || lower.includes("rename")) return "move";
  if (
    lower.includes("search") ||
    lower.includes("grep") ||
    lower.includes("glob") ||
    lower.includes("find")
  )
    return "search";
  if (
    lower.includes("bash") ||
    lower.includes("exec") ||
    lower.includes("shell") ||
    lower.includes("run")
  )
    return "execute";
  if (lower.includes("think") || lower.includes("reason")) return "think";
  if (
    lower.includes("fetch") ||
    lower.includes("http") ||
    lower.includes("web")
  )
    return "fetch";
  return "other";
}

function projectName(cwd: string): string {
  const parts = cwd.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || cwd;
}
