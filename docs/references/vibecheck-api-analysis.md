> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# VibeCheck 项目 API 深度分析

> 项目地址：https://github.com/hwj123hwj/VibeCheck
>
> 分析日期：2026-06-19

---

## 一、项目概览

VibeCheck 是一个基于 LLM 语义评语 + 歌词向量 + TF-IDF 的混合音乐推荐系统（毕业设计）。

- **后端**：FastAPI + PostgreSQL (pgvector)
- **前端**：React 18 + Vite
- **数据来源**：网易云音乐

**核心数据流：**

```
爬虫抓取网易云音乐数据 → LLM 生成情感评语 → 向量化入库 → 混合检索/推荐 → 前端播放+展示
```

---

## 二、网易云音乐 API 调用汇总

### 1. 歌词获取 API

| 项目 | 内容 |
|------|------|
| **端点** | `http://music.163.com/api/song/lyric` |
| **调用位置** | `app/routers/songs.py` (运行时)、`deploy_crawler/app.py` (爬虫) |
| **请求头** | `Referer: https://music.163.com/` |

**参数：**

| 参数 | 值 | 说明 |
|------|-----|------|
| `id` | 歌曲 ID | 网易云歌曲 ID |
| `lv` | 1 | 返回 LRC 格式 |
| `kv` | 1 | 包含逐字歌词 |
| `tv` | -1 | 包含翻译歌词 |

**返回值结构：**

```json
{
  "lrc": { "lyric": "[00:00.00]歌名\n[00:12.34]第一句..." },
  "tlyric": { "lyric": "[00:12.34]翻译歌词..." }
}
```

**用途：**
- **运行时**：`GET /api/songs/{song_id}/lrc` 实时获取带时间戳的 LRC 歌词，供前端歌词滚动播放
- **爬虫阶段**：抓取原始歌词文本，清洗后存入数据库 `songs.lyrics` 字段

---

### 2. 歌曲音频外链 API

| 项目 | 内容 |
|------|------|
| **端点** | `https://music.163.com/song/media/outer/url?id={song_id}.mp3` |
| **调用位置** | `app/routers/songs.py` 第 176-185 行 |
| **请求头** | `Referer: https://music.163.com/` + 标准 User-Agent |

**用途：** 免费歌曲的主要播放方式。后端 `GET /api/songs/{song_id}/audio` 接口通过流式代理转发，解决浏览器直接请求网易云 MP3 时的 Referer/CORS 拦截问题。

---

### 3. 歌曲音频 CDN 直链 API（兜底）

| 项目 | 内容 |
|------|------|
| **端点** | `http://music.163.com/api/song/enhance/player/url` |
| **调用位置** | `app/routers/songs.py` 第 192-208 行 |

**参数：**

| 参数 | 值 | 说明 |
|------|-----|------|
| `id` | 歌曲 ID | - |
| `ids` | `[{song_id}]` | 数组字符串 |
| `br` | 320000 | 比特率 320kbps |

**返回值结构：**

```json
{
  "data": [{ "url": "https://...cdn...mp3" }]
}
```

**用途：** 当外链方式失败时的兜底方案，通过 enhance/player/url API 获取 CDN 直链，再流式转发给前端。

---

### 4. 歌曲详情 API

| 项目 | 内容 |
|------|------|
| **端点** | `http://music.163.com/api/song/detail` |
| **调用位置** | `deploy_crawler/app.py` 第 160 行、`deploy_crawler/backfill_album_covers.py` 第 30/53 行 |
| **请求头** | `Referer: https://music.163.com/` + 随机 User-Agent |

**参数：**

| 参数 | 值 | 说明 |
|------|-----|------|
| `ids` | `[123, 456, ...]` | 歌曲 ID 数组（单次最多 50 首） |

**返回值结构：**

```json
{
  "songs": [{
    "id": 123456,
    "name": "歌名",
    "artists": [{"name": "歌手"}],
    "album": {"picUrl": "https://...cover.jpg"}
  }]
}
```

**用途：**
- **爬虫阶段**：批量获取歌曲标题、歌手名
- **补录脚本**：为数据库中 `album_cover` 为空的歌曲补录专辑封面 URL

---

### 5. 歌单详情 API

| 项目 | 内容 |
|------|------|
| **端点** | `https://music.163.com/api/v6/playlist/detail` |
| **调用位置** | `deploy_crawler/app.py` 第 139 行 |

**参数：**

| 参数 | 值 | 说明 |
|------|-----|------|
| `id` | 歌单 ID | - |

**返回值结构（关键字段）：**

```json
{
  "playlist": {
    "trackIds": [{"id": 123456}, ...],
    "tracks": [{"id": 123456}, ...]
  }
}
```

**用途：** 爬虫获取歌单内所有歌曲 ID 列表，用于后续批量获取歌曲详情和歌词。

---

### 6. 歌单列表页面（HTML 爬取）

