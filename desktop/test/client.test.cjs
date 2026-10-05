const { test } = require("node:test");
const assert = require("node:assert/strict");
const {
  emptyProjection,
  historyProjection,
  reduceEnvelope,
} = require("../.test-output/src/client/reducer.js");
const {
  parseCommand,
  filterCommands,
} = require("../.test-output/src/client/commands.js");
const { AgentTransport } = require("../.test-output/src/client/transport.js");
const accepted = (s = emptyProjection()) =>
  reduceEnvelope(s, {
    type: "accepted",
    session_id: "session",
    run_id: "run",
    request_id: "request",
    state: "running",
    prompt: "帮我检查代码",
  });
const delta = (s, seq, text = "你好") =>
  reduceEnvelope(s, {
    type: "event",
    run_id: "run",
    seq,
    event: { type: "text_delta", text_delta: text },
  });
test("accepted is idempotent; replayed chunks never duplicate text", () => {
  let s = accepted();
  s = accepted(s);
  s = delta(s, 1);
  s = delta(s, 1);
  assert.equal(s.transcript.length, 2);
  assert.equal(s.transcript[1].text, "你好");
  s = delta(s, 2, "！");
  assert.equal(s.transcript[1].text, "你好！");
});
test("late old-run events cannot corrupt a newer run", () => {
  let s = accepted();
  s = reduceEnvelope(s, {
    type: "accepted",
    run_id: "new",
    state: "running",
    prompt: "新任务",
  });
  const previous = s;
  s = reduceEnvelope(s, {
    type: "event",
    run_id: "run",
    seq: 999,
    event: { type: "text_delta", text_delta: "旧文本" },
  });
  assert.equal(s, previous);
});
test("history reconstructs thought/text/tools and marks missing results interrupted", () => {
  const s = historyProjection([
    { role: "user", content: "任务" },
    {
      role: "assistant",
      content: "先检查",
      thinking: "思考",
      tool_calls: [
        { id: "a", name: "bash", args: '{"command":"pwd"}' },
        { id: "b", name: "read_file" },
      ],
    },
    { role: "tool", tool_call_id: "a", content: "/project" },
  ]);
  assert.equal(s.transcript.filter((i) => i.kind === "tool").length, 2);
  assert.equal(
    s.transcript.find((i) => i.toolCallId === "a").rawInput.command,
    "pwd",
  );
  assert.equal(s.transcript.find((i) => i.toolCallId === "b").status, "failed");
});
test("snapshot reconstructs run baseline and authoritative cancel state", () => {
  const s = reduceEnvelope(emptyProjection(), {
    type: "snapshot",
    seq: 20,
    run: { run_id: "run", state: "cancelled", prompt: "检查" },
    messages: [{ role: "user", content: "旧历史" }],
    events: [{ type: "text_delta", text_delta: "部分结果" }],
  });
  assert.deepEqual(
    s.transcript.map((i) => i.text),
    ["旧历史", "检查", "部分结果"],
  );
  assert.equal(s.phase, "cancelled");
  assert.equal(s.seq, 20);
});
test("tool arguments, progress and final result remain available", () => {
  let s = accepted();
  for (const [seq, event] of [
    [
      1,
      {
        type: "tool_start",
        tool_call_id: "a",
        tool_name: "bash",
        tool_args: { command: "pwd" },
      },
    ],
    [2, { type: "tool_update", tool_call_id: "a", partial_result: "进行中" }],
    [
      3,
      {
        type: "tool_end",
        tool_call_id: "a",
        tool_result: { Content: "完整输出", UserFacing: "用户输出" },
        is_error: false,
      },
    ],
  ])
    s = reduceEnvelope(s, { type: "event", run_id: "run", seq, event });
  const tool = s.transcript.find((i) => i.kind === "tool");
  assert.equal(tool.rawInput.command, "pwd");
  assert.equal(tool.status, "completed");
  assert.equal(tool.terminalOutput, "用户输出");
});
test("confirmation message restores approval then clears after confirmed", () => {
  let s = accepted();
  s = reduceEnvelope(s, {
    type: "confirmation",
    run_id: "run",
    seq: 2,
    confirmation: {
      confirmation_id: "confirm",
      tool_call_id: "tool",
      tool_name: "bash",
      description: "危险操作",
    },
  });
  assert.equal(s.phase, "approval");
  s = reduceEnvelope(s, {
    type: "confirmed",
    run_id: "run",
    confirmation_id: "confirm",
  });
  assert.equal(s.confirmations.length, 0);
  assert.equal(s.phase, "thinking");
});
test("replay merges sequence cursor and final status", () => {
  let s = delta(accepted(), 1, "a");
  s = reduceEnvelope(s, {
    type: "replay",
    seq: 3,
    run: { run_id: "run", state: "completed" },
    events: [
      {
        type: "event",
        run_id: "run",
        seq: 1,
        event: { type: "text_delta", text_delta: "a" },
      },
      {
        type: "event",
        run_id: "run",
        seq: 2,
        event: { type: "text_delta", text_delta: "b" },
      },
      { type: "status", run_id: "run", seq: 3, state: "completed" },
    ],
  });
  assert.equal(s.transcript[1].text, "ab");
  assert.equal(s.phase, "idle");
  assert.equal(s.run.state, "completed");
});
test("slash is parsed only as a command prefix, supports argument newlines and // escape", () => {
  assert.equal(parseCommand("//普通消息"), null);
  assert.deepEqual(parseCommand("/workflow 第一行\n第二行"), {
    name: "workflow",
    args: "第一行\n第二行",
  });
  assert(filterCommands("/m").some((c) => c.name === "mcp"));
  assert.equal(filterCommands("/workflow 任务"), null);
});
test("transport retains prompt until accepted and reports an unsent message", async () => {
  let handler,
    sent = true;
  global.window = {
    piAPI: {
      onAgentEvent: (callback) => {
        handler = callback;
        return () => {};
      },
      connect: async () => {},
      disconnect: async () => {},
      send: async () => sent,
    },
  };
  const transport = new AgentTransport();
  await transport.connect("http://server");
  handler({ type: "transport", state: "connected" });
  sent = false;
  await assert.rejects(
    transport.prompt("session", "草稿", "id"),
    /消息没有发送/,
  );
  sent = true;
  const promise = transport.prompt("session", "草稿", "id2");
  handler({
    type: "message",
    data: {
      type: "accepted",
      session_id: "session",
      run_id: "run",
      request_id: "id2",
      state: "running",
    },
  });
  assert.equal((await promise).prompt, "草稿");
  await transport.disconnect();
  delete global.window;
});
test("explicit disconnect rejects pending ACK and does not send cancel", async () => {
  let handler;
  const sent = [];
  global.window = {
    piAPI: {
      onAgentEvent: (callback) => {
        handler = callback;
        return () => {};
      },
      connect: async () => {},
      disconnect: async () => {},
      send: async (value) => {
        sent.push(value);
        return true;
      },
    },
  };
  const transport = new AgentTransport();
  await transport.connect("http://server");
  handler({ type: "transport", state: "connected" });
  const promise = transport.prompt("s", "保留草稿", "id");
  await transport.disconnect();
  await assert.rejects(promise, /草稿已保留/);
  assert(sent.every((value) => value.type !== "cancel"));
  delete global.window;
});
test("late confirmations and stale snapshots cannot resurrect older work", () => {
  let state = delta(accepted(), 4);
  const before = state;
  state = reduceEnvelope(state, {
    type: "confirmation",
    run_id: "old",
    seq: 50,
    confirmation: {
      confirmation_id: "old",
      tool_name: "bash",
      tool_call_id: "a",
      description: "old",
    },
  });
  assert.equal(state, before);
  state = reduceEnvelope(state, {
    type: "snapshot",
    seq: 1,
    run: { run_id: "run", state: "running" },
    messages: [],
    events: [],
  });
  assert.equal(state, before);
});
test("final-only provider output is shown; final text does not duplicate streamed output", () => {
  let state = accepted();
  state = reduceEnvelope(state, {
    type: "event",
    run_id: "run",
    seq: 1,
    event: { type: "done", final_message: { text: "最终回复" } },
  });
  assert.equal(state.transcript.at(-1).text, "最终回复");
  state = reduceEnvelope(state, {
    type: "event",
    run_id: "run",
    seq: 2,
    event: { type: "done", final_message: { text: "最终回复" } },
  });
  assert.equal(
    state.transcript.filter((i) => i.kind === "assistant").length,
    1,
  );
});
test("cancelled snapshots mark outstanding tools interrupted and clear approvals", () => {
  const state = reduceEnvelope(emptyProjection(), {
    type: "snapshot",
    seq: 4,
    run: { run_id: "r", state: "cancelled", prompt: "任务" },
    messages: [],
    events: [
      {
        type: "tool_start",
        tool_name: "bash",
        tool_call_id: "a",
        tool_args: { command: "sleep 1" },
      },
    ],
    pending_confirmations: [
      {
        confirmation_id: "c",
        tool_name: "bash",
        tool_call_id: "a",
        description: "desc",
      },
    ],
  });
  assert.equal(state.phase, "cancelled");
  assert.equal(state.confirmations.length, 0);
  assert.equal(state.transcript.at(-1).status, "failed");
});
test("server catalog offers workflow subcommands without treating normal arguments as suggestions", () => {
  const catalog = [
    {
      name: "workflow",
      label: "工作流",
      description: "工作流",
      subcommands: [
        { name: "run", description: "运行" },
        { name: "list", description: "列表" },
      ],
    },
  ];
  assert.equal(
    filterCommands("/workflow r", catalog)[0].insert,
    "/workflow run ",
  );
  assert.equal(filterCommands("/workflow run 描述", catalog), null);
});
test("receipt snapshot explicitly resets a cursor after core restart without duplicating its user message", () => {
  let state = delta(accepted(), 99, "已产生部分结果");
  state = reduceEnvelope(state, {
    type: "snapshot",
    reset: true,
    run: {
      run_id: "run",
      request_id: "request",
      state: "interrupted",
      prompt: "",
      error: "核心服务重启",
    },
    messages: [
      { role: "user", content: "帮我检查代码" },
      { role: "assistant", content: "已产生部分结果" },
    ],
    events: [],
  });
  assert.equal(state.seq, 0);
  assert.equal(state.phase, "interrupted");
  assert.equal(
    state.transcript.filter((item) => item.kind === "user").length,
    1,
  );
  assert.equal(state.transcript.at(-1).text, "核心服务重启");
});
test("waiting-confirmation snapshots keep approval and tools active across reconnect", () => {
  const confirmation = {
    confirmation_id: "approval",
    tool_call_id: "tool",
    tool_name: "bash",
    description: "确认执行",
  };
  let state = reduceEnvelope(emptyProjection(), {
    type: "snapshot",
    seq: 2,
    run: { run_id: "r", state: "waiting_confirmation", prompt: "运行命令" },
    messages: [],
    events: [{ type: "tool_start", tool_call_id: "tool", tool_name: "bash" }],
    pending_confirmations: [confirmation],
  });
  assert.equal(state.phase, "approval");
  assert.equal(state.confirmations.length, 1);
  assert.equal(state.transcript.at(-1).status, "in_progress");
  state = reduceEnvelope(state, {
    type: "status",
    run_id: "r",
    seq: 3,
    state: "running",
    streaming: true,
  });
  state = reduceEnvelope(state, {
    type: "confirmed",
    run_id: "r",
    confirmation_id: "approval",
  });
  assert.equal(state.confirmations.length, 0);
  assert.equal(state.phase, "thinking");
  assert.equal(state.transcript.at(-1).status, "in_progress");
});
test("visible text switches the run to responding while real thinking remains distinct", () => {
  let state = delta(accepted(), 1, "即时回复");
  assert.equal(state.phase, "responding");
  state = reduceEnvelope(state, {
    type: "event",
    seq: 2,
    run_id: "run",
    event: { type: "thinking_delta", text_delta: "进一步推理" },
  });
  assert.equal(state.phase, "thinking");
  state = delta(state, 3, "继续回复");
  assert.equal(state.phase, "responding");
});
test("session timestamps normalize Unix seconds while retaining milliseconds", () => {
  const {
    timestampMillis,
  } = require("../.test-output/src/client/timestamps.js");
  const now = Date.now();
  assert.equal(
    timestampMillis(Math.floor(now / 1000)),
    Math.floor(now / 1000) * 1000,
  );
  assert.equal(timestampMillis(now), now);
  assert.equal(timestampMillis(undefined, now), now);
  assert.equal(timestampMillis(Number.NaN), 0);
});
test("different runs can reuse tool IDs without overwriting earlier command results", () => {
  let state = emptyProjection();
  for (const message of [
    { type: "accepted", run_id: "old", state: "running", prompt: "第一条任务" },
    {
      type: "event",
      run_id: "old",
      seq: 1,
      event: {
        type: "tool_start",
        tool_call_id: "call_0",
        tool_name: "bash",
        tool_args: { command: "old command" },
      },
    },
    {
      type: "event",
      run_id: "old",
      seq: 2,
      event: {
        type: "tool_end",
        tool_call_id: "call_0",
        tool_result: "old result",
      },
    },
    { type: "status", run_id: "old", seq: 3, state: "completed" },
    { type: "accepted", run_id: "new", state: "running", prompt: "第二条任务" },
    {
      type: "event",
      run_id: "new",
      seq: 4,
      event: {
        type: "tool_start",
        tool_call_id: "call_0",
        tool_name: "bash",
        tool_args: { command: "new command" },
      },
    },
    {
      type: "event",
      run_id: "new",
      seq: 5,
      event: {
        type: "tool_update",
        tool_call_id: "call_0",
        partial_result: "new progress",
      },
    },
    {
      type: "event",
      run_id: "new",
      seq: 6,
      event: {
        type: "tool_end",
        tool_call_id: "call_0",
        tool_result: "new result",
      },
    },
  ])
    state = reduceEnvelope(state, message);
  const tools = state.transcript.filter((item) => item.kind === "tool");
  assert.deepEqual(
    tools.map((tool) => [tool.rawInput.command, tool.content[0].text]),
    [
      ["old command", "old result"],
      ["new command", "new result"],
    ],
  );
});
test("history pairs reused tool IDs with their own assistant invocation", () => {
  const state = historyProjection([
    {
      role: "assistant",
      tool_calls: [
        { id: "call_0", name: "bash", args: { command: "old command" } },
      ],
    },
    { role: "tool", tool_call_id: "call_0", content: "old result" },
    {
      role: "assistant",
      tool_calls: [
        { id: "call_0", name: "bash", args: { command: "new command" } },
      ],
    },
    { role: "tool", tool_call_id: "call_0", content: "new result" },
    {
      role: "assistant",
      tool_calls: [
        {
          id: "call_0",
          name: "bash",
          args: { command: "interrupted command" },
        },
      ],
    },
  ]);
  assert.deepEqual(
    state.transcript.map((tool) => [
      tool.rawInput.command,
      tool.content[0].text,
      tool.status,
    ]),
    [
      ["old command", "old result", "completed"],
      ["new command", "new result", "completed"],
      ["interrupted command", "此工具调用未完成或已中断", "failed"],
    ],
  );
});

