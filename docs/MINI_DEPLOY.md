# 迷你主机更新

正式网页由 `q@192.168.5.16` 的用户级 `easyagent-core.service` 提供，入口 `http://192.168.5.16:8080`。Mac 用于开发预览。

合并到 GitHub `main` 后，用户级 `easyagent-update.timer` 每 5 分钟检查一次（加 0–30 秒随机延迟）。更新器按 SHA 获取源码，在独立构建目录测试根模块和 Bubble Tea、运行 vet 与网页脚本测试、构建 CLI、bridge 和动态工作流 bundle；成功后向认证的 `/admin/deploy` 获取空闲租约，再原子替换服务程序并重启。正在执行的对话、循环任务、动态工作流及等待审批的 YAML 流水线都会让本轮更新推迟；下次 timer 再检查，不取消任务。取得租约后短暂拒绝新执行请求，避免空闲检查与新任务之间的竞态。健康检查必须返回对应提交版本，失败自动恢复旧程序并重启，失败版本不重复部署，后续新提交仍可更新。源码开发副本、运行配置、会话数据不被覆盖。

## 安装（以运行服务的用户执行）

前提：已配置并启动 `easyagent-core.service`，已安装 Go、Node.js 22+、curl、tar、Python 3、flock。更新器通过 GitHub API 获取 main 的 SHA，并下载该 SHA 的源码包（不依赖 GitHub Git 协议连通性）；不要把服务令牌或网关凭据写入仓库。

```bash
mkdir -p ~/.local/bin ~/.config/systemd/user ~/.config/easyagent
install -m 755 scripts/update-mini.sh ~/.local/bin/easyagent-update
cp deploy/systemd/easyagent-update.* ~/.config/systemd/user/
# 首次安装才复制，已有配置不要覆盖：
cp -n deploy/mini.env.example ~/.config/easyagent/deploy.env
systemctl --user daemon-reload
systemctl --user start easyagent-update.service
systemctl --user enable --now easyagent-update.timer
```

按实际主机修改 deploy.env 中 Go 路径和健康检查地址。配置 `EA_DEPLOY_BRIDGE_BIN` 后，桥接程序与主程序一起替换、重启和回滚，并检查桥接服务处于 active；没有桥接服务的主机省略该变量。桥接使用长连接，现有服务退出最多可能等待 90 秒。保持用户 linger 开启，使服务在 SSH 退出后继续运行。更新器无需 GitHub Runner；不执行 PR 分支，不开放远程命令入口。生产服务已有的工具权限和网络监听设置保持不变。

主服务、桥接和动态工作流 bundle 应来自同一发布 SHA。迷你主机配置 `EA_DEPLOY_REQUIRE_PATH_POLICY=true` 和 `EA_DEPLOY_REQUIRE_OWNER_ACCESS=true`，更新器在替换前检查核心二进制的路径开关及桥接身份校验符号，避免旧分支覆盖已启用的能力；这些兼容性检查不能代替权限行为测试。现有配对状态和 API 令牌保持在运行配置目录，不随发布重建。

开发权限在 `core.env` 中独立设置 `EA_ALLOW_OUTSIDE_WORKSPACE=true`。默认仍限制文件工具在工作区内；开关与 `EA_AUTO_APPROVE`、API 认证独立，仍受运行用户的操作系统权限限制。可通过认证后的 `/sessions/{id}/info` 的 `allow_outside_workspace` 字段核验实际会话策略。

## 运维

```bash
systemctl --user start easyagent-update.service  # 立即检查
systemctl --user status easyagent-core.service easyagent-update.timer
journalctl --user -u easyagent-update.service -n 80 --no-pager
curl -fsS http://192.168.5.16:8080/health
systemctl --user stop easyagent-update.timer     # 暂停自动更新
```

`~/.local/share/easyagent-deploy/current-revision` 记录健康版本；`previous-binary` 是最近部署前的可执行文件；`releases/<SHA>/easyagent` 保存已构建版本。健康失败的 SHA 记在 `failed-revision`，修复环境后删除该标记再手动启动更新服务即可重试。

手工回滚：先停 timer，停止相关服务，从同一已验证 SHA 的 release 同时恢复 core、已配置的 bridge 和 `workflow-runtime.mjs`，保留同版来源与许可文件；然后重启服务，检查健康版本与桥接连接。不能只替换 core 而留下不兼容的桥接或 bundle。保留 timer 停止状态直到问题解决。没有自动清理发布备份，定期按磁盘空间人工保留需要的版本。

部署会短暂断开 WebSocket，进行中的请求可能中断；网页保留当前输入草稿，完成后重新连接。需要避免打断长任务时先暂停 timer，任务结束再开启。构建或测试失败不重启线上服务，网络暂时不可用时保持现有版本。

### 网页飞书设置

在核心服务的 `core.env` 中显式配置以下两个绝对路径（同时必须设置 `EA_API_KEY`）：

```dotenv
EA_FEISHU_ENV_FILE=/home/q/.config/easyagent/feishu.env
FEISHU_OWNER_STATE_FILE=/home/q/.config/easyagent/feishu-owner.json
```

`EA_FEISHU_ENV_FILE` 必须与 `easyagent-bridge.service` 的 `EnvironmentFile` 相同；owner 路径也必须与桥接一致。网页「设置」可查看核心/桥接进程及开机启动状态、保存 App ID/Secret 并重启桥接、按需读取未过期的私聊配对指令。Secret 不回传；留空保留旧密钥；首次配置后 App ID 固定，网页可更新密钥，更换机器人需另行重新配置使用者绑定（不同应用的 open_id 不可复用）。配置原子保存为 0600，保留无关环境项；重启命令失败恢复旧文件。网页不会修改已有 owner 绑定，也不自动发送飞书消息。进程运行不等于飞书凭据有效或 WebSocket 已连接，须完成飞书侧机器人发布与长连接订阅。

管理接口要求 Bearer 认证，即使本机无令牌模式也拒绝管理；未配置路径的其他部署只显示未托管说明。配置和配对响应使用 `Cache-Control: no-store`。生产部署使用 HTTPS 或可信内网访问；配对指令只私聊目标机器人。

开机启动需要同时满足 `systemctl --user enable easyagent-core.service easyagent-bridge.service easyagent-update.timer` 和 `loginctl enable-linger <user>`；linger 让用户无需登录即可启动用户服务。设置页读取实际状态，未进行物理断电重启测试时不要宣称已验证该过程。

### 更新时保护正在执行的任务

`EA_DEPLOY_CORE_ENV` 指向核心 EnvironmentFile（默认 `~/.config/easyagent/core.env`），更新器从中读取 `EA_API_KEY`，不在命令行或日志中打印它。可用 `EA_DEPLOY_CONTROL` 显式设置部署控制 URL，默认由健康检查 URL 推导 `/admin/deploy`。

控制接口始终要求认证：GET 返回活动数，POST 仅在空闲时取得 5 分钟租约，DELETE 使用原租约释放。忙碌返回 409；鉴权失败、接口不存在、网络失败均保留当前服务。更新器失败时尽力释放租约，异常退出后的租约也会过期，避免永久阻塞新任务。

首次从没有部署保护接口的旧版本升级必须作为明确的维护操作：检查没有活动任务后统一替换核心/桥接，并安装新版 `scripts/update-mini.sh` 到 `~/.local/bin/easyagent-update`。不要给定时更新配置“忽略忙碌”的回退。后续更新自动执行保护协议。服务收到退出信号时取消请求并最多等待 15 秒，给工具结果和会话状态留出落盘时间；断电等硬中断仍不保证执行副作用恰好一次。
