package musictools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
)

// SearchTool searches songs across multiple sources.
type SearchTool struct {
	router *music.SourceRouter
}

func NewSearchTool(router *music.SourceRouter) *SearchTool {
	return &SearchTool{router: router}
}

func (t *SearchTool) Name() string { return "music_search" }
func (t *SearchTool) Description() string {
	return "搜索歌曲，返回匹配的歌曲列表，含名称、歌手、时长。支持网易云和B站。"
}
func (t *SearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "搜索关键词",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "返回数量，不填默认 5",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "播放源：\"netease\"（网易云，默认）或 \"bilibili\"（B站）",
				"enum":        []string{"netease", "bilibili"},
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if p.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	return params, nil
}

func (t *SearchTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		Query  string  `json:"query"`
		Limit  float64 `json:"limit"`
		Source string  `json:"source"`
	}
	_ = json.Unmarshal(params, &p)

	limit := 5
	if p.Limit > 0 {
		limit = int(p.Limit)
	}
	src := ParseSource(p.Source)

	result, err := t.router.Search(context.Background(), p.Query, limit, src)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("Search failed: %v", err), IsError: true}, nil
	}
	if len(result.Songs) == 0 {
		return agent.ToolResult{Content: "没有找到匹配的歌曲", IsError: true}, nil
	}

	header := "🎵 搜索结果 (网易云)"
	if src == music.SourceBilibili {
		header = "🎵 搜索结果 (B站)"
	}

	out := header + "：\n"
	for i, s := range result.Songs {
		out += fmt.Sprintf("  %d. %s - %s [%s]  ID: %s\n", i+1, s.Name, s.Artist, formatDuration(s.Duration), s.ID)
	}
	out += "使用 music_play 工具播放，参数 song_id 填括号里的 ID 字符串\n"
	return agent.ToolResult{Content: out}, nil
}

func formatDuration(ms int) string {
	s := ms / 1000
	m := s / 60
	s = s % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
