package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/sdk/config"
)

// BuiltinAppID is the pre-registered Feishu app for easyagent.
// Users scan a QR code with this app_id to authorize the bot.
const BuiltinAppID = "cli_a94f42eb71f9dccc"
const BuiltinAppSecret = "GJkOZto6hbML2QWdEkqE4chkwSiArva7"

// Feishu domain (mainland China). Use open.larksuite.com for international.
const FeishuDomain = "https://open.feishu.cn"

// OAuthResult holds the result of an OAuth login flow.
type OAuthResult struct {
	AppID        string
	AppSecret    string
	UserOpenID   string
	AccessToken  string
	BotName      string
	ErrorMessage string
}

// StartOAuthFlow initiates the Feishu OAuth login flow.
//
// 1. Starts a local HTTP server on a random port
// 2. Opens the browser to Feishu's authorize URL
// 3. Waits for the callback with the auth code
// 4. Exchanges code for user token
// 5. Returns the credentials
func StartOAuthFlow(ctx context.Context) (*OAuthResult, error) {
	// Pick a random port for the callback server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	slog.Info("oauth server started", "port", port)

	// Channel to receive the result
	resultCh := make(chan *OAuthResult, 1)

	var server *http.Server
	server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleOAuthCallback(w, r, resultCh, server)
		}),
	}

	// Start server in goroutine
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("oauth server error", "error", err)
		}
	}()

	// Build the authorize URL
	// Using the app authorization endpoint
	authorizeURL := fmt.Sprintf("%s/open-apis/authen/v1/authorize?%s",
		FeishuDomain,
		url.Values{
			"app_id":        {BuiltinAppID},
			"redirect_uri":  {redirectURI},
			"response_type": {"code"},
			"state":         {"easyagent"},
		}.Encode(),
	)

	// Open browser
	fmt.Printf("\n📱 请在浏览器中完成飞书授权:\n")
	fmt.Printf("   %s\n\n", authorizeURL)
	openBrowser(authorizeURL)

	// Wait for callback or timeout
	select {
	case result := <-resultCh:
		server.Shutdown(context.Background())
		if result.ErrorMessage != "" {
			return result, fmt.Errorf("%s", result.ErrorMessage)
		}
		return result, nil

	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return nil, fmt.Errorf("OAuth login timed out (5 minutes)")

	case <-ctx.Done():
		server.Shutdown(context.Background())
		return nil, ctx.Err()
	}
}

// handleOAuthCallback processes the redirect from Feishu after user authorizes.
func handleOAuthCallback(w http.ResponseWriter, r *http.Request, resultCh chan<- *OAuthResult, server *http.Server) {
	code := r.URL.Query().Get("code")
	if code == "" {
		// Show error page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `<html><body style="font-family:sans-serif;text-align:center;padding:50px">
<h2>❌ 授权失败</h2>
<p>未收到授权码，请重试。</p>
</body></html>`)
		resultCh <- &OAuthResult{ErrorMessage: "no auth code received"}
		return
	}

	// Success — show a nice page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<html><body style="font-family:sans-serif;text-align:center;padding:50px">
<h1>✅ 授权成功！</h1>
<p>请返回终端继续。凭证已自动保存。</p>
<p style="color:#999;font-size:small">你可以关闭此页面。</p>
</body></html>`)

	// Exchange code for token
	result := exchangeCodeForToken(code)
	resultCh <- &result
}

// exchangeCodeForToken exchanges the OAuth code for a user access token.
func exchangeCodeForToken(code string) OAuthResult {
	// Step 1: Get app_access_token
	appToken, err := getAppAccessToken(BuiltinAppID, BuiltinAppSecret)
	if err != nil {
		return OAuthResult{ErrorMessage: fmt.Sprintf("get app_access_token: %v", err)}
	}

	// Step 2: Exchange code for user_access_token
	tokenResp, err := getUserAccessToken(code, appToken)
	if err != nil {
		return OAuthResult{ErrorMessage: fmt.Sprintf("get user_access_token: %v", err)}
	}

	// Step 3: Get user info
	userInfo, err := getUserInfo(tokenResp.AccessToken)
	if err != nil {
		slog.Warn("failed to get user info", "error", err)
	}

	botName := ""
	openID := tokenResp.OpenID
	if userInfo != nil {
		botName = userInfo.Name
		openID = userInfo.OpenID
	}

	return OAuthResult{
		AppID:       BuiltinAppID,
		AppSecret:   BuiltinAppSecret,
		UserOpenID:  openID,
		AccessToken: tokenResp.AccessToken,
		BotName:     botName,
	}
}

// ── API calls ──────────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"redirect_uri"`
	OpenID       string `json:"open_id"`
}

