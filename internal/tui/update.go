package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// handleKeyPress processes all keyboard input, routing to the appropriate
// context handler based on what overlay/popup is active.
func (m *TuiModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC && len(m.selection.lines) > 0 {
		return m, m.copyText(m.selection.selectedText())
	}
	if msg.Type == tea.KeyEsc && len(m.selection.lines) > 0 {
		m.clearSelection()
		m.copyNotice = ""
		return m, nil
	}
	m.clearSelection()
	m.copyNotice = ""
	if msg.Type == tea.KeyF2 && m.toolFocus && !m.confirmation.IsActive() {
		return m, m.copyFocusedTool()
	}
	if msg.Type == tea.KeyF2 {
		return m, m.copyLastReply()
	}
	if msg.Type == tea.KeyF3 {
		return m, m.copyConversation()
	}

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
	if m.modelSelect || m.completion.IsActive() {
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD {
			m.modelSelect = false
			m.completion.Close()
			if msg.Type == tea.KeyCtrlC && !m.agentBusy {
				return m, nil
			}
			return m.handleInputKey(msg)
		}
	}
	if m.modelSelect {
		return m.handleModelSelectKey(msg)
	}
	if m.completion.Kind() == CompletionHistory || m.completion.Kind() == CompletionSession {
		return m.handleSearchPickerKey(msg)
	}
	if msg.Type == tea.KeyCtrlR {
		m.openHistoryPicker()
		return m, nil
	}
	if msg.Type == tea.KeyCtrlG {
		return m.openSessionPicker()
	}
	if m.completion.IsActive() {
		return m.handleCompletionKey(msg)
	}

	if m.toolFocus {
		return m.handleToolFocusKey(msg)
	}
	if msg.Type == tea.KeyCtrlT {
		m.openToolFocus()
		return m, nil
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
			m.streamID++ // discard late events from the cancelled run
			m.streaming = false
			m.agentBusy = false
			// 真正中断底层 LLM 流/工具执行（否则 token 会烧到本轮结束）
			m.cancelStream()
			m.finishToolGroups(true)
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
		if !m.input.IsEmpty() {
			m.copyNotice = "Draft kept · Ctrl+U: clear · Ctrl+D on empty input: exit"
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
		return m, tea.ClearScreen

	case ActionToggleToolPanel: // Ctrl+O
		m.toggleLatestToolGroup()
		return m, nil

	case ActionOpenModelSelect: // Ctrl+P
		return m.openModelSelector()
	case ActionOpenSessions: // Ctrl+G
		return m.openSessionPicker()

	case ActionSubmit: // Enter
		if m.agentBusy {
			m.copyNotice = "Working | Ctrl+C: cancel; draft kept"
			return m, nil
		}
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
		m.viewport.Resize(m.width, maxInt(1, m.height-m.inputHeight()-m.statusBarHeight()))
		return m, nil

	case ActionSearchHistory: // Ctrl+R
		m.openHistoryPicker()
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
		if item != nil && msg.Type == tea.KeyEnter && (item.InsertText == "/models" || item.InsertText == "/sessions") {
			m.input.Reset()
			m.completion.Close()
			if item.InsertText == "/sessions" {
				return m.openSessionPicker()
			}
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
	if m.agentBusy {
		m.copyNotice = "Working | Ctrl+C: cancel before switching model"
		return m, nil
	}
	if !m.completion.TriggerModel(m.getAvailableModels()) {
		m.modelSelect = false
		m.messages = append(m.messages, ChatMessage{Role: "system", Content: "No models available from the configured catalog."})
		m.viewport.SetMessages(m.messages)
		return m, nil
	}
	m.modelSelect = true
	m.completion.query = ""
	m.modelCatalog = append([]CompletionItem(nil), m.completion.items...)
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
		if item == nil {
			return m, nil
		}
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
	case tea.KeyRunes:
		m.completion.query += strings.ReplaceAll(sanitizeInput(string(msg.Runes)), "\n", " ")
		m.filterModels()
	case tea.KeyBackspace, tea.KeyCtrlH:
		query := []rune(m.completion.query)
		if len(query) > 0 {
			m.completion.query = string(query[:len(query)-1])
			m.filterModels()
		}
	case tea.KeyCtrlU:
		m.completion.query = ""
		m.filterModels()
	}
	return m, nil
}

func (m *TuiModel) filterModels() {
	filterPicker(&m.completion, m.modelCatalog)
}

func filterPicker(cm *CompletionState, catalog []CompletionItem) {
	selected := ""
	if item := cm.SelectedItem(); item != nil {
		selected = item.InsertText
	}
	cm.items = nil
	for _, item := range catalog {
		match := true
		for _, word := range strings.Fields(strings.ToLower(cm.query)) {
			if !strings.Contains(strings.ToLower(item.Label+" "+item.Description+" "+item.InsertText+" "+item.Preview), word) {
				match = false
				break
			}
		}
		if match {
			cm.items = append(cm.items, item)
		}
	}
	cm.selected = 0
	for i, item := range cm.items {
		if item.InsertText == selected {
			cm.selected = i
			break
		}
	}

}

// checkTriggerCompletion evaluates the current input and triggers the
// appropriate completion popup.
func (m *TuiModel) checkTriggerCompletion() {
	if m.agentBusy {
		m.completion.Close()
		return
	}
	input := m.input.lines[m.input.cursorY]
	cursorX := m.input.cursorX

	if m.input.cursorY == 0 {
		if m.completion.TriggerSlash(input, cursorX, m.slashCmds) {
			return
		}
		if m.completion.TriggerSub(input, cursorX, m.slashCmds) {
			return
		}
	}
	if m.completion.TriggerFile(input, cursorX, m.workspace) {
		return
	}
	m.completion.Close()
}

// acceptCompletion replaces the trigger text in the input with the completion.
func (m *TuiModel) acceptCompletion(insertText string) {
	line := []rune(m.input.lines[m.input.cursorY])
	beforeCursor := string(line[:m.input.cursorX])
	start := 0
	switch m.completion.Kind() {
	case CompletionSlash:
		slashIdx := strings.LastIndex(beforeCursor, "/")
		if slashIdx < 0 {
			return
		}
		start = utf8.RuneCountInString(beforeCursor[:slashIdx])
		insertText += " "

	case CompletionFile:
		atIdx := strings.LastIndex(beforeCursor, "@")
		if atIdx < 0 {
			return
		}
		start = utf8.RuneCountInString(beforeCursor[:atIdx])

	case CompletionSub:
		start = m.completion.queryStart
		if start < 0 || start > m.input.cursorX {
			return
		}
		insertText += " "
	default:
		return
	}
	m.input.saveUndo()
	m.input.lines[m.input.cursorY] = string(line[:start]) + insertText + string(line[m.input.cursorX:])
	m.input.cursorX = start + utf8.RuneCountInString(insertText)
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
	m.streaming = false
	m.agentBusy = true
	m.err = nil
	m.runStarted = time.Now()
	m.toolFocus = false
	m.streamBuf = ""

	m.viewport.SetMessages(m.messages)
	m.viewport.GotoBottom()

	return m, tea.Batch(m.startAgentStream(input), m.spinnerTick())
}

// startAgentStream runs the agent stream loop in a goroutine.
// CRITICAL: We must NOT mutate model fields from inside this goroutine.
// Instead, we send each event to the Bubble Tea program via program.Send(),
// which safely delivers it to the Update() function on the main goroutine.
type streamEventMsg struct {
	id  uint64
	msg tea.Msg
}

func (m *TuiModel) startAgentStream(input string) tea.Cmd {
	m.streamID++
	id := m.streamID
	ctx, cancel := context.WithCancel(context.Background())
	m.registerStreamCancel(cancel)
	program := m.program // capture before goroutine starts
	session := m.session
	return func() tea.Msg {
		// 可取消 ctx：Ctrl+C 中断时真正掐断底层 LLM 流与工具执行
		defer cancel()
		stream, err := session.PromptStream(ctx, input)
		if err != nil {
			return streamEventMsg{id, AgentErrorMsg{Err: err}}
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

			case agent.StreamEventCompactionFailed:
				msg = CompactionMsg{Kind: "failed", Summary: "上下文压缩失败，继续使用原上下文：" + event.Error}
			case agent.StreamEventMicroCompacted:
				msg = CompactionMsg{Kind: "micro", Summary: fmt.Sprintf("微压缩：清理 %d 个旧工具输出；消息估算 %d → %d tokens（后续请求沿用清理结果）", event.ClearedCount, event.TokensBefore, event.TokensAfter)}

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
				if message, ok := event.Message.(ai.AssistantMessage); ok {
					msg = StreamTurnEndMsg{Text: message.Text}
				}
			}

			// Send each event immediately to the TUI for live updates.
			if program != nil && msg != nil {
				program.Send(streamEventMsg{id, msg})
			}
		}

		// If StreamEventDone was received, the final StreamDoneMsg was already
		// sent via program.Send(). Return nil to avoid a duplicate.
		if done {
			return nil
		}
		// Fallback: stream closed without a Done event
		return streamEventMsg{id, StreamDoneMsg{}}
	}
}

// handleSlashCommand processes /commands locally.
func (m *TuiModel) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	m.input.Reset()
	if strings.TrimSpace(input) == "/models" {
		return m.openModelSelector()
	}
	if strings.TrimSpace(input) == "/sessions" || strings.TrimSpace(input) == "/switch" {
		return m.openSessionPicker()
	}

	cmdCtx := slashcmd.Context{
		Ctx:     context.Background(),
		Session: m.session,
		App:     m.app,
	}
	result, err := m.slashCmds.Execute(cmdCtx, input)
	if err == nil && result.ShouldQuery && result.QueryPrompt != "" {
		return m.sendMessage(result.QueryPrompt)
	}
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
				m.useSession(as)
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
