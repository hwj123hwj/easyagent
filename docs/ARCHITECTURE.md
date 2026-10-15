# EasyAgent 架构

EasyAgent 由 Go Agent 核心、运行时、应用与入口组成。桌面、Web、TUI 和飞书共享会话与工具能力；动态工作流通过独立 Node 组件调用 Go 侧的真实 AgentSession。

## 分层与源码

依赖方向：`Entrypoints → Application → Platform → Core`。

| 层 | 主要目录 | 职责 |
|---|---|---|
| 入口 | `cmd/easyagent`、`cmd/easyagent-bridge` | 配置加载、CLI / TUI / HTTP 启动、飞书桥接 |
| 应用 | `internal/app`、`internal/agents`、`internal/server`、`internal/tui`、`internal/feishu`、`internal/web` | 应用组装、领域工具、协议和界面 |
| 平台 | `sdk/runtime` | AgentSession、Application 注册、会话配置、命令与扩展 |
| 核心 | `sdk/agent`、`sdk/ai`、`sdk/session`、`sdk/tools`、`sdk/mcp`、`sdk/operations` | Agent 循环、模型流、工具执行、MCP、历史和执行后端 |
| 编排 | `sdk/workflow`、`internal/dynamicworkflow`、`workflow-runtime` | YAML DAG 与动态 TypeScript 工作流 |

`sdk/` 是外部 Go 模块可导入的公共能力，不能依赖 `internal/`，由 [架构测试](../sdk/arch_test.go) 检查。音乐、飞书等领域逻辑属于应用层。

## 执行与流式回复

1. TUI 直接调用运行时；桌面和 Web 通过 HTTP / WebSocket；飞书桥接通过核心 HTTP / SSE 接口提交任务。
2. `AgentSession` 组装模型、系统提示、工具与历史，进入 Agent 循环。
3. Provider 持续产生文本和工具调用事件。Web、TUI 或桥接消费这些事件，无需等待整个 Agent 任务结束才显示回复。
4. 工具经过校验、参数准备、hooks 和执行；允许并发的连续工具组成批次，其余按序执行。结果回到模型上下文。
5. 会话消息、压缩和检查点写入 JSONL，由会话存储重建上下文。

飞书为避免频繁更新卡片而合并短时间内的事件；此刷新节奏与模型首字延迟、工具执行时间是不同阶段。取消和超时必须保留已有结果，未启动的工具不会因为“补齐历史”而自动执行。

服务端 run 持有任务，连接仅订阅其事件。接受前落盘请求凭据，WS 按 seq 恢复；核心重启后未完成 run 标记中断，旧请求不会自动再次执行。MCP 配置由 App 为各工作区组装，工具仍经过同一确认与执行路径；项目配置先授权，运行期修改受工具租约保护。

## Application 扩展

[runtime.Application](../sdk/runtime/application.go) 提供 `BuildTools`、`BuildPrompt` 和 `NewSessionExt`。入口目前注册 coding、music、kb；它们复用运行时，并分别提供领域能力。Skills 是任务方法和提示资源，不替代工具权限与 Application 的状态管理。

## 两类工作流

- [动态工作流](DYNAMIC_WORKFLOW.md)：Agent 编写 TypeScript，ZCode 引擎负责 Actor 和脚本调度，Go 适配层负责真实会话、权限、结果与持久化。需要 Node.js 22+ 和已构建 bundle。
- [YAML 流水线](WORKFLOW.md)：Go 原生 DAG，适用于已确定的步骤依赖、fan-out、重试、审批与缓存。

动态工作流支持按记录恢复，但中断中的外部副作用可能重复，恢复需要检查与确认。YAML 流水线的限制单独记录在其文档中，不能把两个引擎的恢复能力混用。

## 权限与部署边界

HTTP API 认证、飞书使用者配对、工具确认、文件路径限制是不同机制。`-y` 不解除工作区限制；`--allow-outside-workspace` 也不跳过认证。Bash 和 SSH 按运行账号的系统权限执行，文件工具的路径检查不是 OS 沙箱。

Web 静态资源嵌入 Go 程序；桌面本地模式管理独立 loopback 核心与令牌，远程模式通过主进程代理。迷你主机运行核心服务、飞书桥接和更新 timer，Mac 用于开发预览与本地任务；发布按 main 的确定 SHA 构建并验证，配置与会话数据独立保留。详见 [配置](CONFIG.md)、[MCP](MCP.md)、[桌面](DESKTOP.md)、[飞书](FEISHU.md) 和 [部署](MINI_DEPLOY.md)。
