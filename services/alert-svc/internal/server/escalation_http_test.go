package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/alert-svc/internal/escalation"
)

const (
	policiesPath  = "/api/v1/escalation/policies"
	activePath    = "/api/v1/escalation/active"
	schedulesPath = "/api/v1/oncall/schedules"
	onCallPath    = "/api/v1/oncall/current"
)

type escalationFixture struct {
	t   *testing.T
	mux *http.ServeMux
	esc *escalation.Escalator
}

// newEscalationFixture 用真实 ServeMux 注册路由，请求走完整路由表而不是直接调
// 未导出 handler——路径前缀/方法分发本身也是被测契约。
func newEscalationFixture(t *testing.T) *escalationFixture {
	t.Helper()
	esc := escalation.NewEscalator()
	t.Cleanup(esc.Stop)
	mux := http.NewServeMux()
	NewEscalationHandler(esc).RegisterRoutes(mux)
	return &escalationFixture{t: t, mux: mux, esc: esc}
}

func (f *escalationFixture) request(method, path string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			f.t.Fatalf("序列化请求体失败: %v", err)
		}
		raw = b
	}
	return f.requestRaw(method, path, raw)
}

func (f *escalationFixture) requestRaw(method, path string, body []byte) *httptest.ResponseRecorder {
	f.t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, r)
	return rec
}

func decodeObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v (body=%q)", err, rec.Body.String())
	}
	return out
}

func decodeArray(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON 数组: %v (body=%q)", err, rec.Body.String())
	}
	return out
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d, want %d (body=%q)", rec.Code, want, rec.Body.String())
	}
}

func assertJSONContentType(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMsg string) {
	t.Helper()
	assertStatus(t, rec, wantStatus)
	assertJSONContentType(t, rec)
	body := decodeObject(t, rec)
	if len(body) != 1 {
		t.Fatalf("错误响应应只含 error 键，实得 %v", body)
	}
	got, ok := body["error"].(string)
	if !ok {
		t.Fatalf("错误响应缺少字符串 error 字段: %v", body)
	}
	if got != wantMsg {
		t.Fatalf("error = %q, want %q", got, wantMsg)
	}
}

func assertKeys(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("键集合 = %v, want %v", got, want)
	}
}

func mustParseTime(t *testing.T, v any, field string) time.Time {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s 不是字符串: %#v", field, v)
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("%s 不是 RFC3339 时间: %q (%v)", field, s, err)
	}
	return parsed
}

func findByField(t *testing.T, items []map[string]any, field, value string) map[string]any {
	t.Helper()
	for _, item := range items {
		if item[field] == value {
			return item
		}
	}
	t.Fatalf("未找到 %s=%s 的条目: %v", field, value, items)
	return nil
}

// policyPayload 中 timeout 用整数纳秒：EscalationLevel.Timeout 是 time.Duration，
// 未实现自定义 UnmarshalJSON，JSON 形态只能是数字（字符串 "5m" 会被拒）。
func policyPayload(id, tenant, name string, enabled bool) map[string]any {
	return map[string]any{
		"id":        id,
		"name":      name,
		"tenant_id": tenant,
		"levels": []map[string]any{
			{"level": 1, "timeout": int64(5 * time.Minute), "channels": []string{"pagerduty"}},
			{"level": 2, "timeout": int64(10 * time.Minute), "channels": []string{"slack", "email"}},
		},
		"enabled": enabled,
	}
}

func schedulePayload(id, tenant, user string, start, end time.Time, rotation string) map[string]any {
	return map[string]any{
		"id":        id,
		"name":      id + "-name",
		"tenant_id": tenant,
		"entries": []map[string]any{
			{"user_id": user, "start_time": start.Format(time.RFC3339), "end_time": end.Format(time.RFC3339)},
		},
		"rotation": rotation,
	}
}

