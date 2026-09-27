package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// handleKeyPress processes all keyboard input, routing to the appropriate
// context handler based on what overlay/popup is active.
func (m *TuiModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ── Priority 1: Confirmation dialog ──
	if m.confirmation.IsActive() {
		approved, _, resolved := m.confirmation.HandleKey(msg)
		if resolved {
			m.resolveConfirmation(approved)
		}
		// Consume all keys when dialog is active (but don't resolve on navigation keys)
		return m, nil
	}

	// Model selection owns its keys before generic completion.
	if m.modelSelect {
		return m.handleModelSelectKey(msg)
	}
	if m.completion.IsActive() {
		return m.handleCompletionKey(msg)
	}

	// ── Priority 4: Normal input context ──
	return m.handleInputKey(msg)
}

// handleInputKey handles keys in the normal input context.
func (m *TuiModel) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := DefaultKeyBindings.ResolveInput(msg)

	switch action {

	case ActionCancel: // Ctrl+C
		if m.agentBusy {
			m.streaming = false
			m.agentBusy = false
			m.spinnerOn = false
			// 真正中断底层 LLM 流/工具执行（否则 token 会烧到本轮结束）
			m.cancelStream()
			// Save partial response and clear stream buffer
			if m.streamBuf != "" {
				m.messages = append(m.messages, ChatMessage{
					Role:      "assistant",
					Content:   m.streamBuf + "\n\n_⚠ Interrupted_",
					Timestamp: time.Now(),
				})
				m.streamBuf = ""
			}
			m.appliedStreamLen = 0
			m.viewport.SetStreaming("")
			m.viewport.SetMessages(m.messages)
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case ActionExit: // Ctrl+D
		if m.input.IsEmpty() {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case ActionClearScreen: // Ctrl+L
		m.viewport.Clear()
		return m, nil

	case ActionToggleToolPanel: // Ctrl+O
		if len(m.messages) > 0 && len(m.messages[len(m.messages)-1].Tools) > 0 {
			idx := len(m.messages) - 1
			lastTool := len(m.messages[idx].Tools) - 1
			m.messages[idx].Tools[lastTool].Collapsed = !m.messages[idx].Tools[lastTool].Collapsed
			m.viewport.SetMessages(m.messages)
		}
		return m, nil

	case ActionOpenModelSelect: // Ctrl+P
		return m.openModelSelector()

	case ActionSubmit: // Enter
		input := m.input.Text()
		if input == "" {
			return m, nil
		}
		// Final safety check: strip any remaining control characters
		input = sanitizeInput(input)
		if input == "" {
			return m, nil
		}
		if slashcmd.IsSlashCommand(input) {
			return m.handleSlashCommand(input)
		}
		lower := strings.ToLower(strings.TrimSpace(input))
		if lower == "exit" || lower == "quit" {
			m.quitting = true
			return m, tea.Quit
		}
		return m.sendMessage(input)

	case ActionAcceptCompletion: // Tab
		// No popup visible — offer completions for whatever is typed
		// (slash command, its subcommands, or @file).
		m.checkTriggerCompletion()
		return m, nil

	case ActionNewline: // Ctrl+J
		m.input.newLine()
		// Resize viewport to account for new input line
		m.viewport.Resize(m.width, m.height-m.inputHeight()-m.statusBarHeight())
		return m, nil

	case ActionSearchHistory: // Ctrl+R
		m.input.navigateHistory(-1)
		return m, nil

	case ActionPageUp:
		m.viewport.ScrollUp(m.viewport.height)
		return m, nil

	case ActionPageDown:
		m.viewport.ScrollDown(m.viewport.height)
		return m, nil

	case ActionClosePopup:
		m.completion.Close()
		return m, nil

	default:
		m.input.HandleKey(msg)
		m.checkTriggerCompletion()
		return m, nil
	}
}

// handleCompletionKey handles keys when the autocomplete popup is visible.
func (m *TuiModel) handleCompletionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := DefaultKeyBindings.ResolveCompletion(msg)

	switch action {
	case ActionAcceptCompletion:
		item := m.completion.SelectedItem()
		if item != nil && msg.Type == tea.KeyEnter && item.InsertText == "/models" {
			m.input.Reset()
			m.completion.Close()
			return m.openModelSelector()
		}
		if item != nil {
			m.acceptCompletion(item.InsertText)
			if m.completion.Kind() == CompletionSlash {
				// Accepting a command ("/feishu") should immediately offer
				// its subcommands.
				m.checkTriggerCompletion()
			} else {
				m.completion.Close()
			}
		}
		return m, nil

	case ActionHistoryNext:
		m.completion.Next()
		return m, nil

	case ActionHistoryPrev:
		m.completion.Prev()
		return m, nil

	case ActionClosePopup:
		m.completion.Close()
		return m, nil

	default:
		m.input.HandleKey(msg)
		m.checkTriggerCompletion()
		return m, nil
	}
}

