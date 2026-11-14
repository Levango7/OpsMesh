// netsec_test.go 验证 device-svc netsec 端点的三态语义：
//   - 合法动作 + 已注入 TaskDispatcher → 202 + taskID
//   - 未知动作 → 400
//   - 未注入 TaskDispatcher → 503（显式失败优先于假成功）
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDispatcher 记录最后一次下发请求并返回固定 taskID。
type fakeDispatcher struct {
	lastDeviceID string
	lastScript   string
	lastType     string
	calls        int
}

func (f *fakeDispatcher) DispatchTask(ctx context.Context, deviceID, script, taskType string) (string, error) {
	f.calls++
	f.lastDeviceID, f.lastScript, f.lastType = deviceID, script, taskType
	return "task-netsec-1", nil
}

func newNetSecMux(t *testing.T, td TaskDispatcher) *http.ServeMux {
	t.Helper()
	g := &Gateway{}
	if td != nil {
		g.SetTaskDispatcher(td)
	}
	mux := http.NewServeMux()
	g.RegisterNetSecRoutes(mux, func(h http.Handler) http.Handler { return h })
	return mux
}

func doNetSec(t *testing.T, mux *http.ServeMux, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestNetSec_AllCapabilitiesDispatch202 六端点合法动作均应 202 并下发任务。
func TestNetSec_AllCapabilitiesDispatch202(t *testing.T) {
	td := &fakeDispatcher{}
	mux := newNetSecMux(t, td)

	cases := map[string]string{
		"/api/v1/netsec/ips":       "status",
		"/api/v1/netsec/siem":      "status",
		"/api/v1/netsec/honeypot":  "status",
		"/api/v1/netsec/traffic":   "status",
		"/api/v1/netsec/vpn":       "status",
		"/api/v1/netsec/zerotrust": "status",
	}
	for path, action := range cases {
		rec := doNetSec(t, mux, path, `{"deviceID":"dev-1","action":"`+action+`"}`)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s = %d, want 202; body=%s", path, rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		if resp["taskID"] != "task-netsec-1" {
			t.Fatalf("%s: taskID = %v, want task-netsec-1", path, resp["taskID"])
		}
	}
	if td.calls != len(cases) {
		t.Fatalf("DispatchTask 调用 %d 次, want %d", td.calls, len(cases))
	}
	if td.lastDeviceID != "dev-1" {
		t.Fatalf("lastDeviceID = %q, want dev-1", td.lastDeviceID)
	}
	if td.lastScript == "" {
		t.Fatal("lastScript 为空")
	}
}

// TestNetSec_UnknownAction400 未知动作必须 400。
func TestNetSec_UnknownAction400(t *testing.T) {
	mux := newNetSecMux(t, &fakeDispatcher{})
	rec := doNetSec(t, mux, "/api/v1/netsec/ips", `{"deviceID":"dev-1","action":"nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestNetSec_MissingDeviceID400 deviceID 必填。
func TestNetSec_MissingDeviceID400(t *testing.T) {
	mux := newNetSecMux(t, &fakeDispatcher{})
	rec := doNetSec(t, mux, "/api/v1/netsec/ips", `{"action":"status"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing deviceID = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestNetSec_NoDispatcher503 未注入 TaskDispatcher 时必须 503（不得假成功）。
func TestNetSec_NoDispatcher503(t *testing.T) {
	mux := newNetSecMux(t, nil)
	rec := doNetSec(t, mux, "/api/v1/netsec/ips", `{"deviceID":"dev-1","action":"status"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no dispatcher = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

// TestNetSec_GetMethod405 非 POST 必须 405。
func TestNetSec_GetMethod405(t *testing.T) {
	mux := newNetSecMux(t, &fakeDispatcher{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/netsec/ips", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d, want 405", rec.Code)
	}
}
