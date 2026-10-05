package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// TuiModel is the root Bubble Tea model for easyagent's interactive TUI.
type TuiModel struct {
	// Dimensions
	width  int
	height int

	// Sub-components
	input            InputModel
	viewport         MessageViewport
	statusBar        StatusBar
	spinnerIdx       int
	tickPending      bool
	selection        textSelection
	frame            []string
	frameToolTargets map[int]toolTarget
	frameCaret       screenPoint
	hasFrameCaret    bool
	copyNotice       string
	clipboardWriter  func(string) error

	// State
	messages  []ChatMessage
	streaming bool // LLM is generating text
	agentBusy bool // agent is running tools or thinking
	streamID  uint64
	streamBuf string // accumulating text from LLM
	quitting  bool

	// Session
	session   *runtime.AgentSession
	slashCmds *slashcmd.Registry

	// Agent communication
	confirmCh chan ConfirmationResultMsg // user's confirmation reply
	err       error

	// Token tracking
	inputTokens  int
	outputTokens int

	// UI metadata
	provider  string
	modelID   string
	workspace string

	// Theme
	theme *Theme

	// Phase 3: completion + confirmation + model selector
	completion    CompletionState
	confirmation  *ConfirmationState
	modelSelect   bool // Ctrl+P model selector popup active
	modelCatalog  []CompletionItem
	searchCatalog []CompletionItem
	pickerID      uint64
	toolFocus     bool
	focusedTool   toolTarget
	runStarted    time.Time
	program       *tea.Program // ref to program for sending msgs

	// 当前 agent 流的取消函数：Ctrl+C 时真正中断底层 LLM 流/工具执行，
	// 而不只是把 UI 状态置停。cmd goroutine 写、Update goroutine 读，需互斥。
	streamCancelMu sync.Mutex
	streamCancel   context.CancelFunc

	// 已应用到 viewport 的流式文本长度（流式 100ms 节流用）
	appliedStreamLen int

	// App context for slash commands that need session management (/new, /switch, etc.)
	app slashcmd.AppContext

	// autoApprove 全权模式：跳过危险工具确认（config.auto_approve）
	autoApprove bool
}

// registerStreamCancel 在流启动时注册取消函数。
func (m *TuiModel) registerStreamCancel(cancel context.CancelFunc) {
	m.streamCancelMu.Lock()
	m.streamCancel = cancel
	m.streamCancelMu.Unlock()
}

// cancelStream 取消当前流（若有）并清空注册，返回是否确实取消了流。
func (m *TuiModel) cancelStream() bool {
	m.streamCancelMu.Lock()
	cancel := m.streamCancel
	m.streamCancel = nil
	m.streamCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return cancel != nil
}

// SetProgram stores a reference to the tea.Program so we can send msgs from callbacks.
func (m *TuiModel) SetProgram(p *tea.Program) {
	m.program = p
}

// New creates a new TuiModel.
func New(session *runtime.AgentSession, cmds *slashcmd.Registry, autoApprove bool) *TuiModel {
	provider, modelID := session.ModelInfo()
	m := &TuiModel{
		width:           80,
		height:          24,
		input:           NewInputModel(),
		viewport:        NewMessageViewport(80, 20),
		statusBar:       *NewStatusBar(),
		messages:        []ChatMessage{},
		session:         session,
		slashCmds:       cmds,
		provider:        provider,
		modelID:         modelID,
		confirmCh:       make(chan ConfirmationResultMsg, 1),
		theme:           DefaultTheme(),
		clipboardWriter: writeClipboard,
		completion:      NewCompletionState(),
		confirmation:    NewConfirmationState(),
	}

	// Wire confirmation callback：始终安装对话框，autoApprove/-y 只决定
	// 初始状态；会话内随时 /confirm on|off 运行时切换。
	m.autoApprove = autoApprove
	session.SetConfirmFunc(func(ctx context.Context, req agent.ConfirmationRequest) agent.ConfirmDecision {
		return m.handleConfirmation(ctx, req)
	})
	session.SetConfirmEnabled(!autoApprove)

	m.restoreHistory()
	return m
}

// SetWorkspace sets the workspace path for display.
func (m *TuiModel) SetWorkspace(ws string) {
	m.workspace = ws
}

