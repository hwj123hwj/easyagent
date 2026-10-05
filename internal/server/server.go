package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/internal/scheduler"
	"github.com/hwj123hwj/easyagent/internal/web"
	"github.com/hwj123hwj/easyagent/sdk/agent"
	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/hwj123hwj/easyagent/sdk/workflow"
)

// Version is the server build version. Set by main.go via SetVersion().
var Version = "dev"

// SetVersion updates the server's build version (called from main).
func (s *Server) SetVersion(v string) {
	Version = v
}

// Server provides HTTP REST + SSE endpoints for the agent.
// It routes requests to AgentSessions via the App's SessionRegistry.
type Server struct {
	activity        activityGate
	runs            *runRegistry
	queueMu         sync.Mutex
	queues          map[string]*messageQueue
	ctx             context.Context
	cancel          context.CancelFunc
	app             *app.App
	slashCmds       *slashcmd.Registry
	externalTools   []agent.ExternalToolDef
	toolMu          sync.Mutex
	extraRoutes     *http.ServeMux // optional extra routes (e.g. music audio proxy)
	apiKey          string         // if non-empty, requires Bearer token auth on all endpoints
	terminalEnabled bool
	terminalSlots   chan struct{}

	wfMu      sync.Mutex
	wfReg     *workflow.Registry
	wfFactory workflow.RunnerFactory

	allowNoAuth    bool     // EA_ALLOW_NO_AUTH=1：未配 key 时完全开放（调试用）
	allowedOrigins []string // EA_ALLOWED_ORIGINS：显式 CORS 白名单；空 = 不返回 CORS 头
}

// SetExtraRoutes sets an additional ServeMux to be merged into the server's routes.
func (s *Server) SetExtraRoutes(mux *http.ServeMux) {
	s.extraRoutes = mux
}

// SetAPIKey enables Bearer token authentication on all REST endpoints.
// If set, all requests must include "Authorization: Bearer <apiKey>" header.
// The /health endpoint is always accessible without auth (for health checks).
func (s *Server) SetAPIKey(key string) {
	s.apiKey = key
}