func TestCreatePolicy_EchoesCreatedPolicy(t *testing.T) {
	f := newEscalationFixture(t)

	rec := f.request(http.MethodPost, policiesPath, policyPayload("pol-1", "tenant-a", "primary", true))
	assertStatus(t, rec, http.StatusCreated)
	assertJSONContentType(t, rec)

	body := decodeObject(t, rec)
	assertKeys(t, body, "id", "name", "tenant_id", "levels", "enabled", "created_at", "updated_at")
	if body["id"] != "pol-1" || body["name"] != "primary" || body["tenant_id"] != "tenant-a" {
		t.Fatalf("回显字段与请求不符: %v", body)
	}
	if body["enabled"] != true {
		t.Fatalf("enabled = %v, want true", body["enabled"])
	}

	levels, ok := body["levels"].([]any)
	if !ok || len(levels) != 2 {
		t.Fatalf("levels = %#v, want 长度 2 的数组", body["levels"])
	}
	first, ok := levels[0].(map[string]any)
	if !ok {
		t.Fatalf("levels[0] 不是对象: %#v", levels[0])
	}
	assertKeys(t, first, "level", "timeout", "channels")
	if first["level"] != float64(1) {
		t.Fatalf("levels[0].level = %v, want 1", first["level"])
	}
	if first["timeout"] != float64(5*time.Minute) {
		t.Fatalf("levels[0].timeout = %v, want %v（纳秒）", first["timeout"], float64(5*time.Minute))
	}
	channels, ok := first["channels"].([]any)
	if !ok || len(channels) != 1 || channels[0] != "pagerduty" {
		t.Fatalf("levels[0].channels = %#v, want [pagerduty]", first["channels"])
	}

	createdAt := mustParseTime(t, body["created_at"], "created_at")
	updatedAt := mustParseTime(t, body["updated_at"], "updated_at")
	if updatedAt.Before(createdAt) {
		t.Fatalf("updated_at(%v) 早于 created_at(%v)", updatedAt, createdAt)
	}
}

func TestListPolicies_EmptyIsJSONArrayNotNull(t *testing.T) {
	f := newEscalationFixture(t)

	rec := f.request(http.MethodGet, policiesPath, nil)
	assertStatus(t, rec, http.StatusOK)
	assertJSONContentType(t, rec)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("空列表响应 = %q, want []（前端按数组消费，null 会破坏 .map）", got)
	}
}

func TestListPolicies_TenantFilter(t *testing.T) {
	f := newEscalationFixture(t)
	for _, p := range [][3]string{
		{"pol-a", "tenant-a", "a"},
		{"pol-b", "tenant-b", "b"},
		{"pol-c", "tenant-a", "c"},
	} {
		rec := f.request(http.MethodPost, policiesPath, policyPayload(p[0], p[1], p[2], true))
		assertStatus(t, rec, http.StatusCreated)
	}

	rec := f.request(http.MethodGet, policiesPath, nil)
	all := decodeArray(t, rec)
	if len(all) != 3 {
		t.Fatalf("无过滤时条目数 = %d, want 3", len(all))
	}

	rec = f.request(http.MethodGet, policiesPath+"?tenant_id=tenant-a", nil)
	filtered := decodeArray(t, rec)
	if len(filtered) != 2 {
		t.Fatalf("tenant-a 条目数 = %d, want 2", len(filtered))
	}
	for _, p := range filtered {
		if p["tenant_id"] != "tenant-a" {
			t.Fatalf("过滤结果混入其他租户: %v", p)
		}
	}

	rec = f.request(http.MethodGet, policiesPath+"?tenant_id=tenant-missing", nil)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("未知租户响应 = %q, want []", got)
	}
}