// Init implements tea.Model.
func (m *TuiModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *TuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if event, ok := msg.(streamEventMsg); ok {
		if event.id != m.streamID {
			return m, nil
		}
		msg = event.msg
	}
	switch msg := msg.(type) {
	case sessionPickerListMsg:
		return m.receiveSessionList(msg)
	case sessionPickerSwitchMsg:
		return m.receiveSessionSwitch(msg)

	// ── Terminal resize ──
	case tea.WindowSizeMsg:
		m.clearSelection()
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		// Bugfix: 输入框此前不知道终端宽度，长行（长句/URL/中英混排）渲染成单行被截断。
		m.input.SetWidth(msg.Width)
		// Viewport gets: total height - input area - status bar - separators
		viewportHeight := msg.Height - m.inputHeight() - m.statusBarHeight()
		if viewportHeight < 3 {
			viewportHeight = 3
		}
		m.viewport.Resize(msg.Width, viewportHeight)
		return m, nil

	// ── Key press ──
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case clipboardResultMsg:
		if msg.err != nil {
			m.copyNotice = "Copy failed: " + msg.err.Error()
		} else {
			m.copyNotice = "Copied | Esc: clear selection"
		}
		return m, nil

	case ConfirmationDialogMsg:
		m.clearSelection()
		m.confirmation.Show(msg.ToolCallID, msg.ToolName, msg.Description)
		m.confirmation.resultChan = msg.Result
		return m, nil
	case confirmationCancelledMsg:
		if m.confirmation.resultChan == msg.result {
			m.confirmation.Hide()
			m.clearSelection()
		}
		return m, nil

	// ── Agent events ──
	case StreamTextMsg:
		first := m.streamBuf == ""
		m.streamBuf += msg.Delta
		m.streaming = true
		m.agentBusy = true
		// Show the first chunk immediately; coalesce subsequent chunks at 100ms.
		if first {
			m.viewport.SetStreaming(m.streamBuf)
			m.appliedStreamLen = len(m.streamBuf)
		}
		return m, m.spinnerTick()

	case StreamTurnEndMsg:
		text := msg.Text
		if text == "" {
			text = m.streamBuf
		}
		if text != "" {
			m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: text, Timestamp: time.Now()})
		}
		m.streamBuf = ""
		m.appliedStreamLen = 0
		m.streaming = false
		m.viewport.SetStreaming("")
		m.viewport.SetMessages(m.messages)
		return m, nil

	case ToolStartMsg:
		m.streaming = false
		m.agentBusy = true
		// Group tools under the current user turn, even if progress/compaction
		// messages were appended while it was running.
		if len(m.messages) == 0 {
			m.messages = append(m.messages, ChatMessage{
				Role:      "assistant",
				Content:   "",
				Timestamp: time.Now(),
			})
		}
		idx := len(m.messages) - 1
		for i := idx; i >= 0; i-- {
			if m.messages[i].Role == "user" {
				idx = i
				break
			}
		}
		m.viewport.invalidateFrom(idx)
		if !m.messages[idx].ToolsManual {
			m.messages[idx].ToolsExpanded = true
		}
		m.messages[idx].Tools = append(m.messages[idx].Tools, ToolCallInfo{
			ID:        msg.ID,
			Name:      msg.Name,
			Args:      formatArgsForDisplay(msg.Args),
			Streaming: true,
			Collapsed: true,
			StartTime: time.Now(),
		})
		m.viewport.SetMessages(m.messages)
		return m, m.spinnerTick()

	case ToolEndMsg:
		// Find the matching tool call by ID (primary) or name (fallback).
		// Search from the last message backwards.
		found := false
		for msgIdx := len(m.messages) - 1; msgIdx >= 0; msgIdx-- {
			for i := range m.messages[msgIdx].Tools {
				t := &m.messages[msgIdx].Tools[i]
				if t.Streaming && ((msg.ID != "" && t.ID == msg.ID) || (msg.ID == "" && t.Name == msg.Name)) {
					t.Streaming = false
					t.Result = fmt.Sprintf("%v", msg.Result)
					t.IsError = msg.IsError
					t.EndTime = time.Now()
					if msg.IsError && !t.Manual {
						t.Collapsed = false
					}
					if msg.IsError && !m.messages[msgIdx].ToolsManual {
						m.messages[msgIdx].ToolsExpanded = true
					}
					m.viewport.invalidateFrom(msgIdx)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		m.viewport.SetMessages(m.messages)
		return m, nil

	case ToolUpdateMsg:
		// Update partial result for a running tool (live progress)
		for msgIdx := len(m.messages) - 1; msgIdx >= 0; msgIdx-- {
			for i := range m.messages[msgIdx].Tools {
				t := &m.messages[msgIdx].Tools[i]
				if t.Streaming && ((msg.ID != "" && t.ID == msg.ID) || (msg.ID == "" && t.Name == msg.Name)) {
					t.Result = fmt.Sprintf("%v", msg.Result)
					m.viewport.invalidateFrom(msgIdx)
					m.viewport.SetMessages(m.messages)
					return m, nil
				}
			}
		}
		return m, nil

	case StreamDoneMsg:
		m.finishToolGroups(false)
		if m.streamBuf != "" {
			m.messages = append(m.messages, ChatMessage{
				Role:      "assistant",
				Content:   m.streamBuf,
				Timestamp: time.Now(),
			})
			m.streamBuf = ""
		}
		m.appliedStreamLen = 0
		m.streaming = false
		m.agentBusy = false
		// Accumulate token usage
		m.inputTokens += msg.InputTokens
		m.outputTokens += msg.OutputTokens
		m.viewport.SetStreaming("")
		m.viewport.SetMessages(m.messages)
		// 终端响铃 + 角标提醒：长任务跑完不用来回瞄（用户已滚屏时才响）
		if m.viewport.userScrolled {
			return m, tea.Bell()
		}
		return m, nil

	case AgentErrorMsg:
		m.streamID++ // ignore completion/deltas still queued by the failed run
		m.cancelStream()
		m.finishToolGroups(true)
		m.streaming = false
		m.agentBusy = false
		if m.streamBuf != "" {
			m.messages = append(m.messages, ChatMessage{
				Role: "assistant", Content: m.streamBuf, Timestamp: time.Now(),
			})
		}
		m.streamBuf = ""
		m.appliedStreamLen = 0
		m.viewport.SetStreaming("")
		m.viewport.SetMessages(m.messages)
		// 用户 Ctrl+C 主动取消导致的 context canceled 不是错误，不打扰
		if errors.Is(msg.Err, context.Canceled) {
			return m, nil
		}
		m.err = msg.Err
		m.messages = append(m.messages, ChatMessage{
			Role:      "system",
			Content:   fmt.Sprintf("❌ Error: %v", msg.Err),
			Timestamp: time.Now(),
		})
		m.viewport.SetMessages(m.messages)
		return m, nil

	case ConfirmationMsg:
		// Show confirmation prompt in viewport
		m.messages = append(m.messages, ChatMessage{
			Role:      "system",
			Content:   fmt.Sprintf("⚠️ Confirm: %s (y/n)", msg.Req.Description),
			Timestamp: time.Now(),
		})
		m.viewport.SetMessages(m.messages)
		return m, nil

	case CompactionMsg:
		content := msg.Summary
		if msg.Kind == "" {
			content = fmt.Sprintf("📦 Context compacted: %s", msg.Summary)
		}
		m.messages = append(m.messages, ChatMessage{
			Role:      "system",
			Content:   content,
			Timestamp: time.Now(),
		})
		m.viewport.SetMessages(m.messages)
		return m, nil

	case LoopDetectedMsg:
		m.messages = append(m.messages, ChatMessage{
			Role:      "system",
			Content:   fmt.Sprintf("⚠️ Loop detected: %s (%d repeats)", msg.Tool, msg.Count),
			Timestamp: time.Now(),
		})
		m.viewport.SetMessages(m.messages)
		return m, nil

	case TickMsg:
		m.tickPending = false
		if m.agentBusy {
			m.spinnerIdx++
			// 节流应用流式文本：100ms 一次，替代每个 delta 全量重渲
			if len(m.streamBuf) != m.appliedStreamLen {
				m.appliedStreamLen = len(m.streamBuf)
				m.viewport.SetStreaming(m.streamBuf)
			}
			return m, m.spinnerTick()
		}
		return m, nil
	}

	return m, nil
}

// View implements tea.Model.
func (m *TuiModel) View() string {
	if len(m.selection.lines) > 0 && !m.quitting {
		lines := strings.Split(m.selection.view(), "\n")
		if len(lines) >= 3 && m.copyNotice != "" {
			lines[len(lines)-2] = ansi.Truncate(m.copyNotice, m.width, "")
		}
		return strings.Join(lines, "\n")
	}
	view := m.renderView()
	m.frame = strings.Split(ansi.Strip(view), "\n")
	m.frameToolTargets = make(map[int]toolTarget)
	if !m.confirmation.IsActive() && m.height >= 5 && !(m.height == 5 && m.completion.IsActive()) && !m.quitting {
		for row, hit := range m.viewport.toolTargets {
			screenRow := row - m.viewport.scrollOffset
			limit := m.viewport.height
			if screenRow >= 0 && screenRow < limit {
				m.frameToolTargets[screenRow] = hit
			}
		}
	}
	// The caret is a rendering marker, not part of copied input.
	if m.hasFrameCaret && m.input.cursorX == len([]rune(m.input.lines[m.input.cursorY])) {
		y := m.frameCaret.y
		if y >= 0 && y < len(m.frame) {
			m.frame[y] = strings.TrimSuffix(m.frame[y], "│")
		}
	}

	return view
}

func (m *TuiModel) renderView() string {
	m.hasFrameCaret = false
	if m.quitting {
		return "Goodbye! 👋\n"
	}

	if m.confirmation.IsActive() {
		lines := strings.Split(m.confirmation.Render(m.width), "\n")
		if m.height < 5 {
			lines = []string{"Confirm: Y yes / N no / Esc cancel"}
		} else if len(lines) > m.height {
			lines = append(lines[:m.height-3], lines[len(lines)-3:]...)
		}
		for len(lines) < m.height {
			lines = append(lines, "")
		}
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], m.width, "")
		}
		tea.SetCursorPosition(0, 0)
		return strings.Join(lines, "\n")
	}
	if m.height <= 5 && m.completion.IsActive() {
		tea.SetCursorPosition(0, 0)
		return NewCompletionPopup().RenderHeight(&m.completion, m.width, m.height)
	}
	if m.height < 5 {
		col, row := m.input.CursorPosition()
		lines := strings.Split(m.input.View(), "\n")
		start := max(0, row-m.height+1)
		lines = lines[start:min(len(lines), start+m.height)]
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], m.width, "")
		}
		m.frameCaret = screenPoint{min(col, m.width-1), row - start}
		m.hasFrameCaret = true
		tea.SetCursorPosition(m.frameCaret.x, m.frameCaret.y)
		return strings.Join(lines, "\n")
	}
	inputLines := strings.Split(m.input.View(), "\n")
	col, cursorRow := m.input.CursorPosition()
	inputLimit := maxInt(1, min(8, m.height-5))
	if m.completion.IsActive() {
		inputLimit = max(1, min(inputLimit, m.height-6))
	}
	inputStart := maxInt(0, cursorRow-inputLimit+1)
	inputEnd := min(len(inputLines), inputStart+inputLimit)
	inputLines = inputLines[inputStart:inputEnd]
	popupLimit := maxInt(0, m.height-len(inputLines)-m.statusBarHeight()-1)
	popupText := NewCompletionPopup().RenderHeight(&m.completion, m.width, popupLimit)
	viewportHeight := maxInt(1, m.height-len(inputLines)-m.statusBarHeight()-lipgloss.Height(popupText))
	if popupText == "" {
		viewportHeight = maxInt(1, m.height-len(inputLines)-m.statusBarHeight())
	}
	if m.viewport.height != viewportHeight {
		m.viewport.Resize(m.width, viewportHeight)
	}
	var buf strings.Builder

	// Message viewport
	buf.WriteString(m.focusedViewport())
	buf.WriteByte('\n')

	// Separator line
	sep := m.theme.Separator.Render(strings.Repeat("─", m.width))
	buf.WriteString(sep)
	buf.WriteByte('\n')

	if popupText != "" {
		buf.WriteString(popupText)
		buf.WriteByte('\n')
	}

	inputRow := strings.Count(buf.String(), "\n")
	buf.WriteString(strings.Join(inputLines, "\n"))
	buf.WriteByte('\n')

	// Help hint
	buf.WriteString(m.helpHint())
	buf.WriteByte('\n')

	// Status bar
	status := m.runStatus()
	mode := "confirm"
	if m.session != nil && !m.session.ConfirmEnabled() {
		mode = "auto"
	}
	if m.agentBusy && !m.runStarted.IsZero() {
		mode = formatDuration(time.Since(m.runStarted)) + " · " + mode
	}
	buf.WriteString(m.statusBar.Render(
		m.width, status, m.spinnerIdx,
		m.provider, m.modelID, m.workspace, m.streaming,
		m.inputTokens, m.outputTokens, mode,
	))
	m.frameCaret = screenPoint{min(col, m.width-1), inputRow + cursorRow - inputStart}
	m.hasFrameCaret = true
	tea.SetCursorPosition(m.frameCaret.x, m.frameCaret.y)

	// Prevent terminal line wrapping from pushing the footer outside the screen.
	lines := strings.Split(buf.String(), "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "")
	}
	return strings.Join(lines, "\n")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *TuiModel) inputHeight() int {
	// Bugfix: 长行软换行后占多个终端行，输入区高度必须按可视行算，
	// 否则换行后输入区溢出、把状态栏挤出屏幕。
	visual := 0
	for _, line := range m.input.lines {
		segs := m.input.segments(line)
		if len(segs) == 0 {
			segs = []string{""}
		}
		visual += len(segs)
	}
	if visual < 1 {
		visual = 1
	}
	return visual
}