test("reasoning streams before tools; snapshot replay retains duration without duplicating text", () => {
  const usage = { estimated_tokens: 4321, context_window: 128000, window_known: true, model: 'gpt-4o', messages: 3000, system: 1000, tools: 321 };
  const events = [
    { type: 'context_usage', context_usage: usage },
    { type: 'thinking_delta', text_delta: '先检查', timestamp: 1000 },
    { type: 'thinking_delta', text_delta: '目录' },
    { type: 'thinking_end', duration_ms: 2100 },
    { type: 'tool_start', tool_call_id: 'read', tool_name: 'read' },
  ];
  let s = accepted();
  events.forEach((event, index) => { s = reduceEnvelope(s, { type: 'event', run_id: 'run', seq: index + 1, event }); });
  const thought = s.transcript.find(i => i.kind === 'thought');
  assert.equal(thought.text, '先检查目录');
  assert.equal(thought.durationMs, 2100);
  assert.equal(thought.active, false);
  assert.equal(s.contextUsage.estimated_tokens, 4321);
  const restored = reduceEnvelope(emptyProjection(), { type: 'snapshot', seq: 5, run: s.run, messages: [], events });
  assert.equal(restored.transcript.filter(i => i.kind === 'thought').length, 1);
  assert.equal(restored.transcript.find(i => i.kind === 'thought').text, thought.text);
  assert.deepEqual(restored.contextUsage, usage);
  assert.deepEqual(reduceEnvelope(restored, { type: 'event', run_id: 'run', seq: 5, event: events[1] }), restored);
  const history = historyProjection([{ role: 'assistant', thinking: '保存的思考', thinking_duration_ms: 900 }]);
  assert.equal(history.transcript[0].durationMs, 900);
});

