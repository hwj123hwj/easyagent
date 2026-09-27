package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

func TestNewConfirmationMode(t *testing.T) {
	confirmSession := &runtime.AgentSession{}
	New(confirmSession, slashcmd.NewRegistry(), false)
	if !confirmSession.ConfirmEnabled() {
		t.Fatal("confirmation should be enabled by default")
	}

	fullAccessSession := &runtime.AgentSession{}
	New(fullAccessSession, slashcmd.NewRegistry(), true)
	if fullAccessSession.ConfirmEnabled() {
		t.Fatal("full-access mode should disable confirmation")
	}
}

func TestInputModel_InsertAndText(t *testing.T) {
	im := NewInputModel()
	im.insertString("hello")
	im.insertString(" world")

	got := im.Text()
	want := "hello world"
	if got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
}

func TestInputModel_Backspace(t *testing.T) {
	im := NewInputModel()
	im.insertString("hello")
	im.backspace()

	got := im.Text()
	want := "hell"
	if got != want {
		t.Errorf("after backspace: Text() = %q, want %q", got, want)
	}
}

func TestInputModel_NewLine(t *testing.T) {
	im := NewInputModel()
	im.insertString("line1")
	im.newLine()
	im.insertString("line2")

	got := im.Text()
	want := "line1\nline2"
	if got != want {
		t.Errorf("after newLine: Text() = %q, want %q", got, want)
	}

	if im.cursorY != 1 {
		t.Errorf("cursorY = %d, want 1", im.cursorY)
	}
}

func TestInputModel_Undo(t *testing.T) {
	im := NewInputModel()
	im.insertString("hello")
	im.saveUndo()
	im.insertString(" world")
	im.undo()

	got := im.Text()
	want := "hello"
	if got != want {
		t.Errorf("after undo: Text() = %q, want %q", got, want)
	}
}

func TestInputModel_DeleteWordBackward(t *testing.T) {
	im := NewInputModel()
	im.insertString("hello world")
	// cursor is at end (11)
	im.deleteWordBackward()

	got := im.Text()
	want := "hello "
	if got != want {
		t.Errorf("after deleteWordBackward: Text() = %q, want %q", got, want)
	}
}

func TestInputModel_KillToEnd(t *testing.T) {
	im := NewInputModel()
	im.insertString("hello world")
	// Move cursor to position 5
	im.cursorX = 5
	im.killToEnd()

	got := im.Text()
	want := "hello"
	if got != want {
		t.Errorf("after killToEnd: Text() = %q, want %q", got, want)
	}
}

func TestInputModel_History(t *testing.T) {
	im := NewInputModel()
	im.AddHistory("first")
	im.AddHistory("second")

	// Navigate up (older)
	im.navigateHistory(-1)
	if im.Text() != "second" {
		t.Errorf("history[0] = %q, want 'second'", im.Text())
	}

	// Navigate up again (oldest)
	im.navigateHistory(-1)
	if im.Text() != "first" {
		t.Errorf("history[1] = %q, want 'first'", im.Text())
	}
}

func TestInputModel_IsEmpty(t *testing.T) {
	im := NewInputModel()
	if !im.IsEmpty() {
		t.Error("new input should be empty")
	}
	im.insertString("x")
	if im.IsEmpty() {
		t.Error("input with text should not be empty")
	}
}

func TestInputModel_Reset(t *testing.T) {
	im := NewInputModel()
	im.insertString("hello")
	im.Reset()
	if !im.IsEmpty() {
		t.Error("after Reset(), input should be empty")
	}
}

func TestMessageViewport_SetMessages(t *testing.T) {
	vp := NewMessageViewport(80, 10)
	vp.SetMessages([]ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})

	if len(vp.lines) == 0 {
		t.Error("viewport should have rendered lines")
	}
}

func TestMessageViewport_Streaming(t *testing.T) {
	vp := NewMessageViewport(80, 10)
	vp.SetStreaming("partial response...")

	if len(vp.lines) == 0 {
		t.Error("viewport should have rendered streaming text")
	}
}

func TestStatusBarRender(t *testing.T) {
	sb := NewStatusBar()
	result := sb.Render(80, "ready", 0, "openai", "gpt-4o", "/home/user/easyagent", false, 0, 0)
	if result == "" {
		t.Error("StatusBar.Render() should not be empty")
	}
}

