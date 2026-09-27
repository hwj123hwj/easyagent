package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// InputModel is a multi-line text input editor.
// Supports cursor movement, word deletion, undo, and history.
type InputModel struct {
	lines       []string
	cursorX     int
	cursorY     int
	undoStack   []InputSnapshot
	history     []string
	histIdx     int
	draftLines  []string // saved draft when entering history mode
	draftCursorX int
	draftCursorY int
	prompt      string
	width       int // terminal width for soft-wrap; 0 = unknown (no wrap)
	theme       *Theme
}

// InputSnapshot captures input state for undo.
type InputSnapshot struct {
	lines   []string
	cursorX int
	cursorY int
}

// NewInputModel creates a new input editor.
func NewInputModel() InputModel {
	return InputModel{
		lines:     []string{""},
		cursorX:   0,
		cursorY:   0,
		undoStack: nil,
		history:   nil,
		histIdx:   -1,
		prompt:    "›",
		theme:     DefaultTheme(),
	}
}

// Text returns the full input text as a single string.
func (im *InputModel) Text() string {
	return strings.Join(im.lines, "\n")
}

// SetWidth records the terminal width used for soft-wrapping the input.
// 首行要给 prompt "› " 让出 2 格，续行本身就有 2 格缩进。
func (im *InputModel) SetWidth(termWidth int) {
	im.width = termWidth
}

// wrapWidth returns the per-line visible width available for soft-wrap.
// 宽度未知或过小时返回 0（不换行），避免把输入拆成碎片。
func (im *InputModel) wrapWidth(firstLine bool) int {
	if im.width <= 4 {
		return 0
	}
	if firstLine {
		return im.width - 2
	}
	return im.width - 2
}

// IsEmpty returns true if there's no text.
func (im *InputModel) IsEmpty() bool {
	return len(im.lines) == 1 && im.lines[0] == ""
}

// Reset clears the input.
func (im *InputModel) Reset() {
	im.saveUndo()
	im.lines = []string{""}
	im.cursorX = 0
	im.cursorY = 0
}

// HandleKey processes a key event.
func (im *InputModel) HandleKey(msg tea.KeyMsg) {
	switch msg.Type {

	case tea.KeyBackspace:
		im.backspace()

	case tea.KeyDelete:
		im.deleteForward()

	case tea.KeyLeft:
		im.cursorLeft()

	case tea.KeyRight:
		im.cursorRight()

	case tea.KeyUp:
		if im.cursorY == 0 {
			im.navigateHistory(-1)
		} else {
			im.cursorUp()
		}

	case tea.KeyDown:
		if im.cursorY == len(im.lines)-1 {
			im.navigateHistory(1)
		} else {
			im.cursorDown()
		}

	case tea.KeyHome, tea.KeyCtrlA:
		im.cursorX = 0

	case tea.KeyEnd, tea.KeyCtrlE:
		im.cursorX = utf8.RuneCountInString(im.lines[im.cursorY])

	case tea.KeyCtrlK:
		im.killToEnd()

	case tea.KeyCtrlU:
		im.killToStart()

	case tea.KeyCtrlW:
		im.deleteWordBackward()

	case tea.KeyCtrlZ:
		im.undo()

	case tea.KeyCtrlJ:
		im.newLine()

	default:
		// Regular character input — only accept printable runes.
		// CRITICAL: Filter out ANSI escape sequences (\x1b, control chars).
		// Some terminals send arrow keys as raw escape codes (\x1b[A) that
		// Bubble Tea may pass as KeyRunes. If we insert them, they leak
		// into the message and corrupt API URLs.
		if msg.Type == tea.KeyRunes {
			cleaned := sanitizeRunes(msg.Runes)
			if len(cleaned) > 0 {
				im.insertString(string(cleaned))
			}
		}
	}
}

// View renders the input area with a styled prompt and cursor indicator.
func (im *InputModel) View() string {
	var buf strings.Builder

	for i, line := range im.lines {
		// Bugfix: 长行按可视宽度软换行（CJK 感知），之前渲染成单行被终端截断。
		segments := wrapVisual(line, im.wrapWidth(i == 0))
		for j, seg := range segments {
			if i == 0 && j == 0 {
				// First line: styled prompt
				buf.WriteString(im.theme.InputPrompt.Render(im.prompt))
				buf.WriteByte(' ')
			} else {
				// Continuation lines: align with prompt
				buf.WriteString("  ")
			}

			// Render the segment carrying the cursor with a visible cursor
			if i == im.cursorY && segmentHoldsCursor(segments, j, im.cursorX) {
				buf.WriteString(im.renderSegmentWithCursor(seg, segmentCursorOffset(segments, j, im.cursorX)))
			} else {
				buf.WriteString(seg)
			}
			buf.WriteByte('\n')
		}
	}

	return strings.TrimSuffix(buf.String(), "\n")
}

