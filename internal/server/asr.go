package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/config"
)

func truncateResp(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	return b[:max]
}

// ASRHandler proxies audio files to SiliconFlow's speech-to-text API.
// This avoids exposing the API key to the frontend.
type ASRHandler struct {
	cfg config.Config
}

func NewASRHandler(cfg config.Config) *ASRHandler {
	return &ASRHandler{cfg: cfg}
}

func (h *ASRHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /asr/transcribe", h.transcribe)
}

type asrResponse struct {
	Text string `json:"text"`
}

func (h *ASRHandler) transcribe(w http.ResponseWriter, r *http.Request) {
	if h.cfg.ASRAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "ASR_API_KEY not configured on server")
		return
	}

	// Parse multipart form (audio file upload, max 25MB)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse audio upload: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no 'file' field in upload")
		return
	}
	defer file.Close()

	// Build SiliconFlow multipart request using a byte buffer
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", h.cfg.ASRModel)

	// Ensure filename has an extension (SiliconFlow may require it)
	filename := header.Filename
	if ext := filepath.Ext(filename); ext == "" {
		filename += extFromMIME(header.Header.Get("Content-Type"))
	}

	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create form file: "+err.Error())
		return
	}
	if _, err := io.Copy(part, file); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to copy audio data: "+err.Error())
		return
	}
	mw.Close()

	// Build upstream request
	baseURL := strings.TrimRight(h.cfg.ASRBaseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	sfReq, err := http.NewRequestWithContext(r.Context(), "POST",
		baseURL+"/audio/transcriptions",
		&buf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create upstream request")
		return
	}
	sfReq.Header.Set("Authorization", "Bearer "+h.cfg.ASRAPIKey)
	sfReq.Header.Set("Content-Type", mw.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(sfReq)
	if err != nil {
		slog.Error("ASR upstream request failed", "error", err)
		writeError(w, http.StatusBadGateway, "speech-to-text service unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Error("ASR upstream returned error",
			"status", resp.StatusCode,
			"body", string(respBody))

		// Parse SiliconFlow error message
		var sfErr struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		errDetail := "speech-to-text service error"
		if json.Unmarshal(respBody, &sfErr) == nil {
			if sfErr.Message != "" {
				errDetail = sfErr.Message
			} else if sfErr.Error != "" {
				errDetail = sfErr.Error
			}
		}
		writeError(w, resp.StatusCode, errDetail)
		return
	}

	// SiliconFlow returns { "text": "..." } or { "data": { "text": "..." } }
	result := asrResponse{}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &raw); err == nil {
		if dataRaw, ok := raw["data"]; ok {
			var inner asrResponse
			if json.Unmarshal(dataRaw, &inner) == nil && inner.Text != "" {
				result.Text = inner.Text
			}
		}
		if result.Text == "" {
			if textRaw, ok := raw["text"]; ok {
				var textStr string
				if json.Unmarshal(textRaw, &textStr) == nil {
					result.Text = textStr
				}
			}
		}
	}

	slog.Info("ASR result",
		"text_len", len(result.Text),
		"audio_mime", header.Header.Get("Content-Type"),
		"audio_size", header.Size,
		"resp_body_preview", string(truncateResp(respBody, 200)))

	result.Text = strings.TrimSpace(result.Text)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// extFromMIME guesses file extension from MIME type for multipart upload.
func extFromMIME(mime string) string {
	switch mime {
	case "audio/webm", "audio/webm;codecs=opus":
		return ".webm"
	case "audio/ogg", "audio/ogg;codecs=opus":
		return ".ogg"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "audio/aac":
		return ".aac"
	case "audio/flac":
		return ".flac"
	default:
		return ".webm"
	}
}
