package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
)

func TestForkSessionCopiesHistoryAndMetadata(t *testing.T) {
	s, _, _ := newRunTestServer(t)

	// Create source session with some user/assistant entries
	source, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	sourceID := source.SessionID()

	// Append two rounds of dialogue
	u1 := ai.NewTextUserMessage("第一句提问")
	require.NoError(t, source.Session().AppendMessage(context.Background(), u1))
	a1 := ai.AssistantMessage{Text: "第一句回答"}
	require.NoError(t, source.Session().AppendMessage(context.Background(), a1))

	u2 := ai.NewTextUserMessage("第二句提问")
	require.NoError(t, source.Session().AppendMessage(context.Background(), u2))
	a2 := ai.AssistantMessage{Text: "第二句回答"}
	require.NoError(t, source.Session().AppendMessage(context.Background(), a2))

	// Verify messages endpoint returns entry_id for all entries
	{
		req := httptest.NewRequest("GET", "/sessions/"+sourceID+"/messages", nil)
		req.SetPathValue("id", sourceID)
		rec := httptest.NewRecorder()
		s.getSessionMessages(rec, req)
		require.Equal(t, 200, rec.Code)

		var msgs []map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &msgs))
		require.Len(t, msgs, 4)
		for _, m := range msgs {
			entryID, ok := m["entry_id"].(string)
			require.True(t, ok)
			require.NotEmpty(t, entryID)
		}
	}

	// 1. Full fork (empty body)
	{
		req := httptest.NewRequest("POST", "/sessions/"+sourceID+"/fork", bytes.NewReader([]byte("{}")))
		req.SetPathValue("id", sourceID)
		rec := httptest.NewRecorder()
		s.forkSession(rec, req)
		require.Equal(t, 201, rec.Code)

		var resp forkResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.ID)
		require.NotEqual(t, sourceID, resp.ID)
		require.Equal(t, sourceID, resp.SourceID)
		require.Equal(t, 4, resp.KeptMessages)
		require.Equal(t, 2, resp.KeptUserMessages)

		// Check the forked session info from List
		list, err := s.app.SessionManager().List(context.Background())
		require.NoError(t, err)
		var forkedInfo *struct {
			Title      string
			ForkedFrom string
		}
		for _, item := range list {
			if item.ID == resp.ID {
				forkedInfo = &struct {
					Title      string
					ForkedFrom string
				}{Title: item.Title, ForkedFrom: item.ForkedFrom}
				break
			}
		}
		require.NotNil(t, forkedInfo)
		require.Equal(t, sourceID, forkedInfo.ForkedFrom)
		require.Contains(t, forkedInfo.Title, "分叉")
	}

	// 2. Fork before the 2nd user message (before_message_index = 1)
	{
		body := `{"before_message_index": 1}`
		req := httptest.NewRequest("POST", "/sessions/"+sourceID+"/fork", bytes.NewReader([]byte(body)))
		req.SetPathValue("id", sourceID)
		rec := httptest.NewRecorder()
		s.forkSession(rec, req)
		require.Equal(t, 201, rec.Code)

		var resp forkResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.ID)
		require.Equal(t, 2, resp.KeptMessages)
		require.Equal(t, 1, resp.KeptUserMessages)
		require.Equal(t, 2, resp.DroppedMessages)
	}
}

func TestAttachmentRawEndpointServesInlineImage(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)

	var imgBuf bytes.Buffer
	require.NoError(t, png.Encode(&imgBuf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	att := uploadInput(t, s, sess.SessionID(), "test.png", imgBuf.Bytes(), 201)

	// Call GET raw
	req := httptest.NewRequest("GET", "/sessions/"+sess.SessionID()+"/attachments/"+att.ID+"/raw", nil)
	req.SetPathValue("id", sess.SessionID())
	req.SetPathValue("attID", att.ID)
	rec := httptest.NewRecorder()
	s.getAttachmentRaw(rec, req)

	require.Equal(t, 200, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "inline")
	require.Equal(t, imgBuf.Bytes(), rec.Body.Bytes())
}

func TestForkEmptyAndCompactedAnchors(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	ctx := context.Background()
	source, err := s.resolveSession(ctx, "")
	require.NoError(t, err)
	history := []ai.Message{ai.NewTextUserMessage("old question"), ai.AssistantMessage{Text: "old answer"}, ai.NewTextUserMessage("retained question"), ai.AssistantMessage{Text: "retained answer"}, ai.NewTextUserMessage("future question"), ai.AssistantMessage{Text: "future answer"}}
	for _, msg := range history {
		require.NoError(t, source.Session().AppendMessage(ctx, msg))
	}
	ids, err := source.Session().BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	require.NoError(t, source.Session().AppendCompactionKeeping(ctx, "keep architecture", history[2:], nil))
	compactIDs, err := source.Session().BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, ids[2:], compactIDs[1:])
	snapshot := serializeSessionContext(source, append([]ai.Message{ai.NewTextUserMessage("synthetic")}, history[2:]...))
	require.Equal(t, "compaction", snapshot[0]["role"])
	require.Equal(t, ids[2], snapshot[1]["entry_id"])
	for _, tc := range []struct {
		name, body string
		kept       int
		compact    bool
	}{
		{"empty", `{"before_message_index":0}`, 0, false},
		{"retained user", `{"entry_id":"` + ids[2] + `"}`, 1, true},
		{"retained answer", `{"entry_id":"` + ids[3] + `"}`, 2, true},
		{"before next user", `{"before_message_index":1}`, 2, true},
		{"full", `{}`, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/sessions/"+source.SessionID()+"/fork", bytes.NewBufferString(tc.body))
			req.SetPathValue("id", source.SessionID())
			rec := httptest.NewRecorder()
			s.forkSession(rec, req)
			require.Equal(t, 201, rec.Code, rec.Body.String())
			var response forkResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, tc.kept, response.KeptMessages)
			forked, _, err := s.app.SessionManager().Open(ctx, response.ID)
			require.NoError(t, err)
			defer forked.Storage().Close()
			messages, err := forked.BuildContext(ctx)
			require.NoError(t, err)
			want := tc.kept
			if tc.compact {
				want++
				require.Contains(t, messages[0].(ai.UserMessage).Content[0].Text, "keep architecture")
			}
			require.Len(t, messages, want)
			if tc.compact {
				require.Equal(t, history[2], messages[1])
			}
		})
	}
	// Forking must never close or reposition the registry's live source storage.
	require.NoError(t, source.Session().AppendMessage(ctx, ai.NewTextUserMessage("source still works")))
	messages, err := source.Session().BuildContext(ctx)
	require.NoError(t, err)
	require.Len(t, messages, 6)
}