// segmentCursorBounds 返回第 segIdx 段在逻辑行内的 [start, end) 逻辑列区间。
// 每个非末段末尾的换行占一个逻辑位置（对应真实换行/折行边界）。
func segmentCursorBounds(segments []string, segIdx int) (start, end int) {
	for i, seg := range segments {
		segLen := len([]rune(seg))
		if i == segIdx {
			return start, start + segLen
		}
		start += segLen + 1 // +1 for the break
	}
	return start, start
}

// segmentHoldsCursor 判断逻辑列 cursorX 是否落在第 segIdx 段（含段尾断行位）。
func segmentHoldsCursor(segments []string, segIdx, cursorX int) bool {
	start, end := segmentCursorBounds(segments, segIdx)
	// 段尾断行位（cursorX == end 且不是最后一行的段尾）也归该段，光标画在段尾
	if segIdx < len(segments)-1 {
		return cursorX >= start && cursorX <= end
	}
	return cursorX >= start
}

// segmentCursorOffset 把逻辑列 cursorX 换算成 seg 段内的本地列。
func segmentCursorOffset(segments []string, segIdx, cursorX int) int {
	start, end := segmentCursorBounds(segments, segIdx)
	if cursorX < start {
		return 0
	}
	if cursorX > end {
		return end - start
	}
	return cursorX - start
}

// renderSegmentWithCursor 在软换行段内渲染光标，localX 为段内本地列。
func (im *InputModel) renderSegmentWithCursor(seg string, localX int) string {
	runes := []rune(seg)
	var buf strings.Builder
	for i, r := range runes {
		if i == localX {
			buf.WriteString(im.cursorHighlight(string(r)))
		} else {
			buf.WriteRune(r)
		}
	}
	if localX >= len(runes) {
		buf.WriteString(im.theme.InputPrompt.Render("│"))
	}
	return buf.String()
}

// cursorHighlight renders a character with a highlighted background.
func (im *InputModel) cursorHighlight(ch string) string {
	// Use lipgloss reverse video — safer than raw escape codes.
	return im.theme.InputPrompt.Reverse(true).Render(ch)
}

// sanitizeRunes filters out non-printable control characters from a rune slice.
// This prevents ANSI escape sequences (ESC = \x1b = 0x1B) and other control
// characters from leaking into the input buffer.
func sanitizeRunes(runes []rune) []rune {
	var result []rune
	for _, r := range runes {
		// Allow printable characters (including Unicode) and newline/tab.
		// Block ESC (0x1B), backspace (0x08), delete (0x7F), and other control chars.
		if r >= 0x20 && r != 0x7F {
			result = append(result, r)
		}
	}
	return result
}

// sanitizeInput strips control characters from a string (for final safety check).
func sanitizeInput(s string) string {
	var result []rune
	for _, r := range s {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7F) {
			result = append(result, r)
		}
	}
	return string(result)
}

// ── Cursor movement ───────────────────────────────────────────────────────────

func (im *InputModel) cursorLeft() {
	if im.cursorX > 0 {
		im.cursorX--
	} else if im.cursorY > 0 {
		im.cursorY--
		im.cursorX = utf8.RuneCountInString(im.lines[im.cursorY])
	}
}

func (im *InputModel) cursorRight() {
	lineLen := utf8.RuneCountInString(im.lines[im.cursorY])
	if im.cursorX < lineLen {
		im.cursorX++
	} else if im.cursorY < len(im.lines)-1 {
		im.cursorY++
		im.cursorX = 0
	}
}

func (im *InputModel) cursorUp() {
	if im.cursorY > 0 {
		im.cursorY--
		lineLen := utf8.RuneCountInString(im.lines[im.cursorY])
		if im.cursorX > lineLen {
			im.cursorX = lineLen
		}
	}
}

func (im *InputModel) cursorDown() {
	if im.cursorY < len(im.lines)-1 {
		im.cursorY++
		lineLen := utf8.RuneCountInString(im.lines[im.cursorY])
		if im.cursorX > lineLen {
			im.cursorX = lineLen
		}
	}
}

// ── Text editing ──────────────────────────────────────────────────────────────

func (im *InputModel) insertString(s string) {
	im.saveUndo()
	line := im.lines[im.cursorY]
	runes := []rune(line)
	insertAt := im.cursorX
	newRunes := append(runes[:insertAt], append([]rune(s), runes[insertAt:]...)...)
	im.lines[im.cursorY] = string(newRunes)
	im.cursorX += utf8.RuneCountInString(s)
}

func (im *InputModel) backspace() {
	if im.cursorX == 0 && im.cursorY == 0 {
		return
	}
	im.saveUndo()
	if im.cursorX == 0 {
		// Merge with previous line
		prevLine := im.lines[im.cursorY-1]
		im.cursorX = utf8.RuneCountInString(prevLine)
		im.lines[im.cursorY-1] = prevLine + im.lines[im.cursorY]
		im.lines = append(im.lines[:im.cursorY], im.lines[im.cursorY+1:]...)
		im.cursorY--
	} else {
		runes := []rune(im.lines[im.cursorY])
		im.lines[im.cursorY] = string(append(runes[:im.cursorX-1], runes[im.cursorX:]...))
		im.cursorX--
	}
}

