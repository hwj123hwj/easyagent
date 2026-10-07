package musictools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
)

// DetailTool fetches song details.
type DetailTool struct {
	router *music.SourceRouter
}

func NewDetailTool(router *music.SourceRouter) *DetailTool {
	return &DetailTool{router: router}
}

func (t *DetailTool) Name() string { return "music_detail" }
func (t *DetailTool) Description() string {
	return "获取歌曲详情，包括封面、歌手、时长等。"
}
func (t *DetailTool) Parameters() map[string]any {
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

func (t *DetailTool) Validate(params json.RawMessage) (json.RawMessage, error) {
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

func (t *DetailTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		SongID string `json:"song_id"`
	}
	_ = json.Unmarshal(params, &p)

	s, rawID, err := t.router.ByCompositeID(p.SongID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("Invalid song_id: %v", err), IsError: true}, nil
	}

	detail, err := s.GetSongByID(context.Background(), rawID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取详情失败: %v", err), IsError: true}, nil
	}

	out := "🎶 歌曲详情：\n"
	out += fmt.Sprintf("  曲名：%s\n", detail.Name)
	out += fmt.Sprintf("  歌手：%s\n", detail.Artist)
	if detail.AlbumName != "" {
		out += fmt.Sprintf("  专辑：%s\n", detail.AlbumName)
	}
	out += fmt.Sprintf("  时长：%s\n", formatDuration(detail.Duration))
	out += fmt.Sprintf("  ID：%s\n", detail.ID)
	if detail.AlbumCover != "" {
		out += fmt.Sprintf("  封面：%s\n", detail.AlbumCover)
	}
	if detail.Source == music.SourceBilibili {
		out += "  来源：B站\n"
	}
	return agent.ToolResult{Content: out}, nil
}
