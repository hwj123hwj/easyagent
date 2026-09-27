package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/ai/models"
	"github.com/hwj123hwj/easyagent/sdk/ai/providers"
	"github.com/hwj123hwj/easyagent/sdk/compaction"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/extensions"
	"github.com/hwj123hwj/easyagent/sdk/operations"
	"github.com/hwj123hwj/easyagent/sdk/prompt"
	"github.com/hwj123hwj/easyagent/sdk/session"
	"github.com/hwj123hwj/easyagent/sdk/sessionmgr"
	"github.com/hwj123hwj/easyagent/sdk/skill"
	"github.com/hwj123hwj/easyagent/sdk/util"
)

// Dependencies holds the shared dependencies needed to create an AgentSession.
// These are provided by the App layer and shared across sessions.
type Dependencies struct {
	Registry        *providers.Registry
	SessionMgr      *sessionmgr.Manager
	ExtRegistry     *extensions.Registry
	Application     Application
	ExternalTools   []agent.ExternalToolDef
	BuildOperations func(cfg config.Config, workspace string) *operations.Operations
}

// AgentSessionOptions holds the options for creating a new AgentSession.
type AgentSessionOptions struct {
	SessionID string
	Config    config.Config
	SkillDirs []string
}

// AgentSession is the central runtime abstraction for agent sessions.
// It unifies agent lifecycle, session, events, compaction, and branch navigation.
// All modes (interactive, print, serve) depend on this object.
// Application-specific state (profile, goal) is delegated to SessionExt.
type AgentSession struct {
	agent       *agent.Agent
	session     *session.Session
	sessionID   string
	sessionMgr  *sessionmgr.Manager
	cfg         config.Config
	extRegistry *extensions.Registry
	sessionPath string
	deps        Dependencies
	skillDirs   []string
	application Application
	confirmFunc agent.ConfirmFunc // 可选：危险工具执行前的确认回调（interactive 注入，serve/feishu 留空=放行）
	// confirmEnabled 控制已注入回调的生效状态（/confirm on|off 运行时切换；
	// auto_approve/-y 只是它的初始值）。回调本身始终包装经此开关。
	confirmEnabled atomic.Bool
	ext            SessionExt
}

// NewAgentSession creates a new AgentSession.
// If sessionID is empty, a new session is created.
// If sessionID is provided, an existing session is loaded.
func NewAgentSession(ctx context.Context, opts AgentSessionOptions, deps Dependencies) (*AgentSession, error) {
	s := &AgentSession{
		cfg:         opts.Config,
		sessionMgr:  deps.SessionMgr,
		extRegistry: deps.ExtRegistry,
		deps:        deps,
		skillDirs:   opts.SkillDirs,
	}

	s.application = deps.Application

	// Create per-session extension (holds application-specific state like profile/goal)
	if deps.Application != nil {
		s.ext = deps.Application.NewSessionExt()
		if cse, ok := s.ext.(interface{ SetRebuild(func() error) }); ok {
			cse.SetRebuild(func() error {
				// Use Background instead of the creation context: profile/goal
				// changes can happen long after the request that created this
				// session has completed and its context been canceled.
				_, err := s.rebuildAgent(context.Background(), s.deps.Registry, s.skillDirs)
				return err
			})
		}
	}

	var err error

	if opts.SessionID != "" && s.sessionMgr.Exists(opts.SessionID) {
		// Load existing session
		s.sessionID = opts.SessionID
		s.session, s.sessionPath, err = s.sessionMgr.Open(ctx, opts.SessionID)
		if err != nil {
			return nil, fmt.Errorf("open session %q: %w", opts.SessionID, err)
		}
		slog.Info("loaded existing session", "id", opts.SessionID)
	} else {
		// Create new session
		s.sessionID, s.sessionPath, err = s.sessionMgr.Create(ctx)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		// Re-open to get Session object
		s.session, s.sessionPath, err = s.sessionMgr.Open(ctx, s.sessionID)
		if err != nil {
			return nil, fmt.Errorf("open new session: %w", err)
		}
		slog.Info("created new session", "id", s.sessionID)
	}

	// Build the agent
	ag, err := s.buildAgent(ctx, deps.Registry, opts.SkillDirs)
	if err != nil {
		return nil, fmt.Errorf("build agent: %w", err)
	}
	s.agent = ag

	return s, nil
}

// Prompt sends a message and waits for the complete response.
func (s *AgentSession) Prompt(ctx context.Context, input string) (ai.AssistantMessage, error) {
	return s.agent.Prompt(ctx, ai.NewTextUserMessage(input))
}

