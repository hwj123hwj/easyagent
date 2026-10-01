package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

type sessionPickerListMsg struct {
	id       uint64
	sessions []slashcmd.SessionInfo
	err      error
}

type sessionPickerSwitchMsg struct {
	id      uint64
	session slashcmd.SessionContext
	err     error
}

func (m *TuiModel) openHistoryPicker() {
	m.searchCatalog = nil
	seen := make(map[string]bool)
	for i := len(m.input.history) - 1; i >= 0; i-- {
		text := m.input.history[i]
		if seen[text] {
			continue
		}
		seen[text] = true
		m.searchCatalog = append(m.searchCatalog, CompletionItem{
			Label: strings.Join(strings.Fields(text), " "), InsertText: text, Preview: text,
		})
	}
	m.completion.Close()
	m.completion.kind, m.completion.visible = CompletionHistory, true
	filterPicker(&m.completion, m.searchCatalog)
}

func (m *TuiModel) openSessionPicker() (tea.Model, tea.Cmd) {
	if m.agentBusy {
		m.copyNotice = "Working | Ctrl+C: cancel before switching session"
		return m, nil
	}
	if m.app == nil {
		m.copyNotice = "Session catalog unavailable"
		return m, nil
	}
	m.pickerID++
	id, app := m.pickerID, m.app
	m.completion.Close()
	m.completion.kind, m.completion.visible, m.completion.loading = CompletionSession, true, true
	m.searchCatalog = nil
	return m, func() tea.Msg {
		sessions, err := app.ListSessionsInfo()
		return sessionPickerListMsg{id, sessions, err}
	}
}

func (m *TuiModel) receiveSessionList(msg sessionPickerListMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.pickerID || m.completion.Kind() != CompletionSession {
		return m, nil
	}
	m.completion.loading = false
	if msg.err != nil {
		m.completion.Close()
		m.copyNotice = "Could not list sessions: " + msg.err.Error()
		return m, nil
	}
	sort.SliceStable(msg.sessions, func(i, j int) bool { return msg.sessions[i].LastActive > msg.sessions[j].LastActive })
	for _, s := range msg.sessions {
		title := strings.Join(strings.Fields(s.Title), " ")
		if title == "" {
			title = s.ID
		}
		marker := ""
		if s.ID == m.session.SessionID() {
			marker = " · current"
		}
		m.searchCatalog = append(m.searchCatalog, CompletionItem{
			Label: title, InsertText: s.ID,
			Description: fmt.Sprintf("%d msgs · %s%s", s.MessageCount, time.Unix(s.LastActive, 0).Format("01-02 15:04"), marker),
			Preview:     strings.TrimSpace(s.ID + " · " + s.Workspace + " · " + s.Application),
		})
	}
	filterPicker(&m.completion, m.searchCatalog)
	return m, nil
}

func (m *TuiModel) handleSearchPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A pending switch owns its selected target; only cancellation is accepted.
	if m.completion.loading && len(m.searchCatalog) > 0 && msg.Type != tea.KeyEsc && msg.Type != tea.KeyCtrlR && msg.Type != tea.KeyCtrlG {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlR, tea.KeyCtrlG:
		m.completion.Close()
	case tea.KeyUp:
		m.completion.Prev()
	case tea.KeyDown:
		m.completion.Next()
	case tea.KeyEnter:
		item := m.completion.SelectedItem()
		if item == nil || m.completion.loading {
			return m, nil
		}
		if m.completion.Kind() == CompletionHistory {
			m.input.saveUndo()
			m.input.lines = strings.Split(item.InsertText, "\n")
			m.input.cursorY = len(m.input.lines) - 1
			m.input.cursorX = utf8.RuneCountInString(m.input.lines[m.input.cursorY])
			m.input.histIdx = -1
			m.completion.Close()
			m.viewport.Resize(m.width, max(1, m.height-m.inputHeight()-m.statusBarHeight()))
			return m, nil
		}
		if item.InsertText == m.session.SessionID() {
			m.completion.Close()
			return m, nil
		}
		m.completion.loading = true
		id, target, app := m.pickerID, item.InsertText, m.app
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			s, err := app.SwitchSession(ctx, target)
			return sessionPickerSwitchMsg{id, s, err}
		}
	case tea.KeyRunes:
		m.completion.query += strings.ReplaceAll(sanitizeInput(string(msg.Runes)), "\n", " ")
		filterPicker(&m.completion, m.searchCatalog)
	case tea.KeyBackspace, tea.KeyCtrlH:
		runes := []rune(m.completion.query)
		if len(runes) > 0 {
			m.completion.query = string(runes[:len(runes)-1])
		}
		filterPicker(&m.completion, m.searchCatalog)
	case tea.KeyCtrlU:
		m.completion.query = ""
		filterPicker(&m.completion, m.searchCatalog)
	}
	return m, nil
}

func (m *TuiModel) receiveSessionSwitch(msg sessionPickerSwitchMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.pickerID || m.completion.Kind() != CompletionSession {
		return m, nil
	}
	m.completion.loading = false
	if msg.err != nil {
		m.copyNotice = "Could not open session: " + msg.err.Error()
		return m, nil
	}
	as, ok := msg.session.(*runtime.AgentSession)
	if !ok || as == nil {
		m.copyNotice = "Could not open session: incompatible session"
		return m, nil
	}
	m.useSession(as)
	m.completion.Close()
	return m, nil
}

func (m *TuiModel) useSession(as *runtime.AgentSession) {
	confirmEnabled := m.session.ConfirmEnabled()
	m.streamID++ // events from the previous session cannot enter the new transcript
	m.streaming, m.agentBusy = false, false
	m.streamBuf, m.appliedStreamLen = "", 0
	m.runStarted = time.Time{}
	m.session = as
	m.wireConfirmationCallback()
	m.session.SetConfirmEnabled(confirmEnabled)
	m.provider, m.modelID = as.ModelInfo()
	m.toolFocus = false
	m.restoreHistory()
}
