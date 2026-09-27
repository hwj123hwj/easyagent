package commands

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSession struct {
	confirmEnabled bool
	sessionID      string
	provider       string
	modelID        string
	toolNames      []string
	switchErr      error
	profile        string
	goal           string
	compactResult  string
	compactFrom    int
	compactTo      int
	compactErr     error
}

func (m *mockSession) SessionID() string { return m.sessionID }

func (m *mockSession) ModelInfo() (string, string) {
	if m.provider == "" && m.modelID == "" {
		return "openai", "gpt-4o"
	}
	return m.provider, m.modelID
}

func (m *mockSession) SwitchModel(ctx context.Context, modelID string, provider string) error {
	if m.switchErr != nil {
		return m.switchErr
	}
	m.modelID = modelID
	if provider != "" {
		m.provider = provider
	}
	return nil
}

func (m *mockSession) ToolNames() []string {
	if m.toolNames == nil {
		return []string{"bash", "read", "write", "edit"}
	}
	return m.toolNames
}

func (m *mockSession) Profile() string {
	if m.profile == "" {
		return "coding"
	}
	return m.profile
}

func (m *mockSession) SwitchProfile(_ context.Context, profile string) error {
	switch profile {
	case "coding", "review":
		m.profile = profile
		return nil
	default:
		return assert.AnError
	}
}

func (m *mockSession) Goal() string                   { return m.goal }
func (m *mockSession) SetGoal(goal string)            { m.goal = goal }
func (m *mockSession) ClearGoal()                     { m.goal = "" }
func (m *mockSession) ConfirmEnabled() bool           { return m.confirmEnabled }
func (m *mockSession) SetConfirmEnabled(enabled bool) { m.confirmEnabled = enabled }
func (m *mockSession) Compact(_ context.Context, _ string) (string, int, int, error) {
	if m.compactErr != nil {
		return "", 0, 0, m.compactErr
	}
	return m.compactResult, m.compactFrom, m.compactTo, nil
}

type mockApp struct {
	sessions     []slashcmd.SessionInfo
	newSession   slashcmd.SessionContext
	switchTarget slashcmd.SessionContext
	newErr       error
	switchErr    error
}

func (m *mockApp) ListSessionsInfo() ([]slashcmd.SessionInfo, error) {
	return m.sessions, nil
}

func (m *mockApp) CreateSession(ctx context.Context) (slashcmd.SessionContext, error) {
	if m.newErr != nil {
		return nil, m.newErr
	}
	return m.newSession, nil
}

func (m *mockApp) SwitchSession(ctx context.Context, sessionID string) (slashcmd.SessionContext, error) {
	if m.switchErr != nil {
		return nil, m.switchErr
	}
	return m.switchTarget, nil
}

func (m *mockApp) Profiles() []string {
	return []string{"coding", "review"}
}

func (m *mockApp) AvailableModels() []slashcmd.ModelInfo {
	return []slashcmd.ModelInfo{
		{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
		{Provider: "openai", ModelID: "gpt-4o"},
	}
}

func newRegistry() *slashcmd.Registry {
	reg := slashcmd.NewRegistry()
	RegisterBuiltins(reg)
	return reg
}

func TestRegisterBuiltins(t *testing.T) {
	reg := newRegistry()

	assert.Contains(t, reg.Names(), "help")
	assert.Contains(t, reg.Names(), "compact")
	assert.Contains(t, reg.Names(), "sessions")
	assert.Contains(t, reg.Names(), "session")
	assert.Contains(t, reg.Names(), "branch")
	assert.Contains(t, reg.Names(), "new")
	assert.Contains(t, reg.Names(), "switch")
	assert.Contains(t, reg.Names(), "tools")
	assert.Contains(t, reg.Names(), "model")
	assert.Contains(t, reg.Names(), "models")
	assert.Contains(t, reg.Names(), "profiles")
	assert.Contains(t, reg.Names(), "profile")
	assert.Contains(t, reg.Names(), "goal")
	assert.Contains(t, reg.Names(), "context")
	assert.Contains(t, reg.Names(), "clear")
}

func TestHelp_Formatted(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/help")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Session management")
	assert.Contains(t, result.Output, "Information & control")
	assert.Contains(t, result.Output, "/help")
	assert.Contains(t, result.Output, "/tools")
	assert.Contains(t, result.Output, "/new")
	assert.Contains(t, result.Output, "/switch")
	assert.Contains(t, result.Output, "/profiles")
	assert.Contains(t, result.Output, "/profile")
	assert.Contains(t, result.Output, "/goal")
	assert.Contains(t, result.Output, "/context")
	assert.Contains(t, result.Output, "/models")
	assert.Contains(t, result.Output, "/clear")
}

func TestTools_UsesRealToolList(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{toolNames: []string{"bash", "read", "grep", "custom_tool"}},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/tools")
	require.NoError(t, err)

	assert.Contains(t, result.Output, "bash")
	assert.Contains(t, result.Output, "read")
	assert.Contains(t, result.Output, "grep")
	assert.Contains(t, result.Output, "custom_tool")
}

