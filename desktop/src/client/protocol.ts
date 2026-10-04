import type {
  AcpToolKind,
  ToolCallStatus,
  ToolCallContent,
  ToolLocation,
} from "../types";
export interface ConnectionProfile {
  id: string;
  name: string;
  kind: "local" | "remote";
  url: string;
  hasToken: boolean;
}
export interface RunInfo {
  run_id: string;
  request_id?: string;
  state:
    | "running"
    | "waiting_confirmation"
    | "completed"
    | "failed"
    | "cancelled"
    | "interrupted";
  error?: string;
  prompt?: string;
  display_prompt?: string;
  inputs?: PromptInputs;
}
export function isActiveRun(run: RunInfo | undefined): boolean {
  return run?.state === "running" || run?.state === "waiting_confirmation";
}
export interface Confirmation {
  confirmation_id: string;
  tool_call_id: string;
  tool_name: string;
  description: string;
  args?: unknown;
}
export type ChatItem =
  | {
      kind: "user" | "assistant" | "thought" | "system" | "error";
      id: string;
      text: string;
    }
  | {
      kind: "tool";
      startedAt?: number;
      durationMs?: number;
      id: string;
      toolCallId: string;
      title: string;
      toolKind: AcpToolKind;
      status: ToolCallStatus;
      locations?: ToolLocation[];
      content: ToolCallContent[];
      terminalOutput?: string;
      rawInput?: Record<string, unknown>;
      details?: Record<string, unknown>;
    };
export interface AgentEvent {
  timestamp?: number;
  duration_ms?: number;
  type: string;
  text_delta?: string;
  tool_call_id?: string;
  tool_name?: string;
  tool_args?: Record<string, unknown>;
  tool_result?: unknown;
  partial_result?: unknown;
  is_error?: boolean;
  error?: string;
  final_message?: { content?: unknown; text?: string };
  confirmation_id?: string;
  description?: string;
  [key: string]: unknown;
}
export interface StoredMessage {
  duration_ms?: number;
  role: string;
  content?: unknown;
  thinking?: string;
  tool_calls?: Array<{ id: string; name: string; args?: unknown }>;
  tool_call_id?: string;
  is_error?: boolean;
  tool_details?: Record<string, unknown>;
}
export interface Envelope {
  inputs?: PromptInputs;
  queue?: MessageQueue;
  type: string;
  session_id?: string;
  request_id?: string;
  run_id?: string;
  seq?: number;
  reset?: boolean;
  run?: RunInfo;
  state?: RunInfo["state"];
  streaming?: boolean;
  event?: AgentEvent;
  events?: Array<AgentEvent | Envelope>;
  messages?: StoredMessage[];
  pending_confirmations?: Confirmation[];
  confirmation?: Confirmation;
  confirmation_id?: string;
  message?: string;
  error?: string;
  code?: string;
  prompt?: string;
  duplicate?: boolean;
  [key: string]: unknown;
}

export interface MessageQueue {
  items: {
    id: string;
    prompt: string;
    created_at: string;
    inputs?: PromptInputs;
  }[];
  paused: boolean;
  revision: number;
}

export interface PromptInputs {
  attachments?: string[];
  files?: { path: string; workspace: string }[];
}
export interface InputAttachment {
  id: string;
  name: string;
  path: string;
  workspace: string;
  mime_type: string;
  size: number;
}
export interface DraftInputs {
  attachments: InputAttachment[];
  files: { path: string; workspace: string }[];
}
export const EMPTY_INPUTS: DraftInputs = { attachments: [], files: [] };
export function promptInputs(draft: DraftInputs): PromptInputs {
  return {
    attachments: draft.attachments.map((item) => item.id),
    files: draft.files,
  };
}
export interface RunProjection {
  transcript: ChatItem[];
  seq: number;
  run?: RunInfo;
  confirmations: Confirmation[];
  phase:
    | "idle"
    | "thinking"
    | "responding"
    | "tool"
    | "approval"
    | "cancelled"
    | "interrupted"
    | "error";
}