// getAppAccessToken retrieves the app_access_token using app_id + app_secret.
func getAppAccessToken(appID, appSecret string) (string, error) {
	body := fmt.Sprintf(`{"app_id":"%s","app_secret":"%s"}`, appID, appSecret)

	req, err := http.NewRequest("POST", FeishuDomain+"/open-apis/auth/v3/app_access_token/internal",
		strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Code           int    `json:"code"`
		Msg            string `json:"msg"`
		AppAccessToken string `json:"app_access_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("feishu API error: code=%d msg=%s", result.Code, result.Msg)
	}

	return result.AppAccessToken, nil
}

// getUserAccessToken exchanges auth code for user access token.
func getUserAccessToken(code, appAccessToken string) (*tokenResponse, error) {
	body := fmt.Sprintf(`{"grant_type":"authorization_code","code":"%s"}`, code)

	req, err := http.NewRequest("POST", FeishuDomain+"/open-apis/authen/v1/oidc/access_token",
		strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+appAccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var wrapper struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken  string `json:"access_token"`
			TokenType    string `json:"token_type"`
			ExpiresIn    int    `json:"expires_in"`
			RefreshToken string `json:"refresh_token"`
			OpenID       string `json:"open_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse access_token response: %w", err)
	}
	if wrapper.Code != 0 {
		return nil, fmt.Errorf("feishu API error: code=%d msg=%s", wrapper.Code, wrapper.Msg)
	}

	return &tokenResponse{
		AccessToken:  wrapper.Data.AccessToken,
		TokenType:    wrapper.Data.TokenType,
		ExpiresIn:    wrapper.Data.ExpiresIn,
		RefreshToken: wrapper.Data.RefreshToken,
		OpenID:       wrapper.Data.OpenID,
	}, nil
}

// userInfoResponse holds the response from /open-apis/authen/v1/user_info
type userInfoResponse struct {
	Name   string `json:"name"`
	OpenID string `json:"open_id"`
}

// getUserInfo retrieves the authenticated user's info.
func getUserInfo(userAccessToken string) (*userInfoResponse, error) {
	req, err := http.NewRequest("GET", FeishuDomain+"/open-apis/authen/v1/user_info", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+userAccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var wrapper struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Name   string `json:"name"`
			OpenID string `json:"open_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse user_info response: %w", err)
	}
	if wrapper.Code != 0 {
		return nil, fmt.Errorf("feishu API error: code=%d msg=%s", wrapper.Code, wrapper.Msg)
	}

	return &userInfoResponse{
		Name:   wrapper.Data.Name,
		OpenID: wrapper.Data.OpenID,
	}, nil
}

// OpenBrowser tries to open the default browser with the given URL.
func OpenBrowser(url string) {
	openBrowser(url)
}

// openBrowser tries to open the default browser with the given URL.
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	if err != nil {
		slog.Warn("failed to open browser", "error", err, "url", url)
	}
}

// GatewayManager manages the feishu WebSocket gateway lifecycle.
// This is the runtime singleton for the /feishu start/stop commands.
type GatewayManager struct {
	mu      sync.Mutex
	gateway *Gateway
	cancel  context.CancelFunc
	running bool
}

// NewGatewayManager creates a new GatewayManager.
func NewGatewayManager() *GatewayManager {
	return &GatewayManager{}
}

// StartWithCredentials starts the feishu gateway using saved or provided credentials.
func (gm *GatewayManager) StartWithCredentials(creds Credentials, client *Client, handler MessageHandler) error {
	return gm.start(creds, client, handler, nil, nil)
}

// StartWithHandler starts the gateway with the full Feishu handler, including
// card actions and callback routing.
func (gm *GatewayManager) StartWithHandler(creds Credentials, client *Client, handler *Handler) error {
	if handler == nil {
		return fmt.Errorf("feishu handler is required")
	}
	return gm.start(creds, client, handler.Handle, handler.HandleCardAction, func(g *Gateway) { handler.SetGateway(g); handler.SetOwnerAccess(g.access) })
}

func (gm *GatewayManager) start(creds Credentials, client *Client, handler MessageHandler, cardHandler CardActionHandler, attach func(*Gateway)) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.running {
		return fmt.Errorf("feishu bot is already running; use /feishu stop first")
	}

	ctx, cancel := context.WithCancel(context.Background())
	gw := NewGateway(creds.AppID, creds.AppSecret, client, handler)
	owner := strings.TrimSpace(os.Getenv("FEISHU_OWNER_OPEN_ID"))
	if owner == "" {
		owner = creds.UserOpenID
	}
	statePath := os.Getenv("FEISHU_OWNER_STATE_FILE")
	if statePath == "" {
		statePath = filepath.Join(config.HomeDir(), "feishu-owner.json")
	}
	access, err := OpenOwnerAccess(statePath, owner)
	if err != nil {
		cancel()
		return fmt.Errorf("initialize owner access: %w", err)
	}
	gw.SetOwnerAccess(access)
	if cardHandler != nil {
		gw.SetCardActionHandler(cardHandler)
	}
	if attach != nil {
		attach(gw)
	}

	gm.gateway = gw
	gm.cancel = cancel
	gm.running = true

	go func() {
		defer func() {
			gm.mu.Lock()
			gm.running = false
			gm.cancel = nil
			gm.gateway = nil
			gm.mu.Unlock()
		}()

		slog.Info("feishu gateway starting", "app_id", creds.AppID)
		if err := gw.Start(ctx); err != nil {
			slog.Error("feishu gateway stopped", "error", err)
		}
	}()

	return nil
}

// Stop stops the feishu gateway.
func (gm *GatewayManager) Stop() {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.cancel != nil {
		gm.cancel()
	}
	gm.running = false
}

// IsRunning returns whether the gateway is currently active.
func (gm *GatewayManager) IsRunning() bool {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	return gm.running
}
