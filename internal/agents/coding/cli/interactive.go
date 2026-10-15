package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/internal/ui"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// clearScreen clears the terminal display using ANSI escape sequences.
func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// InteractiveMode is the coding-agent CLI conversation loop.
// It owns coding-specific command handling and terminal presentation behavior.
type InteractiveMode struct {
	session   *runtime.AgentSession
	slashCmds *slashcmd.Registry
	app       *app.App
	presenter ui.TUIRenderer
}

// NewInteractiveMode creates a coding-agent interactive CLI.
// Uses the lightweight TUI by default; -tags fancy enables Bubble Tea.
func NewInteractiveMode(session *runtime.AgentSession, cmds *slashcmd.Registry, application *app.App) *InteractiveMode {
	return &InteractiveMode{
		session:   session,
		slashCmds: cmds,
		app:       application,
		presenter: ui.NewTUI(os.Stdout),
	}
}

// Run starts the interactive chat loop.
func (m *InteractiveMode) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	provider, modelID := m.session.ModelInfo()
	cwd, _ := os.Getwd()

	// 注入危险工具确认回调：交互模式下弹 y/n 确认。
	// 仅在交互式入口注入；serve/feishu 不注入（默认放行）。
	// auto_approve（easyagent.yaml / EA_AUTO_APPROVE / -y）只决定初始状态：
	// 会话内随时 /confirm on|off 运行时切换。
	// 时机安全：ConfirmFunc 仅在 Agent 等待确认时被调，此时主循环阻塞在
	// range stream 上、不在读 stdin，故此处独立读 os.Stdin 不会与主 scanner 抢占。
	m.session.SetConfirmFunc(func(ctx context.Context, req agent.ConfirmationRequest) agent.ConfirmDecision {
		if req.ToolName == "ask_user_question" {
			return promptAskUser(os.Stdout, os.Stdin, req)
		}
		return promptConfirm(os.Stdout, os.Stdin, req.Description)
	})
	m.session.SetConfirmEnabled(!m.app.Config().AutoApprove)

	// Print banner
	ui.PrintBanner(os.Stdout)

	// Print session status with cleaner formatting
	fmt.Println(ui.FormatSessionStatus(m.session.SessionID(), provider, modelID, m.session.Profile(), cwd))
	if ui.IsFancyMode() {
		fmt.Printf("%s  ✨ Fancy TUI mode (Bubble Tea + Lipgloss)%s\n", ui.ColorMagenta, ui.ColorReset)
	}
	fmt.Println()
	ui.PrintHelp(os.Stdout, true)

	for {
		fmt.Print(ui.FormatInputPrompt())
		if !scanner.Scan() {
			return nil
		}
		input := scanner.Text()
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			return nil
		}

		if slashcmd.IsSlashCommand(input) {
			cmdCtx := slashcmd.Context{
				Ctx:     ctx,
				Session: m.session,
				App:     m.app,
				Stdin:   os.Stdin,
				Stdout:  os.Stdout,
			}
			result, err := m.slashCmds.Execute(cmdCtx, input)
			if err != nil {
				fmt.Printf("%serror: %s%s\n", ui.ColorRed, err, ui.ColorReset)
			} else {
				// Handle clear screen first
				if result.ClearScreen {
					clearScreen()
					continue
				}
				// Handle session switch if the command requests it.
				// We only swap the pointer; the SessionRegistry owns the
				// lifecycle (including Close) for all registered sessions.
				if result.SessionSwitchTo != nil {
					oldID := m.session.SessionID()
					m.session = result.SessionSwitchTo.(*runtime.AgentSession)
					provider, modelID := m.session.ModelInfo()
					fmt.Printf("%sSession switched: %s → %s%s\n", ui.ColorYellow, oldID, m.session.SessionID(), ui.ColorReset)
					fmt.Println(ui.FormatSessionStatus(m.session.SessionID(), provider, modelID, m.session.Profile(), cwd))
					fmt.Println()
				}
				if result.Output != "" {
					fmt.Println(result.Output)
				}
			}
			// Handle ShouldQuery: auto-trigger agent execution
			if result.ShouldQuery {
				m.runPrompt(ctx, "Start working on the goal.")
				continue
			}
			continue
		}

		m.runPrompt(ctx, input)
	}
}

func (m *InteractiveMode) runPrompt(ctx context.Context, input string) {
	// Goal-driven prompts need much more time: use 30 minutes instead of 5.
	timeout := 5 * time.Minute
	if m.session != nil && m.session.Goal() != "" {
		timeout = 30 * time.Minute
	}
	promptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stream, err := m.session.PromptStream(promptCtx, input)
	if err != nil {
		fmt.Printf("%serror: %s%s\n", ui.ColorRed, err, ui.ColorReset)
		return
	}

	fmt.Print(ui.FormatAssistantPrompt())
	for event := range stream {
		m.presenter.Present(event)
	}
	fmt.Println()
}
