/**
 * types.ts — Inline type definitions for the easyagent desktop renderer.
 * Replaces the @shared/ipc types from DeepVcodeClient, keeping only what
 * easyagent's REST + WebSocket backend needs.
 */

// ── Sessions ──────────────────────────────────────────────────────────────

export type SessionRunStatus =
  | 'idle'
  | 'starting'
  | 'thinking'
  | 'error'
  | 'exited';

export interface ModelInfo {
  modelId: string;
  name: string;
}

export interface ModelCatalog {
  models: Array<{ id: string; name: string; provider: string }>;
  current?: { id: string };
  source?: "gateway" | "configured" | "unconfigured";
  discovery_error?: string;
}

export interface ProviderConfig {
  mode: "inherit" | "override";
  provider?: "openai" | "anthropic";
  baseUrl?: string;
  model?: string;
  hasKey: boolean;
}

export interface ProviderConfigInput {
  mode: ProviderConfig["mode"];
  provider?: ProviderConfig["provider"];
  baseUrl?: string;
  model?: string;
  apiKey?: string;
}

export interface ProviderCheckResult {
  ok: boolean;
  models: Array<{ id: string; name: string }>;
  message: string;
}

export interface SessionMeta {
  id: string;
  title: string;
  cwd: string;
  status: SessionRunStatus;
  model?: string;
  application?: string; // e.g. "coding", "music"
  availableModels: ModelInfo[];
  createdAt: number;
  updatedAt: number;
}

// ── Session events (from WebSocket) ────────────────────────────────────────

export type AcpToolKind =
  | 'read'
  | 'edit'
  | 'delete'
  | 'move'
  | 'search'
  | 'execute'
  | 'think'
  | 'fetch'
  | 'switch_mode'
  | 'other';

export type ToolCallStatus =
  | 'pending'
  | 'in_progress'
  | 'completed'
  | 'failed';

export interface ToolLocation {
  path: string;
  line?: number;
}

export interface ToolDiff {
  path: string;
  oldText?: string | null;
  newText: string;
}

export interface ToolCallContent {
  text?: string;
  diff?: ToolDiff;
}

export type DesktopSessionEvent =
  | { kind: 'turn_start' }
  | { kind: 'turn_end'; stopReason?: string }
  | { kind: 'message_chunk'; text: string }
  | { kind: 'thought_chunk'; text: string }
  | { kind: 'tool_call'; toolCallId: string; title: string; toolKind: AcpToolKind; status: ToolCallStatus; locations?: ToolLocation[]; content?: ToolCallContent[]; rawInput?: Record<string, unknown> }
  | { kind: 'tool_update'; toolCallId: string; status?: ToolCallStatus; title?: string; content?: ToolCallContent[]; terminalOutput?: string }
  | { kind: 'error'; message: string };

// ── Plan / Diff / File (side panes) ───────────────────────────────────────

export interface PlanEntry {
  content: string;
  status: 'pending' | 'in_progress' | 'completed';
}

export interface GitFileDiff {
  path: string;
  added: number;
  removed: number;
  patch: string;
}

// ── Version updates ────────────────────────────────────────────────────────

export interface UpdateInfo {
  version: string;
  downloadUrl: string;
  releaseNotes: string;
}

export type UpdatePhase =
  | 'idle'
  | 'checking'
  | 'available'
  | 'error';

export interface UpdateState {
  supported: boolean;
  phase: UpdatePhase;
  info?: UpdateInfo | null;
  error?: string;
  currentVersion?: string;
  skipped?: boolean;
  snoozed?: boolean;
}

// ── Mobile Update ──────────────────────────────────────────────────────────

export interface MobileUpdateInfo {
  version: string;
  downloadUrl: string;
  releaseNotes: string;
  apkSize: number;
}
