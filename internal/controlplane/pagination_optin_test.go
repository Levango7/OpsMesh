// pagination_optin_test.go — 列表端点 opt-in 分页契约测试。
//
// 覆盖 GET /api/v1/devices、/api/v1/audits、/api/v1/webhooks、/api/v1/scripts：
//   - 无 page/pageSize：旧响应形态逐字段保持不变（向后兼容）；
//   - 任一参数存在：统一信封 {data,total,page,pageSize,hasMore}；
//   - page 越界：data 为空数组、total 保持；
//   - pageSize 超上限：clamp 到 200；
//   - 非数字参数：400 {"error":"invalid page/pageSize"}；
//   - 仅 pageSize：同样触发信封且 page 默认 1；
//   - page<1：clamp 到 1（与 paginate.ParsePagination 一致）；
//   - 租户隔离：他租户行永不出现。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// paginationCase 是四个列表端点共用的表驱动用例。
type paginationCase struct {
	name         string
	query        string
	wantStatus   int
	wantLegacy   bool
	wantTotal    int
	wantPage     int
	wantPageSize int
	wantLen      int
	wantHasMore  bool
}

// pagedEnvelope 是统一分页信封的强类型解码形态。
type pagedEnvelope[T any] struct {
	Data     []T  `json:"data"`
	Total    int  `json:"total"`
	Page     int  `json:"page"`
	PageSize int  `json:"pageSize"`
	HasMore  bool `json:"hasMore"`
}

func paginationCases() []paginationCase {
	return []paginationCase{
		{name: "legacy_no_params", query: "", wantStatus: http.StatusOK, wantLegacy: true, wantLen: 5},
		{name: "first_page", query: "?page=1&pageSize=2", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 1, wantPageSize: 2, wantLen: 2, wantHasMore: true},
		{name: "page_only_default_size", query: "?page=1", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 1, wantPageSize: 20, wantLen: 5, wantHasMore: false},
		{name: "page_size_only", query: "?pageSize=2", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 1, wantPageSize: 2, wantLen: 2, wantHasMore: true},
		{name: "page_below_one_clamped", query: "?page=0&pageSize=2", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 1, wantPageSize: 2, wantLen: 2, wantHasMore: true},
		{name: "page_beyond_last", query: "?page=99&pageSize=2", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 99, wantPageSize: 2, wantLen: 0, wantHasMore: false},
		{name: "page_size_capped", query: "?page=1&pageSize=500", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 1, wantPageSize: 200, wantLen: 5, wantHasMore: false},
		{name: "non_numeric_page", query: "?page=abc", wantStatus: http.StatusBadRequest},
		{name: "non_numeric_page_size", query: "?page=1&pageSize=xyz", wantStatus: http.StatusBadRequest},
		{name: "tenant_isolation", query: "?page=1&pageSize=200", wantStatus: http.StatusOK, wantTotal: 5, wantPage: 1, wantPageSize: 200, wantLen: 5, wantHasMore: false},
	}
}

// assertEnvelope 校验统一信封元数据与 data 长度。
func assertEnvelope[T any](t *testing.T, rec *httptest.ResponseRecorder, tc paginationCase) []T {
	t.Helper()
	var env pagedEnvelope[T]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, rec.Body.String())
	}
	if env.Total != tc.wantTotal {
		t.Errorf("total=%d, want %d", env.Total, tc.wantTotal)
	}
	if env.Page != tc.wantPage {
		t.Errorf("page=%d, want %d", env.Page, tc.wantPage)
	}
	if env.PageSize != tc.wantPageSize {
		t.Errorf("pageSize=%d, want %d", env.PageSize, tc.wantPageSize)
	}
	if env.HasMore != tc.wantHasMore {
		t.Errorf("hasMore=%v, want %v", env.HasMore, tc.wantHasMore)
	}
	if len(env.Data) != tc.wantLen {
		t.Errorf("len(data)=%d, want %d; body=%s", len(env.Data), tc.wantLen, rec.Body.String())
	}
	if tc.wantLen == 0 && !bytes.Contains(rec.Body.Bytes(), []byte(`"data":[]`)) {
		t.Errorf("empty page must encode data as [], got %s", rec.Body.String())
	}
	return env.Data
}

