package music

import (
	"path/filepath"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	musicprompt "github.com/hwj123hwj/easyagent/internal/agents/music/prompt"
	musictools "github.com/hwj123hwj/easyagent/internal/agents/music/tools"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/internal/music"
	"github.com/hwj123hwj/easyagent/internal/music/pref"
	"github.com/hwj123hwj/easyagent/internal/profile"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

// MusicApplication implements runtime.Application for the music-agent.
type MusicApplication struct {
	Cfg     config.Config
	Router  *music.SourceRouter
	Cache   *music.Cache
	Pref    *pref.Store
	Profile *profile.Store // unified user profile
}

// NewMusicApplication creates a new MusicApplication with multi-source support.
// A preference store is created in cfg.DataDir for persistent listening history.
// If profileStore is non-nil, music prefs are automatically synced to it.
func NewMusicApplication(cfg config.Config, router *music.SourceRouter, cache *music.Cache, profileStore *profile.Store) MusicApplication {
	prefPath := filepath.Join(cfg.DataDir, "music_pref.json")
	prefStore := pref.NewStore(prefPath)

	// Connect music pref store to unified profile for cross-agent sharing
	if profileStore != nil {
		prefStore.SetProfileSyncer(profileStore)
	}

	return MusicApplication{
		Cfg:     cfg,
		Router:  router,
		Cache:   cache,
		Pref:    prefStore,
		Profile: profileStore,
	}
}

// BuildTools assembles the music-agent toolset.
// audioBaseURL uses a relative path so the frontend resolves it against the
// current server — works correctly on mobile (Capacitor), desktop, and web.
func (a MusicApplication) BuildTools(opts runtime.ToolBuildOptions) []agent.Tool {
	audioBaseURL := "/music/audio"
	return musictools.BuildList(musictools.ListOptions{
		Router:        a.Router,
		Cache:         a.Cache,
		Pref:          a.Pref,
		AudioBaseURL:  audioBaseURL,
		AllowedTools:  opts.AllowedTools,
		BlockedTools:  opts.BlockedTools,
	})
}

// BuildPrompt constructs the music-agent system prompt.
func (a MusicApplication) BuildPrompt(opts runtime.PromptBuildOptions, profile, goal string) string {
	return musicprompt.BuildSystemPrompt(musicprompt.Options{
		Tools:   opts.Tools,
		Goal:    goal,
		Pref:    a.Pref,
		Profile: a.Profile, // unified profile (supersedes music-only summary when present)
	})
}

// NewSessionExt creates a per-session MusicSessionExt.
func (a MusicApplication) NewSessionExt() runtime.SessionExt {
	return NewMusicSessionExt()
}

// ToolNames returns the canonical music-agent tool names.
func (MusicApplication) ToolNames(_ bool) []string {
	return musictools.BaseToolNames()
}

// Verify interface compliance at compile time.
var _ runtime.Application = MusicApplication{}
