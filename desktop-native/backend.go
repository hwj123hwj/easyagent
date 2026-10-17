package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// The native client uses the authenticated core REST/WS protocol.
// The UI owns all projections; background work only delivers immutable results.
type backendClient struct {
	URL, Token string
	HTTP       *http.Client
	mu         sync.Mutex
	ws         *websocket.Conn
}

func newBackendClient(raw, token string) (*backendClient, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("服务地址必须是无凭据、参数和片段的 HTTP(S) 地址")
	}
	return &backendClient{URL: u.String(), Token: token, HTTP: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func requestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "native-" + hex.EncodeToString(b[:])
}
func (b *backendClient) request(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.URL+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return b.execute(req, out)
}
func (b *backendClient) execute(req *http.Request, out any) error {
	if b.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.Token)
	}
	res, err := b.HTTP.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("服务处理超时，草稿已保留。目录操作请检查 macOS 文件与文件夹访问提示，或用浏览目录重新选择项目")
		}
		return errors.New("无法连接服务，请检查主机地址和网络")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
	if err != nil {
		return errors.New("读取服务响应失败")
	}
	if len(data) > 32<<20 {
		return errors.New("服务响应超过 32 MiB 限制")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var v struct{ Error, Message string }
		_ = json.Unmarshal(data, &v)
		if res.StatusCode == 401 || res.StatusCode == 403 {
			return fmt.Errorf("服务拒绝访问（HTTP %d），请检查认证或权限", res.StatusCode)
		}
		msg := v.Error
		if msg == "" {
			msg = v.Message
		}
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		if b.Token != "" {
			msg = strings.ReplaceAll(msg, b.Token, "[已隐藏]")
		}
		return fmt.Errorf("%s（HTTP %d）", msg, res.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("服务响应格式无效")
	}
	return nil
}
func (b *backendClient) upload(ctx context.Context, sid, filename string) (map[string]any, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 4<<20 {
		return nil, errors.New("单个附件必须为 1 字节至 4 MiB")
	}
	var result map[string]any
	err = b.request(ctx, "POST", sessionPath(sid)+"/attachments", map[string]any{"name": filepath.Base(filename), "data": base64.StdEncoding.EncodeToString(data)}, &result)
	return result, err
}

func sessionPath(id string) string { return "/sessions/" + url.PathEscape(id) }
func query(values map[string]string) string {
	v := url.Values{}
	for k, s := range values {
		if s != "" {
			v.Set(k, s)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}
func (b *backendClient) send(value any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ws == nil {
		return errors.New("连接已断开，草稿已保留")
	}
	_ = b.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return b.ws.WriteJSON(value)
}
func (b *backendClient) closeSocket() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ws != nil {
		_ = b.ws.Close()
		b.ws = nil
	}
}
func (b *backendClient) observe(ctx context.Context, emit func(envelope), status func(bool)) {
	defer b.closeSocket()
	backoff := time.Second
	for ctx.Err() == nil {
		u, _ := url.Parse(b.URL + "/ws")
		if u.Scheme == "https" {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
		h := http.Header{}
		if b.Token != "" {
			h.Set("Authorization", "Bearer "+b.Token)
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), h)
		if err == nil {
			conn.SetReadLimit(32 << 20)
			b.mu.Lock()
			b.ws = conn
			b.mu.Unlock()
			status(true)
			backoff = time.Second
			closed := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					_ = conn.Close()
				case <-closed:
				}
			}()
			for {
				var msg envelope
				if conn.ReadJSON(&msg) != nil {
					break
				}
				emit(msg)
			}
			close(closed)
			b.closeSocket()
		}
		if ctx.Err() != nil {
			return
		}
		status(false)
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}
