// Package sdk 是 easyagent 的库入口：Agent 框架的原子能力集合。
//
// easyagent 由两部分组成：本目录（可被外部 Go 模块 import 的 SDK）与
// internal/（easyagent 自身的 Application 与入口，如 coding/music agent、
// TUI、飞书、server）。
//
// # 能力地图
//
//   - ai / ai/providers — 统一 LLM 流式 API 与 Provider 注册制
//   - agent — Agent 状态机与双层循环（外层 follow-up，内层 tool call）
//   - session / sessionmgr — JSONL 树状会话持久化与管理
//   - compaction — 上下文压缩（LLM 摘要 + 保留最近消息）
//   - operations — 本地 / SSH 执行后端抽象
//   - tools — 内置通用工具（read/write/edit/bash/grep/find/ls）
//   - runtime — AgentSession 生命周期与 Application 接口（Platform 层）
//   - slashcmd / skill / extensions / hooks / prompt / config —
//     命令框架、技能、扩展、钩子、提示与配置
//
// # 架构约束
//
// sdk/ 下的包不得 import easyagent/internal/ 的任何包（有测试强制）。
// 领域能力（音乐、飞书等）属于应用层，永远不进 SDK。
//
// # 稳定性
//
// v0 阶段：API 随版本演进，不承诺向后兼容；首个正式消费方是
// easyagent 自身的 Application 层。
package sdk
