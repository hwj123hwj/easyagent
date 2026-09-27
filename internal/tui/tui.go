package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	input      InputModel
	viewport   MessageViewport
	statusBar  StatusBar
	spinnerOn  bool
	spinnerIdx int

	// State
	messages  []ChatMessage
	streaming bool   // LLM is generating text
	agentBusy bool   // agent is running tools or thinking
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
	completion   CompletionState
	confirmation *ConfirmationState
	modelSelect  bool         // Ctrl+P model selector popup active
	program      *tea.Program // ref to program for sending msgs

	// App context for slash commands that need session management (/new, /switch, etc.)
	app slashcmd.AppContext

	// autoApprove 全权模式：跳过危险工具确认（config.auto_approve）
	autoApprove bool
}

// SetProgram stores a reference to the tea.Program so we can send msgs from callbacks.
func (m *TuiModel) SetProgram(p *tea.Program) {
	m.program = p
}

// New creates a new TuiModel.
func New(session *runtime.AgentSession, cmds *slashcmd.Registry, autoApprove bool) *TuiModel {
	provider, modelID := session.ModelInfo()
	m := &TuiModel{
		width:        80,
		height:       24,
		input:        NewInputModel(),
		viewport:     NewMessageViewport(80, 20),
		statusBar:    *NewStatusBar(),
		messages:     []ChatMessage{},
		session:      session,
		slashCmds:    cmds,
		provider:     provider,
		modelID:      modelID,
		confirmCh:    make(chan ConfirmationResultMsg, 1),
		theme:        DefaultTheme(),
		completion:   NewCompletionState(),
		confirmation: NewConfirmationState(),
	}

	// Wire confirmation callback：始终安装对话框，autoApprove/-y 只决定
	// 初始状态；会话内随时 /confirm on|off 运行时切换。
	m.autoApprove = autoApprove
	session.SetConfirmFunc(func(ctx context.Context, req agent.ConfirmationRequest) agent.ConfirmDecision {
		return m.handleConfirmation(ctx, req)
	})
	session.SetConfirmEnabled(!autoApprove)

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
	switch msg := msg.(type) {

	// ── Terminal resize ──
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
		if m.confirmation.IsActive() {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.completion.IsActive() {
				m.completion.Prev()
			} else {
				m.viewport.ScrollUp(3)
			}
		case tea.MouseButtonWheelDown:
			if m.completion.IsActive() {
				m.completion.Next()
			} else {
				m.viewport.ScrollDown(3)
			}
		}
		return m, nil

	// ── Agent events ──
	case StreamTextMsg:
		m.streamBuf += msg.Delta
		m.streaming = true
		m.agentBusy = true
		m.viewport.SetStreaming(m.streamBuf)
		return m, m.spinnerTick()

	case ToolStartMsg:
		m.agentBusy = true
		m.spinnerOn = true
		// Create a pending tool entry on the LAST message.
		// If messages is empty, create a placeholder.
		if len(m.messages) == 0 {
			m.messages = append(m.messages, ChatMessage{
				Role:      "assistant",
				Content:   "",
				Timestamp: time.Now(),
			})
		}
		idx := len(m.messages) - 1
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
		m.spinnerOn = false
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
					m.viewport.SetMessages(m.messages)
					return m, nil
				}
			}
		}
		return m, nil

	case StreamDoneMsg:
		if m.streamBuf != "" {
			m.messages = append(m.messages, ChatMessage{
				Role:      "assistant",
				Content:   m.streamBuf,
				Timestamp: time.Now(),
			})
			m.streamBuf = ""
		}
		m.streaming = false
		m.agentBusy = false
		m.spinnerOn = false
		// Accumulate token usage
		m.inputTokens += msg.InputTokens
		m.outputTokens += msg.OutputTokens
		m.viewport.SetStreaming("")
		m.viewport.SetMessages(m.messages)
		return m, nil

	case AgentErrorMsg:
		m.streaming = false
		m.agentBusy = false
		m.spinnerOn = false
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
		m.messages = append(m.messages, ChatMessage{
			Role:      "system",
			Content:   fmt.Sprintf("📦 Context compacted: %s", msg.Summary),
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
		if m.spinnerOn || m.streaming {
			m.spinnerIdx++
			return m, m.spinnerTick()
		}
		return m, nil
	}

	return m, nil
}

