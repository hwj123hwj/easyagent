package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestOAuthRefreshDoesNotBlockIndependentServer(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(server.Close)
	store := &authStore{path: t.TempDir() + "/auth.json"}
	blocked := server.URL + "/blocked"
	other := server.URL + "/other"
	require.NoError(t, store.put(blocked, &authRecord{Token: &oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Minute)}, ClientID: "client", Resource: blocked, Endpoint: oauth2.Endpoint{TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInParams}}))
	require.NoError(t, store.put(other, &authRecord{Token: &oauth2.Token{AccessToken: "valid", Expiry: time.Now().Add(time.Hour)}}))
	var released sync.Once
	defer released.Do(func() { close(release) })
	refreshed := make(chan error, 1)
	go func() { _, err := store.token(context.Background(), blocked); refreshed <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh request did not start")
	}
	cancelCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	canceled := make(chan error, 1)
	go func() { _, err := store.token(cancelCtx, blocked); canceled <- err }()
	select {
	case err := <-canceled:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled call remained blocked behind credential refresh")
	}
	result := make(chan string, 1)
	go func() { token, _ := store.token(context.Background(), other); result <- token }()
	select {
	case token := <-result:
		require.Equal(t, "valid", token)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("independent MCP credentials blocked behind another server's refresh")
	}
	released.Do(func() { close(release) })
	select {
	case err := <-refreshed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("refresh did not finish after release")
	}
}

func TestOAuthLoginPKCEStateRefreshAndLogout(t *testing.T) {
	server := testServer()
	handler := protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil)
	var base, expectedChallenge string
	var refreshes atomic.Int32
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mcp":
			if r.Header.Get("Authorization") != "Bearer token-one" && r.Header.Get("Authorization") != "Bearer token-two" {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			handler.ServeHTTP(w, r)
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}, "scopes_supported": []string{"read"}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "registration_endpoint": base + "/register", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}})
		case "/register":
			var request map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "EasyAgent", request["client_name"])
			require.Equal(t, "none", request["token_endpoint_auth_method"])
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-one"})
		case "/token":
			require.NoError(t, r.ParseForm())
			require.Equal(t, base+"/mcp", r.Form.Get("resource"))
			require.Equal(t, "client-one", r.Form.Get("client_id"))
			if r.Form.Get("grant_type") == "refresh_token" {
				require.Equal(t, "refresh-one", r.Form.Get("refresh_token"))
				refreshes.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-two", "refresh_token": "refresh-two", "token_type": "Bearer", "expires_in": 3600})
				return
			}
			require.Equal(t, "code-one", r.Form.Get("code"))
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			require.Equal(t, expectedChallenge, base64.RawURLEncoding.EncodeToString(hash[:]))
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-one", "refresh_token": "refresh-one", "token_type": "Bearer", "expires_in": 3600})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	base = hs.URL
	t.Cleanup(hs.Close)
	m := managerFor(t, MCPServerConfig{URL: base + "/mcp", Trust: true})
	m.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, m.WaitReady(ctx))
	require.Equal(t, StatusNeedsAuth, m.Servers()[0].State)
	login, err := m.BeginLogin(context.Background(), "test", "http://127.0.0.1:8765/callback")
	require.NoError(t, err)
	parsed, err := url.Parse(login.AuthorizationURL)
	require.NoError(t, err)
	require.Equal(t, login.State, parsed.Query().Get("state"))
	require.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))
	require.Equal(t, base+"/mcp", parsed.Query().Get("resource"))
	expectedChallenge = parsed.Query().Get("code_challenge")
	require.Error(t, m.CompleteLogin(context.Background(), "test", "wrong-state", "code-one"))
	require.NoError(t, m.CompleteLogin(context.Background(), "test", login.State, "code-one"))
	require.Error(t, m.CompleteLogin(context.Background(), "test", login.State, "code-one"))
	tool := m.AllTools()[0]
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.False(t, result.IsError, result)
	authFileMu.Lock()
	records, err := m.auth.read()
	authFileMu.Unlock()
	require.NoError(t, err)
	record := records[base+"/mcp"]
	record.Token.Expiry = time.Now().Add(-time.Minute)
	require.NoError(t, m.auth.put(base+"/mcp", &record))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := tool.Execute(context.Background(), json.RawMessage(`{}`), nil)
			require.NoError(t, err)
			require.False(t, result.IsError, result)
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), refreshes.Load())
	raw, _ := json.Marshal(m.Servers())
	require.NotContains(t, string(raw), "token-one")
	require.NotContains(t, string(raw), "refresh-two")
	info, err := os.Stat(m.options.AuthStorePath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.NoError(t, m.Logout(context.Background(), "test"))
	require.Equal(t, StatusNeedsAuth, m.Servers()[0].State)
	result, err = tool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.True(t, result.IsError)
}

func TestOAuthRejectsUntrustedIssuerAndInsecureRedirect(t *testing.T) {
	var base string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": "https://different.example", "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "code_challenge_methods_supported": []string{"S256"}})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	base = hs.URL
	t.Cleanup(hs.Close)
	m := managerFor(t, MCPServerConfig{URL: base + "/mcp"})
	_, err := m.BeginLogin(context.Background(), "test", "http://example.com/callback")
	require.Error(t, err)
	_, err = m.BeginLogin(context.Background(), "test", "http://127.0.0.1:8765/callback")
	require.ErrorContains(t, err, "issuer mismatch")
}

func TestStaticAuthorizationHeaderAndErrorRedaction(t *testing.T) {
	t.Setenv("EA_MCP_TEST_TOKEN", "private-value")
	server := testServer()
	handler := protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, nil)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-value" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(hs.Close)
	m := managerFor(t, MCPServerConfig{URL: hs.URL, Headers: map[string]string{"Authorization": "Bearer ${EA_MCP_TEST_TOKEN}"}, Trust: true})
	m.Start(context.Background())
	waitConnected(t, m)
	result, err := m.AllTools()[0].Execute(context.Background(), json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.False(t, result.IsError)
	_, err = m.BeginLogin(context.Background(), "test", "http://127.0.0.1/callback")
	require.Error(t, err)
	require.False(t, strings.Contains(m.Servers()[0].Error, "private-value"))
}
