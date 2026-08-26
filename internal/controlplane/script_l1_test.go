// script_l1_test.go 测试 L1 输入校验加固（script.go timeout clamp + Enabled 409）。
//
// 覆盖：
//   - handleCreateScript：timeoutSec clamp 至 [1,600]；
//   - handleScriptExecute：禁用脚本返回 409 Conflict。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/store"
)

// TestHandleCreateScript_TimeoutClampLow 验证 timeoutSec<1 被 clamp 到 1。
func TestHandleCreateScript_TimeoutClampLow(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"clamped","content":"echo hi","language":"shell","timeoutSec":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var sc store.Script
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sc.TimeoutSec != 1 {
		t.Fatalf("TimeoutSec=%d, want 1 (clamped from 0)", sc.TimeoutSec)
	}
}

// TestHandleCreateScript_TimeoutClampHigh 验证 timeoutSec>600 被 clamp 到 600。
func TestHandleCreateScript_TimeoutClampHigh(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"clamped","content":"echo hi","language":"shell","timeoutSec":99999}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var sc store.Script
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sc.TimeoutSec != 600 {
		t.Fatalf("TimeoutSec=%d, want 600 (clamped from 99999)", sc.TimeoutSec)
	}
}

// TestHandleScriptExecute_DisabledScript409 验证禁用脚本 execute 返回 409。
func TestHandleScriptExecute_DisabledScript409(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	// 先创建脚本（CreateScript 默认 Enabled=true），再 UpdateScript 显式禁用。
	created := s.store.CreateScript("default", &store.Script{
		Name:       "disabled-script",
		Content:    "echo hi",
		Language:   "shell",
		TimeoutSec: 10,
	})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}
	created.Enabled = false
	if _, ok := s.store.UpdateScript("default", created); !ok {
		t.Fatal("UpdateScript failed")
	}
	body := `{"deviceID":"dev1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+created.ID+"/execute", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleScriptExecute_EnabledScriptOK 验证启用脚本 execute 不返回 409。
func TestHandleScriptExecute_EnabledScriptOK(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateScript("default", &store.Script{
		Name:       "enabled-script",
		Content:    "echo hi",
		Language:   "shell",
		Enabled:    true,
		TimeoutSec: 10,
	})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}
	body := `{"deviceID":"dev1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+created.ID+"/execute", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)
	if w.Code == http.StatusConflict {
		t.Fatalf("status=409, want non-409 for enabled script; body=%s", w.Body.String())
	}
}
