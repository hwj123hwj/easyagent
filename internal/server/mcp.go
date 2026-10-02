package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/hwj123hwj/easyagent/sdk/mcp"
)

func (s *Server) registerMCPRoutes(mux *http.ServeMux) {
	edit := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			release, err := s.app.BeginMCPEdit()
			if err != nil {
				writeError(w, 409, err.Error())
				return
			}
			defer release()
			handler(w, r)
		}
	}
	mux.HandleFunc("GET /mcp", s.listMCP)
	mux.HandleFunc("PUT /mcp/project-trust", s.trustMCPProject)
	mux.HandleFunc("PUT /mcp/servers/{name}", edit(s.putMCPServer))
	mux.HandleFunc("DELETE /mcp/servers/{name}", edit(s.deleteMCPServer))
	mux.HandleFunc("POST /mcp/servers/{name}/tools/{tool}", edit(s.toggleMCPTool))
	mux.HandleFunc("POST /mcp/servers/{name}/login/complete", edit(s.completeMCPLogin))
	mux.HandleFunc("POST /mcp/servers/{name}/{action}", edit(s.actMCPServer))
}

func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) mcpWorkspace(r *http.Request) string {
	return s.app.MCPWorkspace(r.URL.Query().Get("workspace"))
}
func (s *Server) mcpStatus(workspace string) map[string]any {
	manager := s.app.MCP(workspace)
	return map[string]any{"workspace": s.app.MCPWorkspace(workspace), "project_trusted": s.app.MCPProjectTrusted(workspace), "servers": manager.Servers(), "issues": manager.Issues()}
}
func (s *Server) listMCP(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, s.mcpStatus(s.mcpWorkspace(r)))
}
func mcpScope(r *http.Request, manager *mcp.Manager) string {
	if scope := r.URL.Query().Get("scope"); scope != "" {
		return scope
	}
	for _, server := range manager.Servers() {
		if server.Name == r.PathValue("name") {
			return server.Scope
		}
	}
	return "user"
}
func (s *Server) mcpEdit(w http.ResponseWriter, r *http.Request) (*mcp.Manager, string, string, bool) {
	workspace := s.mcpWorkspace(r)
	manager := s.app.MCP(workspace)
	scope := mcpScope(r, manager)
	if scope != "user" && scope != "project" {
		writeError(w, 400, "scope must be user or project")
		return nil, "", "", false
	}
	if err := s.app.CheckMCPIdle(workspace, scope); err != nil {
		writeError(w, 409, err.Error())
		return nil, "", "", false
	}
	if scope == "project" && !s.app.MCPProjectTrusted(workspace) {
		writeError(w, 403, "请先授权此项目的 MCP 配置，再编辑项目服务")
		return nil, "", "", false
	}
	return manager, workspace, scope, true
}
func (s *Server) mcpUpdated(w http.ResponseWriter, workspace, scope string, manager *mcp.Manager) {
	if scope == "user" {
		s.app.ReloadMCP(manager)
	}
	jsonResponse(w, s.mcpStatus(workspace))
}
func (s *Server) putMCPServer(w http.ResponseWriter, r *http.Request) {
	manager, workspace, scope, ok := s.mcpEdit(w, r)
	if !ok {
		return
	}
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024)).Decode(&patch); err != nil || patch == nil {
		writeError(w, 400, "invalid MCP configuration")
		return
	}
	current, _ := manager.ServerConfig(r.PathValue("name"), scope)
	b, _ := json.Marshal(current)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(b, &merged)
	// Field-level patch: omitted secrets survive, explicit null/{} clears them.
	for key, value := range patch {
		merged[key] = value
	}
	b, _ = json.Marshal(merged)
	var cfg mcp.MCPServerConfig
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		writeError(w, 400, "invalid MCP field or value")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := manager.PutServer(r.Context(), r.PathValue("name"), cfg, scope); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mcpUpdated(w, workspace, scope, manager)
}
func (s *Server) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	manager, workspace, scope, ok := s.mcpEdit(w, r)
	if !ok {
		return
	}
	if err := manager.RemoveServer(r.Context(), r.PathValue("name"), scope); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mcpUpdated(w, workspace, scope, manager)
}
func (s *Server) trustMCPProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Workspace string `json:"workspace"`
		Trusted   *bool  `json:"trusted"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil || body.Trusted == nil {
		writeError(w, 400, "workspace and trusted are required")
		return
	}
	workspace := s.app.MCPWorkspace(body.Workspace)
	if err := s.app.SetMCPProjectTrust(r.Context(), workspace, *body.Trusted); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	jsonResponse(w, s.mcpStatus(workspace))
}
func (s *Server) actMCPServer(w http.ResponseWriter, r *http.Request) {
	manager, workspace, scope, ok := s.mcpEdit(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	for _, server := range manager.Servers() {
		if server.Name == name && server.Scope != scope {
			writeError(w, 409, "此操作只能作用于当前生效的服务来源；请先移除同名项目覆盖")
			return
		}
	}
	var err error
	switch r.PathValue("action") {
	case "enable", "disable":
		err = manager.SetEnabled(r.Context(), name, r.PathValue("action") == "enable")
	case "trust", "untrust":
		err = manager.SetTrust(r.Context(), name, r.PathValue("action") == "trust")
	case "refresh":
		err = manager.RefreshTools(r.Context(), name)
	case "reconnect":
		err = manager.Reconnect(r.Context(), name)
	case "logout":
		err = manager.Logout(r.Context(), name)
	case "login":
		var body struct {
			RedirectURL string `json:"redirect_url"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); e != nil {
			writeError(w, 400, "redirect_url is required")
			return
		}
		info, e := manager.BeginLogin(r.Context(), name, body.RedirectURL)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		jsonResponse(w, info)
		return
	default:
		writeError(w, 404, "unknown MCP action")
		return
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mcpUpdated(w, workspace, scope, manager)
}
func (s *Server) completeMCPLogin(w http.ResponseWriter, r *http.Request) {
	manager, workspace, scope, ok := s.mcpEdit(w, r)
	if !ok {
		return
	}
	var body struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); e != nil {
		writeError(w, 400, "invalid OAuth response")
		return
	}
	if e := manager.CompleteLogin(r.Context(), r.PathValue("name"), body.State, body.Code); e != nil {
		writeError(w, 400, e.Error())
		return
	}
	s.mcpUpdated(w, workspace, scope, manager)
}
func (s *Server) toggleMCPTool(w http.ResponseWriter, r *http.Request) {
	manager, workspace, scope, ok := s.mcpEdit(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); e != nil || body.Enabled == nil {
		writeError(w, 400, "enabled is required")
		return
	}
	name, tool := r.PathValue("name"), r.PathValue("tool")
	cfg, exists := manager.ServerConfig(name, scope)
	if !exists {
		writeError(w, 404, "MCP server not found in this scope")
		return
	}
	found := false
	for _, srv := range manager.Servers() {
		if srv.Name == name {
			for _, t := range srv.Tools {
				if t.Name == tool {
					found = true
				}
			}
		}
	}
	if !found {
		writeError(w, 404, "MCP tool not found")
		return
	}
	excluded := cfg.ExcludeTools[:0]
	for _, t := range cfg.ExcludeTools {
		if t != tool {
			excluded = append(excluded, t)
		}
	}
	cfg.ExcludeTools = excluded
	if *body.Enabled {
		for _, pattern := range cfg.ExcludeTools {
			if matched, _ := filepath.Match(pattern, tool); matched {
				writeError(w, 409, "此工具被排除规则限制，请先编辑 excludeTools 规则")
				return
			}
		}
		if len(cfg.IncludeTools) > 0 {
			cfg.IncludeTools = append(cfg.IncludeTools, tool)
		}
	} else {
		cfg.ExcludeTools = append(cfg.ExcludeTools, tool)
	}
	if e := manager.PutServer(r.Context(), name, cfg, scope); e != nil {
		writeError(w, 400, e.Error())
		return
	}
	s.mcpUpdated(w, workspace, scope, manager)
}

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	commands := []map[string]any{}
	if s.slashCmds != nil {
		for _, name := range s.slashCmds.Names() {
			if name == "clear" || name == "quit" || name == "exit" {
				continue // Terminal-only actions have no meaning for service clients.
			}
			cmd := s.slashCmds.Command(name)
			subs := []map[string]string{}
			for _, sub := range cmd.Subcommands {
				subs = append(subs, map[string]string{"name": sub.Name, "description": sub.Description})
			}
			commands = append(commands, map[string]any{"name": name, "description": cmd.Description, "subcommands": subs})
		}
	}
	jsonResponse(w, map[string]any{"commands": commands})
}
