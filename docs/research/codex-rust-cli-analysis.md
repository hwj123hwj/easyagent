> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# OpenAI Codex CLI (Rust) 调研报告 — 架构设计与功能分析

> 调研日期：2025-05-24
> 来源：本地仓库 `/Users/weijian/Desktop/develop/test/pi/codex`（GitHub: `openai/codex`）
> 调研目标：深入分析 Codex CLI 的 Rust 实现架构，对比 EasyAgent 的设计，提炼可迁移的模式

---

## 1. 概述

### 项目定位

| 项目 | 角色 | 技术栈 | 定位 |
|------|------|--------|------|
| Codex CLI | OpenAI 官方编码 Agent | Rust (90+ crate workspace) | 本地运行的 AI 编码助手，强调安全沙箱和多客户端支持 |
| EasyAgent | 我们的 Agent 框架 | Go | 通用 Agent 底座 + coding-agent 应用层 |

### 核心发现摘要

1. **单层 Turn 循环**：不同于 EasyAgent 的双层循环，Codex 采用单层 Turn 循环，Turn 内部通过采样循环处理多轮工具调用
2. **Responses API + WebSocket 优先**：使用 OpenAI Responses API（非 Chat Completions），优先 WebSocket 连接，失败自动回退 HTTP
3. **90+ crate 的超细粒度模块化**：每个功能领域都是独立 crate，`codex-core` 是最大的但也被明确要求"抵制膨胀"
4. **跨平台沙箱是核心卖点**：macOS Seatbelt + Linux Landlock+Seccomp+Bubblewrap + Windows RestrictedToken，三层安全模型
5. **App Server 架构支持多客户端**：JSON-RPC 2.0 协议，4 种传输模式（stdio/WebSocket/UnixSocket/In-Process），支持 VSCode、桌面应用等富客户端

---

## 2. 架构分析

### 整体架构

```
┌──────────────────────────────────────────────────────────────┐
│  Entrypoints                                                 │
│  cli/ (多命令入口)  tui/ (终端 UI)  exec/ (非交互)           │
│  app-server/ (JSON-RPC 服务)  mcp-server/ (MCP 服务器)       │
├──────────────────────────────────────────────────────────────┤
│  Core 业务逻辑                                               │
│  core/ — Agent 循环、工具、会话、压缩、配置、沙箱策略        │
├──────────────────────────────────────────────────────────────┤
│  Protocol & Tools                                            │
│  protocol/ (事件/消息协议)  tools/ (工具定义/执行器)          │
│  config/ (TOML 配置)  sandboxing/ (沙箱抽象)                 │
├──────────────────────────────────────────────────────────────┤
│  Infrastructure                                              │
│  codex-api/ (LLM API 客户端)  codex-mcp/ (MCP 客户端)       │
│  hooks/ (钩子引擎)  login/ (认证)  otel/ (可观测性)          │
│  linux-sandbox/  process-hardening/  network-proxy/          │
└──────────────────────────────────────────────────────────────┘
```

### 核心组件职责

| 组件 | 路径 | 职责 |
|------|------|------|
| `cli` | `codex-rs/cli/` | 多命令入口（tui/exec/sandbox/mcp/doctor 等） |
| `tui` | `codex-rs/tui/` | Ratatui 全屏 TUI，事件驱动异步架构 |
| `exec` | `codex-rs/exec/` | 非交互式执行，用于自动化/CI |
| `core` | `codex-rs/core/` | Agent 循环、工具执行、会话管理、压缩、安全策略 |
| `tools` | `codex-rs/tools/` | 工具定义（ToolSpec）、执行器 trait（ToolExecutor） |
| `protocol` | `codex-rs/protocol/` | 事件协议、消息类型、配置类型 |
| `config` | `codex-rs/config/` | TOML 配置加载、合并、验证 |
| `sandboxing` | `codex-rs/sandboxing/` | 跨平台沙箱抽象（Seatbelt/Bubblewrap/RestrictedToken） |
| `app-server` | `codex-rs/app-server/` | JSON-RPC 2.0 服务端，支持多客户端 |
| `codex-mcp` | `codex-rs/codex-mcp/` | MCP 客户端，连接外部 MCP 服务器 |
| `mcp-server` | `codex-rs/mcp-server/` | MCP 服务器，让 Codex 作为其他 Agent 的工具 |
| `hooks` | `codex-rs/hooks/` | 10 种事件的钩子引擎 |
| `skills` | `codex-rs/skills/` | 技能加载和管理 |
| `codex-api` | `codex-rs/codex-api/` | LLM API 客户端（Responses API） |

