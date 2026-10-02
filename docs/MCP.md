# MCP 工具

MCP 工具与内置工具使用同一条 Agent 执行路径，保留工具开始、进度、结果、错误与确认事件。公共实现位于 `sdk/mcp`，传输使用官方 MCP Go SDK。支持 stdio、Streamable HTTP 和旧式 SSE；当前提供工具能力，不提供 MCP 资源浏览器或 prompts 选择器。

## 配置与作用域

用户配置默认为 `~/.easyagent/mcp.json`，可由 `EA_HOME`、`EA_MCP_CONFIG`、`--mcp-config` 或 YAML 的 `mcp_config` 改变。项目配置位于 `<workspace>/.easyagent/mcp.json`，必须先在设置中明确授权该工作区才会读取、连接或启动其中的进程。

两种配置均使用 `mcpServers`：

```json
{
  "mcpServers": {
    "local-tools": {
      "command": "/absolute/path/to/mcp-server",
      "args": [],
      "env": {"SERVICE_TOKEN": "${SERVICE_TOKEN}"},
      "trust": false
    },
    "remote-tools": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {"Authorization": "Bearer ${REMOTE_MCP_TOKEN}"},
      "timeout": 30000,
      "excludeTools": ["delete_*"]
    }
  }
}
```

将占位地址与进程替换为自己的服务器；环境变量需存在于运行 EasyAgent 的主机进程环境中。`env`、`headers` 和 OAuth client secret 支持 `${VAR}`，未定义时报错。`command` / `cwd` 支持 `~`，`args` 保持原样，不经过 shell。`timeout` 单位为毫秒，`enabled` 默认 true。

已授权的项目中，同名项目配置覆盖用户配置。设置显示每台服务的来源和作用域；编辑被遮盖的用户定义不会改变当前生效的项目定义。无效条目单独报错，不妨碍其他服务连接。

远程桌面连接到迷你主机时，stdio 进程、路径、环境与配置都属于迷你主机。SSH 文件执行后端也不会自动把 MCP 服务搬到 SSH 目标；MCP 在 EasyAgent 服务所在主机连接。

## 使用与审批

Web「设置」和桌面「设置 → MCP 工具」支持添加服务、授权项目、启停、信任、重连、刷新及逐工具开关。TUI / 服务端命令入口为：

```text
/mcp list
/mcp reconnect <服务名>
/mcp refresh <服务名>
/mcp enable <服务名>
/mcp disable <服务名>
/mcp trust <服务名>
/mcp untrust <服务名>
/mcp project trust
/mcp project untrust
```

`includeTools` / `excludeTools` 使用 glob 匹配原始工具名；排除规则优先。工具在模型侧通常命名为 `mcp__服务名__工具名`，超长名称使用稳定短名。服务器工具列表变化会刷新状态，下一次任务使用新工具集。

项目授权控制是否加载项目定义；服务的 `trust` 控制是否每次调用需要批准。两者独立。未信任工具在 Web / 桌面显示审批，超时拒绝；SDK 未提供确认回调时拒绝调用。`/confirm off` 不绕过这一要求，明确设置 `-y` / `EA_AUTO_APPROVE=true` 会批准。信任服务前也需判断服务本身的行为；内置文件工具的工作区限制不自动约束外部 MCP 工具。

启动先显示界面，MCP 在后台连接。未就绪或失败的服务不会阻塞其他能力；设置显示连接状态与故障。活动任务固定使用开始时的工具集，配置编辑在存在活动任务时返回 409，避免中途关闭调用所需的连接。

## OAuth 与凭据

HTTP 服务可配置 `oauth`，支持发现授权服务器、PKCE、动态注册及令牌刷新；需要固定客户端时填写 `clientId`、可选 `clientSecret` 和 `scopes`。授权服务器须支持所选客户端与回调方式。

桌面在主进程启动临时 loopback 回调并打开系统浏览器。Web 使用当前页面的回调地址，需要浏览器所在地址满足 OAuth 的 HTTPS 要求（本机回环地址可用 HTTP）；纯 HTTP 局域网入口应改用桌面或 HTTPS。SDK 调用者通过 `BeginLogin` / `CompleteLogin` 提供回调。state 与一次性 PKCE 校验后才保存令牌。

配置与 `mcp-auth.json` 以私有权限原子保存；管理 API 不回传 headers、env 或令牌。编辑时省略秘密字段保留原值，显式空对象或 null 清除对应字段。运行凭据不要提交到 Git；其他项目需自行忽略 `.easyagent/mcp.json` 或使用不含秘密的模板。

同一进程内多个工作区的凭据更新会合并保存；多个独立进程同时管理 OAuth 时应使用独立配置/凭据目录，当前文件保存没有跨进程事务锁。工具结果保留外部服务返回的完整内容，服务自身回传的秘密不会自动脱敏。

当前 OAuth 协议通过模拟授权服务器验证，不表示所有第三方服务均已实测。普通工具列表与调用已覆盖真实 SDK HTTP、SSE 和 stdio 测试。
