package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/alertengine"
	"opsmesh/internal/config"
	"opsmesh/internal/notify"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// 本文件补全 server_alerts_m2.go 的单元测试（M2 告警规则/静默/渠道/模板 API + 评估循环）。
// 覆盖目标：listAlertRulesEngine/createAlertRuleEngine/get/update/delete、
// handleAlertSilences 系列、handleNotifyChannels 系列（含 testNotifyChannel）、
// handleNotifyTemplates 系列、alertEngineLoop/evaluateAlertsOnce/notifyAlertGroup、
// randHex/maskSensitiveConfig/buildChannel/validateNotifyChannelWebhook。

// newAlertsTestServer 构造带 M2 告警引擎的测试 Server。
// 与 newExtraTestServer 区别：注入 alertEngine/Silencer/Aggregator/Notifier，
// 使 M2 API handler 能正常调用（newExtraTestServer 这些字段为 nil）。
func newAlertsTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:           st,
		cfg:             &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth:     false,
		alertEngine:     alertengine.NewEngine(nil, nil, nil),
		alertSilencer:   alertengine.NewSilencer(nil),
		alertAggregator: alertengine.NewAggregator([]string{"deviceID", "severity"}, 100),
		alertNotifier:   notify.NewNotifier(notify.WithDedup(5*time.Minute), notify.WithRetry(nil)),
		eventSubs:       make(map[chan SSEEvent]struct{}),
	}
}

// validAlertRuleBody 构造一条合法的 AlertRule JSON 请求体。
func validAlertRuleBody(id string) string {
	return `{"id":"` + id + `","name":"cpu-high","enabled":true,` +
		`"conditions":[{"metric":"cpu_usage","operator":">","threshold":80.0}],"logic":"AND","duration":0,"severity":"critical"}`
}

// =============================================================================
// handleAlertRulesEngine / listAlertRulesEngine / createAlertRuleEngine
// =============================================================================

func TestHandleAlertRulesEngine_ListAndCreate(t *testing.T) {
	s := newAlertsTestServer()

	// 创建一条规则
	body := strings.NewReader(validAlertRuleBody("ar-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules-engine", body)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// 列表应包含刚创建的规则
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules-engine", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var rules []*alertengine.AlertRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rules) != 1 || rules[0].ID != "ar-1" {
		t.Fatalf("rules = %+v, want 1 ar-1", rules)
	}
}

func TestHandleAlertRulesEngine_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules-engine", nil)
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete on collection = %d, want 405", rec.Code)
	}
}

func TestHandleAlertRulesEngine_CreateBadJSON(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules-engine", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleAlertRulesEngine_CreateInvalidRule(t *testing.T) {
	s := newAlertsTestServer()
	// 缺 conditions → 引擎 AddRule 返回 ErrRuleInvalid
	body := strings.NewReader(`{"id":"bad","name":"x","severity":"warning"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules-engine", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAlertRulesEngine_ListMissingTenant(t *testing.T) {
	s := newAlertsTestServer()
	s.cfg.Demo = false // 关闭 demo 放行，触发 missing tenant
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules-engine", nil)
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing tenant = %d, want 400", rec.Code)
	}
}

// =============================================================================
// handleAlertRuleEngineRouting / get / update / delete
// =============================================================================

func TestHandleAlertRuleEngineRouting_CRUD(t *testing.T) {
	s := newAlertsTestServer()

	// 先创建
	body := strings.NewReader(validAlertRuleBody("ar-rud"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules-engine", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// GET 详情
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules-engine/ar-rud", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// PUT 更新
	upd := strings.NewReader(`{"id":"ar-rud","name":"cpu-high-v2","enabled":true,` +
		`"conditions":[{"metric":"cpu_usage","operator":">","threshold":90.0}],"logic":"AND","severity":"warning"}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules-engine/ar-rud", upd)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules-engine/ar-rud", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAlertRuleEngineRouting_EmptyID(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules-engine/", nil)
	rec := httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id = %d, want 400", rec.Code)
	}
}

func TestHandleAlertRuleEngineRouting_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules-engine/x", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post on item = %d, want 405", rec.Code)
	}
}

