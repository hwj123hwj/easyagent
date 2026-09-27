package feishu

import (
	"context"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOwnerPairing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "owner.json")
	a, err := OpenOwnerAccess(p, "")
	if err != nil {
		t.Fatal(err)
	}
	code := a.state.Code
	for _, m := range []Message{{Text: "hello"}, {Text: "/pair " + code, SenderOpenID: "attacker", ChatType: "group", MsgType: "text"}, {Text: "/pair wrong", SenderOpenID: "attacker", ChatType: "p2p", MsgType: "text"}, {Text: "/pair " + code + " ", SenderOpenID: "attacker", ChatType: "p2p", MsgType: "text"}} {
		if a.Pair(m) {
			t.Fatal("invalid pairing accepted")
		}
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, id := range []string{"holder1", "holder2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if a.Pair(Message{Text: "/pair " + code, SenderOpenID: id, ChatType: "p2p", MsgType: "text"}) {
				wins.Add(1)
			}
		}(id)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("code not single-use")
	}
	reloaded, err := OpenOwnerAccess(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Allowed(a.state.Owner) || reloaded.Allowed("other") || reloaded.Allowed("") || reloaded.state.Code != "" {
		t.Fatal("invalid persisted authorization")
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal("state not private")
	}
	a.state = ownerState{Code: code, Expires: time.Now().Add(-time.Second)}
	if a.Pair(Message{Text: "/pair " + code, SenderOpenID: "holder", ChatType: "p2p", MsgType: "text"}) {
		t.Fatal("expired code accepted")
	}
}

func TestUnauthorizedEntrypoints(t *testing.T) {
	a, err := OpenOwnerAccess(filepath.Join(t.TempDir(), "owner.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"", "owner"} {
		a.state.Owner = owner
		h := &Handler{access: a} // nil clients ensure a denial cannot perform any IO
		for _, text := range []string{"hello", "/project create /tmp/x", "/worktree discard", "/new"} {
			h.Handle(context.Background(), Message{Text: text, SenderOpenID: "stranger"})
		}
		ev := cardActionEvent("chat", worktreeActionDiscard, "")
		ev.Event.Operator.OpenID = "stranger"
		resp, err := h.HandleCardAction(context.Background(), ev)
		if err != nil || resp.Toast.Content != "未授权" {
			t.Fatal("card accepted")
		}
		var calls atomic.Int32
		g := NewGateway("", "", nil, func(context.Context, Message) { calls.Add(1) })
		g.SetOwnerAccess(a)
		choice := make(chan string, 1)
		g.choiceWaiters["chat"] = &choiceWaiter{ch: choice, buttons: []string{"yes"}}
		for _, kind := range []string{"text", "image", "post"} {
			text := `{"text":"yes","image_key":"unused"}`
			actor := "stranger"
			senderType := "user"
			chat := "chat"
			id := "message"
			g.handleEvent(context.Background(), &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{Sender: &larkim.EventSender{SenderType: &senderType, SenderId: &larkim.UserId{OpenId: &actor}}, Message: &larkim.EventMessage{MessageType: &kind, Content: &text, ChatId: &chat, MessageId: &id}}})
		}
		if calls.Load() != 0 || len(choice) != 0 {
			t.Fatal("unauthorized message reached choice or handler")
		}
	}
}

func TestOwnerCallbackDeniesMissingTokenAndUnknownSession(t *testing.T) {
	a, err := OpenOwnerAccess(filepath.Join(t.TempDir(), "owner.json"), "owner")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{access: a, piAgentAPIKey: "test-key", senders: map[string]string{"known": "owner"}}
	for _, tc := range []struct {
		token, session string
		want           int
	}{
		{"", "known", http.StatusUnauthorized},
		{"wrong", "known", http.StatusUnauthorized},
		{"test-key", "unknown", http.StatusForbidden},
		{"test-key", "known", http.StatusBadRequest}, // admitted to parameter validation; no network IO
	} {
		r := httptest.NewRequest(http.MethodPost, "/tool-callback", strings.NewReader(`{"session_id":"`+tc.session+`","params":{}}`))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.HandleToolCallback(w, r)
		if w.Code != tc.want {
			t.Fatalf("got %d, want %d", w.Code, tc.want)
		}
	}
}

func TestAuthorizedGatewayMessageReachesHandler(t *testing.T) {
	a, err := OpenOwnerAccess(filepath.Join(t.TempDir(), "owner.json"), "owner")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan Message, 1)
	g := NewGateway("", "", nil, func(_ context.Context, msg Message) { got <- msg })
	g.SetOwnerAccess(a)
	kind, content, actor, senderType, chat, id := "text", `{"text":"hello"}`, "owner", "user", "private-chat", "message"
	g.handleEvent(context.Background(), &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender:  &larkim.EventSender{SenderType: &senderType, SenderId: &larkim.UserId{OpenId: &actor}},
		Message: &larkim.EventMessage{MessageType: &kind, Content: &content, ChatId: &chat, MessageId: &id},
	}})
	select {
	case msg := <-got:
		if msg.SenderOpenID != actor || msg.Text != "hello" {
			t.Fatal(msg)
		}
	case <-time.After(time.Second):
		t.Fatal("owner message was blocked")
	}
}