// PromptStream sends a message and returns an event channel for streaming.
func (s *AgentSession) PromptStream(ctx context.Context, input string) (<-chan agent.AgentStreamEvent, error) {
	return s.agent.PromptStream(ctx, ai.NewTextUserMessage(input))
}

// SessionID returns the current session ID.
func (s *AgentSession) SessionID() string {
	return s.sessionID
}

// Session returns the underlying session object.
func (s *AgentSession) Session() *session.Session {
	return s.session
}

// Agent returns the underlying agent.
func (s *AgentSession) Agent() *agent.Agent {
	return s.agent
}

// Config returns the current config.
func (s *AgentSession) Config() config.Config {
	return s.cfg
}

// Workspace returns the session's working directory.
func (s *AgentSession) Workspace() string {
	return s.cfg.Workspace
}

// ModelInfo returns the provider name and model ID.
func (s *AgentSession) ModelInfo() (string, string) {
	modelID := s.cfg.AnthropicModel
	providerName := s.cfg.Provider
	if providerName == "openai" {
		modelID = s.cfg.OpenAIModel
	}
	if modelID == "" {
		providerName = ""
		modelID = ""
	}
	return providerName, modelID
}

// ToolNames returns the names of tools available in the current session.
func (s *AgentSession) ToolNames() []string {
	if s.agent == nil {
		return nil
	}
	return s.agent.ToolNames()
}

// SwitchModel changes the model (and optionally the provider) at runtime
// and rebuilds the agent. The change takes effect for subsequent prompts.
// If provider is non-empty, both provider and model are switched;
// otherwise only the model field for the current provider is updated.
func (s *AgentSession) SwitchModel(ctx context.Context, modelID string, provider string) error {
	// If provider is specified, switch to it
	if provider != "" && provider != s.cfg.Provider {
		s.cfg.Provider = provider
	}

	providerName := s.cfg.Provider
	switch providerName {
	case "openai":
		s.cfg.OpenAIModel = modelID
	case "anthropic":
		s.cfg.AnthropicModel = modelID
	default:
		s.cfg.Provider = "openai"
		s.cfg.OpenAIModel = modelID
		providerName = "openai"
	}

	// Rebuild agent with new model
	ag, err := s.buildAgent(ctx, s.deps.Registry, s.skillDirs)
	if err != nil {
		return fmt.Errorf("rebuild agent with model %q: %w", modelID, err)
	}
	s.agent = ag

	slog.Info("switched model", "provider", providerName, "model", modelID)
	return nil
}

// Compact manually triggers context compaction.
// It generates an LLM summary of older messages, persists it to session storage,
// and returns the summary along with trimming stats.
func (s *AgentSession) Compact(ctx context.Context, customInstructions string) (string, int, int, error) {
	if s.agent == nil {
		return "", 0, 0, fmt.Errorf("no active agent")
	}
	return s.agent.CompactNow(ctx, customInstructions)
}

// Profile returns the current profile name.
// Delegates to SessionExt if available.
func (s *AgentSession) Profile() string {
	if s.ext != nil {
		return s.ext.Profile()
	}
	return ""
}

// SwitchProfile changes the active profile and rebuilds the agent
// so that the new profile's system prompt takes effect immediately.
// Delegates to SessionExt if available.
func (s *AgentSession) SwitchProfile(ctx context.Context, profile string) error {
	if s.ext == nil {
		return fmt.Errorf("profile switching not supported")
	}
	return s.ext.SwitchProfile(ctx, profile)
}

// Goal returns the current session goal.
// Delegates to SessionExt if available.
func (s *AgentSession) Goal() string {
	if s.ext != nil {
		return s.ext.Goal()
	}
	return ""
}

// SetGoal sets the current session goal and rebuilds the agent
// so the goal is injected into the system prompt immediately.
// Delegates to SessionExt if available.
func (s *AgentSession) SetGoal(goal string) {
	if s.ext == nil {
		return
	}
	s.ext.SetGoal(goal)
}

// ClearGoal clears the current session goal and rebuilds the agent.
// Delegates to SessionExt if available.
func (s *AgentSession) ClearGoal() {
	if s.ext == nil {
		return
	}
	s.ext.ClearGoal()
}

// MoveTo navigates the session to a specific entry (branch navigation).
func (s *AgentSession) MoveTo(ctx context.Context, entryID string, summary string) error {
	if s.session == nil {
		return fmt.Errorf("no active session")
	}
	return s.session.MoveTo(ctx, entryID, summary)
}