func TestCreatePolicy_RejectsBadBody(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{name: "empty body", body: nil, want: "invalid request body"},
		{name: "malformed json", body: []byte("{"), want: "invalid request body"},
		{name: "json null", body: []byte("null"), want: "escalation: policy ID is required"},
		{name: "missing id", body: []byte("{}"), want: "escalation: policy ID is required"},
		{name: "empty id", body: []byte(`{"id":""}`), want: "escalation: policy ID is required"},
		{
			// 纳秒整数是当前唯一可解码的 timeout 形态；字符串会被 400，
			// 解码细节不得回显给客户端。
			name: "duration as string",
			body: []byte(`{"id":"pol-str","levels":[{"level":1,"timeout":"5m"}]}`),
			want: "invalid request body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEscalationFixture(t)
			rec := f.requestRaw(http.MethodPost, policiesPath, tc.body)
			assertJSONError(t, rec, http.StatusBadRequest, tc.want)
		})
	}
}

func TestCreatePolicy_UpsertByID(t *testing.T) {
	f := newEscalationFixture(t)

	rec := f.request(http.MethodPost, policiesPath, policyPayload("pol-dup", "tenant-a", "original", true))
	assertStatus(t, rec, http.StatusCreated)
	first := decodeObject(t, rec)
	createdAtRaw := first["created_at"]
	if _, ok := createdAtRaw.(string); !ok {
		t.Fatalf("created_at 不是字符串: %#v", createdAtRaw)
	}

	// 同一 ID 再创建 = 更新；显式回传 created_at 时它必须被保留。
	updated := policyPayload("pol-dup", "tenant-a", "rotated", false)
	updated["created_at"] = createdAtRaw
	rec = f.request(http.MethodPost, policiesPath, updated)
	assertStatus(t, rec, http.StatusCreated)
	second := decodeObject(t, rec)
	if second["name"] != "rotated" {
		t.Fatalf("更新后 name = %v, want rotated", second["name"])
	}
	if second["enabled"] != false {
		t.Fatalf("更新后 enabled = %v, want false", second["enabled"])
	}
	wantCreated := mustParseTime(t, createdAtRaw, "created_at")
	gotCreated := mustParseTime(t, second["created_at"], "created_at")
	if !gotCreated.Equal(wantCreated) {
		t.Fatalf("created_at = %v, want 保留 %v", gotCreated, wantCreated)
	}

	list := decodeArray(t, f.request(http.MethodGet, policiesPath, nil))
	if len(list) != 1 {
		t.Fatalf("upsert 后条目数 = %d, want 1", len(list))
	}
	if list[0]["name"] != "rotated" {
		t.Fatalf("列表中的 name = %v, want rotated", list[0]["name"])
	}
}

func TestActiveEscalations_OnlyActiveStates(t *testing.T) {
	f := newEscalationFixture(t)

	rec := f.request(http.MethodPost, policiesPath, policyPayload("pol-esc", "tenant-a", "esc", true))
	assertStatus(t, rec, http.StatusCreated)

	if _, err := f.esc.StartEscalation("alert-1", "pol-esc"); err != nil {
		t.Fatalf("StartEscalation(alert-1): %v", err)
	}
	if _, err := f.esc.StartEscalation("alert-2", "pol-esc"); err != nil {
		t.Fatalf("StartEscalation(alert-2): %v", err)
	}
	if _, err := f.esc.Escalate("alert-1"); err != nil {
		t.Fatalf("Escalate(alert-1): %v", err)
	}

	rec = f.request(http.MethodGet, activePath, nil)
	assertStatus(t, rec, http.StatusOK)
	assertJSONContentType(t, rec)
	items := decodeArray(t, rec)
	if len(items) != 2 {
		t.Fatalf("active 条目数 = %d, want 2", len(items))
	}

	state := findByField(t, items, "alert_id", "alert-1")
	assertKeys(t, state, "id", "alert_id", "policy_id", "current_level", "status", "started_at", "notified_at", "channels_used")
	if state["id"] != "esc-alert-1" || state["policy_id"] != "pol-esc" {
		t.Fatalf("状态字段不符: %v", state)
	}
	if state["status"] != "active" {
		t.Fatalf("status = %v, want active", state["status"])
	}
	if state["current_level"] != float64(0) {
		t.Fatalf("current_level = %v, want 0", state["current_level"])
	}
	notified, ok := state["notified_at"].(map[string]any)
	if !ok || len(notified) != 1 {
		t.Fatalf("notified_at = %#v, want 含一个 level 的对象", state["notified_at"])
	}
	if _, ok := notified["1"]; !ok {
		t.Fatalf("notified_at 缺少 level 1: %v", notified)
	}
	channels, ok := state["channels_used"].(map[string]any)
	if !ok {
		t.Fatalf("channels_used = %#v, want 对象", state["channels_used"])
	}
	used, ok := channels["1"].([]any)
	if !ok || len(used) != 1 || used[0] != "pagerduty" {
		t.Fatalf("channels_used[1] = %#v, want [pagerduty]", channels["1"])
	}

	if err := f.esc.Acknowledge("alert-1"); err != nil {
		t.Fatalf("Acknowledge(alert-1): %v", err)
	}
	if err := f.esc.Resolve("alert-2"); err != nil {
		t.Fatalf("Resolve(alert-2): %v", err)
	}

	rec = f.request(http.MethodGet, activePath, nil)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("ack/resolve 后 active 响应 = %q, want []", got)
	}
}