### Agent 循环

Codex 的 Agent 循环是**单层 Turn 循环**，与 EasyAgent 的双层循环不同：

```
ThreadManager
  └── CodexThread（单个会话线程）
        └── Session（会话状态）
              └── run_turn（轮次执行）
                    └── 采样循环（loop）
                          ├── 构建 Prompt（历史 + 技能注入 + 上下文）
                          ├── ModelClient.stream() → LLM Responses API
                          ├── 解析 ResponseItem → TurnItem
                          ├── 工具调用 → ToolRouter.dispatch()
                          ├── 判断 needs_follow_up → continue/break
                          └── token_limit_reached → auto_compact → continue
```

**关键差异**：
- EasyAgent：外层循环处理 follow-up，内层循环处理 tool call，职责分离
- Codex：单层采样循环，Turn 内部通过 `needs_follow_up` 标志控制是否继续

### 消息流转

```
用户输入 → Codex.tx_sub (Submission 通道)
  → Session.run_turn()
    → TurnContext（构建 Prompt）
    → ModelClientSession.stream()（WebSocket/HTTP）
    → LLM Responses API
    → ResponseStream（SSE/WebSocket 事件流）
    → ResponseItem → parse_turn_item() → TurnItem
    → Event → Codex.rx_event → UI/Client
```

### LLM API 交互

```rust
// client.rs 核心设计
pub struct ModelClient {
    state: Arc<ModelClientState>,  // 持有 auth、provider、transport 状态
}

pub struct ModelClientSession {
    client: ModelClient,
    websocket_session: WebsocketSession,  // WebSocket 连接（懒初始化）
    turn_state: Arc<OnceLock<String>>,    // 粘性路由 token
}
```

**传输策略**：
1. 优先 WebSocket（`stream_responses_websocket`）
2. WebSocket 失败 → 回退 HTTP SSE（`stream_responses_api`）
3. 支持 WebSocket prewarm：`response.create` with `generate=false` 预热连接
4. `turn_state` 实现粘性路由（sticky routing）

### 上下文压缩

三种压缩模式：

| 模式 | 触发时机 | 注入策略 |
|------|---------|---------|
| Mid-turn | token_limit_reached + needs_follow_up | `BeforeLastUserMessage` — 在最后用户消息前注入初始上下文 |
| Pre-turn | 轮次开始前 | `DoNotInject` — 清空 reference_context_item，依赖下次轮次重新注入 |
| Remote | 远程压缩服务 | `compact_remote_v2` — 调用远程 API 压缩 |

---

## 3. 功能分析

### 工具系统

#### 内置工具清单

| 工具 | 职责 | 创新程度 |
|------|------|---------|
| `exec_command` | 统一命令执行（支持 TTY、超时、截断） | 核心 |
| `shell_command` | Shell 命令执行（传统工具） | 核心 |
| `apply_patch` | 应用 unified diff 补丁 | 核心 |
| `write_stdin` | 向持久进程写入 stdin | 创新 |
| `view_image` | 查看图像 | 基础 |
| `tool_search` | 工具搜索发现 | 创新 |
| `plan` | 计划生成 | 创新 |
| `goal` (CRUD) | 目标管理（创建/获取/更新） | 创新 |
| `multi_agents` | 多 Agent 管理（spawn/close/resume/send_input/wait） | 创新 |
| `agent_jobs` | Agent 任务管理 | 创新 |
| `request_user_input` | 请求用户输入 | 基础 |
| `request_permissions` | 请求额外权限 | 基础 |
| `request_plugin_install` | 请求插件安装 | 创新 |
| `mcp` | MCP 工具调用 | 创新 |
| `mcp_resource` | MCP 资源（列出/读取） | 创新 |
| `dynamic` | 动态工具 | 创新 |
| `extension_tools` | 扩展工具 | 创新 |
| `test_sync` | 测试同步 | 基础 |