func TestSession_ShowsFullInfo(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_abc123", provider: "anthropic", modelID: "claude-3.5-sonnet"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/session")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "sess_abc123")
	assert.Contains(t, result.Output, "anthropic")
	assert.Contains(t, result.Output, "claude-3.5-sonnet")
	assert.Contains(t, result.Output, "Tools:")
	assert.Contains(t, result.Output, "Profile:")
}

func TestModel_SwitchWithProvider(t *testing.T) {
	reg := newRegistry()

	sess := &mockSession{provider: "anthropic", modelID: "claude-3.5-sonnet"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/model openai:gpt-4o")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Switched:")
	assert.Contains(t, result.Output, "openai")
	assert.Contains(t, result.Output, "gpt-4o")
}

func TestSessions_ListWithCurrentMarked(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_current"},
		App: &mockApp{
			sessions: []slashcmd.SessionInfo{
				{ID: "sess_current", MessageCount: 5, LastActive: 1700000000},
				{ID: "sess_other", MessageCount: 3, LastActive: 1699990000},
			},
		},
	}
	result, err := reg.Execute(cmdCtx, "/sessions")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "sess_current")
	assert.Contains(t, result.Output, "sess_other")
	assert.Contains(t, result.Output, "→")
}

func TestNew_CreatesSession(t *testing.T) {
	reg := newRegistry()

	newSess := &mockSession{sessionID: "sess_new", provider: "openai", modelID: "gpt-4o"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_old"},
		App:     &mockApp{newSession: newSess},
	}
	result, err := reg.Execute(cmdCtx, "/new")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "sess_new")
	assert.Contains(t, result.Output, "Created new session")
}

func TestNew_ReturnsSessionSwitchTo(t *testing.T) {
	reg := newRegistry()

	newSess := &mockSession{sessionID: "sess_new", provider: "openai", modelID: "gpt-4o"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_old"},
		App:     &mockApp{newSession: newSess},
	}
	result, err := reg.Execute(cmdCtx, "/new")
	require.NoError(t, err)
	assert.NotNil(t, result.SessionSwitchTo)
	assert.Equal(t, "sess_new", result.SessionSwitchTo.SessionID())
}

func TestSwitch_LoadsSession(t *testing.T) {
	reg := newRegistry()

	targetSess := &mockSession{sessionID: "sess_target", provider: "anthropic", modelID: "claude-3.5-sonnet"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_current"},
		App:     &mockApp{switchTarget: targetSess},
	}
	result, err := reg.Execute(cmdCtx, "/switch sess_target")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "sess_target")
	assert.Contains(t, result.Output, "Switched to session")
	assert.NotNil(t, result.SessionSwitchTo)
	assert.Equal(t, "sess_target", result.SessionSwitchTo.SessionID())
}

func TestSwitch_NoArgs(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_current"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/switch")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Usage:")
}

func TestProfiles_ListsProfiles(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test", profile: "coding"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/profiles")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "coding")
	assert.Contains(t, result.Output, "review")
	assert.Contains(t, result.Output, "→ coding")
}

func TestProfile_ShowCurrent(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test", profile: "coding"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/profile")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Current profile:")
	assert.Contains(t, result.Output, "coding")
}

func TestProfile_SwitchToReview(t *testing.T) {
	reg := newRegistry()

	sess := &mockSession{sessionID: "test", profile: "coding"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/profile review")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Switched profile:")
	assert.Contains(t, result.Output, "review")
	assert.Equal(t, "review", sess.Profile())
}

func TestProfile_SwitchInvalid(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	_, err := reg.Execute(cmdCtx, "/profile nonexistent")
	assert.Error(t, err)
}

func TestBranch_IsPlaceholder(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/branch")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not yet implemented")
}

func TestSession_IncludesProfile(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "sess_abc123", provider: "anthropic", modelID: "claude-3.5-sonnet", profile: "review"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/session")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "review")
}

// --- New command tests ---

func TestRegisterBuiltins_IncludesNewCommands(t *testing.T) {
	reg := newRegistry()

	assert.Contains(t, reg.Names(), "goal")
	assert.Contains(t, reg.Names(), "context")
	assert.Contains(t, reg.Names(), "models")
	assert.Contains(t, reg.Names(), "clear")
}

func TestGoal_ShowEmpty(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/goal")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No goal set")
}

func TestGoal_Set(t *testing.T) {
	reg := newRegistry()

	sess := &mockSession{sessionID: "test"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/goal Review the auth flow for regressions")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Goal set:")
	assert.Contains(t, result.Output, "Review the auth flow for regressions")
	assert.Equal(t, "Review the auth flow for regressions", sess.Goal())
}

func TestGoal_ShowSet(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test", goal: "Fix the login bug"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/goal")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Current goal:")
	assert.Contains(t, result.Output, "Fix the login bug")
}

