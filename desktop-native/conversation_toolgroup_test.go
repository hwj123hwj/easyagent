package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func groupHeader(t *testing.T, tt *ui.Tester) string {
	t.Helper()
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, "个工具调用") {
			return txt
		}
	}
	t.Fatal("group header missing")
	return ""
}

func TestToolGroupDefaultsCollapsed(t *testing.T) {
	a := newPreview()
	a.Live = &liveState{Workbench: newNativeWorkbench()}
	s := a.current()
	s.Messages = []*message{
		{User: true, Text: "你好"},
		{Kind: "tool", Tool: "bash", CallID: "c1", Args: map[string]any{"command": "ls"}, Result: "ok", Status: "完成"},
		{Kind: "tool", Tool: "read", CallID: "c2", Args: map[string]any{"path": "x"}, Result: "ok", Status: "完成"},
		{Text: "done"},
	}
	s.Run = &runInfo{ID: "r1", State: "completed"}
	s.Seq = 5
	tt := ui.NewTester(a.View, 1240, 820)
	tt.Frame()
	for _, m := range s.Messages {
		if m.Kind == "tool" && m.Expanded {
			t.Fatal("finished tool rows should default collapsed")
		}
	}
	// expand group
	if err := tt.Click(groupHeader(t, tt)); err != nil {
		t.Fatalf("expand group: %v", err)
	}
	tt.Frame()
	// rows are individually collapsed until the user expands one; the group just makes them visible
	if err := tt.Click("展开工具结果"); err != nil {
		t.Fatalf("expand row: %v", err)
	}
	tt.Frame()
	if !s.Messages[1].Expanded {
		t.Fatal("clicked tool row should expand")
	}
	if err := tt.Click("收起工具结果"); err != nil {
		t.Fatalf("collapse row: %v", err)
	}
	tt.Frame()
	if s.Messages[1].Expanded {
		t.Fatal("clicked tool row should collapse back")
	}
}

func TestToolGroupCollapseSurvivesRerender(t *testing.T) {
	a := newPreview()
	a.Live = &liveState{Workbench: newNativeWorkbench()}
	s := a.current()
	s.Messages = []*message{
		{User: true, Text: "你好"},
		{Kind: "tool", Tool: "bash", CallID: "c1", Args: map[string]any{"command": "ls"}, Result: "ok", Status: "完成"},
		{Kind: "tool", Tool: "boom", CallID: "c2", Args: map[string]any{"command": "x"}, Result: "exit 1", Status: "失败"},
		{Text: "done"},
	}
	s.Run = &runInfo{ID: "r1", State: "completed"}
	s.Seq = 5
	tt := ui.NewTester(a.View, 1240, 820)
	tt.Frame()
	if err := tt.Click(groupHeader(t, tt)); err != nil {
		t.Fatalf("collapse: %v", err)
	}
	tt.Frame()
	// hammer more frames like a running session does
	for i := 0; i < 10; i++ {
		tt.Frame()
	}
	if tt.HasText("收起工具结果") {
		t.Fatal("collapsed group re-opened on rerender")
	}
}

func TestToolGroupFailedCanBeCollapsed(t *testing.T) {
	a := newPreview()
	a.Live = &liveState{Workbench: newNativeWorkbench()}
	s := a.current()
	s.Messages = []*message{
		{User: true, Text: "你好"},
		{Kind: "tool", Tool: "bash", CallID: "c1", Args: map[string]any{"command": "ls"}, Result: "ok", Status: "完成"},
		{Kind: "tool", Tool: "boom", CallID: "c2", Args: map[string]any{"command": "x"}, Result: "exit 1", Status: "失败"},
		{Text: "done"},
	}
	s.Run = &runInfo{ID: "r1", State: "completed"}
	s.Seq = 5
	tt := ui.NewTester(a.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("展开工具结果") {
		t.Fatal("failed group should start open")
	}
	tt.Frame() // a second pass, like live ticking does
	// group starts open (failed). Collapse it:
	if err := tt.Click(groupHeader(t, tt)); err != nil {
		t.Fatalf("collapse failed group: %v", err)
	}
	tt.Frame()
	for i := 0; i < 3; i++ {
		tt.Frame() // settle: a forced re-open would surface here
	}
	// simulate incoming live events (new snapshot) after collapse, like real session traffic
	calls := []struct {
		ID, Name string
		Args     any
	}{{ID: "c1", Name: "bash", Args: map[string]any{"command": "ls"}}, {ID: "c2", Name: "boom", Args: map[string]any{"command": "x"}}}
	s.reduce(envelope{Type: "snapshot", Seq: 6, Reset: false, Messages: []storedMessage{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "", ToolCalls: calls},
		{Role: "tool", ToolID: "c1", Content: "ok"},
		{Role: "tool", ToolID: "c2", Content: "exit 1", Error: true},
	}})
	tt.Frame()
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	if tt.HasText("exit 1") || tt.HasText("收起工具结果") {
		t.Fatal("failed group should stay collapsed after user collapses it (even after snapshot refresh)")
	}
}

func TestToolGroupNewFailureAutoOpensOnce(t *testing.T) {
	a := newPreview()
	a.Live = &liveState{Workbench: newNativeWorkbench()}
	s := a.current()
	s.Messages = []*message{
		{User: true, Text: "你好"},
		{Kind: "tool", Tool: "bash", CallID: "c1", Args: map[string]any{"command": "ls"}, Result: "ok", Status: "完成"},
		{Text: "done"},
	}
	s.Run = &runInfo{ID: "r1", State: "completed"}
	s.Seq = 5
	tt := ui.NewTester(a.View, 1240, 820)
	tt.Frame()
	// all-success group starts closed
	if tt.HasText("收起工具结果") {
		t.Fatal("all-success group should start closed")
	}
	// a failure arrives later: group should auto-open once to surface it
	s.Messages[1].Status = "失败"
	s.Messages[1].Result = "exit 1"
	tt.Frame()
	if !tt.HasText("展开工具结果") {
		t.Fatal("new failure should auto-open the group once")
	}
	// user collapses; it must stay closed despite failure persisting
	if err := tt.Click(groupHeader(t, tt)); err != nil {
		t.Fatalf("collapse after failure: %v", err)
	}
	tt.Frame()
	for i := 0; i < 5; i++ {
		tt.Frame()
	}
	if tt.HasText("展开工具结果") {
		t.Fatal("group must stay collapsed after user closes it, failure or not")
	}
}
