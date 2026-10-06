package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Registration implements the Feishu device-code app registration flow.
//
// This is a private (undocumented) API at accounts.feishu.cn that allows
// users to create a new PersonalAgent-type Feishu app by scanning a QR code,
// without needing to manually go to open.feishu.cn.
//
// Flow:
//   1. init  — check environment supports client_secret auth
//   2. begin — get device_code + QR URL
//   3. poll  — wait for user scan, returns client_id + client_secret
//
// Ported from hwjcode registration.ts.

const (
	accountsFeishuURL = "https://accounts.feishu.cn"
	accountsLarkURL   = "https://accounts.larksuite.com"
	registrationPath  = "/oauth/v1/app/registration"
	tpTag             = "pigo"
)

// BeginResult holds the result of the 'begin' registration step.
type BeginResult struct {
	DeviceCode string
	QRURL      string
	UserCode   string
	Interval   int // seconds between polls
	ExpireIn   int // total seconds before the code expires
}

// PollResult holds the result of a successful poll — the freshly created app credentials.
type PollResult struct {
	AppID     string
	AppSecret string
	Domain    string // "feishu" or "lark"
	OpenID    string
}

// RegistrationError represents an error from the registration API.
type RegistrationError struct {
	ErrorCode string
	Message   string
}

func (e *RegistrationError) Error() string {
	return fmt.Sprintf("registration error: %s — %s", e.ErrorCode, e.Message)
}

// postRegistration sends a POST to the registration endpoint.
func postRegistration(baseURL string, body map[string]string) (map[string]interface{}, error) {
	return postRegistrationContext(context.Background(), baseURL, body)
}

func postRegistrationContext(ctx context.Context, baseURL string, body map[string]string) (map[string]interface{}, error) {
	formData := url.Values{}
	for k, v := range body {
		formData.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+registrationPath, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse JSON (HTTP %d): %s", resp.StatusCode, truncate(string(raw), 200))
	}

	return result, nil
}

// InitRegistration checks if the registration environment supports client_secret auth.
func InitRegistration(domain string) error {
	return InitRegistrationContext(context.Background(), domain)
}

func InitRegistrationContext(ctx context.Context, domain string) error {
	baseURL := getAccountsURL(domain)
	res, err := postRegistrationContext(ctx, baseURL, map[string]string{"action": "init"})
	if err != nil {
		return fmt.Errorf("init registration: %w", err)
	}

	methodsRaw, ok := res["supported_auth_methods"]
	if !ok {
		return fmt.Errorf("registration init: no supported_auth_methods in response")
	}

	methods, ok := methodsRaw.([]interface{})
	if !ok {
		return fmt.Errorf("registration init: supported_auth_methods is not an array")
	}

	for _, m := range methods {
		if s, ok := m.(string); ok && s == "client_secret" {
			return nil
		}
	}

	return fmt.Errorf("registration env does not support client_secret auth")
}

// BeginRegistration starts the device-code flow and returns a QR URL for the user to scan.
func BeginRegistration(domain string) (*BeginResult, error) {
	return BeginRegistrationContext(context.Background(), domain)
}

func BeginRegistrationContext(ctx context.Context, domain string) (*BeginResult, error) {
	baseURL := getAccountsURL(domain)
	res, err := postRegistrationContext(ctx, baseURL, map[string]string{
		"action":            "begin",
		"archetype":         "PersonalAgent",
		"auth_method":       "client_secret",
		"request_user_info": "open_id tenant_brand",
	})
	if err != nil {
		return nil, fmt.Errorf("begin registration: %w", err)
	}

	deviceCode, _ := res["device_code"].(string)
	if deviceCode == "" {
		return nil, fmt.Errorf("registration did not return device_code: %v", res)
	}

	// Build QR URL
	qrURL, _ := res["verification_uri_complete"].(string)
	if qrURL != "" {
		sep := "?"
		if strings.Contains(qrURL, "?") {
			sep = "&"
		}
		qrURL = fmt.Sprintf("%s%sfrom=%s&tp=%s", qrURL, sep, tpTag, tpTag)
	} else {
		userCode, _ := res["user_code"].(string)
		openBase := "https://open.feishu.cn"
		if domain == "lark" {
			openBase = "https://open.larksuite.com"
		}
		qrURL = fmt.Sprintf("%s/page/launcher?user_code=%s&from=%s&tp=%s", openBase, userCode, tpTag, tpTag)
	}

	interval := 5
	if v, ok := res["interval"].(float64); ok {
		interval = int(v)
	}
	expireIn := 600
	if v, ok := res["expires_in"].(float64); ok {
		expireIn = int(v)
	} else if v, ok := res["expire_in"].(float64); ok {
		expireIn = int(v)
	}

	userCode, _ := res["user_code"].(string)

	return &BeginResult{
		DeviceCode: deviceCode,
		QRURL:      qrURL,
		UserCode:   userCode,
		Interval:   interval,
		ExpireIn:   expireIn,
	}, nil
}

