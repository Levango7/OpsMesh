package controlplane

// notify_create_nilguard_test.go —「Create* 落库失败 ⇒ store 返回 nil」时 handler 的守卫。
//
// 背景：sqlstore 的 20 来个 Create* 在 SQLStore INSERT 失败时返回 nil
// （FIXPLAN-phase1-6.md H2 定的"Create 类一律返回 nil"，杜绝 201 假成功）。
// 契约的下半场在调用方：每个收 Create* 返回值的 handler 都必须 nil-check。
//
// 2026-10-12 审计（31 处生产调用点逐点过）发现两个洞：
//   - createNotifyChannel / createNotifyTemplate 拿到 nil 后直接用 created.ID
//     写审计 ⇒ nil 解引用 panic ⇒ net/http 掐断连接，客户端连 500 都收不到；
//   - saveAlertRule 是 void 签名，create handler 无论成败都 201。
// 本文件锁住三者：nil ⇒ 500（不是 panic、不是 201），并保留 201 正路径对照。
//
// 判据延伸：audit 脚本只覆盖这三个已知洞；新增 Create* 调用点时按同款补测试。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/store"
)

// nilCreateStore 包装真实 Store，把三个"创建"方法强制返回 nil——
// 与 SQLStore 的 DB 失败路径逐字同形（storefail.Record 后 return nil）。
type nilCreateStore struct {
	store.Store
}

func (n *nilCreateStore) CreateNotifyChannel(_ *store.NotifyChannel) *store.NotifyChannel {
	return nil
}

func (n *nilCreateStore) CreateNotifyTemplate(_ *store.NotifyTemplate) *store.NotifyTemplate {
	return nil
}

func (n *nilCreateStore) CreateAlertRule(_ *store.AlertRule) *store.AlertRule {
	return nil
}

// TestCreateNotifyChannel_StoreNilReturns500 落库失败必须 500：此前 created.ID
// 直接 panic（连接被掐断，客户端连错误码都看不到）。
func TestCreateNotifyChannel_StoreNilReturns500(t *testing.T) {
	s := newAlertsTestServer()
	s.store = &nilCreateStore{Store: s.store}

	body := `{"name":"ops-email","type":"email","config":"{}","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store 返回 nil 时应 500，实得 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed") {
		t.Errorf("错误体应说明失败，实得 %q", rec.Body.String())
	}
}

// TestCreateNotifyTemplate_StoreNilReturns500 同上（通知模板路径同洞）。
func TestCreateNotifyTemplate_StoreNilReturns500(t *testing.T) {
	s := newAlertsTestServer()
	s.store = &nilCreateStore{Store: s.store}

	body := `{"name":"ops-tpl","type":"alert","title":"t","body":"b","format":"text"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-templates", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store 返回 nil 时应 500，实得 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateAlertRule_StoreNilReturns500 saveAlertRule 从 void 改 bool 后，
// 落库失败不再"无论成败都 201"。
func TestCreateAlertRule_StoreNilReturns500(t *testing.T) {
	s := newM4TestServer()
	s.store = &nilCreateStore{Store: s.store}

	body, err := json.Marshal(map[string]any{
		"metric": "cpu.usage", "op": ">", "threshold": 80.0,
		"severity": "critical", "message": "CPU too high", "enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "m4-alert")
	rec := httptest.NewRecorder()
	s.handleAlertRules(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store 返回 nil 时应 500，实得 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateNotify_StoreHealthyStill201 正路径对照：内存后端下三者照旧 201
// （守卫不误伤正常创建）。
func TestCreateNotify_StoreHealthyStill201(t *testing.T) {
	s := newAlertsTestServer()

	body := `{"name":"ops-email","type":"email","config":"{}","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("healthy store create channel = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/notify-templates",
		strings.NewReader(`{"name":"tpl","type":"alert","title":"t","body":"b"}`))
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec = httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("healthy store create template = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}