func (m *TuiModel) statusBarHeight() int {
	return 3 // separator + help hint + status bar
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// formatArgsForDisplay converts tool args (interface{}) to a readable string.
func formatArgsForDisplay(args interface{}) string {
	if args == nil {
		return ""
	}
	switch v := args.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		if data, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(data)
		}
		return fmt.Sprintf("%v", v)
	}
}

func (m *TuiModel) spinnerTick() tea.Cmd {
	if m.tickPending {
		return nil
	}
	m.tickPending = true
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg{Time: t}
	})
}

// handleConfirmation is called by the agent when a dangerous tool needs user approval.
// It shows the TUI confirmation dialog and blocks until the user responds.
func (m *TuiModel) handleConfirmation(ctx context.Context, req agent.ConfirmationRequest) agent.ConfirmDecision {
	if ctx.Err() != nil || m.program == nil {
		return agent.ConfirmDecision{Approved: false, Reason: "confirmation unavailable"}
	}
	resultCh := make(chan ConfirmationResultMsg, 1)
	m.program.Send(ConfirmationDialogMsg{
		ToolCallID: req.ToolCallID, ToolName: req.ToolName, Description: req.Description, Result: resultCh,
	})
	select {
	case result := <-resultCh:
		return agent.ConfirmDecision{Approved: result.Approved}
	case <-ctx.Done():
		m.program.Send(confirmationCancelledMsg{result: resultCh})
		return agent.ConfirmDecision{Approved: false, Reason: "context cancelled"}
	}
}

