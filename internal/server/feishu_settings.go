package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"

	"github.com/hwj123hwj/easyagent/internal/feishu"
)

// The operator explicitly opts in with a fixed environment file used by the
// systemd bridge. Requests cannot choose paths, units, or shell commands.
type feishuSettings struct {
	mu                 sync.Mutex
	envFile, ownerFile string
	run                func(context.Context, string, ...string) (string, error)
}

func newFeishuSettings() *feishuSettings {
	return &feishuSettings{envFile: os.Getenv("EA_FEISHU_ENV_FILE"), ownerFile: os.Getenv("FEISHU_OWNER_STATE_FILE"), run: func(ctx context.Context, name string, args ...string) (string, error) {
		b, err := exec.CommandContext(ctx, name, args...).Output()
		return strings.TrimSpace(string(b)), err
	}}
}
func (f *feishuSettings) command(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return f.run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}
func (f *feishuSettings) values() (map[string]string, error) {
	v, err := godotenv.Read(f.envFile)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	return v, err
}
func (f *feishuSettings) status(ctx context.Context) (map[string]any, error) {
	v, err := f.values()
	if err != nil {
		return nil, err
	}
	services := map[string]any{}
	for _, name := range []string{"core", "bridge"} {
		unit := "easyagent-" + name + ".service"
		active, e := f.command(ctx, "is-active", unit)
		if e != nil && active == "" {
			active = "unknown"
		}
		enabled, e := f.command(ctx, "is-enabled", unit)
		if e != nil && enabled == "" {
			enabled = "unknown"
		}
		services[name] = map[string]string{"state": active, "autostart": enabled}
	}
	lingerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	linger, err := f.run(lingerCtx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value")
	if err != nil || linger == "" {
		linger = "unknown"
	}
	result := map[string]any{"managed": true, "app_id": v["FEISHU_APP_ID"], "secret_configured": v["FEISHU_APP_SECRET"] != "", "services": services, "linger": linger}
	// Read fresh state: the bridge may have completed pairing since the last GET.
	var owner struct {
		Owner   string    `json:"owner"`
		Expires time.Time `json:"expires"`
	}
	b, err := os.ReadFile(f.ownerFile)
	if err == nil && json.Unmarshal(b, &owner) == nil {
		result["paired"] = owner.Owner != ""
		result["pairing_expires"] = owner.Expires
	} else {
		result["pairing_unavailable"] = true
	}
	return result, nil
}

var feishuCredential = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func privateReplace(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".feishu-env-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (f *feishuSettings) save(ctx context.Context, id, secret string) error {
	v, err := f.values()
	if err != nil {
		return errors.New("无法读取飞书配置")
	}
	if v["FEISHU_APP_ID"] != "" && id != v["FEISHU_APP_ID"] {
		return errors.New("当前机器人已配置，网页仅支持更新密钥；更换机器人需要重新配置使用者绑定")
	}
	if secret == "" {
		if id != v["FEISHU_APP_ID"] {
			return errors.New("首次配置请同时填写 App ID 和 App Secret")
		}
		secret = v["FEISHU_APP_SECRET"]
	}
	if !feishuCredential.MatchString(id) || !feishuCredential.MatchString(secret) {
		return errors.New("请填写有效的 App ID 和 App Secret（字母、数字、下划线或连字符）")
	}
	old, err := os.ReadFile(f.envFile)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return errors.New("无法读取飞书配置")
	}
	// Preserve every unrelated deployment setting and comment verbatim.
	lines := strings.Split(string(old), "\n")
	kept := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		key, _, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		if ok && (strings.TrimSpace(key) == "FEISHU_APP_ID" || strings.TrimSpace(key) == "FEISHU_APP_SECRET") {
			continue
		}
		kept = append(kept, line)
	}
	kept = append(kept, `FEISHU_APP_ID="`+id+`"`, `FEISHU_APP_SECRET="`+secret+`"`, "")
	if err = privateReplace(f.envFile, []byte(strings.Join(kept, "\n"))); err != nil {
		return errors.New("保存失败，请检查配置目录权限")
	}
	if _, err = f.command(ctx, "restart", "easyagent-bridge.service"); err != nil {
		var restoreErr error
		if existed {
			restoreErr = privateReplace(f.envFile, old)
		} else {
			restoreErr = os.Remove(f.envFile)
		}
		if restoreErr != nil {
			return errors.New("桥接重启失败，旧配置恢复失败，请检查主机配置")
		}
		_, _ = f.command(context.Background(), "restart", "easyagent-bridge.service")
		return errors.New("桥接重启失败，已恢复旧配置")
	}
	return nil
}
func (s *Server) registerFeishuSettings(mux *http.ServeMux) {
	f := newFeishuSettings()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if s.apiKey == "" {
			http.Error(w, `{"error":"飞书配置需要先设置 EA_API_KEY"}`, http.StatusForbidden)
			return
		}
		if f.envFile == "" || f.ownerFile == "" {
			if r.Method == "GET" && r.URL.Path == "/settings/feishu" {
				// Even if systemd is not managing the bridge, expose saved local credentials if present
				creds, err := feishu.LoadCredentials()
				if err != nil {
					http.Error(w, `{"error":"无法读取飞书凭据"}`, 500)
					return
				}
				appID := ""
				secretConfigured := false
				if creds != nil {
					appID = creds.AppID
					secretConfigured = creds.AppSecret != ""
				}
				json.NewEncoder(w).Encode(map[string]any{
					"managed":           false,
					"app_id":            appID,
					"secret_configured": secretConfigured,
					"local_mode":        true,
				})
				return
			}
			http.Error(w, `{"error":"此服务器未启用飞书配置管理"}`, http.StatusServiceUnavailable)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/settings/feishu/pairing" {
			var state struct {
				Owner   string    `json:"owner"`
				Code    string    `json:"code"`
				Expires time.Time `json:"expires"`
			}
			b, err := os.ReadFile(f.ownerFile)
			if err != nil || json.Unmarshal(b, &state) != nil {
				http.Error(w, `{"error":"配对状态暂不可用，请稍后刷新"}`, 503)
				return
			}
			if state.Owner != "" {
				http.Error(w, `{"error":"已经配对，无需再次绑定"}`, 409)
				return
			}
			if state.Code == "" || time.Now().After(state.Expires) {
				http.Error(w, `{"error":"配对码已过期，请保存配置并重启桥接后重试"}`, 409)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"command": "/pair " + state.Code, "expires": state.Expires})
			return
		}
		if r.Method == "PUT" {
			var request struct {
				AppID  string `json:"app_id"`
				Secret string `json:"app_secret"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&request) != nil {
				http.Error(w, `{"error":"配置格式无效"}`, 400)
				return
			}
			if err := f.save(r.Context(), strings.TrimSpace(request.AppID), strings.TrimSpace(request.Secret)); err != nil {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
				return
			}
		}
		status, err := f.status(r.Context())
		if err != nil {
			http.Error(w, `{"error":"无法读取飞书配置"}`, 500)
			return
		}
		json.NewEncoder(w).Encode(status)
	}
	mux.HandleFunc("GET /settings/feishu", handler)
	mux.HandleFunc("PUT /settings/feishu", handler)
	mux.HandleFunc("GET /settings/feishu/pairing", handler)

	registerFeishuQR(mux, s.apiKey, f.envFile != "" || f.ownerFile != "")
}