test("cancelled reasoning stops its live clock and context counters replace rather than accumulate", () => {
  let s = accepted();
  s = reduceEnvelope(s, { type: 'event', run_id: 'run', seq: 1, event: { type: 'thinking_delta', text_delta: '思考片段', timestamp: 1000 } });
  assert.equal(s.transcript.at(-1).active, true);
  s = reduceEnvelope(s, { type: 'status', run_id: 'run', seq: 2, state: 'cancelled' });
  assert.equal(s.transcript.at(-1).active, false);
  for (const count of [100, 80]) s = reduceEnvelope(s, { type: 'event', run_id: 'run', event: { type: 'context_usage', context_usage: { estimated_tokens: count } } });
  assert.equal(s.contextUsage.estimated_tokens, 80);
});

test('separate reasoning blocks preserve their own duration and never merge with historical thinking', () => {
 let s = accepted();
 for (const [i,event] of [
  {type:'thinking_delta',text_delta:'first',timestamp:1000},
  {type:'thinking_end',duration_ms:300},
  {type:'thinking_delta',text_delta:'second',timestamp:1400},
  {type:'thinking_end',duration_ms:600},
 ].entries()) s=reduceEnvelope(s,{type:'event',run_id:'run',seq:i+1,event});
 const thoughts=s.transcript.filter(item=>item.kind==='thought');
 assert.equal(thoughts.length,2);
 assert.equal(thoughts[0].text,'first');
 assert.equal(thoughts[0].durationMs,300);
 assert.equal(thoughts[1].text,'second');
 assert.equal(thoughts[1].durationMs,600);
});

test("declined tool remains distinct from success in live events and history", () => {
  let s = accepted();
  s = reduceEnvelope(s, { type: "event", run_id: "run", seq: 1, event: { type: "tool_start", tool_call_id: "deny", tool_name: "write" } });
  s = reduceEnvelope(s, { type: "event", run_id: "run", seq: 2, event: { type: "tool_end", tool_call_id: "deny", is_error: false, tool_result: "user declined this action", tool_details: { approval: "declined" } } });
  assert.equal(s.transcript.find(item => item.kind === "tool").status, "declined");
  const history = historyProjection([
    { role: "assistant", tool_calls: [{ id: "deny", name: "write" }] },
    { role: "tool", tool_call_id: "deny", content: "user declined this action", is_error: false, tool_details: { approval: "declined" } },
  ]);
  assert.equal(history.transcript[0].status, "declined");
});
