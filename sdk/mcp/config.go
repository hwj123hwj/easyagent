// Package mcp connects external MCP servers to the Agent tool pipeline.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// MCPServerConfig is compatible with the mcpServers format used by other clients.
// Timeout is milliseconds, retaining EasyAgent's existing configuration format.
type MCPServerConfig struct {
	Type         string            `json:"type,omitempty"`
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	URL          string            `json:"url,omitempty"`
	HTTPURL      string            `json:"httpUrl,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
	Trust        bool              `json:"trust,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	IncludeTools []string          `json:"includeTools,omitempty"`
	ExcludeTools []string          `json:"excludeTools,omitempty"`
	Description  string            `json:"description,omitempty"`
	OAuth        *OAuthConfig      `json:"oauth,omitempty"`
}

type OAuthConfig struct {
	ClientID              string   `json:"clientId,omitempty"`
	ClientSecret          string   `json:"clientSecret,omitempty"`
	Scopes                []string `json:"scopes,omitempty"`
	AuthServerMetadataURL string   `json:"authServerMetadataUrl,omitempty"`
}

func (c MCPServerConfig) Transport() string {
	if c.Type == "sse" {
		return "sse"
	}
	if c.Command != "" {
		return "stdio"
	}
	if c.HTTPURL != "" || c.URL != "" {
		return "http"
	}
	return ""
}
func (c MCPServerConfig) endpoint() string {
	if c.HTTPURL != "" {
		return c.HTTPURL
	}
	return c.URL
}
func (c MCPServerConfig) enabled() bool { return c.Enabled == nil || *c.Enabled }

func (c MCPServerConfig) Validate() error {
	if c.Type != "" && c.Type != "stdio" && c.Type != "http" && c.Type != "streamable-http" && c.Type != "sse" {
		return errors.New("unsupported transport type")
	}
	if c.Transport() == "" {
		return errors.New("specify command or url/httpUrl")
	}
	if c.Command != "" && (c.URL != "" || c.HTTPURL != "" || (c.Type != "" && c.Type != "stdio")) {
		return errors.New("stdio and HTTP configuration cannot be combined")
	}
	if c.Type == "stdio" && c.Command == "" {
		return errors.New("stdio requires command")
	}
	if c.URL != "" && c.HTTPURL != "" {
		return errors.New("specify only one of url and httpUrl")
	}
	if c.Timeout < 0 {
		return errors.New("timeout must be non-negative milliseconds")
	}
	if c.Command == "" {
		if err := validateHTTPURL(c.endpoint()); err != nil {
			return err
		}
	}
	for k, v := range c.Headers {
		if strings.ContainsAny(k+v, "\r\n") || strings.TrimSpace(k) == "" {
			return errors.New("invalid HTTP header")
		}
	}
	for k := range c.Env {
		if strings.ContainsAny(k, "=\x00") || k == "" {
			return errors.New("invalid environment variable name")
		}
	}
	for _, pattern := range append(append([]string(nil), c.IncludeTools...), c.ExcludeTools...) {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return errors.New("invalid tool filter pattern")
		}
	}
	if c.OAuth != nil && c.Command != "" {
		return errors.New("OAuth is only supported for HTTP servers")
	}
	if c.OAuth != nil && c.OAuth.AuthServerMetadataURL != "" {
		if err := validateOAuthURL(c.OAuth.AuthServerMetadataURL); err != nil {
			return err
		}
	}
	return nil
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("endpoint must be an HTTP(S) URL without credentials or fragment")
	}
	return nil
}
func validateOAuthURL(raw string) error {
	if err := validateHTTPURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	if u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return errors.New("OAuth URLs require HTTPS except on loopback")
	}
	return nil
}

var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateName(name string) error {
	if !serverNamePattern.MatchString(name) || len(name) > 64 {
		return errors.New("server name must contain 1-64 letters, digits, underscores or hyphens")
	}
	return nil
}
func namespace(name string) string { return strings.ReplaceAll(name, "-", "_") }

type ConfigIssue struct {
	Source  string `json:"source"`
	Server  string `json:"server,omitempty"`
	Message string `json:"message"`
}
type ConfigEntry struct {
	Name   string
	Config MCPServerConfig
	Source string
	Scope  string
}

// LoadMergedConfig skips invalid entries independently. Project files are never
// read until the caller explicitly grants project trust.
func LoadMergedConfig(userPath, workspace string, projectTrusted bool) ([]ConfigEntry, []ConfigIssue) {
	entries := map[string]ConfigEntry{}
	issues := []ConfigIssue{}
	load := func(path, scope string) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			issues = append(issues, ConfigIssue{Source: path, Message: "cannot read MCP configuration"})
			return
		}
		var root struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(data, &root) != nil {
			issues = append(issues, ConfigIssue{Source: path, Message: "invalid MCP configuration JSON"})
			return
		}
		names := make([]string, 0, len(root.Servers))
		for name := range root.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			raw := root.Servers[name]
			var cfg MCPServerConfig
			err := validateName(name)
			if err == nil {
				err = json.Unmarshal(raw, &cfg)
			}
			if err == nil {
				err = cfg.Validate()
			}
			if err != nil {
				issues = append(issues, ConfigIssue{Source: path, Server: name, Message: "invalid server configuration: " + safeConfigError(err)})
				continue
			}
			collision := false
			for other := range entries {
				if other != name && (namespace(other) == namespace(name) || toolNamespace(other) == toolNamespace(name)) {
					collision = true
				}
			}
			if collision {
				issues = append(issues, ConfigIssue{Source: path, Server: name, Message: "server namespace collides with another entry"})
				continue
			}
			entries[name] = ConfigEntry{Name: name, Config: cfg, Source: path, Scope: scope}
		}
	}
	load(userPath, "user")
	if projectTrusted && workspace != "" {
		load(filepath.Join(workspace, ".easyagent", "mcp.json"), "project")
	}
	result := make([]ConfigEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result, issues
}
func safeConfigError(err error) string {
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typed) {
		return "invalid field type or JSON"
	}
	return err.Error()
}

// Config writes are serialized across managers and replace only one entry.
var configFileMu sync.Mutex

func editConfig(path, name string, cfg *MCPServerConfig) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	root := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &root) != nil {
			return errors.New("cannot edit invalid MCP configuration")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok {
		if json.Unmarshal(raw, &servers) != nil || servers == nil {
			return errors.New("invalid mcpServers object")
		}
	}
	if cfg == nil {
		delete(servers, name)
	} else {
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		servers[name] = raw
	}
	root["mcpServers"], _ = json.Marshal(servers)
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateWrite(path, append(data, '\n'))
}
func atomicPrivateWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mcp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("save MCP configuration: %w", err)
	}
	return nil
}
