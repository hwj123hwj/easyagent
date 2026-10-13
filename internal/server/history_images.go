package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/ai"
)

const historyImagePrefix = "history_"

func historyImageID(image *ai.ImageBlock) string {
	hash := sha256.New()
	hash.Write([]byte(image.MediaType))
	hash.Write([]byte{0})
	hash.Write(image.Data)
	return historyImagePrefix + hex.EncodeToString(hash.Sum(nil))
}

// 历史保留模型所需的图片本体，客户端投影只携带当前会话范围的引用。
// hash 只定位内容；读取仍要求 API 鉴权，并验证图片属于该会话当前分支。
func (s *Server) getHistoryImage(w http.ResponseWriter, r *http.Request, sessionID, imageID string) {
	if len(imageID) != len(historyImagePrefix)+64 {
		writeError(w, http.StatusBadRequest, "invalid image reference")
		return
	}
	sess, err := s.app.LoadSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	messages, err := sess.Session().BuildDisplayContext(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read image history")
		return
	}
	for _, message := range messages {
		user, ok := message.(ai.UserMessage)
		if !ok {
			continue
		}
		for _, block := range user.Content {
			if block.Type != "image" || block.Image == nil || historyImageID(block.Image) != imageID {
				continue
			}
			mime := block.Image.MediaType
			if len(block.Image.Data) > maxAttachmentBytes || (mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp") {
				writeError(w, http.StatusBadRequest, "invalid or oversized image")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.URL.Query().Get("format") == "data_url" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"data_url": "data:" + block.Image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(block.Image.Data)})
			} else {
				w.Header().Set("Content-Type", block.Image.MediaType)
				http.ServeContent(w, r, "attachment-image", time.Time{}, bytes.NewReader(block.Image.Data))
			}
			return
		}
	}
	writeError(w, http.StatusNotFound, "image not found in this session")
}
