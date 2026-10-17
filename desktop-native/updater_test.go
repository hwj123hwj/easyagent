package main

import (
	"github.com/egoist/mygo/ui/editor"
	"testing"
	"time"
)

func TestQuitProtectsBackgroundWorkAndEditor(t *testing.T) {
	a := alignmentPreview(t)
	if got := a.quitBlockReason(); got != "" {
		t.Fatal(got)
	}
	background := &session{ID: 99, Running: true}
	a.Sessions = append(a.Sessions, background)
	if a.quitBlockReason() == "" {
		t.Fatal("background task would be killed by update")
	}
	background.Running = false
	a.Live.Loading["model-save"] = true
	if a.quitBlockReason() == "" {
		t.Fatal("configuration save would be interrupted")
	}
	a.Live.Loading["model-save"] = false
	f := &nativeFile{Code: &editor.State{Buffer: editor.NewBuffer("original")}}
	f.Code.Insert("unsaved")
	a.Live.Workbench.Files["file"] = f
	if a.quitBlockReason() == "" {
		t.Fatal("unsaved editor would be lost")
	}
	f.Code.Buffer.Reset("original")
	if got := a.quitBlockReason(); got != "" {
		t.Fatal(got)
	}
}
func TestIdleTickDoesNotInvalidateLayout(t *testing.T) {
	a := alignmentPreview(t)
	a.Live.LastSave = time.Now()
	for i := 0; i < 100; i++ {
		if a.tick() {
			t.Fatal("idle tick requested a full layout")
		}
	}
	applied := false
	a.Live.results <- liveResult{epoch: a.Live.epoch, apply: func() { applied = true }}
	if !a.tick() || !applied {
		t.Fatal("background result failed to invalidate")
	}
	if a.tick() {
		t.Fatal("result kept invalidating after drain")
	}
}
