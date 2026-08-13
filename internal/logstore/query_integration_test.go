package logstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// ===========================================================================
// Memory 后端：结构化查询集成
// ===========================================================================

// seedForQ 构造一组覆盖多维度条件的日志，供结构化查询测试使用。
//
// 数据布局（均属租户 t1）：
//
//	e1: level=info,  device=dev-1, agent=a1, source=task,   task=T1, msg="started ok"
//	e2: level=error, device=dev-1, agent=a1, source=task,   task=T2, msg="panic: nil deref"
//	e3: level=error, device=dev-2, agent=a2, source=task,   task=T3, msg="panic: oom"
//	e4: level=warn,  device=dev-1, agent=a1, source=agent,  task="",  msg="cpu high"
//	e5: level=info,  device=dev-1, agent=a1, source=system, task="",  msg="heartbeat"
func seedForQ(t *testing.T, ls *MemoryLogStore) {
	t.Helper()
	ctx := context.Background()
	base := time.Now()
	entries := []Entry{
		{TenantID: "t1", DeviceID: "dev-1", AgentID: "a1", Source: "task", TaskID: "T1", Level: "info", Message: "started ok", Timestamp: base.Add(-4 * time.Minute)},
		{TenantID: "t1", DeviceID: "dev-1", AgentID: "a1", Source: "task", TaskID: "T2", Level: "error", Message: "panic: nil deref", Timestamp: base.Add(-3 * time.Minute)},
		{TenantID: "t1", DeviceID: "dev-2", AgentID: "a2", Source: "task", TaskID: "T3", Level: "error", Message: "panic: oom", Timestamp: base.Add(-2 * time.Minute)},
		{TenantID: "t1", DeviceID: "dev-1", AgentID: "a1", Source: "agent", Level: "warn", Message: "cpu high", Timestamp: base.Add(-1 * time.Minute)},
		{TenantID: "t1", DeviceID: "dev-1", AgentID: "a1", Source: "system", Level: "info", Message: "heartbeat", Timestamp: base},
	}
	for i := range entries {
		if err := ls.Append(ctx, &entries[i]); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
}

func TestMemoryQuery_WithStructuredQuery(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	seedForQ(t, ls)

	cases := []struct {
		name   string
		q      string
		wantID []int64 // 期望命中的 Entry.ID 序列（按时间倒序）
	}{
		{
			name:   "level=error",
			q:      "level=error",
			wantID: []int64{3, 2}, // e3(error/dev-2) 最新，e2(error/dev-1) 次之
		},
		{
			name:   "level=error AND device=dev-1",
			q:      "level=error AND device=dev-1",
			wantID: []int64{2},
		},
		{
			name:   "level=warn OR level=error",
			q:      "level=warn OR level=error",
			wantID: []int64{4, 3, 2}, // e4(warn), e3(error), e2(error)
		},
		{
			name:   "source=task AND (level=warn OR level=error)",
			q:      "source=task AND (level=warn OR level=error)",
			wantID: []int64{3, 2},
		},
		{
			name:   `level=error AND message~"panic"`,
			q:      `level=error AND message~"panic"`,
			wantID: []int64{3, 2},
		},
		{
			name:   `level=error AND device=dev-1 AND message~"panic"`,
			q:      `level=error AND device=dev-1 AND message~"panic"`,
			wantID: []int64{2},
		},
		{
			name:   "level!=info",
			q:      "level!=info",
			wantID: []int64{4, 3, 2}, // 排除 e1(info), e5(info)
		},
		{
			name:   `message!~"panic"`,
			q:      `message!~"panic"`,
			wantID: []int64{5, 4, 1}, // 排除 e2,e3（含 panic）
		},
		{
			name:   "NOT level=info AND source=task",
			q:      "NOT level=info AND source=task",
			wantID: []int64{3, 2},
		},
		{
			name:   "agent=a2",
			q:      "agent=a2",
			wantID: []int64{3},
		},
		{
			name:   "task=T2",
			q:      "task=T2",
			wantID: []int64{2},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ls.Query(ctx, Query{TenantID: "t1", Q: c.q, Limit: 100})
			if err != nil {
				t.Fatalf("query %q: %v", c.q, err)
			}
			if len(out) != len(c.wantID) {
				t.Fatalf("want %d hits %v, got %d: %#v", len(c.wantID), c.wantID, len(out), out)
			}
			for i, want := range c.wantID {
				if out[i].ID != want {
					t.Fatalf("hit %d: want ID %d, got %d (msg=%q)", i, want, out[i].ID, out[i].Message)
				}
			}
		})
	}
}

