package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/skillmarket"
)

// ── Skill marketplace REST endpoints ────────────────────────────────────────
//
// Read-only browsing proxies the official store; install/uninstall mutate the
// local personal skills directory. Installs run as background jobs whose
// progress streams to clients via SSE (GET /skills/market/jobs/{id}/events).
// The agent-facing skill_market tool shares the same client (and the same
// revision-bump reload mechanism).

type marketSkillJSON struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	Version      string   `json:"version"`
	InstallCount int      `json:"install_count"`
	Featured     bool     `json:"featured"`
	SizeBytes    int64    `json:"size_bytes"`
	SHA256       *string  `json:"sha256"`
	SectionID    *int     `json:"section_id"`
	SectionName  *string  `json:"section_name"`
	IconURL      *string  `json:"icon_url"`
	// Showcase fields for the detail view; slices are always non-nil so
	// clients can iterate without nil checks.
	RichDescription   *string                         `json:"rich_description"`
	PreviewImages     []string                        `json:"preview_images"`
	PreviewThumbnails []*skillmarket.PreviewThumbnail `json:"preview_thumbnails"`
	UsageExample      *string                         `json:"usage_example"`
	ExampleFiles      []skillmarket.ExampleFile       `json:"example_files"`
}

func toMarketSkillJSON(s skillmarket.SkillItem) marketSkillJSON {
	if s.Tags == nil {
		s.Tags = []string{}
	}
	if s.PreviewImages == nil {
		s.PreviewImages = []string{}
	}
	if s.PreviewThumbnails == nil {
		s.PreviewThumbnails = []*skillmarket.PreviewThumbnail{}
	}
	if s.ExampleFiles == nil {
		s.ExampleFiles = []skillmarket.ExampleFile{}
	}
	return marketSkillJSON{
		ID:                s.ID,
		Name:              s.Name,
		DisplayName:       s.DisplayName,
		Description:       s.Description,
		Category:          s.Category,
		Tags:              s.Tags,
		Version:           s.Version,
		InstallCount:      s.InstallCount,
		Featured:          s.Featured,
		SizeBytes:         s.TarballSize,
		SHA256:            s.SHA256,
		SectionID:         s.SectionID,
		SectionName:       s.SectionName,
		IconURL:           s.IconURL,
		RichDescription:   s.RichDescription,
		PreviewImages:     s.PreviewImages,
		PreviewThumbnails: s.PreviewThumbnails,
		UsageExample:      s.UsageExample,
		ExampleFiles:      s.ExampleFiles,
	}
}

// registerSkillMarketRoutes adds skill-marketplace endpoints to the mux.
func (s *Server) registerSkillMarketRoutes(mux *http.ServeMux) {
	if s.app.SkillMarket() == nil {
		return // disabled via EA_SKILL_MARKET=0
	}
	// s.marketJobs is created once in New(); never here (Handler() rebuilds
	// the mux per request and must not reset job state).
	if s.marketJobs == nil {
		s.marketJobs = newMarketJobRegistry(s.ctx, s.app)
	}
	mux.HandleFunc("GET /skills/market/search", s.marketSearch)
	mux.HandleFunc("GET /skills/market/sections", s.marketSections)
	mux.HandleFunc("GET /skills/market/skills/{id}", s.marketDetail)
	mux.HandleFunc("GET /skills/market/installed", s.marketInstalled)
	mux.HandleFunc("POST /skills/market/install", s.marketInstall)
	mux.HandleFunc("GET /skills/market/jobs/{id}", s.marketJobStatus)
	mux.HandleFunc("GET /skills/market/jobs/{id}/events", s.marketJobEvents)
	mux.HandleFunc("POST /skills/market/jobs/{id}/cancel", s.marketJobCancel)
	mux.HandleFunc("DELETE /skills/market/installed/{name}", s.marketUninstall)
}

func (s *Server) marketClient() *skillmarket.Client {
	return s.app.SkillMarket()
}

