package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// RegisterBuiltins registers coding-agent slash commands into the shared framework registry.
func RegisterBuiltins(registry *slashcmd.Registry) {
	registry.Register(slashcmd.Command{
		Name: "mcp", Description: "管理 MCP 服务、项目授权与工具 (/mcp list)",
		Subcommands: []slashcmd.Subcommand{{Name: "list", Description: "查看服务与连接状态"}, {Name: "reconnect", Description: "重连服务"}, {Name: "refresh", Description: "刷新工具"}, {Name: "enable", Description: "启用服务"}, {Name: "disable", Description: "停用服务"}, {Name: "trust", Description: "授权服务工具"}, {Name: "untrust", Description: "恢复工具逐次确认"}, {Name: "project", Description: "项目配置 trust|untrust"}},
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			host, ok := ctx.App.(interface {
				ManageMCP(context.Context, string, string) (string, error)
			})
			if !ok {
				return slashcmd.CommandResult{}, fmt.Errorf("MCP management is unavailable in this host")
			}
			workspace := ""
			if session, ok := ctx.Session.(interface{ Workspace() string }); ok {
				workspace = session.Workspace()
			}
			out, err := host.ManageMCP(ctx.Ctx, workspace, args)
			return slashcmd.CommandResult{Output: out}, err
		},
	})
	registerWorkflow(registry)
	registry.Register(slashcmd.Command{
		Name:        "confirm",
		Description: "Show or toggle dangerous-tool confirmation (/confirm on|off)",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			switch strings.ToLower(strings.TrimSpace(args)) {
			case "", "status":
				if ctx.Session.ConfirmEnabled() {
					return slashcmd.CommandResult{Output: "确认已开启：危险工具执行前会询问（/confirm off 切换为全权模式）"}, nil
				}
				return slashcmd.CommandResult{Output: "全权模式：危险工具直接执行（/confirm on 恢复确认）"}, nil
			case "off":
				ctx.Session.SetConfirmEnabled(false)
				return slashcmd.CommandResult{Output: "已进入全权模式：危险工具不再询问，直接执行（/confirm on 恢复）"}, nil
			case "on":
				ctx.Session.SetConfirmEnabled(true)
				return slashcmd.CommandResult{Output: "已恢复确认：危险工具执行前会询问"}, nil
			default:
				return slashcmd.CommandResult{Output: "用法：/confirm [on|off|status]"}, nil
			}
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "help",
		Description: "List all commands",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			return slashcmd.CommandResult{Output: formatHelp(registry)}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "compact",
		Description: "Compact conversation context, summarizing older messages",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}

			customInstructions := strings.TrimSpace(args)

			summary, from, to, err := ctx.Session.Compact(ctx.Ctx, customInstructions)
			if err != nil {
				return slashcmd.CommandResult{}, fmt.Errorf("compact failed: %w", err)
			}

			var b strings.Builder
			b.WriteString(fmt.Sprintf("Context compacted: %d → %d messages\n", from, to))
			b.WriteString(fmt.Sprintf("Summary (%d chars):\n", utf8.RuneCountInString(summary)))
			b.WriteString(summary)
			b.WriteString("\n")

			return slashcmd.CommandResult{Output: b.String()}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "sessions",
		Description: "List all sessions",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.App == nil {
				return slashcmd.CommandResult{Output: "app not available"}, nil
			}
			sessions, err := ctx.App.ListSessionsInfo()
			if err != nil {
				return slashcmd.CommandResult{}, fmt.Errorf("list sessions: %w", err)
			}
			if len(sessions) == 0 {
				return slashcmd.CommandResult{Output: "no sessions found"}, nil
			}

			currentID := ""
			if ctx.Session != nil {
				currentID = ctx.Session.SessionID()
			}

			var b strings.Builder
			b.WriteString("Sessions:\n")
			for _, s := range sessions {
				marker := "  "
				if s.ID == currentID {
					marker = "→ "
				}
				lastActive := time.Unix(s.LastActive, 0).Format("2006-01-02 15:04")
				b.WriteString(fmt.Sprintf("%s%-20s  messages=%-4d  last_active=%s\n", marker, s.ID, s.MessageCount, lastActive))
			}
			return slashcmd.CommandResult{Output: b.String()}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "session",
		Description: "Show current session info",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			provider, modelID := ctx.Session.ModelInfo()
			tools := ctx.Session.ToolNames()
			profile := ctx.Session.Profile()
			return slashcmd.CommandResult{Output: fmt.Sprintf("Session:  %s\nModel:    %s/%s\nProfile:  %s\nTools:    %s",
				ctx.Session.SessionID(),
				provider, modelID,
				profile,
				strings.Join(tools, ", "))}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "branch",
		Description: "Switch to a specific entry (branch navigation) [planned]",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			return slashcmd.CommandResult{Output: "Branch navigation is not yet implemented. It will be available in a future update."}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "new",
		Description: "Create a new session",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.App == nil {
				return slashcmd.CommandResult{Output: "app not available — cannot create new session"}, nil
			}
			newSession, err := ctx.App.CreateSession(ctx.Ctx)
			if err != nil {
				return slashcmd.CommandResult{}, fmt.Errorf("create session: %w", err)
			}
			provider, modelID := newSession.ModelInfo()
			output := fmt.Sprintf("Created new session: %s\nModel: %s/%s\nReady for input.", newSession.SessionID(), provider, modelID)
			return slashcmd.CommandResult{
				Output:          output,
				SessionSwitchTo: newSession,
			}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "switch",
		Description: "Switch to an existing session by ID",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			sessionID := strings.TrimSpace(args)
			if sessionID == "" {
				return slashcmd.CommandResult{Output: "Usage: /switch <session-id>"}, nil
			}
			if ctx.App == nil {
				return slashcmd.CommandResult{Output: "app not available — cannot switch session"}, nil
			}
			targetSession, err := ctx.App.SwitchSession(ctx.Ctx, sessionID)
			if err != nil {
				return slashcmd.CommandResult{}, fmt.Errorf("switch session: %w", err)
			}
			provider, modelID := targetSession.ModelInfo()
			output := fmt.Sprintf("Switched to session: %s\nModel: %s/%s", targetSession.SessionID(), provider, modelID)
			return slashcmd.CommandResult{
				Output:          output,
				SessionSwitchTo: targetSession,
			}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "tools",
		Description: "Show available tools",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			tools := ctx.Session.ToolNames()
			if len(tools) == 0 {
				return slashcmd.CommandResult{Output: "no tools available"}, nil
			}
			return slashcmd.CommandResult{Output: "Available tools:\n  " + strings.Join(tools, "\n  ")}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "model",
		Description: "Show or switch model (/model [provider:]model_name)",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			provider, modelID := ctx.Session.ModelInfo()

			newInput := strings.TrimSpace(args)
			if newInput == "" {
				return slashcmd.CommandResult{Output: fmt.Sprintf("Current model: %s/%s", provider, modelID)}, nil
			}

			// 网关模型 ID 普遍含斜杠（openai/workbuddy/kimi-k3），先按可用
			// 模型目录精确匹配全名；未命中才走冒号拆分/继承当前 provider，
			// 且未知的模型直接报错，避免「切换成功、下次请求 404」。
			var available []slashcmd.ModelInfo
			if ctx.App != nil {
				available = ctx.App.AvailableModels()
			}
			newProvider, newModel, err := resolveModelSwitch(newInput, available)
			if err != nil {
				return slashcmd.CommandResult{}, err
			}

			if err := ctx.Session.SwitchModel(ctx.Ctx, newModel, newProvider); err != nil {
				return slashcmd.CommandResult{}, fmt.Errorf("switch model: %w", err)
			}
			newP, newM := ctx.Session.ModelInfo()
			return slashcmd.CommandResult{Output: fmt.Sprintf("Switched: %s → %s", modelLabel(provider, modelID), modelLabel(newP, newM))}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "models",
		Description: "List available models for switching",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.App == nil {
				return slashcmd.CommandResult{Output: "app not available"}, nil
			}
			models := ctx.App.AvailableModels()
			if len(models) == 0 {
				return slashcmd.CommandResult{Output: "no models available"}, nil
			}

			// Get current model for marking
			var currentProvider, currentModel string
			if ctx.Session != nil {
				currentProvider, currentModel = ctx.Session.ModelInfo()
			}

			var b strings.Builder
			b.WriteString("Available models:\n")
			for _, m := range models {
				marker := "  "
				if m.Provider == currentProvider && m.ModelID == currentModel {
					marker = "→ "
				}
				b.WriteString(fmt.Sprintf("%s%s/%s\n", marker, m.Provider, m.ModelID))
			}
			b.WriteString("\nUse /model <model-id> to switch — 粘贴上面任意 ID 即可（也支持 provider:model）")
			return slashcmd.CommandResult{Output: b.String()}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "profiles",
		Description: "List all available profiles",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.App == nil {
				return slashcmd.CommandResult{Output: "app not available"}, nil
			}
			profiles := ctx.App.Profiles()
			currentProfile := ""
			if ctx.Session != nil {
				currentProfile = ctx.Session.Profile()
			}
			var b strings.Builder
			b.WriteString("Profiles:\n")
			for _, p := range profiles {
				marker := "  "
				if p == currentProfile {
					marker = "→ "
				}
				b.WriteString(fmt.Sprintf("%s%s\n", marker, p))
			}
			return slashcmd.CommandResult{Output: b.String()}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "profile",
		Description: "Show or switch profile (/profile [name])",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			name := strings.TrimSpace(args)
			if name == "" {
				return slashcmd.CommandResult{Output: fmt.Sprintf("Current profile: %s", ctx.Session.Profile())}, nil
			}
			oldProfile := ctx.Session.Profile()
			if err := ctx.Session.SwitchProfile(ctx.Ctx, name); err != nil {
				return slashcmd.CommandResult{}, err
			}
			return slashcmd.CommandResult{Output: fmt.Sprintf("Switched profile: %s → %s", oldProfile, ctx.Session.Profile())}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "goal",
		Description: "Show, set, or clear the current session goal",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			args = strings.TrimSpace(args)
			switch {
			case args == "":
				// Show current goal
				goal := ctx.Session.Goal()
				if goal == "" {
					return slashcmd.CommandResult{Output: "No goal set. Use /goal <text> to set one."}, nil
				}
				return slashcmd.CommandResult{Output: fmt.Sprintf("Current goal:\n  %s", goal)}, nil
			case args == "clear":
				// Clear the goal
				if sess, ok := ctx.Session.(interface{ TrySetGoal(string) error }); ok {
					if err := sess.TrySetGoal(""); err != nil {
						return slashcmd.CommandResult{}, err
					}
				} else {
					ctx.Session.ClearGoal()
				}
				return slashcmd.CommandResult{Output: "Goal cleared."}, nil
			default:
				// Set the goal and auto-trigger agent execution
				if sess, ok := ctx.Session.(interface{ TrySetGoal(string) error }); ok {
					if err := sess.TrySetGoal(args); err != nil {
						return slashcmd.CommandResult{}, err
					}
				} else {
					ctx.Session.SetGoal(args)
				}
				return slashcmd.CommandResult{
					Output:      fmt.Sprintf("Goal set:\n  %s\n\nStarting to work on this goal...", args),
					ShouldQuery: true,
				}, nil
			}
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "context",
		Description: "Inspect current logical model input, token estimates and compaction history",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			if ctx.Session == nil {
				return slashcmd.CommandResult{Output: "no active session"}, nil
			}
			if inspector, ok := ctx.Session.(interface {
				ContextSnapshot(context.Context) (agent.ContextSnapshot, error)
			}); ok {
				snapshot, err := inspector.ContextSnapshot(ctx.Ctx)
				if err != nil {
					return slashcmd.CommandResult{}, err
				}
				data, err := json.MarshalIndent(snapshot, "", "  ")
				return slashcmd.CommandResult{Output: "Current logical model input (before provider serialization; excludes drafts/queued messages). Token counts are estimates.\n" + string(data)}, err
			}
			provider, modelID := ctx.Session.ModelInfo()
			tools := ctx.Session.ToolNames()
			profile := ctx.Session.Profile()
			goal := ctx.Session.Goal()

			var b strings.Builder
			b.WriteString("Session:  " + ctx.Session.SessionID() + "\n")
			b.WriteString("Model:    " + provider + "/" + modelID + "\n")
			b.WriteString("Profile:  " + profile + "\n")
			if goal != "" {
				b.WriteString("Goal:     " + goal + "\n")
			} else {
				b.WriteString("Goal:     (not set)\n")
			}
			b.WriteString("Tools:    " + strings.Join(tools, ", ") + "\n")
			return slashcmd.CommandResult{Output: b.String()}, nil
		},
	})

	registry.Register(slashcmd.Command{
		Name:        "clear",
		Description: "Clear the terminal display",
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			return slashcmd.CommandResult{ClearScreen: true}, nil
		},
	})
}

