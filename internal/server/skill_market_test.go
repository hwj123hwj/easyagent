package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/skillmarket"
	"github.com/stretchr/testify/require"
)

// skillMarketTestServer builds a Server whose marketplace points at a fake
// store serving one installable skill, with skills installed into a temp dir.
func skillMarketTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	file, err := zw.Create("rest-demo/SKILL.md")
	require.NoError(t, err)
	_, err = file.Write([]byte("---\nname: rest-demo\ndescription: rest\n---\n"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	digest := sha256.Sum256(buf.Bytes())

	item := skillmarket.SkillItem{
		ID:          42,
		Name:        "rest-demo",
		DisplayName: "REST Demo",
		Description: "Installed via REST",
		Version:     "1.2.3",
		SHA256:      &[]string{hex.EncodeToString(digest[:])}[0],
		TarballSize: int64(buf.Len()),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": []skillmarket.SkillItem{item}, "total": 1, "page": 1})
	})
	mux.HandleFunc("GET /api/skills/42", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(item)
	})
	mux.HandleFunc("GET /api/skills/99", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sections": []skillmarket.Section{{ID: 1, Name: "G"}}})
	})
	mux.HandleFunc("GET /api/skills/42/download", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	})
	store := httptest.NewServer(mux)
	t.Cleanup(store.Close)

	ws := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIBaseURL = "http://localhost:4001"
	cfg.Workspace = ws
	cfg.SkillMarketBaseURL = store.URL
	cfg.SkillMarketSkillDir = t.TempDir()
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { application.Close() })
	return New(application, nil), cfg.SkillMarketSkillDir
}

