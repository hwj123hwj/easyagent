> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# cc-haha WebFetch 调研报告 — 源码级实现分析

> 调研日期：2026-06-20
> 来源：本地 `/Users/weijian/Desktop/develop/test/pi/cc-haha`（基于 Anthropic Claude Code 泄露源码修复版）
> 调研目标：为 EasyAgent 实现 `web_fetch` 内置工具提供权威参照。cc-haha 的 WebFetch 即 Anthropic 官方 Claude Code 的真实实现，比 DeepVcodeClient 的 web_fetch 成熟一个档次，是最佳落地参照。
> 对照：DeepVcodeClient `packages/core/src/tools/web-fetch.ts`（444 行，gemini-cli 继承）

---

## 1. 为什么调研这个

EasyAgent 当前**完全没有 web 工具**（10 个内置工具都是本地文件/shell 操作），Agent 离线。P2 计划加 `web_fetch`（让 Agent 能读用户给的 URL 内容）。

调研前已确认：
- **不做 `web_search`**（编程 agent 低频，且 DeepV 那条路靠 Gemini grounding，EasyAgent 多 Provider 走不了；用户主动给链接更准）。
- **只做 `web_fetch`**，要找最成熟的实现作参照 → cc-haha（=官方 Claude Code）。

---

## 2. 文件结构

```
src/tools/WebFetchTool/
├── WebFetchTool.ts   (318 行) — 工具定义、call 逻辑、权限确认
├── utils.ts          (543 行) — 抓取、HTML→markdown、安全校验
├── prompt.ts         (46 行)  — 工具描述、提示词
├── preapproved.ts    (166 行) — 预批准域名白名单（免确认）
└── utils.test.ts
```

工具名常量 `WEB_FETCH_TOOL_NAME`，参数结构化：
```ts
inputSchema = z.object({
  url:    z.string().url(),
  prompt: z.string(),  // 对抓回内容的处理指令，可选语义
})
```

**比 DeepV 设计更好**：DeepV 把 URL 塞在 `prompt` 字符串里用正则提取（最多 20 个），松散易错；cc-haha 用结构化 `{url, prompt}`，干净。

---

## 3. 核心流程

`call({url, prompt})` 主流程：

1. `validateURL(url)` — URL 基础校验
2. `checkDomainBlocklist(domain)` — 域名黑名单查询
3. `getURLMarkdownContent(url, abortController)` — 抓取 + HTML→markdown
4. 若返回 `redirect` 类型 → 返回提示让 Agent 用新 URL 重调（**不自动跨域跟随**）
5. `applyPromptToMarkdown(prompt, markdown)` — 把 prompt 和内容一起处理/截断返回

---

## 4. 关键工程亮点（值得 EasyAgent 学的）

### 4.1 HTML→markdown 用成熟库（turndown），懒加载

`utils.ts:98-108`：
```ts
// Lazy singleton — defers the turndown → @mixmark-io/domino import (~1.4MB)
let turndownServicePromise
function getTurndownService() {
  return (turndownServicePromise ??= import('turndown').then(m => new m.default()))
}
// 转换：
markdownContent = (await getTurndownService()).turndown(htmlContent)
```

注释强调：1.4MB 的依赖延迟到首次使用才加载，`.turndown()` 本身无状态。

**对比 DeepV**：DeepV 的 web_fetch **根本不做 HTML→markdown**，主路径直接把原始内容喂给 LLM 让它自己消化（费 token、噪音多）。cc-haha 用成熟库转 markdown 是正确做法。

**EasyAgent 启示**：Go 侧用 `github.com/JohannesKaufmann/html-to-markdown`（Go 生态最成熟的等价库），同样懒加载。

### 4.2 三层长度控制（比 DeepV 精细）

| 层级 | 常量 | 作用 |
|------|------|------|
| URL 长度 | `MAX_URL_LENGTH` | 防超长 URL |
| 原始 HTML | `MAX_HTTP_CONTENT_LENGTH`（10MB） | 抓回的 HTML 上限，超了**先截断再转 markdown**（让 GC 回收原始大 buffer） |
| markdown | `MAX_MARKDOWN_LENGTH = 100_000` | 转换后最终上限 |