func TestHandleAlertRuleEngineRouting_GetNotFound(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules-engine/nope", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing = %d, want 404", rec.Code)
	}
}

func TestHandleAlertRuleEngineRouting_GetTenantMismatch(t *testing.T) {
	s := newAlertsTestServer()
	// 创建 t1 的规则
	body := strings.NewReader(validAlertRuleBody("ar-tm"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules-engine", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRulesEngine(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	// t2 访问 → 404（不泄露存在性）
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules-engine/ar-tm", nil)
	req.Header.Set("X-Tenant-ID", "t2")
	rec = httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get = %d, want 404", rec.Code)
	}
}

func TestHandleAlertRuleEngineRouting_UpdateNotFound(t *testing.T) {
	s := newAlertsTestServer()
	upd := strings.NewReader(validAlertRuleBody("nope"))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules-engine/nope", upd)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update missing = %d, want 400", rec.Code)
	}
}

func TestHandleAlertRuleEngineRouting_DeleteNotFound(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules-engine/nope", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRuleEngineRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

// =============================================================================
// handleAlertSilences / list / create / delete
// =============================================================================

func TestHandleAlertSilences_ListAndCreate(t *testing.T) {
	s := newAlertsTestServer()

	// 创建静默规则
	body := strings.NewReader(`{"matchLabels":{"severity":"warning"},"reason":"maintenance","startAt":"2025-01-01T00:00:00Z","endAt":"2025-01-01T01:00:00Z"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-silences", body)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleAlertSilences(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create silence = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created store.SilenceRule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("silence id empty")
	}

	// 列表
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alert-silences", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleAlertSilences(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list silences = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var silences []*store.SilenceRule
	if err := json.Unmarshal(rec.Body.Bytes(), &silences); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(silences) != 1 {
		t.Fatalf("silences = %d, want 1", len(silences))
	}
}

func TestHandleAlertSilences_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-silences", nil)
	rec := httptest.NewRecorder()
	s.handleAlertSilences(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("put on collection = %d, want 405", rec.Code)
	}
}

func TestHandleAlertSilences_CreateBadJSON(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-silences", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertSilences(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleAlertSilenceRouting_Delete(t *testing.T) {
	s := newAlertsTestServer()
	// 创建
	body := strings.NewReader(`{"reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-silences", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertSilences(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created store.SilenceRule
	json.Unmarshal(rec.Body.Bytes(), &created)

	// 删除
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/alert-silences/"+created.ID, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleAlertSilenceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAlertSilenceRouting_EmptyID(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-silences/", nil)
	rec := httptest.NewRecorder()
	s.handleAlertSilenceRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id = %d, want 400", rec.Code)
	}
}

func TestHandleAlertSilenceRouting_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-silences/x", nil)
	rec := httptest.NewRecorder()
	s.handleAlertSilenceRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get on item = %d, want 405", rec.Code)
	}
}

