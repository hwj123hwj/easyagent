package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/sessionmgr"
	"github.com/stretchr/testify/require"
)

func uploadInput(t *testing.T, s *Server, session, name string, data []byte, expected int) inputAttachment {
	t.Helper()
	body, err := json.Marshal(map[string]string{"name": name, "data": base64.StdEncoding.EncodeToString(data)})
	require.NoError(t, err)
	request := httptest.NewRequest("POST", "/sessions/"+session+"/attachments", bytes.NewReader(body))
	request.SetPathValue("id", session)
	response := httptest.NewRecorder()
	s.uploadAttachment(response, request)
	require.Equal(t, expected, response.Code, response.Body.String())
	var result inputAttachment
	if expected == 201 {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	}
	return result
}

func TestImageAttachmentReachesProviderAndRestoresHistory(t *testing.T) {
	s, _, gateway := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	attachment := uploadInput(t, s, sess.SessionID(), "截图.png", imageBytes.Bytes(), 201)
	inputs := promptInputs{Attachments: []string{attachment.ID}}
	run, duplicate, err := s.startRun(sess.SessionID(), "分析图片", "image-request", inputs)
	require.NoError(t, err)
	require.False(t, duplicate)
	var request map[string]any
	select {
	case request = <-gateway.requests:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not receive request")
	}
	messages := request["messages"].([]any)
	user := messages[len(messages)-1].(map[string]any)
	content := user["content"].([]any)
	imageBlock := content[len(content)-1].(map[string]any)
	require.Equal(t, "image_url", imageBlock["type"])
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(imageBytes.Bytes()), imageBlock["image_url"].(map[string]any)["url"])
	_, _, err = s.startRun(sess.SessionID(), "分析图片", "image-request", promptInputs{})
	require.ErrorContains(t, err, "request_id")
	close(gateway.finish)
	_, err = s.waitRun(context.Background(), run)
	require.NoError(t, err)
	restored, _, err := s.app.SessionManager().Open(context.Background(), sess.SessionID())
	require.NoError(t, err)
	defer restored.Storage().Close()
	history, err := restored.BuildContext(context.Background())
	require.NoError(t, err)
	first := history[0].(ai.UserMessage)
	require.Contains(t, first.DisplayText, "截图.png")
	require.Equal(t, imageBytes.Bytes(), first.Content[len(first.Content)-1].Image.Data)
	display := serializeRunMessages(history)
	require.NotContains(t, display[0]["content"], "base64")
	info, err := sessionmgr.NewManager(s.app.Config().DataDir).List(context.Background())
	require.NoError(t, err)
	require.Equal(t, "分析图片", info[0].Title)
}

func TestInputBoundariesAndFileContext(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	other, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	attachment := uploadInput(t, s, sess.SessionID(), "note.txt", []byte("context-value"), 201)
	_, err = s.buildPromptMessage(other, "read", promptInputs{Attachments: []string{attachment.ID}})
	require.Error(t, err)
	msg, err := s.buildPromptMessage(sess, "read", promptInputs{Attachments: []string{attachment.ID}})
	require.NoError(t, err)
	require.Contains(t, msg.Content[1].Text, "context-value")
	require.NotContains(t, msg.DisplayText, "context-value")
	for _, name := range []string{"../evil", "..", "a/b", "a\\b"} {
		uploadInput(t, s, sess.SessionID(), name, []byte("x"), 400)
	}
	uploadInput(t, s, sess.SessionID(), "big", bytes.Repeat([]byte("x"), maxAttachmentBytes+1), 400)
	outside := filepath.Join(t.TempDir(), "private.txt")
	require.NoError(t, os.WriteFile(outside, []byte("private"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(sess.Workspace(), "escape.txt")))
	for _, path := range []string{outside, "../outside", "escape.txt"} {
		_, err := s.buildPromptMessage(sess, "read", promptInputs{Files: []fileReference{{Path: path, Workspace: sess.Workspace()}}})
		require.Error(t, err)
	}
	_, err = s.buildPromptMessage(sess, "read", promptInputs{Files: []fileReference{{Path: attachment.Path, Workspace: "another"}}})
	require.Error(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(sess.Workspace(), ".easyagent"), 0700))
	require.NoError(t, os.RemoveAll(filepath.Join(sess.Workspace(), ".easyagent", "attachments")))
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(sess.Workspace(), ".easyagent", "attachments")))
	uploadInput(t, s, sess.SessionID(), "escape.txt", []byte(strings.Repeat("x", 10)), 400)
}
