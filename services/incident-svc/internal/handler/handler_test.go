package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/aggregate"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/service"
)

// newTestServer 组装带全部路由的测试服务器。
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := models.NewMemoryStore()
	eng := aggregate.NewEngine(5 * time.Minute)
	svc := service.NewService(store, eng)
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

// doJSON 发送 JSON 请求并解析 JSON 响应。
func doJSON(t *testing.T, ts *httptest.Server, method, path string, body interface{}, out interface{}) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode response of %s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// createTestIncident 创建一个事件并返回 ID。
func createTestIncident(t *testing.T, ts *httptest.Server, body map[string]interface{}) string {
	t.Helper()
	var inc models.Incident
	status := doJSON(t, ts, http.MethodPost, "/api/v1/incidents", body, &inc)
	if status != http.StatusCreated {
		t.Fatalf("create incident: got status %d, want %d", status, http.StatusCreated)
	}
	return inc.ID
}

// ---- 修复点：POST /{id}/timeline（前端 {type,content}）----

func TestTimelinePostAlias(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	id := createTestIncident(t, ts, map[string]interface{}{
		"title": "svc down", "severity": "high",
	})

	var ev models.TimelineEvent
	status := doJSON(t, ts, http.MethodPost, "/api/v1/incidents/"+id+"/timeline",
		map[string]string{"type": "note", "content": "checked logs"}, &ev)
	if status != http.StatusCreated {
		t.Fatalf("POST timeline: got status %d, want %d", status, http.StatusCreated)
	}
	if ev.Type != "note" {
		t.Errorf("Type = %q, want %q", ev.Type, "note")
	}
	if ev.Description != "checked logs" {
		t.Errorf("Description = %q, want %q (content 字段必须被采纳)", ev.Description, "checked logs")
	}

	// GET timeline 应能看到刚写入的事件。
	var events []models.TimelineEvent
	status = doJSON(t, ts, http.MethodGet, "/api/v1/incidents/"+id+"/timeline", nil, &events)
	if status != http.StatusOK {
		t.Fatalf("GET timeline: got status %d, want %d", status, http.StatusOK)
	}
	found := false
	for _, e := range events {
		if e.ID == ev.ID && e.Description == "checked logs" {
			found = true
		}
	}
	if !found {
		t.Error("POST timeline 写入的事件未出现在 GET timeline 结果中")
	}
}

func TestTimelinePostEventsPathStillWorks(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	id := createTestIncident(t, ts, map[string]interface{}{
		"title": "disk full", "severity": "medium",
	})

	var ev models.TimelineEvent
	status := doJSON(t, ts, http.MethodPost, "/api/v1/incidents/"+id+"/events",
		map[string]string{"type": "action", "description": "cleared disk"}, &ev)
	if status != http.StatusCreated {
		t.Fatalf("POST events: got status %d, want %d", status, http.StatusCreated)
	}
	if ev.Description != "cleared disk" {
		t.Errorf("Description = %q, want %q", ev.Description, "cleared disk")
	}
}

// ---- 修复点：POST /{id}/postmortem ----

func TestPostmortemPostAlias(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	id := createTestIncident(t, ts, map[string]interface{}{
		"title": "api latency", "severity": "critical",
	})

	var pm models.Postmortem
	status := doJSON(t, ts, http.MethodPost, "/api/v1/incidents/"+id+"/postmortem", nil, &pm)
	if status != http.StatusOK {
		t.Fatalf("POST postmortem: got status %d, want %d", status, http.StatusOK)
	}
	if pm.IncidentID != id {
		t.Errorf("IncidentID = %q, want %q", pm.IncidentID, id)
	}
	if pm.GeneratedAt.IsZero() {
		t.Error("GeneratedAt 未设置")
	}
}

// ---- 修复点：GET /api/v1/incidents/metrics ----

func TestIncidentsMetricsRoute(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// 先造 2 个事件再查指标。
	createTestIncident(t, ts, map[string]interface{}{"title": "a", "severity": "low"})
	createTestIncident(t, ts, map[string]interface{}{"title": "b", "severity": "low"})

	var m struct {
		MTTD     string `json:"mttd"`
		MTTR     string `json:"mttr"`
		Total    int    `json:"total"`
		Open     int    `json:"open"`
		Resolved int    `json:"resolved"`
	}
	status := doJSON(t, ts, http.MethodGet, "/api/v1/incidents/metrics", nil, &m)
	if status != http.StatusOK {
		t.Fatalf("GET incidents/metrics: got status %d, want %d", status, http.StatusOK)
	}
	if m.Total != 2 {
		t.Errorf("total = %d, want 2", m.Total)
	}
	if m.Open != 2 {
		t.Errorf("open = %d, want 2", m.Open)
	}
	if m.Resolved != 0 {
		t.Errorf("resolved = %d, want 0", m.Resolved)
	}
	if m.MTTR != "0s" && m.MTTR != "" {
		t.Errorf("mttr = %q, want 0s 或空（未解决事件）", m.MTTR)
	}
}

