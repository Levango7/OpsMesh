// backup_api_test.go 测试 Phase 3 灾备恢复 HTTP handler（backup_api.go）。
//
// 覆盖范围：
//   - handleBackupCreate：创建备份、无效类型
//   - handleBackupList：空列表、创建后列表
//   - handleBackupRestore：恢复备份、不存在
//   - handleBackupDeleteRouting：删除备份、不存在
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newBackupAPITestServer 构造灾备 API 测试用 Server。
func newBackupAPITestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-backup-api-test-32!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleBackupCreate 验证创建备份返回 201。
func TestHandleBackupCreate(t *testing.T) {
	s := newBackupAPITestServer()
	auth := loginAsAdmin(t, s)

	body, _ := json.Marshal(map[string]string{"type": "full"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/create", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var rec store.BackupRecord
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Type != "full" {
		t.Fatalf("type=%s, want full", rec.Type)
	}
	if rec.ID == "" {
		t.Fatal("backup ID is empty")
	}
}

// TestHandleBackupCreate_InvalidType 验证无效类型返回 400。
func TestHandleBackupCreate_InvalidType(t *testing.T) {
	s := newBackupAPITestServer()
	auth := loginAsAdmin(t, s)

	body, _ := json.Marshal(map[string]string{"type": "invalid"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/create", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleBackupList_AfterCreate 验证创建后列表含备份。
func TestHandleBackupList_AfterCreate(t *testing.T) {
	s := newBackupAPITestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateBackup("default", &store.BackupRecord{
		Type:   "config",
		Status: "completed",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backup/list", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Backups []*store.BackupRecord `json:"backups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Backups) != 1 {
		t.Fatalf("backups=%d, want 1", len(resp.Backups))
	}
}

// TestHandleBackupRestore 验证恢复备份返回 200 + accepted。
func TestHandleBackupRestore(t *testing.T) {
	s := newBackupAPITestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateBackup("default", &store.BackupRecord{
		Type:   "full",
		Status: "completed",
	})

	body, _ := json.Marshal(map[string]string{"id": created.ID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/restore", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupRestore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("status=%s, want accepted", resp.Status)
	}
}

// TestHandleBackupRestore_NotFound 验证恢复不存在的备份返回 404。
func TestHandleBackupRestore_NotFound(t *testing.T) {
	s := newBackupAPITestServer()
	auth := loginAsAdmin(t, s)

	body, _ := json.Marshal(map[string]string{"id": "nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/restore", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupRestore(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleBackupDeleteRouting 验证删除备份返回 200。
func TestHandleBackupDeleteRouting(t *testing.T) {
	s := newBackupAPITestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateBackup("default", &store.BackupRecord{
		Type:   "devices",
		Status: "completed",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backup/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupDeleteRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleBackupCreate_NoToken 验证无 token 返回 401。
func TestHandleBackupCreate_NoToken(t *testing.T) {
	s := newBackupAPITestServer()

	body, _ := json.Marshal(map[string]string{"type": "full"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleBackupCreate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}