func TestTuiModel_Init(t *testing.T) {
	m := &TuiModel{
		input:     NewInputModel(),
		viewport:  NewMessageViewport(80, 20),
		statusBar: *NewStatusBar(),
		messages:  []ChatMessage{},
		theme:     DefaultTheme(),
	}
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

func TestInputModel_HandleKeyRunes(t *testing.T) {
	im := NewInputModel()
	im.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})

	if im.Text() != "abc" {
		t.Errorf("after typing 'abc': Text() = %q, want 'abc'", im.Text())
	}
}

func TestToolPanelRender(t *testing.T) {
	tp := NewToolPanel(ToolCallInfo{
		Name:      "bash",
		Args:      "go test",
		Result:    "ok  pkg 0.1s",
		Collapsed: true,
	}, 60)
	lines := tp.Render()
	if len(lines) == 0 {
		t.Error("ToolPanel.Render() should produce output")
	}
}

func TestRenderDiff(t *testing.T) {
	theme := DefaultTheme()
	diff := "+added line\n-removed line\n context"
	lines := RenderDiff(diff, theme)
	if len(lines) != 3 {
		t.Errorf("RenderDiff should return 3 lines, got %d", len(lines))
	}
}

func TestMarkdownRender(t *testing.T) {
	mr := NewMarkdownRenderer(80)
	result := mr.Render("**bold text**")
	if result == "" {
		t.Error("Markdown render should not be empty")
	}
}

func TestCompletionSlash(t *testing.T) {
	cm := NewCompletionState()
	// Simulate typing "/he"
	// We can't use a real registry here, so just test the state machine
	cm.kind = CompletionSlash
	cm.visible = true
	cm.items = []CompletionItem{
		{Label: "/help", Description: "Show help"},
		{Label: "/history", Description: "View history"},
	}

	if !cm.IsActive() {
		t.Error("completion should be active")
	}
	if len(cm.Items()) != 2 {
		t.Errorf("expected 2 items, got %d", len(cm.Items()))
	}

	// Test navigation
	cm.Next()
	if cm.SelectedIndex() != 1 {
		t.Errorf("after Next, selected = %d, want 1", cm.SelectedIndex())
	}
	cm.Prev()
	if cm.SelectedIndex() != 0 {
		t.Errorf("after Prev, selected = %d, want 0", cm.SelectedIndex())
	}

	// Test close
	cm.Close()
	if cm.IsActive() {
		t.Error("completion should be inactive after Close()")
	}
}

func TestConfirmationState(t *testing.T) {
	cs := NewConfirmationState()
	if cs.IsActive() {
		t.Error("new confirmation should be inactive")
	}

	cs.Show("tool-1", "bash", "Run: rm -rf /tmp/cache")
	if !cs.IsActive() {
		t.Error("confirmation should be active after Show()")
	}

	if cs.Selected() != 0 {
		t.Errorf("default selected = %d, want 0 (Yes)", cs.Selected())
	}

	cs.Hide()
	if cs.IsActive() {
		t.Error("confirmation should be inactive after Hide()")
	}
}

func TestKeyBindings(t *testing.T) {
	kb := NewKeyBindingTable()

	// Test input context
	action := kb.ResolveInput(tea.KeyMsg{Type: tea.KeyEnter})
	if action != ActionSubmit {
		t.Errorf("Enter should resolve to ActionSubmit, got %v", action)
	}

	action = kb.ResolveInput(tea.KeyMsg{Type: tea.KeyCtrlL})
	if action != ActionClearScreen {
		t.Errorf("Ctrl+L should resolve to ActionClearScreen, got %v", action)
	}

	// Test completion context
	action = kb.ResolveCompletion(tea.KeyMsg{Type: tea.KeyTab})
	if action != ActionAcceptCompletion {
		t.Errorf("Tab in completion should resolve to ActionAcceptCompletion, got %v", action)
	}

	// Test confirmation context
	action = kb.ResolveConfirmation(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if action != ActionSelectYes {
		t.Errorf("'y' in confirmation should resolve to ActionSelectYes, got %v", action)
	}
}

func TestToolPanelToggle(t *testing.T) {
	tp := NewToolPanel(ToolCallInfo{
		Name:      "edit",
		Args:      "test.go",
		Result:    "ok",
		Collapsed: true,
	}, 60)
	if !tp.info.Collapsed {
		t.Error("should start collapsed")
	}
	tp.ToggleCollapsed()
	if tp.info.Collapsed {
		t.Error("should be expanded after toggle")
	}
}

func TestMessageViewport_UserLongLineWraps(t *testing.T) {
	// Bugfix 回归：用户消息长行必须按可视宽度软换行，不能被终端截断。
	vp := NewMessageViewport(40, 10)
	long := "这是一段特别长的中文消息用来验证软换行行为没有问题超过可视宽度之后应当折行"
	vp.SetMessages([]ChatMessage{{Role: "user", Content: long}})

	// 40 列宽、减去 2 格缩进 ≈ 每行 19 个 CJK 字符（每字 2 格）
	wrapped := 0
	for _, line := range vp.lines {
		if strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			wrapped++
			if w := lipgloss.Width(line); w > 40 {
				t.Fatalf("line width %d exceeds terminal width 40: %q", w, line)
			}
		}
	}
	if wrapped < 2 {
		t.Fatalf("long user message should wrap into multiple lines, got %d", wrapped)
	}

	// 内容不能丢：只拼内容行（跳过 header "You 00:00"），去掉缩进后应等于原文
	var joined strings.Builder
	for _, line := range vp.lines[1:] {
		joined.WriteString(strings.TrimPrefix(line, "  "))
	}
	if strings.TrimSpace(joined.String()) != long {
		t.Errorf("wrapped content lost characters:\n got %q\nwant %q", joined.String(), long)
	}
}