`utils.ts:497-507`：
```ts
markdownContent.length > MAX_MARKDOWN_LENGTH
  ? markdownContent.slice(0, MAX_MARKDOWN_LENGTH) + '...(truncated)'
  : markdownContent
```

**对比 DeepV**：只有单一 `MAX_CONTENT_LENGTH = 10000`，没有 HTML 层的截断，超大页面可能先撑爆内存。

### 4.3 安全：URL 校验（validateURL）

`utils.ts:152-186`：
```ts
function validateURL(url: string): boolean {
  if (url.length > MAX_URL_LENGTH) return false
  let parsed
  try { parsed = new URL(url) } catch { return false }
  // 禁止带 username/password 的 URL（防凭证泄露/内网伪装）
  if (parsed.username || parsed.password) return false
  // hostname 至少 2 段（过滤裸主机名如 "localhost"/"intranet"）
  const parts = parsed.hostname.split('.')
  if (parts.length < 2) return false
  return true
}
```

**对比 DeepV**：DeepV 只查 prompt 里有没有 `http://` 前缀。cc-haha 多了"禁凭证 URL"和"hostname 至少 2 段"两条，更严。

### 4.4 安全：跨域重定向不自动跟随（重要）

`isPermittedRedirect`（`utils.ts:225+`）+ `call` 里的 redirect 分支（`WebFetchTool.ts:200-230`）：

允许的重定向：
- 同 origin 改 path/query
- 加/减 `www.`

**跨域重定向不自动跟**，而是返回：
```
REDIRECT DETECTED: The URL redirects to a different host.
Original URL: ...
Redirect URL: ...
To complete your request, please use WebFetch again with:
- url: "${redirectUrl}"
- prompt: "${prompt}"
```

让 Agent 显式决定是否跟——**防恶意重定向引导到危险域名**。

**对比 DeepV**：DeepV 没有重定向特殊处理，默认行为跟随。

### 4.5 安全：域名黑名单（checkDomainBlocklist）