func TestCreateSchedule_EchoesCreatedSchedule(t *testing.T) {
	f := newEscalationFixture(t)
	now := time.Now()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)

	rec := f.request(http.MethodPost, schedulesPath, schedulePayload("sch-1", "tenant-a", "alice", start, end, "weekly"))
	assertStatus(t, rec, http.StatusCreated)
	assertJSONContentType(t, rec)

	body := decodeObject(t, rec)
	assertKeys(t, body, "id", "name", "tenant_id", "entries", "rotation", "created_at", "updated_at")
	if body["id"] != "sch-1" || body["tenant_id"] != "tenant-a" || body["rotation"] != "weekly" {
		t.Fatalf("回显字段与请求不符: %v", body)
	}

	entries, ok := body["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("entries = %#v, want 长度 1 的数组", body["entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entries[0] 不是对象: %#v", entries[0])
	}
	assertKeys(t, entry, "user_id", "start_time", "end_time")
	if entry["user_id"] != "alice" {
		t.Fatalf("entries[0].user_id = %v, want alice", entry["user_id"])
	}
	if got := mustParseTime(t, entry["start_time"], "start_time"); !got.Equal(start.Truncate(time.Second)) {
		t.Fatalf("start_time = %v, want %v", got, start.Truncate(time.Second))
	}
	if got := mustParseTime(t, entry["end_time"], "end_time"); !got.Equal(end.Truncate(time.Second)) {
		t.Fatalf("end_time = %v, want %v", got, end.Truncate(time.Second))
	}

	list := decodeArray(t, f.request(http.MethodGet, schedulesPath, nil))
	if len(list) != 1 || list[0]["id"] != "sch-1" {
		t.Fatalf("列表 = %v, want 仅含 sch-1", list)
	}
}

func TestCreateSchedule_RejectsBadBody(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{name: "empty body", body: nil, want: "invalid request body"},
		{name: "malformed json", body: []byte(`{"id":`), want: "invalid request body"},
		{name: "json null", body: []byte("null"), want: "escalation: schedule ID is required"},
		{name: "missing id", body: []byte(`{"name":"x"}`), want: "escalation: schedule ID is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEscalationFixture(t)
			rec := f.requestRaw(http.MethodPost, schedulesPath, tc.body)
			assertJSONError(t, rec, http.StatusBadRequest, tc.want)
		})
	}
}

