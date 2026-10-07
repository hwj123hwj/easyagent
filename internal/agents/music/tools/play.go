package musictools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
	"github.com/hwj123hwj/easyagent/internal/music/pref"
)

// PlayTool plays a song from any source with cross-source fallback.
type PlayTool struct {
	router       *music.SourceRouter
	cache        *music.Cache
	pref         *pref.Store
	audioBaseURL string
}

// PlayDetails is the structured result for the frontend player.
type PlayDetails struct {
	SongID         string `json:"song_id"`         // Composite ID
	SongName       string `json:"song_name"`
	Artist         string `json:"artist"`
	ProxyURL       string `json:"proxy_url"`
	Duration       int    `json:"duration"`        // Duration in seconds (0 if unknown)
	Source         string `json:"source"`          // "netease" or "bilibili"
	IsFallback     bool   `json:"is_fallback"`     // true if fell back to B站
	OriginalIntent string `json:"original_intent,omitempty"`
}

func NewPlayTool(router *music.SourceRouter, cache *music.Cache, prefStore *pref.Store, audioBaseURL string) *PlayTool {
	return &PlayTool{router: router, cache: cache, pref: prefStore, audioBaseURL: audioBaseURL}
}

func (t *PlayTool) Name() string { return "music_play" }
func (t *PlayTool) Description() string {
	return "播放歌曲。提供 song_id 或 query。默认从B站搜索播放，B站不可用时降级到网易云。"
}
func (t *PlayTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"song_id": map[string]any{
				"type":        "string",
				"description": "复合歌曲 ID，如 \"bilibili:BV1xx\" 或 \"netease:576466\"。与 query 二选一。",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "搜索词（当 song_id 为空时用于搜索播放，如 '周杰伦 晴天'）",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "播放源：\"bilibili\"（B站，默认）或 \"netease\"（网易云）",
				"enum":        []string{"bilibili", "netease"},
			},
		},
	}
}

func (t *PlayTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SongID string `json:"song_id"`
		Query  string `json:"query"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if p.SongID == "" && p.Query == "" {
		return nil, fmt.Errorf("provide either song_id or query")
	}
	return params, nil
}

func (t *PlayTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		SongID string `json:"song_id"`
		Query  string `json:"query"`
		Source string `json:"source"`
	}
	_ = json.Unmarshal(params, &p)
	ctx := context.Background()
	src := ParseSource(p.Source)

	// Mode 1: Direct song_id
	if p.SongID != "" {
		return t.playByID(ctx, p.SongID, false)
	}

	// Mode 2: Query — search and try, with cross-source fallback
	return t.playByQuery(ctx, p.Query, src)
}

func (t *PlayTool) playByID(ctx context.Context, songID string, isFallback bool) (agent.ToolResult, error) {
	s, rawID, err := t.router.ByCompositeID(songID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("Invalid song_id: %v", err), IsError: true}, nil
	}

	detail, err := s.GetSongByID(ctx, rawID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取歌曲信息失败: %v", err), IsError: true}, nil
	}

	audioURL, err := s.GetAudioURL(ctx, rawID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取播放链接失败: %v", err), IsError: true}, nil
	}

	// Cache the audio URL
	src, _ := music.ParseSourceID(songID)
	cacheKey := music.AudioKey(string(src), rawID)
	t.cache.Set(cacheKey, audioURL, music.TTLAudio)

	// Record play in preference store for personalized recommendations
	if t.pref != nil {
		t.pref.Record(songID, detail.Name, detail.Artist, string(detail.Source))
	}

	proxyURL := t.audioBaseURL + "/" + encodeCompositeID(songID)

	out := "🎵 正在播放：\n"
	out += fmt.Sprintf("  曲名：%s\n", detail.Name)
	out += fmt.Sprintf("  歌手：%s\n", detail.Artist)
	out += fmt.Sprintf("  时长：%s\n", formatDuration(detail.Duration))
	if detail.Source == music.SourceBilibili {
		out += "  来源：B站\n"
	}
	if isFallback {
		out += "  ⚠️ B站播放失败，已自动切换到网易云\n"
	}
	out += fmt.Sprintf("\n播放链接：%s\n", proxyURL)
	out += fmt.Sprintf("封面：%s\n", detail.AlbumCover)

	return agent.ToolResult{
		Content: out,
		Details: PlayDetails{
			SongID:       songID,
			SongName:     detail.Name,
			Artist:       detail.Artist,
			ProxyURL:     proxyURL,
			Duration:     detail.Duration / 1000, // ms → seconds
			Source:       string(detail.Source),
			IsFallback:   isFallback,
		},
	}, nil
}

func (t *PlayTool) playByQuery(ctx context.Context, query string, src music.Source) (agent.ToolResult, error) {
	// Search in the preferred source
	result, err := t.router.Search(ctx, query, 10, src)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("搜索失败: %v", err), IsError: true}, nil
	}
	if len(result.Songs) == 0 {
		return agent.ToolResult{Content: "没有找到匹配的歌曲", IsError: true}, nil
	}

	// Try each result in the preferred source (max 5 candidates)
	maxTry := 5
	if len(result.Songs) < maxTry {
		maxTry = len(result.Songs)
	}
	for i := 0; i < maxTry; i++ {
		song := result.Songs[i]
		res, err := t.playByID(ctx, song.ID, false)
		if err == nil && !res.IsError {
			return res, nil
		}
	}

	// All failed in preferred source — try cross-source fallback
	if src == music.SourceBilibili {
		// Fallback to NetEase
		neteaseResult, neteaseErr := t.router.Search(ctx, query, 5, music.SourceNetease)
		if neteaseErr == nil && len(neteaseResult.Songs) > 0 {
			// Try up to 3 netease results
			maxFallback := 3
			if len(neteaseResult.Songs) < maxFallback {
				maxFallback = len(neteaseResult.Songs)
			}
			for i := 0; i < maxFallback; i++ {
				res, err := t.playByID(ctx, neteaseResult.Songs[i].ID, true)
				if err == nil && !res.IsError {
					return res, nil
				}
			}
		}
	}

	return agent.ToolResult{
		Content: "播放失败：所有候选歌曲均不可播放",
		IsError: true,
	}, nil
}

func encodeCompositeID(id string) string {
	return strings.ReplaceAll(id, ":", "_")
}
