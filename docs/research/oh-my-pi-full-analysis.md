> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# Oh-My-Pi (omp) 调研报告 — 全面分析

> 调研日期：2026-05-24
> 来源：https://github.com/can1357/oh-my-pi
> 调研目标：分析 omp 作为 pi-mono 最强 fork 的架构设计、功能特性、工程质量，评估对 EasyAgent 的借鉴价值

---

## 1. 概述

### 项目定位

| 项目 | 角色 | 技术栈 | 定位 |
|------|------|--------|------|
| **oh-my-pi (omp)** | 编码 Agent CLI | TypeScript + Rust (N-API) + Bun | "The most capable agent surface that ships" — 40+ provider, 32 内置工具, 13 LSP ops, 27 DAP ops, ~27k 行 Rust 核心 |
| **EasyAgent** | 通用 Agent 框架 | Go | 可扩展 Agent 底座 + 可插拔应用层，当前 coding-agent |

### 核心发现摘要

1. **omp 是 pi-mono 的最大最强 fork**，在原 pi-mono 基础上增加了海量功能：Rust native 绑定、32 工具、LSP/DAP 集成、子 Agent 系统、Hashline 编辑、TTSR 流规则、Hindsight 记忆等。
2. **Rust N-API 原生模块**是其最关键的架构决策：将 grep/find/glob/shell/PTY/text 处理等性能敏感操作下沉到 Rust，通过 napi-rs 暴露给 TypeScript，避免了 fork-exec 开销和平台差异。
3. **omp 的扩展系统（Extension/Hook）比 EasyAgent 更成熟**：支持事件订阅、生命周期钩子、自定义工具、斜杠命令注册、Marketplace 插件管理，而 EasyAgent 的扩展系统还是 MVP 阶段。
4. **创新功能密集度极高**：Hashline 编辑、TTSR 流规则、Subagent 任务系统、Hindsight 记忆、Internal URL 协议族、ACP 编辑器协议——这些都是 EasyAgent 完全没有的领域。
5. **工程质量优秀**：完善的 CI/CD、详细的文档（每个子系统都有独立文档）、规范的代码风格、约 27k 行 Rust + 大量 TypeScript 的代码量级。

---

## 2. 架构分析

### 整体架构

```
┌──────────────────────────────────────────────────────────────┐
│                    coding-agent (CLI)                        │
│  packages/coding-agent/                                      │
│  ├─ main.ts → CLI 入口 + 参数解析                              │
│  ├─ sdk.ts → createAgentSession() 高层组装                    │
│  ├─ tools/ → 32 内置工具                                      │
│  ├─ task/ → 子 Agent 系统                                      │
│  ├─ hashline/ → 基于内容哈希的编辑系统                           │
│  ├─ lsp/ → LSP 集成                                           │
│  ├─ dap/ → DAP 调试器集成                                      │
│  ├─ mcp/ → MCP 客户端                                         │
│  ├─ exa/ → 浏览器自动化 + 深度研究                                │
│  ├─ hindsight/ → 跨会话记忆系统                                  │
│  ├─ internal-urls/ → agent:// skill:// memory:// 等协议族       │
│  ├─ extensibility/ → Extension + Hook + Skill 系统              │
│  ├─ capability/ → 规则/提示/指令/工具能力发现                     │
│  └─ discovery/ → 多 Agent 配置发现 (Cursor/Cline/Codex 等)      │
├──────────────────────────────────────────────────────────────┤
│                    easyagent-core                              │
│  packages/agent/                                              │
│  ├─ agent-loop.ts → 双层 Agent 循环                            │
│  ├─ agent.ts → Agent 状态机                                   │
│  ├─ types.ts → AgentMessage / AgentTool / AgentEvent 类型      │
│  ├─ compaction.ts → 上下文压缩                                 │
│  └─ telemetry.ts → OpenTelemetry 集成                         │
├──────────────────────────────────────────────────────────────┤
│                    pi-ai (LLM 抽象层)                           │
│  packages/ai/                                                 │
│  ├─ stream.ts → 统一流式 API                                   │
│  ├─ api-registry.ts → Provider 注册制                          │
│  ├─ types.ts → Message / ToolCall / StreamEvent 类型           │
│  ├─ models.json → 模型注册表                                    │
│  └─ auth-storage.ts → OAuth 令牌管理                           │
├──────────────────────────────────────────────────────────────┤
│  pi-natives + pi-tui + pi-utils + pi-stats                    │
│  ┌─────────────────────────────────────────┐                   │
│  │  crates/pi-natives (Rust N-API)         │                   │
│  │  ├─ grep/fd/glob/search — 内嵌 ripgrep  │                   │
│  │  ├─ pty/shell — 进程 + PTY 管理          │                   │
│  │  ├─ ast — AST grep + edit               │                   │
│  │  ├─ text — 宽度/高亮/分词/排版           │                   │
│  │  └─ fs_cache — 文件系统扫描缓存           │                   │
│  └─────────────────────────────────────────┘                   │
└──────────────────────────────────────────────────────────────┘
```

