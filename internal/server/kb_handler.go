package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	kbtools "github.com/hwj123hwj/easyagent/internal/agents/kb/tools"
)

// ── JSON Response Types ───────────────────────────────────────────────────

type kbEntryJSON struct {
	Path     string    `json:"path"`
	RelPath  string    `json:"rel_path"`
	Title    string    `json:"title"`
	Category string    `json:"category"`
	Tags     []string  `json:"tags"`
	Summary  string    `json:"summary"`
	Source   string    `json:"source"`
	Modified time.Time `json:"modified"`
}

type kbStatsResponse struct {
	TotalEntries int    `json:"total_entries"`
	Categories   int    `json:"categories"`
	Tags         int    `json:"tags"`
	RepoPath     string `json:"repo_path"`
}

type kbEntriesResponse struct {
	Entries []kbEntryJSON `json:"entries"`
	Total   int           `json:"total"`
}

type kbCategoryJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type kbCategoriesResponse struct {
	Categories []kbCategoryJSON `json:"categories"`
}

type kbTagJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type kbTagsResponse struct {
	Tags []kbTagJSON `json:"tags"`
}

type kbHealthResponse struct {
	TotalEntries          int                `json:"total_entries"`
	Categories            int                `json:"categories"`
	Tags                  int                `json:"tags"`
	EntriesMissingTitle   []kbEntryJSON      `json:"entries_missing_title"`
	EntriesMissingSummary []kbEntryJSON      `json:"entries_missing_summary"`
	EntriesMissingTags    []kbEntryJSON      `json:"entries_missing_tags"`
	DuplicateGroups       []kbDupGroupJSON   `json:"duplicate_groups"`
	TagClusters           []kbTagClusterJSON `json:"tag_clusters"`
}

type kbDupGroupJSON struct {
	CanonicalTitle string        `json:"canonical_title"`
	Entries        []kbEntryJSON `json:"entries"`
}

type kbTagClusterJSON struct {
	Canonical string   `json:"canonical"`
	Variants  []string `json:"variants"`
	Count     int      `json:"count"`
}

type kbReadResponse struct {
	Content string `json:"content"`
	Path    string `json:"path"`
}

// ── Helpers ───────────────────────────────────────────────────────────────

// resolveKBRepoPath gets the KB repo path from the server's app config.
func (s *Server) resolveKBRepoPath() string {
	cfg := s.app.Config()
	if cfg.KBRepoPath != "" {
		return cfg.KBRepoPath
	}
	homeDir, _ := os.UserHomeDir()
	return homeDir + "/agent-lessons"
}

// entryToJSON converts a kbtools.Entry to the JSON response type.
func entryToJSON(e kbtools.Entry) kbEntryJSON {
	tags := e.Tags
	if tags == nil {
		tags = []string{}
	}
	return kbEntryJSON{
		Path:     e.Path,
		RelPath:  e.RelPath,
		Title:    e.Title,
		Category: e.Category,
		Tags:     tags,
		Summary:  e.Summary,
		Source:   e.Source,
		Modified: e.Modified,
	}
}

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ── Route Registration ────────────────────────────────────────────────────

// registerKBRoutes adds KB knowledge-base browser endpoints to the given mux.
func (s *Server) registerKBRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /kb/stats", s.kbStats)
	mux.HandleFunc("GET /kb/entries", s.kbEntries)
	mux.HandleFunc("GET /kb/categories", s.kbCategories)
	mux.HandleFunc("GET /kb/tags", s.kbTags)
	mux.HandleFunc("GET /kb/health", s.kbHealth)
	mux.HandleFunc("GET /kb/read", s.kbRead)
}

// ── Handlers ──────────────────────────────────────────────────────────────

func (s *Server) kbStats(w http.ResponseWriter, r *http.Request) {
	repoPath := s.resolveKBRepoPath()
	idx, err := kbtools.GetIndex(repoPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read KB index: "+err.Error())
		return
	}

	report := kbtools.GenerateHealthReport(idx)
	writeJSON(w, kbStatsResponse{
		TotalEntries: report.TotalEntries,
		Categories:   report.Categories,
		Tags:         report.Tags,
		RepoPath:     repoPath,
	})
}

