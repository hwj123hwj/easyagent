> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# cc-haha 调研报告 — 核心引擎源码分析

> 调研日期：2026-05-24
> 来源：本地 `/Users/weijian/Desktop/develop/test/pi/cc-haha`，GitHub: [NanmiCoder/cc-haha](https://github.com/NanmiCoder/cc-haha)
> 调研目标：聚焦 cc-haha 的 **Agent 循环、Tool 系统、系统提示构建、上下文管理** 等核心源码，与 EasyAgent 做深度对比，找出 EasyAgent coding-agent 的差距

---

## 1. 概述

### 项目定位

cc-haha 是基于 Anthropic Claude Code（官方 CLI）泄露源码修复而来的项目。**它的核心引擎就是 Claude Code 的底层架构**——这不是一个独立框架，而是一个完整产品级 Coding Agent 的真实源码。

| 项目 | 角色 | 技术栈 | 核心代码量 |
|------|------|--------|-----------|
| cc-haha (Claude Code) | 编码 Agent CLI/Desktop | TypeScript/Bun | `src/` ~400 个模块，`src/utils/` 313 个工具函数 |
| EasyAgent | 通用 Agent 框架 + coding-agent | Go | 核心层 ~50 个模块 |

### 核心发现摘要

1. **Agent 循环是 AsyncGenerator 驱动**：cc-haha 的 `query()` 是一个 `async*` generator (`src/query.ts:220`)，用 `while(true)` + `yield*` 实现内外层嵌套——外层 follow-up，内层 API 流式循环。EasyAgent 的双层循环设计思路一致，但实现更显式。

2. **系统提示是精心设计的缓存策略**：`src/constants/prompts.ts` 将系统提示分为静态（跨组织缓存）和动态（按 section 注册）两部分，用 `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` 分割。EasyAgent 没有等效的缓存策略。

3. **Tool 系统是 15+ 方法的接口**：`src/Tool.ts` 的 `Tool<>` 泛型接口定义了 `call/description/isConcurrencySafe/isReadOnly/isDestructive/inputSchema/outputSchema/shouldDefer/maxResultChars` 等，粒度远超 EasyAgent。

4. **上下文压缩有 4 级渐进策略**：snip → microcompact → context collapse → autocompact，每级解决不同粒度的问题。EasyAgent 只有 1 级 LLM 摘要。

5. **工具执行有完整的编排层**：`src/services/tools/toolOrchestration.ts` 实现了 read-only 工具并行执行 + non-read-only 串行执行的分批策略。EasyAgent 的工具执行是顺序的。

---

## 2. Agent 循环深度分析

### 整体架构

cc-haha 的 Agent 循环分布在三个主要文件：

```
QueryEngine               (src/QueryEngine.ts)      — 外层封装：配置、状态、系统提示、memory、slash command
  └─ query()              (src/query.ts:220)        — 中层调度：循环管理、上下文压缩、API 调用、工具执行
       └─ queryLoop()     (src/query.ts:242)        — 内层：while(true) 每个 iteration 一次 LLM 调用
            ├─ deps.callModel()                     — LLM 流式调用
            ├─ runTools()                           — 工具编排（并发/串行）
            └─ 循环继续/终止判断                      — stop_reason / follow-up 检测
```

### `queryLoop()` 的每个迭代（src/query.ts:308）

```
┌──────────────────────────────────────────────────────────┐
│ while (true) {                                           │
│   1. 上下文压缩阶段：                                       │
│      ├─ snipCompactIfNeeded()    — 裁剪历史               │
│      ├─ microcompact()           — 细粒度压缩              │
│      ├─ contextCollapse()        — 折叠上下文              │
│      └─ autocompact()            — LLM 摘要压缩           │
│                                                           │
│   2. API 调用阶段：                                        │
│      ├─ stream_request_start   → yield                   │
│      ├─ deps.callModel()       → async generator         │
│      │   └─ 流式接收 text/thinking/tool_use blocks        │
│      └─ 收集 assistantMessages + toolUseBlocks            │
│                                                           │
│   3. 工具执行阶段：                                         │
│      ├─ runTools()             → 分批执行                 │
│      │   ├─ read-only 工具并行                              │
│      │   └─ non-read-only 工具串行                          │
│      └─ tool results → push messages                      │
│                                                           │
│   4. 继续判断：                                             │
│      ├─ tool_use detected → needsFollowUp = true          │
│      ├─ max_turns reached → 终止                          │
│      ├─ max_output_tokens → 恢复重试                       │
│      └─ 其他错误 → fallback model / 终止                   │
│                                                           │
│   5. state = { ... } → 继续迭代或 return                   │
│ }                                                         │
└──────────────────────────────────────────────────────────┘
```

**关键差异**（与 EasyAgent 对比）：

| 特性 | cc-haha | EasyAgent |
|------|---------|-------|
| 循环载体 | AsyncGenerator (`yield*`) | 回调 + goroutine |
| 上下文压缩 | 4 级渐进（snip/microcompact/collapse/autocompact） | 1 级 LLM 摘要 |
| Follow-up | 同一次 `query()` 循环内处理 | 外层 `AgentLoop.handleFollowUp()` 循环 |
| 工具执行 | 并行 read-only + 串行 write | 顺序执行 |
| 流式输出 | 细粒度 StreamEvent 事件 | EventBus + SSE |
| 错误恢复 | fallback model + max_output_tokens 重试 | 无 |
| Token 预算 | task_budget + turn token budget | 无 |

### `query()` 的状态管理（src/query.ts:205）

```typescript
type State = {
  messages: Message[]
  toolUseContext: ToolUseContext
  autoCompactTracking: AutoCompactTrackingState | undefined
  maxOutputTokensRecoveryCount: number      // max_output_tokens 恢复计数
  hasAttemptedReactiveCompact: boolean
  maxOutputTokensOverride: number | undefined
  pendingToolUseSummary: Promise<ToolUseSummaryMessage | null> | undefined
  stopHookActive: boolean | undefined
  turnCount: number
  transition: Continue | undefined           // 上一次迭代为何继续
}
```

`state` 在每个 `continue` 点被整体替换（`state = { ... }`），这种不可变风格保证了每个 iteration 对状态的修改不会意外泄漏。

---

## 3. Tool 系统深度分析

### Tool 接口设计（src/Tool.ts:362）

```typescript
export type Tool<
  Input extends AnyObject = AnyObject,
  Output = unknown,
  P extends ToolProgressData = ToolProgressData,
> = {
  name: string
  aliases?: string[]              // 向后兼容的别名
  searchHint?: string             // ToolSearch 关键词
  inputSchema: Input              // Zod v4 schema
  inputJSONSchema?: ToolInputJSONSchema
  outputSchema?: z.ZodType<unknown>

  call(args, context, canUseTool, parentMessage, onProgress): Promise<ToolResult<Output>>
  description(input, options): Promise<string>

  isConcurrencySafe(input): boolean   // 能否并行执行
  isReadOnly(input): boolean          // 是否只读
  isDestructive?(input): boolean      // 是否破坏性操作
  isEnabled(): boolean
  interruptBehavior?(): 'cancel' | 'block'
  isSearchOrReadCommand?(input): { isSearch: boolean; isRead: boolean; isList?: boolean }
  isOpenWorld?(input): boolean
  requiresUserInteraction?(): boolean

  shouldDefer?: boolean            // 延迟加载（ToolSearch）
  alwaysLoad?: boolean             // 始终加载（绕过 ToolSearch）
  isMcp?: boolean
  mcpInfo?: { serverName: string; toolName: string }
  maxResultChars?: number          // 结果截断阈值
  maxResultSizeBytes?: number
  backfillObservableInput?(input): void  // 补全可观测输入
}
```

**EasyAgent 的差距**：

| 功能 | cc-haha | EasyAgent | 重要度 |
|------|---------|-------|--------|
| 并发放行控制 | `isConcurrencySafe()` | ❌ | 高——决定工具能否并行执行 |
| 只读标记 | `isReadOnly()` | ❌ | 高——决定批次划分 |
| 破坏性标记 | `isDestructive()` | ❌ | 中——安全告警 |
| 结果截断 | `maxResultChars` | ✅ 有 | 持平 |
| 延迟加载 | `shouldDefer` + ToolSearch | ❌ | 中——减少初次提示 |
| 搜索/读分类 | `isSearchOrReadCommand` | ❌ | 低——UI 优化 |
| 输入补全 | `backfillObservableInput` | ❌ | 低——调试友好 |
| 进度回调 | `onProgress` | ❌ | 中——流式进度通知 |
| Schema 库 | Zod v4 | TypeBox | 持平 |

### 工具执行编排（src/services/tools/toolOrchestration.ts:19）

这是 cc-haha 最值得 EasyAgent 借鉴的设计之一：

```typescript
export async function* runTools(
  toolUseMessages: ToolUseBlock[],
  ...
): AsyncGenerator<MessageUpdate, void> {
  for (const { isConcurrencySafe, blocks } of partitionToolCalls(toolUseMessages, ctx)) {
    if (isConcurrencySafe) {
      // 所有 read-only 工具并行执行
      yield* runToolsConcurrently(blocks, ...)
    } else {
      // 非 read-only 工具串行执行，每个等待前一个完成
      yield* runToolsSerially(blocks, ...)
    }
  }
}
```

**`partitionToolCalls()` 的核心逻辑**（src/services/tools/toolOrchestration.ts:91）：
- 对每个 tool_use block，查找对应的 Tool 定义
- 调用 `tool.isConcurrencySafe(parsedInput)` 判断
- 将连续的 read-only tools 合并为一个批次并行执行
- 遇到 write tool 则插入分界，串行执行

**EasyAgent 当前行为**：所有工具顺序执行。对于 `Read` / `Grep` / `Glob` 这些纯读操作，完全可以并行。

### 工具执行完整链路（src/services/tools/toolExecution.ts:337）

`runToolUse()` 的执行流程：

```
runToolUse()
├─ 1. findToolByName() — 查找工具（支持别名回退）
├─ 2. 检查 abort signal
├─ 3. streamedCheckPermissionsAndCallTool() — 权限检查 + 工具调用
│     ├─ runPreToolUseHooks()      — 前置钩子
│     ├─ canUseTool()              — 权限判断
│     ├─ tool.call()               — 实际执行
│     ├─ processToolResultBlock()  — 结果截断/持久化
│     └─ runPostToolUseHooks()     — 后置钩子
└─ 4. 错误处理（分类、统计）
```

---

## 4. 系统提示构建深度分析

### 分层架构（src/constants/prompts.ts）

系统提示分为 **4 个区域**，按缓存策略分层：

```
区域 1：Static Intro          — getSimpleIntroSection()
区域 2：Static Body           — getSimpleSystemSection()
区域 3：Static Instructions   — getSimpleDoingTasksSection() + getActionsSection()
                                 + getUsingYourToolsSection() + getToneAndStyleSection()
├── 2 个版本：外部用户版 / Anthropic 内部版（USER_TYPE === 'ant'）
├── 使用 systemPromptSection() 缓存，跨 session 复用
└── 缓存 key 由工具集 + 模型决定
                                 
区域 4：Dynamic Sections      — 注册制 section 列表
├── session_guidance           — 按会话上下文变化
├── memory                     — 记忆加载（可能跨会话）
├── env_info                   — 环境信息
├── language / output_style    — 用户偏好
├── mcp_instructions           — DANGEROUS_uncached（MCP 连接变化）
├── scratchpad / frc / ...     — 功能 section
└── 使用 resolveSystemPromptSections() 解析
```

**缓存策略**（`SYSTEM_PROMPT_DYNAMIC_BOUNDARY`，src/constants/prompts.ts:114）：
- 静态部分使用 `cacheScope: 'global'`，跨组织共享 prompt cache
- 动态部分使用 `systemPromptSection()` 缓存，按 name 缓存计算值
- 变化频繁的部分用 `DANGEROUS_uncachedSystemPromptSection()` 标注

### EasyAgent 的差距

cc-haha 的系统提示构建（`getSystemPrompt()`，445行）远比 EasyAgent 复杂：

- **cc-haha 有 ~15 个独立 section**，每个独立计算和缓存
- **cc-haha 区分内部/外部提示**（`USER_TYPE === 'ant'`），内部版多了代码风格、验证 Agent、假阳性抑制等详细指令
- **cc-haha 的提示是活的**——根据当前启用的工具集、用户设置、MCP 连接动态组装
- EasyAgent 的系统提示是静态拼接，没有 section 注册、缓存、动态组装机制

### MCP 指令增量系统

cc-haha 的 MCP 指令采用了 **delta 机制**（`isMcpInstructionsDeltaEnabled()`），MCP server instructions 通过持久化的 `mcp_instructions_delta` attachments 注入，而不是每次重构系统提示——这避免了 MCP 连接变化破坏 prompt cache。

---

## 5. 上下文压缩深度分析

cc-haha 有 4 级压缩策略，按触发顺序：

| 级别 | 模块 | 作用域 | 策略 | 触发条件 |
|------|------|--------|------|----------|
| 1. Snip | `services/compact/snipCompact.ts` | 单次 | 裁剪历史中的低价值消息 | feature gate `HISTORY_SNIP` |
| 2. Microcompact | `services/compact/` | 单次 | 按 tool_use_id 替换已缓存的结果内容 | 每次 query iteration |
| 3. Context Collapse | `services/contextCollapse/` | 跨 turns | 投影折叠视图（read-time projection） | feature gate `CONTEXT_COLLAPSE` |
| 4. Autocompact | `services/compact/autoCompact.ts` | 跨 turns | LLM 摘要 + 保留最近消息 | 上下文接近限制时 |

**EasyAgent 的差距**：EasyAgent 只有第 4 级的简化版（LLM 摘要），缺少前 3 级轻量级压缩策略。

---

## 6. Message 类型系统

cc-haha 的消息体系（`src/types/message.ts`）比 EasyAgent 精细得多：

```typescript
// 简化的类型联合
type Message =
  | UserMessage           // 用户消息（含 tool_use_result）
  | AssistantMessage      // 助手消息（含 tool_use blocks）
  | SystemMessage         // 系统消息（compact_boundary、local_command 等子类型）
  | AttachmentMessage     // 附件消息
  | ProgressMessage       // 进度消息
  | TombstoneMessage      // 墓碑消息（标记消息已被删除）
  | ToolUseSummaryMessage // 工具使用摘要

// AssistantMessage 的详细结构
type AssistantMessage = {
  type: 'assistant'
  message: {
    id: string
    content: ContentBlock[]  // text | tool_use | thinking | redacted_thinking
    usage?: Usage
    stop_reason?: string | null
    stop_sequence?: string | null
  }
  apiError?: string           // 记录 API 错误类型（如 'max_output_tokens'）
  isSynthetic?: boolean       // 合成消息
  modelName?: string
  requestId?: string
  uuid: string
  timestamp: number
}
```

关键设计点：
- **`TombstoneMessage`**：当消息被删除时（如 fallback 发生后），用墓碑标记通知 UI/transcript 移除旧消息
- **`apiError`** 字段：记录 `max_output_tokens` 等 API 错误，用于恢复逻辑
- **`CompactBoundaryMessage`**：压缩边界标记，包含 `compactMetadata`（preservedSegment 等）用于会话恢复时重建指针

---

## 7. 针对 EasyAgent coding-agent 的改进建议

### P0 — 必须补齐

#### 7.1 工具级并行执行

**现状**：EasyAgent 的 `coding-agent` 中工具按顺序执行。

**改进**：在 Tool 接口中增加 `IsConcurrencySafe() bool` 方法，在 Agent 循环中检测连续的 read-only 工具并并行执行。参考 `src/services/tools/toolOrchestration.ts:91` 的 `partitionToolCalls()`。

**关键文件**：`internal/agent/types.go`（Tool 接口）、`internal/agent/agent-loop.go`（执行逻辑）

#### 7.2 系统提示分层与缓存

**现状**：EasyAgent 的系统提示是平面字符串拼接，每次重新构建。

**改进**：参考 cc-haha 的 `systemPromptSection` 注册机制，将系统提示拆分为静态（跨会话可缓存）和动态（按 section 注册）两部分。区分 prompt 层的缓存边界。

**关键文件**：`internal/agents/coding/`（系统提示构建）

#### 7.3 多级上下文压缩

**现状**：EasyAgent 只有 LLM 摘要压缩。

**改进**：增加轻量级压缩策略：
- 细粒度压缩：用简单规则替换/截断大 tool result
- 投影压缩：将对历史消息的折叠映射为只读投影，不实际删除
- 策略配置：根据上下文使用量渐进触发不同级别

**关键文件**：`internal/compaction/` 

### P1 — 值得补齐

#### 7.4 工具方法丰富化

**现状**：EasyAgent 的 `AgentTool` 接口定义了 `Execute` + 基础方法。

**建议增加**：
- `IsReadOnly() bool` — 标记只读工具
- `IsConcurrencySafe() bool` — 标记可并发工具
- `IsDestructive() bool` — 标记破坏性工具（安全告警）
- `MaxResultSize() int` — 结果截断阈值

#### 7.5 消息类型丰富化

**现状**：EasyAgent 的 `AgentMessage` 联合类型较简单。

**建议增加**：
- 错误元数据字段（API 错误类型、重试状态）
- 压缩边界标记（包含 preservedSegment 元数据）
- 进度消息类型（用于 long-running tool 的进度通知）

#### 7.6 错误恢复机制

**现状**：EasyAgent 对 API 错误没有恢复策略。

**建议增加**：
- `max_output_tokens` 恢复：检测到后自动重试带更大的 output token limit
- fallback model 降级：主模型失败后自动切换到备选模型

### P2 — 视需求补齐

#### 7.7 ToolSearch / 工具延迟加载

cc-haha 的 `ToolSearch` 机制将不常用的工具延迟发送，初次只发送核心工具，需要时通过 `tool_search` 工具发现。EasyAgent 目前所有工具均加载。

#### 7.8 Token Budget 管理

cc-haha 支持 `task_budget`（API 参数）和 `token_budget`（内部跟踪），EasyAgent 无此能力。

---

## 8. 关键源码速查

| 功能 | cc-haha 文件 | 行数 | 值得借鉴的关键点 |
|------|-------------|------|-----------------|
| Agent 循环入口 | `src/query.ts` | ~1200 | `queryLoop()` while(true) generator 模式 |
| 外层封装 | `src/QueryEngine.ts` | ~900 | `submitMessage()` generator，整合系统提示/memory/slash command |
| Tool 类型 | `src/Tool.ts` | ~800 | 15+ 方法的泛型接口 |
| 工具编排 | `src/services/tools/toolOrchestration.ts` | ~100 | `partitionToolCalls()` 并发/串行分批策略 |
| 工具执行 | `src/services/tools/toolExecution.ts` | ~500 | 完整执行链路：权限→钩子→执行→结果处理 |
| 系统提示 | `src/constants/prompts.ts` | ~700 | section 注册、缓存策略、动态边界标记 |
| Section 缓存 | `src/constants/systemPromptSections.ts` | ~70 | memoized section + 按需清除 |
| 上下文压缩 | `src/services/compact/` | 多文件 | 4 级渐进压缩 |
| 消息类型 | `src/types/message.ts` | — | 8 种消息子类型 + Tombstone |
| 用户输入处理 | `src/utils/processUserInput/processUserInput.ts` | ~200 | 处理 slash command / 文本 / 附件三种模式 |
| MCP 集成 | `src/utils/mcpStdioEnvironment.ts` | — | MCP Server 的 stdio 通信管理 |
| 会话持久化 | `src/utils/sessionStorage.ts` | — | JSONL 树状存储 + 快照 |

---

## 9. 总结：EasyAgent coding-agent 差距清单

```
┌────────────────────────────────────────────────────────────────────┐
│ 差距类型      │ 差距项               │ cc-haha 状态 │ EasyAgent 状态   │
├────────────────────────────────────────────────────────────────────┤
│ 架构级 P0    │ 工具并行执行          │ ✅ 分批次     │ ❌ 顺序执行   │
│             │ 系统提示缓存           │ ✅ section注册 │ ❌ 平面拼接  │
│             │ 多级上下文压缩         │ ✅ 4级渐进     │ ⚠️ 仅1级    │
├────────────────────────────────────────────────────────────────────┤
│ 方法级 P1   │ Tool 接口完整性       │ ✅ 15+方法    │ ⚠️ 基础方法  │
│             │ 消息类型丰富度         │ ✅ 8种子类型   │ ⚠️ 基本类型  │
│             │ 错误恢复机制           │ ✅ fallback/重试│ ❌ 无       │
│             │ 工具执行钩子           │ ✅ pre/post   │ ⚠️ 有但简单  │
├────────────────────────────────────────────────────────────────────┤
│ 特性级 P2   │ ToolSearch/延迟加载    │ ✅            │ ❌          │
│             │ Token Budget           │ ✅            │ ❌          │
│             │ MCP 支持              │ ✅ 完整       │ ⚠️ 规划中    │
│             │ Thinking 自适应配置    │ ✅            │ ❌          │
│             │ 会话 fork/subagent    │ ✅            │ ❌          │
└────────────────────────────────────────────────────────────────────┘
```

核心结论：**EasyAgent 的 Agent 框架底座（`internal/agent/`、`internal/ai/`）设计思路正确**，但 coding-agent 应用层（`internal/agents/coding/`）相比 cc-haha 在工具并行、系统提示缓存、上下文压缩、错误恢复等关键点上差距明显。好消息是这些差距大多数是增量改进，无需重构底座。
