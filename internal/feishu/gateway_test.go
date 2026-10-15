package feishu

import (
	"context"
	"fmt"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"normal text", `{"text":"hello world"}`, "hello world"},
		{"empty text", `{"text":""}`, ""},
		{"invalid json", "not json", "not json"},
		{"nil content", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var content *string
			if tt.name != "nil content" {
				content = &tt.content
			}
			got := extractText(content)
			if got != tt.want {
				t.Errorf("extractText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDedup(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)

	// First time: not duplicate
	if g.isDuplicate("msg1") {
		t.Error("msg1 should not be duplicate on first check")
	}

	// Second time: duplicate
	if !g.isDuplicate("msg1") {
		t.Error("msg1 should be duplicate on second check")
	}

	// Different message: not duplicate
	if g.isDuplicate("msg2") {
		t.Error("msg2 should not be duplicate on first check")
	}
}

func TestDedupEviction(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)
	g.maxSeen = 10

	// Add 15 messages (exceeds maxSeen of 10)
	for i := 0; i < 15; i++ {
		msgID := fmt.Sprintf("msg_%03d", i)
		g.isDuplicate(msgID)
	}

	// Map should be at most maxSeen entries
	g.mu.Lock()
	count := len(g.seen)
	g.mu.Unlock()

	if count > g.maxSeen {
		t.Errorf("expected at most %d entries, got %d", g.maxSeen, count)
	}
}

func TestContentDedup(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)

	// First time: not duplicate
	if g.isContentDuplicate("chat1", "hello") {
		t.Error("first occurrence should not be duplicate")
	}

	// Same content within window: duplicate
	if !g.isContentDuplicate("chat1", "hello") {
		t.Error("same content within window should be duplicate")
	}

	// Different content: not duplicate
	if g.isContentDuplicate("chat1", "world") {
		t.Error("different content should not be duplicate")
	}

	// Different chat: not duplicate
	if g.isContentDuplicate("chat2", "hello") {
		t.Error("same content in different chat should not be duplicate")
	}
}

func TestContentDedupExpired(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)
	g.dedupWindowMs = 0 // expire immediately

	// First time
	if g.isContentDuplicate("chat1", "hello") {
		t.Error("first occurrence should not be duplicate")
	}

	// After window expires: not duplicate
	if g.isContentDuplicate("chat1", "hello") {
		t.Error("expired content should not be duplicate")
	}
}

