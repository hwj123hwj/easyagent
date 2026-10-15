> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# Claude Code 源码分析：EasyAgent 对比与借鉴

> 学习日期：2026-05-22
> 来源：Claude Code 官方仓库 (https://github.com/anthropics/claude-code) — plugins 目录 + hooks 文档
> 目标：分析 CC 哪些设计值得学，哪些是冗余可以避免

---

## 一、概述

Claude Code（以下简称 CC）的**核心源码**是编译到二进制（Bun 打包）中的，GitHub 公开仓库主要包含：

- **Plugins** — 插件生态（commands / agents / skills / hooks）
- **文档与示例** — 配置示例、MDM 管理、hooks 开发指南
- **GitHub 脚本** — issue 自动化

本文基于 plugins 目录和 hooks 文档分析 CC 的设计，并与 EasyAgent 做对比。

---

## 二、CC 值得学习的设计

### 2.1 Hooks 系统

**这是 CC 最核心的差异化设计。** EasyAgent 目前缺失最严重的能力。

#### 事件类型

CC 定义了 9 种 Hook 事件：

| 事件 | 触发时机 | 典型用途 |
|---|---|---|
| `PreToolUse` | 工具执行前 | 校验、审批、修改参数 |
| `PostToolUse` | 工具执行后 | 反馈、日志、质量检查 |
| `Stop` | Agent 要停止时 | 完整性检查（跑测试了吗？） |
| `SubagentStop` | 子 Agent 停止时 | 子任务验证 |
| `UserPromptSubmit` | 用户提交 prompt 时 | 添加上下文、校验输入 |
| `SessionStart` | 会话开始时 | 加载项目上下文、检测环境 |
| `SessionEnd` | 会话结束时 | 清理、日志、状态保存 |
| `PreCompact` | 上下文压缩前 | 保留关键信息 |
| `Notification` | Claude 发送通知时 | 日志、自动响应 |

#### Hook 类型

两种实现方式：

**command hook** — 执行外部脚本（bash / python），通过 stdin/stdout 传 JSON：
```json
{
  "type": "command",
  "command": "bash ${CLAUDE_PLUGIN_ROOT}/scripts/validate.sh",
  "timeout": 10
}
```

**prompt hook** — 用 LLM 做判断（更灵活）：
```json
{
  "type": "prompt",
  "prompt": "Command: $TOOL_INPUT.command. Analyze dangerous ops. Return 'approve' or 'deny'.",
  "timeout": 15
}
```

#### 输入输出协议

所有 hook 通过 stdin 接收 JSON：
```json
{
  "session_id": "abc123",
  "transcript_path": "/path/to/transcript.txt",
  "cwd": "/current/working/dir",
  "permission_mode": "ask|allow",
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": { "command": "rm -rf /" }
}
```

PreToolUse 返回格式（可 approve / deny / ask）：
```json
{
  "hookSpecificOutput": {
    "permissionDecision": "allow|deny|ask",
    "updatedInput": { "field": "modified_value" }
  },
  "systemMessage": "Explanation for Claude"
}
```

Stop hook 返回格式（可 block / approve）：
```json
{
  "decision": "approve|block",
  "reason": "Tests must be run after code changes"
}
```

#### hook 配置格式

插件级（`hooks/hooks.json`）：
```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Write|Edit", "hooks": [
        { "type": "prompt", "prompt": "Validate file write safety" }
      ]}
    ]
  }
}
```

用户设置级（`.claude/settings.json` 嵌入，省掉外层 `hooks:`）：
```json
{
  "PreToolUse": [ ... ]
}
```

> ⚠️ **注意冗余**：同协议两套格式，用户需要在两个上下文切换记忆。

#### 借鉴建议

EasyAgent 要实现 hooks，可以：

1. **定义 Hook 接口**（Go 类型），对应事件类型常量
2. **实现 event bus** — Agent 循环的关键点（tool 执行前后、停止前、session 开始/结束）发射事件
3. **支持两阶段的 hook 注册**：编译期（Go 代码注册）+ 运行期（配置文件发现）
4. **简化嵌套层次** — 避免 CC 的 3 层 hooks 嵌套陷阱

---

### 2.2 SubAgent 系统

CC 的子 agent 用**纯 markdown 文件 + YAML frontmatter** 定义：

```markdown
---
name: code-architect
description: Designs feature architectures...
tools: Glob, Grep, LS, Read, WebFetch, TodoWrite, WebSearch, KillShell, BashOutput
model: sonnet
color: green
---

You are a senior software architect...
```

关键能力：
- **工具白名单** — `tools` 字段限制子 agent 可用工具集
- **模型指定** — `model: sonnet | haiku | inherit`，低成本 agent 用 haiku
- **描述即触发** — `description` 写什么场景触发，由主 Agent 判断
- **并行启动** — 主 agent 可以同时 launch 多个子 agent 做不同任务
- **工具隔离** — 子 agent 没有 Write 权限就不给 Write

#### 借鉴建议

EasyAgent 实现 SubAgent：
1. 定义 Agent 规格（支持的 tool 列表、model、系统 prompt）
2. Agent 循环支持 spawn sub-agent（新 Agent 实例 + 独立 tool 上下文）
3. 用 Go 的 `sync.WaitGroup` 做并行执行
4. 注意：**不需要 markdown frontmatter 那套文件系统发现**，Go 代码注册即可

---

### 2.3 插件发现机制

CC 的插件 = **放在 `plugins/` 目录中的一个文件夹**。

目录结构约定：
```
my-plugin/
├── .claude-plugin/plugin.json    # 名称、版本、作者
├── commands/                      # 斜杠命令（自动发现）
├── agents/                        # 子 agent（自动发现）
├── skills/                        # 技能（自动发现）
├── hooks/hooks.json               # 钩子配置
└── hooks/scripts/                 # 钩子脚本
```

启动时 CC 扫描 `plugins/` 目录，从 `plugin.json` 读取元数据，自动发现 commands / agents / skills / hooks。

#### 借鉴建议

EasyAgent 可以将这套目录约定简化移植：
- 不需要每个插件都强制 `plugin.json` + `README.md`
- 支持 `~/.easyagent/plugins/` 和项目内 `.easyagent/` 两个目录层级
- 文件发现优于代码注册

---

### 2.4 上下文压缩（PreCompact hook）

CC 的 `PreCompact` hook 允许在上下文压缩前注入"保留重要信息"的指令。这个设计很小但很精妙：压缩是由 LLM 做摘要的，`PreCompact` 可以告诉 LLM "请保留 X 信息"，解决了压缩丢失关键上下文的痛点。

EasyAgent 已有 `compaction` 包，只需加一个 PreCompact 回调点即可对齐。

---

## 三、CC 的冗余设计（EasyAgent 应避免）

### 3.1 插件功能重叠

| 问题 | 说明 |
|---|---|
| `code-review` vs `pr-review-toolkit` | 两个插件都做 PR review，agent 重复定义 |
| `explanatory-output-style` vs `learning-output-style` | 目录结构一样，但 learning 额外增加了"交互式用户贡献模式"，功能差异比表面看起来大 |
| `feature-dev` 的 code-reviewer vs `pr-review-toolkit` 的 code-reviewer | 同名 agent，能力重叠 |

**EasyAgent 应该**：一个功能只做一个插件，用参数/配置切换行为，而不是分拆多个。

### 3.2 文档即插件的混淆

`plugin-dev` 插件 **20752 行 markdown**，占据了 plugins 总内容的 **~79%**（全量 26313 行）。它本质是一份开发文档，但因为 CC 没有独立的文档系统，只能把文档包装成插件。

**EasyAgent 应该**：文档放文档目录，插件放插件目录，不混在一起。

### 3.3 Hooks 配置的三层嵌套

```json
{
  "hooks": {                     // 第一层
    "PreToolUse": [              // 第二层
      {                          // 第三层
        "matcher": "Bash",       //   matcher
        "hooks": [               //   第四层
          { "type": "command", "command": "..." }  // 第五层
        ]
      }
    ]
  }
}
```

这个嵌套层级过多。一个最小配置（1 event + 1 matcher + 1 hook）需要写 15 行 JSON。

**EasyAgent 应该**：用扁平化配置。比如 Hook 注册直接是 `(event, matcher, handler)` 三元组，不需要多层包装。

### 3.4 Markdown 硬编码脚本

`clean_gone.md` 等命令文件把完整的 bash 脚本写在 markdown 代码块里，让 LLM 读取和执行：

```markdown
3. Execute this command:
   ```bash
   git branch -v | grep '[gone]' | sed 's/^[+* ]//' | awk '{print $1}' | while read branch; do
     ...
   done
   ```
```

这种方式：
- 每次执行 LLM 都要解析脚本再执行，效率低
- 更新脚本要改 markdown，LLM 还得重新理解

对比 CC 自己的 hookify 插件用 Python 脚本文件 + JSON 协议，差了档次。

**EasyAgent 应该**：保持工具函数注册的方式，不在 prompt 里硬编码可执行脚本。

### 3.5 小插件的架子太重

`frontend-design` 插件只有 72 行（1 个 skill 文件），但也要全套基础设施：
```
.claude-plugin/plugin.json
README.md
skills/frontend-design/SKILL.md
```

`security-guidance` 更极端 — 0 行 markdown，只有 2 个 JSON 配置文件。

**EasyAgent 应该**：允许极简注册方式——一个命令 = 一个文件，不需要元数据文件。

---

## 四、EasyAgent 已有的优势

| 维度 | EasyAgent 优势 |
|---|---|
| **流式事件** | `PromptStream` 的 `AgentStreamEvent` channel 设计比 CC 的事件系统更干净 |
| **双层循环** | `loop.go` 的 `processTurn` / `executeToolCallsParallel` 实现简洁 |
| **类型安全** | Go 编译期类型检查天然优于 JS/TS |
| **Provider 抽象** | `providers/interface.go` 比 CC 的 provider 抽象更精简 |
| **无文档膨胀** | 代码和文档分离，没有 CC 那种 2 万行的"文档插件" |

---

## 五、建议补齐优先级

### 第一期（核心差距）

1. **Hooks 系统**
   - 定义 HookEvent 类型常量
   - 在 Agent 循环关键点发射事件（tool 执行前后、stop 时、session 开始）
   - 支持 command hook（外部脚本）和 inline hook（Go 函数回调）
   - 配置文件驱动（JSON 或纯 Go 注册）

2. **SubAgent 系统**
   - Agent 规格定义（tool 白名单、model 选择、系统 prompt）
   - 从 `go func()` 模式升级到结构化子 Agent 执行
   - 子 Agent 结果合并与评审

### 第二期（中等价值）

3. **技能自动匹配**
   - Skill 描述已集成到 system prompt（`<available_skills>` XML 列表）
   - 待补齐：根据用户输入自动加载相关 skill 内容（当前需 Agent 主动读取）

4. **插件文件系统发现**
   - 支持 `~/.easyagent/plugins/` 目录扫描
   - 目录结构约定（commands/、agents/、skills/）

### 第三期（锦上添花）

5. **新增工具**
   - `BashOutput`（只读 bash，不修改文件系统）
   - `WebSearch` / `WebFetch`
   - `TodoWrite`（结构化任务列表）

6. **权限精细化管理**
   - 工具级别的 ask/deny 配置
   - 禁用绕过权限模式

---

## 六、架构图对比

```
┌─ CC Plugin 架构 ─────────────────────────────────┐
│                                                   │
│  claude-code/plugins/                             │
│  ├── my-plugin/                                   │
│  │   ├── .claude-plugin/plugin.json    ← 元数据    │
│  │   ├── commands/*.md                 ← 命令      │
│  │   ├── agents/*.md                   ← 子Agent   │
│  │   ├── skills/*/SKILL.md             ← 技能      │
│  │   ├── hooks/hooks.json              ← 钩子配置   │
│  │   └── hooks/scripts/                ← 钩子脚本   │
│                                                   │
│  发现方式：文件系统扫描 + plugin.json 驱动          │
│  注册方式：零代码，目录放对位置即可                   │
└───────────────────────────────────────────────────┘

┌─ EasyAgent 当前架构 ──────────────────────────────────┐
│                                                   │
│  EasyAgent/                                            │
│  ├── internal/                                     │
│  │   ├── agent/          ← 核心Agent循环           │
│  │   ├── extensions/     ← 代码接口注册             │
│  │   ├── tools/          ← 内置工具                 │
│  │   ├── skill/          ← Skill接口（已集成到AgentSession）│
│  │   └── ...                                       │
│                                                   │
│  发现方式：Go import + 手工注册                      │
│  扩展方式：需要改代码、重新编译                       │
└───────────────────────────────────────────────────┘

┌─ EasyAgent 目标架构 ──────────────────────────────────┐
│                                                   │
│  EasyAgent/                                            │
│  ├── internal/         ← 核心（不变）               │
│  ├── plugins/          ← 新增：文件系统发现           │
│  │   └── my-plugin/                                │
│  │       ├── plugin.json  （可选，有默认值）          │
│  │       ├── commands/                             │
│  │       ├── agents/                               │
│  │       └── hooks/                                │
│                                                   │
│  hooks 事件注入到 Agent 循环的关键节点                │
│  subagent 从 agent/ 目录自动发现                    │
│  简化版：去掉 CC 的三层嵌套和格式混用                  │
└───────────────────────────────────────────────────┘
```
