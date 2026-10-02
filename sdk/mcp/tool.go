package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPToolAdapter struct {
	manager *Manager
	server  string
	tool    *protocol.Tool
	timeout time.Duration
}

func newToolAdapter(manager *Manager, server string, tool *protocol.Tool, cfg MCPServerConfig) *MCPToolAdapter {
	timeout := DefaultTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Millisecond
	}
	return &MCPToolAdapter{manager: manager, server: server, tool: tool, timeout: timeout}
}
func toolNamespace(name string) string {
	server := namespace(name)
	if len(server) > 24 {
		sum := sha256.Sum256([]byte(name))
		server = server[:15] + fmt.Sprintf("_%x", sum[:4])
	}
	return server
}
func (t *MCPToolAdapter) Name() string {
	server := toolNamespace(t.server)
	prefix := "mcp__" + server + "__"
	budget := 64 - len(prefix)
	var b strings.Builder
	for _, r := range t.tool.Name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name != t.tool.Name || len(name) > budget {
		sum := sha256.Sum256([]byte(t.tool.Name))
		if len(name) > budget-9 {
			name = name[:budget-9]
		}
		name += fmt.Sprintf("_%x", sum[:4])
	}
	return prefix + name
}
func (t *MCPToolAdapter) ToolName() string   { return t.tool.Name }
func (t *MCPToolAdapter) ServerName() string { return t.server }
func (t *MCPToolAdapter) Description() string {
	if t.tool.Description != "" {
		return t.tool.Description
	}
	return "MCP tool " + t.server + "/" + t.tool.Name
}
func (t *MCPToolAdapter) Parameters() map[string]any {
	data, _ := json.Marshal(t.tool.InputSchema)
	var result map[string]any
	if json.Unmarshal(data, &result) != nil || result == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return result
}
func (t *MCPToolAdapter) Validate(params json.RawMessage) (json.RawMessage, error) {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	var obj map[string]any
	if err := json.Unmarshal(params, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("MCP tool arguments must be a JSON object")
	}
	return params, nil
}
func (t *MCPToolAdapter) trusted() bool {
	t.manager.mu.RLock()
	defer t.manager.mu.RUnlock()
	srv := t.manager.servers[t.server]
	return srv != nil && srv.entry.Config.Trust
}
func (t *MCPToolAdapter) RequiresConfirmation(params json.RawMessage) (string, bool) {
	return "Run MCP tool " + t.server + "/" + t.tool.Name, !t.trusted()
}
func (t *MCPToolAdapter) RequiresConfirmationAvailable() bool    { return !t.trusted() }
func (t *MCPToolAdapter) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *MCPToolAdapter) ExecutionMode() agent.ExecutionMode     { return agent.ExecutionModeSequential }
func (t *MCPToolAdapter) Execute(ctx context.Context, params json.RawMessage, onUpdate func(agent.PartialResult)) (agent.ToolResult, error) {
	validated, err := t.Validate(params)
	if err != nil {
		return agent.ToolResult{}, err
	}
	var args map[string]any
	_ = json.Unmarshal(validated, &args)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	result, err := t.manager.callTool(ctx, t.server, t.tool.Name, args, onUpdate)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	var parts []string
	for _, block := range result.Content {
		if text, ok := block.(*protocol.TextContent); ok {
			parts = append(parts, text.Text)
		} else {
			raw, _ := json.Marshal(block)
			parts = append(parts, string(raw))
		}
	}
	if result.StructuredContent != nil {
		raw, _ := json.Marshal(result.StructuredContent)
		parts = append(parts, string(raw))
	}
	text := strings.Join(parts, "\n")
	if text == "" {
		text = "(empty MCP result)"
	}
	return agent.ToolResult{Content: text, UserFacing: text, Details: result, IsError: result.IsError}, nil
}
