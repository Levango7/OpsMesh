// routing_fixes_test.go — 2026-10-21 第二批路由/权限缺陷修复的 mux 级守卫。
//
// 背景：这一批修复的四个症状（batch/canary 子路径恒 404、裸 GET task 详情恒 404、
// PUT alert-rule 恒 405、POST lb/lvs/backend 被 301 吞掉）共同点是**注册与解析不一致**，
// 而既有测试全部直接调用 handler（绕过 mux），因此注册写错也测不出来。
// 本文件的用例一律经**真实注册表**（registerTaskRoutes / registerBatchCanaryRoutes /
// registerLBRoutes + alert-rules 两行内联注册）发请求，把「注册写错」纳入守卫。
//
// 环境：memory store + demo 模式（requireAuth=false、Demo=true ⇒ 权限闸放行、
// 聚焦路由分发）+ X-Tenant-ID 头模拟网关注入的租户上下文。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

const rfTenant = "t-rf"

// rfServer 造 demo 模式 Server（复用 newBatchTestServer 的形：memory store +
// requireAuth=false + Demo=true + 空 batchStore），并注册若干属于 rfTenant 的 agent
// （批量执行/灰度推进/任务下发都要能按 deviceID 解析到同租户 agent）。
func rfServer(t *testing.T, agentN int) *Server {
	t.Helper()
	s := newBatchTestServer()
	for i := 0; i < agentN; i++ {
		s.store.Register(&proto.AgentInfo{
			AgentID:  fmt.Sprintf("dev-rf-%d", i),
			Hostname: fmt.Sprintf("host-rf-%d", i),
			TenantID: rfTenant,
			Status:   "online",
			Segment:  "seg-rf",
		})
	}
	return s
}

// rfMux 按生产注册表构建 mux（只含本批修复涉及的域）。
// 注 1：顺序与 server_lifecycle.go Start() 一致——registerTaskRoutes 在前（{id} 通配
// 先注册），字面量与子路径在后；这同时守住「注册顺序写反会 panic」的冲突检测。
// 注 2：alert-rules 的两行照抄 Start() 的内联注册（无独立 register 函数）。
func rfMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	s.registerTaskRoutes(mux)
	mux.HandleFunc("GET /api/v1/tasks/batch", s.handleBatchCreateTasks) // 同 Start() 的字面量注册
	s.registerBatchCanaryRoutes(mux)
	s.registerLBRoutes(mux)
	mux.HandleFunc("/api/v1/alert-rules", s.handleAlertRules)
	mux.HandleFunc("/api/v1/alert-rules/", s.handleAlertRuleRouting)
	return mux
}

// rfDo 经 mux 发一次请求（带租户/用户头，模拟网关注入身份）。
func rfDo(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-ID", rfTenant)
	r.Header.Set("X-User-Id", "u-rf")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func rfDecode(t *testing.T, body string, v interface{}) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
}

// =============================================================================
// FIX 1 附带：任务域注册表不得与 "GET /api/v1/tasks/{id}" 冲突
// =============================================================================