### 核心抽象

| 接口/抽象 | 位置 | 作用 |
|-----------|------|------|
| `AgentTool<T,P,R>` | `packages/agent/src/types.ts` | 泛型工具定义：schema + execute + 生命周期 |
| `AgentLoopConfig` | `packages/agent/src/types.ts` | Agent 循环全配置：stream/steering/follow-up/tool hooks/telemetry |
| `AgentMessage` | `packages/agent/src/types.ts` | 联合类型 + declaration merging 扩展 |
| `CustomAgentMessages` | `packages/agent/src/types.ts` | 通过 declaration merging 扩展消息类型 |
| `HookAPI` | `packages/coding-agent/src/extensibility/hooks/types.ts` | 钩子编程 API：事件订阅 + 斜杠命令 + 消息渲染 |
| `Extension` | `packages/coding-agent/src/extensibility/extensions/types.ts` | 扩展接口：工具/钩子/命令/上下文变换 |
| `CapabilityProvider` | `packages/coding-agent/src/capability/types.ts` | 能力发现抽象（规则/提示/工具等） |
| `InternalUrlRouter` | `packages/coding-agent/src/internal-urls/router.ts` | 内部 URL 协议路由器 |
| `TtsrManager` | `packages/coding-agent/src/session/ | 流规则匹配引擎 |

### 数据流

```
User Input → CLI (main.ts)
  → createAgentSession() → 组装 Session + Extension + Capability + TTSR
    → InteractiveMode (TUI 循环)
      → AgentSession.run() 
        → Agent Loop (agent-loop.ts):
          1. systemPrompt + messages → LLM (pi-ai stream)
          2. stream → text/thinking/tool_call events
          3. TTSR monitor 扫描流中违规
          4. tool_call → beforeToolCall hooks → execute → afterToolCall hooks
          5. tool_result → 回到步骤 1
          6. 无 tool_call → getSteeringMessages → getFollowUpMessages → yield
