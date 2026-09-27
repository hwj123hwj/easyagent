# 飞书接入

`easyagent-bridge` 通过飞书 WebSocket 长连接收取消息，再调用 EasyAgent 核心服务的 HTTP / SSE 接口。对话卡片随生成过程更新。核心服务运行、桥接进程 active、飞书已连接是三个不同状态，需要分别核验。

## 网页配置（托管部署）

按 [迷你主机部署](MINI_DEPLOY.md) 配好 core / bridge 两个 systemd 用户服务。核心配置需要：

```dotenv
EA_API_KEY=your-api-key
EA_FEISHU_ENV_FILE=/home/q/.config/easyagent/feishu.env
FEISHU_OWNER_STATE_FILE=/home/q/.config/easyagent/feishu-owner.json
```

将示例中的用户名与路径换成实际值。`EA_FEISHU_ENV_FILE` 必须对应桥接服务的 EnvironmentFile；桥接使用相同的 owner 状态路径与核心 API 令牌。

在 Web「设置」填写 App ID / App Secret，点击“保存并重启桥接”。Secret 不回显，留空保留现有密钥；首次配置后 App ID 固定。更换机器人需要重新配置使用者绑定，不同应用的 open_id 不能直接复用。

管理 API 即使在本机也要求显式 Bearer 认证。未配置托管路径的部署会显示未托管状态；网页不会替你修改已有 owner 绑定。

## 手动运行

`make build` 构建核心与桥接。先启动核心，再为桥接设置：

```dotenv
FEISHU_APP_ID=cli_your-app-id
FEISHU_APP_SECRET=your-app-secret
PI_AGENT_URL=http://127.0.0.1:8080
EA_API_KEY=your-api-key
# 可选：显式配置已授权账号，否则使用首次配对。
# FEISHU_OWNER_OPEN_ID=ou_your-open-id
# FEISHU_OWNER_STATE_FILE=/absolute/path/feishu-owner.json
```

将这些值放入专用文件后，用 `EA_ENV_FILE=/absolute/path/feishu.env ./bin/easyagent-bridge` 启动。也可通过交互入口 `/feishu setup` 配置凭据。飞书应用侧需启用机器人能力、相应消息权限及长连接事件订阅，并发布可用版本；配置信息以实际应用控制台为准。

## 首次配对

未预置 owner 时，在网页设置按需查看私聊配对指令，**只私聊目标机器人**发送。配对码单次有效且有截止时间，不要把旧截图或文档中的码作为当前凭据。配对前不执行开发指令；完成后只接受绑定账号的操作，配对状态独立持久化。

如果已经配对，刷新设置页确认状态。不要为解决回复问题删除配对文件或关闭身份校验。

## 排错

| 现象 | 检查方向 |
|---|---|
| bridge active 但机器人无响应 | 检查飞书 WebSocket 连接日志、应用发布状态、消息事件与权限 |
| 提示未授权 / 无法配对 | 是否私聊目标应用、是否为当前有效配对码、core 与 bridge 是否使用同一 owner 文件 |
| 配对成功但对话请求失败 | `PI_AGENT_URL` 是否可达、`EA_API_KEY` 是否与核心一致、模型服务是否正常 |
| 卡片只有状态或文字很慢 | 分别检查核心 SSE 首个文本事件、工具耗时和飞书卡片更新；不能只按总耗时判断模型卡顿 |
| 富文本消息无法解析 | 保留消息结构和错误日志用于回归测试；避免在日志中泄漏凭据或私聊内容 |

文本与富文本由桥接解析；无法支持的消息应返回明确说明，不将解析失败占位文本当成用户任务。流式卡片首次内容尽早发送，后续合并短时间内更新，结束时写入最终状态。卡片显示“已完成”不应替代真实回答。

```bash
systemctl --user status easyagent-core.service easyagent-bridge.service
journalctl --user -u easyagent-bridge.service -n 80 --no-pager
curl -fsS http://127.0.0.1:8080/health
```

自启动需 enable 两个用户服务，并为运行账号启用 linger；具体安装与更新步骤见 [部署运维](MINI_DEPLOY.md)。