| 项目 | 内容 |
|------|------|
| **端点** | `https://music.163.com/discover/playlist/?order=hot&cat=%E5%8D%8E%E8%AF%AD&limit=35&offset={offset}` |
| **调用位置** | `deploy_crawler/app.py` 第 70 行 |
| **请求方式** | HTTP GET，BeautifulSoup 解析 HTML |

**参数：**

| 参数 | 值 | 说明 |
|------|-----|------|
| `order` | hot | 按热度排序 |
| `cat` | 华语 | 华语分类 |
| `limit` | 35 | 每页 35 个歌单 |
| `offset` | - | 分页偏移量 |

**解析逻辑：** 从 `ul#m-pl-container` 中提取每个 `li` 里的 `a.msk` 标签的 `title` 和 `href`（提取歌单 ID）

**用途：** 爬虫入口，抓取热门华语歌单列表，共计划抓取 54 页（约 1890 个歌单）。

---

## 三、外部 AI/NLP 服务 API

### 7. 硅基流动 Embedding API

| 项目 | 内容 |
|------|------|
| **端点** | `https://api.siliconflow.cn/v1/embeddings` |
| **模型** | `BAAI/bge-m3` |
| **输出维度** | 1024 维 float 向量 |

**调用位置：**
- `app/services/embedding.py`（运行时搜索/推荐向量化）
- `deploy_crawler/batch_vectorization.py`（批量评语向量化）
- `deploy_crawler/batch_lyrics_vectorization.py`（批量歌词向量化）

**参数：**

| 参数 | 值 | 说明 |
|------|-----|------|
| `model` | `BAAI/bge-m3` | - |
| `input` | 文本字符串或数组 | 截断至 1500 字符 |
| `encoding_format` | `float` | - |

**用途：**
- 运行时：将用户搜索词向量化，与数据库中的 `review_vector` 和 `lyrics_vector` 做余弦相似度计算
- 离线批处理：将 LLM 评语和精华歌词分别向量化存入数据库

---

### 8. LongCat LLM API

| 项目 | 内容 |
|------|------|
| **端点** | `https://api.longcat.chat/openai/chat/completions`（OpenAI 兼容格式） |
| **模型** | LongCat-Flash-Chat / LongCat-Flash-Lite |

**调用位置：**
- `app/services/llm.py`（运行时意图路由）
- `deploy_crawler/batch_ai_analysis.py`（批量情感分析）

**用途 A — 搜索意图路由（运行时）：**

| 参数 | 值 |
|------|-----|
| temperature | 0 |
| max_tokens | 80 |
| response_format | json_object |

输入用户搜索词，输出：
```json
{"type": "exact|lyrics|vibe", "artist": null, "title": null, "vibe": "搜索词"}
```

**用途 B — 歌曲情感分析（离线批量）：**

| 参数 | 值 |
|------|-----|
| temperature | 0.7 |
| response_format | json_object |

输入歌曲标题 + 歌手 + 歌词（截断至 1200 字符），输出：
```json
{
  "vibe_tags": ["标签1", "标签2", "标签3"],
  "emotional_scores": {"维度1": 0.8, "维度2": 0.3},
  "review": "100字评语",
  "scene": "听歌场景"
}
```

---

## 四、VibeCheck 自建 API 端点

### 基础接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 根路径，返回运行状态 |
| GET | `/api/health` | 健康检查，返回数据库连接状态和歌曲总数 |

### 歌曲接口 (`app/routers/songs.py`)

| 方法 | 路径 | 参数 | 说明 |
|------|------|------|------|
| GET | `/api/songs/random/list` | `count=12` | 随机发现歌曲（首页用） |
| GET | `/api/songs/vibe-sections` | `per_section=6` | 首页情绪分区（深夜独处、治愈解压、恋爱心动、怀旧回忆、元气出发） |
| GET | `/api/songs/{song_id}` | - | 歌曲详情（含 AI 评语、vibe_tags、vibe_scores、歌词等） |
| GET | `/api/songs/{song_id}/lrc` | - | LRC 格式歌词（带时间戳 + 翻译歌词），实时从网易云获取 |
| GET | `/api/songs/{song_id}/audio` | - | 代理音频流（外链优先 → enhance API 兜底） |

### 搜索接口 (`app/routers/search.py`)

| 方法 | 路径 | 参数 | 说明 |
|------|------|------|------|
| GET | `/api/search` | `q`, `top_k=10`, `mode`, `personalized` | 多模式语义搜索 |

**mode 可选值：**

| 值 | 说明 |
|----|------|
| `auto`（默认） | LLM/规则引擎自动识别意图 |
| `vibe` | 心情氛围搜索，纯语义向量 |
| `lyrics` | 歌词搜索，歌词向量 + 关键词混合 |
| `title` | 歌名精确匹配（ILIKE） |
| `artist` | 歌手精确匹配（ILIKE） |

### 推荐接口 (`app/routers/recommend.py`)

| 方法 | 路径 | 参数 | 说明 |
|------|------|------|------|
| GET | `/api/recommend/{song_id}` | `top_k`, `w_review`, `w_lyrics`, `w_tfidf`, `w_emotion`, `dedupe` | 单曲推荐（四路融合） |

