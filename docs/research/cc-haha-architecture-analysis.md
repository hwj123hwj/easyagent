> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# cc-haha 调研报告 — 架构与功能全景分析

> 调研日期：2026-05-24
> 来源：本地路径 `/Users/weijian/Desktop/develop/test/pi/cc-haha`，GitHub: [NanmiCoder/cc-haha](https://github.com/NanmiCoder/cc-haha)
> 调研目标：cc-haha 是基于 Anthropic npm registry 泄露的 Claude Code 源码修复而来的桌面端工作台，分析其架构设计、功能特性，对比 EasyAgent 评估迁移价值

---

## 1. 概述

### 项目定位

| 项目 | 角色 | 技术栈 | 定位 |
|------|------|--------|------|
| cc-haha | Claude Code 桌面工作台（修复版） | TypeScript/Bun + Tauri(Rust) + React/Zustand | 把 Claude Code CLI 包装为桌面 APP：多会话、多项目、分支/Worktree、代码 Diff、权限审批、IM 接入、Computer Use、定时任务 |
| EasyAgent | 我们的 Agent 框架 | Go | 通用 Agent 底座 + coding-agent 应用层，面向终端和 server 场景 |

### 核心发现摘要

1. **cc-haha 不是独立框架，而是 Claude Code 源码的修复版**——它继承的是 Anthropic 官方 Claude Code 的全部架构：Ink React TUI、Anthropic SDK、自定义 Agent 循环。与此对比，EasyAgent 是独立从零实现的 Agent 框架。
2. **桌面端是核心差异点**：cc-haha 用 Tauri (Rust + WebView) 将 CLI 包装为 macOS/Windows 桌面 APP，提供多会话工作台、可视化 Diff、权限审批面板等。EasyAgent 也有桌面端（Electron + React + Vite），但功能丰富度不如 cc-haha。
3. **IM 接入体系**：支持 Telegram/飞书/微信/钉钉四种 IM 渠道远程操控 Agent 会话，通过 `adapters/` 独立进程 + 本地 HTTP/WS API 对接。EasyAgent 无此能力。
4. **Provider 代理层**：内置协议转换代理（Anthropic Messages ↔ OpenAI Chat/Responses），支持非 Anthropic 模型（DeepSeek/Ollama/OpenAI）作为后端。EasyAgent 的 Provider 抽象层设计更干净但缺少协议转换。
5. **多 Agent 系统和 Agent SDK 集成**：支持 Agent Teams（多 Agent 编排）、Fork Subagent（子 Agent 分支）、Coordinator Mode（协调器模式）。EasyAgent 规划中有类似能力但尚未实现。

---

## 2. 架构分析

### 整体架构

cc-haha 的整体架构可以分为四个层次：

```
┌─────────────────────────────────────────────────────────┐
│  Desktop App (Tauri + React + Zustand)                   │
│  destkop/src/ — 会话/设置/IM/定时任务/Computer Use UI     │
│  desktop/src-tauri/ — Rust native 层: 窗口/终端/更新     │
├─────────────────────────────────────────────────────────┤
│  Server Layer (Bun HTTP + WebSocket)                     │
│  src/server/ — REST API + WebSocket + Proxy + Auth      │
│  — 桌面端与 CLI 共享同一套文件系统存储                     │
├─────────────────────────────────────────────────────────┤
│  Agent Core + CLI (Ink React TUI)                        │
│  src/ — QueryEngine / Tool 系统 / 命令/ 状态管理          │
│  src/ink/ — 自定义 React Reconciler 终端渲染              │
├─────────────────────────────────────────────────────────┤
│  IM Adapters (独立进程)                                   │
│  adapters/telegram, feishu, wechat, dingtalk             │
│  — 通过本地 Server API 与 Agent 会话交互                  │
└─────────────────────────────────────────────────────────┘
```

**关键特征**：
- CLI 与 Desktop 共享同一套存储（JSONL 会话、JSON 配置），数据互通
- Server Layer 是 Desktop 与 CLI 之间的桥梁：Desktop 通过 REST/WS 与 Server 通信，Server 通过子进程启动 CLI 会话
- IM Adapters 作为独立进程运行，通过 Server 的 API 操作会话

### 核心抽象

#### Tool 系统（`src/Tool.ts`）

Tool 类型是一个泛型接口，包含完整生命周期：

```typescript
// src/Tool.ts:362 — Tool 类型定义
export type Tool<
  Input extends AnyObject = AnyObject,
  Output = unknown,
  P extends ToolProgressData = ToolProgressData,
> = {
  name: string
  aliases?: string[]
  searchHint?: string
  inputSchema: Input
  inputJSONSchema?: ToolInputJSONSchema
  outputSchema?: z.ZodType<unknown>
  call(args, context, canUseTool, parentMessage, onProgress): Promise<ToolResult<Output>>
  description(input, options): Promise<string>
  isConcurrencySafe(input): boolean
  isEnabled(): boolean
  isReadOnly(input): boolean
  isDestructive?(input): boolean
  interruptBehavior?(): 'cancel' | 'block'
  isSearchOrReadCommand?(input): { isSearch, isRead, isList? }
  isOpenWorld?(input): boolean
  requiresUserInteraction?(): boolean
  isMcp?: boolean
  shouldDefer?: boolean  // 用于 ToolSearch 延迟加载
  alwaysLoad?: boolean
  mcpInfo?: { serverName: string; toolName: string }
  maxResultChars?: number
}
```

与 EasyAgent 的对比：
- 两者都使用泛型设计 + schema 校验（cc-haha 用 Zod v4 vs EasyAgent 用 TypeBox）
- cc-haha 多了 `isConcurrencySafe`/`isReadOnly`/`isDestructive`/`shouldDefer` 等细粒度控制
- cc-haha 有 `onProgress` 回调用于流式进度通知

#### QueryEngine（`src/QueryEngine.ts:186`）

这是 Agent 循环的核心封装，接收配置（tools/commands/mcpClients/canUseTool 等）、管理消息、执行 `submitMessage()` 生成器方法。相当于 EasyAgent 中 `agent-loop.ts` 的升级版——它整合了系统提示构建、用户输入处理、slash command 处理、file history、memory 加载等完整流程。

cc-haha 的重要特点是**没有独立分层的 Agent 循环模块**——Agent 循环逻辑散布在 `QueryEngine.submitMessage()`、`query.ts`、`utils/processUserInput/` 等多个文件中，与 Claude Code 的业务逻辑高度耦合。

#### Server API 体系（`src/server/`）

cc-haha 的 Server 层功能极其丰富：

| API 模块 | 文件 | 功能 |
|----------|------|------|
| 会话管理 | `api/sessions.ts` | 创建/列表/删除/恢复会话 |
| 文件系统 | `api/filesystem.ts` | 文件读写、目录浏览 |
| Provider 管理 | `api/providers.ts` | 多 Provider 配置、激活 |
| 模型代理 | `proxy/handler.ts` | Anthropic↔OpenAI 协议转换 |
| 插件管理 | `api/plugins.ts` | 插件的 CRUD |
| Skills | `api/skills.ts` | Skills 管理 |
| MCP | `api/mcp.ts` | MCP 服务器配置 |
| 记忆 | `api/memory.ts` | 跨会话记忆管理 |
| 定时任务 | `api/scheduled-tasks.ts` | Cron 任务管理 |
| Computer Use | `api/computer-use.ts` | Computer Use API |
| H5 远程访问 | `api/h5-access.ts` | 手机端远程访问 |
| IM 管理 | `api/adapters.ts` | IM 适配器配置 |
| Agent | `api/agents.ts` | Agent 定义管理 |
| OpenAI OAuth | `api/haha-oauth.ts` | 第三方 OAuth 登录 |

### 数据流

```
用户输入 → QueryEngine.submitMessage()
  → processUserInput() [处理 slash commands]
  → fetchSystemPromptParts() [构建系统提示]
  → Anthropic SDK stream() [LLM 调用]
  → 流式响应 (text/thinking/tool_use)
  → executeToolCalls() [工具执行]
  → 结果回传 → 继续循环直到完成
```

---

## 3. 功能分析

### 功能清单

| 功能类别 | 具体能力 | 创新度 |
|----------|----------|--------|
| **桌面端** | 多会话工作台、分支/Worktree、右侧代码改动、Diff 视图、权限审批面板、多 Provider 配置、Computer Use、H5 远程访问、定时任务、Token 用量统计 | ⭐⭐⭐ 原创程度高 |
| **IM 接入** | Telegram/飞书/微信/钉钉远程操控 Agent | ⭐⭐⭐ 对开源项目较稀缺 |
| **Provider 代理** | Anthropic ↔ OpenAI/DeepSeek/Ollama 协议转换 | ⭐⭐⭐ 实用性强 |
| **多 Agent 系统** | Agent Teams、Fork Subagent、Coordinator Mode | ⭐⭐ 借鉴 Agent SDK |
| **记忆系统** | 跨会话持久化记忆、自动记忆、AutoDream | ⭐⭐ |
| **Skills 系统** | agentkills.io 格式加载、条件激活、MCP Skill | ⭐⭐ |
| **插件系统** | Tool/Command/Hook/Keybinding 扩展 | ⭐⭐ |
| **定时任务** | Cron 调度、桌面通知 | ⭐⭐⭐ |
| **Computer Use** | Agent 控制桌面（截图/点击/输入） | ⭐⭐⭐ |
| **质量门禁** | 覆盖度门禁、Provider Smoke、PR 质量合约 | ⭐⭐⭐ |

### 亮点特性

#### 1. Provider 协议转换代理（`src/server/proxy/`）

cc-haha 内置了一个完整的协议转换代理，接收 Anthropic Messages API 请求，转换为 OpenAI Chat Completions 或 Responses API 格式，转发给第三方 Provider，再将结果转回 Anthropic 格式。这意味着非 Anthropic 模型（DeepSeek、Ollama、OpenAI）可以无缝替换 Claude。

架构涉及：
```
src/server/proxy/
├── handler.ts                     # 入口：路由、Provider 查找、格式判断
├── claudeCodeAttribution.ts       # 归属标记
├── transform/
│   ├── anthropicToOpenaiChat.ts   # Anhtropic → OpenAI Chat
│   ├── anthropicToOpenaiResponses.ts  # Anhtropic → OpenAI Responses
│   ├── openaiChatToAnthropic.ts   # OpenAI Chat → Anthropic
│   ├── openaiResponsesToAnthropic.ts  # OpenAI Responses → Anthropic
│   └── types.ts
└── streaming/
    ├── openaiChatStreamToAnthropic.ts
    └── openaiResponsesStreamToAnthropic.ts
```

**对 EasyAgent 的启示**：EasyAgent 的 Provider 抽象（`internal/ai/providers/`）已经设计了统一接口，但缺少协议转换层。如果 EasyAgent 需要支持更多模型（如 DeepSeek），可以借鉴此代理模式。

#### 2. IM 接入体系（`adapters/`）

四个 IM 平台适配器共享一套 common 工具库：

```
adapters/
├── common/
│   ├── chat-queue.ts        # 消息队列
│   ├── config.ts            # 配置管理
│   ├── message-buffer.ts    # 消息缓冲
│   ├── message-dedup.ts     # 去重
│   ├── session-store.ts     # 会话存储
│   ├── ws-bridge.ts         # WebSocket 桥接
│   └── ...
├── telegram/                # Telegram Bot API
├── feishu/                  # 飞书卡片消息
├── wechat/                  # 微信协议
└── dingtalk/                # 钉钉卡片
```

数据流：`IM Message → Adapter → Server REST API → Agent Session`

**对 EasyAgent 的启示**：EasyAgent 目前完全是终端应用，无 IM 接入。如果规划中需要，可借鉴 adapter 模式，但需要先建立 Server API 层。

#### 3. 多 Agent 系统

cc-haha 实现了三种多 Agent 模式：

- **Agent Teams**：`coordinate` 模式下，主 Agent 可以创建 Team，Team 内的子 Agent 通过 `send_message` 工具互相通信
- **Fork Subagent**：当前会话 fork 出一个子 Agent 执行独立任务
- **Coordinator Mode**：环境变量 `CLAUDE_CODE_COORDINATOR_MODE` 控制，进入协调器模式

Agent 定义存储在 `~/.claude/agents/` 目录下，支持内置和自定义两种。

---

## 4. 与 EasyAgent 对比

### 架构理念对比

| 维度 | cc-haha (Claude Code) | EasyAgent | 评价 |
|------|----------------------|-------|------|
| 框架独立性 | 高度耦合 Claude Code 业务 | 通用 Agent 底座 + 可插拔应用层 | EasyAgent 更胜一筹 |
| 语言 | TypeScript + Bun | Go | 各有利弊 |
| UI | Ink React TUI + Tauri Desktop | 终端文本输出 | cc-haha 丰富得多 |
| LLM Provider | Anthropic SDK + 协议转换代理 | 统一 Provider 抽象（Anthropic/OpenAI/Mock/DeepV） | EasyAgent 抽象更干净 |
| 工具系统 | Tool 接口 + Zod schema | AgentTool 泛型 + TypeBox schema | 设计思路相似 |
| Agent 循环 | QueryEngine + 隐性循环 | 显式双层循环（外层 follow-up + 内层 tool call） | EasyAgent 结构更清晰 |
| 可扩展性 | 插件/钩子/Skills/Extension | Extension 接口 + Skill 系统 | 各有特色 |
| 桌面端 | Tauri (React) | Electron + React + Vite (`desktop/`) | 各有千秋：cc-haha 的 Tauri (Rust) 更轻量，EasyAgent 的 Electron 生态更成熟 |
| 远程执行 | SSH + Bridge (远程 Cloud 会话) | SSH Operations 接口 | EasyAgent 有基础能力 |
| 会话存储 | JSONL 树状 | JSONL 树状 | 设计一致 |
| 上下文压缩 | 四级渐进式压缩 | LLM 摘要 + 保留最近消息 | cc-haha 更精细 |
| MCP 支持 | 内置 | 规划中 | cc-haha 领先 |

### 功能覆盖对比

| 功能 | cc-haha | EasyAgent | 差距评估 |
|------|---------|-------|----------|
| 统一 LLM API | ✅ Anthropic SDK 直接调用 | ✅ 多 Provider 抽象 | EasyAgent 抽象更好 |
| Agent 双层循环 | ✅ QueryEngine | ✅ agent-loop.ts | 结构不同，均可 |
| 7 个内置工具 | ✅ (read/write/edit/bash/grep/find/ls) | ✅ 同样 7 个 | 持平 |
| 工具过滤 | ✅ AllowedTools/BlockedTools | ✅ config 层 | 持平 |
| 事件系统 + 流式 | ✅ AsyncGenerator | ✅ EventBus + SSE | 持平 |
| 上下文压缩 | ✅ 四级压缩 | ✅ LLM 摘要 | cc-haha 更精细 |
| JSONL 树状会话 | ✅ | ✅ | 持平 |
| 扩展系统 | ✅ 插件/钩子/Skills/MCP | ✅ Extension + Skill | cc-haha 更多样 |
| Skill 系统 | ✅ agentkills.io | ✅ Markdown 格式 | 基本持平 |
| HTTP API Server | ✅ 极其丰富 | ✅ REST + SSE | cc-haha 丰富 10x |
| SSH 远程执行 | ✅ | ✅ | 持平 |
| **桌面端 APP** | ✅ Tauri | ✅ Electron + React (**`desktop/`**) | cc-haha 的功能更丰富（Diff/审批面板等） |
| **IM 接入** | ✅ 4 种 IM | ❌ | **巨大差距** |
| **Provider 协议转换** | ✅ Anthropic↔OpenAI | ❌ | **显著差距** |
| **Computer Use** | ✅ | ❌ | **显著差距** |
| **多 Agent 系统** | ✅ Teams/Fork/Coordinator | ❌ 规划中 | **显著差距** |
| **记忆系统** | ✅ 跨会话持久化 | ✅ √ 文件记忆 | 基本持平 |
| **定时任务** | ✅ Cron + 桌面通知 | ❌ | 显著差距 |
| **MCP 支持** | ✅ 完整 | ❌ 规划中 | 显著差距 |
| **H5 远程访问** | ✅ 手机端 | ❌ | 显著差距 |
| **质量门禁** | ✅ 覆盖度/PR/Release | ❌ | 显著差距 |
| **插件市场** | ✅ 本地安装 | ❌ | 显著差距 |

### EasyAgent 的优势

1. **架构清晰、分层明确**：EasyAgent 的四层架构（Core → Platform → Application → Entrypoints）比 cc-haha 的扁平结构更工程化，模块间依赖关系清晰
2. **Go 语言的优势**：单二进制分发、无运行时依赖、goroutine 并发模型比 Bun/Node 更适合做 Agent 底座
3. **Provider 抽象更干净**：EasyAgent 的 Provider 注册制 + 懒加载 + 统一 EventStream 协议，比 cc-haha 的硬编码 Anthropic SDK 调用更灵活
4. **Agent 循环显式化**：EasyAgent 的双层循环（外层 follow-up + 内层 tool call）结构清晰，cc-haha 的循环逻辑分散在多文件中
5. **Operations 抽象**：EasyAgent 的 `operations.Operations` 接口（本地/SSH 切换）设计优雅，cc-haha 没有等价抽象
6. **代码体积**：EasyAgent 的 Go 代码 vs cc-haha 的 300+ 工具函数文件，EasyAgent 更精简

---

## 5. 迁移建议

### 优先级排序

| 优先级 | 特性/设计 | 迁移难度 | 预期收益 | 实现路径 |
|--------|----------|----------|----------|----------|
| P0 | **Provider 协议转换代理** | 中 | 高——解锁非 Anthropic 模型 | 在 EasyAgent `internal/ai/providers/` 下新增 `proxy` provider，实现 Anthropic ↔ OpenAI 协议转换 |
| P0 | **Server API 丰富化** | 低 | 高——桌面端/IM 的前置条件 | 在现有 `internal/server/` 基础上，按 cc-haha 的 API 目录补齐会话管理/Provider 管理/Skills 管理/文件系统等 API |
| P1 | **MCP 支持** | 中 | 高——标准工具扩展协议 | 新增 `internal/mcp/` 包，实现 MCP 客户端协议、工具发现和调用 |
| P1 | **桌面端 APP** | 高 | 高——提升用户体验 | 使用 Tauri 或 Wails (Go 原生 WebView) 构建桌面端，通过 EasyAgent Server API 通信 |
| P1 | **多 Agent 系统** | 中 | 高——Agent 协作场景 | 基于现有 Extension 系统和 Agent 循环，实现 Agent fork/subagent 和 Team 编排 |
| P2 | **IM 接入** | 中 | 中——远程操控 | 构建 adapter 公共层 + 各平台实现，通过 Server API 与 Agent 交互 |
| P2 | **定时任务** | 低 | 中——自动化场景 | 在 Server 层增加 cron 调度器，使用 EasyAgent 的 Agent Session 执行任务 |
| P2 | **Computer Use** | 高 | 中 | 需要桌面端支持 + 截图/输入模拟 |
| P2 | **质量门禁** | 低 | 中 | 在 CI 中增加覆盖度检查、Provider 冒烟测试 |
| P3 | **H5 远程访问** | 中 | 低 | 在 Server 层增加 Web UI + 一次性令牌认证 |
| P3 | **记忆系统增强** | 中 | 中 | 实现跨会话持久化记忆存储和检索 |

### 实施路线图

#### 阶段一：基础设施补齐（1-2 周）
- **Server API 丰富化**：增加会话管理、Provider 管理、Skills、文件系统等 REST API
- **Provider 协议转换代理**：新增 proxy provider 支持非 Anthropic 模型
- **MCP 支持**：实现 MCP 客户端协议

#### 阶段二：桌面端 + 多 Agent（2-4 周）
- **桌面端**：使用 Tauri 或 Wails 构建桌面 UI 框架，通过 Server API 通信
- **多 Agent 系统**：Agent fork/subagent 实现 + Team 编排协议
- **定时任务**：在 Server 层增加 Cron 调度

#### 阶段三：生态扩展（视需求）
- **IM 接入**：Telegram/飞书/微信/钉钉适配器
- **Computer Use**：桌面端截屏 + 输入模拟
- **H5 远程访问**：Web UI + 令牌认证

---

## 6. 详细参考

### 关键文件索引

| 文件路径 | 职责 | 值得关注的点 |
|----------|------|-------------|
| `src/QueryEngine.ts` | Agent 循环核心 | `submitMessage()` 生成器方法，整合系统提示/工具/命令/MCP/记忆 |
| `src/Tool.ts` | Tool 类型系统 | `Tool<Input, Output, P>` 泛型接口，15+ 方法，Zod v4 schema |
| `src/query.ts` | 系统提示构建 | `getUserContext()`/`getSystemContext()` 动态上下文 |
| `src/server/index.ts` | Server 入口 | Bun HTTP server，CORS/认证/路由/WebSocket |
| `src/server/router.ts` | API 路由 | 路由分发逻辑 |
| `src/server/proxy/handler.ts` | 协议转换代理 | Anthropic↔OpenAI 双向转换 |
| `src/server/services/sessionService.ts` | 会话管理服务 | 会话 CRUD + WebSocket 桥接 |
| `src/server/services/providerService.ts` | Provider 管理 | 多 Provider 配置和激活 |
| `src/coordinator/coordinatorMode.ts` | 协调器模式 | 多 Agent 编排模式控制 |
| `src/tools/AgentTool/forkSubagent.ts` | Fork 子 Agent | Agent 分支执行逻辑 |
| `src/bridge/bridgeMain.ts` | 远程会话桥接 | Cloud 会话管理，轮询/重连/超时 |
| `adapters/common/ws-bridge.ts` | IM WebSocket 桥接 | 通用 WS 适配器模式 |
| `desktop/src-tauri/` | Tauri 原生层 | Rust 侧：窗口管理、终端嵌入、系统更新 |
| `desktop/src/stores/` | React 状态管理 | Zustand store：session/chat/settings/provider/adapter 等 |
| `desktop/src/pages/` | 桌面端页面 | ActiveSession/Settings/ScheduledTasks/ComputerUse |
| `src/utils/thinking.ts` | Thinking 配置 | 扩展思考/自适应思考配置 |
| `src/utils/compaction/` | 上下文压缩 | 四级渐进式压缩策略 |
| `src/utils/mcpStdioEnvironment.ts` | MCP 进程管理 | MCP Server 的 stdio 通信管理 |
| `src/utils/sessionStorage.ts` | 会话持久化 | JSONL 存储 + 快照管理 |
| `src/skills/` | Skills 系统 | 技能加载/发现/条件激活 |
| `src/plugins/` | 插件系统 | 插件注册/加载/生命周期 |

### 参考资料

- [cc-haha GitHub 仓库](https://github.com/NanmiCoder/cc-haha)
- [cc-haha 文档站点](https://claudecode-haha.relakkesyang.org)
- cc-haha 英文 README：原调研仓库中的 `README.en.md`（未复制到本仓库）。
