# 核心机制

面向开发者的机制细节，补充[架构](ARCHITECTURE.md)的概览。以当前源码为准；行为改动时同步更新本文件。

## Agent 循环与事件流

[sdk/agent/loop.go](../sdk/agent/loop.go) 的 `runAgentLoop()` 是 `RunLoop`（同步）与 `PromptStream`（流式）的共享核心，通过 `consumeStreamFunc` 回调区分消费行为。外层循环处理 follow-up 与 goal 评估，内层循环处理工具调用。

事件以类型化的 `AgentEvent` 结构发出（[sdk/agent/event.go](../sdk/agent/event.go)）：Agent 与 Turn 的开始/结束、工具执行开始/增量/结束、压缩完成（含 MicroCompact）、确认请求/结果、循环检测、goal 完成。Web、TUI、飞书各自消费同一事件流，飞书按自身节奏合并刷新。

## 工具执行生命周期

调用先分区：连续的 safe 工具组成并行批次，unsafe 工具串行。每个调用依次经过：

**Validate → PrepareArguments → Before hooks → 确认门 → Execute → After hooks → 输出摘要**

- **确认门**：工具实现 `ToolWithConfirmation` 并在 `RequiresConfirmation` 中声明需要确认时，Agent 先发出 `EventConfirmationRequest`，再调用注入的 `ConfirmFunc`；未注入（serve / 飞书单向流）默认放行。拒绝时工具不执行，拒绝信息作为工具结果回给模型。
- **Before / After hooks**：Before 返回 error 则阻止执行，错误信息成为工具结果；After 返回 error 则整体视为失败，原结果保留在 `AfterHookError` 供排查。
- **循环检测**：同一工具以相同参数连续调用达到阈值（默认 5）时发出 `EventLoopDetected` 并注入软提醒，不中断执行；每次 Prompt 开始时重置。
- **输出摘要**：工具输出超过阈值时，After hook 生成确定性结构摘要（结构 + 摘录）替换模型可见内容，完整输出保留在 `UserFacing` 供界面展示。见 [sdk/agent/synopsis.go](../sdk/agent/synopsis.go)。

## 双层上下文压缩

[sdk/compaction](../sdk/compaction/compaction.go) 两级触发，先便宜后昂贵：

| 层级 | 触发条件 | 动作 | 成本 |
|---|---|---|---|
| MicroCompact | token 占窗口比超过 `MicroCompactRatio`（默认 0.6） | 旧 tool result 内容替换为占位符，保留最近 `MicroKeepRecent`（默认 5）个完整 | 零 LLM 调用 |
| 全量压缩 | `contextTokens > contextWindow - ReserveTokens`（约 90%） | `SummarizeFunc` 总结旧历史，压缩 entry 写入会话 JSONL | 一次 LLM 调用 |

其余默认值：`ReserveTokens` 16384，`KeepRecentTokens` 20000。`Agent.CompactNow()` 手动触发（`/compact` 命令），支持自定义总结指令。压缩前发出的钩子与事件见 [sdk/agent/event.go](../sdk/agent/event.go)。

## 外部工具

通过 HTTP 回调注册的工具（[sdk/agent/external_tool.go](../sdk/agent/external_tool.go)）在执行期分发，与内置工具走同一生命周期与确认门。
