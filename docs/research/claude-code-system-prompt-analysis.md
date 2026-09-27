> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# Claude Code 系统提示（System Prompt）设计分析

> 学习日期：2026-05-23
> 来源：Claude Code 官方仓库 (https://github.com/anthropics/claude-code) — plugins 目录
> 目标：分析 CC 的系统提示构建方式，与 EasyAgent 对比，提炼可借鉴的设计思路

---

## 一、概述

Claude Code（以下简称 CC）的**核心源码**是编译到二进制中的，公开仓库的 plugins 目录通过"约定大于配置"的文件系统发现了完整展现了系统提示的设计哲学。

EasyAgent 的系统提示构建在 `internal/prompt/builder.go` 中，是一个**确定性、有序的字符串拼接过程**。而 CC 的系统提示则是**多源、分层、动态注入**的架构。

本文将分析 CC 的三种系统提示构建方式，并与 EasyAgent 做全面对比。

---

## 二、CC 系统提示的三种构建方式

CC 的系统提示构建有三种机制，按"作用域"从大到小排列：

```
系统提示最终内容 = 基础系统提示 + CLAUDE.md + Hook 注入 + SubAgent 替换
```

### 2.1 基础系统提示（内置不可见）

CC 的核心系统提示（"You are Claude Code..."等）是编译在二进制中的，我们无法看到源码。但从插件设计可以推断其内容结构包括：
- 角色定义（你是谁）
- 能力说明（你能做什么）
- 工具使用规则
- 行为约束

### 2.2 Hook 注入：`additionalContext` 机制

这是 CC **最核心的动态注入机制**。通过 Hook 系统在特定事件点向系统提示追加内容。

#### 工作机制

**SessionStart hook** 在会话开始时触发，返回 JSON 中包含 `hookSpecificOutput.additionalContext` 字段，该字段内容会被**追加到系统提示末尾**。

以 **explanatory-output-style** 插件为例（`hooks/hooks.json`）：

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/session-start.sh"
          }
        ]
      }
    ]
  }
}
```

脚本内容（`hooks-handlers/session-start.sh`）通过 stdout 输出 JSON：

```bash
#!/bin/bash
cat << 'EOF'
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "You are in 'explanatory' output style mode..."
  }
}
EOF
```

这个 `additionalContext` 被 CC 的 Agent 循环接收，拼接到系统提示字符串中。

#### 适用场景

| Hook 事件 | 注入时机 | 典型用途 |
|---|---|---|
| `SessionStart` | 会话开始时 | 输出风格、项目特定指令 |
| `UserPromptSubmit` | 用户输入时 | 基于用户问题的动态上下文 |
| `PreToolUse` | 工具执行前 | 工具特定的安全规则 |

#### 其他 Hook 输出字段

除了 `additionalContext`，Hook 还可以输出：
- `systemMessage` — 在对话中插入一条系统消息（不修改 system prompt，而是作为 assistant 消息展示）
- `permissionDecision` — PreToolUse 的 allow/deny/ask 决策
- `updatedInput` — 修改工具输入参数

### 2.3 SubAgent：完整的系统提示替换

当主 Agent 决定启动一个 subagent 时，subagent 的**系统提示完全由 agent markdown 文件决定**。这是最彻底的"替换"而非"追加"。

#### Agent 文件格式

```
agents/code-architect.md
┌─────────────────────────────────────────┐
│ ---                                     │
│ name: code-architect                    │  ← 元数据（frontmatter）
│ description: Designs feature...         │
│ tools: Glob, Grep, LS, Read, WebFetch   │  ← 工具白名单
│ model: sonnet                           │  ← 模型选择
│ color: green                            │  ← UI 颜色
│ ---                                     │
│                                         │
│ You are a senior software architect...  │  ← 系统提示（markdown body）
│                                         │
│ ## Core Responsibilities                │
│ 1. Analyze requirements...              │
│ ...                                     │
└─────────────────────────────────────────┘
```

关键设计点：
- **frontmatter 不是系统提示**，而是元数据（用于发现、触发、UI 渲染）
- **markdown body 才是系统提示**，完全替换主 Agent 的系统提示
- **tools 字段**是工具白名单，控制 subagent 可用的工具集
- **model 字段**控制 subagent 使用什么模型（sonnet/haiku/inherit）

#### Agent 发现与触发

Agent 通过 `description` 字段中的触发条件被主 Agent 发现：

> `description: Use this agent when the user asks to "create an agent", "generate an agent"...`

主 Agent 在循环中判断"这个任务是否适合交给某个 subagent？"，如果匹配则通过 `Agent` 工具启动 subagent。

#### Agent 系统提示编写规范

从 `agent-creator.md` 可以看到 CC 对 agent 系统提示的质量要求：

> - System prompt is comprehensive (500-3,000 words)
> - System prompt has clear structure (role, responsibilities, process, output)
> - Includes concrete examples
> - Has quality control mechanisms

一个高质量的 agent 系统提示包含：
1. **角色与专长** — "You are an elite AI agent architect..."
2. **核心职责** — 编号列表
3. **详细流程** — 分步骤描述
4. **质量标准** — 自我校验机制
5. **输出格式** — 明确的输出结构
6. **边界情况处理** — 异常场景的应对

### 2.4 Skills：条件性上下文注入

Skills 是 CC 的第三类系统提示注入方式，但它**不直接修改系统提示字符串**。

#### Skill 文件结构

```
skills/agent-development/SKILL.md
┌─────────────────────────────────────────┐
│ ---                                     │
│ name: Agent Development                 │  ← 元数据
│ description: This skill should be...    │  ← 触发条件描述
│ version: 0.1.0                          │
│ ---                                     │
│                                         │
│ # Agent Development for CC Plugins      │  ← skill 正文内容
│ ...                                     │
└─────────────────────────────────────────┘
```

#### Skill 的工作方式

1. **元数据始终可见** — `name` 和 `description` 在会话开始时加载到 Agent 的上下文（作为技能列表）
2. **内容按需加载** — 主 Agent 发现用户输入匹配某个 skill 的 `description` 时，**主动读取** skill 文件内容
3. **不是系统提示注入** — Skill 内容不自动拼接到系统提示，而是由 Agent 决定何时读取

这与 EasyAgent 的 skill 设计非常相似。

---

## 三、CC 系统提示构建的完整流程

```
会话开始
    │
    ├── 1. 加载内置系统提示（二进制，不可见）
    │
    ├── 2. 扫描 plugins/ 目录
    │      ├── 发现 agents/ → 注册 agent 元数据
    │      ├── 发现 skills/ → 注册 skill 元数据
    │      └── 发现 hooks/hooks.json → 注册 hook 配置
    │
    ├── 3. 加载用户设置 hooks（.claude/settings.json）
    │
    ├── 4. 执行 SessionStart hooks（并行）
    │      ├── command hooks → 执行脚本，收集 stdout JSON
    │      └── prompt hooks → 调用 LLM 评估
    │      └── 合并所有 additionalContext 到系统提示
    │
    ├── 5. 加载 CLAUDE.md（项目上下文）
    │
    ├── 6. 监听用户输入
    │      ├── 匹配 skill → 通知 Agent 可以读取 skill 文件
    │      ├── 执行 UserPromptSubmit hooks
    │      ├── 匹配 subagent → 启动 subagent（替换系统提示）
    │      └── 执行 PreToolUse hooks（可能注入上下文）
    │
    └── 用户会话结束
         └── 执行 SessionEnd hooks