func (m *TuiModel) helpHint() string {
	if m.copyNotice != "" {
		return m.theme.HelpText.Render(m.copyNotice)
	}
	var hints []string
	switch {
	case m.toolFocus:
		hints = []string{"↑↓: tools", "Enter: expand", "F2: copy tool", "Esc: input", "PgUp/Dn: scroll"}
	case m.modelSelect:
		hints = []string{"Type: search models", "↑↓: select", "Enter: switch", "Esc: cancel"}
	case m.completion.Kind() == CompletionHistory || m.completion.Kind() == CompletionSession:
		hints = []string{"Type: search " + m.completion.searchName(), "↑↓: select", "Enter: choose", "Esc: cancel"}
	case m.agentBusy:
		hints = []string{"Ctrl+C: cancel", "Ctrl+T: tools", "Drag: copy", "F2: reply", "F3: all"}
	default:
		hints = []string{"Enter: send", "Ctrl+J: newline", "/: commands", "Ctrl+R: history", "Ctrl+G: sessions", "Ctrl+P: models", "Ctrl+T: tools", "Drag: copy", "F2: reply", "F3: all"}
	}
	if m.viewport.userScrolled {
		hints = append([]string{fmt.Sprintf("↓ %d lines · PgDn", m.viewport.NewLinesCount())}, hints...)
	}
	return m.theme.HelpText.Render(fitHints(hints, m.width))
}

// runStatus reflects the current phase, including concurrent tools.
func (m *TuiModel) runStatus() string {
	if !m.agentBusy {
		if m.err != nil {
			return "error"
		}
		return "ready"
	}
	running := 0
	for _, msg := range m.messages {
		for _, tool := range msg.Tools {
			if tool.Streaming {
				running++
			}
		}
	}
	if running > 0 {
		return fmt.Sprintf("tools %d", running)
	}
	if m.streaming {
		return "generating"
	}
	return "thinking"
}

func fitHints(hints []string, width int) string {
	var visible []string
	for _, hint := range hints {
		candidate := strings.Join(append(visible, hint), " · ")
		if lipgloss.Width(candidate) > width {
			continue
		}
		visible = append(visible, hint)
	}
	if len(visible) == 0 {
		return ansi.Truncate(hints[0], width, "")
	}
	return strings.Join(visible, " · ")
}
