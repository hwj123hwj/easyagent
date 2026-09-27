package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type cardRoundTrip func(*http.Request) (*http.Response, error)

func (f cardRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testCard(transport cardRoundTrip) *StreamingCardHandle {
	return &StreamingCardHandle{CardID: "card-test", MessageID: "message-test", client: &Client{cachedToken: "test-token", tokenExpiresAt: time.Now().Add(time.Hour), cardClient: &http.Client{Transport: transport}}}
}
func cardResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}
func cardText(t *testing.T, r *http.Request) (string, int) {
	t.Helper()
	var payload struct {
		Sequence int `json:"sequence"`
		Card     struct {
			Data string `json:"data"`
		} `json:"card"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	var card struct {
		Config struct {
			Streaming bool `json:"streaming_mode"`
		} `json:"config"`
		Body struct {
			Elements []struct {
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(payload.Card.Data), &card); err != nil {
		t.Fatal(err)
	}
	if card.Config.Streaming {
		t.Error("native typewriter should be disabled")
	}
	return card.Body.Elements[0].Content, payload.Sequence
}
func awaitCard[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for card worker")
		var zero T
		return zero
	}
}

func TestCardUpdatesCoalesceAndFlushTrailingText(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	sent := make(chan string, 10)
	var mu sync.Mutex
	sequences := []int{}
	card := testCard(func(r *http.Request) (*http.Response, error) {
		text, seq := cardText(t, r)
		mu.Lock()
		sequences = append(sequences, seq)
		mu.Unlock()
		if text == "first" {
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		sent <- text
		return cardResponse(`{"code":0}`), nil
	})
	updates := newCardUpdater(context.Background(), card, 20*time.Millisecond)
	defer updates.stop()
	updates.update("first", FooterMetrics{Status: "正在生成回复"})
	awaitCard(t, started)
	for i := 0; i < 1000; i++ {
		updates.update(fmt.Sprintf("latest-%d", i), FooterMetrics{Status: "正在生成回复"})
	}
	close(release)
	if got := awaitCard(t, sent); got != "first" {
		t.Fatal(got)
	}
	if got := awaitCard(t, sent); got != "latest-999" {
		t.Fatalf("queued obsolete snapshot: %s", got)
	}
	// No additional token is required to flush text that arrived during an HTTP request.
	mu.Lock()
	defer mu.Unlock()
	if len(sequences) != 2 || sequences[1] <= sequences[0] {
		t.Fatalf("unexpected sequence: %v", sequences)
	}
}

func TestStreamChatDoesNotWaitForSlowCardRequests(t *testing.T) {
	started := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	card := testCard(func(r *http.Request) (*http.Response, error) {
		cardText(t, r)
		started <- struct{}{}
		<-r.Context().Done()
		stopped <- struct{}{}
		return nil, r.Context().Err()
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: text_delta\ndata: {\"text_delta\":\"首\"}\n\n")
		w.(http.Flusher).Flush()
		awaitCard(t, started)
		for i := 0; i < 200; i++ {
			fmt.Fprint(w, "event: text_delta\ndata: {\"text_delta\":\"字\"}\n\n")
		}
		fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer upstream.Close()
	handler := &Handler{piAgentURL: upstream.URL, httpClient: upstream.Client()}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	text, err := handler.streamChat(ctx, "test", "prompt", card)
	if err != nil {
		t.Fatal(err)
	}
	if text != "首"+strings.Repeat("字", 200) {
		t.Fatalf("lost deltas: %d", len(text))
	}
	awaitCard(t, stopped)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("SSE blocked by card I/O: %s", elapsed)
	} else {
		t.Logf("201 deltas with blocked Feishu request consumed in %s", elapsed)
	}
}

func TestCardUpdatesRetryAPIErrorsAndFinalizeLast(t *testing.T) {
	calls := make(chan string, 10)
	attempt := 0
	card := testCard(func(r *http.Request) (*http.Response, error) {
		text, _ := cardText(t, r)
		calls <- text
		attempt++
		if attempt == 1 {
			return cardResponse(`{"code":99991400,"msg":"rate limited"}`), nil
		}
		return cardResponse(`{"code":0}`), nil
	})
	updates := newCardUpdater(context.Background(), card, 10*time.Millisecond)
	updates.update("pending", FooterMetrics{})
	awaitCard(t, calls)
	awaitCard(t, calls)
	updates.stop()
	if err := card.Finalize(context.Background(), "complete reply", &FooterMetrics{Status: "已完成"}); err != nil {
		t.Fatal(err)
	}
	if got := awaitCard(t, calls); got != "complete reply" {
		t.Fatal(got)
	}
	select {
	case extra := <-calls:
		t.Fatalf("progress after final: %s", extra)
	default:
	}
}

func TestCardUpdateReturnsBusinessFailure(t *testing.T) {
	card := testCard(func(r *http.Request) (*http.Response, error) {
		return cardResponse(`{"code":230099,"msg":"invalid card"}`), nil
	})
	if err := card.PushUpdate(context.Background(), "text", FooterMetrics{}); err == nil || !strings.Contains(err.Error(), "230099") {
		t.Fatalf("false success: %v", err)
	}
}

func TestToolOnlyProgressKeepsNonemptyBody(t *testing.T) {
	card := BuildStreamingCard("", RenderFooterMarkdown(FooterMetrics{Status: "正在执行 bash"}))
	elements := card["body"].(map[string]any)["elements"].([]any)
	if elements[0].(map[string]any)["content"] == "" {
		t.Fatal("tool-only progress must retain a visible placeholder")
	}
	if !strings.Contains(elements[2].(map[string]any)["content"].(string), "正在执行 bash") {
		t.Fatal("lost tool status")
	}
}