// Close cleans up resources.
func (s *AgentSession) Close() error {
	if s.session != nil && s.session.Storage() != nil {
		return s.session.Storage().Close()
	}
	return nil
}

// rebuildAgent rebuilds the agent with the current session state.
// This is called after profile/goal changes via SessionExt's rebuild callback.
func (s *AgentSession) rebuildAgent(ctx context.Context, registry *providers.Registry, skillDirs []string) (*agent.Agent, error) {
	ag, err := s.buildAgent(ctx, registry, skillDirs)
	if err != nil {
		return nil, err
	}
	s.agent = ag
	return ag, nil
}

// buildAgent constructs the agent.Agent for this session.
// Tool and prompt assembly is delegated to the injected Application.
func (s *AgentSession) buildAgent(ctx context.Context, registry *providers.Registry, skillDirs []string) (*agent.Agent, error) {
	cfg := s.cfg
	cwd := util.CWD()

	// Build tools via Application interface
	toolList := s.application.BuildTools(s.toolBuildOptions(cwd))

	// Wire the batch tool with a tool registry so it can look up and
	// execute sibling tools. The registry is a simple map built from
	// the tool list itself (no external dependency needed).
	toolReg := &toolListRegistry{tools: make(map[string]agent.Tool, len(toolList))}
	for _, t := range toolList {
		toolReg.tools[t.Name()] = t
	}
	for _, t := range toolList {
		if setter, ok := t.(interface {
			SetRegistry(interface {
				GetTool(string) (agent.Tool, bool)
			})
		}); ok {
			setter.SetRegistry(toolReg)
		}
	}

	// Determine model
	modelID := cfg.AnthropicModel
	providerName := cfg.Provider
	if providerName == "openai" {
		modelID = cfg.OpenAIModel
	}
	if modelID == "" {
		providerName = ""
	}

	model := ai.Model{
		ID:            modelID,
		Name:          modelID,
		Provider:      providerName,
		ContextWindow: models.ContextWindow(modelID),
		MaxTokens:     4096,
	}

	// Compaction settings
	compactionSettings := compaction.DefaultSettings()
	var summarizeFunc compaction.SummarizeFunc
	summarizeFunc = compaction.LLMSummarizer(registry, model)

	// Load context files
	contextFiles := prompt.LoadProjectContextFiles(cwd, "")

	// Load skills
	var skills []skill.Skill
	if len(skillDirs) == 0 {
		// Default skill dirs
		skillDirs = []string{}
		defaultSkillDir := filepath.Join(cwd, ".claude", "skills")
		if fi, err := os.Stat(defaultSkillDir); err == nil && fi.IsDir() {
			skillDirs = append(skillDirs, defaultSkillDir)
		}
		homeSkillDir := filepath.Join(util.HomeDir(), ".claude", "skills")
		if fi, err := os.Stat(homeSkillDir); err == nil && fi.IsDir() {
			skillDirs = append(skillDirs, homeSkillDir)
		}
	}
	if len(skillDirs) > 0 {
		result := skill.LoadFromDirs(skillDirs...)
		skills = result.Skills
		for _, diag := range result.Diagnostics {
			slog.Warn("skill diagnostic", "code", diag.Code, "message", diag.Message, "path", diag.Path)
		}
	}

	// Read profile/goal from SessionExt
	var profileName, goal string
	if s.ext != nil {
		profileName = s.ext.Profile()
		goal = s.ext.Goal()
	}

	// Build system prompt via Application interface
	systemPrompt := s.application.BuildPrompt(PromptBuildOptions{
		CustomPrompt: cfg.PromptTemplate,
		CWD:          cwd,
		Tools:        toolList,
		ContextFiles: contextFiles,
		Skills:       skills,
	}, profileName, goal)

	// Aggregate lifecycle hooks from extension registry
	var lifecycleHooks agent.LifecycleHooks
	if s.extRegistry != nil {
		lifecycleHooks = s.extRegistry.LifecycleHooks()
	}

	// Register the auto-synopsis hook: large tool outputs are automatically
	// replaced with a structural synopsis, saving context window tokens.
	// The full output is preserved in UserFacing for the UI.
	lifecycleHooks.After = append(lifecycleHooks.After, agent.SynopsisAfterHook)

	return agent.New(agent.Options{
		Model:              model,
		Registry:           registry,
		System:             systemPrompt,
		Tools:              toolList,
		MaxTurns:           cfg.MaxTurns,
		Goal:               goal,
		Session:            s.session,
		CompactionSettings: compactionSettings,
		SummarizeFunc:      summarizeFunc,
		LifecycleHooks:     lifecycleHooks,
		ConfirmFunc:        s.wrapConfirm(s.confirmFunc),
		LoopDetectSettings: agent.DefaultLoopDetectSettings(),
	}), nil
}

