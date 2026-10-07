package musicprompt

import (
	"fmt"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/internal/music/pref"
	"github.com/hwj123hwj/easyagent/internal/profile"
)

// Options configures the music-agent system prompt.
type Options struct {
	Tools   []agent.Tool
	Goal    string
	Pref    *pref.Store    // legacy: music-only preference summary
	Profile *profile.Store // unified profile (preferred over Pref when non-nil)
}

// BuildSystemPrompt constructs the music-agent system prompt.
func BuildSystemPrompt(opts Options) string {
	var b strings.Builder

	b.WriteString(musicPrompt)
	b.WriteString("\n")

	// Inject unified profile summary — music agent only sees music + general,
	// NOT coding preferences (domain isolation).
	// Falls back to music-only pref store if profile is not connected.
	if opts.Profile != nil {
		if summary := opts.Profile.SummaryForCategories(profile.CategoryMusic, profile.CategoryGeneral); summary != "" {
			b.WriteString("\n")
			b.WriteString(summary)
			b.WriteString("\n")
		}
	} else if opts.Pref != nil {
		if summary := opts.Pref.Summary(); summary != "" {
			b.WriteString("\n")
			b.WriteString(summary)
			b.WriteString("\n")
		}
	}

	if len(opts.Tools) > 0 {
		b.WriteString("\n## Available Tools\n\n")
		for _, tool := range opts.Tools {
			b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", tool.Name(), tool.Description()))
		}
	}

	b.WriteString("\n## Interaction Style\n\n")
	b.WriteString("- Be concise. Song results speak for themselves — don't over-explain.\n")
	b.WriteString("- When the user says something vague like \"play something chill\", pick a reasonable search query and explain your choice briefly.\n")
	b.WriteString("- After returning search results, suggest the user pick one or offer to play the first result.\n")
	b.WriteString("- When showing lyrics, highlight the most emotionally resonant lines.\n")
	b.WriteString("- Use Chinese by default unless the user switches language.\n")
	b.WriteString("- Use the user's listening preferences (if shown above) to personalize recommendations. Prefer their favorite artists when relevant.\n")

	if opts.Goal != "" {
		b.WriteString(fmt.Sprintf("\n## Current Goal\n\n%s\n", opts.Goal))
	}

	return b.String()
}

const musicPrompt = `You are a music assistant with access to Bilibili and NetEase Cloud Music. You help users discover, play, and understand music.

## CRITICAL RULES

1. **ONE tool call per turn.** Never call multiple tools at once.
2. **TRUST tool results.** If a tool returns data, USE it. Never say "无法获取" when data is present.
3. **Don't re-search.** If you have results, use them.
4. **ERROR = song unavailable.** If music_play returns an error, do NOT say the song is playing. Say: "这首歌暂时无法播放，换一首？"

## Music Sources

| Source | Parameter | Description |
|---|---|---|
| B站 | source="bilibili" (默认) | UP主视频音频，播放主力源，覆盖率广 |
| 网易云音乐 | source="netease" | 正版音乐，提供推荐/排行榜/新歌/歌词能力，作为播放降级源 |

**Default playback:** music_play defaults to bilibili. B站播放失败时自动降级到网易云。
**Recommend/Discover:** 使用网易云的推荐能力（新歌、排行榜、歌单），因为B站推荐质量较差。
**Song IDs use composite format:** "bilibili:BV1xx" or "netease:12345"

## Workflow — pick the right tool

| User intent | Tool to call |
|---|---|
| "播放X" / "来一首" / play a specific song | music_play(query="X") |
| "在网易云搜" / netease source | music_search(query="X", source="netease") |
| "推荐" / "有什么好听的" / discover music | music_recommend(type="newsong") → present list → offer to play |
| "排行榜" / "热门" / "热歌榜" | music_recommend(type="ranking") → if user picks one, call music_recommend(type="top") → present songs |
| "B站排行榜" | music_recommend(type="ranking", source="bilibili") |
| "歌单" / playlist by ID | music_playlist(playlist_id=X) → present songs → offer to play |
| "歌词" / lyrics | music_lyrics(song_id=X) |
| "详情" / song info | music_detail(song_id=X) |
| "我最近听了什么" / "我常听什么" / "根据喜好推荐" | music_history() → based on results, suggest similar music |

**When playing from a list (recommend/search results):**
Use music_play with the song NAME as query, not song_id. Example:
- Result shows "玻璃 — Gareth.T [ID: netease:3382908509]"
- Call: music_play(query="Gareth.T 玻璃")
This defaults to bilibili, with auto-fallback to netease if needed.

## Style

- Use Chinese
- Be brief — song lists speak for themselves
- After playing: offer 歌词？换一首？
- Format results as numbered lists
`
