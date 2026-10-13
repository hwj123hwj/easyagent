# 核心机制

面向开发者的机制细节，补充[架构](ARCHITECTURE.md)的概览。以当前源码为准；行为改动时同步更新本文件。

## Agent 循环与事件流

[sdk/agent/loop.go](../sdk/agent/loop.go) 的 `runAgentLoop()` 是 `RunLoop`（同步）与 `PromptStream`（流式）的共享核心，通过 `consumeStreamFunc` 回调区分消费行为。外层循环处理 follow-up 与 goal 评估，内层循环处理工具调用。

事件以类型化的 `AgentEvent` 结构发出（[sdk/agent/event.go](../sdk/agent/event.go)）：Agent 与 Turn 的开始/结束、工具执行开始/增量/结束、压缩完成（含 MicroCompact）、确认请求/结果、循环检测、goal 完成。Web、TUI、飞书各自消费同一事件流，飞书按自身节奏合并刷新。

### 流式终态与 SDK 消费

Provider 必须收到协议完成标记才确认成功；缺少终态的 EOF、显式业务错误均返回失败，错误优先于已收到的完成标记。已收到的文字、思考、工具参数和用量保留在结果中，失败的工具调用不会继续执行。

消费 `ai.EventStream` 时先读完 `Events()`，再取 `Result()`；消费 `Agent.PromptStream` 时也要读到 channel 关闭。不能看到 `error` 或 `done` 就提前返回，否则可能堵住生产者并影响后续请求。`done` 可携带失败或取消后的部分消息，成功与否须结合错误、上下文取消和 `stop_reason` 判断。实现见 [stream_result.go](../sdk/agent/stream_result.go) 与 [agent.go](../sdk/agent/agent.go)。

消息写入失败会阻止后续执行；JSONL 的 `Write` 和 `Sync` 都成功后才更新内存索引。失败或取消时尝试用独立的收尾上下文保存部分消息（`error` / `aborted`）；存储本身失败会返回错误，不能承诺部分结果已经持久化。

固定对照版本为 ZCode `ba61ca16`，参考其业务错误优先、取消时保存部分输出和工具声明保存后再执行的原则；缺终态 EOF 检查与 JSONL 同步错误传播由本项目的 Go 契约实现。源码对照与差异说明见 [PR #130](https://github.com/hwj123hwj/easyagent/pull/130)。

## 工具执行生命周期

调用先分区：连续的 safe 工具组成并行批次，unsafe 工具串行。每个调用依次经过：

**Validate → PrepareArguments → Before hooks → 确认门 → Execute → After hooks → 输出摘要**

- **确认门**：工具实现 `ToolWithConfirmation` 并声明需要确认时，Agent 先发出 `EventConfirmationRequest`，再调用注入的 `ConfirmFunc`。服务端 run 注入交互审批；必须审批或交互的工具在没有确认通道时拒绝执行，普通可选确认在未注入回调时沿用 SDK 默认放行。拒绝时工具不执行，拒绝信息作为工具结果回给模型；`ask_user_question` 即使完全权限也等待用户回答。
- **Before / After hooks**：Before 返回 error 则阻止执行，错误信息成为工具结果；After 返回 error 则整体视为失败，原结果保留在 `AfterHookError` 供排查。
- **循环检测**：同一工具以相同参数连续调用达到阈值（默认 5）时发出 `EventLoopDetected` 并注入软提醒，不中断执行；每次 Prompt 开始时重置。
- **输出摘要**：默认阈值为 51200 字符，可用 `EA_SYNOPSIS_THRESHOLD` 调整；`read`、`grep`、`read_many_files` 豁免概览化。其他超限输出由 After hook 生成结构摘要替换模型可见内容，完整输出保留在 `UserFacing` 并由 `full_output` 按 ref 取回（最近 16 条）。见 [sdk/agent/synopsis.go](../sdk/agent/synopsis.go)。

## 双层上下文压缩

[sdk/compaction](../sdk/compaction/compaction.go) 按完整请求的文本估算（消息、系统提示词、工具定义）检查水位；达到全量阈值时直接摘要，低水位才尝试微压缩：

| 层级 | 触发条件 | 动作 | 成本 |
|---|---|---|---|
| MicroCompact | 占用超过 `MicroCompactRatio`（默认 0.6），且本次节省至少 `MinSavingsTokens`（默认 2048） | 清理旧的可压缩工具结果，保留最近 `MicroKeepRecent`（默认 5）个完整结果；写入独立微压缩记录 | 零 LLM 调用 |
| 全量压缩 | `contextTokens > min(contextWindow × AutoCompactRatio, contextWindow - ReserveTokens)`，默认比例 0.8，且已配置 `SummarizeFunc` | 总结旧历史，压缩 entry 写入会话 JSONL；不靠少量微压缩延后全量摘要 | 一次 LLM 调用 |

其余默认值：`ReserveTokens` 16384，`KeepRecentTokens` 20000。`Agent.CompactNow()` 手动触发（`/compact` 命令），支持自定义总结指令。压缩前发出的钩子与事件见 [sdk/agent/event.go](../sdk/agent/event.go)。

## 外部工具

通过 HTTP 回调注册的工具（[sdk/agent/external_tool.go](../sdk/agent/external_tool.go)）在执行期分发，与内置工具走同一生命周期与确认门。