// SetConfirmFunc 注入危险工具执行前的确认回调。
// 供交互式入口（chat TUI）调用以启用确认；serve/feishu 等单向流入口不调用，保持默认放行。
// 在首次 PromptStream 之前调用即可生效；若 agent 已构建则会触发重建。
func (s *AgentSession) SetConfirmFunc(fn agent.ConfirmFunc) {
	wasUnset := s.confirmFunc == nil
	s.confirmFunc = fn
	if wasUnset && fn != nil {
		// Installing an interactive callback enables confirmations by default.
		// Callers such as `easyagent -y` can immediately override this below.
		s.confirmEnabled.Store(true)
	}
	if s.agent != nil {
		if _, err := s.rebuildAgent(context.Background(), s.deps.Registry, s.skillDirs); err != nil {
			slog.Error("failed to rebuild agent after updating confirmation handler", "error", err)
		}
	}
}

// wrapConfirm 给确认回调套上运行时开关：/confirm off 时直接放行，不再进对话框。
// 回调为空（serve/feishu）返回 nil，保持引擎默认放行语义。
func (s *AgentSession) wrapConfirm(fn agent.ConfirmFunc) agent.ConfirmFunc {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, req agent.ConfirmationRequest) agent.ConfirmDecision {
		if !s.confirmEnabled.Load() {
			return agent.ConfirmDecision{Approved: true, Reason: "全权模式（/confirm on 可恢复确认）"}
		}
		return fn(ctx, req)
	}
}

// SetConfirmEnabled 运行时切换确认开关（/confirm on|off）。
func (s *AgentSession) SetConfirmEnabled(enabled bool) {
	if s.confirmFunc == nil {
		return // 从未注入回调：本来就没确认可言
	}
	s.confirmEnabled.Store(enabled)
}

// ConfirmEnabled 返回确认开关当前状态；未注入回调时恒为 false（无确认）。
func (s *AgentSession) ConfirmEnabled() bool {
	return s.confirmFunc != nil && s.confirmEnabled.Load()
}

// toolBuildOptions constructs ToolBuildOptions from the current config and session state.
func (s *AgentSession) toolBuildOptions(cwd string) ToolBuildOptions {
	cfg := s.cfg

	// Resolve workspace: prefer config, fallback to cwd
	workspace := cfg.Workspace
	if workspace == "" {
		workspace = cwd
	}

	// Build operations backend via Dependencies callback
	var ops *operations.Operations
	if s.deps.BuildOperations != nil {
		ops = s.deps.BuildOperations(cfg, workspace)
	} else {
		ops = operations.NewLocalOperations()
	}

	// In SSH mode, override workspace with remote working directory
	if cfg.ExecutionMode == "ssh" && cfg.SSHWorkDir != "" {
		workspace = cfg.SSHWorkDir
	}

	// Extension tools
	var extTools []agent.Tool
	if s.extRegistry != nil {
		extTools = s.extRegistry.Tools()
	}

	// External tools (registered via HTTP callback)
	for _, def := range s.deps.ExternalTools {
		if t, err := agent.NewExternalTool(def); err == nil {
			extTools = append(extTools, t)
		} else {
			slog.Warn("skip invalid external tool", "name", def.Name, "error", err)
		}
	}

	return ToolBuildOptions{
		SessionID:      s.sessionID,
		Workspace:      workspace,
		MaxOutputLen:   cfg.MaxOutputLen,
		BashOps:        ops.Bash,
		FileOps:        ops.Files,
		ExtensionTools: extTools,
		AllowedTools:   cfg.AllowedTools,
		BlockedTools:   cfg.BlockedTools,
	}
}

// toolListRegistry is a simple adapter that makes a map of tools
// satisfy the ToolRegistry interface (used by batch tool).
type toolListRegistry struct {
	tools map[string]agent.Tool
}

func (r *toolListRegistry) GetTool(name string) (agent.Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// ConfirmationCallback returns the live parent confirmation policy for child sessions.
func (s *AgentSession) ConfirmationCallback() agent.ConfirmFunc { return s.wrapConfirm(s.confirmFunc) }
