package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteErrorShape 钉住错误体形状：`{"error":"<msg>"}` + JSON Content-Type + 指定状态码。
// 这是前端 request.js `j?.error || err.message` 能否显示真实原因的唯一依据
// —— 退化成 text/plain 时 axios 不解析，用户只会看到 "Request failed with status code 401"。
func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusUnauthorized, "auth: 权限不足")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d，want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q，want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体不是合法 JSON: %v（原文 %q）", err, rec.Body.String())
	}
	if body["error"] != "auth: 权限不足" {
		t.Fatalf("error = %q，want 原文", body["error"])
	}
	if len(body) != 1 {
		t.Fatalf("错误体应只含 error 一个键，实得 %v", body)
	}
}

// TestWriteErrorEscapes 错误消息里的引号/尖括号必须被正确转义（消息可能来自 err.Error()）。
func TestWriteErrorEscapes(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusForbidden, `tenant: "acme" <x>`)
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("含特殊字符的消息未被正确转义: %v（原文 %q）", err, rec.Body.String())
	}
	if body["error"] != `tenant: "acme" <x>` {
		t.Fatalf("error = %q，原文被改写", body["error"])
	}
}