```

---

## 3. 功能分析

### 32 内置工具清单

| 工具 | 模块 | 说明 | 创新度 |
|------|------|------|--------|
| `read` | `src/tools/read.ts` | 文件读取，支持摘要 | 标准 |
| `write` | `src/tools/write.ts` | 文件写入 | 标准 |
| `edit` | `packages/coding-agent/src/edit/` | 编辑系统，支持普通 diff + Hashline | **高** |
| `bash` | `src/tools/bash.ts` | 命令执行，支持 PTY/交互式 | **高**(PTY) |
| `search` | `src/tools/search.ts` | 语义搜索 + 关键词搜索 | **高** |
| `grep` | 实际是 search 的一部分 | 通过 Rust grep 实现 | 中等 |
| `find` | `src/tools/find.ts` | 文件查找 | 标准 |
| `ast-grep` | `src/tools/ast-grep.ts` | AST 模式匹配搜索 | **高** |
| `ast-edit` | `src/tools/ast-edit.ts` | AST 感知的代码编辑 | **高** |
| `lsp` | `packages/coding-agent/src/lsp/` | LSP 集成（13 个操作） | **极高** |
| `debug` | `src/tools/debug.ts` | DAP 调试器控制（27 个操作） | **极高** |
| `task` | `packages/coding-agent/src/task/` | 子 Agent 任务系统 | **极高** |
| `irc` | `src/tools/irc.ts` | 子 Agent 间通信 | **高** |
| `web_search` | `packages/coding-agent/src/web/search.ts` | 多引擎 Web 搜索 | 中等 |
| `browser` | `src/tools/browser.ts` | 无头浏览器控制 | **高** |
| `fetch` | `src/tools/fetch.ts` | URL 读取 | 标准 |
| `gh` | `src/tools/gh.ts` | GitHub API 集成 | **高**(作为 FS) |
| `read`(增强) | `src/tools/read.ts` | 支持 PDF/arxiv/URL | **高** |
| `resolve` | `src/tools/resolve.ts` | 路径/引用解析 | **高** |
| `calculator` | `src/tools/calculator.ts` | 精确计算器 | 低 |
| `ask` | `src/tools/ask.ts` | 询问用户 | 标准 |
| `checkpoint` | `src/tools/checkpoint.ts` | 检查点/回滚 | **高** |
| `yield` | `src/tools/yield.ts` | 等待用户输入后继续 | 标准 |
| `todo-write` | `src/tools/todo-write.ts` | 待办事项跟踪 | 中等 |
| `retain` | `src/tools/hindsight-retain.ts` | 记忆写入 | **高** |
| `recall` | `src/tools/hindsight-recall.ts` | 记忆读取 | **高** |
| `reflect` | `src/tools/hindsight-reflect.ts` | 记忆反思 | **高** |
| `job` | `src/tools/job.ts` | 后台作业管理 | 中等 |
| `inspect-image` | `src/tools/inspect-image.ts` | 图片分析 | 中等 |
| `render-mermaid` | `src/tools/render-mermaid.ts` | Mermaid 图表渲染 | 中等 |
| `ssh` | `src/tools/ssh.ts` | SSH 远程执行 | 中等 |
| `image-gen` | `src/tools/image-gen.ts` | AI 图片生成 | 中等 |
| `review` | `src/tools/review.ts` | 代码审查 | **高** |
| `recipe` | `src/tools/recipe.ts` | 配方/流程 | 中等 |
| `eval` | `src/tools/eval.ts` | 代码执行（Python/JS 沙箱） | **高** |

### 亮点特性

#### 1. Hashline 编辑系统 (`packages/coding-agent/src/hashline/`)

这是 omp 最具创新性的功能之一。传统 `str_replace_editor` 要求模型输出完整的替换文本行，而 Hashline 让模型**通过内容哈希引用锚点**进行编辑：

```typescript
// 模型不再输出完整行，而是输出锚点哈希 + 操作
// 例如插入操作：
{ kind: "insert", cursor: { kind: "before_anchor", anchor: { line: 42, hash: "abc123" } }, text: "new line" }
// 删除操作：
{ kind: "delete", anchor: { line: 42, hash: "abc123" } }
```

**核心优势**：
- 模型输出 token 量减少 61%（对 Grok 4 Fast）
- 锚点不匹配时拒绝补丁，防止损坏文件
- 避免 whitespace 战争和 "string not found" 循环

**原理**：`hashline/hash.ts` 计算每行内容的哈希 → `hashline/parser.ts` 解析模型输出 → `hashline/apply.ts` 应用到文件。

#### 2. TTSR — Time Traveling Stream Rules (`docs/ttsr-injection-lifecycle.md`)

在流式输出过程中实时检测违规，中断并注入纠正指令：

```
流式输出 → TTSR monitor 扫描 delta
  → 匹配规则（如 "不要用 Box::leak"）
  → agent.abort() 立即中断流
  → 注入 <system-interrupt> 纠正指令
  → agent.continue() 从检查点重试
```

**核心文件**：
- `packages/coding-agent/src/session/agent-session.ts` — 流监控
- `packages/coding-agent/src/capability/rule.ts` — 规则发现
- 多种规则来源格式：Cursor MDC、Cline .clinerules、Codex AGENTS.md

**价值**：规则不占用系统提示上下文，只在违规时才介入。注入内容通过 compaction 持久化。

#### 3. Subagent 任务系统 (`packages/coding-agent/src/task/`)

将任务分割给多个子 Agent 并行执行：

```
task → 拆分为子任务
  ├─ explorer → read/find 只读工具
  ├─ planner → 仅生成计划
  ├─ reviewer → 代码审查
  └─ (自定义) → 通过 AGENTS.md 定义的任意 Agent
       ↓
 每个子 Agent 在独立 worktree 中运行
 结果通过 schema-validated object 返回
