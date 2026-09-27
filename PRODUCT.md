# EasyAgent Web

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

项目维护者在自己的 Mac 和迷你主机之间开发、使用 Agent，日常操作以对话、工具执行查看和历史会话为核心（2026-09-27 用户确认）。

## Product Purpose

用一个浏览器工作区发起 Agent 任务、阅读回复和工具结果，并管理历史会话与独立流水线。

## Operating Context

迷你主机作为日常正式入口，Mac 用于开发预览。Go 服务嵌入原生 HTML/CSS/JavaScript 静态资源；HTTP API 和 WebSocket 提供会话、流式回复、工具事件与流水线。

## Capabilities and Constraints

保留已有会话、模型、鉴权、流水线功能及 API；界面优化不放宽服务器工具权限。发布流程需要验证、重启、健康检查和失败回滚，保留运行数据和配置。

## Brand Commitments

名称 EasyAgent；用户明确要求保持青夜风格。继承已有深色表面和青绿色交互强调，重新整理布局与交互细节。

## Product Principles

- 对话和可追溯的执行过程为中心。
- 状态与失败可见，输入草稿和历史不能静默丢失。
- 手机和桌面都能完整操作，重要操作支持键盘。
- 运行配置与源码更新分离，部署失败保留上一可用版本。
