package runtime

import (
	"context"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/operations"
	"github.com/hwj123hwj/easyagent/sdk/prompt"
	"github.com/hwj123hwj/easyagent/sdk/skill"
)

// Application is the interface that an agent application must implement.
// The runtime (Platform layer) depends on this interface, not on any
// concrete application (e.g. coding-agent). The App layer is responsible
// for choosing and injecting the concrete implementation.
type Application interface {
	// BuildTools assembles the tool list for the agent session.
	BuildTools(opts ToolBuildOptions) []agent.Tool

	// BuildPrompt constructs the system prompt for the agent session.
	BuildPrompt(opts PromptBuildOptions, profile, goal string) string

	// NewSessionExt creates a per-session extension for application-specific state.
	// Returns nil if the application does not use session extensions.
	NewSessionExt() SessionExt
}

// SessionExt is an optional extension interface for application-specific
// session features (e.g., profile, goal management).
// If not provided (nil), these features are unavailable.
// Each session gets its own SessionExt instance via Application.NewSessionExt().
type SessionExt interface {
	// Profile returns the current profile name.
	Profile() string

	// SwitchProfile changes the active profile and triggers an agent rebuild.
	SwitchProfile(ctx context.Context, profile string) error

	// Goal returns the current session goal.
	Goal() string

	// SetGoal sets the session goal and triggers an agent rebuild.
	SetGoal(goal string)

	// ClearGoal clears the session goal and triggers an agent rebuild.
	ClearGoal()
}

// ToolBuildOptions contains the context needed by an Application to build its tool list.
type ToolBuildOptions struct {
	SessionID      string
	Workspace      string
	MaxOutputLen   int
	BashOps        operations.BashOperations
	FileOps        operations.FileOperations
	ExtensionTools []agent.Tool
	AllowedTools   []string
	BlockedTools   []string
}

// PromptBuildOptions contains the context needed by an Application to build its system prompt.
type PromptBuildOptions struct {
	CustomPrompt string
	CWD          string
	Tools        []agent.Tool
	ContextFiles []prompt.ContextFile
	Skills       []skill.Skill
}
