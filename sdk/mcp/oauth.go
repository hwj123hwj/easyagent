package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// The official SDK v1.4 transport is used without experimental build tags.
// Its OAuth discovery/authorization helpers require mcp_go_client_oauth, so
// application login uses public protocol types and x/oauth2's PKCE/token flow.
type authRecord struct {
	Token        *oauth2.Token   `json:"token,omitempty"`
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret,omitempty"`
	Endpoint     oauth2.Endpoint `json:"endpoint"`
	Scopes       []string        `json:"scopes,omitempty"`
	RedirectURL  string          `json:"redirect_url"`
	Resource     string          `json:"resource"`
}
type authStore struct{ path string }

var authFileMu sync.Mutex
var authRefreshLocks sync.Map

func (s *authStore) lock(ctx context.Context, endpoint string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, _ := authRefreshLocks.LoadOrStore(s.path+"\x00"+endpoint, make(chan struct{}, 1))
	lock := value.(chan struct{})
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *authStore) read() (map[string]authRecord, error) {
	result := map[string]authRecord{}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, errors.New("cannot read saved MCP credentials")
	}
	if json.Unmarshal(data, &result) != nil {
		return nil, errors.New("invalid saved MCP credentials")
	}
	return result, nil
}
func (s *authStore) put(endpoint string, record *authRecord) error {
	unlock, err := s.lock(context.Background(), endpoint)
	if err != nil {
		return err
	}
	defer unlock()
	authFileMu.Lock()
	defer authFileMu.Unlock()
	records, err := s.read()
	if err != nil {
		return err
	}
	if record == nil {
		delete(records, endpoint)
	} else {
		records[endpoint] = *record
	}
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateWrite(s.path, append(raw, '\n'))
}
func (s *authStore) token(ctx context.Context, endpoint string) (string, error) {
	unlock, err := s.lock(ctx, endpoint)
	if err != nil {
		return "", err
	}
	defer unlock()
	authFileMu.Lock()
	records, err := s.read()
	authFileMu.Unlock()
	if err != nil {
		return "", err
	}
	record, ok := records[endpoint]
	if !ok || record.Token == nil {
		return "", nil
	}
	if record.Token.Valid() {
		return record.Token.AccessToken, nil
	}
	if record.Token.RefreshToken == "" {
		return "", errNeedsAuth
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, oauthHTTPClient(record.Resource))
	cfg := record.config()
	token, err := cfg.TokenSource(ctx, record.Token).Token()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errNeedsAuth
	}
	record.Token = token
	// Only the affected endpoint waits for its refresh; file writes remain
	// serialized without holding a global lock across network requests.
	authFileMu.Lock()
	defer authFileMu.Unlock()
	records, err = s.read()
	if err != nil {
		return "", err
	}
	records[endpoint] = record
	raw, _ := json.MarshalIndent(records, "", "  ")
	if err = atomicPrivateWrite(s.path, append(raw, '\n')); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}
func (r authRecord) config() *oauth2.Config {
	return &oauth2.Config{ClientID: r.ClientID, ClientSecret: r.ClientSecret, Endpoint: r.Endpoint, Scopes: r.Scopes, RedirectURL: r.RedirectURL}
}

// LoginInfo contains no credentials. CompleteLogin consumes the state once.
type LoginInfo struct {
	AuthorizationURL string    `json:"authorization_url"`
	State            string    `json:"state"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type pendingLogin struct {
	info     LoginInfo
	record   authRecord
	verifier string
	cancel   context.CancelFunc
}
type authMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

var resourceMetadataPattern = regexp.MustCompile(`resource_metadata="([^"]+)"`)

func oauthHTTPClient(resource string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &resourceTokenTransport{resource: resource, base: http.DefaultTransport}, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}

type resourceTokenTransport struct {
	resource string
	base     http.RoundTripper
}