func formatHelp(registry *slashcmd.Registry) string {
	names := registry.Names()

	var sessionCmds, infoCmds, actionCmds []string
	for _, name := range names {
		switch name {
		case "sessions", "session", "new", "switch", "branch":
			sessionCmds = append(sessionCmds, name)
		case "help", "tools", "model", "models", "profiles", "profile", "context", "goal":
			infoCmds = append(infoCmds, name)
		default:
			actionCmds = append(actionCmds, name)
		}
	}

	sort.Strings(sessionCmds)
	sort.Strings(infoCmds)
	sort.Strings(actionCmds)

	var b strings.Builder
	b.WriteString("Available commands:\n\n")

	if len(sessionCmds) > 0 {
		b.WriteString("  Session management:\n")
		for _, name := range sessionCmds {
			cmd := registry.Command(name)
			desc := cmd.Description
			b.WriteString(fmt.Sprintf("    %-14s %s\n", "/"+name, desc))
		}
		b.WriteString("\n")
	}

	if len(infoCmds) > 0 {
		b.WriteString("  Information & control:\n")
		for _, name := range infoCmds {
			cmd := registry.Command(name)
			b.WriteString(fmt.Sprintf("    %-14s %s\n", "/"+name, cmd.Description))
		}
		b.WriteString("\n")
	}

	if len(actionCmds) > 0 {
		b.WriteString("  Actions:\n")
		for _, name := range actionCmds {
			cmd := registry.Command(name)
			b.WriteString(fmt.Sprintf("    %-14s %s\n", "/"+name, cmd.Description))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// resolveModelSwitch 把 /model 参数解析成目标 (provider, modelID)。
//
// 网关模型 ID 普遍含斜杠（如 openai/workbuddy/kimi-k3），用户从 /models
// 列表复制的就是展示全名 Provider/ModelID，因此解析顺序：
//  1. 全名精确匹配（含斜杠原样采用，不再拼接 provider）
//  2. provider:model 冒号形式
//  3. 裸名在 ModelID 上唯一命中时采用
//
// available 为空（非编码应用等拿不到目录）时保持旧行为：冒号拆分，裸名
// 继承当前 provider。目录存在但未命中时报错并列出候选，避免「切换成功、
// 下次请求 404」。
func resolveModelSwitch(input string, available []slashcmd.ModelInfo) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errors.New("model name is empty")
	}
	if len(available) == 0 {
		if parts := strings.SplitN(input, ":", 2); len(parts) == 2 {
			return parts[0], parts[1], nil
		}
		return "", input, nil
	}

	byFull := make(map[string]slashcmd.ModelInfo, len(available))
	byModel := make(map[string][]slashcmd.ModelInfo, len(available))
	for _, m := range available {
		byFull[m.Provider+"/"+m.ModelID] = m
		byModel[m.ModelID] = append(byModel[m.ModelID], m)
	}
	if m, ok := byFull[input]; ok {
		return m.Provider, m.ModelID, nil
	}
	if parts := strings.SplitN(input, ":", 2); len(parts) == 2 {
		if m, ok := byFull[input]; ok {
			return m.Provider, m.ModelID, nil
		}
		for _, m := range available {
			if m.Provider == parts[0] && m.ModelID == parts[1] {
				return m.Provider, m.ModelID, nil
			}
		}
		return "", "", unknownModelErr(input, available)
	}
	if matches := byModel[input]; len(matches) == 1 {
		return matches[0].Provider, matches[0].ModelID, nil
	} else if len(matches) > 1 {
		return "", "", fmt.Errorf("model %q is ambiguous across providers; use provider:model", input)
	}
	return "", "", unknownModelErr(input, available)
}

func unknownModelErr(input string, available []slashcmd.ModelInfo) error {
	const maxCandidates = 8
	names := make([]string, 0, len(available))
	for _, m := range available {
		if len(names) >= maxCandidates {
			names = append(names, "…")
			break
		}
		names = append(names, m.Provider+"/"+m.ModelID)
	}
	return fmt.Errorf("unknown model %q; available: %s", input, strings.Join(names, ", "))
}

// modelLabel 渲染模型展示名：模型 ID 已含 provider 前缀（如
// openai/workbuddy/*）时不重复拼接，避免 openai/openai/… 的观感。
func modelLabel(provider, modelID string) string {
	if strings.HasPrefix(modelID, provider+"/") {
		return modelID
	}
	return provider + "/" + modelID
}