// openModelSelector shares the same catalog and behavior for /models and Ctrl+P.
func (m *TuiModel) openModelSelector() (tea.Model, tea.Cmd) {
	if !m.completion.TriggerModel(m.getAvailableModels()) {
		m.modelSelect = false
		m.messages = append(m.messages, ChatMessage{Role: "system", Content: "No models available from the configured catalog."})
		m.viewport.SetMessages(m.messages)
		return m, nil
	}
	m.modelSelect = true
	for i, item := range m.completion.Items() {
		if item.InsertText == m.provider+"/"+m.modelID {
			m.completion.selected = i
			break
		}
	}
	return m, nil
}

// handleModelSelectKey handles keys when the model selector popup is open.
func (m *TuiModel) handleModelSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlP:
		m.modelSelect = false
		m.completion.Close()
		return m, nil
	case tea.KeyEnter:
		// Apply the selected model
		item := m.completion.SelectedItem()
		m.modelSelect = false
		if item != nil {
			// Parse "provider/modelID" from InsertText
			parts := strings.SplitN(item.InsertText, "/", 2)
			if len(parts) == 2 {
				provider := parts[0]
				modelID := parts[1]
				if err := m.session.SwitchModel(context.Background(), modelID, provider); err != nil {
					m.messages = append(m.messages, ChatMessage{
						Role:      "system",
						Content:   fmt.Sprintf("❌ Model switch failed: %v", err),
						Timestamp: time.Now(),
					})
				} else {
					m.provider = provider
					m.modelID = modelID
					m.messages = append(m.messages, ChatMessage{
						Role:      "system",
						Content:   fmt.Sprintf("✅ Switched: %s/%s", provider, modelID),
						Timestamp: time.Now(),
					})
				}
				m.viewport.SetMessages(m.messages)
			}
		}
		m.completion.Close()
		return m, nil
	case tea.KeyUp:
		m.completion.Prev()
		return m, nil
	case tea.KeyDown:
		m.completion.Next()
		return m, nil
	default:
		return m, nil
	}
}

// checkTriggerCompletion evaluates the current input and triggers the
// appropriate completion popup.
func (m *TuiModel) checkTriggerCompletion() {
	if m.agentBusy {
		m.completion.Close()
		return
	}
	input := m.input.Text()
	cursorX := m.input.cursorX

	if m.completion.TriggerSlash(input, cursorX, m.slashCmds) {
		return
	}
	if m.completion.TriggerSub(input, cursorX, m.slashCmds) {
		return
	}
	if m.completion.TriggerFile(input, cursorX, m.workspace) {
		return
	}
	m.completion.Close()
}

// acceptCompletion replaces the trigger text in the input with the completion.
func (m *TuiModel) acceptCompletion(insertText string) {
	switch m.completion.Kind() {
	case CompletionSlash:
		fullText := m.input.Text()
		beforeCursor := substringBefore(fullText, m.input.cursorX)
		slashIdx := strings.LastIndex(beforeCursor, "/")
		if slashIdx < 0 {
			return
		}
		afterCursor := string([]rune(fullText)[m.input.cursorX:])
		newText := beforeCursor[:slashIdx] + insertText + " " + afterCursor
		m.input.lines = []string{newText}
		m.input.cursorX = slashIdx + len(insertText) + 1
		m.input.cursorY = 0

	case CompletionFile:
		fullText := m.input.Text()
		beforeCursor := substringBefore(fullText, m.input.cursorX)
		atIdx := strings.LastIndex(beforeCursor, "@")
		if atIdx < 0 {
			return
		}
		afterCursor := string([]rune(fullText)[m.input.cursorX:])
		newText := beforeCursor[:atIdx] + insertText + afterCursor
		m.input.lines = []string{newText}
		m.input.cursorX = atIdx + len(insertText)
		m.input.cursorY = 0

	case CompletionSub:
		fullText := m.input.Text()
		beforeCursor := substringBefore(fullText, m.input.cursorX)
		runes := []rune(beforeCursor)
		qs := m.completion.queryStart
		if qs > len(runes) {
			return
		}
		afterCursor := string([]rune(fullText)[m.input.cursorX:])
		newText := string(runes[:qs]) + insertText + " " + afterCursor
		m.input.lines = []string{newText}
		m.input.cursorX = qs + utf8.RuneCountInString(insertText) + 1
		m.input.cursorY = 0
	}
}

// sendMessage dispatches user input to the agent and starts streaming.
func (m *TuiModel) sendMessage(input string) (tea.Model, tea.Cmd) {
	// Add user message to history
	m.messages = append(m.messages, ChatMessage{
		Role:      "user",
		Content:   input,
		Timestamp: time.Now(),
	})
	m.input.AddHistory(input)
	m.input.Reset()

	// Start agent streaming
	m.streaming = true
	m.agentBusy = true
	m.spinnerOn = true
	m.streamBuf = ""

	m.viewport.SetMessages(m.messages)
	m.viewport.GotoBottom()

	return m, m.startAgentStream(input)
}

