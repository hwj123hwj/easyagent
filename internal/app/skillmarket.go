package app

import (
	"path/filepath"
	"sync/atomic"

	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/skillmarket"
	"github.com/hwj123hwj/easyagent/sdk/tools"
	"github.com/hwj123hwj/easyagent/sdk/util"
)

// skillMarketState holds the shared marketplace client plus the revision
// counter that triggers skill reloads in live sessions (same mechanism as MCP).
type skillMarketState struct {
	client   *skillmarket.Client
	revision atomic.Uint64
}

// initSkillMarket builds the marketplace client when enabled.
// The skills dir follows the same personal-root convention as
// runtime.DefaultSkillDirs so installed skills are picked up automatically.
func initSkillMarket(cfg config.Config) *skillMarketState {
	if !cfg.SkillMarketEnabled {
		return nil
	}
	skillsDir := cfg.SkillMarketSkillDir
	if skillsDir == "" {
		skillsDir = filepath.Join(util.HomeDir(), ".agents", "skills")
	}
	opts := []skillmarket.Option{}
	if cfg.SkillMarketBaseURL != "" {
		opts = append(opts, skillmarket.WithBaseURL(cfg.SkillMarketBaseURL))
	}
	return &skillMarketState{client: skillmarket.NewClient(skillsDir, opts...)}
}

// SkillMarket returns the shared marketplace client, or nil when disabled.
func (a *App) SkillMarket() *skillmarket.Client {
	if a.skillMarket == nil {
		return nil
	}
	return a.skillMarket.client
}

// bumpSkillMarketRevision signals live sessions to rebuild (reloading skills).
func (a *App) bumpSkillMarketRevision() {
	if a.skillMarket != nil {
		a.skillMarket.revision.Add(1)
	}
}

// BumpSkillMarketRevision is the exported hook used by the REST layer after
// marketplace installs/uninstalls.
func (a *App) BumpSkillMarketRevision() { a.bumpSkillMarketRevision() }

// skillMarketRevision feeds runtime.Dependencies.ToolRevision; always
// non-zero when the marketplace is on so the MCP counter and this one
// compose instead of shadowing each other.
func (a *App) skillMarketRevision() uint64 {
	if a.skillMarket == nil {
		return 0
	}
	return a.skillMarket.revision.Load() + 1
}

// SkillMarketTool builds the agent-facing marketplace tool. Returns nil when
// the marketplace is disabled.
func (a *App) SkillMarketTool() *tools.SkillMarketTool {
	if a.skillMarket == nil {
		return nil
	}
	return tools.NewSkillMarketTool(
		a.skillMarket.client,
		tools.WithSkillMarketOnMutation(a.bumpSkillMarketRevision),
	)
}