`utils.ts:189-222`：
```ts
async function checkDomainBlocklist(domain) {
  if (DOMAIN_CHECK_CACHE.has(domain)) return { status: 'allowed' }
  const response = await axios.get(
    `https://api.anthropic.com/api/web/domain_info?domain=${encodeURIComponent(domain)}`,
    { timeout: DOMAIN_CHECK_TIMEOUT_MS },
  )
  if (response.data.can_fetch === true) {
    DOMAIN_CHECK_CACHE.set(domain, true)
    return { status: 'allowed' }
  }
  return { status: 'blocked' }
}
```

调 Anthropic 私有 API 查域名是否可抓 + 本地缓存。

**EasyAgent 不能照搬**：这是 Anthropic 私有服务。EasyAgent 得用本地规则替代（内网 IP 拦截 + 可选的域名黑名单配置文件）。DeepV 的 `isPrivateIp`（拦 127.x/10.x/192.168/172.16-31/169.254/::1）可作为本地 SSRF 防护的补充。

### 4.6 权限：走确认机制（与 EasyAgent P0 一致）

`WebFetchTool.ts:108-180`：非预批准域名 → 走权限确认（`WebFetchPermissionRequest` 组件）。

`preapproved.ts`（166 行）：常见文档站（官方文档、GitHub 等）预批准，免确认直接抓。

**这印证了 EasyAgent P0 做确认机制是对的**——官方 Claude Code 的 web_fetch 也需要确认。EasyAgent 的 `ToolWithConfirmation` 可直接用于 web_fetch：非白名单域名触发确认。

### 4.7 自定义 User-Agent

`getWebFetchUserAgent()`（注释提到）— 自定义 UA 标识自己，避免被反爬机制拒绝。DeepV 用默认 fetch UA，可能被部分站点挡。

### 4.8 工具提示词的诚实声明（prompt.ts）

`WebFetchTool.ts:188`：
```
IMPORTANT: WebFetch WILL FAIL for authenticated or private URLs. Before using
this tool, check if the URL points to an authenticated service (e.g. Google Docs,
Confluence, Jira, GitHub). If so, look for a specialized MCP tool...
```

工具描述里**主动声明限制**（抓不了需认证的页面），引导 Agent 不要盲目尝试。

---

## 5. cc-haha vs DeepV 对比

| 维度 | DeepV (web-fetch.ts 444行) | cc-haha (WebFetchTool 共1115行) |
|------|---------------------------|-------------------------------|
| 参数 | URL 塞 prompt 里，正则提取 | 结构化 `{url, prompt}` |
| HTML→markdown | **不做**，靠 LLM 消化 | turndown 库，懒加载 |
| 长度控制 | 单一 10000 字符 | 三层（URL/HTML 10MB/markdown 100K） |
| URL 校验 | 仅 http 前缀 | 长度+禁凭证+hostname≥2段 |
| SSRF | `isPrivateIp` 本地表 | Anthropic API 黑名单 + 缓存 |
| 重定向 | 默认跟随 | 跨域不自动跟，返回提示 |
| 权限 | 无 | 预批准白名单 + 确认 |
| User-Agent | 默认 | 自定义 |

cc-haha 全面更成熟，是 EasyAgent 的首选参照。

---

## 6. EasyAgent 落地建议

基于 cc-haha 的成熟实现，EasyAgent `web_fetch` 的设计锚点：

| 设计点 | EasyAgent 做法 | 参照来源 |
|--------|-----------|---------|
| 参数 | 结构化 `{url, prompt?}` | cc-haha |
| HTML→markdown | `JohannesKaufmann/html-to-markdown`，懒加载 | cc-haha (turndown) |
| 长度控制 | 三层（URL / 原始 HTML / markdown） | cc-haha |
| URL 校验 | 长度 + 禁凭证 URL + hostname≥2 段 | cc-haha |
| SSRF | 本地内网 IP 拦截（学 DeepV isPrivateIp）+ 可选域名黑名单配置 | DeepV + cc-haha 本地部分 |
| 重定向 | 跨域不自动跟，返回提示让 Agent 重调 | cc-haha |
| 权限 | 复用 EasyAgent P0 的 `ToolWithConfirmation`，非白名单域名确认 | cc-haha |
| User-Agent | 自定义 UA | cc-haha |
| 工具描述 | 诚实声明"抓不了需认证的页面" | cc-haha |

**不做**（与之前决策一致）：
- ❌ `web_search`（低频，且无通用免费搜索后端）
- ❌ 调用外部域名黑名单服务（cc-haha 那个是 Anthropic 私有）

**预估实现量**：单工具约 200-250 行（含校验、抓取、转换、安全），加 html-to-markdown 依赖。比 cc-haha 的 1115 行少很多（cc-haha 含桌面端权限 UI、预批准域名表、测试等，EasyAgent 复用已有确认机制不需重做）。

---

## 7. 关键文件索引

### cc-haha
| 文件 | 行数 | 关注点 |
|------|-----:|--------|
| `src/tools/WebFetchTool/WebFetchTool.ts` | 318 | 工具定义、call 流程、redirect 处理、权限确认 |
| `src/tools/WebFetchTool/utils.ts` | 543 | validateURL、checkDomainBlocklist、getURLMarkdownContent、turndown、三层长度控制 |
| `src/tools/WebFetchTool/preapproved.ts` | 166 | 预批准域名白名单 |
| `src/tools/WebFetchTool/prompt.ts` | 46 | 工具描述 + 限制声明 |

### EasyAgent（待实现，参照点）
| 文件 | 用途 |
|------|------|
| `internal/tools/web_fetch.go`（新） | web_fetch 工具主体 |
| `internal/tools/web_fetch_security.go`（新） | URL 校验 + SSRF 防护（isPrivateIp） |
| 复用 `internal/agent/tool.go` 的 `ToolWithConfirmation` | 非白名单域名确认 |
| 复用 `internal/tools/truncate.go` 的 `TruncateOutput` | markdown 截断 |

### DeepV（对照）
| 文件 | 行数 | 关注点 |
|------|-----:|--------|
| `packages/core/src/tools/web-fetch.ts` | 444 | 无 markdown 转换、单一长度限制、isPrivateIp |
| `packages/core/src/utils/fetch.ts` | — | fetchWithTimeout（AbortController）、isPrivateIp 内网表 |

---

**文档版本**：v1.0
**最后更新**：2026-06-20
**关联文档**：`research/deepvcode-gap-deep-analysis.md`（web 工具差距章节，需将"WebSearch 内置"纠正为"砍掉，只做 web_fetch"）
