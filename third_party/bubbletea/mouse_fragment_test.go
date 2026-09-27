package tea

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestPartialMouseReportIsNotKeyboardInput(t *testing.T) {
	report := "\x1b[<65;20;5M"
	for n := 3; n < len(report); n++ {
		for _, more := range []bool{true, false} {
			w, msg := detectOneMsg([]byte(report[:n]), more)
			if w != 0 || msg != nil {
				t.Fatalf("partial %q parsed as %v", report[:n], msg)
			}
		}
	}
}

func TestMouseBurstAcrossReadBoundaries(t *testing.T) {
	const count = 160
	input := strings.NewReader(strings.Repeat("\x1b[<65;20;5M", count) + "\x04")
	msgs := make(chan Msg, count+100)
	err := readAnsiInputs(context.Background(), msgs, input)
	if err == nil || !strings.Contains(err.Error(), io.EOF.Error()) {
		t.Fatalf("read error=%v", err)
	}
	close(msgs)
	wheels, exit := 0, 0
	for msg := range msgs {
		switch event := msg.(type) {
		case MouseMsg:
			if event.Button != MouseButtonWheelDown || event.X != 19 || event.Y != 4 {
				t.Fatalf("wrong mouse: %+v", event)
			}
			wheels++
		case KeyMsg:
			if event.Type != KeyCtrlD {
				t.Fatalf("mouse leaked as keyboard text: %+v", event)
			}
			exit++
		default:
			t.Fatalf("unexpected mouse fragment: %#v", msg)
		}
	}
	if wheels != count || exit != 1 {
		t.Fatalf("wheels=%d exit=%d", wheels, exit)
	}
}

type fragmentedReader struct{ chunks [][]byte }

func (r *fragmentedReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if len(r.chunks[0]) == 0 {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

func TestMouseReportSplitAtEveryByteIncludingEscape(t *testing.T) {
	report := []byte("\x1b[<0;5;4M\x1b[<0;5;4m")
	for split := 1; split < len(report); split++ {
		r := &fragmentedReader{[][]byte{report[:split], report[split:]}}
		msgs := make(chan Msg, 32)
		_ = readAnsiInputs(context.Background(), msgs, r)
		close(msgs)
		count := 0
		for msg := range msgs {
			if _, ok := msg.(MouseMsg); !ok {
				t.Fatalf("split %d leaked %#v", split, msg)
			}
			count++
		}
		if count != 2 {
			t.Fatalf("split %d count=%d", split, count)
		}
	}
	r := &fragmentedReader{}
	for _, b := range report {
		r.chunks = append(r.chunks, []byte{b})
	}
	msgs := make(chan Msg, 32)
	_ = readAnsiInputs(context.Background(), msgs, r)
	close(msgs)
	for msg := range msgs {
		if _, ok := msg.(MouseMsg); !ok {
			t.Fatalf("one-byte reads leaked %#v", msg)
		}
	}
}

func TestStandaloneEscapeStillArrives(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgs := make(chan Msg, 4)
	go readAnsiInputs(ctx, msgs, reader)
	_, _ = writer.Write([]byte{27})
	select {
	case msg := <-msgs:
		key, ok := msg.(KeyMsg)
		if !ok || key.Type != KeyEsc {
			t.Fatalf("escape=%#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("standalone Escape stalled")
	}
}