```

**关键文件**：
- `src/task/agents.ts` — 内嵌 Agent 定义（explore/plan/designer/reviewer）
- `src/task/discovery.ts` — 从多个来源发现 Agent 定义
- `src/task/executor.ts` — 子 Agent 执行
- `src/task/worktree.ts` — Git worktree 隔离

#### 4. Internal URL 协议族 (`packages/coding-agent/src/internal-urls/`)

将资源引用统一为 URL 协议：

| 协议 | 处理 | 示例 |
|------|------|------|
| `agent://` | Agent 引用 | `agent://explorer` |
| `artifact://` | Artifact 引用 | `artifact://session/abc/file.txt` |
| `memory://` | 记忆查询 | `memory://recall/project-architecture` |
| `skill://` | 技能加载 | `skill://code-review` |
| `rule://` | 规则引用 | `rule://no-box-leak` |
| `mcp://` | MCP 工具 | `mcp://filesystem/list` |
| `local://` | 本地文件系统 | `local:///path/to/file` |
| `omp://` | omp 内部 | `omp://install-id` |

#### 5. Rust Native 模块 (`crates/pi-natives/`)

性能敏感操作全部用 Rust 实现，通过 napi-rs 暴露给 TypeScript：

| 模块 | 替代方案 | 性能收益 |
|------|---------|---------|
| `grep` | ripgrep (内嵌) | 无需 fork 进程 |
| `glob` | glob 匹配 | 内嵌，跨平台 |
| `fd` | find 命令 | 无需 fork 进程 |
| `pty` | PTY 管理 | 跨平台一致 |
| `shell` | bash 会话 | 会话持久化 |
| `ast` | AST grep + edit | 语法级操作 |
| `text` | Unicode 宽度/高亮 | 高性能渲染 |
| `fs_cache` | 文件系统扫描 | 增量缓存 |

#### 6. 规则发现兼容性 (`packages/coding-agent/src/discovery/`)

**最令人印象深刻**：omp 能读取其他所有 Agent 已有的配置文件格式：

| 格式 | Provider | 来源 |
|------|----------|------|
| `.omp/rules/*.md` | builtin | 自身格式 |
| `.claude/rules/*.mdc` | claude | Claude Code 格式 |
| `.cursor/rules/*.mdc` | cursor | Cursor 格式 |
| `.clinerules` | cline | Cline 格式 |
| `.windsurf/rules/*.md` | windsurf | Windsurf 格式 |
| `AGENTS.md` | agents | Codex 格式 |
| `.github/copilot-instructions.md` | github | GitHub Copilot |

#### 7. Hindsight 跨会话记忆 (`packages/coding-agent/src/hindsight/`)

Agent 在运行时写入记忆（`retain`），跨会话读取（`recall`）：

- `retain` 工具：写入事实到记忆库
- `recall` 工具：查询相关记忆
- `reflect` 工具：会话结束后反思总结
- Mental models: 将每次会话压缩为心智模型，下次会话自动加载

---

## 4. 与 EasyAgent 对比

### 架构理念对比

| 维度 | oh-my-pi | EasyAgent | 评价 |
|------|----------|-------|------|
| 架构分层 | 扁平化，packages 按功能分 | 严格四层：Core → Platform → Application → Entrypoints | EasyAgent 更清晰，omp 更灵活 |
| 语言 | TypeScript + Rust (核心性能) | Go | 不同语言的架构权衡 |
| 扩展系统 | Extension + Hook 双系统 | Extension 接口（MVP） | omp 更成熟 |
| 工具数量 | 32 内置工具 | 7 内置工具 | omp 远超 |
| Provider 抽象 | 统一 pi-ai 接口 | Provider 接口 + 注册制 | 设计相似 |
| Agent 循环 | 双层（follow-up + tool call） | 双层 | 设计一致 |
| 消息类型扩展 | Declaration merging | 暂无 | omp 更灵活 |
| 上下文压缩 | 有 | 有 | 功能相似 |
| 会话存储 | 树状 JSONL | 树状 JSONL | 设计一致 |
| 性能敏感操作 | Rust N-API | Go 原生 | 各有所长 |

