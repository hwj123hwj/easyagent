package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hwj123hwj/easyagent/internal/dynamicflow"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
)

type actorConfig struct {
	Workspace, Provider, Model        string
	Persona                           dynamicflow.Persona
	RequireConfirmation               bool
	AllowedTools                      []string
	EnableBash, AllowOutsideWorkspace bool
}

func (r actorConfig) apply(cfg *config.Config) {
	cfg.Workspace = r.Workspace
	cfg.Provider = r.Provider
	if r.Provider == "openai" {
		cfg.OpenAIModel = r.Model
	} else {
		cfg.AnthropicModel = r.Model
	}
	// Both the captured allow-list and current deployment deny-list apply.
	allowed := append([]string{}, r.AllowedTools...)
	if len(cfg.AllowedTools) > 0 {
		allowed = nil
		for _, original := range r.AllowedTools {
			for _, current := range cfg.AllowedTools {
				if original == current {
					allowed = append(allowed, original)
					break
				}
			}
		}
	}
	if len(allowed) == 0 {
		allowed = []string{"__workflow_no_tools__"}
	}
	cfg.AllowedTools = allowed
	cfg.EnableBash = cfg.EnableBash && r.EnableBash
	cfg.AllowOutsideWorkspace = cfg.AllowOutsideWorkspace && r.AllowOutsideWorkspace
}

func (a *App) saveActorConfig(id string, cfg actorConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(a.sessionMgr.SessionPath(id)), "workflow-actor.json")
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}
func (a *App) readActorConfig(id string) (actorConfig, error) {
	var cfg actorConfig
	data, err := os.ReadFile(filepath.Join(filepath.Dir(a.sessionMgr.SessionPath(id)), "workflow-actor.json"))
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}
func (a *App) loadWorkflowActor(ctx context.Context, id string) (*runtime.AgentSession, error) {
	record, err := a.readActorConfig(id)
	if err != nil {
		return nil, err
	}
	cfg := a.cfg
	record.apply(&cfg)
	cfg.PromptTemplate += "\n\nWorkflow actor: " + record.Persona.Name + "\n" + record.Persona.System
	deps := a.deps()
	deps.Application = a.application
	sess, err := a.sessionStore.Load(ctx, id, runtime.AgentSessionOptions{Config: cfg, SkillDirs: a.skillDirs}, deps)
	if err == nil && record.RequireConfirmation && sess.ConfirmationCallback() == nil {
		sess.SetConfirmFunc(func(context.Context, agent.ConfirmationRequest) agent.ConfirmDecision {
			return agent.ConfirmDecision{Approved: false, Reason: "请回到原工作流会话确认操作"}
		})
	}
	return sess, err
}