func (im *InputModel) deleteForward() {
	lineLen := utf8.RuneCountInString(im.lines[im.cursorY])
	if im.cursorX < lineLen {
		im.saveUndo()
		runes := []rune(im.lines[im.cursorY])
		im.lines[im.cursorY] = string(append(runes[:im.cursorX], runes[im.cursorX+1:]...))
	} else if im.cursorY < len(im.lines)-1 {
		im.saveUndo()
		im.lines[im.cursorY] = im.lines[im.cursorY] + im.lines[im.cursorY+1]
		im.lines = append(im.lines[:im.cursorY+1], im.lines[im.cursorY+2:]...)
	}
}

func (im *InputModel) killToEnd() {
	if im.cursorX < utf8.RuneCountInString(im.lines[im.cursorY]) {
		im.saveUndo()
		runes := []rune(im.lines[im.cursorY])
		im.lines[im.cursorY] = string(runes[:im.cursorX])
	}
}

func (im *InputModel) killToStart() {
	if im.cursorX > 0 {
		im.saveUndo()
		runes := []rune(im.lines[im.cursorY])
		im.lines[im.cursorY] = string(runes[im.cursorX:])
		im.cursorX = 0
	}
}

func (im *InputModel) deleteWordBackward() {
	if im.cursorX == 0 {
		im.backspace()
		return
	}
	im.saveUndo()
	runes := []rune(im.lines[im.cursorY])
	pos := im.cursorX - 1
	// Skip trailing spaces
	for pos > 0 && runes[pos] == ' ' {
		pos--
	}
	// Skip word
	for pos > 0 && runes[pos-1] != ' ' {
		pos--
	}
	im.lines[im.cursorY] = string(append(runes[:pos], runes[im.cursorX:]...))
	im.cursorX = pos
}

func (im *InputModel) newLine() {
	im.saveUndo()
	line := im.lines[im.cursorY]
	runes := []rune(line)
	beforeCursor := string(runes[:im.cursorX])
	afterCursor := string(runes[im.cursorX:])
	im.lines[im.cursorY] = beforeCursor
	// Insert new line
	newLines := make([]string, 0, len(im.lines)+1)
	newLines = append(newLines, im.lines[:im.cursorY+1]...)
	newLines = append(newLines, afterCursor)
	newLines = append(newLines, im.lines[im.cursorY+1:]...)
	im.lines = newLines
	im.cursorY++
	im.cursorX = 0
}

// ── History ───────────────────────────────────────────────────────────────────

func (im *InputModel) navigateHistory(dir int) {
	if len(im.history) == 0 {
		return
	}
	if im.histIdx < 0 {
		// First time navigating — save current input as draft
		im.draftLines = make([]string, len(im.lines))
		copy(im.draftLines, im.lines)
		im.draftCursorX = im.cursorX
		im.draftCursorY = im.cursorY
		im.histIdx = len(im.history)
	}
	im.histIdx += dir
	// Allow navigating past the end to restore draft
	if im.histIdx < 0 {
		im.histIdx = 0
	}
	if im.histIdx >= len(im.history) {
		// Past the end — restore draft
		im.histIdx = -1
		im.lines = make([]string, len(im.draftLines))
		copy(im.lines, im.draftLines)
		im.cursorX = im.draftCursorX
		im.cursorY = im.draftCursorY
		return
	}
	im.lines = []string{im.history[im.histIdx]}
	im.cursorX = utf8.RuneCountInString(im.lines[0])
	im.cursorY = 0
}

// AddHistory adds a submitted input to history.
func (im *InputModel) AddHistory(text string) {
	if text == "" {
		return
	}
	// Don't add consecutive duplicates
	if len(im.history) > 0 && im.history[len(im.history)-1] == text {
		return
	}
	im.history = append(im.history, text)
	im.histIdx = -1
}

// ── Undo ──────────────────────────────────────────────────────────────────────

func (im *InputModel) saveUndo() {
	snap := InputSnapshot{
		lines:   make([]string, len(im.lines)),
		cursorX: im.cursorX,
		cursorY: im.cursorY,
	}
	copy(snap.lines, im.lines)
	im.undoStack = append(im.undoStack, snap)
	// Limit undo stack to 50 entries
	if len(im.undoStack) > 50 {
		im.undoStack = im.undoStack[1:]
	}
}

func (im *InputModel) undo() {
	if len(im.undoStack) == 0 {
		return
	}
	snap := im.undoStack[len(im.undoStack)-1]
	im.undoStack = im.undoStack[:len(im.undoStack)-1]
	im.lines = snap.lines
	im.cursorX = snap.cursorX
	im.cursorY = snap.cursorY
}