// authMiddleware validates access on all REST endpoints.
// 详见 auth.go 顶部的访问控制模型说明。/health 始终开放。
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health check endpoint (always open for monitoring)
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		if !s.authorized(r) {
			if s.apiKey != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(ErrorResponse{
					Error: "unauthorized: set Authorization: Bearer <EA_API_KEY>",
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(ErrorResponse{
				Error: "unauthorized: non-loopback access requires EA_API_KEY " +
					"(or EA_ALLOW_NO_AUTH=1 for open access)",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ChatRequest is the request body for chat endpoints.
type ChatRequest struct {
	Inputs    promptInputs `json:"inputs,omitempty"`
	Prompt    string       `json:"prompt"`
	SessionID string       `json:"session_id,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

// ChatResponse is the response for non-streaming chat.
type ChatResponse struct {
	Text      string        `json:"text"`
	ToolCalls []ai.ToolCall `json:"tool_calls,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
}

// SessionResponse is the response for session metadata.
type SessionResponse struct {
	ID           string `json:"id"`
	CreatedAt    int64  `json:"created_at"`
	MessageCount int    `json:"message_count"`
}

// ErrorResponse is the standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// New creates a new Server backed by the given App and slash command registry.
// It also wires the LoopManager's trigger resolver so /loop can inject prompts.
func New(application *app.App, slashCmds *slashcmd.Registry) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{app: application, slashCmds: slashCmds, ctx: ctx, cancel: cancel, runs: newRunRegistry(),
		terminalEnabled: os.Getenv("EA_ENABLE_TERMINAL") == "1", terminalSlots: make(chan struct{}, 8),
		allowedOrigins: envAllowedOrigins(), allowNoAuth: envAllowNoAuth()}
	srv.restoreRunReceipts()

	// Wire loop trigger: when a /loop fires, inject the prompt into the target session
	if application.LoopManager() != nil {
		application.LoopManager().SetTriggerResolver(func(sessionID string) scheduler.TriggerFunc {
			return func(ctx context.Context, prompt string) error {
				return srv.injectLoopPrompt(ctx, sessionID, prompt)
			}
		})
	}

	return srv
}

// injectLoopPrompt injects a prompt into a session as a background agent turn.
// Used by the /loop scheduler to fire recurring prompts.
func (s *Server) injectLoopPrompt(ctx context.Context, sessionID, prompt string) error {
	run, _, err := s.startRun(sessionID, prompt, "")
	if err != nil {
		return err
	}
	_, err = s.waitRun(ctx, run)
	return err
}

// Handler returns the HTTP handler with all routes and middleware.
func (s *Server) Handler() http.Handler {
	// REST API routes with middleware
	restMux := http.NewServeMux()
	restMux.HandleFunc("GET /health", s.health)
	restMux.HandleFunc("/admin/deploy", s.deploymentControl)
	restMux.HandleFunc("POST /chat", s.chat)
	restMux.HandleFunc("POST /chat/stream", s.chatStream)
	restMux.HandleFunc("GET /sessions", s.listSessions)
	restMux.HandleFunc("POST /sessions", s.createSession)
	restMux.HandleFunc("GET /sessions/{id}/messages", s.getSessionMessages)
	restMux.HandleFunc("GET /sessions/{id}/info", s.getSessionInfo)
	restMux.HandleFunc("POST /sessions/{id}/permissions", s.setSessionPermissions)
	restMux.HandleFunc("GET /sessions/{id}/run", s.getRun)
	restMux.HandleFunc("POST /sessions/{id}/run/cancel", s.cancelRunHTTP)
	restMux.HandleFunc("POST /sessions/{id}/run/confirm", s.confirmRunHTTP)
	restMux.HandleFunc("DELETE /sessions/{id}", s.deleteSession)
	restMux.HandleFunc("PATCH /sessions/{id}", s.patchSession)
	restMux.HandleFunc("GET /sessions/{id}/queue", s.messageQueueHTTP)
	restMux.HandleFunc("POST /sessions/{id}/queue", s.messageQueueHTTP)
	restMux.HandleFunc("POST /sessions/{id}/attachments", s.uploadAttachment)
	restMux.HandleFunc("POST /sessions/{id}/model", s.switchModel)
	restMux.HandleFunc("GET /models", s.listModels)
	restMux.HandleFunc("GET /tools", s.listTools)
	restMux.HandleFunc("GET /commands", s.listCommands)
	restMux.HandleFunc("GET /applications", s.listApplications)
	restMux.HandleFunc("POST /sessions/{id}/compact", s.compactSession)
	restMux.HandleFunc("POST /sessions/{id}/command", s.executeCommand)
	restMux.HandleFunc("POST /tools/register", s.registerTool)
	restMux.HandleFunc("GET /sessions/{id}/diff", s.getSessionDiff)
	restMux.HandleFunc("GET /sessions/{id}/runs/{run}/files", s.getRunFiles)
	restMux.HandleFunc("POST /sessions/{id}/runs/{run}/undo", s.undoRunFile)
	restMux.HandleFunc("GET /workspace/download", s.downloadWorkspaceFile)
	restMux.HandleFunc("GET /workspace/file-data", s.workspaceFileData)
	restMux.HandleFunc("GET /sessions/{id}/run-files", s.listRunFiles)
	restMux.HandleFunc("GET /sessions/{id}/capabilities", s.getCapabilities)
	restMux.HandleFunc("GET /sessions/{id}/file", s.getSessionFile)
	restMux.HandleFunc("PUT /sessions/{id}/file", s.writeFile)
	restMux.HandleFunc("GET /workspace/list-dir", s.listDir)
	restMux.HandleFunc("GET /workspace/search-files", s.searchFiles)
	restMux.HandleFunc("GET /workspace/read-file", s.workspaceReadFile)
	restMux.HandleFunc("GET /workspace/read-file-base64", s.workspaceReadFileBase64)
	restMux.HandleFunc("PUT /workspace/write-file", s.workspaceWriteFile)

	// Knowledge base browser endpoints
	s.registerKBRoutes(restMux)

	// Workflow orchestration endpoints
	s.registerWorkflowRoutes(restMux)
	s.registerDynamicWorkflowRoutes(restMux)

	s.registerFeishuSettings(restMux)
	s.registerMCPRoutes(restMux)

	// User profile endpoints
	s.registerProfileRoutes(restMux)

	// ASR (speech-to-text) endpoint
	NewASRHandler(s.app.Config()).Register(restMux)

	var restHandler http.Handler = s.admissionMiddleware(restMux)
	restHandler = s.authMiddleware(restHandler)
	restHandler = corsMiddleware(s)(restHandler)
	restHandler = recoveryMiddleware(restHandler)
	restHandler = loggingMiddleware(restHandler)

	// WebSocket route — bypasses all middleware to avoid Hijack issues;
	// 鉴权在 handleWebSocket 升级前自行校验
	wsHandler := corsMiddleware(s)(http.HandlerFunc(s.handleWebSocket))

	// Top-level mux: combines REST API + Web UI + WebSocket
	topMux := http.NewServeMux()
	topMux.Handle("GET /ws", wsHandler)
	topMux.Handle("GET /terminal", http.HandlerFunc(s.handleTerminal))

	// Register REST API routes (these take precedence over "/" catch-all)
	topMux.Handle("/health", restHandler)
	topMux.Handle("/admin/deploy", restHandler)
	topMux.Handle("/chat", restHandler)
	topMux.Handle("/chat/", restHandler)
	topMux.Handle("/sessions", restHandler)
	topMux.Handle("/sessions/", restHandler)
	topMux.Handle("/models", restHandler)
	topMux.Handle("/tools", restHandler)
	topMux.Handle("/commands", restHandler)
	topMux.Handle("/mcp", restHandler)
	topMux.Handle("/mcp/", restHandler)
	topMux.Handle("/applications", restHandler)
	topMux.Handle("/workspace/", restHandler)
	topMux.Handle("/kb/", restHandler)
	topMux.Handle("/dynamic-workflows", restHandler)
	topMux.Handle("/dynamic-workflows/", restHandler)
	topMux.Handle("/workflows", restHandler)
	topMux.Handle("/workflows/", restHandler)
	topMux.Handle("/profile", restHandler)
	topMux.Handle("/settings/", restHandler)
	topMux.Handle("/asr/", restHandler)

	// Register web UI routes (serves embedded static files at /)
	web.RegisterRoutes(topMux)

	// Merge extra routes (e.g. music audio proxy) if set
	if s.extraRoutes != nil {
		topMux.Handle("/music/", s.extraRoutes)
	}

	return topMux
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	slog.Info("starting easyagent server", "listen", addr)
	return s.serveUntilSignal(addr)
}

// ─── GET /health ──────────────────────────────────────────────────────────────

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": Version,
	})
}

// ─── POST /chat ───────────────────────────────────────────────────────────────

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024)).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	run, _, err := s.startRun(req.SessionID, req.Prompt, req.RequestID, req.Inputs)
	if err != nil {
		runHTTPError(w, err)
		return
	}
	assistant, err := s.waitRun(r.Context(), run)
	if err != nil {
		runHTTPError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ChatResponse{Text: assistant.Text, ToolCalls: assistant.ToolCalls, SessionID: run.SessionID})
}