// View implements tea.Model.
func (m *TuiModel) View() string {
	if m.quitting {
		return "Goodbye! 👋\n"
	}

	// ── Confirmation dialog overlays on top of normal view ──
	if m.confirmation.IsActive() {
		var baseBuf strings.Builder
		baseBuf.WriteString(m.viewport.View())
		baseBuf.WriteByte('\n')
		baseBuf.WriteString(m.theme.Separator.Render(strings.Repeat("─", m.width)))
		baseBuf.WriteByte('\n')
		baseBuf.WriteString(m.input.View())
		baseBuf.WriteByte('\n')
		baseBuf.WriteString(m.statusBar.HelpHint(m.agentBusy))
		baseBuf.WriteByte('\n')
		baseBuf.WriteString(m.statusBar.Render(
			m.width, "confirm", m.spinnerIdx,
			m.provider, m.modelID, m.workspace, m.streaming,
			m.inputTokens, m.outputTokens,
		))

		// Overlay confirmation dialog centered on screen
		dialog := m.confirmation.Render(m.width)
		dialogLines := strings.Split(dialog, "\n")
		dialogHeight := len(dialogLines)
		blankLines := (m.height - dialogHeight) / 2
		if blankLines < 0 {
			blankLines = 0
		}
		return baseBuf.String() + strings.Repeat("\n", blankLines) + dialog
	}

	popupText := ""
	if m.completion.IsActive() {
		rows := maxInt(1, (m.height-m.inputHeight()-m.statusBarHeight())/2-3)
		popupText = NewCompletionPopup().Render(&m.completion, m.width, rows)
	}
	viewportHeight := maxInt(1, m.height-m.inputHeight()-m.statusBarHeight()-lipgloss.Height(popupText))
	if popupText == "" {
		viewportHeight = maxInt(1, m.height-m.inputHeight()-m.statusBarHeight())
	}
	if m.viewport.height != viewportHeight {
		m.viewport.Resize(m.width, viewportHeight)
	}
	var buf strings.Builder

	// Message viewport
	buf.WriteString(m.viewport.View())
	buf.WriteByte('\n')

	// Separator line
	sep := m.theme.Separator.Render(strings.Repeat("─", m.width))
	buf.WriteString(sep)
	buf.WriteByte('\n')

	if popupText != "" {
		buf.WriteString(popupText)
		buf.WriteByte('\n')
	}

	// Input area
	buf.WriteString(m.input.View())
	buf.WriteByte('\n')

	// Help hint
	buf.WriteString(m.statusBar.HelpHint(m.agentBusy))
	buf.WriteByte('\n')

	// Status bar
	status := "ready"
	if m.agentBusy {
		if m.streaming {
			status = "thinking"
		} else {
			status = "busy"
		}
	}
	buf.WriteString(m.statusBar.Render(
		m.width, status, m.spinnerIdx,
		m.provider, m.modelID, m.workspace, m.streaming,
		m.inputTokens, m.outputTokens,
	))

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
	for i, line := range m.input.lines {
		segs := wrapVisual(line, m.input.wrapWidth(i == 0))
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
	return 4 // separator + help hint + status bar + blank
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
		return fmt.Sprintf("%v", v)
	}
}

func (m *TuiModel) spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg{Time: t}
	})
}

// handleConfirmation is called by the agent when a dangerous tool needs user approval.
// It shows the TUI confirmation dialog and blocks until the user responds.
func (m *TuiModel) handleConfirmation(ctx context.Context, req agent.ConfirmationRequest) agent.ConfirmDecision {
	slog.Info("confirmation requested", "tool", req.ToolName, "desc", req.Description)

	// Show the confirmation dialog
	m.confirmation.Show(req.ToolCallID, req.ToolName, req.Description)

	// Notify the TUI to re-render
	if m.program != nil {
		m.program.Send(ConfirmationDialogMsg{
			ToolCallID:  req.ToolCallID,
			ToolName:    req.ToolName,
			Description: req.Description,
		})
	}

	// Wait for user response
	select {
	case result := <-m.confirmation.resultChan:
		return agent.ConfirmDecision{Approved: result.Approved}
	case <-ctx.Done():
		m.confirmation.Hide()
		return agent.ConfirmDecision{Approved: false, Reason: "context cancelled"}
	}
}