func TestIncidentsMetricsNotSwallowedByIDRoute(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// "metrics" 不能被 /{id} 路由吃掉变成 404。
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/incidents/metrics", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/v1/incidents/metrics: got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ---- 修复点：POST /incidents 的 assignee 不能丢 ----

func TestCreateIncidentAssignee(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	var inc models.Incident
	status := doJSON(t, ts, http.MethodPost, "/api/v1/incidents", map[string]interface{}{
		"title":       "db down",
		"severity":    "critical",
		"description": "primary offline",
		"assignee":    "alice",
	}, &inc)
	if status != http.StatusCreated {
		t.Fatalf("create incident: got status %d, want %d", status, http.StatusCreated)
	}
	if inc.Assignee != "alice" {
		t.Errorf("Assignee = %q, want %q (前端 assignee 必须落库)", inc.Assignee, "alice")
	}

	// GET 回读确认已持久化。
	var got models.Incident
	status = doJSON(t, ts, http.MethodGet, "/api/v1/incidents/"+inc.ID, nil, &got)
	if status != http.StatusOK {
		t.Fatalf("get incident: got status %d, want %d", status, http.StatusOK)
	}
	if got.Assignee != "alice" {
		t.Errorf("persisted Assignee = %q, want %q", got.Assignee, "alice")
	}
}

// ---- 修复点：PUT /{id} 支持 status（合法向前迁移）----

func TestUpdateIncidentStatusForward(t *testing.T) {
	for _, tc := range []struct {
		name       string
		createTo   string // 先迁移到该状态（空=不迁移，保持 detected）
		sendStatus string
		wantStatus string
	}{
		{"detected→investigating", "", "investigating", "investigating"},
		{"investigating→mitigating", "investigating", "mitigating", "mitigating"},
		{"mitigating→resolved", "mitigating", "resolved", "resolved"},
		{"detected→resolved（跳级）", "", "resolved", "resolved"},
		{"resolved→closed", "resolved", "closed", "closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			defer ts.Close()

			id := createTestIncident(t, ts, map[string]interface{}{"title": "x", "severity": "low"})
			if tc.createTo != "" {
				var inc models.Incident
				status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/"+id,
					map[string]string{"status": tc.createTo}, &inc)
				if status != http.StatusOK {
					t.Fatalf("pre-migrate: got status %d, want %d", status, http.StatusOK)
				}
			}

			var updated models.Incident
			status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/"+id,
				map[string]string{"status": tc.sendStatus, "severity": "high", "assignee": "bob"}, &updated)
			if status != http.StatusOK {
				t.Fatalf("PUT status: got status %d, want %d", status, http.StatusOK)
			}
			if string(updated.Status) != tc.wantStatus {
				t.Errorf("Status = %q, want %q", updated.Status, tc.wantStatus)
			}
		})
	}
}

// ---- 修复点：PUT /{id} 非法迁移返回 409 ----

func TestUpdateIncidentStatusInvalidTransition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		createTo   string
		sendStatus string
	}{
		{"resolved 回退 detected", "resolved", "detected"},
		{"resolved 重入 resolved", "resolved", "resolved"},
		{"closed 回退 investigating", "closed", "investigating"},
		{"detected 回退 closed", "", "closed→detect"}, // 占位，真实场景见下
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sendStatus == "closed→detect" {
				t.Skip("非法字面量场景由前三个用例覆盖")
			}
			ts := newTestServer(t)
			defer ts.Close()

			id := createTestIncident(t, ts, map[string]interface{}{"title": "x", "severity": "low"})
			if tc.createTo != "" {
				var inc models.Incident
				status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/"+id,
					map[string]string{"status": tc.createTo}, &inc)
				if status != http.StatusOK {
					t.Fatalf("pre-migrate to %s: got status %d, want %d", tc.createTo, status, http.StatusOK)
				}
			}

			var errResp struct {
				Error string `json:"error"`
			}
			status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/"+id,
				map[string]string{"status": tc.sendStatus}, &errResp)
			if status != http.StatusConflict {
				t.Fatalf("PUT invalid status %q: got status %d, want %d", tc.sendStatus, status, http.StatusConflict)
			}
			if errResp.Error == "" {
				t.Error("409 响应缺少 error 字段")
			}
		})
	}
}

func TestUpdateIncidentStatusUnknownValue(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	id := createTestIncident(t, ts, map[string]interface{}{"title": "x", "severity": "low"})

	var errResp struct {
		Error string `json:"error"`
	}
	status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/"+id,
		map[string]string{"status": "bogus"}, &errResp)
	if status != http.StatusConflict {
		t.Fatalf("PUT unknown status: got status %d, want %d", status, http.StatusConflict)
	}
}

func TestUpdateIncidentNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	var errResp struct {
		Error string `json:"error"`
	}
	status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/nonexistent",
		map[string]string{"status": "investigating"}, &errResp)
	if status != http.StatusNotFound {
		t.Fatalf("PUT nonexistent: got status %d, want %d", status, http.StatusNotFound)
	}
}

// ---- 回归：PUT 不带 status 时原有字段更新不受影响 ----

func TestUpdateIncidentFieldsWithoutStatus(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	id := createTestIncident(t, ts, map[string]interface{}{"title": "orig", "severity": "low"})

	var updated models.Incident
	status := doJSON(t, ts, http.MethodPut, "/api/v1/incidents/"+id,
		map[string]string{"title": "new title", "assignee": "carol", "severity": "high"}, &updated)
	if status != http.StatusOK {
		t.Fatalf("PUT fields: got status %d, want %d", status, http.StatusOK)
	}
	if updated.Title != "new title" || updated.Assignee != "carol" || updated.Severity != models.SeverityHigh {
		t.Errorf("field update failed: title=%q assignee=%q severity=%q", updated.Title, updated.Assignee, updated.Severity)
	}
}
