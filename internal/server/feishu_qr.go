package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/hwj123hwj/easyagent/internal/feishu"
)

// A single host-local registration. Polling never writes credentials: the
// user explicitly confirms a completed registration before replacing them.
type feishuQR struct {
	mu            sync.Mutex
	begin         func(context.Context) (*feishu.BeginResult, error)
	poll          func(context.Context, string, string) (*feishu.PollResult, string, error)
	save          func(feishu.Credentials) error
	code, domain  string
	expires, next time.Time
	interval      time.Duration
	result        *feishu.PollResult
}

func registerFeishuQR(mux *http.ServeMux, key string, managed bool) {
	q := &feishuQR{
		begin: func(ctx context.Context) (*feishu.BeginResult, error) {
			if err := feishu.InitRegistrationContext(ctx, "feishu"); err != nil {
				return nil, err
			}
			return feishu.BeginRegistrationContext(ctx, "feishu")
		},
		poll: feishu.PollRegistrationOnce, save: feishu.SaveCredentials,
	}
	for _, action := range []string{"begin", "poll", "confirm"} {
		mux.HandleFunc("POST /settings/feishu/qr/"+action, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			if key == "" {
				http.Error(w, `{"error":"飞书配置需要先设置 EA_API_KEY"}`, 403)
				return
			}
			if bearerToken(r) != key {
				http.Error(w, `{"error":"未授权"}`, 401)
				return
			}
			if managed {
				http.Error(w, `{"error":"托管桥接请使用自建应用凭据与配对设置"}`, 409)
				return
			}
			q.handle(action, w, r)
		})
	}
}

func (q *feishuQR) handle(action string, w http.ResponseWriter, r *http.Request) {
	// Serialize requests so a stale registration cannot replace a newer one.
	q.mu.Lock()
	defer q.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if action == "begin" {
		q.code, q.result = "", nil
		b, err := q.begin(ctx)
		if err != nil {
			http.Error(w, `{"error":"无法生成飞书授权，请稍后重试或使用自建应用"}`, 502)
			return
		}
		u, err := url.Parse(b.QRURL)
		if err != nil || u.Scheme != "https" || (u.Host != "open.feishu.cn" && u.Host != "accounts.feishu.cn" && u.Host != "open.larksuite.com" && u.Host != "accounts.larksuite.com") || b.DeviceCode == "" {
			http.Error(w, `{"error":"飞书返回了无效授权链接"}`, 502)
			return
		}
		b.Interval = max(3, min(b.Interval, 30))
		b.ExpireIn = max(1, min(b.ExpireIn, 600))
		q.code, q.domain = b.DeviceCode, "feishu"
		q.interval, q.expires = time.Duration(b.Interval)*time.Second, time.Now().Add(time.Duration(b.ExpireIn)*time.Second)
		q.next = time.Time{}
		json.NewEncoder(w).Encode(map[string]any{"device_code": b.DeviceCode, "qr_url": b.QRURL, "user_code": b.UserCode, "interval": b.Interval, "expire_in": b.ExpireIn})
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || req.DeviceCode == "" {
		http.Error(w, `{"error":"请求格式无效"}`, 400)
		return
	}
	if req.DeviceCode != q.code || !time.Now().Before(q.expires) {
		if !time.Now().Before(q.expires) {
			q.result = nil
		}
		http.Error(w, `{"error":"授权已过期或已被新的授权取代"}`, 409)
		return
	}
	if action == "confirm" {
		if q.result == nil {
			http.Error(w, `{"error":"请先完成飞书授权"}`, 409)
			return
		}
		if ctx.Err() != nil {
			return
		}
		p := q.result
		if err := q.save(feishu.Credentials{AppID: p.AppID, AppSecret: p.AppSecret, UserOpenID: p.OpenID, Platform: p.Domain}); err != nil {
			http.Error(w, `{"error":"保存飞书凭据失败，请检查目录权限"}`, 500)
			return
		}
		q.code, q.result = "", nil
		json.NewEncoder(w).Encode(map[string]any{"success": true, "app_id": p.AppID})
		return
	}
	if q.result == nil && !time.Now().Before(q.next) {
		q.next = time.Now().Add(q.interval)
		p, domain, err := q.poll(ctx, q.code, q.domain)
		if err != nil {
			http.Error(w, `{"error":"飞书授权查询失败，请重新授权"}`, 502)
			return
		}
		q.domain = domain
		if ctx.Err() != nil {
			return
		}
		q.result = p
	}
	json.NewEncoder(w).Encode(map[string]any{"authorized": q.result != nil})
}
