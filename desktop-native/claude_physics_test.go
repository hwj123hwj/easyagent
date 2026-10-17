package main

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestScrollPinningCooldown(t *testing.T) {
	a := alignmentPreview(t)
	s := a.current()
	s.Scroll.MaxY = 1000
	s.Scroll.Y = 980
	s.LastUserScrollY = 980

	// User actively scrolls upward by 50px
	s.Scroll.Y = 930
	// Trigger receive with text delta
	a.receive(envelope{
		SessionID: s.RemoteID,
		Type:      "event",
		RunID:     s.runID(),
		Event:     &agentEvent{Type: "text_delta", Text: " new chunk"},
	})

	if time.Now().After(s.ScrollCooldown) {
		t.Fatal("expected ScrollCooldown to be active after user scrolled up")
	}
	if s.Scroll.Y == math.MaxFloat32 {
		t.Fatal("scroll auto-follow should have been blocked during cooldown")
	}

	// User explicitly clicks follow / "回到最新"
	follow(s)
	if !s.ScrollCooldown.IsZero() {
		t.Fatal("follow() should clear ScrollCooldown")
	}
	if s.Scroll.Y != math.MaxFloat32 {
		t.Fatal("follow() should set Scroll.Y to MaxFloat32")
	}
}

func TestNativeThoughtAccordion(t *testing.T) {
	a := alignmentPreview(t)
	s := a.current()
	s.Running = true
	thoughtMsg := &message{
		Kind: "thought",
		Text: "正在分析系统架构与数据流...",
	}
	s.Messages = []*message{thoughtMsg}

	tt := ui.NewTester(a.View, 1240, 820)
	tt.Frame()

	// While generating, thought should auto-expand
	if !thoughtMsg.Expanded {
		t.Fatal("active thought stream should default to expanded")
	}
	if !tt.HasText("正在思考…") {
		t.Fatal("active thought stream should display '正在思考…'")
	}

	// Click to collapse
	click(t, tt, "收起深度思考")
	if thoughtMsg.Expanded {
		t.Fatal("clicking thought header should collapse it")
	}

	// Complete generation
	s.Running = false
	tt.Frame()
	if !tt.HasText("思考过程") {
		t.Fatal("completed thought should display '思考过程'")
	}
}

func TestWorkbenchSplitterPhysics(t *testing.T) {
	a := alignmentPreview(t)
	a.Workbench = true
	a.Settings.Preferences.WorkbenchWidth = 500

	var lastSaved float32
	tt := ui.NewTester(func(c *ui.Context) {
		a.resizeEdge(c, "调整工作台宽度", false, 500, 340, 900, func(v float32) {
			lastSaved = v
			a.Settings.Preferences.WorkbenchWidth = v
		})
	}, 1240, 820)
	tt.Frame()
	_ = lastSaved
}

func TestPromptCachingUsage(t *testing.T) {
	a := alignmentPreview(t)
	s := a.current()
	s.Usage = map[string]any{
		"estimated_tokens": float64(5000),
		"context_window":   float64(200000),
		"window_known":     true,
		"messages":         float64(3000),
		"system":           float64(1000),
		"tools":            float64(1000),
		"last_request": map[string]any{
			"input_tokens":        float64(1500),
			"output_tokens":       float64(400),
			"cached_input_tokens": float64(3500),
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		a.contextUsagePanel(c, s)
	}, 400, 500)
	tt.Frame()

	// 3500 / (3500 + 1500) = 70% cache hit rate
	if !tt.HasText("缓存命中 70%") {
		t.Fatal("expected '缓存命中 70%' to be rendered in context usage panel")
	}
	if !tt.HasText("Prompt Caching 复用 3500 tokens") {
		t.Fatal("expected 'Prompt Caching 复用 3500 tokens' in context usage panel")
	}
}