// assertLegacyNoEnvelope 断言旧形态响应中不存在信封字段。
func assertLegacyNoEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode legacy body: %v; body=%s", err, rec.Body.String())
	}
	if _, ok := m["data"]; ok {
		t.Errorf("legacy response must not contain data field: %s", rec.Body.String())
	}
	if _, ok := m["total"]; ok {
		t.Errorf("legacy response must not contain total field: %s", rec.Body.String())
	}
	return m
}

func assertInvalidPagination(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "invalid page/pageSize" {
		t.Errorf("error=%q, want %q", resp["error"], "invalid page/pageSize")
	}
}

// seedPaginationDevices 写入 5 台本租户设备（跨两个 segment/乱序 ID）+ 1 台他租户设备。
func seedPaginationDevices(s *Server) {
	devs := []struct{ id, seg, tenant string }{
		{"dev-03", "seg-b", "t1"},
		{"dev-01", "seg-a", "t1"},
		{"dev-05", "seg-b", "t1"},
		{"dev-02", "seg-a", "t1"},
		{"dev-04", "seg-a", "t1"},
		{"dev-t2-1", "seg-a", "t2"},
	}
	for _, d := range devs {
		s.store.UpsertDevice(&proto.DeviceInfo{DeviceID: d.id, Segment: d.seg, TenantID: d.tenant, State: "online", Managed: true})
	}
}

func TestPaginationOptInDevices(t *testing.T) {
	for _, tc := range paginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer()
			seedPaginationDevices(s)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/devices"+tc.query, nil)
			req.Header.Set("X-Tenant-ID", "t1")
			rec := httptest.NewRecorder()
			s.handleDevices(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusBadRequest {
				assertInvalidPagination(t, rec)
				return
			}
			if tc.wantLegacy {
				var snap map[string][]proto.DeviceInfo
				if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
					t.Fatalf("decode segmented map: %v; body=%s", err, rec.Body.String())
				}
				if _, ok := snap["seg-a"]; !ok {
					t.Errorf("legacy segmented map lost seg-a: %s", rec.Body.String())
				}
				total := 0
				for _, list := range snap {
					total += len(list)
				}
				if total != tc.wantLen {
					t.Errorf("legacy devices=%d, want %d", total, tc.wantLen)
				}
				return
			}
			data := assertEnvelope[proto.DeviceInfo](t, rec, tc)
			// 稳定顺序：DeviceID 升序。
			for i := 1; i < len(data); i++ {
				if data[i-1].DeviceID > data[i].DeviceID {
					t.Errorf("devices not ordered by DeviceID: %q > %q", data[i-1].DeviceID, data[i].DeviceID)
				}
			}
			for _, d := range data {
				if d.TenantID != "t1" {
					t.Errorf("tenant leak: device %q tenant %q", d.DeviceID, d.TenantID)
				}
			}
		})
	}
}

// seedPaginationAudits 写入 5 条本租户审计（固定时间递增）+ 1 条他租户审计。
func seedPaginationAudits(s *Server) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		s.store.Audit(&proto.AuditEvent{
			TenantID:  "t1",
			Action:    "demo_action",
			Target:    "audit-0" + string(rune('0'+i)),
			Detail:    "seed",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "t2", Action: "demo_action", Target: "audit-t2", CreatedAt: base.Add(10 * time.Minute),
	})
}

