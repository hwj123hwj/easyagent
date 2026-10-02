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
  role: string;
  content?: unknown;
  thinking?: string;
  tool_calls?: Array<{ id: string; name: string; args?: unknown }>;
  tool_call_id?: string;
  is_error?: boolean;
  tool_details?: Record<string, unknown>;
}
export interface Envelope {
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
