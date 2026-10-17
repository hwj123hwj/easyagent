package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/extensions"
	"github.com/hwj123hwj/easyagent/sdk/sessionmgr"
	"github.com/stretchr/testify/require"
)

func TestSessionModelSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	manager := sessionmgr.NewManager(t.TempDir())
	cfg := config.Default()
	cfg.Provider = "openai"
	cfg.OpenAIModel = "initial"
	deps := Dependencies{SessionMgr: manager, Registry: providers.NewRegistry(), ExtRegistry: extensions.NewRegistry(), Application: &confirmationTestApplication{}}
	s, err := NewAgentSession(ctx, AgentSessionOptions{Config: cfg}, deps)
	require.NoError(t, err)
	id := s.SessionID()
	require.NoError(t, manager.SaveMeta(id, "/test/project", "coding"))
	title := "keep title"
	require.NoError(t, manager.UpdatePreferences(id, sessionmgr.PreferencePatch{Title: &title}))
	require.NoError(t, s.SwitchModel(ctx, "chosen", "anthropic"))
	require.NoError(t, s.Close())
	s, err = NewAgentSession(ctx, AgentSessionOptions{Config: cfg, SessionID: id}, deps)
	require.NoError(t, err)
	defer s.Close()
	require.Equal(t, "chosen", s.cfg.AnthropicModel)
	require.Equal(t, "anthropic", s.cfg.Provider)
	workspace, application := manager.Metadata(id)
	require.Equal(t, "/test/project", workspace)
	require.Equal(t, "coding", application)
	metaPath := filepath.Join(manager.SessionsDir(), id, "meta.json")
	meta, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Contains(t, string(meta), title)
	// A failed durable write must not report success or replace the active agent.
	require.NoError(t, os.Rename(metaPath, metaPath+".backup"))
	require.NoError(t, os.Mkdir(metaPath, 0700))
	old := s.agent
	require.Error(t, s.SwitchModel(ctx, "unsaved", "openai"))
	require.Same(t, old, s.agent)
	require.Equal(t, "anthropic", s.cfg.Provider)
	require.Equal(t, "chosen", s.cfg.AnthropicModel)
}

func TestInitialSessionModelSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	manager := sessionmgr.NewManager(t.TempDir())
	cfg := config.Default()
	cfg.Provider = "openai"
	cfg.OpenAIModel = "initial"
	deps := Dependencies{SessionMgr: manager, Registry: providers.NewRegistry(), ExtRegistry: extensions.NewRegistry(), Application: &confirmationTestApplication{}}
	s, err := NewAgentSession(ctx, AgentSessionOptions{Config: cfg}, deps)
	require.NoError(t, err)
	id := s.SessionID()
	require.NoError(t, s.Close())
	cfg.OpenAIModel = "new-default"
	s, err = NewAgentSession(ctx, AgentSessionOptions{Config: cfg, SessionID: id}, deps)
	require.NoError(t, err)
	defer s.Close()
	require.Equal(t, "initial", s.cfg.OpenAIModel)
}