// PollRegistration polls the registration endpoint until the user scans the QR code.
// Returns the newly created app credentials, or nil on timeout/denial.
//
// onProgress is called with a string (e.g. "...") on each poll iteration for UI feedback.
func PollRegistration(
	deviceCode string,
	interval int,
	expireIn int,
	domain string,
	onProgress func(dots string),
) (*PollResult, error) {
	deadline := time.Now().Add(time.Duration(expireIn) * time.Second)
	currentDomain := domain
	domainSwitched := false
	pollCount := 0

	baseURL := getAccountsURL(currentDomain)

	for time.Now().Before(deadline) {
		res, err := postRegistration(baseURL, map[string]string{
			"action":      "poll",
			"device_code": deviceCode,
		})
		if err != nil {
			// Network error — keep polling
			slog.Debug("registration poll error, retrying", "error", err)
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}

		pollCount++
		if onProgress != nil {
			onProgress(strings.Repeat(".", pollCount))
		}

		// Auto-detect domain (lark vs feishu)
		if userInfo, ok := res["user_info"].(map[string]interface{}); ok {
			if tenantBrand, ok := userInfo["tenant_brand"].(string); ok {
				if tenantBrand == "lark" && !domainSwitched {
					currentDomain = "lark"
					baseURL = getAccountsURL(currentDomain)
					domainSwitched = true
					slog.Info("registration domain switched to lark")
				}
			}
		}

		// Success — app created
		clientID, _ := res["client_id"].(string)
		clientSecret, _ := res["client_secret"].(string)
		if clientID != "" && clientSecret != "" {
			openID := ""
			if userInfo, ok := res["user_info"].(map[string]interface{}); ok {
				openID, _ = userInfo["open_id"].(string)
			}
			slog.Info("registration successful", "app_id", clientID, "domain", currentDomain)
			return &PollResult{
				AppID:     clientID,
				AppSecret: clientSecret,
				Domain:    currentDomain,
				OpenID:    openID,
			}, nil
		}

		// Check for denial / expiry
		errCode, _ := res["error"].(string)
		if errCode == "access_denied" || errCode == "expired_token" {
			return nil, fmt.Errorf("registration %s", errCode)
		}

		// authorization_pending — keep polling
		time.Sleep(time.Duration(interval) * time.Second)
	}

	return nil, fmt.Errorf("registration timed out after %d seconds", expireIn)
}

// ProbeCredentials validates app credentials by fetching tenant_access_token and bot info.
// Returns bot name + open_id, or nil if credentials are invalid.
func ProbeCredentials(appID, appSecret, domain string) (botName, botOpenID string, err error) {
	openBase := getOpenBase(domain)

	// 1. Get tenant_access_token
	tokenReq := map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	}
	tokenBody, err := json.Marshal(tokenReq)
	if err != nil {
		return "", "", fmt.Errorf("marshal token request: %w", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("%s/open-apis/auth/v3/tenant_access_token/internal", openBase),
		"application/json",
		strings.NewReader(string(tokenBody)),
	)
	if err != nil {
		return "", "", fmt.Errorf("get tenant_access_token: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", "", fmt.Errorf("decode token response: %w", err)
	}

	if tokenResp.TenantAccessToken == "" {
		return "", "", fmt.Errorf("no tenant_access_token in response (code=%d, msg=%s)", tokenResp.Code, tokenResp.Msg)
	}

	// 2. Get bot info
	botReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/open-apis/bot/v3/info", openBase), nil)
	botReq.Header.Set("Authorization", "Bearer "+tokenResp.TenantAccessToken)
	botClient := &http.Client{Timeout: 10 * time.Second}
	botResp, err := botClient.Do(botReq)
	if err != nil {
		// Token worked but bot info failed — still valid credentials
		slog.Warn("bot info fetch failed, but credentials are valid", "error", err)
		return "(unknown)", "", nil
	}
	defer botResp.Body.Close()

	var botData struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Bot  struct {
			AppName string `json:"app_name"`
			OpenID  string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.NewDecoder(botResp.Body).Decode(&botData); err != nil {
		return "(unknown)", "", nil
	}

	if botData.Code != 0 {
		// Bot might not be enabled yet, but credentials are valid
		slog.Warn("bot not ready", "code", botData.Code, "msg", botData.Msg)
		return "(unknown)", "", nil
	}

	return botData.Bot.AppName, botData.Bot.OpenID, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func getAccountsURL(domain string) string {
	if domain == "lark" {
		return accountsLarkURL
	}
	return accountsFeishuURL
}

func getOpenBase(domain string) string {
	if domain == "lark" {
		return "https://open.larksuite.com"
	}
	return "https://open.feishu.cn"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// PollRegistrationOnce makes one bounded, cancellable request. A nil result
// means authorization is pending; callers schedule subsequent polls themselves.
func PollRegistrationOnce(ctx context.Context, deviceCode, domain string) (*PollResult, string, error) {
	res, err := postRegistrationContext(ctx, getAccountsURL(domain), map[string]string{"action": "poll", "device_code": deviceCode})
	if err != nil {
		return nil, domain, err
	}
	info, _ := res["user_info"].(map[string]interface{})
	if info["tenant_brand"] == "lark" {
		domain = "lark"
	}
	id, _ := res["client_id"].(string)
	secret, _ := res["client_secret"].(string)
	owner, _ := info["open_id"].(string)
	if id != "" && secret != "" {
		if owner == "" {
			return nil, domain, fmt.Errorf("registration returned no owner")
		}
		return &PollResult{AppID: id, AppSecret: secret, OpenID: owner, Domain: domain}, domain, nil
	}
	code, _ := res["error"].(string)
	if code != "" && code != "authorization_pending" && code != "slow_down" {
		return nil, domain, fmt.Errorf("registration %s", code)
	}
	return nil, domain, nil
}
