package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/agent"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

var errNeedsAuth = errors.New("MCP server requires sign-in")
var nextProgress atomic.Uint64

type connection struct {
	manager      *Manager
	entry        ConfigEntry
	mu           sync.Mutex
	updates      map[string]func(agent.PartialResult)
	stderr       *tailBuffer
	challenge    http.Header
	authRequired bool
}

func newConnection(manager *Manager, entry ConfigEntry) *connection {
	return &connection{manager: manager, entry: entry, updates: map[string]func(agent.PartialResult){}, stderr: &tailBuffer{}}
}

func expandEnv(value string) (string, error) {
	missing := false
	value = os.Expand(value, func(key string) string {
		v, ok := os.LookupEnv(key)
		if !ok {
			missing = true
		}
		return v
	})
	if missing {
		return "", errors.New("referenced environment variable is not set")
	}
	return value, nil
}
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func (c *connection) connect(ctx context.Context, workspace string) (*protocol.ClientSession, error) {
	cfg := c.entry.Config
	client := protocol.NewClient(&protocol.Implementation{Name: "easyagent", Version: "0.1.0"}, &protocol.ClientOptions{
		ToolListChangedHandler: func(ctx context.Context, _ *protocol.ToolListChangedRequest) {
			go func() { _ = c.manager.RefreshTools(context.Background(), c.entry.Name) }()
		},
		ProgressNotificationHandler: func(ctx context.Context, request *protocol.ProgressNotificationClientRequest) {
			key := fmt.Sprint(request.Params.ProgressToken)
			c.mu.Lock()
			update := c.updates[key]
			c.mu.Unlock()
			if update != nil {
				text := request.Params.Message
				if text == "" {
					text = fmt.Sprintf("Progress %.0f", request.Params.Progress)
					if request.Params.Total > 0 {
						text += fmt.Sprintf("/%.0f", request.Params.Total)
					}
				}
				update(agent.PartialResult{Content: text})
			}
		},
	})
	var transport protocol.Transport
	if cfg.Transport() == "stdio" {
		cmd := exec.Command(expandHome(cfg.Command), cfg.Args...)
		cmd.Dir = workspace
		if cfg.Cwd != "" {
			cmd.Dir = expandHome(cfg.Cwd)
			if !filepath.IsAbs(cmd.Dir) {
				cmd.Dir = filepath.Join(workspace, cmd.Dir)
			}
		}
		cmd.Env = os.Environ()
		for key, raw := range cfg.Env {
			value, err := expandEnv(raw)
			if err != nil {
				return nil, err
			}
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		cmd.Stderr = c.stderr
		prepareProcess(cmd)
		transport = &processTransport{delegate: &protocol.CommandTransport{Command: cmd, TerminateDuration: 250 * time.Millisecond}, command: cmd}
	} else {
		headers := map[string]string{}
		for key, raw := range cfg.Headers {
			value, err := expandEnv(raw)
			if err != nil {
				return nil, err
			}
			headers[key] = value
		}
		httpClient := &http.Client{Transport: &credentialTransport{connection: c, headers: headers, base: http.DefaultTransport}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && (req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme) {
				return errors.New("cross-origin MCP redirect refused")
			}
			if len(via) > 5 {
				return errors.New("too many redirects")
			}
			return nil
		}}
		if cfg.Transport() == "sse" {
			transport = &detachedTransport{delegate: &protocol.SSEClientTransport{Endpoint: cfg.endpoint(), HTTPClient: httpClient}}
		} else {
			transport = &protocol.StreamableClientTransport{Endpoint: cfg.endpoint(), HTTPClient: httpClient, MaxRetries: -1}
		}
	}
	return client.Connect(ctx, transport, nil)
}

func (c *connection) progress(params *protocol.CallToolParams, update func(agent.PartialResult)) func() {
	if update == nil {
		return func() {}
	}
	key := fmt.Sprintf("easyagent-%d", nextProgress.Add(1))
	// SDK v1.4's SetProgressToken does not install a newly allocated nil Meta.
	// Initialize it explicitly so the token is present on the wire.
	params.SetMeta(map[string]any{})
	params.SetProgressToken(key)
	c.mu.Lock()
	c.updates[key] = update
	c.mu.Unlock()
	return func() { c.mu.Lock(); delete(c.updates, key); c.mu.Unlock() }
}

func (c *connection) safeError(err error) string {
	if err == nil {
		return "connection closed"
	}
	if c.needsAuth(err) {
		return "Sign-in required or saved credentials were rejected"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "MCP request timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "MCP request canceled"
	}
	if errors.Is(err, protocol.ErrConnectionClosed) {
		return "MCP connection closed; reconnect to continue"
	}
	// Transport errors can contain header values, command arguments and URL
	// query secrets. Public state must not expose their raw text or stderr.
	return "MCP connection or protocol error; check server configuration and availability"
}

func (c *connection) needsAuth(err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authRequired || errors.Is(err, errNeedsAuth)
}

type credentialTransport struct {
	connection *connection
	headers    map[string]string
	base       http.RoundTripper
}

func (t *credentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint, _ := url.Parse(t.connection.entry.Config.endpoint())
	if req.URL.Scheme != endpoint.Scheme || req.URL.Host != endpoint.Host {
		return nil, errors.New("cross-origin MCP endpoint refused")
	}
	req = req.Clone(req.Context())
	if req.Method == http.MethodDelete {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		req = req.Clone(ctx)
	}
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	if req.Header.Get("Authorization") == "" {
		token, err := t.connection.manager.auth.token(req.Context(), t.connection.entry.Config.endpoint())
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			t.connection.mu.Lock()
			t.connection.authRequired = true
			t.connection.mu.Unlock()
			return nil, errNeedsAuth
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.connection.mu.Lock()
		t.connection.challenge = resp.Header.Clone()
		t.connection.authRequired = true
		t.connection.mu.Unlock()
		resp.Body.Close()
		return nil, errNeedsAuth
	}
	return resp, nil
}

// A bounded stderr tail is retained for local diagnostics, never public status.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 4096 {
		b.data = append([]byte(nil), b.data[len(b.data)-4096:]...)
	}
	return len(p), nil
}

type processTransport struct {
	delegate protocol.Transport
	command  *exec.Cmd
}

// Unlike Streamable HTTP, the SDK v1.4 legacy SSE transport binds its hanging
// GET to Connect's context. Detach the established stream while preserving the
// caller's deadline during connection setup.
type detachedTransport struct{ delegate protocol.Transport }

func (t *detachedTransport) Connect(ctx context.Context) (protocol.Connection, error) {
	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-done:
		}
	}()
	conn, err := t.delegate.Connect(lifetime)
	close(done)
	if err != nil {
		cancel()
		return nil, err
	}
	return &cancelConnection{Connection: conn, cancel: cancel}, nil
}

type cancelConnection struct {
	protocol.Connection
	cancel context.CancelFunc
}

func (c *cancelConnection) Close() error { c.cancel(); return c.Connection.Close() }

func (t *processTransport) Connect(ctx context.Context) (protocol.Connection, error) {
	conn, err := t.delegate.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &processConnection{Connection: conn, command: t.command}, nil
}

type processConnection struct {
	protocol.Connection
	command *exec.Cmd
	once    sync.Once
	err     error
}

func (c *processConnection) Close() error {
	c.once.Do(func() {
		timer := time.AfterFunc(time.Second, func() { killProcessGroup(c.command) })
		c.err = c.Connection.Close()
		timer.Stop()
		killProcessGroup(c.command)
	})
	return c.err
}
