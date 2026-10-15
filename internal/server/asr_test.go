package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/config"
)

func TestASRGatewayEndpoint(t *testing.T) {
	for _, base := range []string{"", "/v1", "/v1/", "/gateway/v1"} {
		t.Run(base, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := "/v1/audio/transcriptions"
				if base == "/gateway/v1" {
					want = "/gateway/v1/audio/transcriptions"
				}
				if r.URL.Path != want || r.Header.Get("Authorization") != "Bearer qa-only" {
					t.Error("wrong endpoint/credential", r.URL.Path)
				}
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
				}
				defer r.MultipartForm.RemoveAll()
				if r.FormValue("model") != "qa-asr" {
					t.Error("lost model")
				}
				io.WriteString(w, `{"text":"recognized"}`)
			}))
			defer upstream.Close()
			var b bytes.Buffer
			m := multipart.NewWriter(&b)
			part, _ := m.CreateFormFile("file", "qa.webm")
			part.Write([]byte("test audio"))
			m.Close()
			r := httptest.NewRequest("POST", "/asr/transcribe", &b)
			r.Header.Set("Content-Type", m.FormDataContentType())
			w := httptest.NewRecorder()
			NewASRHandler(config.Config{ASRAPIKey: "qa-only", ASRBaseURL: upstream.URL + base, ASRModel: "qa-asr"}).transcribe(w, r)
			if w.Code != 200 || w.Body.String() != "{\"text\":\"recognized\"}\n" {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
