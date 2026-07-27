package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestPostAlert_Generic 验证 generic 模式下 POST Alert JSON 到 webhook server。
func TestPostAlert_Generic(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		var buf strings.Builder
		b, _ := io.ReadAll(r.Body)
		buf.Write(b)
		received <- buf.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &proto.Alert{
		AlertID: "alert-1", TenantID: "t1", DeviceID: "dev-1", AgentID: "agent-1",
		Severity: "critical", Message: "task dead letter", CreatedAt: time.Date(2026, 7, 26, 22, 0, 0, 0, time.UTC),
	}
	if err := PostByType("generic", srv.URL, a); err != nil {
		t.Fatalf("PostByType(generic) err = %v", err)
	}

	body := <-received
	var parsed proto.Alert
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v\nbody: %s", err, body)
	}
	if parsed.AlertID != "alert-1" || parsed.Severity != "critical" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

// TestPostByType_Feishu 验证飞书格式消息体。
func TestPostByType_Feishu(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf strings.Builder
		b, _ := io.ReadAll(r.Body)
		buf.Write(b)
		received <- buf.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &proto.Alert{AlertID: "a1", Severity: "critical", DeviceID: "d1", AgentID: "ag1",
		Message: "test alert", CreatedAt: time.Date(2026, 7, 26, 22, 0, 0, 0, time.UTC)}
	if err := PostByType("feishu", srv.URL, a); err != nil {
		t.Fatalf("PostByType(feishu) err = %v", err)
	}

	body := <-received
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v\nbody: %s", err, body)
	}
	if parsed["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %v, want interactive", parsed["msg_type"])
	}
	card, ok := parsed["card"].(map[string]interface{})
	if !ok {
		t.Fatal("card missing")
	}
	header, ok := card["header"].(map[string]interface{})
	if !ok {
		t.Fatal("card.header missing")
	}
	if header["template"] != "red" {
		t.Fatalf("template = %v, want red", header["template"])
	}
	_, ok = card["elements"]
	if !ok {
		t.Fatal("card.elements missing")
	}
}

// TestPostByType_DingTalk 验证钉钉格式消息体。
func TestPostByType_DingTalk(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf strings.Builder
		b, _ := io.ReadAll(r.Body)
		buf.Write(b)
		received <- buf.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &proto.Alert{AlertID: "a1", Severity: "critical", DeviceID: "d1", AgentID: "ag1",
		Message: "test alert", CreatedAt: time.Date(2026, 7, 26, 22, 0, 0, 0, time.UTC)}
	if err := PostByType("dingtalk", srv.URL, a); err != nil {
		t.Fatalf("PostByType(dingtalk) err = %v", err)
	}

	body := <-received
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v\nbody: %s", err, body)
	}
	if parsed["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", parsed["msgtype"])
	}
	md, ok := parsed["markdown"].(map[string]interface{})
	if !ok {
		t.Fatal("markdown missing")
	}
	if md["title"] != "🔴 OpsMesh 告警" {
		t.Fatalf("title = %v", md["title"])
	}
	text, ok := md["text"].(string)
	if !ok || !strings.Contains(text, "OpsMesh") {
		t.Fatalf("text missing or malformed: %v", text)
	}
}

// TestPostByType_HTTPError 验证非 2xx 返回错误。
func TestPostByType_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &proto.Alert{AlertID: "a1", Severity: "critical"}
	if err := PostByType("generic", srv.URL, a); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestPostByType_EmptyURL 验证空 URL 直接返回 nil。
func TestPostByType_EmptyURL(t *testing.T) {
	if err := PostByType("generic", "", &proto.Alert{}); err != nil {
		t.Fatalf("empty URL err = %v", err)
	}
}

// TestPostByType_NilAlert 验证 nil alert 直接返回 nil。
func TestPostByType_NilAlert(t *testing.T) {
	if err := PostByType("generic", "http://example.com", nil); err != nil {
		t.Fatalf("nil alert err = %v", err)
	}
}