### 功能覆盖对比

| 功能 | oh-my-pi | EasyAgent | 差距评估 |
|------|----------|-------|----------|
| 基础 7 工具 | ✅ 全部 + 扩展 | ✅ read/write/edit/bash/grep/find/ls | ✅ 基础对齐 |
| 语义搜索 | ✅ `search` 工具 | ❌ 无 | **大差距** |
| LSP 集成 | ✅ 13 个操作 | ❌ 无 | **大差距** |
| DAP 调试器 | ✅ 27 个操作 | ❌ 无 | **大差距** |
| AST 操作 | ✅ ast-grep + ast-edit | ❌ 无 | **大差距** |
| 子 Agent | ✅ task + irc | ❌ 无 | **大差距** |
| Hashline 编辑 | ✅ | ❌ 标准 diff | **大差距** |
| TTSR 流规则 | ✅ | ❌ 无 | **大差距** |
| 代码执行沙箱 | ✅ Python + Bun eval | ❌ 无 | **大差距** |
| Web 搜索/浏览器 | ✅ web_search + browser | ❌ 无 | **大差距** |
| GitHub 集成 | ✅ `gh` 工具 | ❌ 无 | **大差距** |
| 跨会话记忆 | ✅ Hindsight | ❌ 无 | **大差距** |
| MCP 客户端 | ✅ | ❌ 无 | **大差距** |
| 多 Agent 配置发现 | ✅ 8 种格式 | ❌ 仅自身格式 | **大差距** |
| Extension 系统 | ✅ 成熟（事件/钩子/工具/命令） | ✅ MVP（工具 + 命令 + 钩子） | 中等差距 |
| SSH 远程 | ✅ ssh 工具 | ✅ Operations 抽象 | ✅ 对齐 |
| Internal URL 协议 | ✅ 8 种协议 | ❌ 无 | **大差距** |
| Marketplace 插件 | ✅ | ❌ 无 | **大差距** |
| 路由/协议统一 | ✅ InternalUrlRouter | ❌ 无 | **大差距** |
| 会话树导航 | ✅ `/tree` 交互式导航 | ❌ 无 | **大差距** |
| 代码审查 | ✅ review 子 Agent | ❌ 无 | **大差距** |
| 检查点/回滚 | ✅ checkpoint + rewind | ❌ 无 | **大差距** |
| 图片分析/生成 | ✅ inspect-image + image-gen | ❌ 无 | **大差距** |
| 后台作业 | ✅ job 工具 | ❌ 无 | **大差距** |
| ACP 编辑器协议 | ✅ | ❌ 无 | **大差距** |
| OTel 遥测 | ✅ | ❌ 无 | **大差距** |
| OAuth 认证 | ✅ | ❌ 无 | **大差距** |
| 待办事项 | ✅ todo-write | ❌ 无 | **大差距** |

### EasyAgent 的优势

1. **架构更清晰**：严格的四层架构（Core → Platform → Application → Entrypoints），关注点分离更好。omp 的 packages 之间依赖关系更松散，但也更容易产生循环依赖。

2. **单二进制分发**：Go 编译为单个二进制，部署简单。omp 需要 Bun runtime + Rust native 模块 + npm 包管理，分发复杂度高。

3. **并发模型更简洁**：goroutine + channel 比 JS async/await 更直观，且没有事件循环阻塞问题。

4. **层间解耦更规范**：`runtime.Application` 接口实现了 Platform 与 Application 的严格分离。omp 的 sdk.ts 是过程式组装，没有接口约束。

5. **依赖极简**：EasyAgent 以标准库为主，外部依赖少。omp 有大量 npm 依赖和 Rust crate 依赖。

6. **跨语言桥接成本**：omp 需要维护 napi-rs 绑定层 + TypeScript 声明生成，构建链复杂（tsc → bun build → napi build）。EasyAgent 只需 `go build`。

7. **Operations 抽象**：EasyAgent 的 `operations.Operations` 接口统一了本地/SSH 文件操作，比 omp 的工具直接操作文件系统更灵活。

---

## 5. 迁移建议

### 优先级排序

