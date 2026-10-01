package slashcmd

import "context"

// Context provides the runtime context that slash command handlers need.
// It uses interfaces to avoid import cycles with app/runtime packages.
type Context struct {
	// Ctx is the context.Context for the current operation.
	Ctx context.Context

	// Session provides session-level operations.
	Session SessionContext

	// App provides app-level operations.
	App AppContext
}

// SessionContext is the interface for session operations available to slash commands.
type SessionContext interface {
	SessionID() string
	ModelInfo() (provider string, modelID string)
	SwitchModel(ctx context.Context, modelID string, provider string) error
	ToolNames() []string
	Profile() string
	SwitchProfile(ctx context.Context, profile string) error
	Goal() string
	SetGoal(goal string)
	ClearGoal()
	// ConfirmEnabled / SetConfirmEnabled：危险工具确认的运行时开关（/confirm）。
	ConfirmEnabled() bool
	SetConfirmEnabled(enabled bool)
	Compact(ctx context.Context, customInstructions string) (summary string, trimmedFrom int, trimmedTo int, err error)
}

// AppContext is the interface for app operations available to slash commands.
type AppContext interface {
	ListSessionsInfo() ([]SessionInfo, error)
	CreateSession(ctx context.Context) (SessionContext, error)
	SwitchSession(ctx context.Context, sessionID string) (SessionContext, error)
	Profiles() []string
	AvailableModels() []ModelInfo
}

// ModelInfo holds metadata about an available model.
type ModelInfo struct {
	Provider string
	ModelID  string
}

// SessionInfo holds session metadata for listing.
type SessionInfo struct {
	ID           string
	Title        string
	Workspace    string
	Application  string
	MessageCount int
	LastActive   int64
}
