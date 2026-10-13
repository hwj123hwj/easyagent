package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
)

func TestHistoryImagesUseBoundedSessionReferencesAcrossFork(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	s.SetAPIKey("test-key")
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	// 合法 PNG 后附填充，模拟每张 4 MiB、每轮两张的实际上传边界。
	data := append(encoded.Bytes(), make([]byte, maxAttachmentBytes-encoded.Len())...)
	for turn := 0; turn < 2; turn++ {
		ids := []string{}
		for i := 0; i < 2; i++ {
			attachment := uploadInput(t, s, sess.SessionID(), "image.png", data, 201)
			ids = append(ids, attachment.ID)
		}
		message, err := s.buildPromptMessage(sess, "images", promptInputs{Attachments: ids})
		require.NoError(t, err)
		require.NoError(t, sess.Session().AppendMessage(context.Background(), message))
	}
	snapshot, err := s.runSnapshot(sess.SessionID(), 0, "", nil)
	require.NoError(t, err)
	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.Less(t, len(body), 64*1024, "历史图片本体不能放进 WS 恢复快照")
	images := snapshot.Messages[0]["images"].([]map[string]string)
	require.Empty(t, images[0]["data_url"])
	require.NotEmpty(t, images[0]["attachmentId"])
	require.Equal(t, sess.SessionID(), images[0]["sessionId"])
	read := func(sessionID, attachmentID, token string, status int) []byte {
		r := localReq(http.MethodGet, "/sessions/"+sessionID+"/attachments/"+attachmentID+"/raw?format=data_url", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.Bytes()
	}
	read(sess.SessionID(), images[0]["attachmentId"], "", http.StatusUnauthorized)
	var preview map[string]string
	require.NoError(t, json.Unmarshal(read(sess.SessionID(), images[0]["attachmentId"], "test-key", 200), &preview))
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(data), preview["data_url"])
	other, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	read(other.SessionID(), images[0]["attachmentId"], "test-key", http.StatusNotFound)
	forkID, _, err := sess.Fork(context.Background(), nil)
	require.NoError(t, err)
	fork, err := s.app.LoadSession(context.Background(), forkID)
	require.NoError(t, err)
	history, err := fork.Session().BuildDisplayContext(context.Background())
	require.NoError(t, err)
	refs := serializeSessionContext(fork, history)[0]["images"].([]map[string]string)
	require.Equal(t, forkID, refs[0]["sessionId"])
	read(forkID, refs[0]["attachmentId"], "test-key", 200)
}

func TestHistoryImagesRemainReadableAfterFullCompactionButExcludeSiblingBranch(t *testing.T) {
	s, _, _ := newRunTestServer(t)
	sess, err := s.resolveSession(context.Background(), "")
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, sess.Session().AppendMessage(ctx, ai.NewTextUserMessage("shared root")))
	root, err := sess.Session().Storage().GetLeaf(ctx)
	require.NoError(t, err)
	var pngData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	original := &ai.ImageBlock{MediaType: "image/png", Data: pngData.Bytes()}
	require.NoError(t, sess.Session().AppendMessage(ctx, ai.UserMessage{Content: []ai.ContentBlock{{Type: "image", Image: original}}}))
	messages, err := sess.Session().BuildDisplayContext(ctx)
	require.NoError(t, err)
	baseline := serializeSessionContext(sess, messages)
	// 自动 full compaction 会替换逻辑上下文，但 run 的恢复基线仍显示此前图片。
	require.NoError(t, sess.Session().AppendCompaction(ctx, "summary"))
	compacted, err := sess.Session().BuildDisplayContext(ctx)
	require.NoError(t, err)
	require.Len(t, compacted, 1)
	s.runs.mu.Lock()
	s.runs.sessionLocked(sess.SessionID()).run = &sessionRun{ID: "compacted-run", SessionID: sess.SessionID(), State: "completed", baseline: baseline}
	s.runs.mu.Unlock()
	snapshot, err := s.runSnapshot(sess.SessionID(), 0, "", nil)
	require.NoError(t, err)
	refs := snapshot.Messages[1]["images"].([]map[string]string)
	read := func(imageID string, status int) {
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, localReq(http.MethodGet, "/sessions/"+sess.SessionID()+"/attachments/"+imageID+"/raw", nil))
		require.Equal(t, status, response.Code, response.Body.String())
	}
	read(refs[0]["attachmentId"], http.StatusOK)
	// 同一 JSONL 中的兄弟分支依然保存原图，不能仅按 hash 搜索全部记录。
	require.NoError(t, sess.Session().MoveTo(ctx, root, ""))
	sibling := &ai.ImageBlock{MediaType: "image/png", Data: append(append([]byte{}, original.Data...), 1)}
	require.NoError(t, sess.Session().AppendMessage(ctx, ai.UserMessage{Content: []ai.ContentBlock{{Type: "image", Image: sibling}}}))
	read(refs[0]["attachmentId"], http.StatusNotFound)
	read(historyImageID(sibling), http.StatusOK)
}