func TestMemoryQuery_QPrecedenceOverKeyword(t *testing.T) {
	// Q 非空时优先于 Keyword：Keyword 应被忽略。
	ctx := context.Background()
	ls := NewMemory(0)
	seedForQ(t, ls)

	// Q=level=error（命中 e2,e3）；Keyword=heartbeat（命中 e5）。
	// 若 Q 优先，结果应为 e2,e3；若 Keyword 仍生效（AND 语义），结果应为空。
	out, err := ls.Query(ctx, Query{TenantID: "t1", Q: "level=error", Keyword: "heartbeat", Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("Q 应优先于 Keyword，want 2 hits (e2,e3), got %d: %#v", len(out), out)
	}
	for _, e := range out {
		if e.Level != "error" {
			t.Fatalf("应仅返回 level=error，got level=%q", e.Level)
		}
	}
}

func TestMemoryQuery_InvalidQReturnsError(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	seedForQ(t, ls)

	cases := []string{
		"level=",          // 缺值
		"foo=bar",         // 未知字段
		"level=error AND", // AND 后无表达式
		"(level=error",    // 括号未闭合
		`message="x"`,     // message 用 = 不允许
	}
	for _, q := range cases {
		_, err := ls.Query(ctx, Query{TenantID: "t1", Q: q})
		if err == nil {
			t.Fatalf("q=%q 应返回解析错误", q)
		}
		if !strings.Contains(err.Error(), "解析结构化查询失败") {
			t.Fatalf("q=%q 错误应包含\"解析结构化查询失败\"前缀，got %v", q, err)
		}
	}
}

func TestMemoryQuery_QWithBaseFilters(t *testing.T) {
	// Q 与基础条件（DeviceID/Level/From/To）联合过滤。
	ctx := context.Background()
	ls := NewMemory(0)
	seedForQ(t, ls)

	// 基础条件 DeviceID=dev-1（命中 e1,e2,e4,e5）+ Q=level=error（命中 e2,e3）→ 交集 e2。
	out, err := ls.Query(ctx, Query{TenantID: "t1", DeviceID: "dev-1", Q: "level=error", Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 1 || out[0].ID != 2 {
		t.Fatalf("want e2, got %#v", out)
	}
}

func TestMemoryQuery_QPagination(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	seedForQ(t, ls)

	// Q=level!=info 命中 e4,e3,e2（倒序）。limit=2 offset=0 → e4,e3。
	p1, err := ls.Query(ctx, Query{TenantID: "t1", Q: "level!=info", Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(p1) != 2 || p1[0].ID != 4 || p1[1].ID != 3 {
		t.Fatalf("page1 失败：want IDs [4,3], got %#v", p1)
	}
	// limit=2 offset=2 → e2。
	p2, err := ls.Query(ctx, Query{TenantID: "t1", Q: "level!=info", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(p2) != 1 || p2[0].ID != 2 {
		t.Fatalf("page2 失败：want ID [2], got %#v", p2)
	}
}

// ===========================================================================
// 向后兼容：Q 为空时回退到 Keyword LIKE
// ===========================================================================

func TestQuery_BackwardCompat(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	seedForQ(t, ls)

	// Q 为空，Keyword 非空：保持原有 LIKE 行为。
	out, err := ls.Query(ctx, Query{TenantID: "t1", Keyword: "panic", Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("Keyword LIKE 应命中 2 条（e2,e3），got %d", len(out))
	}
	for _, e := range out {
		if !strings.Contains(e.Message, "panic") {
			t.Fatalf("LIKE panic 失败：%q", e.Message)
		}
	}

	// Q 与 Keyword 均空：返回全部（受 Limit 约束）。
	all, err := ls.Query(ctx, Query{TenantID: "t1", Limit: 100})
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("无过滤应返回 5 条，got %d", len(all))
	}
}

// ===========================================================================
// Handler：q 查询参数集成
// ===========================================================================

func newTestHandler() (*Handler, *MemoryLogStore) {
	ls := NewMemory(0)
	return NewHandler(ls), ls
}

func doLogs(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	h.handleLogs(rec, req)
	return rec
}

func TestHandler_QueryParam(t *testing.T) {
	h, ls := newTestHandler()
	seedForQ(t, ls)

	// q=level=error：应返回 e2,e3。
	rec := doLogs(t, h, "/api/v1/logs?q=level%3Derror&limit=100")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 error entries, got %d", len(got))
	}
	for _, e := range got {
		if e.Level != "error" {
			t.Fatalf("应仅返回 error，got %q", e.Level)
		}
	}
}

func TestHandler_QueryParamComplex(t *testing.T) {
	h, ls := newTestHandler()
	seedForQ(t, ls)

	// q=level=error AND device=dev-1 AND message~"panic"：仅 e2。
	// URL 编码：空格=%20, "=无需编码, "~"无需编码, 引号=%22
	rec := doLogs(t, h, "/api/v1/logs?q=level%3Derror%20AND%20device%3Ddev-1%20AND%20message~%22panic%22&limit=100")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("want 1 hit (e2), got %#v", got)
	}
}

func TestHandler_QueryParamInvalidReturns400(t *testing.T) {
	h, ls := newTestHandler()
	seedForQ(t, ls)

	cases := []string{
		"q=level%3D",            // level=
		"q=foo%3Dbar",           // 未知字段
		"q=level%3Derror%20AND", // AND 后无表达式
	}
	for _, c := range cases {
		rec := doLogs(t, h, "/api/v1/logs?"+c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %q: want 400, got %d: %s", c, rec.Code, rec.Body.String())
		}
		// 响应体应包含 error 字段。
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("case %q: decode: %v", c, err)
		}
		if _, ok := body["error"]; !ok {
			t.Fatalf("case %q: 响应应含 error 字段", c)
		}
		if !strings.Contains(body["error"], "invalid query syntax") {
			t.Fatalf("case %q: error 应含 \"invalid query syntax\"，got %q", c, body["error"])
		}
	}
}

func TestHandler_KeywordBackwardCompat(t *testing.T) {
	h, ls := newTestHandler()
	seedForQ(t, ls)

	// 不带 q，仅 keyword=panic：保持 LIKE 行为。
	rec := doLogs(t, h, "/api/v1/logs?keyword=panic&limit=100")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Keyword LIKE 应命中 2 条，got %d", len(got))
	}
}

func TestHandler_QPrecedenceOverKeyword(t *testing.T) {
	h, ls := newTestHandler()
	seedForQ(t, ls)

	// 同时带 q=level=error 和 keyword=heartbeat：Q 优先，返回 error 条目。
	rec := doLogs(t, h, "/api/v1/logs?q=level%3Derror&keyword=heartbeat&limit=100")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Q 应优先，want 2 error entries, got %d", len(got))
	}
}

// ===========================================================================
// SQL 后端：结构化查询集成（用 go-sqlmock 模拟，无需真实 MySQL）
// ===========================================================================

// newSQLMock 构造 sqlmock + SQLLogStore（跳过 initSchema，测试不依赖真实建表）。
func newSQLMock(t *testing.T) (*SQLLogStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return &SQLLogStore{db: db}, mock
}

func TestSQLQuery_WithStructuredQuery(t *testing.T) {
	store, mock := newSQLMock(t)
	ctx := context.Background()

	// 策略 A：q.Q 非空时，SQL 层粗筛 maxQueryLimit 条（不用 OFFSET），
	// SQL WHERE 仅含基础条件（此处仅 tenant_id），再在内存用 AST 过滤。
	// q.Q=level=error AND device=dev-1，基础条件只有 TenantID=t1。
	mock.ExpectQuery(`SELECT id, tenant_id, device_id, agent_id, task_id, ts, level, source, message FROM log_entries WHERE tenant_id = \? ORDER BY ts DESC LIMIT \?`).
		WithArgs("t1", maxQueryLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "device_id", "agent_id", "task_id", "ts", "level", "source", "message"}).
			AddRow(2, "t1", "dev-1", "a1", "T2", time.Now(), "error", "task", "panic: nil").
			AddRow(3, "t1", "dev-2", "a2", "T3", time.Now(), "error", "task", "panic: oom").
			AddRow(5, "t1", "dev-1", "a1", "", time.Now(), "info", "system", "heartbeat"))

	out, err := store.Query(ctx, Query{TenantID: "t1", Q: "level=error AND device=dev-1", Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// AST 过滤后仅命中 ID=2（error + dev-1）。
	if len(out) != 1 || out[0].ID != 2 {
		t.Fatalf("want 1 hit (ID=2), got %#v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLQuery_QPrecedenceOverKeyword(t *testing.T) {
	store, mock := newSQLMock(t)
	ctx := context.Background()

	// Q 非空时 Keyword 被清空，SQL WHERE 不应含 message LIKE。
	mock.ExpectQuery(`SELECT id, tenant_id, device_id, agent_id, task_id, ts, level, source, message FROM log_entries WHERE tenant_id = \? ORDER BY ts DESC LIMIT \?`).
		WithArgs("t1", maxQueryLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "device_id", "agent_id", "task_id", "ts", "level", "source", "message"}).
			AddRow(1, "t1", "dev-1", "a1", "", time.Now(), "info", "system", "ok").
			AddRow(2, "t1", "dev-1", "a1", "", time.Now(), "error", "task", "boom"))

	out, err := store.Query(ctx, Query{TenantID: "t1", Q: "level=error", Keyword: "heartbeat", Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 1 || out[0].ID != 2 {
		t.Fatalf("Q 应优先，want 1 hit (ID=2 error), got %#v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLQuery_InvalidQReturnsError(t *testing.T) {
	store, _ := newSQLMock(t)
	ctx := context.Background()

	_, err := store.Query(ctx, Query{TenantID: "t1", Q: "level="})
	if err == nil {
		t.Fatal("应返回解析错误")
	}
	if !strings.Contains(err.Error(), "解析结构化查询失败") {
		t.Fatalf("错误应含\"解析结构化查询失败\"前缀，got %v", err)
	}
}

func TestSQLQuery_BackwardCompat(t *testing.T) {
	store, mock := newSQLMock(t)
	ctx := context.Background()

	// Q 为空，Keyword 非空：保持原有 SQL WHERE + LIMIT + OFFSET 行为。
	// SQL 应含 message LIKE ? 且使用 LIMIT ? OFFSET ?。
	mock.ExpectQuery(`SELECT id, tenant_id, device_id, agent_id, task_id, ts, level, source, message FROM log_entries WHERE tenant_id = \? AND message LIKE \? ORDER BY ts DESC LIMIT \? OFFSET \?`).
		WithArgs("t1", "%panic%", 100, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "device_id", "agent_id", "task_id", "ts", "level", "source", "message"}).
			AddRow(2, "t1", "dev-1", "a1", "T2", time.Now(), "error", "task", "panic: nil"))

	out, err := store.Query(ctx, Query{TenantID: "t1", Keyword: "panic", Limit: 100, Offset: 5})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 1 || out[0].ID != 2 {
		t.Fatalf("want 1 hit (ID=2), got %#v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLQuery_QWithBaseFilters(t *testing.T) {
	store, mock := newSQLMock(t)
	ctx := context.Background()

	// 基础条件 DeviceID=dev-1 + Level=error + Q=device=dev-1（冗余但验证组合）。
	// SQL WHERE 应含 device_id=? AND level=? AND tenant_id=?，LIMIT maxQueryLimit。
	mock.ExpectQuery(`SELECT id, tenant_id, device_id, agent_id, task_id, ts, level, source, message FROM log_entries WHERE tenant_id = \? AND device_id = \? AND level = \? ORDER BY ts DESC LIMIT \?`).
		WithArgs("t1", "dev-1", "error", maxQueryLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "device_id", "agent_id", "task_id", "ts", "level", "source", "message"}).
			AddRow(2, "t1", "dev-1", "a1", "T2", time.Now(), "error", "task", "panic: nil").
			AddRow(7, "t1", "dev-1", "a1", "T7", time.Now(), "error", "task", "ok"))

	out, err := store.Query(ctx, Query{TenantID: "t1", DeviceID: "dev-1", Level: "error", Q: "device=dev-1", Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 hits, got %d", len(out))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