#### 工具抽象层

```rust
// tools/tool_executor.rs — 核心执行器 trait
pub trait ToolExecutor<Invocation>: Send + Sync {
    fn tool_name(&self) -> ToolName;
    fn spec(&self) -> ToolSpec;
    fn exposure(&self) -> ToolExposure;  // Direct/Deferred/DirectModelOnly/Hidden
    fn supports_parallel_tool_calls(&self) -> bool;
    async fn handle(&self, invocation: Invocation) -> Result<Box<dyn ToolOutput>, FunctionCallError>;
}

// core/tools/registry.rs — 扩展运行时 trait
pub(crate) trait CoreToolRuntime: ToolExecutor<ToolInvocation> {
    fn search_info(&self) -> Option<ToolSearchInfo>;
    fn telemetry_tags(&self, invocation: &ToolInvocation) -> BoxFuture<ToolTelemetryTags>;
    fn pre_tool_use_payload(&self, invocation: &ToolInvocation) -> Option<PreToolUsePayload>;
    fn post_tool_use_payload(&self, invocation: &ToolInvocation, result: &dyn ToolOutput) -> Option<PostToolUsePayload>;
}
```

**ToolSpec 类型**（Responses API 兼容）：

```rust
pub enum ToolSpec {
    Function(ResponsesApiTool),      // 标准函数工具
    Namespace(ResponsesApiNamespace), // 命名空间工具（code mode）
    ToolSearch { ... },               // 工具搜索
    ImageGeneration { ... },          // 图像生成
    WebSearch { ... },                // 网页搜索
    Freeform(FreeformTool),           // 自由格式工具
}
```

#### 工具执行流程

```
ToolRouter.dispatch()
  → ToolRegistry 查找
  → Pre-Tool-Use Hooks（可修改输入、阻断执行）
  → ToolOrchestrator（审批 → 沙箱选择 → 执行 → 重试升级）
    → SandboxManager.transform()（包装沙箱命令）
    → ToolRuntime.handle()（实际执行）
  → Post-Tool-Use Hooks
  → 返回 ToolOutput
```

### 沙箱机制

#### 跨平台沙箱类型

| 平台 | 沙箱技术 | 实现 |
|------|---------|------|
| macOS | Seatbelt (`/usr/bin/sandbox-exec`) | SBPL 策略文件 |
| Linux | Landlock + Seccomp + Bubblewrap | 文件系统隔离 + 网络过滤 + 命名空间 |
| Windows | Restricted Token | 受限令牌 |

#### Linux 沙箱实现细节

```
codex-linux-sandbox 进程
  ├── 1. Bubblewrap 构建文件系统视图
  │     --ro-bind / /  (整个文件系统只读)
  │     --bind workspace workspace  (工作区可写)
  │     .git/.agents/.codex 永久只读
  ├── 2. set_no_new_privs()
  ├── 3. Seccomp 网络过滤
  │     Restricted: 拒绝 connect/accept/bind，仅允许 AF_UNIX
  │     ProxyRouted: 仅允许 AF_INET/AF_INET6（连接本地代理）
  ├── 4. Landlock 文件系统规则
  └── 5. execvp 进入最终命令
```

#### 三种沙箱模式

| 模式 | 文件系统 | 网络 | 用途 |
|------|---------|------|------|
| `read-only` | 完全只读 | 禁止 | 默认安全模式 |
| `workspace-write` | 工作区可写 + `~/.codex/memories` | 禁止 | 日常开发 |
| `danger-full-access` | 完全访问 | 允许 | 容器内使用 |

### App Server 架构

#### 协议设计

基于 JSON-RPC 2.0，但省略 `"jsonrpc":"2.0"` 头以减少带宽：