func TestMessageViewport_UserShortLineUnchanged(t *testing.T) {
	vp := NewMessageViewport(80, 10)
	vp.SetMessages([]ChatMessage{{Role: "user", Content: "hello"}})

	if len(vp.lines) != 3 { // header + content + blank separator
		t.Fatalf("expected 3 lines, got %d", len(vp.lines))
	}
	if vp.lines[1] != "  hello" {
		t.Errorf("short line should stay untouched, got %q", vp.lines[1])
	}
}

func TestInputModel_SoftWrapLongLine(t *testing.T) {
	// Bugfix 回归：输入框长行必须软换行，不能渲染成单行被终端截断。
	im := NewInputModel()
	im.SetWidth(40)
	long := "这是一段特别长的中文输入用来验证输入框软换行行为超过宽度之后应当折行显示"
	for _, r := range long {
		im.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	view := im.View()
	for _, line := range strings.Split(view, "\n") {
		// 每个可视行去掉 ANSI 后不得超过终端宽度
		if w := lipgloss.Width(line); w > 40 {
			t.Fatalf("input visual line width %d exceeds 40: %q", w, line)
		}
	}
	visualLines := strings.Count(view, "\n") + 1
	if visualLines < 2 {
		t.Fatalf("long input should wrap into multiple visual lines, got %d", visualLines)
	}
	if im.Text() != long {
		t.Errorf("soft-wrap must not change logical text")
	}
}

func TestInputModel_InputHeightMatchesView(t *testing.T) {
	im := NewInputModel()
	im.SetWidth(40)
	im.lines = []string{"短行", "这是第二个逻辑行内容比较长同样会触发软换行的行为需要验证高度计算正确"}
	im.cursorY = 1
	im.cursorX = 5

	m := &TuiModel{input: im, width: 40}
	viewLines := strings.Count(im.View(), "\n") + 1
	if m.inputHeight() != viewLines {
		t.Errorf("inputHeight=%d but View renders %d lines", m.inputHeight(), viewLines)
	}
}

func TestView_InputAboveFooter(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.Update(tea.WindowSizeMsg{Width: 99, Height: 37})
	m.input.insertString("你好")
	view := m.View()
	if !(strings.Index(view, "你好") < strings.Index(view, "Enter: send") && strings.Index(view, "Enter: send") < strings.Index(view, "ready")) {
		t.Fatal("expected input, help, status in that order")
	}
	if got := lipgloss.Height(view); got != 37 {
		t.Fatalf("height=%d", got)
	}
}

func TestInputCursorPosition(t *testing.T) {
	im := NewInputModel()
	im.SetWidth(10) // 7 cells of content plus prompt and caret
	im.insertString("abcdefg中文")
	for _, tc := range []struct{ x, col, row int }{{0, 2, 0}, {6, 8, 0}, {7, 2, 1}, {8, 4, 1}, {9, 6, 1}} {
		im.cursorX = tc.x
		col, row := im.CursorPosition()
		if col != tc.col || row != tc.row {
			t.Errorf("x=%d got (%d,%d), want (%d,%d)", tc.x, col, row, tc.col, tc.row)
		}
	}
	im.newLine()
	im.cursorY = 0
	im.cursorX = 2
	col, row := im.CursorPosition()
	if col != 4 || row != 0 {
		t.Fatalf("editing earlier line: (%d,%d)", col, row)
	}
}

func TestPasteMultilineAndUndo(t *testing.T) {
	im := NewInputModel()
	im.insertString("beforeafter")
	im.cursorX = 6
	im.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("中文\r\nline 2\r\n")})
	if got := im.Text(); got != "before中文\nline 2\nafter" {
		t.Fatalf("paste=%q", got)
	}
	if im.cursorY != 2 || im.cursorX != 0 {
		t.Fatal("wrong paste caret")
	}
	im.undo()
	if im.Text() != "beforeafter" || im.cursorX != 6 {
		t.Fatal("paste must undo in one step")
	}
	im.AddHistory("one\ntwo")
	im.navigateHistory(-1)
	if len(im.lines) != 2 || im.cursorY != 1 || im.cursorX != 3 {
		t.Fatal("multiline history was flattened")
	}
}