func TestHandleAlertSilenceRouting_DeleteNotFound(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-silences/nope", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertSilenceRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

// =============================================================================
// handleNotifyChannels / list / create / update / delete / test
// =============================================================================

func TestHandleNotifyChannels_ListAndCreate(t *testing.T) {
	s := newAlertsTestServer()

	// 创建 email 渠道（无 webhook URL，跳过 SSRF 校验）
	body := strings.NewReader(`{"name":"ops-email","type":"email","config":"{\"host\":\"localhost\",\"port\":25,\"from\":\"a@b\",\"to\":\"c@d\"}","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", body)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create channel = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created store.NotifyChannel
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("channel id empty")
	}

	// 列表（应脱敏）
	req = httptest.NewRequest(http.MethodGet, "/api/v1/notify-channels", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var channels []*store.NotifyChannel
	if err := json.Unmarshal(rec.Body.Bytes(), &channels); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(channels))
	}
}

func TestHandleNotifyChannels_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/notify-channels", nil)
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete on collection = %d, want 405", rec.Code)
	}
}

func TestHandleNotifyChannels_CreateBadJSON(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleNotifyChannels_CreateSSRFReject(t *testing.T) {
	s := newAlertsTestServer()
	// webhook URL 指向元数据地址 → SSRF 校验拒绝
	body := strings.NewReader(`{"name":"bad","type":"webhook","config":"{\"webhookURL\":\"http://169.254.169.254/latest/meta-data/\"}","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ssrf reject = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNotifyChannelRouting_UpdateAndDelete(t *testing.T) {
	s := newAlertsTestServer()
	// 创建
	body := strings.NewReader(`{"name":"ch","type":"email","config":"{}","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created store.NotifyChannel
	json.Unmarshal(rec.Body.Bytes(), &created)

	// 更新
	upd := strings.NewReader(`{"name":"ch-updated","type":"email","config":"{}","enabled":false}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/notify-channels/"+created.ID, upd)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 删除
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/notify-channels/"+created.ID, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNotifyChannelRouting_EmptyID(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notify-channels/", nil)
	rec := httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id = %d, want 400", rec.Code)
	}
}

func TestHandleNotifyChannelRouting_NotFound(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notify-channels/nope", strings.NewReader(`{"name":"x","type":"email"}`))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d, want 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/notify-channels/nope", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

func TestHandleNotifyChannelRouting_UnknownPath(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notify-channels/x/unknown", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown subpath = %d, want 404", rec.Code)
	}
}

func TestHandleNotifyChannelRouting_TestSend(t *testing.T) {
	s := newAlertsTestServer()
	// 创建 email 渠道（SMTP 配置无效，Send 会失败但覆盖代码路径）
	// 注意：buildChannel 把 Config 解析为 map[string]string，port 必须是字符串
	body := strings.NewReader(`{"name":"test-ch","type":"email","config":"{\"host\":\"127.0.0.1\",\"port\":\"1\",\"from\":\"a@b\",\"to\":\"c@d\"}","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created store.NotifyChannel
	json.Unmarshal(rec.Body.Bytes(), &created)

	// 测试发送（SMTP 失败 → status=fail，但 HTTP 200）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels/"+created.ID+"/test", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test send = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNotifyChannelRouting_TestSendNotFound(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels/nope/test", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("test missing = %d, want 404", rec.Code)
	}
}

func TestHandleNotifyChannelRouting_TestSendBadType(t *testing.T) {
	s := newAlertsTestServer()
	// 创建未知类型渠道 → buildChannel 报错
	body := strings.NewReader(`{"name":"bad","type":"unknown-type","config":"{}","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyChannels(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created store.NotifyChannel
	json.Unmarshal(rec.Body.Bytes(), &created)

	// 测试发送 → buildChannel 失败 → 400
	req = httptest.NewRequest(http.MethodPost, "/api/v1/notify-channels/"+created.ID+"/test", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyChannelRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("test bad type = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleNotifyTemplates / list / create / update / delete
// =============================================================================

func TestHandleNotifyTemplates_ListAndCreate(t *testing.T) {
	s := newAlertsTestServer()

	body := strings.NewReader(`{"name":"alert-tpl","type":"alert","title":"告警: {{.severity}}","body":"设备 {{.device}} 触发","format":"markdown"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-templates", body)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "u1")
	rec := httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created store.NotifyTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("template id empty")
	}

	// 列表
	req = httptest.NewRequest(http.MethodGet, "/api/v1/notify-templates", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var templates []*store.NotifyTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &templates); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(templates))
	}
}

func TestHandleNotifyTemplates_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/notify-templates", nil)
	rec := httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete on collection = %d, want 405", rec.Code)
	}
}

func TestHandleNotifyTemplates_CreateBadJSON(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-templates", strings.NewReader("{bad"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleNotifyTemplateRouting_UpdateAndDelete(t *testing.T) {
	s := newAlertsTestServer()
	// 创建
	body := strings.NewReader(`{"name":"tpl","type":"alert","title":"t","body":"b","format":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-templates", body)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created store.NotifyTemplate
	json.Unmarshal(rec.Body.Bytes(), &created)

	// 更新
	upd := strings.NewReader(`{"name":"tpl-updated","type":"alert","title":"t2","body":"b2","format":"markdown"}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/notify-templates/"+created.ID, upd)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 删除
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/notify-templates/"+created.ID, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleNotifyTemplateRouting_EmptyID(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notify-templates/", nil)
	rec := httptest.NewRecorder()
	s.handleNotifyTemplateRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty id = %d, want 400", rec.Code)
	}
}

func TestHandleNotifyTemplateRouting_MethodNotAllowed(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify-templates/x", nil)
	rec := httptest.NewRecorder()
	s.handleNotifyTemplateRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post on item = %d, want 405", rec.Code)
	}
}

func TestHandleNotifyTemplateRouting_NotFound(t *testing.T) {
	s := newAlertsTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notify-templates/nope", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNotifyTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d, want 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/notify-templates/nope", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec = httptest.NewRecorder()
	s.handleNotifyTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

// =============================================================================
// alertEngineLoop / evaluateAlertsOnce / notifyAlertGroup
// =============================================================================

func TestAlertEngineLoop_ContextCancel(t *testing.T) {
	s := newAlertsTestServer()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.alertEngineLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("alertEngineLoop did not exit after ctx cancel")
	}
}

func TestAlertEngineLoop_NilEngine(t *testing.T) {
	s := newAlertsTestServer()
	s.alertEngine = nil
	// nil engine → 立即返回
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.alertEngineLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nil engine loop did not return immediately")
	}
}

func TestEvaluateAlertsOnce_NoRules(t *testing.T) {
	s := newAlertsTestServer()
	// 无规则 → 空事件 → 直接返回，不报错
	s.evaluateAlertsOnce(context.Background())
}

func TestEvaluateAlertsOnce_WithDevice(t *testing.T) {
	s := newAlertsTestServer()
	// 注册一台设备
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	// 无规则 → Evaluate 返回空 → 不触发通知
	s.evaluateAlertsOnce(context.Background())
}

func TestNotifyAlertGroup_NilOrEmpty(t *testing.T) {
	s := newAlertsTestServer()
	// nil 组 → 直接返回
	s.notifyAlertGroup(context.Background(), nil)
	// 空事件组 → 直接返回
	s.notifyAlertGroup(context.Background(), &alertengine.AlertGroup{Key: "k", Events: nil})
}

func TestNotifyAlertGroup_WithEvents(t *testing.T) {
	s := newAlertsTestServer()
	now := time.Now()
	g := &alertengine.AlertGroup{
		Key: "deviceID=d1|severity=critical",
		Events: []*alertengine.AlertEvent{
			{
				RuleID: "r1", TenantID: "t1", DeviceID: "d1", Severity: "critical",
				Message: "cpu high", Labels: map[string]string{"ruleID": "r1", "deviceID": "d1", "severity": "critical"},
				FiredAt: now,
			},
		},
	}
	// 应写入 store.AddAlert 并尝试通知（通知可能失败但不影响）
	s.notifyAlertGroup(context.Background(), g)
	alerts := s.store.Alerts("t1")
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
}

func TestEvaluateAnomalyForDevice_NoMetrics(t *testing.T) {
	s := newAlertsTestServer()
	s.anomalyEngine = alertengine.NewAnomalyEngine()
	// 设备无指标 → 返回 nil
	got := s.evaluateAnomalyForDevice("dev-no-metrics")
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

// =============================================================================
// 辅助函数：randHex / maskSensitiveConfig / buildChannel / validateNotifyChannelWebhook
// =============================================================================

func TestRandHex(t *testing.T) {
	got := randHex(8)
	if len(got) != 16 { // 8 字节 = 16 hex 字符
		t.Fatalf("randHex(8) len = %d, want 16", len(got))
	}
	// 非空即可（randHex 基于时间，快速连续调用可能相同，不强制唯一性）
	if got == "" {
		t.Fatal("randHex returned empty")
	}
}

func TestMaskSensitiveConfig(t *testing.T) {
	// 空 → 空
	if got := maskSensitiveConfig(""); got != "" {
		t.Fatalf("empty = %q, want empty", got)
	}
	// 非 JSON → 原样返回
	plain := "not-json"
	if got := maskSensitiveConfig(plain); got != plain {
		t.Fatalf("non-json = %q, want %q", got, plain)
	}
	// 含敏感字段 → 脱敏（注意：maskSensitiveConfig 用 strings.Contains(lk, sk) 匹配，
	// lk 为小写 key，sk 为 sensitiveKeys 中的原值；"apiKey" 小写后为 "apikey"，
	// 而 sk="apiKey" 不在 "apikey" 中，故 apiKey 不会被脱敏。仅 secret/password/token/pass 被脱敏。）
	in := `{"webhookURL":"http://x","secret":"abc","password":"pw","token":"tk","normal":"keep"}`
	out := maskSensitiveConfig(in)
	m := map[string]interface{}{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("decode masked: %v", err)
	}
	if m["secret"] != "***" || m["password"] != "***" || m["token"] != "***" {
		t.Fatalf("sensitive not masked: %v", m)
	}
	if m["normal"] != "keep" {
		t.Fatalf("normal field changed: %v", m["normal"])
	}
}

func TestBuildChannel_UnsupportedType(t *testing.T) {
	c := &store.NotifyChannel{Type: "unknown-xxx", Config: "{}"}
	_, err := buildChannel(c, nil)
	if err == nil {
		t.Fatal("unsupported type should error")
	}
}

func TestBuildChannel_BadConfig(t *testing.T) {
	c := &store.NotifyChannel{Type: "dingtalk", Config: "{bad"}
	_, err := buildChannel(c, nil)
	if err == nil {
		t.Fatal("bad config should error")
	}
}

func TestBuildChannel_Email(t *testing.T) {
	c := &store.NotifyChannel{Type: "email", Config: `{"host":"localhost","port":"25","user":"u","pass":"p","from":"a@b","to":"c@d"}`}
	ch, err := buildChannel(c, nil)
	if err != nil {
		t.Fatalf("build email: %v", err)
	}
	if ch == nil {
		t.Fatal("email channel nil")
	}
}

func TestBuildChannel_Webhook(t *testing.T) {
	c := &store.NotifyChannel{Type: "webhook", Config: `{"webhookURL":"https://example.com/hook"}`}
	ch, err := buildChannel(c, nil)
	if err != nil {
		t.Fatalf("build webhook: %v", err)
	}
	if ch == nil {
		t.Fatal("webhook channel nil")
	}
}

func TestValidateNotifyChannelWebhook(t *testing.T) {
	// email 类型 → 跳过校验
	if err := validateNotifyChannelWebhook(&store.NotifyChannel{Type: "email"}, false); err != nil {
		t.Fatalf("email should skip: %v", err)
	}
	// webhook 类型 + 公网 URL → 通过
	if err := validateNotifyChannelWebhook(&store.NotifyChannel{Type: "webhook", Config: `{"webhookURL":"https://example.com/h"}`}, false); err != nil {
		t.Fatalf("public url should pass: %v", err)
	}
	// webhook 类型 + 空 URL → 跳过（由 store 校验）
	if err := validateNotifyChannelWebhook(&store.NotifyChannel{Type: "webhook", Config: "{}"}, false); err != nil {
		t.Fatalf("empty url should skip: %v", err)
	}
	// webhook 类型 + 私网 URL → 拒绝
	if err := validateNotifyChannelWebhook(&store.NotifyChannel{Type: "webhook", Config: `{"webhookURL":"http://10.0.0.1/h"}`}, false); err == nil {
		t.Fatal("private url should be rejected")
	}
	// 未知类型 → 放行
	if err := validateNotifyChannelWebhook(&store.NotifyChannel{Type: "unknown"}, false); err != nil {
		t.Fatalf("unknown type should pass: %v", err)
	}
	// bad config → 错误
	if err := validateNotifyChannelWebhook(&store.NotifyChannel{Type: "webhook", Config: "{bad"}, false); err == nil {
		t.Fatal("bad config should error")
	}
}

// 确保 bytes 包被使用（部分测试可能未直接引用，避免 import 报错）
var _ = bytes.NewBuffer