```json
// 请求
{"method": "thread/start", "id": 10, "params": {"model": "gpt-5.1-codex", "cwd": "/project"}}
// 响应
{"id": 10, "result": {"thread": {"id": "thr_123"}}}
// 通知（无 id）
{"method": "thread/started", "params": {"thread": {"id": "thr_123"}}}
```

#### 核心原语

```
Thread  → 会话（包含多个 Turn）
Turn    → 对话轮次（用户输入 + 响应，包含多个 Item）
Item    → 具体操作项（消息/命令/文件变更/工具调用）
```

#### 传输模式

| 传输 | 用途 | 特点 |
|------|------|------|
| stdio | CLI 默认 | JSONL，管道友好 |
| WebSocket | VSCode 扩展 | 双向实时 |
| Unix Socket | 本地 IPC | 高效进程间通信 |
| In-Process | TUI/Exec | 零序列化开销 |

#### 串行化策略

通过声明式 Scope 自动管理并发：

| Scope | 说明 | 示例 |
|-------|------|------|
| None | 不串行 | `model/list`、`fs/readFile` |
| Global(key) | 全局单锁 | `config/write` |
| Thread(id) | 单线程锁 | `turn/start` |
| Process(id) | 进程级锁 | `command/exec` |

### TUI 实现

#### 架构模式

事件驱动异步架构（非严格 Elm/MVU）：

```
┌─────────────────────────────────────────────┐
│            App::run 主事件循环               │
│  tokio::select! 多路复用:                    │
│  - app_event_rx (应用事件)                   │
│  - active_thread_rx (线程事件)               │
│  - tui_events (键盘/终端事件)                │
│  - app_server.next_event() (后端事件)        │
└─────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────┐
│              App (顶层协调器)                 │
│  管理会话状态、线程生命周期、事件分发         │
└─────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────┐
│           ChatWidget (核心聊天界面)           │
│  历史消息管理、流式输出、协议事件处理         │
└─────────────────────────────────────────────┘
    ↓              ↓              ↓
BottomPane    HistoryCell     StreamState
(底部面板)    (消息单元)      (流式输出)
```

#### HistoryCell trait（多态消息单元）

```rust
pub(crate) trait HistoryCell: Debug + Send + Sync + Any {
    fn display_lines(&self, width: u16) -> Vec<Line<'static>>;  // 主视口渲染
    fn raw_lines(&self) -> Vec<Line<'static>>;                   // 覆盖层原始文本
    fn desired_height(&self, width: u16) -> u16;                 // 高度计算
    fn transcript_lines(&self, width: u16) -> Vec<Line<'static>>; // 覆盖层专用
    fn transcript_animation_tick(&self) -> Option<u64>;           // 动画节拍
}
```

实现类型：`AssistantMessageCell`、`UserMessageCell`、`ExecCell`、`DiffCell`、`WebSearchCell`、`McpToolCallCell` 等。

#### 键盘快捷键系统（7 层上下文）

```rust
pub(crate) struct RuntimeKeymap {
    pub(crate) app: AppKeymap,           // 应用级
    pub(crate) chat: ChatKeymap,         // 聊天级
    pub(crate) composer: ComposerKeymap, // 输入框级
    pub(crate) editor: EditorKeymap,     // 编辑器级
    pub(crate) vim_normal: VimNormalKeymap,
    pub(crate) vim_operator: VimOperatorKeymap,
    pub(crate) pager: PagerKeymap,       // 分页器
    pub(crate) list: ListKeymap,         // 列表选择
    pub(crate) approval: ApprovalKeymap, // 批准弹窗
}
```

#### 流式输出

- 自适应分块策略（`AdaptiveChunkingPolicy`）
- 队列压力感知（`oldest_queued_age()`）
- ~60fps 动画节拍（16ms `COMMIT_ANIMATION_TICK`）
- Markdown 流式解析（`pulldown-cmark`）

#### Diff 渲染

- 主题感知（Dark/Light + TrueColor/Ansi256/Ansi16）
- 语法高亮（`syntect`）
- 行号右对齐 + gutter 符号

### 配置系统

