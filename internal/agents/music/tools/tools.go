package musictools

import (
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music"
	"github.com/hwj123hwj/easyagent/internal/music/pref"
)

// ListOptions controls how the music-agent toolset is assembled.
type ListOptions struct {
	Router        *music.SourceRouter
	Cache         *music.Cache
	Pref          *pref.Store
	AudioBaseURL  string
	AllowedTools  []string
	BlockedTools  []string
}

// BaseToolNames returns the canonical music-agent tool names.
func BaseToolNames() []string {
	return []string{"music_search", "music_play", "music_lyrics", "music_detail", "music_playlist", "music_recommend", "music_history"}
}

// BuildList assembles the concrete music-agent toolset.
func BuildList(opts ListOptions) []agent.Tool {
	toolList := []agent.Tool{
		NewSearchTool(opts.Router),
		NewPlayTool(opts.Router, opts.Cache, opts.Pref, opts.AudioBaseURL),
		NewLyricsTool(opts.Router),
		NewDetailTool(opts.Router),
		NewPlaylistTool(opts.Router),
		NewRecommendTool(opts.Router),
		NewHistoryTool(opts.Pref),
	}
	return filterTools(toolList, opts.AllowedTools, opts.BlockedTools)
}

func filterTools(tools []agent.Tool, allowed []string, blocked []string) []agent.Tool {
	if len(allowed) == 0 && len(blocked) == 0 {
		return tools
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	blockedSet := make(map[string]bool, len(blocked))
	for _, name := range blocked {
		blockedSet[name] = true
	}
	var filtered []agent.Tool
	for _, tool := range tools {
		name := tool.Name()
		if len(allowed) > 0 && !allowedSet[name] {
			continue
		}
		if blockedSet[name] {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

// ParseSource converts a source string to music.Source, defaulting to bilibili.
func ParseSource(s string) music.Source {
	if s == "" {
		return music.SourceBilibili
	}
	return music.Source(s)
}
