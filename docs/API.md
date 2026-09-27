# HTTP API 参考

> EasyAgent Server 模式（`easyagent serve`）的完整 API 文档。

---

## 快速开始

```bash
easyagent serve
# Server running on http://127.0.0.1:8080
```

### 认证

三级访问控制模型（`/health` 始终开放）：

1. **设置了 `EA_API_KEY`** → 所有请求必须带 `Authorization: Bearer <key>`；WebSocket 用 `?token=<key>` 查询参数。
2. **未设置（默认）** → 仅放行 loopback 来源；非回环请求返回 401。本机消费方（网页 UI、飞书 bridge、桌面端）零配置可用。
3. **`EA_ALLOW_NO_AUTH=1`** → 完全开放（仅限本机调试，切勿暴露端口）。

CORS 默认不返回跨域头；需要浏览器跨域访问时配置 `EA_ALLOWED_ORIGINS`（逗号分隔白名单）。

```bash
curl -H "Authorization: Bearer $EA_API_KEY" http://127.0.0.1:8080/health
```

---

## 对话

### 同步对话

```
POST /chat
```

```json
{
  "message": "帮我看看这个项目结构",
  "session_id": "sess_123"
}
```

### SSE 流式对话

```
POST /chat/stream
```

Server-Sent Events 流式输出，实时返回 AI 回复的每个 token。

```bash
curl -N -X POST http://127.0.0.1:8080/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"message": "你好"}'
```

### WebSocket

```
GET /ws
```

全双工 WebSocket 连接，支持对话、模型切换、流式回复。

---

## 会话管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/sessions` | 列出所有会话 |
| `POST` | `/sessions` | 创建新会话 |
| `GET` | `/sessions/{id}/messages` | 获取会话消息 |
| `GET` | `/sessions/{id}/info` | 获取会话信息 |
| `DELETE` | `/sessions/{id}` | 删除会话 |
| `POST` | `/sessions/{id}/model` | 切换会话模型 |
| `POST` | `/sessions/{id}/compact` | 压缩会话上下文 |
| `POST` | `/sessions/{id}/command` | 执行斜杠命令 |
| `GET` | `/sessions/{id}/diff` | 获取会话 Git diff |
| `GET` | `/sessions/{id}/file` | 获取会话文件内容 |

### 切换模型

```
POST /sessions/{id}/model
```

```json
{
  "model": "gpt-4o",
  "provider": "openai"
}
```

---

## 模型与工具

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/models` | 列出可用模型 |
| `GET` | `/tools` | 列出已注册工具 |
| `POST` | `/tools/register` | 注册外部工具 |
| `GET` | `/applications` | 列出可用应用 |

---

## 知识库

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/kb/stats` | 知识库统计 |
| `GET` | `/kb/entries` | 列出条目 |
| `GET` | `/kb/categories` | 分类列表 |
| `GET` | `/kb/tags` | 标签列表 |
| `GET` | `/kb/health` | 健康报告 |
| `GET` | `/kb/read` | 读取条目内容 |

---

## 用户画像

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/profile` | 获取用户画像（所有分类+摘要） |
| `DELETE` | `/profile` | 删除指定画像条目 |

---

## 文件操作

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/workspace/list-dir` | 列出工作目录内容 |
| `GET` | `/workspace/search-files` | 模糊搜索文件 |
| `GET` | `/workspace/read-file` | 读取文件内容 |
| `PUT` | `/workspace/write-file` | 写入文件内容 |
---

## 工作流

多步骤 Agent 编排（DAG、fan-out、重试、人工确认门、步骤缓存）。YAML 规范与模板语法见 [WORKFLOW.md](WORKFLOW.md)。

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/workflows` | 提交工作流并启动运行（body 为 YAML 原文，或 `{"yaml":"...","vars":{...}}`），返回 `run_id` |
| `GET` | `/workflows` | 列出全部运行（活跃 + 历史） |
| `GET` | `/workflows/{id}` | 运行详情：`meta`（状态、各步骤、产出）+ `events`（journal） |
| `POST` | `/workflows/{id}/cancel` | 取消运行；也用于清理重启后残留的孤儿运行 |
| `POST` | `/workflows/{id}/approve` | 放行确认门（body 可选 `{"step":"..."}`，省略时须只有一个等待门） |
| `POST` | `/workflows/{id}/reject` | 拒绝确认门，运行终态 `rejected` |

```bash
curl -s -X POST http://localhost:8080/workflows -H 'Content-Type: text/yaml' --data-binary @workflow.yaml
# 202 {"run_id":"wf-1790344371-2333d68c","status":"running"}
curl -s http://localhost:8080/workflows/wf-1790344371-2333d68c | jq .meta.status
```

---

## Web 控制台

serve 模式内嵌网页控制台（浏览器打开 `http://<host>:<port>/`），无需独立前端：

- **对话**：原有聊天界面（WebSocket 流式 + 会话侧栏 + 模型切换）。
- **工作流**：YAML 提交运行、运行列表与详情（步骤状态、产出、事件流、确认门批准/拒绝/取消）。运行中每 1.5s 轮询详情，列表每 5s 刷新。
- **会话**：会话列表、消息回看（含工具调用）、删除。

需要令牌时（非本机访问或已配置 `EA_API_KEY`），页面会弹出登录框，令牌保存在浏览器 localStorage；WebSocket 连接自动附带 `?token=`。

---

## 健康检查

```
GET /health
```

```json
{
  "status": "ok"
}
```


## 动态工作流（ZCode 引擎）

沿用 API 认证。创建由对话中的 `/workflow <任务>` 和 `create_workflow` 工具完成，Actor 继承父会话配置。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/dynamic-workflows` | `runs` 摘要，以及运行组件 `available` / `error` |
| GET | `/dynamic-workflows/{id}` | 状态、脚本、父会话、Actor 会话映射、节点快照与最终结果 |
| POST | `/dynamic-workflows/{id}/cancel` | 取消运行，成功返回 202 |
| POST | `/dynamic-workflows/{id}/resume` | body 必须为 `{"acknowledge_incomplete_actions":true}`；成功返回 202 及运行记录 |

取消/恢复状态冲突返回 409，详情不存在返回 404。恢复重复使用已完成结果，未完成 ask 的外部操作可能重复执行。运行中的 Actor 会话拒绝外部聊天、命令、删除、压缩和模型修改请求（409）。协议详情与限制见 [DYNAMIC_WORKFLOW.md](DYNAMIC_WORKFLOW.md)。