func (t *resourceTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.resource != "" && req.Method == http.MethodPost && req.Body != nil && strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		values.Set("resource", t.resource)
		req = req.Clone(req.Context())
		encoded := values.Encode()
		req.Body = io.NopCloser(strings.NewReader(encoded))
		req.ContentLength = int64(len(encoded))
	}
	return t.base.RoundTrip(req)
}
func getOAuthJSON(ctx context.Context, raw string, result any) error {
	if err := validateOAuthURL(raw); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	resp, err := oauthHTTPClient("").Do(req)
	if err != nil {
		return errors.New("OAuth metadata unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("OAuth metadata unavailable")
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result) != nil {
		return errors.New("invalid OAuth metadata")
	}
	return nil
}
func discoverOAuth(ctx context.Context, endpoint, override string, challenge http.Header) (*authMetadata, []string, error) {
	if err := validateOAuthURL(endpoint); err != nil {
		return nil, nil, err
	}
	if override != "" {
		var meta authMetadata
		if err := getOAuthJSON(ctx, override, &meta); err != nil {
			return nil, nil, err
		}
		return validateMetadata(&meta, "")
	}
	metadataURL := ""
	if match := resourceMetadataPattern.FindStringSubmatch(challenge.Get("WWW-Authenticate")); len(match) > 1 {
		metadataURL = match[1]
	}
	u, _ := url.Parse(endpoint)
	if metadataURL == "" {
		metadataURL = u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource" + u.Path
	}
	var resource oauthex.ProtectedResourceMetadata
	if err := getOAuthJSON(ctx, metadataURL, &resource); err != nil {
		if err = getOAuthJSON(ctx, u.Scheme+"://"+u.Host+"/.well-known/oauth-protected-resource", &resource); err != nil {
			return nil, nil, err
		}
	}
	if strings.TrimSuffix(resource.Resource, "/") != strings.TrimSuffix(endpoint, "/") || len(resource.AuthorizationServers) == 0 {
		return nil, nil, errors.New("OAuth resource metadata does not match this MCP server")
	}
	issuer := resource.AuthorizationServers[0]
	if err := validateOAuthURL(issuer); err != nil {
		return nil, nil, err
	}
	i, _ := url.Parse(issuer)
	candidates := []string{i.Scheme + "://" + i.Host + "/.well-known/oauth-authorization-server" + strings.TrimSuffix(i.Path, "/"), strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"}
	var meta authMetadata
	found := false
	for _, candidate := range candidates {
		if getOAuthJSON(ctx, candidate, &meta) == nil {
			found = true
			break
		}
	}
	if !found {
		return nil, nil, errors.New("OAuth authorization server metadata unavailable")
	}
	checked, _, err := validateMetadata(&meta, issuer)
	return checked, resource.ScopesSupported, err
}
func validateMetadata(meta *authMetadata, issuer string) (*authMetadata, []string, error) {
	if issuer != "" && meta.Issuer != issuer {
		return nil, nil, errors.New("OAuth issuer mismatch")
	}
	for _, raw := range []string{meta.Issuer, meta.AuthorizationEndpoint, meta.TokenEndpoint} {
		if err := validateOAuthURL(raw); err != nil {
			return nil, nil, err
		}
	}
	if meta.RegistrationEndpoint != "" {
		if err := validateOAuthURL(meta.RegistrationEndpoint); err != nil {
			return nil, nil, err
		}
	}
	supported := false
	for _, method := range meta.CodeChallengeMethodsSupported {
		if method == "S256" {
			supported = true
		}
	}
	if !supported {
		return nil, nil, errors.New("OAuth server must support PKCE S256")
	}
	return meta, meta.ScopesSupported, nil
}

func (m *Manager) BeginLogin(ctx context.Context, name, redirectURL string) (*LoginInfo, error) {
	if err := validateOAuthURL(redirectURL); err != nil {
		return nil, err
	}
	srv, err := m.server(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	entry := srv.entry
	conn := srv.client
	m.mu.RUnlock()
	cfg := entry.Config
	if cfg.Transport() == "stdio" {
		return nil, errors.New("stdio servers do not use OAuth")
	}
	for key := range cfg.Headers {
		if strings.EqualFold(key, "Authorization") {
			return nil, errors.New("update configured Authorization header instead of OAuth login")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	challenge := http.Header{}
	if conn != nil {
		conn.mu.Lock()
		challenge = conn.challenge.Clone()
		conn.mu.Unlock()
	}
	if challenge.Get("WWW-Authenticate") == "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfg.endpoint(), nil)
		resp, e := oauthHTTPClient("").Do(req)
		if e == nil {
			challenge = resp.Header.Clone()
			resp.Body.Close()
		}
	}
	oauthCfg := cfg.OAuth
	if oauthCfg == nil {
		oauthCfg = &OAuthConfig{}
	}
	meta, scopes, err := discoverOAuth(ctx, cfg.endpoint(), oauthCfg.AuthServerMetadataURL, challenge)
	if err != nil {
		return nil, err
	}
	if len(oauthCfg.Scopes) > 0 {
		scopes = oauthCfg.Scopes
	}
	clientID, clientSecret := oauthCfg.ClientID, oauthCfg.ClientSecret
	if clientSecret != "" {
		clientSecret, err = expandEnv(clientSecret)
		if err != nil {
			return nil, err
		}
	}
	if clientID == "" {
		if meta.RegistrationEndpoint == "" {
			return nil, errors.New("OAuth server requires a configured clientId")
		}
		registration := map[string]any{"redirect_uris": []string{redirectURL}, "client_name": "EasyAgent", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
		raw, _ := json.Marshal(registration)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := oauthHTTPClient("").Do(req)
		if err != nil {
			return nil, errors.New("OAuth client registration failed")
		}
		defer resp.Body.Close()
		var registered struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return nil, errors.New("OAuth client registration rejected")
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&registered) != nil || registered.ClientID == "" {
			return nil, errors.New("invalid OAuth registration response")
		}
		clientID, clientSecret = registered.ClientID, registered.ClientSecret
	}
	record := authRecord{ClientID: clientID, ClientSecret: clientSecret, Endpoint: oauth2.Endpoint{AuthURL: meta.AuthorizationEndpoint, TokenURL: meta.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}, Scopes: scopes, RedirectURL: redirectURL, Resource: cfg.endpoint()}
	for _, method := range meta.TokenEndpointAuthMethodsSupported {
		if method == "client_secret_basic" && clientSecret != "" {
			record.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
			break
		}
	}
	stateBytes := make([]byte, 32)
	if _, err = rand.Read(stateBytes); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(stateBytes)
	verifier := oauth2.GenerateVerifier()
	authorizationURL := record.config().AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", cfg.endpoint()))
	info := LoginInfo{AuthorizationURL: authorizationURL, State: state, ExpiresAt: time.Now().Add(10 * time.Minute)}
	_, loginCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	if old := m.logins[name]; old != nil {
		old.cancel()
	}
	m.logins[name] = &pendingLogin{info: info, record: record, verifier: verifier, cancel: loginCancel}
	m.mu.Unlock()
	return &info, nil
}
func (m *Manager) CompleteLogin(ctx context.Context, name, state, code string) error {
	m.mu.Lock()
	pending := m.logins[name]
	if pending == nil || time.Now().After(pending.info.ExpiresAt) || subtle.ConstantTimeCompare([]byte(state), []byte(pending.info.State)) != 1 || code == "" {
		m.mu.Unlock()
		return errors.New("invalid or expired OAuth state")
	}
	delete(m.logins, name)
	m.mu.Unlock()
	defer pending.cancel()
	srv, err := m.server(name)
	if err != nil {
		return err
	}
	m.mu.RLock()
	endpoint := srv.entry.Config.endpoint()
	m.mu.RUnlock()
	if endpoint != pending.record.Resource {
		return errors.New("MCP server changed during sign-in")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, oauthHTTPClient(endpoint))
	token, err := pending.record.config().Exchange(ctx, code, oauth2.VerifierOption(pending.verifier), oauth2.SetAuthURLParam("resource", endpoint))
	if err != nil {
		return errors.New("OAuth token exchange failed")
	}
	pending.record.Token = token
	if err = m.auth.put(endpoint, &pending.record); err != nil {
		return err
	}
	return m.Reconnect(ctx, name)
}
func (m *Manager) Logout(ctx context.Context, name string) error {
	srv, err := m.server(name)
	if err != nil {
		return err
	}
	m.mu.RLock()
	endpoint := srv.entry.Config.endpoint()
	m.mu.RUnlock()
	if endpoint == "" {
		return errors.New("stdio servers do not use OAuth")
	}
	if err = m.auth.put(endpoint, nil); err != nil {
		return err
	}
	m.mu.Lock()
	if pending := m.logins[name]; pending != nil {
		pending.cancel()
		delete(m.logins, name)
	}
	m.mu.Unlock()
	m.stop(srv)
	m.mu.Lock()
	if m.servers[name] == srv {
		srv.state = StatusNeedsAuth
		srv.err = "Sign-in required"
	}
	m.mu.Unlock()
	m.changed()
	return nil
}