func (s *Server) marketSearch(w http.ResponseWriter, r *http.Request) {
	q := skillmarket.BrowseQuery{
		Category: r.URL.Query().Get("category"),
		Sort:     r.URL.Query().Get("sort"),
		Query:    r.URL.Query().Get("q"),
		Locale:   r.URL.Query().Get("locale"),
	}
	if page, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && page > 0 {
		q.Page = page
	}
	if section, err := strconv.Atoi(r.URL.Query().Get("section")); err == nil && section > 0 {
		q.Section = section
	}
	result, err := s.marketClient().Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusBadGateway, "marketplace search failed: "+err.Error())
		return
	}
	skills := make([]marketSkillJSON, 0, len(result.Skills))
	for _, item := range result.Skills {
		skills = append(skills, toMarketSkillJSON(item))
	}
	writeJSON(w, map[string]any{"skills": skills, "total": result.Total, "page": result.Page})
}

func (s *Server) marketSections(w http.ResponseWriter, r *http.Request) {
	sections, err := s.marketClient().Sections(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "marketplace sections failed: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"sections": sections})
}

func (s *Server) marketDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid skill id")
		return
	}
	item, err := s.marketClient().Detail(r.Context(), id, r.URL.Query().Get("locale"))
	if errors.Is(err, skillmarket.ErrNotFound) {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "marketplace detail failed: "+err.Error())
		return
	}
	writeJSON(w, toMarketSkillJSON(item))
}

func (s *Server) marketInstalled(w http.ResponseWriter, r *http.Request) {
	installed, err := s.marketClient().ListInstalled()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installed skills: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"installed": installed, "skills_dir": s.marketClient().SkillsDir()})
}

type marketInstallRequest struct {
	ID int `json:"id"`
}

// marketInstall starts a background install job and returns its id. Progress
// streams via SSE at /skills/market/jobs/{id}/events.
func (s *Server) marketInstall(w http.ResponseWriter, r *http.Request) {
	var req marketInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	jobID, err := s.marketJobs.Start(req.ID)
	if err != nil {
		if strings.Contains(err.Error(), "already running") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"job_id":     jobID,
		"skill_id":   req.ID,
		"state":      "running",
		"events_url": fmt.Sprintf("/skills/market/jobs/%s/events", jobID),
	})
}

// marketJobStatus polls one install job (fallback for clients without SSE).
func (s *Server) marketJobStatus(w http.ResponseWriter, r *http.Request) {
	job, ok := s.marketJobs.Snapshot(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "install job not found")
		return
	}
	writeJSON(w, job)
}

// marketJobCancel aborts a running install.
func (s *Server) marketJobCancel(w http.ResponseWriter, r *http.Request) {
	if !s.marketJobs.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusConflict, "job not running or not found")
		return
	}
	writeJSON(w, map[string]any{"cancelled": true})
}

// marketJobEvents streams install progress as server-sent events:
//
//	event: progress
//	data: {"job_id":"inst-...","state":"running","phase":"downloading","bytes_done":...,"bytes_total":...}
//
//	event: done
//	data: {"job_id":"inst-...","state":"succeeded",...}
//
// Subscribing mid-flight replays the current snapshot (or the terminal event)
// as the first message, so a page refresh never loses the outcome.
func (s *Server) marketJobEvents(w http.ResponseWriter, r *http.Request) {
	events, cancel, err := s.marketJobs.Subscribe(r.PathValue("id"), 64)
	if errors.Is(err, ErrJobNotFound) {
		writeError(w, http.StatusNotFound, "install job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case evt, ok := <-events:
			if !ok {
				return
			}
			name := "progress"
			if evt.State == "succeeded" || evt.State == "failed" {
				name = "done"
			}
			data, err := json.Marshal(evt)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
				return
			}
			flusher.Flush()
			if evt.State == "succeeded" || evt.State == "failed" {
				return
			}
		}
	}
}

func (s *Server) marketUninstall(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing skill name")
		return
	}
	if err := s.marketClient().Uninstall(name); err != nil {
		switch {
		case errors.Is(err, skillmarket.ErrNotMarketplace):
			writeError(w, http.StatusConflict, "not a marketplace-installed skill; refusing to remove")
		case errors.Is(err, skillmarket.ErrNotFound):
			writeError(w, http.StatusNotFound, "skill not installed")
		default:
			writeError(w, http.StatusInternalServerError, "uninstall failed: "+err.Error())
		}
		return
	}
	s.app.BumpSkillMarketRevision()
	writeJSON(w, map[string]any{"uninstalled": true, "name": name})
}
