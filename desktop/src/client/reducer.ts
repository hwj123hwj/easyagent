import type {
  AgentEvent,
  ChatItem,
  Envelope,
  RunProjection,
  StoredMessage,
} from "./protocol";
import type { AcpToolKind } from "../types";
import { isActiveRun } from "./protocol";
export function emptyProjection(): RunProjection {
  return { transcript: [], seq: 0, confirmations: [], phase: "idle" };
}
export function displayText(value: unknown): string {
  if (typeof value === "string") return value;
  if (value == null) return "";
  if (Array.isArray(value))
    return value
      .map((v) =>
        typeof v === "object" && v?.type === "image"
          ? "[图片]"
          : displayText(v?.text ?? v),
      )
      .join("");
  if (typeof value === "object") {
    const v = value as Record<string, unknown>;
    return String(
      v.UserFacing ||
        v.user_facing ||
        v.Content ||
        v.content ||
        JSON.stringify(value),
    );
  }
  return String(value);
}
export function toolKind(name: string): AcpToolKind {
  if (/bash|exec|shell|run/i.test(name)) return "execute";
  if (/edit|write|save|replace/i.test(name)) return "edit";
  if (/read|cat|view/i.test(name)) return "read";
  if (/search|grep|glob|find|list/i.test(name)) return "search";
  if (/fetch|http|web/i.test(name)) return "fetch";
  return "other";
}
function args(value: unknown): Record<string, unknown> | undefined {
  try {
    const v = typeof value === "string" ? JSON.parse(value) : value;
    return v && typeof v === "object"
      ? (v as Record<string, unknown>)
      : undefined;
  } catch {
    return undefined;
  }
}
export function historyProjection(messages: StoredMessage[]): RunProjection {
  const result = emptyProjection();
  const outcomes = new Map<string, StoredMessage>();
  const pending = new Map<string, number>();
  messages.forEach((message, index) => {
    if (message.role === "assistant") {
      for (const call of message.tool_calls || []) pending.set(call.id, index);
    } else if (message.role === "tool" && message.tool_call_id) {
      const owner = pending.get(message.tool_call_id);
      if (owner !== undefined) {
        outcomes.set(`${owner}:${message.tool_call_id}`, message);
        pending.delete(message.tool_call_id);
      }
    }
  });
  messages.forEach((message, index) => {
    const prefix = `history-${index}`;
    if (message.role === "user" && message.content)
      result.transcript.push({
        kind: "user",
        id: prefix,
        text: displayText(message.content),
      });
    if (message.role !== "assistant") return;
    if (message.thinking)
      result.transcript.push({
        kind: "thought",
        id: prefix + "-thought",
        text: message.thinking,
      });
    if (message.content)
      result.transcript.push({
        kind: "assistant",
        id: prefix + "-text",
        text: displayText(message.content),
      });
    for (const call of message.tool_calls || []) {
      const outcome = outcomes.get(`${index}:${call.id}`);
      result.transcript.push({
        kind: "tool",
        id: prefix + "-" + call.id,
        toolCallId: call.id,
        title: call.name,
        toolKind: toolKind(call.name),
        rawInput: args(call.args),
        status: !outcome || outcome.is_error ? "failed" : "completed",
        content: [
          {
            text: outcome
              ? displayText(outcome.content)
              : "此工具调用未完成或已中断",
          },
        ],
        terminalOutput:
          toolKind(call.name) === "execute" && outcome
            ? displayText(outcome.content)
            : undefined,
        details: outcome?.tool_details,
      });
    }
  });
  return result;
}
function eventProjection(
  state: RunProjection,
  event: AgentEvent,
  key: string,
): RunProjection {
  const transcript = [...state.transcript];
  const runKey = state.run?.run_id || "stream";
  if (event.type === "text_delta" && event.text_delta) {
    const last = transcript.at(-1);
    if (last?.kind === "assistant")
      transcript[transcript.length - 1] = {
        ...last,
        text: last.text + event.text_delta,
      };
    else
      transcript.push({
        kind: "assistant",
        id: `${runKey}-${key}-text`,
        text: event.text_delta,
      });
    return { ...state, transcript, phase: "responding" };
  }
  if (event.type === "thinking_delta" && event.text_delta) {
    const last = transcript.at(-1);
    if (last?.kind === "thought")
      transcript[transcript.length - 1] = {
        ...last,
        text: last.text + event.text_delta,
      };
    else
      transcript.push({
        kind: "thought",
        id: `${runKey}-${key}-thought`,
        text: event.text_delta,
      });
    return { ...state, transcript, phase: "thinking" };
  }
  if (event.type === "tool_start") {
    if (
      !transcript.some(
        (item) =>
          item.kind === "tool" &&
          item.toolCallId === event.tool_call_id &&
          item.id.startsWith(runKey + "-tool-"),
      )
    )
      transcript.push({
        kind: "tool",
        id: `${runKey}-tool-${event.tool_call_id || key}`,
        toolCallId: event.tool_call_id || key,
        title: event.tool_name || "tool",
        toolKind: toolKind(event.tool_name || ""),
        rawInput: event.tool_args,
        status: "in_progress",
        content: [],
      });
    return { ...state, transcript, phase: "tool" };
  }
  if (event.type === "tool_update" || event.type === "tool_end") {
    const text = displayText(
      event.type === "tool_update" ? event.partial_result : event.tool_result,
    );
    const updated = transcript.map((item) =>
      item.kind === "tool" &&
      item.toolCallId === event.tool_call_id &&
      item.id.startsWith(runKey + "-tool-")
        ? {
            ...item,
            content: text ? [{ text }] : item.content,
            terminalOutput:
              item.toolKind === "execute" ? text : item.terminalOutput,
            status:
              event.type === "tool_end"
                ? ((event.is_error ? "failed" : "completed") as
                    | "failed"
                    | "completed")
                : item.status,
            details: (event.tool_details ||
              (event.tool_result as any)?.Details) as
              | Record<string, unknown>
              | undefined,
          }
        : item,
    );
    return {
      ...state,
      transcript: updated,
      phase: event.type === "tool_end" ? "thinking" : "tool",
    };
  }
  if (event.type === "confirmation_request" && event.confirmation_id) {
    const value = {
      confirmation_id: event.confirmation_id,
      tool_call_id: event.tool_call_id || "",
      tool_name: event.tool_name || "tool",
      description: event.description || "此操作需要确认",
      args: event.tool_args,
    };
    return {
      ...state,
      confirmations: [
        ...state.confirmations.filter(
          (c) => c.confirmation_id !== value.confirmation_id,
        ),
        value,
      ],
      phase: "approval",
    };
  }
  if (event.type === "confirmation_result")
    return {
      ...state,
      confirmations: state.confirmations.filter(
        (c) => c.confirmation_id !== event.confirmation_id,
      ),
      phase: "thinking",
    };
  if (event.type === "done") {
    const text =
      event.final_message?.text || displayText(event.final_message?.content);
    if (text) {
      const last = transcript.at(-1);
      if (last?.kind === "assistant" && last.id.startsWith(runKey))
        transcript[transcript.length - 1] = { ...last, text };
      else
        transcript.push({
          kind: "assistant",
          id: `${runKey}-${key}-final`,
          text,
        });
    }
  }
  if (event.type === "error")
    transcript.push({
      kind: "error",
      id: `${runKey}-${key}-error`,
      text: event.error || "任务失败",
    });
  return {
    ...state,
    transcript,
    phase: event.type === "error" ? "error" : state.phase,
  };
}
export function reduceEnvelope(
  state: RunProjection,
  message: Envelope,
): RunProjection {
  if (message.type === "snapshot") {
    if (!message.reset && (message.seq || 0) < state.seq) return state;
    let next = historyProjection(message.messages || []);
    next.run = message.run;
    if (message.run?.prompt)
      next.transcript.push({
        kind: "user",
        id: `${message.run.run_id}-user`,
        text: message.run.prompt,
      });
    (message.events || []).forEach((event, index) => {
      next = eventProjection(next, event as AgentEvent, `snapshot-${index}`);
    });
    return terminalProjection({
      ...next,
      seq: message.seq || 0,
      confirmations: message.pending_confirmations || [],
      phase: message.pending_confirmations?.length
        ? "approval"
        : isActiveRun(message.run)
          ? next.phase === "idle"
            ? "thinking"
            : next.phase
          : message.run?.state === "failed"
            ? "error"
            : message.run?.state === "cancelled"
              ? "cancelled"
              : "idle",
    });
  }
  if (message.type === "replay") {
    let next: RunProjection = { ...state, run: message.run || state.run };
    for (const event of message.events || [])
      next = reduceEnvelope(next, event as Envelope);
    return terminalProjection({
      ...next,
      seq: Math.max(next.seq, message.seq || 0),
      confirmations: message.pending_confirmations || [],
    });
  }
  if (message.type === "accepted") {
    if (state.run && state.run.run_id === message.run_id)
      return terminalProjection({
        ...state,
        run: {
          ...state.run,
          state: message.state || state.run.state,
          error: message.message || message.error || state.run.error,
        },
      });
    const run = {
      run_id: message.run_id || "",
      request_id: message.request_id,
      error: message.message || message.error,
      state: message.state || "running",
      prompt: message.prompt,
    };
    return terminalProjection({
      ...state,
      run,
      phase: isActiveRun(run) ? "thinking" : "idle",
      confirmations: [],
      transcript: message.prompt
        ? [
            ...state.transcript,
            { kind: "user", id: `${run.run_id}-user`, text: message.prompt },
          ]
        : state.transcript,
    });
  }
  if (message.seq && message.seq <= state.seq) return state;
  if (
    message.run_id &&
    state.run?.run_id &&
    message.run_id !== state.run.run_id
  )
    return state;
  let next = { ...state, seq: Math.max(state.seq, message.seq || 0) };
  if (message.type === "confirmation" && message.confirmation)
    return {
      ...next,
      run: next.run
        ? { ...next.run, state: "waiting_confirmation" }
        : undefined,
      confirmations: [
        ...state.confirmations.filter(
          (c) => c.confirmation_id !== message.confirmation!.confirmation_id,
        ),
        message.confirmation,
      ],
      phase: "approval",
    };
  if (message.type === "event" && message.event)
    next = eventProjection(
      next,
      message.event,
      String(message.seq || Date.now()),
    );
  if (message.type === "status") {
    const status =
      message.state || (message.streaming ? "running" : "completed");
    const active = status === "running" || status === "waiting_confirmation";
    next = {
      ...next,
      run: next.run
        ? {
            ...next.run,
            state: status,
            error: message.message || message.error || next.run.error,
          }
        : undefined,
      phase: active
        ? next.phase === "idle"
          ? "thinking"
          : next.phase
        : status === "failed"
          ? "error"
          : status === "cancelled"
            ? "cancelled"
            : "idle",
      confirmations: active ? next.confirmations : [],
    };
  }
  if (message.type === "confirmed") {
    const confirmations = next.confirmations.filter(
      (c) => c.confirmation_id !== message.confirmation_id,
    );
    next = {
      ...next,
      confirmations,
      run:
        next.run && next.run.state === "waiting_confirmation"
          ? {
              ...next.run,
              state: confirmations.length ? "waiting_confirmation" : "running",
            }
          : next.run,
      phase: "thinking",
    };
  }
  return terminalProjection(next);
}

function terminalProjection(state: RunProjection): RunProjection {
  const status = state.run?.state;
  if (isActiveRun(state.run))
    return {
      ...state,
      phase:
        state.confirmations.length || status === "waiting_confirmation"
          ? "approval"
          : state.phase === "idle"
            ? "thinking"
            : state.phase,
    };
  if (!status) return state;
  const transcript = state.transcript.map((item) =>
    item.kind === "tool" &&
    (item.status === "in_progress" || item.status === "pending")
      ? {
          ...item,
          status: "failed" as const,
          content: [...item.content, { text: "此工具调用已中断" }],
        }
      : item,
  );
  if (
    status === "interrupted" &&
    !transcript.some((item) => item.id === state.run?.run_id + "-interrupted")
  )
    transcript.push({
      kind: "error",
      id: state.run?.run_id + "-interrupted",
      text:
        state.run?.error ||
        "服务重启使任务中断，请检查已执行的操作后再明确重新执行。",
    });
  return {
    ...state,
    transcript,
    confirmations: [],
    phase:
      status === "interrupted"
        ? "interrupted"
        : status === "failed"
          ? "error"
          : status === "cancelled"
            ? "cancelled"
            : "idle",
  };
}