func TestForkRejectsMalformedAndActiveRequests(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	source, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	for _, body := range []string{`{"entry_id":"missing"}`, `{"before_message_index":-1}`, `{"before_message_index":0,"entry_id":""}`, `{"unknown":true}`, `{`, `{} {}`} {
		req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
		req.SetPathValue("id", source.SessionID())
		rec := httptest.NewRecorder()
		s.forkSession(rec, req)
		require.Equal(t, 400, rec.Code, body)
	}
	s.runs.sessions[source.SessionID()] = &sessionRunState{run: &sessionRun{State: "running"}}
	req := httptest.NewRequest("POST", "/", nil)
	req.SetPathValue("id", source.SessionID())
	rec := httptest.NewRecorder()
	s.forkSession(rec, req)
	require.Equal(t, 409, rec.Code)
	s.runs.sessions[source.SessionID()].run = nil
	list, err := s.app.SessionManager().List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestAttachmentPreviewRequiresAuthAndRejectsWorkspaceEscape(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	att := uploadInput(t, s, sess.SessionID(), "preview.png", pngBytes.Bytes(), 201)
	s.SetAPIKey("fixture-token")
	url := "/sessions/" + sess.SessionID() + "/attachments/" + att.ID + "/raw?format=data_url"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	require.Equal(t, 401, rec.Code)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer fixture-token")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "data:image/png;base64,")
	require.NoError(t, os.Remove(att.Path))
	external := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(external, pngBytes.Bytes(), 0600))
	require.NoError(t, os.Symlink(external, att.Path))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	require.Equal(t, 400, rec.Code)
}

func TestForkFinalAnswerRetainsToolsAndExcludesNextTurn(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	ctx := context.Background()
	source, err := s.resolveSession(ctx, "")
	require.NoError(t, err)
	history := []ai.Message{
		ai.NewTextUserMessage("repeat"),
		ai.AssistantMessage{Text: "same", ToolCalls: []ai.ToolCall{{ID: "read", Name: "read"}}},
		ai.ToolResultMessage{ToolCallID: "read", Content: "file contents"},
		ai.AssistantMessage{Text: "same", StopReason: ai.StopReasonStop},
		ai.NewTextUserMessage("repeat"),
		ai.AssistantMessage{Text: "same", StopReason: ai.StopReasonStop},
	}
	for _, msg := range history {
		require.NoError(t, source.Session().AppendMessage(ctx, msg))
	}
	ids, err := source.Session().BuildContextEntryIDs(ctx)
	require.NoError(t, err)
	cleaned := append([]ai.Message(nil), history...)
	cleaned[2] = ai.ToolResultMessage{ToolCallID: "read", Content: "cleared model input"}
	require.NoError(t, source.Session().AppendMicroCompaction(ctx, cleaned))
	// Cleanup markers must not shift the displayed answer's fork anchor or
	// replace the original output in the history used by the desktop.
	historyReq := httptest.NewRequest("GET", "/", nil)
	historyReq.SetPathValue("id", source.SessionID())
	historyRec := httptest.NewRecorder()
	s.getSessionMessages(historyRec, historyReq)
	require.Equal(t, 200, historyRec.Code)
	var displayed []map[string]any
	require.NoError(t, json.Unmarshal(historyRec.Body.Bytes(), &displayed))
	require.Len(t, displayed, len(history))
	require.Equal(t, ids[3], displayed[3]["entry_id"])
	require.Contains(t, historyRec.Body.String(), "file contents")
	require.NotContains(t, historyRec.Body.String(), "cleared model input")
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"entry_id":"`+ids[3]+`"}`))
	req.SetPathValue("id", source.SessionID())
	rec := httptest.NewRecorder()
	s.forkSession(rec, req)
	require.Equal(t, 201, rec.Code, rec.Body.String())
	var response forkResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 4, response.KeptMessages)
	forked, _, err := s.app.SessionManager().Open(ctx, response.ID)
	require.NoError(t, err)
	defer forked.Storage().Close()
	retained, err := forked.BuildContext(ctx)
	require.NoError(t, err)
	require.Equal(t, history[:4], retained)
}
