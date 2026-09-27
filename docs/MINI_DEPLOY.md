# 迷你主机更新

正式网页由 `q@192.168.5.16` 的用户级 `easyagent-core.service` 提供，入口 `http://192.168.5.16:8080`。Mac 用于开发预览。

合并到 GitHub `main` 后，用户级 `easyagent-update.timer` 每 5 分钟检查一次（加 0–30 秒随机延迟）。更新器按 SHA 获取源码，在独立构建目录测试根模块和 Bubble Tea、运行 vet 与网页脚本测试、构建 CLI 和 bridge；成功后原子替换服务程序并重启。健康检查必须返回对应提交版本，失败自动恢复旧程序并重启，失败版本不重复部署，后续新提交仍可更新。源码开发副本、运行配置、会话数据不被覆盖。

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

按实际主机修改 deploy.env 中 Go 路径和健康检查地址。保持用户 linger 开启，使服务在 SSH 退出后继续运行。更新器无需 GitHub Runner；不执行 PR 分支，不开放远程命令入口。生产服务已有的工具权限和网络监听设置保持不变。

## 运维

```bash
systemctl --user start easyagent-update.service  # 立即检查
systemctl --user status easyagent-core.service easyagent-update.timer
journalctl --user -u easyagent-update.service -n 80 --no-pager
curl -fsS http://192.168.5.16:8080/health
systemctl --user stop easyagent-update.timer     # 暂停自动更新
```

`~/.local/share/easyagent-deploy/current-revision` 记录健康版本；`previous-binary` 是最近部署前的可执行文件；`releases/<SHA>/easyagent` 保存已构建版本。健康失败的 SHA 记在 `failed-revision`，修复环境后删除该标记再手动启动更新服务即可重试。

手工回滚：先停 timer，复制选定 release 到 `~/.easyagent/bin/easyagent.next`，再 `mv` 到 `easyagent`，重启 core 并检查健康；保留 timer 停止状态直到问题解决。没有自动清理发布备份，定期按磁盘空间人工保留需要的版本。

部署会短暂断开 WebSocket，进行中的请求可能中断；网页保留当前输入草稿，完成后重新连接。需要避免打断长任务时先暂停 timer，任务结束再开启。构建或测试失败不重启线上服务，网络暂时不可用时保持现有版本。