| 优先级 | 特性/设计 | 迁移难度 | 预期收益 | 实现路径 |
|--------|----------|----------|----------|---------|
| **P0** | **语义搜索 (`search` 工具)** | 低 | **极高** | 在现有 `grep` + `find` 基础上，增加 BM25/向量搜索。实际 omp 的 search 工具就是 `src/tools/search.ts` + `src/tools/search-tool-bm25.ts`。EasyAgent 可以先用简单的 TF-IDF 实现，后续接入向量数据库 |
| **P0** | **子 Agent 系统 (`task`)** | 中 | **极高** | 这是核心基础设施。EasyAgent 的 AgentSession 已经支持多会话，可以创建子 AgentSession 在独立 worktree 中运行。需要实现：Agent 定义发现、worktree 隔离、结构化输出合并 |
| **P1** | **Hashline 编辑** | 中 | **高** | 核心算法不复杂：计算每行哈希 → 模型引用哈希 → 应用到文件。关键文件 `hashline/hash.ts` + `hashline/apply.ts` 逻辑可移植。重点在于 prompt 设计和模型适配 |
| **P1** | **Internal URL 协议** | 中 | **高** | EasyAgent 可以定义类似 `pi://` 协议族，用 Router 模式统一资源访问。对于已实现的 skill、session、artifact 等功能，提供统一 URL 接口 |
| **P1** | **扩展系统完善** | 中 | **高** | EasyAgent 已有 Extension 接口 MVP，但缺少事件钩子（`beforeToolCall`/`afterToolCall`/`context` 等）、Marketplace 插件管理、能力发现机制。逐步补齐 |
| **P1** | **MCP 客户端** | 中 | **高** | MCP 是 Agent 工具标准化协议。EasyAgent 需要实现 MCP JSON-RPC 传输层 + 工具桥接。可以参考 omp 的 `packages/coding-agent/src/mcp/` |
| **P2** | **配置发现兼容性** | 低 | **中** | 读取 `.claude/`、`.cursor/`、`.clinerules` 等已有配置。每个格式一个 provider，注册到能力发现系统 |
| **P2** | **TTSR 流规则** | 中 | **中** | 需要在 Agent 循环的流式输出中插入监控点。EasyAgent 的 `Stream` 事件流已经支持事件，可以添加流监控钩子 |
| **P2** | **LSP 集成** | 高 | **高** | 需要 LSP 协议客户端实现（jsonrpc over stdio）、language server 管理、13 个操作的 tool 封装。工程量较大 |
| **P2** | **代码执行沙箱** | 中 | **中** | Python REPL + Bun worker 的双执行引擎。可以通过 `bash` 工具 + 持久化会话实现基础版本 |
| **P3** | **DAP 调试器** | 高 | **低-中** | 实现成本高，使用场景相对窄。可以作为长期储备 |
| **P3** | **Hindsight 记忆** | 中 | **中** | 需要向量存储后端 + 记忆管理逻辑。可以先用简单的 JSONL 文件作为记忆存储 |
| **P3** | **Web 搜索/浏览器** | 低 | **中** | 通过 `bash` 调用 curl/fetch 可实现基础 Web 搜索 |
| **P3** | **ACP 编辑器协议** | 高 | **低** | 仅有 Zed 集成场景，优先级最低 |

### 实施路线图

#### 第一阶段：基础设施补齐（1-2 个月）

**目标**：让 EasyAgent 具备与 omp 竞争的基础能力集

1. **语义搜索**（2 周）
   - 基于现有 grep 工具，增加 BM25 相关性排序
   - 实现 `search` 工具 schema：支持关键词、路径过滤、上下文行数
   - 整合到系统提示中

2. **子 Agent 系统**（3 周）
   - 定义 AgentDefinition 数据结构（参考 omp `src/task/types.ts`）
   - 实现子 Agent session 创建 + 独立 worktree 隔离
   - 实现结构化输出合并（参考 omp `src/task/output-manager.ts`）
   - 内嵌 bundled agents：explore/plan

3. **Internal URL 协议**（1 周）
   - 定义 `pi://` 协议族（skill://, session://, agent://）
   - 实现 Router + Handler 模式
   - 整合到 read 工具中

4. **扩展系统增强**（2 周）
   - 添加事件钩子：beforeToolCall/afterToolCall/context/session events
   - 实现能力发现机制（CapabilityProvider）
   - 添加 Marketplace 配置定义