// SSE observes the same session-owned run as WS; losing the HTTP connection only unsubscribes.
func (s *Server) chatStream(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024)).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	run, duplicate, err := s.startRun(req.SessionID, req.Prompt, req.RequestID, req.Inputs)
	if err != nil {
		runHTTPError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	writeSSE(w, "session_id", run.SessionID)
	writeRunSSE(w, "accepted", map[string]any{"session_id": run.SessionID, "run_id": run.ID, "request_id": run.RequestID, "duplicate": duplicate})
	observer := &wsConn{outgoing: make(chan []byte, 512), done: make(chan struct{})}
	defer observer.close()
	defer s.unsubscribeRun(observer, run.SessionID)
	snapshot, err := s.runSnapshot(run.SessionID, 0, "", observer)
	if err != nil {
		writeSSE(w, "error", err.Error())
		return
	}
	if run.restored || snapshot.Run == nil || snapshot.Run.ID != run.ID {
		// A later command/run may have replaced the session projection. Repeating
		// the old request still returns its own result rather than following a new run.
		assistant, err := s.waitRun(r.Context(), run)
		if err != nil {
			writeSSE(w, "error", err.Error())
			return
		}
		writeRunSSE(w, "done", agent.AgentStreamEvent{Type: agent.StreamEventDone, FinalMessage: assistant})
		return
	}
	if events, ok := snapshot.Events.([]agent.AgentStreamEvent); ok {
		for _, event := range events {
			writeRunSSE(w, string(event.Type), event)
		}
	}
	// A completed duplicate is replayed above; no new execution or subscription wait.
	if snapshot.Run != nil && !runActive(snapshot.Run) {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-observer.done:
			return
		case data := <-observer.outgoing:
			var event struct {
				Type         string                 `json:"type"`
				Event        agent.AgentStreamEvent `json:"event"`
				State        string                 `json:"state"`
				Confirmation *pendingConfirmation   `json:"confirmation"`
			}
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			if event.Type == "event" {
				writeRunSSE(w, string(event.Event.Type), event.Event)
			}
			if event.Type == "confirmation" {
				writeRunSSE(w, "confirmation", event.Confirmation)
			}
			if event.Type == "status" && event.State != "running" && event.State != "waiting_confirmation" {
				return
			}
		}
	}
}

