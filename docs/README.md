# EasyAgent 文档中心

> 本目录是 EasyAgent 项目的所有文档入口。按文档性质分目录组织，方便不同角色（调研/计划/审核/执行 agent）快速定位。

---

## 目录结构

```
docs/
├── README.md                          ← 你在这里
├── PROJECT_CONTEXT.md                 # 项目上下文快照
├── PRODUCT_ROADMAP.md                 # 产品路线图
├── CONTRIBUTING.md                    # 贡献指南
│
├── references/                        # 稳定查阅资料（接口/集成/项目快照）
│   ├── feishu-integration-ref.md      # 飞书接入参考
│   ├── original-pi-built-in-prompts.md # 原始 Pi 内置提示词
│   ├── netease-recommendation-apis.md # 网易云推荐 API 参考
│   └── vibecheck-api-analysis.md      # VibeCheck API 分析
│
├── decisions/                         # 当前采纳判断（会演进，但比 research 更接近团队共识）
│   ├── skills-vs-application.md
│   ├── personal-assistant-roadmap.md
│   ├── goal-compact-cross-framework.md
│   ├── manual-compaction-design-analysis.md
│   └── deepvcode-essence-absorption.md
│
├── research/                          # 外部项目调研原始报告（天然会过期）
│   ├── oh-my-pi-full-analysis.md
│   ├── claude-code-plugins-hooks-analysis.md
│   ├── claude-code-system-prompt-analysis.md
│   ├── cc-haha-architecture-analysis.md
│   ├── cc-haha-core-engine-analysis.md
│   ├── codex-rust-cli-analysis.md
│   ├── deepv-code-full-analysis.md
│   └── competitive-research.md        # (archived)
│
└── archive/                           # 已完成的开发主题
    ├── goal-driven-loop/              # Goal 驱动循环（已完成）
    ├── coding-agent/                  # Coding Agent 规格（已完成）
    ├── coding-agent-slash-hardening/  # Slash 命令补强（已完成）
    ├── coding-agent-cli-control-plane/ # CLI 控制面（已完成）
    ├── feishu-bridge/                 # 飞书桥接基础版（已完成）
    ├── layering/                      # 分层架构提案（已完成）
    ├── layering-refactor/             # 分层深度重构（已完成，四层架构 + Application 层落地）
    ├── second-agent-validation/       # 第二个 Agent 验证（已完成，由 music agent 真实验证）
    ├── parallel/                      # 并行工具执行（已完成）
    ├── cli-tui/                       # CLI/TUI（已完成）
    ├── runtime-decoupling/            # Runtime 解耦（已完成）
    ├── ssh-operations/                # SSH 远程执行（已完成）
    ├── tool-lifecycle/                # Tool Lifecycle（已完成）
    ├── desktop-golang/                # Desktop Go 实现
    ├── learning-notes/                # 早期学习笔记
    ├── skills-support/                # Skills 完整支持（已落地：skill/ 包）
    ├── web-access/                    # Web 工具（已落地：web_fetch/web_search）
    ├── tui-bubbletea.md               # TUI 设计（已落地：tui/ 包）
    ├── llm-wiki-integration.md        # llm-wiki 集成设计（已落地：/wiki 命令）
    ├── pi-go-analysis.md              # EasyAgent 深度分析（2026-05-25 历史快照）
    ├── code-review-suggestions.md     # 全面代码审查建议（历史快照）
    ├── easycode-memories.md           # EasyCode agent 记忆（误置于根目录，存档）
    ├── project-overview.md            # 项目概览（历史快照）
    └── code-review-issues.md          # 代码评审问题清单
```

---

## 长期文档

这些文档始终位于 `docs/` 根目录，持续维护：

| 文档 | 用途 |
|------|------|
| [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) | 项目架构、核心能力、技术栈快照。调研 agent 和维护 agent 的对比基准 |
| [PRODUCT_ROADMAP.md](PRODUCT_ROADMAP.md) | 产品与技术主路线图 |
| [CONTRIBUTING.md](CONTRIBUTING.md) | 贡献和开发协作说明 |

## 参考资料

`references/` 下放的是相对稳定的查阅资料。它们可以过时，但定位不是"当前决策"，而是"查这个主题时先看什么"。

| 文档 | 用途 |
|------|------|
| [feishu-integration-ref.md](references/feishu-integration-ref.md) | 飞书 Bot 接入参考 |
| [original-pi-built-in-prompts.md](references/original-pi-built-in-prompts.md) | 原始 Pi 项目内置提示词完整整理 |
| [netease-recommendation-apis.md](references/netease-recommendation-apis.md) | 网易云推荐 API 参考 |
| [vibecheck-api-analysis.md](references/vibecheck-api-analysis.md) | VibeCheck API 分析 |

