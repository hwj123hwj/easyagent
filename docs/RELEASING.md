# 版本与发布

## 版本规则

采用 [Semantic Versioning](https://semver.org/)，仓库 tag 加 `v` 前缀。正式版使用 `vMAJOR.MINOR.PATCH`，预发布仅使用 `vMAJOR.MINOR.PATCH-alpha.N`、`-beta.N` 或 `-rc.N`（N 从 1 开始）。不使用日期标签、`latest` 浮动标签或版本号中的前导零。

- PATCH：兼容的错误修复。
- MINOR：新增能力；0.x 阶段的不兼容变更也提升 MINOR，并在更新记录说明迁移要求。
- MAJOR：1.0 之后不兼容的公开接口变更。当前仍处于 0.x，不为发布流程整改直接跳到 1.0。
- 预发布用于验收，不标记为 GitHub Latest；正式发布只推进版本，不倒退。

公开接口包括 CLI 参数、配置名、HTTP 协议及 `sdk/`。`desktop/package.json` 描述 Electron 客户端的包版本；从 `v0.2.0-alpha.1` 起，同一个 Release 包含 CLI、桥接和桌面；自 `v0.15.12` 起再包含 MyGo 原生客户端 DMG（`easyagent-native-darwin-arm64.dmg`）。发布时桌面包版本必须与 tag 去掉 `v` 后一致。

2026-10-01 按维护者明确要求，在备份旧 Pi Agent 的 Release 元数据、全部资产和 Git 标签对象后，清理 `v0.10.0` 至 `v0.11.0` 的公开 Release 与 tag，以 EasyAgent `v0.1.0` 重新开始。这是更名时的一次性迁移；提交历史不清理。后续**一个 tag 永远对应一个提交；不能删除重打、强推移动或覆盖已发布资产。** GitHub 的 Release immutability 对启用之后的新发布锁定资产；tag ruleset 保护 `v*` 标签免于移动、删除。[GitHub 不可变发布说明](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)

## 两条分发路径

| 路径 | 标识与用途 |
|---|---|
| GitHub Release | 经验证的 tag；对外可下载版本，带明确变更说明与校验和 |
| 迷你主机持续部署 | 跟踪 main 的完整 SHA；日常开发更新，构建验证后重启与回滚 |

合并 PR 不等于发布新版本，也不需要为每个修复立刻打 tag。迷你主机保持当前 main 更新模式；不把它的 SHA 版本伪装成已发布 tag。服务暂停更新、回滚与自启动见 [部署运维](MINI_DEPLOY.md)。本地 `make build` 的 `git describe` 信息用于开发定位，带提交距离或 dirty 后缀的构建不是正式 Release。

## 准备版本

1. 选定版本，更新 [CHANGELOG.md](../CHANGELOG.md)：将本轮变化整理到精确的 `## [vX.Y.Z] - YYYY-MM-DD` 节，写明新功能、修复与迁移要求。保留下一轮 `Unreleased`，同步桌面 `package.json` 与 lockfile 的版本。
2. 通过 PR 合并代码和版本说明，等 CI 完成。在干净的主工作副本或专用临时 checkout 操作，不提交凭据、运行数据或本地审阅文件。
3. 同步 main 和 tags，创建附注 tag，显式推送该 tag：

```bash
git switch main
git fetch --prune --tags origin
git pull --ff-only origin main
# 示例版本；先按本次实际变化选择并提交对应 CHANGELOG 节。
python3 scripts/release.py tag v0.1.1-rc.1
git push origin refs/tags/v0.1.1-rc.1
```

脚本拒绝脏工作区、非 main、未同步远端、重复/倒退版本和缺失说明。支持手工 `git tag -s` 签名；CI 至少强制附注标签。不要执行 `git push --tags`，以免顺带发布本地实验标签。

`Release` 工作流也可从 Actions 手动触发：必须选 main，`tag` 填**已经存在**的附注 tag。手动运行会检出该 tag 的提交，不会拿 main 当前代码冒充指定版本。用 `GITHUB_TOKEN` 创建标签通常不会递归触发 push 工作流，此时应显式 dispatch，不能假设发布已经开始。[触发规则](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)

## 自动发布的强制检查

1. 严格版本格式；附注 tag；HEAD 与 tag 一致；提交已进入 main；版本高于现有版本；对应 CHANGELOG 节非空。
2. 复用完整 Verify：Go 测试/vet、Bubble Tea、Web、桌面 TypeScript / 测试 / 构建、更新器，以及发布与安装脚本测试；Release build check 另在 Apple Silicon runner 实际构建原生客户端。
3. 同一 SHA 构建 Linux/macOS、amd64/arm64 的核心与桥接共 8 个程序。注入相同版本，并验证 Linux 程序的 `--version`。桌面 DMG 与原生客户端 DMG 仅在 Apple Silicon runner 构建；桌面 DMG 核验包版本、内置核心的源码 SHA 与架构及 DMG 内部内容，并检查包内应用的完整 ad hoc 签名；原生 DMG 核验 Bundle ID 为 `com.hwj.easyagent`。Intel 安装包自 v0.11.0 起不再构建。
4. 汇集 8 个核心及桥接程序、1 个桌面 DMG 和 1 个原生 DMG，生成完整 `release.json`（版本、完整提交 SHA、各资产 SHA-256）和 `checksums.txt`，共 12 项资产。Linux core 阶段的 `core_only` 清单不能发布；缺失任一 DMG、空文件、错版本文件名、额外旧文件或校验失败都会阻止发布。
5. 先建草稿，上传全部资产，核对 GitHub 返回的各文件 digest，再一次性公开；已公开版本拒绝重传。失败只保留草稿供检查，不公开半套文件。

发布相关脚本变更还会运行 `Release build check`：在临时仓库创建仅供测试的 tag，实际构建所有核心目标及两个桌面目标，不向 GitHub 推送测试 tag，也不创建 Release。

发布失败后先看 Actions 日志。纯网络上传失败可以重跑同一 tag，继续未公开的草稿；若需要改代码或脚本，提交修复后使用新版本，不移动旧 tag。草稿中出现不匹配的提交或额外资产会拒绝发布，应先核查来源。

## 安装与核验

macOS 桌面从 Release 下载 `EasyAgent-版本-arm64.dmg`（Apple Silicon）；Intel Mac 请使用 v0.10.1 及更早的安装包。桌面更新检查只提示正式版；RC 需手动下载。当前 macOS 安装包使用 ad hoc 测试签名，未进行 Developer ID 签名或 Apple 公证，应在 Release 中明确说明；完整测试签名与 GitHub SHA-256 校验均不代替 Apple 的开发者认证和公证。

安装脚本默认选择最新正式 Release，也可显式指定已有版本：

```bash
EA_VERSION=v0.1.0 bash scripts/install.sh
```

脚本校验 SHA-256 和程序报告的版本后才替换文件，新版同时安装核心与桥接。下载或校验失败保留旧安装；不再偷偷回退到 main 编译。安装器仍识别旧 `pi-agent-*` 资产，但本仓库旧公开 Release 已在更名迁移中清理。旧程序的 `--version` 存在启动副作用，因此安装器不会执行它来探测版本。

安装器适合 CLI 安装，不负责生产服务的并发切换；正在运行的迷你主机继续使用其带锁和健康回滚的更新器。

SHA-256 用于检测下载损坏，信任根仍是 GitHub HTTPS 和发布权限。新版 Release 同时受 GitHub 不可变发布保护；下载完整资产后可运行 `sha256sum --check checksums.txt` 核验，macOS 可用 `shasum -a 256 -c checksums.txt`。

## 本机固定签名与权限继承

公开 CI 在没有 Developer ID 证书时仍产出 ad hoc 包，其默认 designated requirement 绑定本次构建的 CDHash；内容改变会改变身份，不能承诺旧的辅助功能 / 屏幕录制授权一直有效。公证与 TCC 授权是不同机制，签名不会自动授予权限。

个人 Mac 可使用仓库外的固定项目证书，首次准备需要 OpenSSL 3（如 Homebrew openssl@3）。`python3 scripts/prepare-local-signing.py` 准备专用钥匙串及固定身份，默认不修改信任；确认后才运行 `python3 scripts/prepare-local-signing.py --trust-for-codesign`，仅为这一张证书添加当前用户的 CodeSigning 信任，并限定 `/usr/bin/codesign`，不添加网页 / TLS 或系统级信任。私钥保留在专用钥匙串，原始 PEM / P12 在导入后移除；配置和钥匙串密码在 `~/Library/Application Support/EasyAgent Signing/`，目录 0700、敏感文件 0600，不上传、提交或打印。首次准备失败或已有身份不完整时拒绝自动重建，避免轮换身份。

`scripts/build-desktop.sh arm64` 自动读取该固定配置；也可用 `EA_MAC_SIGNING_CONFIG` 指定同样受权限检查的配置。主应用与 helper 的 DR 都包含各自 Bundle ID 和同一证书指纹，不依赖应用版本或 CDHash；helper 签名先校验、打包时不重复签名。代码签名需要临时加入专用钥匙串搜索项，完成或失败后只移除本次加入项，保留其他设置。本机签名通过互斥锁协调自动更新与手动打包；不会并发修改专用钥匙串搜索项。

`EA_MAC_SIGNING_MODE=adhoc` 是明确的临时签名选择，可用于 CI / 无身份的试包。配置存在但不可用时不会自动使用它；不要用临时包覆盖固定签名的正式本机入口。公开 Release 的 DMG 仍是 CI ad hoc 包，从 v0.15.5 起，已配置固定身份且安装于 `/Applications/EasyAgent.app` 的 Mac 桌面默认自动检查正式版本：在独立进程中下载公开 DMG 并核验 SHA-256、版本、源码提交和架构，再复用本机证书分别签名 helper / 主应用，验证相同身份后安装。无需 Go、构建工具或手动构建，私钥不会离开本机。首次启用固定身份仍按前述准备及授权流程执行；无固定身份的公开版仅提供手动下载，已有固定身份损坏时不会用公开 ad hoc 包覆盖。这条本机路径不是 Apple Developer ID 分发或公证，不能拿来保证其他设备直接下载后的权限继承。

从旧 ad hoc 身份切换到固定证书时仍需一次辅助功能和屏幕录制授权；随后比较升级前后 DR 并读取实际权限，不只看系统设置开关。证书有期限，私钥 / 证书更换、切回 ad hoc、Bundle ID 变化或用户撤权都会破坏继承，不自动修改 TCC 数据库、绕过权限或关闭 Gatekeeper。

## 仓库保护配置

配置保存在 [main 规则](../.github/rulesets/main.json) 与 [tag 规则](../.github/rulesets/release-tags.json)。main 要求 PR、最新基线的 GitHub Actions `verify` 成功且讨论已解决，禁止强推和删除。个人维护不强制另一个账号审批；发布仍受上述检查约束。

维护者修改规则时先更新仓库配置，再通过 GitHub Rulesets API 更新对应现有 ruleset（按名称查 ID，避免重复创建）。Release immutability 使用仓库设置开启；它只约束之后的新发布，不会重写历史资产。

### 自动安装与回滚

设置 → 常规可查看自动更新进度或关闭自动更新；关闭只阻止自动安装，仍可检查更新并手动触发已校验的固定身份更新。启动后检查一次，之后每小时检查正式版本；已准备的更新每 30 秒确认能否安装。用户仍在 EasyAgent 前台、有终端打开、未保存文件编辑或本地 / 当前远程主机任务运行时等待，不强制取消任务。

安装前取得本地核心 `/admin/deploy` 租约，正常关闭应用和核心；独立安装进程等待退出后，在同一文件系统内替换应用并保留旧版。新版在实际托管核心就绪后写入私有回执；若旧版电脑控制权限已完整授予，回执还要求新版实际权限继续有效，不请求权限或修改 TCC。未就绪或权限丢失则正常停止新版并恢复旧版，本轮自动安装暂停至下一次启动。更新器不会创建、轮换或额外信任证书；身份失效时保留旧应用，需先恢复原有证书。

中断的准备记录保存在应用用户数据下 `desktop-update-pending.json`；已准备版本重启后重新校验身份再恢复，未完成准备只清理记录指向的当前用户私有 `.noindex` 临时目录，忙碌的挂载保留。安装结果 `desktop-update-result.json` 记录回滚应用路径，旧版保留供恢复。固定身份更新不等于 Developer ID / Apple 公证，公开 DMG 仍由 CI ad hoc 签名；自动更新会先在本机重新签名，而手动直接拖入公开包仍会改变权限身份。
