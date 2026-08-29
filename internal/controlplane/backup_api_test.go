// backup_api_test.go 测试 Phase 3 灾备恢复 HTTP handler（backup_api.go）。
//
// 覆盖范围（真实备份/恢复，非模拟）：
//   - handleBackupCreate：创建备份返回 201、异步归档后 status=completed 且回填真实 Size/Path、无效类型
//   - handleBackupList：空列表、创建后列表
//   - handleBackupRestore：恢复备份返回真实恢复计数、不存在返回 404、归档缺失返回 404
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
	"os"
	"path/filepath"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newBackupAPITestServer 构造灾备 API 测试用 Server（backupDir 指向临时目录）。
func newBackupAPITestServer(t *testing.T) *Server {
	t.Helper()
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-backup-api-test-32!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
		backupDir:    t.TempDir(),
	}
}

// waitBackupStatus 轮询备份记录直至 status 达到期望值（异步归档 goroutine 完成）。
func waitBackupStatus(t *testing.T, s *Server, id, want string) *store.BackupRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := s.store.GetBackup("default", id); ok && rec.Status == want {
			return rec
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec, _ := s.store.GetBackup("default", id)
	t.Fatalf("backup %s 未在超时内达到 status=%s（当前: %+v）", id, want, rec)
	return nil
}

// TestHandleBackupCreate 验证创建备份返回 201，且异步归档后 status=completed + 真实 Size/Path。
func TestHandleBackupCreate(t *testing.T) {
	s := newBackupAPITestServer(t)
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
	// 异步归档完成后：status=completed + 真实 Path/Size>0。
	done := waitBackupStatus(t, s, rec.ID, "completed")
	if done.Path == "" {
		t.Fatal("completed backup Path 为空（应回填真实归档路径）")
	}
	if _, err := os.Stat(done.Path); err != nil {
		t.Fatalf("归档文件不存在: %v", err)
	}
	if done.Size <= 0 {
		t.Fatalf("Size=%d, want > 0（真实归档字节数）", done.Size)
	}
}

// TestHandleBackupCreate_InvalidType 验证无效类型返回 400。
func TestHandleBackupCreate_InvalidType(t *testing.T) {
	s := newBackupAPITestServer(t)
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
	s := newBackupAPITestServer(t)
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

// TestHandleBackupRestore 验证真实恢复：创建备份→异步归档→恢复→数据写回 store。
func TestHandleBackupRestore(t *testing.T) {
	s := newBackupAPITestServer(t)
	auth := loginAsAdmin(t, s)

	// 预置一条设备，备份后删除，恢复验证数据回写。
	s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: "restore-dev-1", Segment: "default", TenantID: "default", State: "online"})
	// 通过 HTTP 创建备份并等待归档完成。
	body, _ := json.Marshal(map[string]string{"type": "full"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/create", bytes.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleBackupCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d, body=%s", w.Code, w.Body.String())
	}
	var created store.BackupRecord
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	done := waitBackupStatus(t, s, created.ID, "completed")

	// 删除设备模拟数据丢失（退役后 Retired=true，restore 后应被快照覆写为 Retired=false）。
	s.store.RetireDevice("restore-dev-1", "default")
	if d := s.store.Device("restore-dev-1"); d == nil || !d.Retired {
		t.Fatal("退役前置断言失败：设备应处于 retired 状态")
	}

	// 恢复。
	restoreBody, _ := json.Marshal(map[string]string{"id": done.ID})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/backup/restore", bytes.NewReader(restoreBody))
	req2.Header.Set("Authorization", auth)
	w2 := httptest.NewRecorder()
	s.handleBackupRestore(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("restore status=%d, body=%s", w2.Code, w2.Body.String())
	}
	var resp struct {
		Status   string         `json:"status"`
		Restored map[string]int `json:"restored"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "restored" {
		t.Fatalf("status=%s, want restored", resp.Status)
	}
	if resp.Restored["devices"] < 1 {
		t.Fatalf("restored.devices=%d, want >=1", resp.Restored["devices"])
	}
	// 数据已写回 store（快照覆盖退役标记）。
	if d := s.store.Device("restore-dev-1"); d == nil || d.Retired {
		t.Fatal("restore 后设备 restore-dev-1 应存在且未退役（快照已写回）")
	}
}

// TestHandleBackupRestore_NotFound 验证恢复不存在的备份返回 404。
func TestHandleBackupRestore_NotFound(t *testing.T) {
	s := newBackupAPITestServer(t)
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

// TestHandleBackupRestore_ArchiveMissing 验证备份记录存在但归档文件缺失返回 404。
func TestHandleBackupRestore_ArchiveMissing(t *testing.T) {
	s := newBackupAPITestServer(t)
	auth := loginAsAdmin(t, s)

	created := s.store.CreateBackup("default", &store.BackupRecord{
		Type:   "full",
		Status: "completed",
		Path:   filepath.Join(s.backupDir, "backup-does-not-exist.tar.gz"),
	})

	body, _ := json.Marshal(map[string]string{"id": created.ID})
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
	s := newBackupAPITestServer(t)
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
	s := newBackupAPITestServer(t)

	body, _ := json.Marshal(map[string]string{"type": "full"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleBackupCreate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}