```

### 系统提示的动态演变

CC 的系统提示是**动态变化**的：

```
时间 → 会话开始 ─────────────────────────────────────→ 会话结束
        │                                                 │
        │ 系统提示 = 基础 + CLAUDE.md + hook注入           │
        │            ↓                                    │
        │         用户输入 → subagent 启动                  │
        │            ↓                                    │
        │         subagent 系统提示 = agent body           │
        │            ↓                                    │
        │         subagent 结束 → 恢复主系统提示            │
        │            ↓                                    │
        │         工具执行 → PreToolUse hook 可能追加       │
        │            ↓                                    │
        │         上下文压缩 → PreCompact hook 保留关键信息  │
```

---

## 四、EasyAgent 现有设计

### 4.1 系统提示构建流程

EasyAgent 的系统提示在 `internal/prompt/builder.go` 中构建，是一个**静态的一次性拼接**过程：

```
buildAgent()
    │
    ├── prompt.BuildSystemPrompt(Options{
    │     CustomPrompt:  cfg.PromptTemplate,   ← 可选覆盖（EA_PROMPT_TEMPLATE）
    │     CWD:           cwd,
    │     Tools:         toolList,             ← 工具列表
    │     ContextFiles:  contextFiles,          ← CLAUDE.md 内容
    │     Skills:        skills,               ← skill 元数据（XML 格式）
    │   })
    │
    └──→ string（最终系统提示，之后不再变化）
              ↓
         agent.New(Options{System: ...})
              ↓
         每次 LLM 调用都使用同一个 System 字符串