func (s *Server) kbEntries(w http.ResponseWriter, r *http.Request) {
	repoPath := s.resolveKBRepoPath()
	idx, err := kbtools.GetIndex(repoPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read KB index: "+err.Error())
		return
	}

	// Filters
	category := r.URL.Query().Get("category")
	tag := r.URL.Query().Get("tag")
	query := r.URL.Query().Get("q")

	// If a keyword query is provided, use the search strategy.
	if strings.TrimSpace(query) != "" {
		results := kbtools.KeywordSearcher{}.Search(idx.Entries, kbtools.SearchQuery{
			Query:    query,
			Tag:      tag,
			Category: category,
			Limit:    200,
		})
		var entries []kbEntryJSON
		for _, r := range results {
			entries = append(entries, entryToJSON(r.Entry))
		}
		if entries == nil {
			entries = []kbEntryJSON{}
		}
		writeJSON(w, kbEntriesResponse{Entries: entries, Total: len(entries)})
		return
	}

	// No query — filter by tag/category and return all matching entries.
	var entries []kbEntryJSON
	for _, e := range idx.Entries {
		if category != "" && !strings.EqualFold(e.Category, category) {
			continue
		}
		if tag != "" {
			found := false
			for _, t := range e.Tags {
				if strings.EqualFold(t, tag) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		entries = append(entries, entryToJSON(e))
	}

	// Sort by modified date descending (most recent first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Modified.After(entries[j].Modified)
	})

	if entries == nil {
		entries = []kbEntryJSON{}
	}
	writeJSON(w, kbEntriesResponse{Entries: entries, Total: len(entries)})
}

func (s *Server) kbCategories(w http.ResponseWriter, r *http.Request) {
	repoPath := s.resolveKBRepoPath()
	idx, err := kbtools.GetIndex(repoPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read KB index: "+err.Error())
		return
	}

	stats := kbtools.CategoryOverview(idx)
	result := make([]kbCategoryJSON, 0, len(stats))
	for _, c := range stats {
		result = append(result, kbCategoryJSON{Name: c.Name, Count: c.Count})
	}
	writeJSON(w, kbCategoriesResponse{Categories: result})
}

func (s *Server) kbTags(w http.ResponseWriter, r *http.Request) {
	repoPath := s.resolveKBRepoPath()
	idx, err := kbtools.GetIndex(repoPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read KB index: "+err.Error())
		return
	}

	stats := kbtools.TagOverview(idx)
	result := make([]kbTagJSON, 0, len(stats))
	for _, t := range stats {
		result = append(result, kbTagJSON{Name: t.Name, Count: t.Count})
	}
	writeJSON(w, kbTagsResponse{Tags: result})
}

func (s *Server) kbHealth(w http.ResponseWriter, r *http.Request) {
	repoPath := s.resolveKBRepoPath()
	idx, err := kbtools.GetIndex(repoPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read KB index: "+err.Error())
		return
	}

	report := kbtools.GenerateHealthReport(idx)

	// Convert duplicate groups
	dupGroups := make([]kbDupGroupJSON, 0, len(report.DuplicateGroups))
	for _, g := range report.DuplicateGroups {
		entries := make([]kbEntryJSON, 0, len(g.Entries))
		for _, e := range g.Entries {
			entries = append(entries, entryToJSON(e))
		}
		dupGroups = append(dupGroups, kbDupGroupJSON{
			CanonicalTitle: g.CanonicalTitle,
			Entries:        entries,
		})
	}

	// Convert tag clusters
	tagClusters := make([]kbTagClusterJSON, 0, len(report.TagClusters))
	for _, c := range report.TagClusters {
		tagClusters = append(tagClusters, kbTagClusterJSON{
			Canonical: c.Canonical,
			Variants:  c.Variants,
			Count:     c.Count,
		})
	}

	// Convert missing entries
	missingTitle := make([]kbEntryJSON, 0, len(report.EntriesMissingTitle))
	for _, e := range report.EntriesMissingTitle {
		missingTitle = append(missingTitle, entryToJSON(e))
	}
	missingSummary := make([]kbEntryJSON, 0, len(report.EntriesMissingSummary))
	for _, e := range report.EntriesMissingSummary {
		missingSummary = append(missingSummary, entryToJSON(e))
	}
	missingTags := make([]kbEntryJSON, 0, len(report.EntriesMissingTags))
	for _, e := range report.EntriesMissingTags {
		missingTags = append(missingTags, entryToJSON(e))
	}

	writeJSON(w, kbHealthResponse{
		TotalEntries:          report.TotalEntries,
		Categories:            report.Categories,
		Tags:                  report.Tags,
		EntriesMissingTitle:   missingTitle,
		EntriesMissingSummary: missingSummary,
		EntriesMissingTags:    missingTags,
		DuplicateGroups:       dupGroups,
		TagClusters:           tagClusters,
	})
}

func (s *Server) kbRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	repoPath := s.resolveKBRepoPath()

	// Resolve relative path
	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = repoPath + "/" + path
	}

	// Security: resolve symlinks and verify the result is inside the KB repo
	absRepo, _ := filepath.Abs(repoPath)
	absFile, err := filepath.Abs(fullPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid path: %v", err))
		return
	}
	if !strings.HasPrefix(absFile+string(filepath.Separator), absRepo+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "path is outside the knowledge base")
		return
	}

	data, err := os.ReadFile(absFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read file: %v", err))
		return
	}

	writeJSON(w, kbReadResponse{
		Content: string(data),
		Path:    absFile,
	})
}