## 决策文档

`decisions/` 下放的是"基于调研 + 当前项目状态"得出的阶段性结论。它们会随着代码演进而更新，角色上介于 `research/` 和 `dev/` 之间：

- 比 `research/` 更偏当前项目判断
- 比 `dev/` 更偏方向与原则，不直接充当执行计划

| 文档 | 用途 |
|------|------|
| [skills-vs-application.md](decisions/skills-vs-application.md) | 什么时候用 Skills，什么时候拆成独立 Application |
| [personal-assistant-roadmap.md](decisions/personal-assistant-roadmap.md) | EasyAgent 从编程助手演进为通用个人助手：多 agent + 记忆层（OpenViking）路线 |
| [goal-compact-cross-framework.md](decisions/goal-compact-cross-framework.md) | `/goal` 与 `/compact` 的跨框架对比与采纳建议 |
| [manual-compaction-design-analysis.md](decisions/manual-compaction-design-analysis.md) | `/compact` 手动上下文治理设计收敛文档 |
| [deepvcode-essence-absorption.md](decisions/deepvcode-essence-absorption.md) | 基于 DeepV 对比得出的阶段性增强建议 |

## 调研报告

`research/` 下是对外部项目的调研原始报告。这些文档天然带时间戳，通常会过期；它们的作用是提供证据，而不是直接代表当前团队结论。

| 文档 | 内容 |
|------|------|
| [oh-my-pi-full-analysis.md](research/oh-my-pi-full-analysis.md) | oh-my-pi (omp) 全面分析：32 工具、Native Rust、TTSR、Subagent 等 |
| [claude-code-plugins-hooks-analysis.md](research/claude-code-plugins-hooks-analysis.md) | Claude Code 插件生态、Hooks 系统与 EasyAgent 对比借鉴 |
| [claude-code-system-prompt-analysis.md](research/claude-code-system-prompt-analysis.md) | Claude Code 系统提示构建方式与 EasyAgent 对比 |
| [cc-haha-architecture-analysis.md](research/cc-haha-architecture-analysis.md) | cc-haha 桌面端架构与功能全景分析 |
| [cc-haha-core-engine-analysis.md](research/cc-haha-core-engine-analysis.md) | cc-haha Agent 循环、Tool 系统、核心引擎源码深度分析 |
| [cc-haha-web-fetch-analysis.md](research/cc-haha-web-fetch-analysis.md) | cc-haha(=Claude Code 官方) WebFetch 源码级分析，EasyAgent web_fetch 实现参照 |
| [codex-rust-cli-analysis.md](research/codex-rust-cli-analysis.md) | Codex Rust CLI 详细调研 |
| [deepv-code-full-analysis.md](research/deepv-code-full-analysis.md) | DeepV Code 全面分析 |
| [easyagent-feature-review-2026-10.md](research/easyagent-feature-review-2026-10.md) | 功能体验评审：Agent 本体第一人称报告（进程管理/子 agent/多模态 read） |
| [pi-go-enhancement-status.md](research/pi-go-enhancement-status.md) | EasyAgent P0-P2 增强落地状态（确认机制/循环检测/Hook/web_fetch/MicroCompact 的 commit 与 PR 索引） |
| ~~competitive-research.md~~ | *(已归档)* 阶段性竞品对比分析 |

## 开发文档

`dev/` 下按主题分目录，每个目录对应一个**活跃的**开发主题。已完成的主题移入 `archive/`。

每个目录包含该主题的全生命周期文档：

```
dev/{topic}/
├── proposal.md          # 提案（plan-agent 产出）
├── review.md            # 审核（review-agent 产出）
└── execution-plan.md    # 执行计划（确认后，exec-agent 使用）
```

每个 `dev/` 文档都有 YAML 元信息头，标注状态和角色：

```yaml
---
status: draft | reviewed | approved | done
author: plan-agent | review-agent | exec-agent | research-agent
created: YYYY-MM-DD
updated: YYYY-MM-DD
---
```

**状态流转**：`draft → reviewed → approved → done`（rejected 回到 draft）

### 当前活跃主题

（无。`dev/` 已清空——skills-support 与 web-access 均已落地，按维护规则归档；
新主题由 plan-agent 重新创建。）

## 归档

`archive/` 下是已完成的开发主题。整个主题目录从 `dev/` 移入，保留完整上下文。

