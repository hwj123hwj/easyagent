package musictools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
)

// RecommendTool fetches recommendations and rankings.
type RecommendTool struct {
	router *music.SourceRouter
}

func NewRecommendTool(router *music.SourceRouter) *RecommendTool {
	return &RecommendTool{router: router}
}

func (t *RecommendTool) Name() string { return "music_recommend" }
func (t *RecommendTool) Description() string {
	return "获取推荐歌曲或排行榜。B站支持排行榜，网易云支持新歌和排行榜。"
}
func (t *RecommendTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type": map[string]any{
				"type":        "string",
				"description": "\"newsong\" 新歌速递（默认）；\"ranking\" 官方排行榜列表；\"top\" 热门歌曲",
				"enum":        []string{"newsong", "ranking", "top"},
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
	}
}

func (t *RecommendTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	return params, nil
}

func (t *RecommendTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		Type   string  `json:"type"`
		Limit  float64 `json:"limit"`
		Source string  `json:"source"`
	}
	_ = json.Unmarshal(params, &p)

	if p.Type == "" {
		p.Type = "newsong"
	}
	limit := 5
	if p.Limit > 0 {
		limit = int(p.Limit)
	}
	src := ParseSource(p.Source)
	ctx := context.Background()

	switch p.Type {
	case "ranking":
		return t.handleRanking(ctx, src)
	case "top":
		return t.handleTop(ctx, src, limit)
	default:
		return t.handleNewSongs(ctx, src, limit)
	}
}

func (t *RecommendTool) handleRanking(ctx context.Context, src music.Source) (agent.ToolResult, error) {
	rankings, err := t.router.GetRankings(ctx, src)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取排行榜失败: %v", err), IsError: true}, nil
	}

	header := "🏆 排行榜：\n"
	if src == music.SourceBilibili {
		header = "🏆 B站排行榜：\n"
	}
	out := header
	for i, r := range rankings {
		out += fmt.Sprintf("  %d. %s (ID: %s)\n", i+1, r.Name, r.ID)
	}
	out += "使用 music_recommend type=top 并指定 source 查看榜单内容\n"
	return agent.ToolResult{Content: out}, nil
}

func (t *RecommendTool) handleTop(ctx context.Context, src music.Source, limit int) (agent.ToolResult, error) {
	rankings, err := t.router.GetRankings(ctx, src)
	if err != nil || len(rankings) == 0 {
		return agent.ToolResult{Content: "暂无排行榜"}, nil
	}
	rankingID := rankings[0].ID

	detail, err := t.router.GetTopList(ctx, rankingID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取榜单失败: %v", err), IsError: true}, nil
	}

	songs := detail.Songs
	if limit > 0 && len(songs) > limit {
		songs = songs[:limit]
	}

	header := "🔥 热门歌曲：\n"
	if src == music.SourceBilibili {
		header = "🔥 B站热门：\n"
	}
	out := fmt.Sprintf("  来源：%s\n\n", detail.Playlist.Name)
	for i, s := range songs {
		out += fmt.Sprintf("  %d. %s - %s [%s]  ID: %s\n", i+1, s.Name, s.Artist, formatDuration(s.Duration), s.ID)
	}
	return agent.ToolResult{Content: header + out}, nil
}

func (t *RecommendTool) handleNewSongs(ctx context.Context, src music.Source, limit int) (agent.ToolResult, error) {
	songs, err := t.router.GetNewSongs(ctx, limit, src)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("获取新歌失败: %v", err), IsError: true}, nil
	}
	if len(songs) == 0 {
		return agent.ToolResult{Content: "暂无新歌推荐"}, nil
	}

	header := "🆕 新歌推荐：\n"
	if src == music.SourceBilibili {
		header = "🆕 B站音乐热门：\n"
	}
	out := header
	for i, s := range songs {
		out += fmt.Sprintf("  %d. %s - %s [%s]  ID: %s\n", i+1, s.Name, s.Artist, formatDuration(s.Duration), s.ID)
	}
	return agent.ToolResult{Content: out}, nil
}
