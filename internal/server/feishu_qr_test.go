package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hwj123hwj/easyagent/internal/feishu"
)

func TestQRConfirmAndExpiry(t *testing.T) {
	saves, polls := 0, 0
	q := &feishuQR{begin: func(context.Context) (*feishu.BeginResult, error) {
		return &feishu.BeginResult{DeviceCode: "issued", QRURL: "https://open.feishu.cn/page/launcher?user_code=qa", Interval: 3, ExpireIn: 60}, nil
	}, poll: func(context.Context, string, string) (*feishu.PollResult, string, error) {
		polls++
		return &feishu.PollResult{AppID: "qa", AppSecret: "private", OpenID: "owner", Domain: "feishu"}, "feishu", nil
	}, save: func(c feishu.Credentials) error {
		saves++
		if c.UserOpenID != "owner" {
			t.Fatal("missing owner")
		}
		return nil
	}}
	call := func(action, code string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q.handle(action, w, httptest.NewRequest("POST", "/", strings.NewReader(`{"device_code":"`+code+`"}`)))
		return w
	}
	if w := call("begin", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("poll", "forged"); w.Code != 409 || polls != 0 {
		t.Fatal("unissued code reached provider")
	}
	if w := call("confirm", "issued"); w.Code != 409 {
		t.Fatal("confirmed before authorization")
	}
	w := call("poll", "issued")
	if w.Code != 200 || saves != 0 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("poll must not save or return credentials", w.Body.String())
	}
	// Pending registration was superseded; late confirmation cannot overwrite it.
	call("begin", "")
	if w := call("confirm", "issued"); w.Code != 409 {
		t.Fatal("stale result retained")
	}
	call("poll", "issued")
	q.expires = time.Now().Add(-time.Second)
	if w := call("confirm", "issued"); w.Code != 409 || saves != 0 {
		t.Fatal("expired authorization saved")
	}
	call("begin", "")
	call("poll", "issued")
	if w := call("confirm", "issued"); w.Code != 200 || saves != 1 {
		t.Fatal("explicit confirmation did not save")
	}
	if w := call("confirm", "issued"); w.Code != 409 || saves != 1 {
		t.Fatal("confirmation replay")
	}
}

func TestQRPendingThrottleAndUntrustedURL(t *testing.T) {
	calls := 0
	q := &feishuQR{code: "issued", domain: "feishu", expires: time.Now().Add(time.Minute), interval: 3 * time.Second, poll: func(context.Context, string, string) (*feishu.PollResult, string, error) {
		calls++
		return nil, "lark", nil
	}}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		q.handle("poll", w, httptest.NewRequest("POST", "/", strings.NewReader(`{"device_code":"issued"}`)))
		var res map[string]any
		json.Unmarshal(w.Body.Bytes(), &res)
		if w.Code != 200 || res["authorized"] != false {
			t.Fatal(w.Body.String())
		}
	}
	if calls != 1 || q.domain != "lark" {
		t.Fatal("throttle/domain lost", calls, q.domain)
	}
	q.begin = func(context.Context) (*feishu.BeginResult, error) {
		return &feishu.BeginResult{DeviceCode: "bad", QRURL: "https://attacker.test/?code=x"}, nil
	}
	w := httptest.NewRecorder()
	q.handle("begin", w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 502 || q.code != "" {
		t.Fatal("untrusted URL accepted")
	}
}

func TestQRAuthAndManagedBoundary(t *testing.T) {
	for _, tc := range []struct {
		key, provided string
		managed       bool
		want          int
	}{{"", "", false, 403}, {"key", "", false, 401}, {"key", "key", true, 409}} {
		mux := http.NewServeMux()
		registerFeishuQR(mux, tc.key, tc.managed)
		for _, action := range []string{"begin", "poll", "confirm"} {
			r := httptest.NewRequest("POST", "/settings/feishu/qr/"+action, nil)
			r.Header.Set("Authorization", "Bearer "+tc.provided)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(tc, w.Code)
			}
		}
	}
}
