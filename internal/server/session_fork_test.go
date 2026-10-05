package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
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