func TestPaginationOptInAudits(t *testing.T) {
	for _, tc := range paginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer()
			seedPaginationAudits(s)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/audits"+tc.query, nil)
			req.Header.Set("X-Tenant-ID", "t1")
			rec := httptest.NewRecorder()
			s.handleAudits(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusBadRequest {
				assertInvalidPagination(t, rec)
				return
			}
			if tc.wantLegacy {
				var events []*proto.AuditEvent
				if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
					t.Fatalf("decode bare array: %v; body=%s", err, rec.Body.String())
				}
				if len(events) != tc.wantLen {
					t.Errorf("legacy audits=%d, want %d", len(events), tc.wantLen)
				}
				return
			}
			data := assertEnvelope[*proto.AuditEvent](t, rec, tc)
			// 稳定顺序：CreatedAt 倒序（最新在前）。
			for i := 1; i < len(data); i++ {
				if data[i-1].CreatedAt.Before(data[i].CreatedAt) {
					t.Errorf("audits not ordered by createdAt desc at %d", i)
				}
			}
			for _, e := range data {
				if e.TenantID != "t1" {
					t.Errorf("tenant leak: audit %q tenant %q", e.Target, e.TenantID)
				}
			}
		})
	}
}

func seedPaginationWebhooks(s *Server) {
	for _, id := range []string{"wh-03", "wh-01", "wh-05", "wh-02", "wh-04"} {
		s.store.CreateWebhook("default", &store.Webhook{ID: id, Name: id, URL: "https://203.0.113.1/hook"})
	}
	s.store.CreateWebhook("t2", &store.Webhook{ID: "wh-t2", Name: "t2", URL: "https://203.0.113.1/hook"})
}

func seedPaginationScripts(s *Server) {
	for _, id := range []string{"sc-03", "sc-01", "sc-05", "sc-02", "sc-04"} {
		s.store.CreateScript("default", &store.Script{ID: id, Name: id, Content: "echo hi"})
	}
	s.store.CreateScript("t2", &store.Script{ID: "sc-t2", Name: "t2", Content: "echo hi"})
}

func TestPaginationOptInWebhooks(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)
	seedPaginationWebhooks(s)
	for _, tc := range paginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks"+tc.query, nil)
			req.Header.Set("Authorization", auth)
			rec := httptest.NewRecorder()
			s.handleListWebhooks(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusBadRequest {
				assertInvalidPagination(t, rec)
				return
			}
			if tc.wantLegacy {
				m := assertLegacyNoEnvelope(t, rec)
				var list []*store.Webhook
				if err := json.Unmarshal(m["webhooks"], &list); err != nil {
					t.Fatalf("decode webhooks: %v", err)
				}
				if len(list) != tc.wantLen {
					t.Errorf("legacy webhooks=%d, want %d", len(list), tc.wantLen)
				}
				return
			}
			data := assertEnvelope[*store.Webhook](t, rec, tc)
			for i := 1; i < len(data); i++ {
				if data[i-1].ID > data[i].ID {
					t.Errorf("webhooks not ordered by ID: %q > %q", data[i-1].ID, data[i].ID)
				}
			}
			for _, wh := range data {
				if wh.TenantID != "default" {
					t.Errorf("tenant leak: webhook %q tenant %q", wh.ID, wh.TenantID)
				}
			}
		})
	}
}

func TestPaginationOptInScripts(t *testing.T) {
	s := newWebhookTestServer()
	auth := loginAsAdmin(t, s)
	seedPaginationScripts(s)
	for _, tc := range paginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts"+tc.query, nil)
			req.Header.Set("Authorization", auth)
			rec := httptest.NewRecorder()
			s.handleListScripts(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusBadRequest {
				assertInvalidPagination(t, rec)
				return
			}
			if tc.wantLegacy {
				m := assertLegacyNoEnvelope(t, rec)
				var list []*store.Script
				if err := json.Unmarshal(m["scripts"], &list); err != nil {
					t.Fatalf("decode scripts: %v", err)
				}
				if len(list) != tc.wantLen {
					t.Errorf("legacy scripts=%d, want %d", len(list), tc.wantLen)
				}
				return
			}
			data := assertEnvelope[*store.Script](t, rec, tc)
			for i := 1; i < len(data); i++ {
				if data[i-1].ID > data[i].ID {
					t.Errorf("scripts not ordered by ID: %q > %q", data[i-1].ID, data[i].ID)
				}
			}
			for _, sc := range data {
				if sc.TenantID != "default" {
					t.Errorf("tenant leak: script %q tenant %q", sc.ID, sc.TenantID)
				}
			}
		})
	}
}