// ─── GET /sessions ────────────────────────────────────────────────────────────

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	mgr := s.app.SessionManager()
	sessions, err := mgr.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessions)
}

// ─── POST /sessions ───────────────────────────────────────────────────────────

// CreateSessionRequest is the request body for creating a session.
type CreateSessionRequest struct {
	Cwd         string `json:"cwd,omitempty"`
	Model       string `json:"model,omitempty"`
	Application string `json:"application,omitempty"` // e.g. "coding", "music"
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	// Allow empty body
	_ = json.NewDecoder(r.Body).Decode(&req)

	cfg := s.app.Config()

	// Apply cwd override if provided
	if req.Cwd != "" {
		cfg.Workspace = req.Cwd
	}

	// Apply model override if provided
	if req.Model != "" {
		switch cfg.Provider {
		case "openai":
			cfg.OpenAIModel = req.Model
		case "anthropic":
			cfg.AnthropicModel = req.Model
		}
	}

	deps := s.app.SessionDepsWithApp(req.Application)
	opts := runtime.AgentSessionOptions{
		Config: cfg,
	}
	sess, err := s.app.SessionStore().Create(r.Context(), opts, deps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Persist the resolved workspace, including sessions using the server default.
	if err := s.app.SessionManager().SaveMeta(sess.SessionID(), sess.Workspace(), req.Application); err != nil {
		_ = s.app.SessionStore().Delete(sess.SessionID())
		writeError(w, http.StatusInternalServerError, "cannot save session metadata")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SessionResponse{
		ID:        sess.SessionID(),
		CreatedAt: time.Now().Unix(),
	})
}

// ─── GET /sessions/{id}/messages ──────────────────────────────────────────────

func (s *Server) getSessionMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	// Check if session exists in the session manager
	if !s.app.SessionManager().Exists(sessionID) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	sess, err := s.app.LoadSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	messages, err := sess.Session().BuildContext(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type toolCallEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Args string `json:"args"`
	}

	type messageEntry struct {
		ThinkingDurationMS int64           `json:"thinking_duration_ms,omitempty"`
		Usage              ai.Usage        `json:"usage,omitempty"`
		DurationMS         int64           `json:"duration_ms,omitempty"`
		Role               string          `json:"role"`
		Content            string          `json:"content"`
		Thinking           string          `json:"thinking,omitempty"`
		ToolCalls          []toolCallEntry `json:"tool_calls,omitempty"`
		ToolCallID         string          `json:"tool_call_id,omitempty"`
		ToolDetails        any             `json:"tool_details,omitempty"`
		IsError            bool            `json:"is_error,omitempty"`
	}

	var result []messageEntry
	for _, msg := range messages {
		entry := messageEntry{Role: string(msg.Role())}
		switch m := msg.(type) {
		case ai.UserMessage:
			var texts []string
			for _, block := range m.Content {
				if block.Type == "text" {
					texts = append(texts, block.Text)
				}
			}
			entry.Content = joinTexts(texts)
			if m.DisplayText != "" {
				entry.Content = m.DisplayText
			}
		case ai.AssistantMessage:
			entry.Content = m.Text
			entry.Thinking = m.Thinking
			entry.ThinkingDurationMS = m.ThinkingDurationMS
			entry.Usage = m.Usage
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					entry.ToolCalls = append(entry.ToolCalls, toolCallEntry{
						ID: tc.ID, Name: tc.Name, Args: tc.Args,
					})
				}
			}
		case ai.ToolResultMessage:
			entry.DurationMS = m.DurationMS
			entry.Content = m.Content
			entry.ToolCallID = m.ToolCallID
			entry.IsError = m.IsError
			entry.ToolDetails = m.Details
		}
		result = append(result, entry)
	}

	if result == nil {
		result = []messageEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ─── DELETE /sessions/{id} ────────────────────────────────────────────────────

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if s.rejectActiveWorkflowActor(w, r.PathValue("id")) {
		return
	}
	sessionID := r.PathValue("id")
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	if state := s.runs.sessions[sessionID]; state != nil && state.run != nil && runActive(state.run) {
		writeError(w, http.StatusConflict, "会话仍在执行任务，请先取消或等待完成")
		return
	}
	if sess, ok := s.app.SessionStore().Get(sessionID); ok && sess.IsBusy() {
		writeError(w, http.StatusConflict, "会话仍在执行另一入口的任务，请先取消或等待完成")
		return
	}
	mgr := s.app.SessionManager()
	if err := mgr.Delete(sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.app.SessionStore().Delete(sessionID)
	delete(s.runs.sessions, sessionID)
	for requestID, run := range s.runs.requests {
		if run.SessionID == sessionID {
			delete(s.runs.requests, requestID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ─── GET /tools ───────────────────────────────────────────────────────────────

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	toolNames := s.app.ToolNames()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tools": toolNames,
	})
}

// ─── GET /applications ───────────────────────────────────────────────────────

func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	names := s.app.ApplicationNames()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"applications": names,
	})
}

