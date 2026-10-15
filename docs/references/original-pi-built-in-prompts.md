> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# Pi 内置提示词

本文档整理了 pi 项目中所有的内置提示词，包括系统提示词、提示词模板、压缩总结提示词等。

## 目录

- [1. 主系统提示词](#1-主系统提示词)
- [2. 提示词模板](#2-提示词模板)
- [3. Skills 格式化](#3-skills-格式化)
- [4. 压缩与总结提示词](#4-压缩与总结提示词)
- [5. 分支总结提示词](#5-分支总结提示词)

---

## 1. 主系统提示词

**文件位置**: `packages/coding-agent/src/core/system-prompt.ts`

### 默认系统提示词

```
You are an expert coding assistant operating inside pi, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files.

Available tools:
${toolsList}

In addition to the tools above, you may have access to other custom tools depending on the project.

Guidelines:
${guidelines}

Pi documentation (read only when the user asks about pi itself, its SDK, extensions, themes, skills, or TUI):
- Main documentation: ${readmePath}
- Additional docs: ${docsPath}
- Examples: ${examplesPath} (extensions, custom tools, SDK)
- When asked about: extensions (docs/extensions.md, examples/extensions/), themes (docs/themes.md), skills (docs/skills.md), prompt templates (docs/prompt-templates.md), TUI components (docs/tui.md), keybindings (docs/keybindings.md), SDK integrations (docs/sdk.md), custom providers (docs/custom-provider.md), adding models (docs/models.md), pi packages (docs/packages.md)
- When working on pi topics, read the docs and examples, and follow .md cross-references before implementing
- Always read pi .md files completely and follow links to related docs (e.g., tui.md for TUI API details)

Current date: ${date}
Current working directory: ${promptCwd}
```

### 动态构建的 Guidelines

根据可用工具动态添加：

- `Use bash for file operations like ls, rg, find` (仅有 bash 无 grep/find/ls 时)
- `Prefer grep/find/ls tools over bash for file exploration (faster, respects .gitignore)` (有 bash 且有 grep/find/ls 时)
- `Be concise in your responses` (始终添加)
- `Show file paths clearly when working with files` (始终添加)
- 自定义的 `promptGuidelines` 也会被添加

### 可用工具列表

默认工具: `read`, `bash`, `edit`, `write`

工具以以下格式列出:
```
- read: ${toolSnippets.read}
- bash: ${toolSnippets.bash}
- edit: ${toolSnippets.edit}
- write: ${toolSnippets.write}
```

---

## 2. 提示词模板

**文件位置**: `packages/coding-agent/src/core/prompt-templates.ts`

### 参数替换语法

| 语法 | 说明 |
|------|------|
| `$1`, `$2`, ... | 位置参数 |
| `$@` | 所有参数（空格分隔） |
| `$ARGUMENTS` | 所有参数（空格分隔，新语法） |
| `${@:N}` | 从第 N 个参数开始的所有参数 |
| `${@:N:L}` | 从第 N 个参数开始，共 L 个参数 |

### 模板加载位置

1. **全局模板**: `{agentDir}/prompts/`
2. **项目模板**: `{cwd}/.pi/prompts/`
3. **自定义路径**: 通过配置指定

### 模板文件格式

```markdown
---
description: 模板描述
argument-hint: arg1 arg2
---

模板内容...
```

### 模板调用

模板通过 `/templateName arg1 arg2` 语法调用。

---

## 3. Skills 格式化

**文件位置**: `packages/coding-agent/src/core/skills.ts`

### formatSkillsForPrompt 输出格式

```
The following skills provide specialized instructions for specific tasks.
Use the read tool to load a skill's file when the task matches its description.
When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.

<available_skills>
  <skill>
    <name>${skill.name}</name>
    <description>${skill.description}</description>
    <location>${skill.filePath}</location>
  </skill>
</available_skills>
```

**注意**: `disableModelInvocation=true` 的 skill 不会出现在提示词中。

---

## 4. 压缩与总结提示词

**文件位置**: `packages/agent/src/harness/compaction/compaction.ts`

### SUMMARIZATION_SYSTEM_PROMPT

```
You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.
```

### SUMMARIZATION_PROMPT (初始总结)

```
The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.
```

### UPDATE_SUMMARIZATION_PROMPT (增量更新)

```
The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.
```

### TURN_PREFIX_SUMMARIZATION_PROMPT (大Turn截断)

```
This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.
```

---

## 5. 分支总结提示词

**文件位置**: `packages/agent/src/harness/compaction/branch-summarization.ts`

### BRANCH_SUMMARY_PREAMBLE

```
The user explored a different conversation branch before returning here.
Summary of that exploration:

```

### BRANCH_SUMMARY_PROMPT

```
Create a structured summary of this conversation branch for context when returning later.

Use this EXACT format:

## Goal
[What was the user trying to accomplish in this branch?]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Work that was started but not finished]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [What should happen next to continue this work]

Keep each section concise. Preserve exact file paths, function names, and error messages.
```

### 消息包裹格式

**COMPACTION_SUMMARY_PREFIX**:
```
The conversation history before this point was compacted into the following summary:

<summary>
```

**COMPACTION_SUMMARY_SUFFIX**:
```
</summary>
```

**BRANCH_SUMMARY_PREFIX**:
```
The following is a summary of a branch that this conversation came back from:

<summary>
```

**BRANCH_SUMMARY_SUFFIX**:
```
</summary>
```

---

## 总结

Pi 项目使用结构化的提示词系统：

1. **主系统提示词** 由 `buildSystemPrompt()` 动态构建，包含工具列表、指南和文档路径
2. **提示词模板** 从 `.md` 文件加载，支持参数替换
3. **Skills** 通过 XML 格式嵌入系统提示词
4. **压缩总结** 使用固定格式生成结构化摘要，用于会话历史压缩
5. **分支总结** 用于跨分支上下文恢复

所有提示词都强调：
- 保持简洁
- 保留精确的文件路径、函数名、错误信息
- 使用指定的 EXACT 格式