func marketReq(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := localReq(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestSkillMarketRESTDisabledOmitsRoutes(t *testing.T) {
	ws := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIBaseURL = "http://localhost:4001"
	cfg.Workspace = ws
	cfg.SkillMarketEnabled = false
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { application.Close() })
	srv := New(application, nil)

	w := marketReq(t, srv, "GET", "/skills/market/search", nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillMarketRESTSearchDetailSections(t *testing.T) {
	srv, _ := skillMarketTestServer(t)

	w := marketReq(t, srv, "GET", "/skills/market/search?q=demo", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var search struct {
		Skills []map[string]any `json:"skills"`
		Total  int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &search))
	require.Equal(t, 1, search.Total)
	require.Equal(t, float64(42), search.Skills[0]["id"])
	require.Equal(t, "REST Demo", search.Skills[0]["display_name"])

	w = marketReq(t, srv, "GET", "/skills/market/skills/42", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"version":"1.2.3"`)

	w = marketReq(t, srv, "GET", "/skills/market/skills/99", nil)
	require.Equal(t, http.StatusNotFound, w.Code)

	w = marketReq(t, srv, "GET", "/skills/market/sections", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "sections")

	w = marketReq(t, srv, "GET", "/skills/market/skills/not-a-number", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSkillMarketRESTInstallAndUninstallLifecycle(t *testing.T) {
	srv, skillsDir := skillMarketTestServer(t)

	// Install starts a background job.
	w := marketReq(t, srv, "POST", "/skills/market/install", map[string]any{"id": 42})
	require.Equal(t, http.StatusOK, w.Code)
	var startResp struct {
		JobID     string `json:"job_id"`
		EventsURL string `json:"events_url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &startResp))
	require.NotEmpty(t, startResp.JobID)
	require.Contains(t, startResp.EventsURL, startResp.JobID)

	// Wait for the terminal SSE event via the polling fallback.
	var finalState string
	for i := 0; i < 100; i++ {
		w = marketReq(t, srv, "GET", "/skills/market/jobs/"+startResp.JobID, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var snap struct {
			State     string `json:"state"`
			SkillName string `json:"skill_name"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
		if snap.State != "running" {
			finalState = snap.State
			require.Equal(t, "rest-demo", snap.SkillName)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Equal(t, "succeeded", finalState)

	_, err := os.Stat(filepath.Join(skillsDir, "rest-demo", "rest-demo", "SKILL.md"))
	require.NoError(t, err)

	// Duplicate install while first is running conflicts; after completion a
	// second Start also fails fast at the registry level (running check only
	// matches running jobs) but Install itself returns ErrAlreadyInstalled,
	// which lands in the job as a failure.
	w = marketReq(t, srv, "POST", "/skills/market/install", map[string]any{"id": 42})
	require.Equal(t, http.StatusOK, w.Code)
	var dup struct {
		JobID string `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dup))
	for i := 0; i < 100; i++ {
		w = marketReq(t, srv, "GET", "/skills/market/jobs/"+dup.JobID, nil)
		var snap struct {
			State string `json:"state"`
			Error string `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
		if snap.State != "running" {
			require.Equal(t, "failed", snap.State)
			require.Contains(t, snap.Error, "already exists")
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Listed as installed.
	w = marketReq(t, srv, "GET", "/skills/market/installed", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "rest-demo")

	// Uninstall removes it.
	w = marketReq(t, srv, "DELETE", "/skills/market/installed/rest-demo", nil)
	require.Equal(t, http.StatusOK, w.Code)
	_, err = os.Stat(filepath.Join(skillsDir, "rest-demo"))
	require.True(t, os.IsNotExist(err))

	// Uninstalling again is 404.
	w = marketReq(t, srv, "DELETE", "/skills/market/installed/rest-demo", nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillMarketRESTInstallEventsSSE(t *testing.T) {
	srv, _ := skillMarketTestServer(t)

	w := marketReq(t, srv, "POST", "/skills/market/install", map[string]any{"id": 42})
	require.Equal(t, http.StatusOK, w.Code)
	var start struct {
		JobID string `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &start))

	// Consume the SSE stream until the terminal event.
	req := localReq("GET", "/skills/market/jobs/"+start.JobID+"/events", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	var body string
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case <-deadline:
			t.Fatal("SSE stream did not terminate")
		case <-done:
			body = rec.Body.String()
			break loop
		}
	}
	// Fast installs may finish before subscribing; terminal replay is valid.
	require.Contains(t, body, "event: done")
	require.Contains(t, body, `"state":"succeeded"`)

	// Subscribing after completion replays the terminal event immediately.
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, localReq("GET", "/skills/market/jobs/"+start.JobID+"/events", nil))
	require.Contains(t, rec2.Body.String(), `"state":"succeeded"`)

	// Unknown job id is 404.
	w = marketReq(t, srv, "GET", "/skills/market/jobs/does-not-exist/events", nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillMarketRESTUninstallRefusesHandWrittenSkill(t *testing.T) {
	srv, skillsDir := skillMarketTestServer(t)

	handDir := filepath.Join(skillsDir, "hand-written")
	require.NoError(t, os.MkdirAll(handDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(handDir, "SKILL.md"), []byte("mine"), 0o644))

	w := marketReq(t, srv, "DELETE", "/skills/market/installed/hand-written", nil)
	require.Equal(t, http.StatusConflict, w.Code)
	_, err := os.Stat(handDir)
	require.NoError(t, err, "hand-written skill must survive")
}

func TestSkillMarketRESTInstallValidation(t *testing.T) {
	srv, _ := skillMarketTestServer(t)

	w := marketReq(t, srv, "POST", "/skills/market/install", map[string]any{"id": -1})
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Unknown skill starts a job that fails asynchronously; verify via status.
	w = marketReq(t, srv, "POST", "/skills/market/install", map[string]any{"id": 99})
	require.Equal(t, http.StatusOK, w.Code)
	var start struct {
		JobID string `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &start))
	for i := 0; i < 100; i++ {
		w = marketReq(t, srv, "GET", "/skills/market/jobs/"+start.JobID, nil)
		var snap struct {
			State string `json:"state"`
			Error string `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
		if snap.State != "running" {
			require.Equal(t, "failed", snap.State)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A slow client must always be able to observe the terminal state.
func TestMarketSubscriberRetainsTerminalWhenBufferFull(t *testing.T) {
	srv, _ := skillMarketTestServer(t)
	job := &marketJob{ID: "slow", State: "running", subs: map[chan marketEvent]struct{}{}}
	srv.marketJobs.jobs[job.ID] = job
	events, cancel, err := srv.marketJobs.Subscribe(job.ID, 64)
	require.NoError(t, err)
	defer cancel()
	for range 100 {
		srv.marketJobs.publish(job, marketEvent{JobID: job.ID, State: "running"})
	}
	srv.marketJobs.publish(job, marketEvent{JobID: job.ID, State: "succeeded"})
	found := false
	for len(events) > 0 {
		if (<-events).State == "succeeded" {
			found = true
		}
	}
	require.True(t, found)
}

func TestMarketCustomDirectoryReloadsExistingAndNewSessions(t *testing.T) {
	srv, _ := skillMarketTestServer(t)
	sess, err := srv.app.NewSession(t.Context())
	require.NoError(t, err)
	_, err = srv.app.SkillMarket().Install(t.Context(), 42)
	require.NoError(t, err)
	srv.app.BumpSkillMarketRevision()
	require.NoError(t, sess.RefreshTools(t.Context()))
	found := func() bool {
		for _, s := range sess.LoadedSkills() {
			if s.Name == "rest-demo" {
				return true
			}
		}
		return false
	}
	require.True(t, found(), "custom marketplace root must be loaded")
	newer, err := srv.app.NewSession(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, newer.LoadedSkills())
	require.NoError(t, srv.app.SkillMarket().Uninstall("rest-demo"))
	srv.app.BumpSkillMarketRevision()
	require.NoError(t, sess.RefreshTools(t.Context()))
	require.False(t, found(), "uninstalled skill must disappear after reload")
}
