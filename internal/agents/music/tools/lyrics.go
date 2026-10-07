package musictools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
)

// LyricsTool fetches LRC lyrics.
type LyricsTool struct {
	router *music.SourceRouter
}

func NewLyricsTool(router *music.SourceRouter) *LyricsTool {
	return &LyricsTool{router: router}
}

func (t *LyricsTool) Name() string { return "music_lyrics" }
func (t *LyricsTool) Description() string {
	return "获取歌词。返回 LRC 格式歌词。B站暂不支持歌词。"
}
func (t *LyricsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"song_id": map[string]any{
				"type":        "string",
				"description": "复合歌曲 ID，如 \"netease:576466\" 或 \"bilibili:BV1qD4y1U7fs\"",
			},
		},
		"required": []string{"song_id"},
	}
}

func (t *LyricsTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SongID string `json:"song_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if p.SongID == "" {
		return nil, fmt.Errorf("song_id is required")
	}
	return params, nil
}

func (t *LyricsTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		SongID string `json:"song_id"`
	}
	_ = json.Unmarshal(params, &p)

	lyrics, err := t.router.GetLyrics(context.Background(), p.SongID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取歌词失败: %v", err), IsError: true}, nil
	}

	if lyrics.LRC == "" {
		return agent.ToolResult{Content: "暂无歌词"}, nil
	}

	out := "🎤 歌词：\n"
	if lyrics.TransLRC != "" {
		out += "原词 + 翻译：\n"
	}
	out += lyrics.LRC
	if lyrics.TransLRC != "" {
		out += "\n\n翻译：\n" + lyrics.TransLRC
	}
	return agent.ToolResult{Content: out}, nil
}
