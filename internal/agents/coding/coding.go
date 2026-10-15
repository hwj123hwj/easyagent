package coding

import (
	codingcommands "github.com/hwj123hwj/easyagent/internal/agents/coding/commands"
	codingtools "github.com/hwj123hwj/easyagent/internal/agents/coding/tools"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

func BaseToolNames(enableBash bool) []string {
	return codingtools.BaseToolNames(enableBash)
}

func RegisterCommands(registry *slashcmd.Registry) {
	codingcommands.RegisterBuiltins(registry)
	codingcommands.RegisterWikiCommands(registry)
}
