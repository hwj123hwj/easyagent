# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## 行为准则

减少常见 LLM 编码错误。**权衡：偏谨慎而非速度。简单任务自行判断。**

### 先想后写

- 不要假设。有困惑先问，不要默默选一个方案。
- 有更简单的方案直接说，该反驳就反驳。

### 简洁优先

- 最少代码解决问题，不写需求之外的功能。
- 不为单次调用创建抽象，不为不可能的场景做错误处理。
- 自问："一个 senior 工程师会觉得这过度设计了吗？"

### 精准改动

- 只碰必须改的，不要顺手优化相邻代码、注释、格式。
- 匹配现有风格，即使你觉得自己的写法更好。
- 删除你的改动产生的孤儿代码（import/变量/函数），但不要删之前就存在的死代码。

### 目标驱动执行

- 把任务转为可验证目标："加校验" → "先写测试用例，再让它通过"
- 多步骤任务先列计划，每步带验证标准。

### 分支收尾

- 长期只保留 `main`，开发分支仅用于当前任务，不积累已完成的分支。
- PR 合并后，切回 `main` 并同步远端，删除对应的本地和远端开发分支，再运行 `git fetch --prune origin` 清理失效引用；这属于任务收尾，不能留给用户手动处理。
- 删除前确认提交已进入 `main`。未合并的提交、未提交的改动和仍在使用的 worktree 必须先核实并保留，不用强制删除来凑成“只剩 main”。
- 汇报完成前检查本地、远端分支；如仍有未合并或正在使用的分支，说明原因。

---

### 版本发布

- tag 使用带 `v` 的语义化版本和附注标签，只发布已进入 main 的确定提交。按 `docs/RELEASING.md` 和 `scripts/release.py` 操作。
- 发布前更新 CHANGELOG 并通过完整 CI；已发布 tag 和资产不得移动、删除重打或覆盖。
- main 的 SHA 自动部署与正式 Release 分开，不为每次合并自动打版本标签。

## 项目文档

- 项目介绍 & 架构：`README.md` / `docs/ARCHITECTURE.md`
- 开发流程 & 编码规范（分支命名、commit 格式等）：`docs/CONTRIBUTING.md`
- 架构决策：`docs/decisions/`
- 竞品调研：`docs/research/`

## 项目架构

四层分层，依赖方向：`Entrypoints → Application → Platform → Core`

```
cmd/easyagent  cmd/easyagent-bridge     ← 入口
internal/agents/ music/ feishu/ tui/…  ← 应用层（领域应用，不属于 SDK）
sdk/runtime/                           ← 平台层（AgentSession、Application 接口）
sdk/agent/ sdk/ai/ sdk/session/ …      ← 核心层（零领域知识）
```

层间规则：Core 不依赖上层；Platform 只依赖 Core；Application 通过 `runtime.Application` 接口解耦。

**SDK 边界**：`sdk/` 是可被外部 Go 模块 import 的公共 API 面（EasyAgent 的"原子能力"）；
`internal/` 是 EasyAgent 自身的应用与入口。**sdk/ 不得 import internal/ 的任何包**，
由 `sdk/arch_test.go` 强制。给 sdk/ 加代码必须保持零领域知识（音乐/飞书等永远不进 SDK）。

## 核心接口

| 接口 | 文件 | 作用 |
|------|------|------|
| `agent.Tool` | `sdk/agent/tool.go` | 工具系统，可选接口：`ToolWithMode`、`ConcurrencySafeChecker`、`ToolWithPrepareArguments` |
| `providers.Provider` | `sdk/ai/providers/interface.go` | LLM Provider 注册制（Name + Stream + StreamSimple） |
| `runtime.Application` | `sdk/runtime/application.go` | Platform↔App 解耦点：`BuildTools()` + `BuildPrompt()` + `NewSessionExt()` |
| `operations.Operations` | `sdk/operations/interface.go` | 本地/SSH 执行后端切换 |

## 关键设计

### Agent 双层循环 (`sdk/agent/loop.go`)

外层处理 follow-up，内层处理 tool call。`runAgentLoop()` 是 `RunLoop` 和 `PromptStream` 的共享核心，通过 `consumeStreamFunc` 回调区分行为。

### Tool 执行流程

分区（`partitionToolCalls`：连续 safe call→并行批次，unsafe→串行批次）→ 每批执行：Validate → PrepareArguments → Before hooks → Execute → After hooks

### Provider 注册

Provider 接口与实现位于 `sdk/ai/providers/`，应用入口组装注入。新增 Provider 必须同时接入实际配置选择逻辑并覆盖测试；不要仅添加实现后假定 CLI 会自动发现。

### 会话持久化

JSONL append-only（`sdk/session/jsonl.go`）：message/compaction/checkpoint 三种 entry，通过 `BuildContext()` 重建历史，支持 `MoveTo(entryID)` 分支导航。

### Goal-Driven Loop

当 goal 非空：取消 maxTurns 限制、LLM 评估完成度、自动注入 follow-up reminder。

## 命令

```bash
go build -o easyagent ./cmd/easyagent        # 构建
go test ./...                               # 全量测试
go test ./sdk/tools/ -v                # 单包测试
./easyagent chat                             # 交互式
./easyagent serve --listen :8080             # HTTP 服务
```

Provider 开发使用测试中注入的 mock 或本地模拟 HTTP 服务，不调真实 LLM。CLI 当前不接受 `EA_PROVIDER=mock`。
`EA_*` 为当前环境变量前缀；旧的 `PI_GO_*` 配置需要迁移，不会自动回退读取。

## 环境变量

核心变量（配置说明见 `docs/CONFIG.md`）：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `EA_PROVIDER` | 空，需显式配置 | `anthropic` / `openai` |
| `EA_ENABLE_BASH` | `false` | 启用 Bash 工具 |
| `EA_DATA_DIR` | `./data` | 会话数据目录 |