#### 第二阶段：差异化竞争力（2-3 个月）

**目标**：在关键功能上追赶 omp

1. **Hashline 编辑**（3 周）
   - 实现行哈希计算 + 锚点系统
   - 修改 edit tool 支持 Hashline 格式
   - 适配系统提示

2. **MCP 客户端**（2 周）
   - JSON-RPC over stdio/SSE 传输层
   - MCP 工具桥接
   - MCP 服务发现 + 配置管理

3. **配置发现兼容性**（1 周）
   - 实现 builtin/cursor/cline/windsurf 格式读取
   - 集成到能力发现系统

4. **TTSR 流规则**（2 周）
   - 在 Agent 循环中添加流监控点
   - 实现规则匹配引擎
   - 中断 + 注入 + 重试流程

5. **代码执行沙箱**（3 周）
   - Python REPL 持久会话
   - Bun Worker 持久会话
   - 双向工具桥接（沙箱内可调用 agent tools）

#### 第三阶段：高级功能（3-6 个月）

**目标**：形成独特优势

1. **LSP 集成**（1-2 个月）
2. **Hindsight 记忆**（3 周）
3. **Web 搜索**（1 周）
4. **DAP 调试器**（如有需求）
5. **ACP 编辑器协议**（如有需求）

---

## 6. 详细参考

### 关键文件索引

| 文件路径 | 职责 | 值得关注的点 |
|----------|------|-------------|
| `packages/coding-agent/src/main.ts` | CLI 入口 + 组装 | 如何从参数组装 AgentSession（参考价值高） |
| `packages/coding-agent/src/sdk.ts` | AgentSession 工厂 | 完整的组装流程：Extension/Capability/TTSR/MCP/Session |
| `packages/coding-agent/src/task/discovery.ts` | 子 Agent 发现 | 多来源合并 + 优先级 + 去重策略 |
| `packages/coding-agent/src/task/executor.ts` | 子 Agent 执行 | worktree 隔离 + 结构输出合并 |
| `packages/coding-agent/src/hashline/` | Hashline 编辑 | 内容哈希锚点 + 编辑应用算法 |
| `packages/coding-agent/src/lsp/` | LSP 集成 | LSP 客户端 + 13 操作封装 |
| `packages/coding-agent/src/dap/` | DAP 调试器 | 调试器适配层 + 27 操作 |
| `packages/coding-agent/src/internal-urls/router.ts` | URL 协议路由 | Router + Handler 模式 |
| `packages/coding-agent/src/capability/` | 能力发现 | 规则/提示/工具的统一发现模式 |
| `packages/coding-agent/src/discovery/` | 配置发现 | 8 种 Agent 配置格式读取 |
| `packages/coding-agent/src/extensibility/` | 扩展系统 | Extension + Hook + Skill |
| `packages/coding-agent/src/tools/index.ts` | 工具注册 | 32 工具的批量注册模式 |
| `packages/coding-agent/src/mcp/` | MCP 客户端 | MCP JSON-RPC + 工具桥接 |
| `packages/coding-agent/src/hindsight/` | 跨会话记忆 | retain/recall/reflect 三工具 |
| `packages/coding-agent/src/eval/` | 代码执行 | Python + Bun 沙箱 |
| `packages/coding-agent/src/exa/` | 浏览器自动化 | puppeteer + 深度研究 |
| `crates/pi-natives/src/` | Rust 原生模块 | napi-rs 绑定、grep/fd/glob/pty/shell |
| `docs/ttsr-injection-lifecycle.md` | TTSR 文档 | 流规则中断注入完整流程 |
| `docs/rulebook-matching-pipeline.md` | 规则匹配 | 规则发现 → 规范化 → 去重 → 路由 |
| `docs/hooks.md` | 钩子系统 | 事件类型 + 执行模型 |
| `docs/task-agent-discovery.md` | 子 Agent 发现 | 发现来源 + 合并策略 |

### 参考资料

- omp 官方站点：https://omp.sh
- GitHub 仓库：https://github.com/can1357/oh-my-pi
- The Harness Problem（博客文章）：https://blog.can.ac/2026/02/12/the-harness-problem/
- pi-mono（上游项目）：https://github.com/badlogic/pi-mono