// startAgentStream runs the agent stream loop in a goroutine.
// CRITICAL: We must NOT mutate model fields from inside this goroutine.
// Instead, we send each event to the Bubble Tea program via program.Send(),
// which safely delivers it to the Update() function on the main goroutine.
func (m *TuiModel) startAgentStream(input string) tea.Cmd {
	program := m.program // capture before goroutine starts
	return func() tea.Msg {
		// 可取消 ctx：Ctrl+C 中断时真正掐断底层 LLM 流与工具执行
		ctx, cancel := context.WithCancel(context.Background())
		m.registerStreamCancel(cancel)
		defer cancel()
		stream, err := m.session.PromptStream(ctx, input)
		if err != nil {
			return AgentErrorMsg{Err: err}
		}

		done := false
		for event := range stream {
			var msg tea.Msg
			switch event.Type {
			case agent.StreamEventTextDelta:
				msg = StreamTextMsg{Delta: event.TextDelta}

			case agent.StreamEventToolStart:
				msg = ToolStartMsg{
					ID:   event.ToolCallID,
					Name: event.ToolName,
					Args: event.ToolArgs,
				}

			case agent.StreamEventToolUpdate:
				msg = ToolUpdateMsg{
					ID:     event.ToolCallID,
					Name:   event.ToolName,
					Result: event.ToolResult,
				}

			case agent.StreamEventToolEnd:
				msg = ToolEndMsg{
					ID:      event.ToolCallID,
					Name:    event.ToolName,
					Result:  event.ToolResult,
					IsError: event.IsError,
				}

			case agent.StreamEventCompacted:
				msg = CompactionMsg{Summary: event.Summary}

			case agent.StreamEventMicroCompacted:
				msg = CompactionMsg{Summary: "micro-compact: " + event.Summary}

			case agent.StreamEventLoopDetected:
				msg = LoopDetectedMsg{Tool: event.ToolName, Count: event.RepeatCount}

			case agent.StreamEventError:
				msg = AgentErrorMsg{Err: fmt.Errorf("%s", event.Error)}

			case agent.StreamEventDone:
				msg = StreamDoneMsg{
					InputTokens:  event.Usage.InputTokens,
					OutputTokens: event.Usage.OutputTokens,
				}
				done = true

			case agent.StreamEventTurnEnd:
				// Turn ended but stream may continue (multi-turn)
				continue
			}

			// Send each event immediately to the TUI for live updates.
			if program != nil && msg != nil {
				program.Send(msg)
			}
		}

		// If StreamEventDone was received, the final StreamDoneMsg was already
		// sent via program.Send(). Return nil to avoid a duplicate.
		if done {
			return nil
		}
		// Fallback: stream closed without a Done event
		return StreamDoneMsg{}
	}
}

// handleSlashCommand processes /commands locally.
func (m *TuiModel) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	m.input.Reset()
	if strings.TrimSpace(input) == "/models" {
		return m.openModelSelector()
	}

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: m.session,
		App:     m.app,
	}
	result, err := m.slashCmds.Execute(cmdCtx, input)
	if err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role:    "system",
			Content: fmt.Sprintf("❌ Command error: %v", err),
		})
	} else {
		m.messages = append(m.messages, ChatMessage{
			Role:    "user",
			Content: input,
		})
		if result.Output != "" {
			m.messages = append(m.messages, ChatMessage{
				Role:    "system",
				Content: result.Output,
			})
		}

		// Handle session switch (/new, /switch)
		if result.SessionSwitchTo != nil {
			// Convert SessionContext to AgentSession
			if as, ok := result.SessionSwitchTo.(*runtime.AgentSession); ok {
				m.session = as
				m.provider, m.modelID = as.ModelInfo()
				m.messages = []ChatMessage{} // clear conversation display
				m.inputTokens = 0
				m.outputTokens = 0
			}
		}

		// Handle clear screen
		if result.ClearScreen {
			m.viewport.Clear()
			m.messages = []ChatMessage{}
		}
	}

	m.viewport.SetMessages(m.messages)
	m.viewport.GotoBottom()
	return m, nil
}

// getAvailableModels returns the list of models the user can switch to.
// 从 App 的模型注册表取（内置清单 + 网关 /models 同步），不再直连内置目录——
// 否则 TUI 切换器看不到网关模型（与 /models 命令不一致）。
func (m *TuiModel) getAvailableModels() []ModelOption {
	if m.app == nil {
		return nil
	}
	infos := m.app.AvailableModels()
	result := make([]ModelOption, 0, len(infos))
	for _, mi := range infos {
		result = append(result, ModelOption{
			Provider:    mi.Provider,
			ModelID:     mi.ModelID,
			Description: "",
		})
	}
	return result
}