#### 多层配置堆栈

支持全局、项目、会话等多层配置覆盖，使用 TOML 格式：

```toml
# MCP 服务器
[mcp_servers.server_name]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
enabled = true
default_tools_approval_mode = "auto"

# 技能
[skills]
bundled.enabled = true
[[skills.config]]
path = "/path/to/skill"

# Hooks
[hooks.PreToolUse]
[[hooks.PreToolUse]]
matcher = "tool:*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "./pre_tool_hook.sh"

# 记忆
[memories]
generate_memories = true
use_memories = true
max_rollouts_per_startup = 2
```

### MCP 集成（双向）

Codex **同时是 MCP 客户端和 MCP 服务器**：

- **客户端**（`codex-mcp/`）：连接外部 MCP 服务器，工具发现/注册/调用，OAuth 认证
- **服务器**（`mcp-server/`）：`codex mcp-server` 启动，暴露线程/turn/账户/配置管理接口

### 技能系统

四种技能范围：

| 范围 | 存储位置 | 说明 |
|------|---------|------|
| System | `~/.codex/skills/.system/` | 内置系统技能 |
| Bundled | 随发行版打包 | 可配置启用/禁用 |
| User | 用户目录 | 用户自定义 |
| Project | 项目目录 | 项目级技能 |

### Hooks 系统

10 种钩子事件：

```
PreToolUse / PostToolUse / PermissionRequest
PreCompact / PostCompact
SessionStart / UserPromptSubmit / Stop
SubagentStart / SubagentStop
```

支持 Command/Prompt/Agent 三种钩子类型，matcher 模式匹配，异步执行。

### 记忆系统（两阶段）

1. **Phase 1 - Rollout 提取**：扫描最近会话，模型提取结构化记忆，并发处理
2. **Phase 2 - 全局整合**：聚合 Phase 1 输出，生成 `MEMORY.md` 等最终输出

存储在 `~/.codex/memories/`，支持 Git 版本控制。

---

## 4. 与 EasyAgent 对比

### 架构理念对比

| 维度 | Codex CLI | EasyAgent | 评价 |
|------|-----------|-------|------|
| **语言** | Rust | Go | Rust 更安全但门槛高，Go 更简洁 |
| **模块化** | 90+ crate 超细粒度 | ~10 package 适度粒度 | Codex 更灵活但复杂度高 |
| **Agent 循环** | 单层 Turn 循环 | 双层循环（follow-up + tool call） | EasyAgent 职责分离更清晰 |
| **LLM API** | Responses API（WebSocket 优先） | 多 Provider 统一 API | EasyAgent 更通用，Codex 更深度集成 OpenAI |
| **工具系统** | ToolExecutor trait + CoreToolRuntime | Tool 泛型 + Operations 接口 | 思路相似，Codex 更多内置工具 |
| **沙箱** | 跨平台三层沙箱 | 无内置沙箱（依赖外部） | Codex 安全性远超 EasyAgent |
| **配置** | TOML 多层堆栈 | JSON 配置 | Codex 更灵活 |
| **TUI** | Ratatui 事件驱动 | pi-tui Elm/MVU | 架构风格不同，各有优劣 |
| **Server** | JSON-RPC 2.0（4 种传输） | HTTP REST + SSE | Codex 更丰富 |
| **MCP** | 双向（客户端+服务器） | 无 | Codex 领先 |
| **可观测性** | OTEL 集成 | 基础日志 | Codex 更完善 |

### 功能覆盖对比