func TestListSchedules_TenantFilter(t *testing.T) {
	f := newEscalationFixture(t)
	now := time.Now()
	for _, s := range [][3]string{
		{"sch-a", "tenant-a", "alice"},
		{"sch-b", "tenant-b", "bob"},
		{"sch-c", "tenant-a", "carol"},
	} {
		rec := f.request(http.MethodPost, schedulesPath, schedulePayload(s[0], s[1], s[2], now.Add(-time.Hour), now.Add(time.Hour), ""))
		assertStatus(t, rec, http.StatusCreated)
	}

	all := decodeArray(t, f.request(http.MethodGet, schedulesPath, nil))
	if len(all) != 3 {
		t.Fatalf("无过滤时条目数 = %d, want 3", len(all))
	}

	filtered := decodeArray(t, f.request(http.MethodGet, schedulesPath+"?tenant_id=tenant-a", nil))
	if len(filtered) != 2 {
		t.Fatalf("tenant-a 条目数 = %d, want 2", len(filtered))
	}
	for _, s := range filtered {
		if s["tenant_id"] != "tenant-a" {
			t.Fatalf("过滤结果混入其他租户: %v", s)
		}
	}

	rec := f.request(http.MethodGet, schedulesPath+"?tenant_id=tenant-missing", nil)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("未知租户响应 = %q, want []", got)
	}
}

