package tea

import (
	"bytes"
	"strings"
	"testing"
)

func TestCursorPositionAboveFooter(t *testing.T) {
	defer SetCursorColumn(0)
	var out bytes.Buffer
	r := newRenderer(&out, false, 60).(*standardRenderer)
	r.altScreenActive = true
	r.width, r.height = 80, 24
	SetCursorPosition(6, 1)
	r.write("messages\ninput\nhelp\nstatus")
	r.flush()
	if !strings.HasSuffix(out.String(), "\x1b[2;7H") {
		t.Fatalf("wrong terminal caret: %q", out.String())
	}
	out.Reset()
	SetCursorPosition(2, 0)
	r.write("changed\ninput\nhelp\nstatus")
	r.flush()
	if !strings.HasPrefix(out.String(), "\x1b[H") || !strings.HasSuffix(out.String(), "\x1b[1;3H") {
		t.Fatalf("repaint must start at home and restore caret: %q", out.String())
	}
	out.Reset()
	SetCursorColumn(2)
	r.write("changed again\ninput\nhelp\nstatus")
	r.flush()
	if !strings.HasSuffix(out.String(), "\x1b[4;3H") {
		t.Fatal("legacy column API must reset row")
	}
}
