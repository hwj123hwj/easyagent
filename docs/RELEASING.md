# 版本与发布

## 版本规则

采用 [Semantic Versioning](https://semver.org/)，仓库 tag 加 `v` 前缀。正式版使用 `vMAJOR.MINOR.PATCH`，预发布仅使用 `vMAJOR.MINOR.PATCH-alpha.N`、`-beta.N` 或 `-rc.N`（N 从 1 开始）。不使用日期标签、`latest` 浮动标签或版本号中的前导零。

- PATCH：兼容的错误修复。
- MINOR：新增能力；0.x 阶段的不兼容变更也提升 MINOR，并在更新记录说明迁移要求。
- MAJOR：1.0 之后不兼容的公开接口变更。当前仍处于 0.x，不为发布流程整改直接跳到 1.0。
- 预发布用于验收，不标记为 GitHub Latest；正式发布只推进版本，不倒退。

公开接口包括 CLI 参数、配置名、HTTP 协议及 `sdk/`。`desktop/package.json` 描述独立 Electron 客户端的包版本，不用于决定核心 Release；工作流 bundle 跟随核心 tag 和提交发布。

2026-10-01 按维护者明确要求，在备份旧 Pi Agent 的 Release 元数据、全部资产和 Git 标签对象后，清理 `v0.10.0` 至 `v0.11.0` 的公开 Release 与 tag，以 EasyAgent `v0.1.0` 重新开始。这是更名时的一次性迁移；提交历史不清理。后续**一个 tag 永远对应一个提交；不能删除重打、强推移动或覆盖已发布资产。** GitHub 的 Release immutability 对启用之后的新发布锁定资产；tag ruleset 保护 `v*` 标签免于移动、删除。[GitHub 不可变发布说明](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)

## 两条分发路径

| 路径 | 标识与用途 |
|---|---|
| GitHub Release | 经验证的 tag；对外可下载版本，带明确变更说明与校验和 |
| 迷你主机持续部署 | 跟踪 main 的完整 SHA；日常开发更新，构建验证后重启与回滚 |

合并 PR 不等于发布新版本，也不需要为每个修复立刻打 tag。迷你主机保持当前 main 更新模式；不把它的 SHA 版本伪装成已发布 tag。服务暂停更新、回滚与自启动见 [部署运维](MINI_DEPLOY.md)。本地 `make build` 的 `git describe` 信息用于开发定位，带提交距离或 dirty 后缀的构建不是正式 Release。

## 准备版本

1. 选定版本，更新 [CHANGELOG.md](../CHANGELOG.md)：将本轮变化整理到精确的 `## [vX.Y.Z] - YYYY-MM-DD` 节，写明新功能、修复与迁移要求。保留下一轮 `Unreleased`。
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
2. 复用完整 Verify：动态工作流测试、Go 测试/vet、Bubble Tea、Web、更新器，以及发布与安装脚本测试。
3. 同一 SHA 构建 Linux/macOS、amd64/arm64 的核心与桥接共 8 个程序。注入相同版本，并验证 Linux 程序的 `--version`；Node bundle 与许可文件一起构建。
4. 生成 `release.json`（版本、完整提交 SHA、各资产 SHA-256）和 `checksums.txt`。缺失、空文件、额外旧文件、校验失败都会阻止发布。
5. 先建草稿，上传全部资产，核对 GitHub 返回的各文件 digest，再一次性公开；已公开版本拒绝重传。失败只保留草稿供检查，不公开半套文件。

发布相关脚本变更还会运行 `Release build check`：在临时仓库创建仅供测试的 tag，实际构建所有目标，不向 GitHub 推送测试 tag，也不创建 Release。

发布失败后先看 Actions 日志。纯网络上传失败可以重跑同一 tag，继续未公开的草稿；若需要改代码或脚本，提交修复后使用新版本，不移动旧 tag。草稿中出现不匹配的提交或额外资产会拒绝发布，应先核查来源。

## 安装与核验

安装脚本默认选择最新正式 Release，也可显式指定已有版本：

```bash
EA_VERSION=v0.1.0 bash scripts/install.sh
```

脚本校验 SHA-256 和程序报告的版本后才替换文件，新版同时安装核心、桥接、工作流 bundle 与许可归档。下载或校验失败保留旧安装；不再偷偷回退到 main 编译。安装器仍识别旧 `pi-agent-*` 资产，但本仓库旧公开 Release 已在更名迁移中清理。旧程序的 `--version` 存在启动副作用，因此安装器不会执行它来探测版本。

从带工作流 bundle 的新版降级到没有 bundle 的历史版本，应使用独立 `EA_HOME`，防止混用组件。安装器适合 CLI 安装，不负责生产服务的并发切换；正在运行的迷你主机继续使用其带锁和健康回滚的更新器。

SHA-256 用于检测下载损坏，信任根仍是 GitHub HTTPS 和发布权限。新版 Release 同时受 GitHub 不可变发布保护；下载完整资产后可运行 `sha256sum --check checksums.txt` 核验，macOS 可用 `shasum -a 256 -c checksums.txt`。

## 仓库保护配置

配置保存在 [main 规则](../.github/rulesets/main.json) 与 [tag 规则](../.github/rulesets/release-tags.json)。main 要求 PR、最新基线的 GitHub Actions `verify` 成功且讨论已解决，禁止强推和删除。个人维护不强制另一个账号审批；发布仍受上述检查约束。

维护者修改规则时先更新仓库配置，再通过 GitHub Rulesets API 更新对应现有 ruleset（按名称查 ID，避免重复创建）。Release immutability 使用仓库设置开启；它只约束之后的新发布，不会重写历史资产。
