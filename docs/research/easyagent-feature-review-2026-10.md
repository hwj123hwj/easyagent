---
status: reviewed
author: research-agent
created: 2026-10-15
updated: 2026-10-15
---

# 功能体验评审：来自 Agent 本体的第一人称报告（2026-10）

> 评审者身份特殊：本次评审由运行在 EasyAgent 自身之上的 EasyAgent 完成。
> 以下反馈不是基于竞品分析或代码走读的推测，而是评审过程中**真实操作产生的一手体验**，
> 场景包括跑全量测试、检索代码库、读路线图等典型 agent 工作流。
>
> 范围限定为**功能层**（工程层问题——测试污染、lint 缺失、可观测性等——另见本文件末尾附注，
> 不在本文展开）。

---

## TL;DR

| 优先级 | 建议 | 一句话理由 |
|--------|------|-----------|
| P0 | **进程管理：bash 流式输出 + 后台任务** | 一套基础设施同时解决"长任务黑盒"和"无法并行"两大痛点 |
| P1 | **子 agent / task 工具** | 核心价值是上下文隔离，防止探索性工作污染主会话 |
| P1 | **read 图片/二进制感知** | 多模态入口（桌面/飞书）已存在，工具层接不住用户发截图的场景 |
| P2 | 修正 ROADMAP 中"工具 7 件套"的过时描述 | 实际已有 20+ 工具，文档失真会误导后续决策 |

---

## 1. P0：进程管理 —— bash 流式输出 + 后台任务

### 1.1 现状与体感

`bash` 工具当前为阻塞式执行：命令不结束，agent 收不到任何输出。

评审过程中的真实体感：执行 `go test ./...` 时**完全黑盒等待**——
无法区分"正在正常跑"与"已经卡死"，无法基于中间输出（如编译错误）提前决策，
超时只能凭经验拍脑袋。

### 1.2 为什么合并为一件事

流式输出与后台任务共享同一套基础设施（进程生命周期管理 + 输出管道），且痛点高度互补：

- **流式输出**解决"看得见"：
  - 基于中间输出决策（见到编译错误即提前终止，不等全量跑完）
  - 超时判断有据可依
- **后台任务**解决"并行"：
  - 典型场景："后台跑测试，前台继续读代码"——这是 agent 工作流最自然的并行模式，当前被堵死

### 1.3 设计要点（建议）

- `sdk/tools/bash.go`：输出回调接口（如 `OnOutput(chunk)`），由上层（TUI/HTTP WS）订阅
- 进程管理器：`start / poll / kill / write-stdin` 四原语 + 任务句柄
- 新工具形态参考竞品惯例：`bash_output(task_id)` 轮询 + `bash_kill(task_id)` 终止
- 与现有 tool lifecycle（`BeforeToolCallHook` 等）衔接，而非绕开
- 注意与 `sdk/policy` 确认门的交互：后台任务的 kill/output 读取应默认免确认

### 1.4 价值定位

1. **所有入口共用**：CLI / Desktop / 飞书 bridge 全部受益（符合路线图"能力沉淀在共用层"原则）
2. **决定长任务体验上限**：产品叫 EasyAgent，长任务黑盒恰是最 "hard" 的部分
3. **基础设施属性**：越晚做，越多功能要为它返工

---

## 2. P1：子 agent / task 工具

### 2.1 现状

所有工具调用与中间结果都进入主会话上下文。探索性任务（"看看这代码有什么问题"）
产生的大量 grep/read 中间输出只能依赖 compaction 兜底。

### 2.2 核心价值：上下文隔离（而非并行）

- 脏活（大范围检索、多文件走读）在子 agent 内完成，仅返回结论
- 主会话上下文保持干净，compaction 频率下降，长会话质量提升
- SDK 层 agent 循环（`sdk/agent/loop.go`）已可复用，包一层 `task` 工具的边际成本可控

### 2.3 设计要点（建议）

- `sdk/tools/task.go`：新工具，参数大致为 `{ description, prompt }`
- 子 agent 默认**受限工具集**（如只读类：read/grep/find/ls），阻断递归创建
- 与 `sdk/session` 的关系需要先想清楚：子 agent 会话是否持久化、是否出现在 `/sessions`
- 竞品参照：仓库内已有现成调研（`docs/research/oh-my-pi-full-analysis.md` 的 Subagent 章节可先读）

---

## 3. P1：read 图片/二进制感知

### 3.1 现状

`read` 工具当前仅面向文本。检测到图片/二进制时的行为未定义（当前表现为读入乱码或截断）。

### 3.2 为什么重要

Desktop 客户端与飞书入口均已落地，"用户发一张 UI 截图说这里坏了"是**必然发生**的场景，
而工具层目前接不住。模型侧（多模态）早已就绪，缺口纯粹在工具层。

### 3.3 设计要点（建议）

- `read` 增加文件类型嗅探：图片 → base64 编码为 image content block 返回
- 二进制（非图片）→ 明确报错并提示用途，而非静默乱码
- 需要与 Provider 层确认多模态消息的传递链路（`sdk/ai/` 消息结构是否已支持 image part）

---

## 4. P2：文档失真修正

`docs/PRODUCT_ROADMAP.md` 第 94 行附近"tools 只有基础 7 件套"的表述已过时：
`sdk/tools/` 实际已有 **20+ 工具**（todo / memory / patch / multiedit / web_fetch / web_search /
ask_user / local_time 等）。

建议同步更新，否则后续回看路线图会系统性低估当前进度、误判优先级。
（本文件刻意不改动 ROADMAP 原文——该修正应由维护者确认后进行。）

---

## 附注：评审中发现但不展开的工程层问题

以下问题在评审中顺带发现，属工程健康度而非功能层，此处仅做索引备忘：

1. **测试污染**：`sdk/config/config_test.go` 的 `TestLoadFromEnv` 会被宿主机 `EA_API_KEY`
   环境变量击穿（`config.go` 中 EA_API_KEY 优先于 OPENAI_API_KEY），建议迁移 `t.Setenv` 并
   用空串显式遮蔽
2. **无 linter 配置**：仅 `go vet`，建议引入 `.golangci.yml`（errcheck / staticcheck 起步）并挂入 CI
3. **serve 模式无 pprof**：排查内存/CPU 问题缺抓手，加 `net/http/pprof` + flag 开关成本极低
4. **JSONL 每次 append 即 `Sync()`**（`sdk/session/jsonl.go:155`）：当前规模无碍，
   高频写入场景可改为可配置
5. **feishu registration 轮询用 `time.Sleep`**：可改 context + ticker 以支持优雅退出
