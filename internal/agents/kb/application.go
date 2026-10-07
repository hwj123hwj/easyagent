package kb

import (
	"log/slog"
	"path/filepath"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	kbprompt "github.com/hwj123hwj/easyagent/internal/agents/kb/prompt"
	kbtools "github.com/hwj123hwj/easyagent/internal/agents/kb/tools"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/internal/kbvector"
	"github.com/hwj123hwj/easyagent/internal/profile"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

// KBApplication implements runtime.Application for the knowledge-base agent.
type KBApplication struct {
	Cfg      config.Config
	RepoPath string         // path to agent-lessons repo, e.g. ~/agent-lessons
	Profile  *profile.Store // unified user profile for personalization
}

// NewKBApplication creates a new KBApplication.
func NewKBApplication(cfg config.Config, repoPath string) KBApplication {
	return KBApplication{
		Cfg:      cfg,
		RepoPath: repoPath,
	}
}

// NewKBApplicationWithProfile creates a KBApplication with a unified profile store.
func NewKBApplicationWithProfile(cfg config.Config, repoPath string, profileStore *profile.Store) KBApplication {
	return KBApplication{
		Cfg:      cfg,
		RepoPath: repoPath,
		Profile:  profileStore,
	}
}

// BuildTools assembles the kb-agent toolset.
// If embedding API key is configured, uses HybridSearcher (keyword + vector).
// Otherwise falls back to KeywordSearcher (pure keyword).
func (a KBApplication) BuildTools(opts runtime.ToolBuildOptions) []agent.Tool {
	searchStrategy := a.buildSearchStrategy()

	return kbtools.BuildList(kbtools.ListOptions{
		RepoPath:         a.RepoPath,
		SearchStrategy:   searchStrategy,
		AllowedTools:     opts.AllowedTools,
		BlockedTools:     opts.BlockedTools,
	})
}

// buildSearchStrategy creates the search strategy based on config.
// When embedding API is available, returns HybridSearcher; else KeywordSearcher.
func (a KBApplication) buildSearchStrategy() kbtools.SearchStrategy {
	if a.Cfg.KBEmbeddingAPIKey == "" {
		return kbtools.KeywordSearcher{} // fallback: pure keyword
	}

	// Vector search available — create HybridSearcher
	client := kbvector.NewEmbeddingClient(
		a.Cfg.KBEmbeddingAPIKey,
		a.Cfg.KBEmbeddingBaseURL,
		a.Cfg.KBEmbeddingModel,
	)
	vectorPath := filepath.Join(a.Cfg.DataDir, "kb_vectors.json")
	store := kbvector.NewStore(vectorPath)

	slog.Info("KB vector search enabled", "model", a.Cfg.KBEmbeddingModel, "store", vectorPath)

	return kbvector.NewHybridSearcher(client, store)
}

// BuildPrompt constructs the kb-agent system prompt.
func (a KBApplication) BuildPrompt(opts runtime.PromptBuildOptions, profile, goal string) string {
	return kbprompt.BuildSystemPrompt(kbprompt.Options{
		Tools:      opts.Tools,
		Goal:       goal,
		RepoPath:   a.RepoPath,
		UserProfile: a.Profile,
	})
}

// NewSessionExt creates a per-session KBSessionExt.
func (a KBApplication) NewSessionExt() runtime.SessionExt {
	return NewKBSessionExt()
}

// ToolNames returns the canonical kb-agent tool names.
// keepPublished is ignored (KB agent doesn't distinguish published/unpublished tools).
func (KBApplication) ToolNames(_ bool) []string {
	return kbtools.BaseToolNames()
}

// Verify interface compliance at compile time.
var _ runtime.Application = KBApplication{}