```

### 4.2 输出结构（9 层）

```
1. 基础提示（默认或 EA_PROMPT_TEMPLATE）
   └─ "You are EasyAgent, a server-side coding agent..."

2. ## Tool Summary
   └─ 每个工具一行摘要

3. ## Available Tools
   └─ 每个工具的完整描述 + 参数 schema

4. ## Guidelines
   └─ 智能规则 + 工具自定义规则 + 通用规则

5. # Project Context
   └─ CLAUDE.md / AGENTS.md 内容

6. <available_skills>（XML 格式技能列表）
   └─ skill 名称 + 描述 + 路径

7. AppendSystemPrompt（预留，未使用）

8. ---
   Current date: ...
   Current working directory: ...
   Current git branch: ...
```

### 4.3 Skill 的"引用而非注入"设计

EasyAgent 的技能（Skills）是**只引用不注入**的：

```xml
<available_skills>
  <skill>
    <name>graphify</name>
    <description>Convert input to knowledge graph</description>
    <location>/Users/weijian/.claude/skills/graphify/SKILL.md</location>
  </skill>
</available_skills>
```

Agent 看到这个列表后，如果需要使用某个 skill，会主动读取其文件内容。这与 CC 的 skill 机制一致。

---

## 五、对比分析

| 维度 | CC | EasyAgent | 分析 |
|---|---|---|---|
| **构建时机** | 动态（会话过程中持续变化） | 静态（Agent 创建时一次性构建） | CC 更灵活，EasyAgent 更简单可控 |
| **动态注入** | `additionalContext` Hook 输出机制 | 无（AppendSystemPrompt 预留但未用） | **EasyAgent 最大缺失** |
| **SubAgent 系统提示** | 完整替换为 agent markdown body | 无 subagent 系统 | **EasyAgent 第二大缺失** |
| **工具描述注入** | 内置，机制不可见 | 通过 `ToolWithPromptInfo` 接口（Snippet + Guidelines） | EasyAgent 设计更清晰 |
| **CLAUDE.md** | 加载方式不可见，但 plugins 中多处引用 | 自底向上遍历目录，按优先级加载 | EasyAgent 实现更明确 |
| **Skill 注入** | 元数据始终可见，内容按需读取 | 同 CC（XML 列表 + 按需读取） | 两者一致 |
| **基础提示自定义** | 不可见（编译二进制） | 支持 `EA_PROMPT_TEMPLATE` 环境变量覆盖 | EasyAgent 更开放 |
| **运行时信息** | 不可见 | 自动注入日期/CWD/分支 | EasyAgent 更实用 |
| **扩展注入点** | 9 个 Hook 事件点 | `AppendSystemPrompt` 预留字段 | CC 的 Hook 事件更丰富 |
| **Guidelines 生成** | 不可见 | 基于工具组合的智能规则生成 | EasyAgent 有独特优势 |

---

## 六、CC 可借鉴的设计

### 6.1 `additionalContext` 动态注入机制（高优先级）

这是 CC 最实用的系统提示扩展方式，EasyAgent 可以通过已有的 Extension 系统实现。

**CC 的做法：**

```json
// Hook 返回 JSON
{
  "hookSpecificOutput": {
    "additionalContext": "要追加到系统提示的文本内容"
  }
}
```

**EasyAgent 的实现思路：**

EasyAgent 已有 `internal/extensions/types.go` 中的 `Hook` 类型和 `EmitHook` 机制。需要补齐的是：

1. `EmitHook` 返回值中收集 `additionalContext`
2. 在 `agent.Prompt()` / `PromptStream()` 调用时，将收集到的 context 追加到 `llmRequest().System`
3. 或者在 `loop.go` 的 `processTurn()` 中每次调用 LLM 前重新构建 system prompt

关键区别：CC 的 system prompt 是**每次 LLM 调用前都可能变化的**，而 EasyAgent 是固定的。

### 6.2 SubAgent 系统提示替换（高优先级）

CC 的 subagent 在启动时**完全替换系统提示**，这是 subagent 最核心的设计特点。

EasyAgent 当前没有 subagent 概念，如果未来要实现：
- 每个 subagent 有自己的系统提示（markdown body）
- 系统提示与工具白名单、模型选择一起构成 Agent 规格

### 6.3 SessionStart Hook 注入项目上下文（中优先级）

CC 的 `explanatory-output-style` 和 `learning-output-style` 演示了如何通过 SessionStart hook 注入"输出风格"这类贯穿整个会话的指令。

EasyAgent 可以通过以下方式实现类似能力：
- 新增 `SessionStart` 事件类型到 Extension 系统
- 在 `buildAgent()` 中调用 `EmitHook("SessionStart")`
- 将返回的 `additionalContext` 拼接到系统提示

### 6.4 Agent 作为一等公民的设计理念（中优先级）

CC 将 agent 视为**可复用、可发现、可触发**的一等公民：

```
agent 定义 = 元数据 + 系统提示 + 工具白名单 + 模型选择 + UI 配置
```

EasyAgent 当前没有对应的抽象。但值得思考的是：EasyAgent 是否需要 subagent？还是说当前"一个 Agent 实例 + 所有工具"的模式已经足够？

### 6.5 Hook 的 `systemMessage` 机制（低优先级）

CC 的 Hook 还可以输出 `systemMessage`，这不是修改系统提示，而是在对话流中插入一条 assistant 消息。这可以用于：
- Hook 拦截后给出解释
- Tool 执行前后插入指导信息

---

## 七、EasyAgent 的优势与应保持的设计

### 7.1 明确的构建流程

EasyAgent 的 `prompt/builder.go` 只有 ~130 行代码，9 层结构清晰可读。CC 的构建流程分散在 Hook 系统、Agent 系统、Skill 系统中，理解成本更高。

**EasyAgent 应保持**：核心构建逻辑的简单性和可读性。

### 7.2 开放的基础提示自定义

`EA_PROMPT_TEMPLATE` 环境变量允许用户完全替换基础提示。CC 的基础提示是编译在二进制中的，用户无法修改。

**EasyAgent 应保持**：对用户的自定义开放性。

### 7.3 Tool 贡献系统提示

每个 tool 通过 `ToolWithPromptInfo` 接口贡献：
- `PromptSnippet()` — 一句话摘要
- `PromptGuidelines()` — 使用规则

这是 CC 没有的优雅设计。CC 的工具描述可能也是内置的，但从 plugins 中看不到对应机制。

**EasyAgent 应保持**：Tool 自描述系统提示的设计。

### 7.4 智能 Guidelines 生成

`builder.go` 中的 `generateGuidelines()` 根据工具组合自动生成规则，例如：
- 有 grep/find/ls 时 → "优先使用专用工具而非 bash"
- 有 read 和 edit 时 → "先读后改"

这是 CC 没有的智能特性。

**EasyAgent 应保持**：基于上下文感知的规则生成。

---

## 八、建议补齐的优先级

### 第一期：核心差距

| 项目 | 描述 | 工作量估计 |
|---|---|---|
| `additionalContext` 注入 | Extension Hook 支持返回 `additionalContext`，每次 LLM 调用前拼接 | 小（在现有 Hook 系统上扩展） |
| SessionStart 事件 | 在 `buildAgent()` 中新增 SessionStart Hook 事件点 | 小 |

### 第二期：中等价值

| 项目 | 描述 | 工作量估计 |
|---|---|---|
| SubAgent 系统提示 | Agent 规格定义中包含系统提示，subagent 启动时替换 | 中（依赖 subagent 系统实现） |
| 运行时 system prompt 重建 | 每次 LLM 调用前重新构建 system prompt（而非一次性） | 中（影响 loop.go 的核心流程） |

### 第三期：锦上添花

| 项目 | 描述 | 工作量估计 |
|---|---|---|
| Hook `systemMessage` 支持 | Hook 输出 `systemMessage` 插入对话 | 小 |
| PreCompact hook | 上下文压缩前保留关键信息 | 小 |

---

## 九、架构对比图

```
┌─ CC 系统提示架构 ─────────────────────────────────────────┐
│                                                            │
│  编译期固定                                                     │
│  ├── 基础系统提示（二进制内置）                                      │
│  ├── CLAUDE.md 处理逻辑（内置）                                    │
│                                                            │
│  运行时动态                                                     │
│  ├── SessionStart Hook → additionalContext → 追加到系统提示       │
│  ├── UserPromptSubmit Hook → 基于用户输入的动态上下文               │
│  ├── PreToolUse Hook → 工具执行前的 context 注入                  │
│  ├── SubAgent 启动 → 完整替换系统提示                              │
│  ├── PreCompact Hook → 保留关键信息                               │
│  └── 技能按需读取（不修改系统提示）                                    │
│                                                            │
│  特点：动态、灵活、事件驱动                                           │
│  缺点：行为难以预测、调试困难                                          │
└────────────────────────────────────────────────────────────┘

