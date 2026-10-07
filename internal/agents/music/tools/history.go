package musictools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music/pref"
)

// HistoryTool lets the LLM query the user's listening history on demand.
// This is Tier 2/3: the result goes into the conversation context (not the
// system prompt), so it doesn't bloat future prompts.
type HistoryTool struct {
	pref *pref.Store
}

func NewHistoryTool(prefStore *pref.Store) *HistoryTool {
	return &HistoryTool{pref: prefStore}
}

func (t *HistoryTool) Name() string { return "music_history" }

func (t *HistoryTool) Description() string {
	return "查看用户的播放历史和偏好画像。支持查看最近播放记录、常听歌手、最常听的歌曲。" +
		"当用户说\"我最近听了什么\"\"我常听什么\"\"根据我的喜好推荐\"时使用。"
}

func (t *HistoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "number",
				"description": "返回最近播放记录的条数，不填默认 10，最大 50",
			},
		},
	}
}

func (t *HistoryTool) Validate(params json.RawMessage) (json.RawMessage, error) {
	return params, nil
}

func (t *HistoryTool) Execute(_ context.Context, params json.RawMessage, _ func(agent.PartialResult)) (agent.ToolResult, error) {
	var p struct {
		Limit float64 `json:"limit"`
	}
	_ = json.Unmarshal(params, &p)

	limit := 10
	if p.Limit > 0 {
		limit = int(p.Limit)
	}
	if limit > 50 {
		limit = 50
	}

	recent, topArtists, topSongs, totalPlays := t.pref.HistoryDetail(limit)

	if totalPlays == 0 {
		return agent.ToolResult{Content: "暂无播放历史。"}, nil
	}

	out := fmt.Sprintf("📊 播放统计（累计 %d 首）\n\n", totalPlays)

	if len(topArtists) > 0 {
		out += "🎤 常听歌手：\n"
		for i, a := range topArtists {
			out += fmt.Sprintf("  %d. %s\n", i+1, a)
		}
	}

	if len(topSongs) > 0 {
		out += "\n🎵 最常听歌曲：\n"
		for i, s := range topSongs {
			out += fmt.Sprintf("  %d. %s\n", i+1, s)
		}
	}

	out += fmt.Sprintf("\n🕒 最近 %d 首播放记录：\n", len(recent))
	for i, r := range recent {
		out += fmt.Sprintf("  %d. %s - %s（%s）\n", i+1, r.Name, r.Artist, r.Source)
	}

	return agent.ToolResult{Content: out}, nil
}