func TestGoal_Clear(t *testing.T) {
	reg := newRegistry()

	sess := &mockSession{sessionID: "test", goal: "Fix the login bug"}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/goal clear")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Goal cleared")
	assert.Equal(t, "", sess.Goal())
}

func TestContext_ShowsFullInfo(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx: context.Background(),
		Session: &mockSession{
			sessionID: "sess_abc",
			provider:  "anthropic",
			modelID:   "claude-sonnet-4-6",
			profile:   "coding",
			goal:      "Refactor the auth module",
			toolNames: []string{"bash", "read", "edit"},
		},
		App: &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/context")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "sess_abc")
	assert.Contains(t, result.Output, "anthropic")
	assert.Contains(t, result.Output, "claude-sonnet-4-6")
	assert.Contains(t, result.Output, "coding")
	assert.Contains(t, result.Output, "Refactor the auth module")
	assert.Contains(t, result.Output, "bash")
	assert.Contains(t, result.Output, "read")
	assert.Contains(t, result.Output, "edit")
}

func TestContext_ShowsGoalNotSet(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/context")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "(not set)")
}

func TestModels_ListsModels(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test", provider: "openai", modelID: "gpt-4o"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/models")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "anthropic/claude-sonnet-4-6")
	assert.Contains(t, result.Output, "openai/gpt-4o")
	assert.Contains(t, result.Output, "→")
}

func TestModels_ShowsSwitchHint(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/models")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/model")
}

func TestClear_ReturnsClearScreen(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/clear")
	require.NoError(t, err)
	assert.True(t, result.ClearScreen)
	assert.Nil(t, result.SessionSwitchTo)
	assert.Empty(t, result.Output)
}

func TestCompact_Success(t *testing.T) {
	reg := newRegistry()

	sess := &mockSession{
		sessionID:     "test",
		compactResult: "Summary: user asked to build a web server.",
		compactFrom:   10,
		compactTo:     3,
	}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/compact focus on file changes")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "compacted")
	assert.Contains(t, result.Output, "10 → 3")
	assert.Contains(t, result.Output, "Summary: user asked to build a web server.")
}

func TestCompact_NoSession(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx: context.Background(),
		App: &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/compact")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "no active session")
}

func TestCompact_Error(t *testing.T) {
	reg := newRegistry()

	sess := &mockSession{
		sessionID:  "test",
		compactErr: fmt.Errorf("not enough messages to compact (have 1)"),
	}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	_, err := reg.Execute(cmdCtx, "/compact")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enough messages")
}

func TestCompact_TruncatesLongSummary(t *testing.T) {
	reg := newRegistry()

	longSummary := strings.Repeat("x", 600)
	sess := &mockSession{
		sessionID:     "test",
		compactResult: longSummary,
		compactFrom:   20,
		compactTo:     5,
	}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/compact")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "20 → 5")
	assert.Contains(t, result.Output, "...")
}

func TestHelp_IncludesNewCommands(t *testing.T) {
	reg := newRegistry()

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: &mockSession{sessionID: "test"},
		App:     &mockApp{},
	}
	result, err := reg.Execute(cmdCtx, "/help")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/goal")
	assert.Contains(t, result.Output, "/context")
	assert.Contains(t, result.Output, "/models")
	assert.Contains(t, result.Output, "/clear")
	// Help section should now be called "Information & control"
	assert.Contains(t, result.Output, "Information & control")
}

func TestConfirmCommand_TogglesAndStatus(t *testing.T) {
	reg := newRegistry()
	sess := &mockSession{sessionID: "test", confirmEnabled: true}
	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: sess,
		App:     &mockApp{},
	}

	// 状态查询
	result, err := reg.Execute(cmdCtx, "/confirm")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "确认已开启")

	// off → 全权
	result, err = reg.Execute(cmdCtx, "/confirm off")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "全权模式")
	assert.False(t, sess.ConfirmEnabled())

	// 全权下状态查询
	result, err = reg.Execute(cmdCtx, "/confirm")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "全权模式")

	// on → 恢复
	result, err = reg.Execute(cmdCtx, "/confirm on")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "已恢复确认")
	assert.True(t, sess.ConfirmEnabled())

	// 非法参数
	result, err = reg.Execute(cmdCtx, "/confirm blah")
	require.NoError(t, err)
	assert.Contains(t, result.Output, "用法")
}

func TestWorkflowCommandProducesExplicitAgentPrompt(t *testing.T) {
	registry := slashcmd.NewRegistry()
	RegisterBuiltins(registry)
	result, err := registry.Execute(slashcmd.Context{}, "/workflow 研究方案")
	require.NoError(t, err)
	assert.True(t, result.ShouldQuery)
	assert.Equal(t, "/workflow 研究方案", result.QueryPrompt)
	result, err = registry.Execute(slashcmd.Context{}, "/workflow")
	require.NoError(t, err)
	assert.False(t, result.ShouldQuery)
	assert.Contains(t, result.Output, "Actor")
}
