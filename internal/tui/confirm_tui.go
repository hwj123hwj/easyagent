package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmationState manages the yes/no confirmation dialog overlay.
// When active, all key input is routed to the dialog instead of the input editor.
type ConfirmationState struct {
	active      bool
	description string
	toolCallID  string
	selected    int // 0=Yes, 1=No
	toolName    string
	resultChan  chan ConfirmationResultMsg
}

// NewConfirmationState creates a new confirmation state.
func NewConfirmationState() *ConfirmationState {
	return &ConfirmationState{
		selected:   0,
		resultChan: make(chan ConfirmationResultMsg, 1),
	}
}

// IsActive returns true if the confirmation dialog is showing.
func (cs *ConfirmationState) IsActive() bool {
	return cs.active
}

// Show activates the confirmation dialog.
func (cs *ConfirmationState) Show(toolCallID, toolName, description string) {
	cs.active = true
	cs.description = terminalText(description)
	cs.toolCallID = toolCallID
	cs.toolName = toolName
	cs.selected = 0 // default to Yes
}

// Hide deactivates the dialog.
func (cs *ConfirmationState) Hide() {
	cs.active = false
}

// HandleKey processes a key event when the dialog is visible.
// Returns: approved (the decision if resolved), consumed (whether the key was handled),
// resolved (whether the dialog was dismissed and a decision was made).
func (cs *ConfirmationState) HandleKey(msg tea.KeyMsg) (approved bool, consumed bool, resolved bool) {
	if !cs.active {
		return false, false, false
	}

	if msg.Type == tea.KeyEnter {
		return cs.selected == 0, true, true
	}
	action := DefaultKeyBindings.ResolveConfirmation(msg)

	switch action {
	case ActionSelectYes:
		return true, true, true
	case ActionSelectNo:
		return false, true, true
	case ActionClosePopup:
		return false, true, true // Esc = deny
	case ActionCursorLeft:
		cs.selected = 0
		return false, true, false
	case ActionCursorRight:
		cs.selected = 1
		return false, true, false
	default:
		return false, false, false
	}
}

// Selected returns the current selection (0=Yes, 1=No).
func (cs *ConfirmationState) Selected() int {
	return cs.selected
}

// Description returns the confirmation description.
func (cs *ConfirmationState) Description() string {
	return cs.description
}

// ToolCallID returns the tool call ID being confirmed.
func (cs *ConfirmationState) ToolCallID() string {
	return cs.toolCallID
}

// Render produces the confirmation dialog string.
func (cs *ConfirmationState) Render(width int) string {
	if !cs.active {
		return ""
	}
	return RenderConfirmationPopup(cs.description, cs.selected, width)
}

// wireConfirmationCallback sets up the agent's confirmation function to use
// the TUI dialog instead of auto-approving.
func (m *TuiModel) wireConfirmationCallback() {
	m.session.SetConfirmFunc(m.handleConfirmation)
}

// ConfirmationResultMsg carries the user's decision from the dialog.
type ConfirmationResultMsg struct {
	ToolCallID string
	Approved   bool
}

// resolveConfirmation is called when the user presses Y, N, or Esc in the dialog.
func (m *TuiModel) resolveConfirmation(approved bool) {
	if !m.confirmation.IsActive() {
		return
	}

	toolCallID := m.confirmation.ToolCallID()
	m.confirmation.Hide()

	// Send result to the waiting agent goroutine
	select {
	case m.confirmation.resultChan <- ConfirmationResultMsg{
		ToolCallID: toolCallID,
		Approved:   approved,
	}:
	default:
	}
}

// renderConfirmationOverlay renders the confirmation dialog as an overlay
// on top of the normal TUI view.
func (m *TuiModel) renderConfirmationOverlay() string {
	if !m.confirmation.IsActive() {
		return ""
	}
	dialog := m.confirmation.Render(m.width)

	// Center the dialog vertically (simple approach: add blank lines above)
	overlayLines := strings.Split(dialog, "\n")
	dialogHeight := len(overlayLines)

	blankLines := (m.height - dialogHeight) / 2
	if blankLines < 0 {
		blankLines = 0
	}

	result := strings.Repeat("\n", blankLines)
	result += dialog
	return result
}

// ConfirmationMsg is a message sent when a confirmation is requested.
type confirmationCancelledMsg struct{ result chan ConfirmationResultMsg }

type ConfirmationDialogMsg struct {
	Result      chan ConfirmationResultMsg
	ToolCallID  string
	ToolName    string
	Description string
}

// String returns a readable representation of the confirmation.
func (m ConfirmationDialogMsg) String() string {
	return fmt.Sprintf("Confirm: %s", m.Description)
}
