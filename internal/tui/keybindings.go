package tui

import tea "github.com/charmbracelet/bubbletea"

// KeyBinding maps a key combination to a named action.
type KeyBinding struct {
	KeyType tea.KeyType
	Alt     bool
	Action  string // human-readable action name
}

// KeyAction is an enumerated action that key bindings resolve to.
type KeyAction int

const (
	ActionNone KeyAction = iota
	ActionSubmit
	ActionNewline
	ActionCancel           // Ctrl+C (cancel agent or quit)
	ActionExit             // Ctrl+D
	ActionClearScreen      // Ctrl+L
	ActionToggleToolPanel  // Ctrl+O
	ActionOpenModelSelect  // Ctrl+P
	ActionSearchHistory    // Ctrl+R
	ActionUndo             // Ctrl+Z
	ActionAcceptCompletion // Tab
	ActionClosePopup       // Esc
	ActionPageUp           // PgUp
	ActionPageDown         // PgDn
	ActionScrollUp
	ActionScrollDown
	ActionHistoryPrev // Up (input)
	ActionHistoryNext // Down (input)
	ActionCursorLeft
	ActionCursorRight
	ActionCursorUp
	ActionCursorDown
	ActionDeleteWord
	ActionKillLine
	ActionKillToStart
	ActionHome
	ActionEnd
	ActionSelectYes // Y in confirmation
	ActionSelectNo  // N in confirmation
)

// KeyBindingTable maps key events to actions depending on context.
// Different contexts may interpret the same key differently.
type KeyBindingTable struct{}

// NewKeyBindingTable creates the default binding table.
func NewKeyBindingTable() *KeyBindingTable {
	return &KeyBindingTable{}
}

// ResolveInput maps a key event to an action in the INPUT context
// (when typing in the input box, no popup visible).
func (k *KeyBindingTable) ResolveInput(msg tea.KeyMsg) KeyAction {
	// Alt-modified keys
	if msg.Alt {
		switch msg.String() {
		case "alt+b":
			return ActionCursorLeft // word left (simplified)
		case "alt+f":
			return ActionCursorRight // word right (simplified)
		}
	}

	switch msg.Type {
	case tea.KeyEnter:
		return ActionSubmit
	case tea.KeyCtrlC:
		return ActionCancel
	case tea.KeyCtrlD:
		return ActionExit
	case tea.KeyCtrlL:
		return ActionClearScreen
	case tea.KeyCtrlO:
		return ActionToggleToolPanel
	case tea.KeyCtrlP:
		return ActionOpenModelSelect
	case tea.KeyCtrlR:
		return ActionSearchHistory
	case tea.KeyCtrlZ:
		return ActionUndo
	case tea.KeyCtrlJ:
		return ActionNewline
	case tea.KeyCtrlK:
		return ActionKillLine
	case tea.KeyCtrlU:
		return ActionKillToStart
	case tea.KeyCtrlW:
		return ActionDeleteWord
	case tea.KeyCtrlA:
		return ActionHome
	case tea.KeyCtrlE:
		return ActionEnd
	case tea.KeyLeft:
		return ActionCursorLeft
	case tea.KeyRight:
		return ActionCursorRight
	case tea.KeyUp:
		return ActionHistoryPrev
	case tea.KeyDown:
		return ActionHistoryNext
	case tea.KeyPgUp:
		return ActionPageUp
	case tea.KeyPgDown:
		return ActionPageDown
	case tea.KeyBackspace:
		// Let the input handle it
		return ActionNone
	case tea.KeyTab:
		return ActionAcceptCompletion
	case tea.KeyEsc:
		return ActionClosePopup
	default:
		return ActionNone
	}
}

// ResolveCompletion maps a key event to an action in the COMPLETION context
// (when the autocomplete popup is visible).
func (k *KeyBindingTable) ResolveCompletion(msg tea.KeyMsg) KeyAction {
	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		return ActionAcceptCompletion
	case tea.KeyDown:
		return ActionHistoryNext // next completion item
	case tea.KeyUp:
		return ActionHistoryPrev // prev completion item
	case tea.KeyEsc:
		return ActionClosePopup
	default:
		return ActionNone
	}
}

// ResolveConfirmation maps a key event to an action in the CONFIRMATION context
// (when the yes/no dialog is visible).
func (k *KeyBindingTable) ResolveConfirmation(msg tea.KeyMsg) KeyAction {
	switch msg.Type {
	case tea.KeyLeft:
		return ActionCursorLeft
	case tea.KeyRight:
		return ActionCursorRight
	case tea.KeyEnter:
		return ActionSelectYes // accept with current selection (default: Yes)
	case tea.KeyEsc:
		return ActionClosePopup
	case tea.KeyCtrlC:
		return ActionSelectNo
	default:
		// Check for 'y' or 'n' keypress
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			switch msg.Runes[0] {
			case 'y', 'Y':
				return ActionSelectYes
			case 'n', 'N':
				return ActionSelectNo
			}
		}
		return ActionNone
	}
}

// DefaultKeyBindings is the shared singleton.
var DefaultKeyBindings = NewKeyBindingTable()

// KeyHelpText returns a summary of key bindings for display.
func KeyHelpText() []string {
	return []string{
		"Enter      Send message",
		"Ctrl+J     New line (multi-line input)",
		"Ctrl+C     Cancel agent / Quit",
		"Ctrl+D     Exit",
		"Ctrl+L     Redraw screen (keep conversation)",
		"Ctrl+O     Expand/collapse latest tool group",
		"Ctrl+P     Select model",
		"Ctrl+R     Search history",
		"Ctrl+Z     Undo input",
		"Ctrl+W     Delete word",
		"Ctrl+K     Kill to end of line",
		"Ctrl+A/E   Start/End of line",
		"↑/↓        History navigation",
		"PgUp/PgDn  Scroll messages",
		"F2         Copy latest assistant reply",
		"F3         Copy entire conversation (including tool results)",
		"Click      Expand/collapse a tool group or command header",
		"Drag       Select text and release to copy automatically",
		"Esc        Clear selection and resume live view",
		"Tab        Accept autocomplete",
		"Esc        Close popup",
		"/          Slash commands",
		"@          File path completion",
	}
}