| 功能 | Codex CLI | EasyAgent | 差距评估 |
|------|-----------|-------|----------|
| 多 Provider LLM | OpenAI 深度集成 | 多 Provider 抽象 | EasyAgent 更通用 |
| Agent 双层循环 | 单层 Turn 循环 | 外层 follow-up + 内层 tool call | EasyAgent 设计更清晰 |
| 内置工具 | 18+ 工具 | 7 工具 | Codex 工具集更丰富 |
| 沙箱安全 | 三层跨平台沙箱 | 无 | **Codex 压倒性优势** |
| 执行策略 | 规则引擎 + 启发式 | 工具过滤 | Codex 更精细 |
| TUI | Ratatui 全功能 | pi-tui | Codex 更成熟 |
| App Server | JSON-RPC 2.0（4 传输） | HTTP REST | Codex 更丰富 |
| MCP 集成 | 双向（client+server） | 无 | **Codex 压倒性优势** |
| 技能系统 | 4 种范围 + 依赖管理 | Markdown 格式加载 | Codex 更完善 |
| 插件系统 | 插件市场 + MCP + Hooks | Extension 接口 | Codex 更丰富 |
| Hooks | 10 种事件 + 3 种类型 | 事件钩子 | Codex 更完善 |
| 记忆系统 | 两阶段 + Git 版本控制 | 无 | Codex 领先 |
| 多 Agent | multi_agents 工具 | 无 | Codex 领先 |
| 上下文压缩 | Mid-turn + Pre-turn + Remote | LLM 摘要 + 保留最近消息 | Codex 更多样 |
| 会话持久化 | SQLite + JSONL rollout | JSONL 树状存储 | 各有特色，EasyAgent 支持分支 |
| 认证 | ChatGPT OAuth + API Key | API Key | Codex 更便利 |
| 可观测性 | OTEL 集成 | 基础日志 | Codex 更完善 |

### EasyAgent 的优势

1. **多 Provider 通用性**：EasyAgent 的统一 LLM API 抽象（Provider 注册制 + 懒加载）比 Codex 深度绑定 OpenAI 更灵活
2. **双层循环设计**：外层 follow-up + 内层 tool call 的职责分离比 Codex 的单层循环更清晰
3. **树状会话存储**：JSONL 树状存储支持分支和 MoveTo，比 Codex 的线性 rollout 更灵活
4. **Go 语言优势**：单二进制分发、编译速度快、goroutine 并发模型简洁
5. **SSH 远程执行**：EasyAgent 内置 SSH Operations 支持远程执行，Codex 无此能力
6. **Application 接口解耦**：runtime.Application 接口实现了 Platform 与 Application 的彻底解耦
7. **更简洁的架构**：~10 个 package vs 90+ crate，更易理解和维护

---

## 5. 迁移建议

### 优先级排序

| 优先级 | 特性/设计 | 迁移难度 | 预期收益 | 实现路径 |
|--------|----------|----------|----------|----------|
| **P0** | 执行策略引擎（规则+启发式） | 中 | 高 — 安全性基础 | 新建 `internal/execpolicy/`，实现 PrefixRule + NetworkRule + 启发式回退 |
| **P0** | 沙箱抽象层 | 高 | 极高 — 安全性核心 | 新建 `internal/sandboxing/`，先支持 Linux landlock，后续扩展 |
| **P1** | MCP 双向集成 | 中 | 高 — 生态扩展 | 新建 `internal/mcp/`，先实现 client，再实现 server |
| **P1** | Hooks 系统增强 | 低 | 中 — 扩展性 | 扩展 `internal/extensions/`，增加 PreToolUse/PostToolUse 等事件 |
| **P1** | 多层配置堆栈 | 中 | 中 — 灵活性 | 改造 `internal/config/`，支持全局/项目/会话层覆盖 |
| **P2** | 记忆系统 | 中 | 中 — 用户体验 | 新建 `internal/memory/`，两阶段架构 |
| **P2** | App Server 协议增强 | 高 | 中 — 多客户端 | 改造 `internal/server/`，支持 JSON-RPC 2.0 + 多传输 |
| **P2** | 多 Agent 支持 | 高 | 高 — 高级场景 | 扩展 agent 循环，支持子 agent spawn/manage |
| **P2** | HistoryCell 多态设计 | 低 | 中 — TUI 扩展性 | 改造 pi-tui 消息渲染为 trait 多态 |
| **P3** | 插件市场 | 高 | 低 — 生态（需基础设施） | 需要后端服务支持 |

### 实施路线图

**Phase 1（安全基础，2-3 周）**：
- 实现执行策略引擎（execpolicy）
- 实现沙箱抽象层（先 Linux landlock）
- 集成到现有 bash 工具