// ─── GET /models ───────────────────────────────────────────────────────────────

// ModelInfo holds metadata about a single model.
type ModelInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

// ModelsResponse is the response for the models list endpoint.
type ModelsResponse struct {
	Models         []ModelInfo `json:"models"`
	Current        *ModelInfo  `json:"current,omitempty"`
	Source         string      `json:"source"`
	DiscoveryError string      `json:"discovery_error,omitempty"`
}

// gatewayModel represents a single model entry from the gateway /v1/models API.
type gatewayModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// gatewayModelsResponse is the response shape from the gateway.
type gatewayModelsResponse struct {
	Data []gatewayModel `json:"data"`
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config()

	// Determine current model from config
	var current *ModelInfo
	provider := cfg.Provider
	modelID := ""
	switch provider {
	case "openai":
		modelID = cfg.OpenAIModel
	case "anthropic":
		modelID = cfg.AnthropicModel
	}
	if modelID != "" {
		current = &ModelInfo{ID: modelID, Provider: provider, Name: modelID}
	}

	result := ModelsResponse{Models: []ModelInfo{}, Current: current, Source: "unconfigured"}
	if provider == "openai" && cfg.OpenAIBaseURL != "" {
		models, err := s.fetchGatewayModels(r.Context(), cfg.OpenAIBaseURL, cfg.OpenAIAPIKey)
		if err != nil {
			result.DiscoveryError = err.Error()
		} else if len(models) > 0 {
			result.Models, result.Source = models, "gateway"
		}
	}
	// A configured model can still be used when discovery is unsupported. Do
	// not invent selectable models or treat this fallback as a connection test.
	if len(result.Models) == 0 && current != nil {
		result.Models, result.Source = []ModelInfo{*current}, "configured"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// fetchGatewayModels queries an OpenAI-compatible /v1/models endpoint and returns
// the list advertised by the gateway; it does not perform inference.
func (s *Server) fetchGatewayModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	url := baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		slog.Debug("failed to create gateway models request", "error", err)
		return nil, fmt.Errorf("model discovery URL is invalid")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("failed to fetch gateway models", "url", url, "error", err)
		return nil, fmt.Errorf("model discovery could not connect")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("gateway models returned non-200", "status", resp.StatusCode)
		return nil, fmt.Errorf("model discovery returned HTTP %d", resp.StatusCode)
	}

	var gwResp gatewayModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&gwResp); err != nil {
		slog.Debug("failed to decode gateway models response", "error", err)
		return nil, fmt.Errorf("model discovery returned invalid JSON")
	}

	models := make([]ModelInfo, 0, len(gwResp.Data))
	for _, m := range gwResp.Data {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		models = append(models, ModelInfo{
			ID:       m.ID,
			Provider: "openai",
			Name:     name,
		})
	}
	return models, nil
}

// ─── GET /sessions/{id}/info ──────────────────────────────────────────────────

func (s *Server) getSessionInfo(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	if !s.app.SessionManager().Exists(sessionID) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	sess, err := s.app.LoadSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	provider, modelID := sess.ModelInfo()

	usage, err := sess.ContextUsage(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get workspace from config
	workspace := sess.Config().Workspace

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_mode":             sess.AccessMode(),
		"context_usage":           usage,
		"id":                      sess.SessionID(),
		"provider":                provider,
		"model":                   modelID,
		"workspace":               workspace,
		"allow_outside_workspace": sess.Config().AllowOutsideWorkspace,
	})
}

// ─── POST /sessions/{id}/model ────────────────────────────────────────────────

// SwitchModelRequest is the request body for switching a session's model.
type SwitchModelRequest struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"` // Optional: change provider along with model
}

func (s *Server) switchModel(w http.ResponseWriter, r *http.Request) {
	if s.rejectActiveWorkflowActor(w, r.PathValue("id")) {
		return
	}
	sessionID := r.PathValue("id")

	var req SwitchModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	sess, err := s.app.LoadSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if err := sess.SwitchModel(r.Context(), req.Model, req.Provider); err != nil {
		writeError(w, http.StatusInternalServerError, "switch model failed: "+err.Error())
		return
	}

	provider, modelID := sess.ModelInfo()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"provider": provider,
		"model":    modelID,
	})
}