| 归档主题 | 内容 |
|---------|------|
| [feishu-integration/](archive/feishu-integration/proposal.md) | 飞书完整集成：外部工具注册、CardKit 流式卡片、项目群管理（已完成） |
| [goal-driven-loop/](archive/goal-driven-loop/proposal.md) | Goal 驱动 Agent 循环 + LLM 评估器（已完成） |
| [coding-agent/](archive/coding-agent/spec.md) | Coding Agent 应用层规格（已完成） |
| [coding-agent-slash-hardening/](archive/coding-agent-slash-hardening/execution-plan.md) | Slash 命令第二阶段补强（已完成） |
| [coding-agent-cli-control-plane/](archive/coding-agent-cli-control-plane/execution-plan.md) | Coding Agent CLI 控制面（已完成） |
| [feishu-bridge/](archive/feishu-bridge/execution-plan.md) | 飞书桥接基础版（已完成） |
| [layering/](archive/layering/proposal.md) | 分层架构提案（已完成，演进为 layering-refactor） |
| [layering-refactor/](archive/layering-refactor/proposal.md) | 分层深度重构：四层架构（Core/Platform/Application/Entrypoints）落地，coding-agent 语义抽到 Application 层（已完成） |
| [second-agent-validation/](archive/second-agent-validation/execution-plan.md) | 第二个 Agent 验证预案；其目标（验证分层能承载多 agent）已由 music agent 真实落地验证（已完成） |
| [parallel/](archive/parallel/execution-plan.md) | 并行工具执行（已完成） |
| [cli-tui/](archive/cli-tui/execution-plan.md) | CLI/TUI 执行计划（已完成，方向已明确为 CLI） |
| [runtime-decoupling/](archive/runtime-decoupling/execution-plan.md) | Runtime 解耦执行计划（已完成） |
| [ssh-operations/](archive/ssh-operations/execution-plan.md) | SSH 远程执行计划（已完成） |
| [tool-lifecycle/](archive/tool-lifecycle/execution-plan.md) | Tool Lifecycle 执行计划（已完成） |
| [desktop-golang/](archive/desktop-golang/changes.md) | Desktop Go 实现变更说明 |
| [learning-notes/](archive/learning-notes/01-agent-framework-extensibility.md) | 早期学习笔记归档 |
| [skills-support/](archive/skills-support/proposal.md) | Skills 完整支持提案（已落地：skill/ 包 + 命令集） |
| [web-access/](archive/web-access/spec.md) | Web 工具规格（已落地：sdk/tools/web_fetch、web_search） |
| [tui-bubbletea.md](archive/tui-bubbletea.md) | TUI Bubble Tea 设计（已落地：internal/tui/） |
| [llm-wiki-integration.md](archive/llm-wiki-integration.md) | llm-wiki 集成设计（已落地：/wiki 命令） |
| [pi-go-analysis.md](archive/pi-go-analysis.md) | EasyAgent 深度分析（2026-05-25 快照，当时 65 个源文件） |
| [code-review-suggestions.md](archive/code-review-suggestions.md) | 全面代码审查建议（历史快照，路径为重构前） |
| [easycode-memories.md](archive/easycode-memories.md) | EasyCode agent 记忆文件（原误置于仓库根目录） |
| [project-overview.md](archive/project-overview.md) | 项目概览（历史快照） |
| [code-review-issues.md](archive/code-review-issues.md) | 代码评审问题清单 |

---

## 维护规则

- **docs-maintainer skill** 负责定期同步文档与代码的一致性
- **research skill** 负责生成 `research/` 下的调研报告
- 跨项目调研得出的"当前采纳判断"优先放入 `decisions/`
- 新开发主题由 plan-agent 在 `dev/` 下创建目录
- 完成的主题整体移入 `archive/`
- 功能已合并到 main 的主题应及时归档，不要留在 `dev/`

### docs 与 `.llm-wiki` 的分工（重要，避免重复维护）

项目有两套文档，职责严格不重叠：

| | `docs/` | `.llm-wiki/` |
|---|---------|--------------|
| **回答** | 为什么这么做、将来怎么走、怎么协作 | 代码现在是什么样 |
| **内容** | 决策、调研、方向、规范 | 实体、概念、能力的客观描述 |
| **谁写** | 人（主观意图） | 工具自动 ingest（客观事实） |
| **举例** | personal-assistant-roadmap（为什么做记忆层）、CONTRIBUTING（怎么提 PR） | music-agent.md（6 个工具、SourceRouter 怎么路由） |

**原则：写 `docs/` 时不写"代码现在长什么样"。** 问"X 实现在哪、怎么工作"查 `.llm-wiki`（结构化、自动、最新）；`docs/` 只承载人才能产生的判断——为什么、往哪去、该怎么。两处描述同一代码事实，必然不同步，是文档腐化的头号来源。

> 注：`PROJECT_CONTEXT.md` 的能力表/文件速查与 `.llm-wiki/overview.md` 有历史重叠，作为过渡期保留；新内容优先沉淀到 `.llm-wiki`，`docs/` 侧不再扩充代码事实。
