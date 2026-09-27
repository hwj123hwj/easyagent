> 历史文档：本文保留更名前的 pi-go 项目记录；项目现名为 EasyAgent。

# 决策：workflow 引擎——Go 原生重设计，不移植上游 TS 栈

日期：2026-09-25
状态：已实施（`sdk/workflow` + `/workflows` API）；动态工作流方向已调整，见 [动态工作流接入契约](../DYNAMIC_WORKFLOW.md)。原先否决 Node sidecar 的取舍仅描述当时决策，本轮采用固定版本的 ZCode 引擎与独立 Node 组件。

## 背景

服务端形态需要"编排多个 Agent 任务"的能力（对应管理面路线的 P1/P2）。本地参考仓库 `../pi`（earendil-works/pi 上游）与本机 ZCode（`zai-org/ZCode` 开源仓库，Apache-2.0）各有一套 workflow 实现：上游是 protocol/server/durable 分层栈，ZCode 是模型写 TypeScript 脚本 + 编译器静态分析 + 沙箱子进程的 dynamic-workflow（约 2.2 万行 TS）。需要决定 pi-go 采用哪条路线。

## 决策

三条候选路线的取舍：

1. **移植 ZCode 的 dynamic-workflow 到 Go——否决**。它的核心资产（TS 编译器类型检查、污点分析、site graph）深度绑定 TypeScript AST，Go 无等价物；移植是无底洞。
2. **Node sidecar 复用原包——否决**。把 Node 拖进 Go 服务的部署链，违背单二进制哲学。
3. **Go 原生重设计（采纳）**：保留 ZCode 方案的语义精华，换掉载体：
   - 编排对象 = `runtime.AgentSession`：每步派生独立会话（上下文隔离，prompt 自包含）；
   - 步骤缓存 = 内容签名（工作流名/步骤/项/渲染后 prompt/模型）→ 重跑只付未变化步骤的 token，即 ZCode journal 缓存的等价物；
   - journal = `DataDir/workflows/<run-id>/journal.jsonl` + `meta.json`，磁盘为唯一事实来源；
   - 入口 = 声明式 YAML 管道（顺序依赖、foreach fan-out、retries、confirm 人工确认门），而非"模型写脚本"；模型写脚本的 UX 留作观察项（若做，用 goja 嵌 JS facade，放弃 TS 类型检查的严谨性）。

工程边界：

- `sdk/workflow` 零领域知识、不依赖 internal（arch_test 兼容）；应用差异通过 `RunnerFactory` 接口注入，`internal/server` 用 `app.App` 提供实现。
- 磁盘是查询事实来源：进程重启后历史运行仍可 GET；活跃运行信息（取消、审批）在内存 Registry。
- runID 强格式校验（`wf-<unix>-<hex8>`）防路径穿越。

## ZCode 调研补充

- ZCode 开源完整（`dynamic-workflow` + `dynamic-workflow-runtime` + bootstrap driver），Apache-2.0，可合法借鉴；本次只借语义不搬代码，故无 NOTICE 义务。
- ZCode 的 **computer use 未开源**：`packages/zcode-cua` 是 fail-closed 占位包（README "This build ships without Computer Use"，全部运行时接口 throw UNAVAILABLE）。pi-go 的 OS/浏览器操作路线改为 MCP 接现成 server（Playwright MCP / chromedp-based / AppleScript-based），工具权限走 `sdk/policy`，见 [server-admin-plane-roadmap.md](server-admin-plane-roadmap.md)。

## 备选与放弃原因

- **上游 pi 的多 presentation attach**：一个持久会话多端挂载对三端共用会话有吸引力，但当前会话模型是单消费方，改造面大；列为观察项，不随本次实现。
- **共享会话模式（步骤共用上下文）**：与 foreach 并发冲突，且隔离步骤 + 模板自包含已覆盖主要场景；先不做。

## 影响

- 新增 `sdk/workflow`（约 1200 行含测试）与 `/workflows` 六个端点；`docs/WORKFLOW.md` 为规范与用户文档。
- 每个工作流步骤生成真实会话，`/sessions` 列表会出现工作流步骤会话（可回看，是特性）。
- 已知限制记录在 WORKFLOW.md 末尾：重启丢失等待中的审批、confirm 不支持 foreach、无共享会话。