┌─ EasyAgent 当前系统提示架构 ──────────────────────────────────────┐
│                                                              │
│  一次性构建（Agent 创建时）                                         │
│  ├── 基础提示（默认或 EA_PROMPT_TEMPLATE）                       │
│  ├── Tool Summary（每个工具的 PromptSnippet）                      │
│  ├── Available Tools（完整描述）                                   │
│  ├── Guidelines（智能规则 + 自定义规则 + 通用规则）                    │
│  ├── Project Context（CLAUDE.md 内容）                           │
│  ├── Skills（XML 列表，按需读取）                                    │
│  ├── AppendSystemPrompt（预留未用）                                │
│  └── Runtime Info（日期/CWD/分支）                                │
│                                                              │
│  特点：确定、可预测、易于调试                                           │
│  缺点：无动态注入、无 subagent 系统提示替换                              │
└────────────────────────────────────────────────────────────┘

┌─ EasyAgent 建议目标架构 ─────────────────────────────────────────┐
│                                                              │
│  构建时（Agent 创建时一次性构建基础）                                  │
│  ├── 基础提示 + Tool 信息 + Guidelines + Context + Skills        │
│                                                              │
│  运行时（每次 LLM 调用前）                                          │
│  ├── 追加 SessionStart Hook 的 additionalContext                 │
│  ├── 追加当前 Turn 中 Hook 注入的 additionalContext                │
│  └── 如果是 SubAgent → 使用 SubAgent 的系统提示替换                  │
│                                                              │
│  变化：在保持核心静态结构的基础上，增加关键动态注入点                        │
│  原则：80% 静态 + 20% 动态，而非 CC 的全动态                           │
└────────────────────────────────────────────────────────────┘
```

---

## 十、总结

CC 的系统提示设计体现了三个核心哲学：

1. **事件驱动** — 通过 Hook 事件在不同时机注入系统提示内容
2. **组合优于继承** — 系统提示 = 基础 + CLAUDE.md + Hook 注入 + SubAgent 替换，各层独立
3. **约定优于配置** — Agent/Skill 通过文件系统发现和 frontmatter 描述自注册

EasyAgent 当前的静态构建方式更简单、更可预测，但缺少动态注入能力。**建议的策略是"保留 80% 静态核心 + 增加 20% 动态注入点"**，而不是照搬 CC 的全动态架构。这样既获得了灵活性，又不会失去 EasyAgent 当前的可维护性优势。