**Phase 2（生态扩展，2-3 周）**：
- MCP 客户端集成
- Hooks 系统增强（PreToolUse/PostToolUse）
- 多层配置堆栈

**Phase 3（高级特性，3-4 周）**：
- 记忆系统
- 多 Agent 支持
- App Server 协议增强

---

## 6. 详细参考

### 关键文件索引

| 文件路径 | 职责 | 值得关注的点 |
|----------|------|-------------|
| `codex-rs/core/src/client.rs` | LLM API 客户端 | WebSocket 优先 + HTTP 回退，粘性路由 |
| `codex-rs/core/src/session/turn.rs` | Turn 执行循环 | 单层采样循环，auto_compact 触发 |
| `codex-rs/core/src/session/session.rs` | Session 状态 | 双层上下文（Session + TurnContext） |
| `codex-rs/core/src/thread_manager.rs` | 线程管理 | Thread 生命周期、多线程并发 |
| `codex-rs/core/src/codex_delegate.rs` | 子代理 Delegate | 事件转发、操作转发模式 |
| `codex-rs/core/src/exec.rs` | 命令执行 | 沙箱包装、输出截断、超时处理 |
| `codex-rs/core/src/exec_policy.rs` | 执行策略 | 规则引擎 + 启发式回退 |
| `codex-rs/core/src/tools/registry.rs` | 工具注册表 | CoreToolRuntime trait、dispatch 流程 |
| `codex-rs/core/src/tools/orchestrator.rs` | 工具编排 | 审批 → 沙箱 → 执行 → 重试升级 |
| `codex-rs/tools/src/tool_spec.rs` | ToolSpec 定义 | Responses API 兼容的 6 种工具类型 |
| `codex-rs/tools/src/tool_executor.rs` | ToolExecutor trait | 工具执行器核心接口 |
| `codex-rs/sandboxing/src/manager.rs` | 沙箱管理器 | 跨平台沙箱抽象 |
| `codex-rs/sandboxing/src/seatbelt.rs` | macOS Seatbelt | SBPL 策略文件 |
| `codex-rs/linux-sandbox/src/landlock.rs` | Linux 沙箱 | Landlock + Seccomp 实现 |
| `codex-rs/linux-sandbox/src/bwrap.rs` | Bubblewrap | 文件系统命名空间隔离 |
| `codex-rs/app-server-protocol/src/protocol/common.rs` | 协议定义 | 宏驱动的 RPC 方法定义 |
| `codex-rs/tui/src/app.rs` | TUI 主应用 | 事件循环、组件协调 |
| `codex-rs/tui/src/chatwidget.rs` | 聊天组件 | 消息管理、流式输出 |
| `codex-rs/tui/src/keymap.rs` | 键盘快捷键 | 7 层上下文 keymap |
| `codex-rs/tui/src/diff_render.rs` | Diff 渲染 | 主题感知 + 语法高亮 |
| `codex-rs/tui/src/markdown_render.rs` | Markdown 渲染 | pulldown-cmark 事件流处理 |
| `codex-rs/config/src/config_toml.rs` | 配置加载 | TOML 多层堆栈 |
| `codex-rs/codex-mcp/` | MCP 客户端 | MCP 工具发现/注册/调用 |
| `codex-rs/mcp-server/` | MCP 服务器 | Codex 作为 MCP 工具 |
| `codex-rs/hooks/src/lib.rs` | Hooks 引擎 | 10 种事件 + 3 种钩子类型 |
| `codex-rs/core/src/compact.rs` | 上下文压缩 | Mid-turn/Pre-turn 两阶段 |
| `codex-rs/core/src/compact_remote_v2.rs` | 远程压缩 | 调用远程 API 压缩 |

### 参考资料

- GitHub 仓库：`https://github.com/openai/codex`
- 官方文档：`https://developers.openai.com/codex`
- 配置文档：`docs/config.md`
- 沙箱文档：`docs/sandbox.md`
- AGENTS.md：项目的 AI Agent 编码规范