func TestOnCallCurrent_Resolution(t *testing.T) {
	newWindow := func() (time.Time, time.Time) {
		now := time.Now()
		return now.Add(-time.Hour), now.Add(time.Hour)
	}

	t.Run("by schedule_id", func(t *testing.T) {
		f := newEscalationFixture(t)
		start, end := newWindow()
		rec := f.request(http.MethodPost, schedulesPath, schedulePayload("sch-a", "team-x", "alice", start, end, ""))
		assertStatus(t, rec, http.StatusCreated)

		rec = f.request(http.MethodGet, onCallPath+"?schedule_id=sch-a", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSONContentType(t, rec)
		entry := decodeObject(t, rec)
		assertKeys(t, entry, "user_id", "start_time", "end_time")
		if entry["user_id"] != "alice" {
			t.Fatalf("user_id = %v, want alice", entry["user_id"])
		}
	})

	t.Run("by tenant_id", func(t *testing.T) {
		f := newEscalationFixture(t)
		start, end := newWindow()
		rec := f.request(http.MethodPost, schedulesPath, schedulePayload("sch-a", "team-x", "alice", start, end, ""))
		assertStatus(t, rec, http.StatusCreated)

		rec = f.request(http.MethodGet, onCallPath+"?tenant_id=team-x", nil)
		assertStatus(t, rec, http.StatusOK)
		entry := decodeObject(t, rec)
		if entry["user_id"] != "alice" {
			t.Fatalf("user_id = %v, want alice", entry["user_id"])
		}
	})

	t.Run("schedule_id wins over tenant_id", func(t *testing.T) {
		f := newEscalationFixture(t)
		start, end := newWindow()
		rec := f.request(http.MethodPost, schedulesPath, schedulePayload("sch-a", "team-x", "alice", start, end, ""))
		assertStatus(t, rec, http.StatusCreated)
		rec = f.request(http.MethodPost, schedulesPath, schedulePayload("sch-b", "team-y", "bob", start, end, ""))
		assertStatus(t, rec, http.StatusCreated)

		rec = f.request(http.MethodGet, onCallPath+"?schedule_id=sch-a&tenant_id=team-y", nil)
		assertStatus(t, rec, http.StatusOK)
		if entry := decodeObject(t, rec); entry["user_id"] != "alice" {
			t.Fatalf("user_id = %v, want alice（schedule_id 优先于 tenant_id）", entry["user_id"])
		}
	})

	t.Run("missing both params", func(t *testing.T) {
		f := newEscalationFixture(t)
		assertJSONError(t, f.request(http.MethodGet, onCallPath, nil), http.StatusBadRequest, "schedule_id or tenant_id required")
		assertJSONError(t, f.request(http.MethodGet, onCallPath+"?tenant_id=", nil), http.StatusBadRequest, "schedule_id or tenant_id required")
	})

	t.Run("unknown schedule", func(t *testing.T) {
		f := newEscalationFixture(t)
		assertJSONError(t, f.request(http.MethodGet, onCallPath+"?schedule_id=missing", nil), http.StatusNotFound, "no on-call found")
	})

	t.Run("unknown tenant", func(t *testing.T) {
		f := newEscalationFixture(t)
		assertJSONError(t, f.request(http.MethodGet, onCallPath+"?tenant_id=missing", nil), http.StatusNotFound, "no on-call found")
	})

	t.Run("no active window", func(t *testing.T) {
		f := newEscalationFixture(t)
		now := time.Now()
		// 窗口已过期：排班存在但当前时刻无人值班。
		rec := f.request(http.MethodPost, schedulesPath, schedulePayload("sch-old", "team-x", "alice", now.Add(-3*time.Hour), now.Add(-2*time.Hour), ""))
		assertStatus(t, rec, http.StatusCreated)
		assertJSONError(t, f.request(http.MethodGet, onCallPath+"?schedule_id=sch-old", nil), http.StatusNotFound, "no on-call found")
	})

	t.Run("schedule without entries", func(t *testing.T) {
		f := newEscalationFixture(t)
		rec := f.request(http.MethodPost, schedulesPath, map[string]any{
			"id": "sch-empty", "tenant_id": "team-x", "entries": []any{}, "rotation": "",
		})
		assertStatus(t, rec, http.StatusCreated)
		assertJSONError(t, f.request(http.MethodGet, onCallPath+"?schedule_id=sch-empty", nil), http.StatusNotFound, "no on-call found")
	})
}

func TestEscalationRoutes_MethodMatrix(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		{method: http.MethodPut, path: policiesPath},
		{method: http.MethodDelete, path: policiesPath},
		{method: http.MethodPatch, path: policiesPath},
		{method: http.MethodPost, path: activePath},
		{method: http.MethodPut, path: activePath},
		{method: http.MethodDelete, path: schedulesPath},
		{method: http.MethodPut, path: schedulesPath},
		{method: http.MethodPost, path: onCallPath},
		{method: http.MethodDelete, path: onCallPath},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			f := newEscalationFixture(t)
			rec := f.requestRaw(tc.method, tc.path, nil)
			assertJSONError(t, rec, http.StatusMethodNotAllowed, "method not allowed")
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	f := newEscalationFixture(t)
	now := time.Now()
	start, end := now.Add(-time.Hour), now.Add(time.Hour)

	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("pol-%d", i)
			rec := f.request(http.MethodPost, policiesPath, policyPayload(id, "tenant-a", id, true))
			if rec.Code != http.StatusCreated {
				t.Errorf("POST policy %s 状态码 = %d (body=%q)", id, rec.Code, rec.Body.String())
			}

			rec = f.request(http.MethodGet, policiesPath+"?tenant_id=tenant-a", nil)
			if rec.Code != http.StatusOK {
				t.Errorf("GET policies 状态码 = %d", rec.Code)
			}

			rec = f.request(http.MethodPost, schedulesPath, schedulePayload("sch-"+id, "tenant-a", "alice", start, end, ""))
			if rec.Code != http.StatusCreated {
				t.Errorf("POST schedule %s 状态码 = %d", id, rec.Code)
			}

			rec = f.request(http.MethodGet, activePath, nil)
			if rec.Code != http.StatusOK {
				t.Errorf("GET active 状态码 = %d", rec.Code)
			}
		}(i)
	}
	wg.Wait()

	list := decodeArray(t, f.request(http.MethodGet, policiesPath, nil))
	if len(list) != workers {
		t.Fatalf("并发创建后策略数 = %d, want %d", len(list), workers)
	}
	list = decodeArray(t, f.request(http.MethodGet, schedulesPath, nil))
	if len(list) != workers {
		t.Fatalf("并发创建后排班数 = %d, want %d", len(list), workers)
	}
}