默认权重：评语向量 0.40 + 歌词向量 0.30 + TF-IDF 0.15 + 情感维度 0.15

### 认证接口 (`app/routers/auth.py`)

| 方法 | 路径 | 参数 | 说明 |
|------|------|------|------|
| POST | `/api/auth/register` | `username`, `password` | 注册，返回 JWT |
| POST | `/api/auth/login` | `username`, `password` | 登录，返回 JWT |
| GET | `/api/auth/me` | - | 获取当前用户信息（需 Bearer Token） |

### 用户接口 (`app/routers/user.py`)

| 方法 | 路径 | 参数 | 说明 |
|------|------|------|------|
| POST | `/api/user/history` | `song_id` | 记录播放（upsert，累加 play_count） |
| GET | `/api/user/history` | - | 获取最近 50 条播放历史 |
| DELETE | `/api/user/history` | - | 清空播放历史 |
| GET | `/api/user/profile` | - | 用户情感画像（5 维情感均值、top tags/artists/scenes） |

---

## 五、数据采集流水线

```
deploy_crawler/app.py          → 爬取歌单列表 → 歌单详情 → 歌曲详情 → 歌词 → 清洗入库
backfill_album_covers.py       → 通过歌曲详情 API 补录专辑封面
mark_duplicates.py             → 基于歌词内容去重标记
batch_update_core_lyrics.py    → 提取精华歌词（高频重复行）
extract_core_lyrics.py         → 精华歌词提取
batch_ai_analysis.py           → LongCat LLM 批量生成情感评语/vibe_tags/scores
batch_vectorization.py         → 硅基流动 API 将评语向量化为 1024 维 review_vector
batch_lyrics_vectorization.py  → 硅基流动 API 将精华歌词向量化为 1024 维 lyrics_vector
compute_tfidf.py               → jieba 分词 + scikit-learn TF-IDF 计算关键词向量
```

---

## 六、后端服务架构

### 搜索：两阶段混合检索

1. **向量召回**：并行 ANN 搜索（pgvector HNSW 索引），分别检索 `review_vector` 和 `lyrics_vector`
2. **精排**：SQL 条件过滤（歌名 ILIKE、歌手 ILIKE）+ 向量余弦相似度加权排序

### 推荐：四路融合

| 路径 | 默认权重 | 说明 |
|------|---------|------|
| `review_vector` 余弦相似 | 0.40 | AI 评语语义相似度 |
| `lyrics_vector` 余弦相似 | 0.30 | 歌词语义相似度 |
| TF-IDF 关键词重叠 | 0.15 | 关键词层面的相似度 |
| 五维情感余弦相似 | 0.15 | 情感维度相似度 |

### 个性化

- 基于用户播放历史计算情感偏好向量
- 高频歌手加权
- 通过 `personalized` 参数控制是否启用

### 缓存策略

- LRC 歌词和音频播放均通过后端代理转发网易云 API
- 24 小时 TTL 缓存

---

## 七、网易云 API 调用全景图

```
┌─────────────────────────────────────────────────────────────────┐
│                        网易云音乐 API                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─ 数据采集阶段 ─────────────────────────────────────────────┐ │
│  │                                                            │ │
│  │  歌单列表页 HTML  ──→  歌单详情 API  ──→  歌曲详情 API     │ │
│  │       │                     │                   │          │ │
│  │       │                     │                   │          │ │
│  │       ▼                     ▼                   ▼          │ │
│  │  BeautifulSoup       trackIds[]          标题/歌手/封面    │ │
│  │  解析歌单ID                                               │ │
│  │                                         歌词 API           │ │
│  │                                              │             │ │
│  │                                              ▼             │ │
│  │                                         LRC歌词入库        │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                 │
│  ┌─ 运行时播放 ──────────────────────────────────────────────┐ │
│  │                                                            │ │
│  │  前端请求                                                   │ │
│  │      │                                                      │ │
│  │      ├──→ /api/songs/{id}/audio  ──→ 音频外链（优先）      │ │
│  │      │                            ──→ CDN直链（兜底）      │ │
│  │      │                                                      │ │
│  │      └──→ /api/songs/{id}/lrc    ──→ 歌词API（实时+缓存） │ │
│  │                                                            │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

---

## 八、技术亮点

1. **双层音频代理策略**：先试外链 → 失败走 enhance API 兜底，保证播放可靠性
2. **歌词实时获取 + 24h 缓存**：不依赖入库歌词的 LRC 时间戳，而是实时从网易云拉取带时间戳的版本
3. **请求头伪装**：所有 API 调用都带 `Referer: https://music.163.com/` + 随机 User-Agent，绕过网易云反爬
4. **四路融合推荐**：评语向量 + 歌词向量 + TF-IDF + 情感维度，权重可调
5. **LLM 意图路由**：自动识别用户搜索是精确匹配（歌名/歌手）还是语义搜索（氛围/歌词）
