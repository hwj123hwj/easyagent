package musictools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
)

// PlaylistTool fetches playlist details.
type PlaylistTool struct {
	router *music.SourceRouter
}

func NewPlaylistTool(router *music.SourceRouter) *PlaylistTool {
	return &PlaylistTool{router: router}
}

func (t *PlaylistTool) Name() string { return "music_playlist" }
func (t *PlaylistTool) Description() string {
	return "获取歌单详情（歌单内歌曲列表）。B站暂不支持歌单。"
}
func (t *PlaylistTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"playlist_id": map[string]any{
				"type":        "string",
				"description": "复合歌单 ID，如 \"netease:2438542821\"",
			},
		},
		"required": []string{"playlist_id"},
	}
}

func (t *PlaylistTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		PlaylistID string `json:"playlist_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if p.PlaylistID == "" {
		return nil, fmt.Errorf("playlist_id is required")
	}
	return params, nil
}

func (t *PlaylistTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		PlaylistID string `json:"playlist_id"`
	}
	_ = json.Unmarshal(params, &p)

	detail, err := t.router.GetPlaylistDetail(context.Background(), p.PlaylistID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取歌单失败: %v", err), IsError: true}, nil
	}

	out := fmt.Sprintf("📋 歌单：%s\n", detail.Playlist.Name)
	if detail.Playlist.Description != "" {
		out += fmt.Sprintf("  %s\n", truncate(detail.Playlist.Description, 80))
	}
	out += fmt.Sprintf("  曲数：%d，播放：%s\n", detail.Playlist.TrackCount, formatPlayCount(detail.Playlist.PlayCount))
	if detail.Playlist.Creator != "" {
		out += fmt.Sprintf("  创建者：%s\n", detail.Playlist.Creator)
	}
	out += "\n"
	for i, s := range detail.Songs {
		out += fmt.Sprintf("  %d. %s - %s [%s]  ID: %s\n", i+1, s.Name, s.Artist, formatDuration(s.Duration), s.ID)
	}
	if len(detail.Songs) < detail.Playlist.TrackCount {
		out += fmt.Sprintf("\n还有 %d 首未显示\n", detail.Playlist.TrackCount-len(detail.Songs))
	}
	return agent.ToolResult{Content: out}, nil
}

func formatPlayCount(count int64) string {
	if count >= 100000000 {
		return fmt.Sprintf("%.1f亿", float64(count)/100000000)
	}
	if count >= 10000 {
		return fmt.Sprintf("%.1f万", float64(count)/10000)
	}
	return fmt.Sprintf("%d", count)
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