func TestMessageChatKey(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantKey string
	}{
		{
			name: "group chat uses chatID",
			msg: Message{
				ChatType:     "group",
				ChatID:       "oc_123",
				SenderOpenID: "ou_456",
			},
			wantKey: "oc_123",
		},
		{
			name: "direct chat uses senderOpenID",
			msg: Message{
				ChatType:     "p2p",
				ChatID:       "",
				SenderOpenID: "ou_789",
			},
			wantKey: "ou_789",
		},
		{
			name: "fallback to chatID",
			msg: Message{
				ChatType:     "",
				ChatID:       "oc_fallback",
				SenderOpenID: "",
			},
			wantKey: "oc_fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.ChatKey(); got != tt.wantKey {
				t.Errorf("ChatKey() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

func TestMatchChoice_ByNumber(t *testing.T) {
	buttons := []string{"Option A", "Option B", "Option C"}
	tests := []struct {
		input string
		want  string
	}{
		{"1", "Option A"},
		{"2", "Option B"},
		{"3", "Option C"},
		{"4", ""},
		{"0", ""},
	}
	for _, tt := range tests {
		got := matchChoice(tt.input, buttons)
		if got != tt.want {
			t.Errorf("matchChoice(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchChoice_ByText(t *testing.T) {
	buttons := []string{"Yes", "No", "Maybe"}
	tests := []struct {
		input string
		want  string
	}{
		{"yes", "Yes"},
		{"YES", "Yes"},
		{"No", "No"},
		{"maybe", "Maybe"},
		{"unknown", ""},
		{"  yes  ", "Yes"}, // trimmed
	}
	for _, tt := range tests {
		got := matchChoice(tt.input, buttons)
		if got != tt.want {
			t.Errorf("matchChoice(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchChoice_EmptyButtons(t *testing.T) {
	got := matchChoice("1", nil)
	if got != "" {
		t.Errorf("expected empty for nil buttons, got %q", got)
	}
}

func TestTryResolveChoice_NoWaiter(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)
	if g.tryResolveChoice("chat1", "hello") {
		t.Error("should return false when no waiter exists")
	}
}

func TestTryResolveChoice_Match(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)
	ch := make(chan string, 1)
	g.choiceMu.Lock()
	g.choiceWaiters["chat1"] = &choiceWaiter{
		ch:         ch,
		buttons:    []string{"Yes", "No"},
		defaultVal: "No",
	}
	g.choiceMu.Unlock()

	if !g.tryResolveChoice("chat1", "1") {
		t.Error("should return true when match found")
	}
	if got := <-ch; got != "Yes" {
		t.Errorf("expected 'Yes' on channel, got %q", got)
	}
}

func TestTryResolveChoice_NoMatch(t *testing.T) {
	g := NewGateway("test", "test", nil, nil)
	ch := make(chan string, 1)
	g.choiceMu.Lock()
	g.choiceWaiters["chat1"] = &choiceWaiter{
		ch:         ch,
		buttons:    []string{"Yes", "No"},
		defaultVal: "No",
	}
	g.choiceMu.Unlock()

	if g.tryResolveChoice("chat1", "random text") {
		t.Error("should return false when no match")
	}

	// Channel should be empty
	select {
	case v := <-ch:
		t.Errorf("channel should be empty, got %q", v)
	default:
	}
}

func TestPostMessageFormats(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"event body", `{"title":"分析","content":[[{"tag":"text","text":"看看 "},{"tag":"a","text":"项目","href":"https://example.com"}],[{"tag":"text","text":"第二行"}]]}`, "**分析**\n看看 [项目](https://example.com)\n第二行"},
		{"localized", `{"zh_cn":{"title":"中文","content":[[{"tag":"text","text":"你好"}]]},"en_us":{"title":"English","content":[[{"tag":"text","text":"hello"}]]}}`, "**中文**\n你好"},
		{"code and markdown", `{"content":[[{"tag":"md","text":"**任务**"}],[{"tag":"code_block","language":"go","text":"hello()"}]]}`, "**任务**\n\n```go\nhello()\n```\n"},
		{"title only", `{"title":"任务","content":[]}`, "**任务**"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&Gateway{}).handlePostMessage(context.Background(), &larkim.EventMessage{Content: &tt.content})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q; want %q", got, tt.want)
			}
		})
	}
}

func TestInvalidPostDoesNotReachAgent(t *testing.T) {
	access, err := OpenOwnerAccess(filepath.Join(t.TempDir(), "owner.json"), "owner")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan Message, 1)
	g := NewGateway("", "", nil, func(_ context.Context, msg Message) { got <- msg })
	g.SetOwnerAccess(access)
	kind, content, actor, senderType, chat, id := "post", `{"broken":true}`, "owner", "user", "private", "invalid-post"
	event := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender:  &larkim.EventSender{SenderType: &senderType, SenderId: &larkim.UserId{OpenId: &actor}},
		Message: &larkim.EventMessage{MessageType: &kind, Content: &content, ChatId: &chat, MessageId: &id},
	}}
	g.handleEvent(context.Background(), event)
	content = `{"title":"","content":[[{"tag":"text","text":"正常消息"}]]}`
	id = "valid-post"
	g.handleEvent(context.Background(), event)
	select {
	case msg := <-got:
		if msg.Text != "正常消息" {
			t.Fatalf("invalid post reached agent: %q", msg.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("valid post was not dispatched")
	}
}
func TestInvalidPostFormats(t *testing.T) {
	for _, content := range []string{`not JSON`, `null`, `{"content":"broken"}`, `{"title":"","content":[]}`} {
		if _, err := (&Gateway{}).handlePostMessage(context.Background(), &larkim.EventMessage{Content: &content}); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}
