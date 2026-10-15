> 历史参考：本文仅反映记录时的外部项目或接口版本，不代表 EasyAgent 当前实现。旧路径与建议仅供追溯；当前使用方式见 [文档索引](../README.md)。

# NetEase Cloud Music API Reference -- Recommendation & Discovery

> Reverse-engineered API endpoints for the NetEase Cloud Music (网易云音乐) web client.
> Primary reference: [Binaryify/NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi) (Node.js wrapper).

---

## Table of Contents

1. [Authentication Overview](#authentication-overview)
2. [Personal Recommendations (推荐歌曲)](#1-personal-recommendations-推荐歌曲)
3. [Top / High Quality Playlists (精品歌单)](#2-top--high-quality-playlists-精品歌单)
4. [Playlist Detail (歌单详情)](#3-playlist-detail-歌单详情)
5. [Daily Recommendations (每日推荐)](#4-daily-recommendations-每日推荐)
6. [Similar Songs (相似推荐)](#5-similar-songs-相似推荐)
7. [Hot Songs / Top Lists (排行榜)](#6-hot-songs--top-lists-排行榜)
8. [Common Song Object Structure](#common-song-object-structure)
9. [Request Encryption Notes](#request-encryption-notes)

---

## Authentication Overview

### Cookie-Based Auth

All personalized/restricted endpoints require a valid session cookie. The key cookie values are:

| Cookie Field | Description |
|---|---|
| `MUSIC_U` | Main authentication token (obtained from login) |
| `__csrf` | CSRF protection token |
| `__remember_me` | Session persistence flag |
| `NMTID` | Session identifier |

### How to Obtain Cookies

```
POST /login/cellphone?phone={phone}&password={password}
POST /login/email?email={email}&password={password}
```

The response `Set-Cookie` header contains the full cookie string. Extract `MUSIC_U` and `__csrf` for subsequent requests.

### Passing Cookies in Requests

Two approaches depending on your client:

1. **As a query/form parameter** (NeteaseCloudMusicApi wrapper style):
   ```
   GET /endpoint?cookie=MUSIC_U=xxx;__csrf=xxx
   ```

2. **As HTTP headers** (direct API call style):
   ```
   Cookie: MUSIC_U=xxx; __csrf=xxx
   ```

### Common Required Headers

```
Content-Type: application/x-www-form-urlencoded
User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 ...
Referer: https://music.163.com
Origin: https://music.163.com
```

---

## 1. Personal Recommendations (推荐歌曲)

**Endpoint:** `POST /api/v3/discovery/recommend/songs`

> Note: The NeteaseCloudMusicApi wrapper exposes this as `GET /recommend/songs`.

| Field | Value |
|---|---|
| **Auth Required** | YES -- requires logged-in user (MUSIC_U cookie) |
| **Method** | POST (raw API) / GET (wrapper) |
| **Parameters** | None (uses cookie to identify user) |

### Response Structure

```json
{
  "code": 200,
  "data": {
    "dailySongs": [
      {
        "name": "Song Name",
        "id": 123456,
        "ar": [
          { "id": 789, "name": "Artist Name" }
        ],
        "al": {
          "id": 111,
          "name": "Album Name",
          "picUrl": "https://p1.music.126.net/..."
        },
        "dt": 240000,
        "reason": "You may like this song",
        "privilege": { ... }
      }
    ],
    "recommendReasons": [
      {
        "songId": 123456,
        "reason": "Based on your listening history"
      }
    ]
  }
}
```

### Key Response Fields

| Field | Description |
|---|---|
| `data.dailySongs` | Array of recommended song objects |
| `data.recommendReasons` | Per-song recommendation reason text |
| `code` | `200` = success |

### Notes

- Returns approximately 30 songs per call.
- This is the **same underlying endpoint** used for daily recommendations (the NetEase web player calls it "Daily Mix" / 每日推荐).
- Unauthenticated requests will return error code `-1` or a login prompt.

---

## 2. Top / High Quality Playlists (精品歌单)

### 2a. High Quality Playlists (精品歌单)

**Endpoint:** `GET /top/playlist/highquality`

| Field | Value |
|---|---|
| **Auth Required** | NO -- public endpoint |
| **Method** | GET |

### Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `cat` | string | No | Category filter: "全部", "华语", "欧美", "电子", "民谣", "摇滚", "说唱", "轻音乐", "R&B", "爵士", etc. Default: "全部" |
| `limit` | number | No | Number of results. Default: 50 |
| `before` | number | No | Pagination cursor. Use `lasttime` from previous response to get the next page |

### Response Structure

```json
{
  "code": 200,
  "more": true,
  "lasttime": 1699000000000,
  "total": 500,
  "playlists": [
    {
      "id": 12345,
      "name": "Playlist Name",
      "coverImgUrl": "https://p1.music.126.net/...",
      "description": "Playlist description",
      "tags": ["流行", "华语"],
      "playCount": 1000000,
      "trackCount": 50,
      "creator": {
        "nickname": "User",
        "userId": 12345,
        "avatarUrl": "https://..."
      },
      "createTime": 1699000000000,
      "updateTime": 1699000000000,
      "subscribedCount": 1000,
      "shareCount": 500,
      "commentCount": 200
    }
  ]
}
```

### Key Response Fields

| Field | Description |
|---|---|
| `playlists` | Array of playlist summary objects |
| `more` | `true` if more pages available |
| `lasttime` | Pass as `before` param for next page |
| `total` | Total matching playlists |

### Pagination

```
# First page
GET /top/playlist/highquality?cat=华语&limit=20

# Next page
GET /top/playlist/highquality?cat=华语&limit=20&before={lasttime_from_previous_response}
```

---

### 2b. Top Playlists (热门歌单)

**Endpoint:** `GET /top/playlist`

| Field | Value |
|---|---|
| **Auth Required** | NO -- public endpoint |
| **Method** | GET |

### Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `cat` | string | No | Category filter (same as highquality). Default: "全部" |
| `order` | string | No | `"hot"` (default) or `"new"` |
| `limit` | number | No | Results per page. Default: 50 |
| `offset` | number | No | Pagination offset. Default: 0 |

### Response Structure

Similar to highquality, returns an array of playlist objects under `playlists`.

---

## 3. Playlist Detail (歌单详情)

**Endpoint:** `GET /playlist/detail`

| Field | Value |
|---|---|
| **Auth Required** | NO -- public endpoint (but privilege info may vary with auth) |
| **Method** | GET |

### Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | number | Yes | The playlist ID |
| `s` | number | No | Number of tracks to return from creator/share comments (optional, affects `relatedVideos`) |

### Response Structure

```json
{
  "code": 200,
  "relatedVideos": {},
  "playlist": {
    "id": 12345,
    "name": "Playlist Name",
    "coverImgUrl": "https://p1.music.126.net/...",
    "createTime": 1699000000000,
    "creator": {
      "nickname": "User",
      "userId": 12345,
      "avatarUrl": "https://...",
      "backgroundUrl": "https://..."
    },
    "trackCount": 50,
    "playCount": 1000000,
    "description": "Playlist description text",
    "tags": ["流行", "华语"],
    "subscribedCount": 1000,
    "shareCount": 500,
    "commentCount": 200,
    "privacy": 0,
    "ordered": true,
    "status": 0,
    "tracks": [
      {
        "id": 123456,
        "name": "Song Name",
        "ar": [{ "id": 789, "name": "Artist Name" }],
        "al": {
          "id": 111,
          "name": "Album Name",
          "picUrl": "https://..."
        },
        "dt": 240000,
        "mv": 0,
        "pop": 95.0
      }
    ],
    "trackIds": [
      { "id": 123456 },
      { "id": 789012 }
    ]
  },
  "urls": {},
  "privileges": [
    {
      "id": 123456,
      "fee": 0,
      "payed": 0,
      "st": 0,
      "pl": 320000,
      "dl": 320000,
      "sp": 7,
      "cp": 1,
      "subp": 1,
      "maxbr": 320000,
      "fl": 320000
    }
  ]
}
```

### Key Response Fields

| Field | Description |
|---|---|
| `playlist.tracks` | Full song objects for the first batch of tracks (typically ~1000 max) |
| `playlist.trackIds` | Complete list of all track IDs in the playlist (use with `/song/detail` for large playlists) |
| `privileges` | Per-track playback rights (fee, bitrate, etc.) |
| `playlist.privacy` | `0` = public, `10` = private |

### Handling Large Playlists

For playlists with more than ~1000 tracks, `tracks` is truncated. Use `trackIds` with:

```
POST /song/detail?ids={id1},{id2},{id3}
```

to fetch full song details in batches.

### Privilege Fields

| Field | Meaning |
|---|---|
| `fee` | `0` = free, `1` = VIP, `4` = paid album, `8` = free with login |
| `pl` | Playable bitrate (0 = cannot play) |
| `dl` | Downloadable bitrate |
| `maxbr` | Maximum allowed bitrate |
| `st` | Status (`0` = normal) |

---

## 4. Daily Recommendations (每日推荐)

**Endpoint:** `GET /recommend/songs`

This is functionally the **same endpoint** as Personal Recommendations (#1). NetEase uses "每日推荐" (Daily Recommendation) as the UI label. The underlying API call is identical:

| Field | Value |
|---|---|
| **Auth Required** | YES -- requires logged-in user |
| **Method** | GET (wrapper) / POST (raw `api/v3/discovery/recommend/songs`) |
| **Parameters** | None |

See [Section 1](#1-personal-recommendations-推荐歌曲) for response structure.

### Related: Recommend Resources (推荐歌单/资源)

**Endpoint:** `GET /recommend/resource`

| Field | Value |
|---|---|
| **Auth Required** | YES |

Returns recommended playlists tailored to the logged-in user (as opposed to songs). Response contains playlist objects under `recommend[]`.

---

## 5. Similar Songs (相似推荐)

### 5a. `/simi/song` (Primary)

**Endpoint:** `GET /simi/song`

| Field | Value |
|---|---|
| **Auth Required** | YES -- requires cookie |
| **Method** | GET |

### Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `songid` | number | Yes | The song ID to find similar songs for |
| `limit` | number | No | Number of results. Default: 50 |

### Example

```
GET /simi/song?songid=347230&limit=20
```

### Response Structure

```json
{
  "code": 200,
  "songs": [
    {
      "id": 123456,
      "name": "Similar Song Name",
      "ar": [{ "id": 789, "name": "Artist" }],
      "al": { "id": 111, "name": "Album", "picUrl": "https://..." },
      "dt": 240000
    }
  ]
}
```

### 5b. `/api/discovery/simiSong` (Legacy)

The older endpoint path used by the web client directly:

| Field | Value |
|---|---|
| **Raw API path** | `POST /api/discovery/simiSong` |
| **Auth Required** | YES |

Parameters are encrypted using the WeAPI scheme (see [Encryption Notes](#request-encryption-notes)). When using the NeteaseCloudMusicApi wrapper, `/simi/song` handles encryption automatically.

---

## 6. Hot Songs / Top Lists (排行榜)

### 6a. Get All Chart Lists (获取所有排行榜)

**Endpoint:** `GET /toplist`

| Field | Value |
|---|---|
| **Auth Required** | NO -- public endpoint |
| **Method** | GET |
| **Parameters** | None |

### Response Structure

```json
{
  "code": 200,
  "list": [
    {
      "id": 19723756,
      "name": "云音乐飙升榜",
      "coverImgUrl": "https://...",
      "description": "...",
      "updateFrequency": "每天更新",
      "tracks": [
        { "first": "Song Name", "second": "Artist" }
      ],
      "updateTime": 1699000000000,
      "playCount": 5000000,
      "trackCount": 100
    }
  ],
  "artistToplist": { ... }
}
```

Use the `id` values from this response to query specific charts via `/top/list`.

### 6b. Get Specific Chart (获取排行榜详情)

**Endpoint:** `GET /top/list`

| Field | Value |
|---|---|
| **Auth Required** | NO -- public endpoint |
| **Method** | GET |

### Parameters

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | number | Yes | Chart ID (from `/toplist` response) |
| `s` | number | No | Number of songs to return. Default: 100 |

### Common Chart IDs

| ID | Name | Update Frequency |
|---|---|---|
| 19723756 | 云音乐飙升榜 (Soaring) | Daily |
| 3779629 | 云音乐新歌榜 (New Songs) | Daily |
| 3778678 | 网易原创榜 (Original) | Daily |
| 2884035 | 云音乐热歌榜 (Hot Songs) | Daily |
| 71385702 | 云音乐说唱榜 (Rap) | Weekly |
| 1978921795 | 云音乐电音榜 (Electronic) | Weekly |
| 71384707 | 云音乐ACG榜 (ACG) | Weekly |
| 745956260 | 云音乐古典榜 (Classical) | Weekly |
| 10520166 | 云音乐国电榜 (National Electronic) | Weekly |
| 180106 | UK Official Singles Chart | Weekly |
| 60198 | Billboard Hot 100 | Weekly |
| 21845217 | Beatport Top 100 | Weekly |

### Response Structure

```json
{
  "code": 200,
  "playlist": {
    "id": 19723756,
    "name": "云音乐飙升榜",
    "coverImgUrl": "https://...",
    "description": "...",
    "trackCount": 100,
    "updateTime": 1699000000000,
    "updateFrequency": "每天更新",
    "tracks": [
      {
        "id": 123456,
        "name": "Song Name",
        "ar": [{ "id": 789, "name": "Artist" }],
        "al": {
          "id": 111,
          "name": "Album",
          "picUrl": "https://..."
        },
        "dt": 240000,
        "popularity": 100.0,
        "no": 1
      }
    ]
  },
  "privileges": [
    {
      "id": 123456,
      "maxbr": 999000,
      "pl": 320000,
      "st": 0
    }
  ]
}
```

---

## Common Song Object Structure

Most endpoints return song objects with a consistent structure:

```json
{
  "id": 123456,
  "name": "Song Name",
  "ar": [
    { "id": 789, "name": "Artist Name", "tns": [], "alias": [] }
  ],
  "al": {
    "id": 111,
    "name": "Album Name",
    "picUrl": "https://p1.music.126.net/...",
    "tns": []
  },
  "dt": 240000,
  "h": { "br": 320000, "size": 9600000 },
  "m": { "br": 192000, "size": 5760000 },
  "l": { "br": 128000, "size": 3840000 },
  "mv": 0,
  "pop": 95.0,
  "fee": 0,
  "privilege": { ... }
}
```

### Key Fields

| Field | Description |
|---|---|
| `id` | Unique song ID (used in all other APIs) |
| `name` | Song title |
| `ar` | Artists array (`ar[].name`, `ar[].id`) |
| `al` | Album object (`al.name`, `al.picUrl`, `al.id`) |
| `dt` | Duration in milliseconds |
| `h` / `m` / `l` | High/Medium/Low quality audio info (`br` = bitrate, `size` = file size) |
| `mv` | MV ID (`0` = no MV) |
| `pop` | Popularity score (0-100) |
| `fee` | `0` free, `1` VIP, `4` paid, `8` free-with-login |

---

## Request Encryption Notes

When calling the raw NetEase API directly (not through the NeteaseCloudMusicApi wrapper), request bodies must be encrypted.

### WeAPI Encryption

1. Serialize parameters to JSON.
2. AES-CBC encrypt with a fixed 16-byte key, prefixed with a random 16-byte string.
3. Reverse the AES key, then RSA-encrypt it with NetEase's public key.
4. POST two fields: `params` (AES ciphertext) and `encSecKey` (RSA ciphertext).

### Practical Implication

If you are using the **NeteaseCloudMusicApi Node.js server** as a middleware/proxy, encryption is handled automatically. You just call the wrapper endpoints listed above.

If you are calling the **raw `music.163.com` API** directly from Go or another language, you must implement WeAPI encryption or use an existing library (e.g., `pyncm` for Python, or port the encryption to Go).

---

## Summary: Auth Requirements by Endpoint

| Endpoint | Auth Required | Description |
|---|---|---|
| `/recommend/songs` | **YES** | Personalized daily song recommendations |
| `/api/v3/discovery/recommend/songs` | **YES** | Same, raw API path |
| `/recommend/resource` | **YES** | Personalized playlist recommendations |
| `/personalized` | **NO** | General recommended playlists (not personalized) |
| `/personalized/newsong` | **NO** | Recommended new songs |
| `/top/playlist/highquality` | **NO** | High quality playlists |
| `/top/playlist` | **NO** | Hot/new playlists |
| `/playlist/detail` | **NO** | Playlist details and tracks |
| `/simi/song` | **YES** | Similar songs |
| `/api/discovery/simiSong` | **YES** | Similar songs (raw path) |
| `/toplist` | **NO** | List of all charts |
| `/top/list` | **NO** | Specific chart tracks |
| `/song/detail` | **NO** | Song details by ID |
| `/login/cellphone` | N/A | Login (produces cookie) |
| `/login/email` | N/A | Login (produces cookie) |

---

## Sources

- [Binaryify/NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi) -- primary open-source Node.js wrapper
- [NeteaseCloudMusicApi Documentation](https://neteasecloudmusicapi.vercel.app/) -- deployed docs
- NetEase Cloud Music web client network inspection (music.163.com)
