package kbtools

import (
	"github.com/hwj123hwj/easyagent/sdk/agent"
)

// ListOptions controls how the kb-agent toolset is assembled.
type ListOptions struct {
	RepoPath       string          // path to agent-lessons repo
	SearchStrategy SearchStrategy  // optional: inject custom search strategy (vector/hybrid)
	AllowedTools   []string
	BlockedTools   []string
}

// BaseToolNames returns the canonical kb-agent tool names.
func BaseToolNames() []string {
	return []string{"kb_search", "kb_read", "kb_list", "kb_save", "kb_maintain"}
}

// BuildList assembles the concrete kb-agent toolset.
func BuildList(opts ListOptions) []agent.Tool {
	searchTool := NewSearchTool(opts.RepoPath)
	// Inject custom search strategy if provided (e.g. HybridSearcher)
	if opts.SearchStrategy != nil {
		searchTool = NewSearchToolWithStrategy(opts.RepoPath, opts.SearchStrategy)
	}
	toolList := []agent.Tool{
		searchTool,
		NewReadTool(opts.RepoPath),
		NewListTool(opts.RepoPath),
		NewSaveTool(opts.RepoPath),
		NewMaintainTool(opts.RepoPath),
	}
	return filterTools(toolList, opts.AllowedTools, opts.BlockedTools)
}

func filterTools(tools []agent.Tool, allowed []string, blocked []string) []agent.Tool {
	if len(allowed) == 0 && len(blocked) == 0 {
		return tools
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	blockedSet := make(map[string]bool, len(blocked))
	for _, name := range blocked {
		blockedSet[name] = true
	}
	var filtered []agent.Tool
	for _, tool := range tools {
		name := tool.Name()
		if len(allowed) > 0 && !allowedSet[name] {
			continue
		}
		if blockedSet[name] {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}