func TestInputWrapPreservesSpaces(t *testing.T) {
	im := NewInputModel()
	im.SetWidth(12)
	text := "abc   def     中文  xyz"
	im.insertString(text)
	if got := strings.Join(im.segments(text), ""); got != text {
		t.Fatalf("lost whitespace: %q", got)
	}
}

func TestViewFitsWithLongDraft(t *testing.T) {
	for _, height := range []int{1, 4, 5, 8, 24} {
		m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
		m.Update(tea.WindowSizeMsg{Width: 30, Height: height})
		m.input.insertString(strings.Repeat("中文 very long line\n", 40) + "END")
		for _, y := range []int{0, 20, 40} {
			m.input.cursorY = y
			m.input.cursorX = 0
			view := m.View()
			if lipgloss.Height(view) > height {
				t.Fatalf("window=%d rendered=%d", height, lipgloss.Height(view))
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > 30 {
					t.Fatal("line overflow")
				}
			}
		}
	}
}

func TestStreamingUsesSingleTimer(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	_, first := m.Update(StreamTextMsg{Delta: "a"})
	_, second := m.Update(StreamTextMsg{Delta: "b"})
	if first == nil || second != nil {
		t.Fatal("stream deltas must share one timer")
	}
	_, next := m.Update(TickMsg{})
	if next == nil || m.viewport.streaming != "ab" {
		t.Fatal("tick should flush and schedule once")
	}
}

// TestViewport_IncrementalRenderMatchesFull：增量渲染结果必须与全量重建逐行一致。
// 同时验证 SetMessages 缩短(/new 场景)后能正确全量重建。
func TestViewport_IncrementalRenderMatchesFull(t *testing.T) {
	mk := func() *MessageViewport {
		v := NewMessageViewport(80, 20)
		return &v
	}

	// 模拟一轮对话的追加序列
	var msgs []ChatMessage
	v := mk()
	seq := []ChatMessage{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "# 标题\n\n**加粗** 回复"},
		{Role: "user", Content: "继续"},
	}
	for _, m := range seq {
		msgs = append(msgs, m)
		v.SetMessages(msgs)
	}

	// 全量重建的对照 viewport
	full := mk()
	full.SetMessages(msgs)

	if got, want := strings.Join(v.lines, "\n"), strings.Join(full.lines, "\n"); got != want {
		t.Fatalf("incremental render diverged from full rebuild")
	}

	// 工具事件改最后一条消息后仍一致
	msgs[len(msgs)-1].Tools = append(msgs[len(msgs)-1].Tools, ToolCallInfo{ID: "t1", Name: "bash", Collapsed: true})
	v.SetMessages(msgs)
	full.SetMessages(msgs)
	if got, want := strings.Join(v.lines, "\n"), strings.Join(full.lines, "\n"); got != want {
		t.Fatalf("render diverged after tool mutation")
	}

	// 缩短(/new): 清空后重建
	v.SetMessages(msgs[:1])
	if len(v.lines) == 0 {
		t.Fatalf("expected lines after shrink to 1 message")
	}
	if !strings.Contains(strings.Join(v.lines, "\n"), "你好") {
		t.Fatalf("shrunken viewport should contain only first message")
	}
}

func TestViewportResizeClampsScroll(t *testing.T) {
	v := NewMessageViewport(30, 4)
	v.SetMessages([]ChatMessage{{Role: "user", Content: strings.Repeat("line\n", 40)}})
	v.ScrollUp(2)
	v.Resize(30, 80)
	if v.scrollOffset != 0 || v.NewLinesCount() < 0 {
		t.Fatal("resize left an invalid scroll position")
	}
}

func TestConfirmationStaysInsideFrame(t *testing.T) {
	m := New(&runtime.AgentSession{}, slashcmd.NewRegistry(), false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.confirmation.Show("id", "bash", strings.Repeat("long description ", 200))
	view := m.View()
	if lipgloss.Height(view) != 24 || !strings.Contains(view, "Cancel") {
		t.Fatal("confirmation must fit and keep action buttons visible")
	}
}