// TestTaskRouteTable_NoServeMuxConflict 回归守卫：
// 修 FIX 2（新增 "GET /api/v1/tasks/{id}" 通配）后，任务域的**字面量**路径若仍按
// 无方法限定的裸注册（"/api/v1/tasks/batch、/batch-exec、/canary"），就会与该通配
// 构成 ServeMux 冲突——字面量路径更具体但方法集更宽，Go 无法裁定优先级，
// Start() 在注册时**直接 panic，控制面起不来**（本用例的 rfMux 即复现点）。
// 判据：按生产顺序整表注册不 panic；字面量 GET 仍由 handler 返回 405（行为不变）。
func TestTaskRouteTable_NoServeMuxConflict(t *testing.T) {
	s := rfServer(t, 1)
	mux := rfMux(s) // 有冲突会在此 panic，用例直接红

	for _, p := range []string{"/api/v1/tasks/batch", "/api/v1/tasks/batch-exec", "/api/v1/tasks/canary"} {
		if rec := rfDo(t, mux, http.MethodGet, p, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s = %d, want 405（handler 内方法检查，注册不得改变该行为）; body=%s", p, rec.Code, rec.Body.String())
		}
	}
	// 通配详情仍可达（字面量让位给更具体的模式后，{id} 不被吞）。
	if rec := rfDo(t, mux, http.MethodGet, "/api/v1/tasks/rf-no-such-task", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /tasks/{unknown} = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// FIX 1：GET /api/v1/tasks/batch/{id} 与 /api/v1/tasks/canary/{id}[/advance|/traffic-split|/metrics]
// =============================================================================

// TestFix1_BatchCanarySubpathsReturn200ViaMux 回归守卫：
// 修复前注册的是**精确路径** "GET /api/v1/tasks/batch"，而 handler 解析的是
// "/api/v1/tasks/batch/{id}"（TrimPrefix 掉带斜杠前缀后为空 ⇒ 恒 404）；真实 {id}
// 请求落到 "/api/v1/tasks/" 子树 → handleTaskRouting → 404。于是 handleBatchStatus /
// handleCanaryStatus / handleCanaryAdvance 全是死代码，前端 batch/canary 详情与推进
// 永久 404。判据：经真实 mux 打 {id} 子路径（含 advance/traffic-split/metrics）拿 200。
func TestFix1_BatchCanarySubpathsReturn200ViaMux(t *testing.T) {
	s := rfServer(t, 4)
	mux := rfMux(s)

	// --- 批量执行：建一个真实 batch（返回 batchID）---
	rec := rfDo(t, mux, http.MethodPost, "/api/v1/tasks/batch-exec",
		`{"deviceIDs":["dev-rf-0","dev-rf-1","dev-rf-2","dev-rf-3"],"taskType":"shell","command":"echo rf"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tasks/batch-exec = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var batch struct {
		BatchID string `json:"batchID"`
	}
	rfDecode(t, rec.Body.String(), &batch)
	if batch.BatchID == "" {
		t.Fatalf("batchID 为空; body=%s", rec.Body.String())
	}

	// 修复点 1：GET /api/v1/tasks/batch/{id} → 200（修复前恒 404，handleBatchStatus 是死代码）。
	rec = rfDo(t, mux, http.MethodGet, "/api/v1/tasks/batch/"+batch.BatchID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/batch/{id} = %d, want 200 (回归：batch 详情曾恒 404); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), batch.BatchID) {
		t.Fatalf("GET /tasks/batch/{id} 响应未含 batchID=%s; body=%s", batch.BatchID, rec.Body.String())
	}
	// 未建过的 ID 仍须 404（防止把「查不到」变成假成功）。
	if rec := rfDo(t, mux, http.MethodGet, "/api/v1/tasks/batch/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /tasks/batch/nope = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// --- 灰度发布：建一个真实 canary（4 台 @50% ⇒ 2 个阶段，保证 advance 有 pending 阶段）---
	rec = rfDo(t, mux, http.MethodPost, "/api/v1/tasks/canary",
		`{"deviceIDs":["dev-rf-0","dev-rf-1","dev-rf-2","dev-rf-3"],"taskType":"shell","command":"echo rf","strategy":"percentage","percentage":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tasks/canary = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var canary struct {
		CanaryID string `json:"canaryID"`
	}
	rfDecode(t, rec.Body.String(), &canary)
	if canary.CanaryID == "" {
		t.Fatalf("canaryID 为空; body=%s", rec.Body.String())
	}

	// 修复点 2：GET /api/v1/tasks/canary/{id} → 200（修复前恒 404，handleCanaryStatus 是死代码）。
	rec = rfDo(t, mux, http.MethodGet, "/api/v1/tasks/canary/"+canary.CanaryID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/canary/{id} = %d, want 200 (回归：canary 详情曾恒 404); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), canary.CanaryID) {
		t.Fatalf("GET /tasks/canary/{id} 响应未含 canaryID=%s; body=%s", canary.CanaryID, rec.Body.String())
	}

	// 修复点 3：POST /api/v1/tasks/canary/{id}/advance → 200（修复前不可达，恒 404）。
	rec = rfDo(t, mux, http.MethodPost, "/api/v1/tasks/canary/"+canary.CanaryID+"/advance", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /tasks/canary/{id}/advance = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 修复点 4：traffic-split / metrics 子路径可达（经 handleCanaryEnhance，任务域前缀）。
	if rec := rfDo(t, mux, http.MethodPost, "/api/v1/tasks/canary/"+canary.CanaryID+"/traffic-split",
		`{"percentage":30}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /tasks/canary/{id}/traffic-split = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec := rfDo(t, mux, http.MethodGet, "/api/v1/tasks/canary/"+canary.CanaryID+"/metrics", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/canary/{id}/metrics = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// FIX 2：GET /api/v1/tasks/{id} 任务详情
// =============================================================================

// TestFix2_BareTaskDetailReturnsTaskJSON 回归守卫：
// 修复前 handleTaskRouting 只分派 {id}/cancel|result|approve|reject|retry|dead-letter，
// 裸 GET /api/v1/tasks/{id} 恒 404。前端 OSOptimizeView.vue / MiddlewareDeployView.vue
// 每 3s 轮询该端点，把 404 吞掉后只报「poll timeout」，执行输出永远不显示。
// 判据：经 mux 建任务 → GET 详情 200 且响应含 taskID（前端契约字段）。
func TestFix2_BareTaskDetailReturnsTaskJSON(t *testing.T) {
	s := rfServer(t, 2)
	mux := rfMux(s)

	rec := rfDo(t, mux, http.MethodPost, "/api/v1/tasks",
		`{"agentID":"dev-rf-0","type":"shell","command":"echo rf-detail"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tasks = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		TaskID string `json:"taskID"`
	}
	rfDecode(t, rec.Body.String(), &created)
	if created.TaskID == "" {
		t.Fatalf("taskID 为空; body=%s", rec.Body.String())
	}

	// 修复点：裸 GET /api/v1/tasks/{id} → 200，且契约字段 taskID 在响应体里。
	rec = rfDo(t, mux, http.MethodGet, "/api/v1/tasks/"+created.TaskID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/{id} = %d, want 200 (回归：裸详情曾恒 404); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), created.TaskID) {
		t.Fatalf("GET /tasks/{id} 响应未含 taskID=%s; body=%s", created.TaskID, rec.Body.String())
	}
	// 未知 ID → 404（不能把查不到变成 200 空壳）。
	if rec := rfDo(t, mux, http.MethodGet, "/api/v1/tasks/rf-no-such-task", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /tasks/{unknown} = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// FIX 3：PUT /api/v1/alert-rules/{id}
// =============================================================================

// TestFix3_AlertRulePutReturns200 回归守卫：
// 修复前 handleAlertRuleRouting 只实现 DELETE，PUT 落 default 分支恒 405，
// 前端 AlertRulesView.vue 的「编辑规则 → updateRule → PUT」永远失败。
// 请求体刻意用前端真实字段（name + duration 别名，而非 forDuration）。
func TestFix3_AlertRulePutReturns200(t *testing.T) {
	s := rfServer(t, 1)
	mux := rfMux(s)

	// 建规则（201）。
	rec := rfDo(t, mux, http.MethodPost, "/api/v1/alert-rules",
		`{"metric":"cpu.usage","op":">","threshold":90,"forDuration":"5m","severity":"critical","message":"cpu high","enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /alert-rules = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created AlertRule
	rfDecode(t, rec.Body.String(), &created)
	if created.ID == "" {
		t.Fatalf("rule ID 为空; body=%s", rec.Body.String())
	}

	// 修复点：PUT → 200，且 frontend 形态（name + duration）被接受、更新生效。
	rec = rfDo(t, mux, http.MethodPut, "/api/v1/alert-rules/"+created.ID,
		`{"name":"cpu-watch","metric":"cpu.usage","op":">=","threshold":80,"duration":"10m","severity":"warning","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /alert-rules/{id} = %d, want 200 (回归：PUT 曾恒 405); body=%s", rec.Code, rec.Body.String())
	}
	var updated AlertRule
	rfDecode(t, rec.Body.String(), &updated)
	if updated.ID != created.ID {
		t.Fatalf("updated.ID = %s, want %s", updated.ID, created.ID)
	}
	if updated.Threshold != 80 || updated.Op != ">=" || updated.Severity != "warning" || updated.Enabled {
		t.Fatalf("updated = %+v, want threshold=80 op=>= severity=warning enabled=false", updated)
	}
	if updated.Name != "cpu-watch" {
		t.Fatalf("updated.Name = %q, want cpu-watch（前端提交的 name 不应被静默丢弃）", updated.Name)
	}
	// duration 别名须落到 forDuration（10m ⇒ 600s，读回格式化为 "600s"）。
	if updated.ForDuration != "600s" {
		t.Fatalf("updated.ForDuration = %q, want 600s（duration 别名应映射到 forDuration）", updated.ForDuration)
	}

	// 未知 ID → 404；非法 op → 400。
	if rec := rfDo(t, mux, http.MethodPut, "/api/v1/alert-rules/rf-no-such-rule",
		`{"metric":"m","op":">","threshold":1}`); rec.Code != http.StatusNotFound {
		t.Fatalf("PUT /alert-rules/{unknown} = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if rec := rfDo(t, mux, http.MethodPut, "/api/v1/alert-rules/"+created.ID,
		`{"metric":"m","op":"~","threshold":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /alert-rules/{id} 非法 op = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// FIX 4：POST /api/v1/lb/lvs/backend
// =============================================================================

// TestFix4_LBBackendNoSlashRedirect 回归守卫：
// 修复前注册的是 "/api/v1/lb/lvs/backend/"（带尾斜杠），而 haproxy 孪生路径不带。
// Go 1.22 ServeMux 对 slash-less 请求回 301，客户端按 301 重发成 GET（方法丢失），
// handler 再以「method not allowed」拒绝。判据：POST 直达 200，且不被 301 改写。
func TestFix4_LBBackendNoSlashRedirect(t *testing.T) {
	s := rfServer(t, 1)
	mux := rfMux(s)

	body := `{"deviceID":"dev-rf-0","action":"add","backend":"10.0.0.1:80","weight":1}`
	rec := rfDo(t, mux, http.MethodPost, "/api/v1/lb/lvs/backend", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /lb/lvs/backend = %d, want 200 (回归：尾斜杠注册曾致 301→方法丢失); body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("POST /lb/lvs/backend 不应重定向，Location=%s", loc)
	}
	// haproxy 孪生路径同口径（注册对齐的另一半）。
	if rec := rfDo(t, mux, http.MethodPost, "/api/v1/lb/haproxy/backend", body); rec.Code != http.StatusOK {
		t.Fatalf("POST /lb/haproxy/backend = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