// ─── POST /sessions/{id}/compact ──────────────────────────────────────────────

// CompactRequest is the request body for context compaction.
type CompactRequest struct {
	CustomInstructions string `json:"custom_instructions,omitempty"`
}

// CompactResponse is the response for context compaction.
type CompactResponse struct {
	Summary     string `json:"summary"`
	TrimmedFrom int    `json:"trimmed_from"`
	TrimmedTo   int    `json:"trimmed_to"`
}

func (s *Server) compactSession(w http.ResponseWriter, r *http.Request) {
	if s.rejectActiveWorkflowActor(w, r.PathValue("id")) {
		return
	}
	sessionID := r.PathValue("id")
	if s.rejectActiveRun(w, sessionID) {
		return
	}

	var req CompactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body
		req = CompactRequest{}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sess, err := s.app.LoadSession(ctx, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	summary, trimmedFrom, trimmedTo, err := sess.Compact(ctx, req.CustomInstructions)
	if err != nil {
		if errors.Is(err, agent.ErrAgentBusy) {
			writeError(w, http.StatusConflict, "会话仍在执行任务，请先取消或等待完成")
			return
		}
		writeError(w, http.StatusInternalServerError, "compact failed: "+err.Error())
		return
	}
	s.invalidateRunSnapshot(sessionID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(CompactResponse{
		Summary:     summary,
		TrimmedFrom: trimmedFrom,
		TrimmedTo:   trimmedTo,
	})
}

// ─── POST /sessions/{id}/command ──────────────────────────────────────────────

// CommandRequest is the request body for executing a slash command.
type CommandRequest struct {
	Command string `json:"command"` // e.g. "/model claude-sonnet-4-6"
}

// CommandResponse is the response for slash command execution.
type CommandResponse struct {
	Output      string `json:"output"`
	ShouldQuery bool   `json:"should_query"`
	QueryPrompt string `json:"query_prompt,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
}

func (s *Server) executeCommand(w http.ResponseWriter, r *http.Request) {
	if s.rejectActiveWorkflowActor(w, r.PathValue("id")) {
		return
	}
	sessionID := r.PathValue("id")
	if s.rejectActiveRun(w, sessionID) {
		return
	}

	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	if s.slashCmds == nil {
		writeError(w, http.StatusNotImplemented, "slash commands not available in server mode")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	sess, err := s.app.LoadSession(ctx, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	cmdCtx := slashcmd.Context{
		Ctx:     ctx,
		Session: sess,
		App:     s.app,
	}

	result, err := s.slashCmds.Execute(cmdCtx, req.Command)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := CommandResponse{
		Output: result.Output,
	}
	s.invalidateRunSnapshot(sessionID)
	if result.SessionSwitchTo != nil {
		resp.SessionID = result.SessionSwitchTo.SessionID()
	}
	if result.ShouldQuery {
		resp.ShouldQuery = true
		resp.QueryPrompt = result.QueryPrompt
		if resp.QueryPrompt == "" {
			resp.QueryPrompt = "Start working..."
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ─── POST /tools/register ────────────────────────────────────────────────────

func (s *Server) registerTool(w http.ResponseWriter, r *http.Request) {
	var def agent.ExternalToolDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if def.Name == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}
	if def.CallbackURL == "" {
		writeError(w, http.StatusBadRequest, "callback_url is required")
		return
	}

	// Validate the tool can be constructed
	if _, err := agent.NewExternalTool(def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tool: "+err.Error())
		return
	}

	s.toolMu.Lock()
	// Replace if same name exists
	updated := false
	for i, existing := range s.externalTools {
		if existing.Name == def.Name {
			s.externalTools[i] = def
			updated = true
			break
		}
	}
	if !updated {
		s.externalTools = append(s.externalTools, def)
	}
	s.syncToApp()
	s.toolMu.Unlock()

	if updated {
		slog.Info("updated external tool", "name", def.Name, "callback", def.CallbackURL)
	} else {
		slog.Info("registered external tool", "name", def.Name, "callback", def.CallbackURL)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": def.Name})
}

// ExternalTools returns all registered external tool definitions.
func (s *Server) ExternalTools() []agent.ExternalToolDef {
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	result := make([]agent.ExternalToolDef, len(s.externalTools))
	copy(result, s.externalTools)
	return result
}

// syncToApp pushes external tools to the App (must be called with toolMu held).
func (s *Server) syncToApp() {
	defs := make([]agent.ExternalToolDef, len(s.externalTools))
	copy(defs, s.externalTools)
	s.app.SetExternalTools(defs)
}

// ─── session resolution ──────────────────────────────────────────────────────

// resolveSession gets an existing session or creates a new one.
func (s *Server) resolveSession(ctx context.Context, sessionID string) (*runtime.AgentSession, error) {
	return s.app.LoadOrCreateSession(ctx, sessionID)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

func writeSSE(w http.ResponseWriter, eventType, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func joinTexts(texts []string) string {
	result := ""
	for i, t := range texts {
		if i > 0 {
			result += "\n"
		}
		result += t
	}
	return result
}

// ─── middleware ───────────────────────────────────────────────────────────────

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", time.Since(start).Round(time.Millisecond),
		)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("handler panic", "error", err, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("internal error: %v", err))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ─── GET /sessions/{id}/diff ────────────────────────────────────────────────

func (s *Server) getSessionDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.resolveSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cwd := sess.Workspace()
	if cwd == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{}})
		return
	}

	// Run git diff --stat and git diff in the workspace
	files, err := gitDiff(cwd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
}

// ─── GET /sessions/{id}/file?path=... ───────────────────────────────────────

func (s *Server) getSessionFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	sess, err := s.resolveSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	safePath, err := securePath(sess.Workspace(), path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	data, err := os.ReadFile(safePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
}

// ─── PUT /sessions/{id}/file?path=... ────────────────────────────────────────

type writeFileRequest struct {
	Content string `json:"content"`
}

func (s *Server) writeFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	var req writeFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sess, err := s.resolveSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	safePath, err := securePath(sess.Workspace(), path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Ensure parent directory exists
	dir := filepath.Dir(safePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
		return
	}

	if err := os.WriteFile(safePath, []byte(req.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// gitDiff runs git diff in the given directory and returns parsed file diffs.
type fileDiff struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Patch   string `json:"patch"`
}

func gitDiff(cwd string) ([]fileDiff, error) {
	// Get the raw diff
	cmd := exec.Command("git", "diff", "--no-color")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		// git diff returns exit code 1 if there are differences in some configs
		// but with --no-color it should be 0. If it fails, return empty.
		return []fileDiff{}, nil
	}

	diffText := string(output)
	if diffText == "" {
		return []fileDiff{}, nil
	}

	// Parse diff into per-file sections
	var files []fileDiff
	var current *fileDiff
	var currentPatch strings.Builder

	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			if current != nil {
				current.Patch = currentPatch.String()
				files = append(files, *current)
			}
			current = &fileDiff{}
			currentPatch.Reset()
			currentPatch.WriteString(line + "\n")
		} else if current != nil {
			currentPatch.WriteString(line + "\n")
			if strings.HasPrefix(line, "+++ b/") {
				current.Path = strings.TrimPrefix(line, "+++ b/")
			} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				current.Added++
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				current.Removed++
			}
		}
	}
	if current != nil {
		current.Patch = currentPatch.String()
		files = append(files, *current)
	}

	return files, nil
}

// ─── GET /workspace/list-dir?path=... ───────────────────────────────────────

// workspaceRoot uses the selected session's project when explicitly supplied.
// An explicitly selected missing or invalid session never falls back to the global workspace.
func (s *Server) workspaceRoot(w http.ResponseWriter, r *http.Request) (string, bool) {
	query := r.URL.Query()
	if !query.Has("session_id") {
		return s.app.Config().Workspace, true
	}
	id := query.Get("session_id")
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusNotFound, "session not found")
		return "", false
	}
	manager := s.app.SessionManager()
	if _, err := securePath(manager.SessionsDir(), id); err != nil || !manager.Exists(id) {
		writeError(w, http.StatusNotFound, "session not found")
		return "", false
	}
	sess, err := s.app.LoadSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return "", false
	}
	return sess.Workspace(), true
}

// DirEntry represents a single entry in a directory listing.
type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

func (s *Server) listDir(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		dirPath = root
	}
	safePath, err := securePath(root, dirPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := os.ReadDir(safePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read directory: "+err.Error())
		return
	}

	// Hidden directories that should never appear in the file explorer.
	hiddenDirs := map[string]bool{
		"node_modules": true, ".git": true, ".svn": true, ".hg": true,
		".DS_Store": true, "__pycache__": true, ".pytest_cache": true,
		"dist": true, "build": true, "out": true, ".next": true,
		".nuxt": true, ".turbo": true, ".cache": true, "vendor": true,
		"target": true, ".gradle": true, ".idea": true, ".vscode": true,
	}

	var result []DirEntry
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files/dirs (starting with .) except some common config files
		if strings.HasPrefix(name, ".") {
			// Allow .env, .gitignore, .golangci.yml, etc (dotfiles that are useful)
			// but skip .DS_Store
			if name == ".DS_Store" || name == ".git" || name == ".svn" || name == ".hg" {
				continue
			}
		}

		// Skip known heavy directories
		if hiddenDirs[name] {
			continue
		}

		isDir := entry.IsDir()
		result = append(result, DirEntry{
			Name:  name,
			Path:  filepath.Join(safePath, name),
			IsDir: isDir,
		})
	}

	if result == nil {
		result = []DirEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ─── GET /workspace/search-files?path=... ────────────────────────────────────

func (s *Server) searchFiles(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	rootPath := r.URL.Query().Get("path")
	if rootPath == "" {
		rootPath = root
	}
	safeRoot, err := securePath(root, rootPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Collect all file paths under root, respecting ignore patterns.
	var files []string

	hiddenDirs := map[string]bool{
		"node_modules": true, ".git": true, ".svn": true, ".hg": true,
		"__pycache__": true, ".pytest_cache": true, "dist": true,
		"build": true, "out": true, ".next": true, ".nuxt": true,
		".turbo": true, ".cache": true, "vendor": true, "target": true,
		".gradle": true, ".idea": true, ".vscode": true, ".DS_Store": true,
	}

	maxFiles := 20000 // safety limit to avoid scanning huge repos
	var walk func(dir string) bool
	walk = func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			name := entry.Name()
			if len(files) >= maxFiles {
				return true // stop
			}

			// Skip hidden and known heavy directories
			if hiddenDirs[name] {
				continue
			}
			if strings.HasPrefix(name, ".") && name != ".gitignore" && name != ".env" {
				continue
			}

			fullPath := filepath.Join(dir, name)
			relPath, err := filepath.Rel(safeRoot, fullPath)
			if err != nil {
				relPath = name
			}

			if entry.IsDir() {
				if walk(fullPath) {
					return true
				}
			} else {
				files = append(files, relPath)
			}
		}
		return false
	}

	_ = walk(safeRoot)

	if files == nil {
		files = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}

// ─── GET /workspace/read-file?path=... ───────────────────────────────────────

func (s *Server) workspaceReadFile(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	safePath, err := securePath(root, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
}

// ─── GET /workspace/read-file-base64?path=... ────────────────────────────────

func (s *Server) workspaceReadFileBase64(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	safePath, err := securePath(root, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found: "+err.Error())
		return
	}

	// Detect MIME type from extension
	ext := strings.ToLower(filepath.Ext(path))
	mimeType := "application/octet-stream"
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	case ".bmp":
		mimeType = "image/bmp"
	case ".svg":
		mimeType = "image/svg+xml"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"data":     base64.StdEncoding.EncodeToString(data),
		"mimeType": mimeType,
	})
}

// ─── PUT /workspace/write-file?path=... ──────────────────────────────────────

func (s *Server) workspaceWriteFile(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	var req writeFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	safePath, err := securePath(root, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Ensure parent directory exists
	dir := filepath.Dir(safePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
		return
	}

	if err := os.WriteFile(safePath, []byte(req.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func corsMiddleware(s *Server) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			// 默认不返回 CORS 头（同源 UI 与原生客户端不受影响）；
			// 仅白名单内的 Origin 显式放行，不再使用 *。
			if origin != "" && s != nil {
				for _, allowed := range s.allowedOrigins {
					if allowed == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Vary", "Origin")
						w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
						w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
						break
					}
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush preserves SSE delivery through logging middleware. Without this method,
// short progress events stay in net/http's buffer until the whole turn finishes.
func (rw *responseWriter) Flush() {
	_ = http.NewResponseController(rw.ResponseWriter).Flush()
}

// Unwrap lets ResponseController reach the underlying writer's capabilities.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// Hijack implements the http.Hijacker interface so WebSocket upgrades work
// through the logging middleware wrapper.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.ResponseWriter.(http.Hijacker).Hijack()
}

// setSessionPermissions does not resolve an already pending approval or change a busy run.
func (s *Server) setSessionPermissions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.rejectActiveWorkflowActor(w, id) {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || (req.Mode != "ask" && req.Mode != "full") {
		writeError(w, http.StatusBadRequest, "mode must be ask or full")
		return
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	if state := s.runs.sessions[id]; state != nil && state.run != nil && runActive(state.run) {
		writeError(w, http.StatusConflict, "请等待当前任务完成后切换权限")
		return
	}
	sess, err := s.app.LoadSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err := sess.TrySetAccessMode(s.confirmRunTool, req.Mode == "ask"); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"access_mode": sess.AccessMode()})
}
