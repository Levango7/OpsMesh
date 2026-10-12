package controlplane

// task_ownership_gate_test.go — TD-60 task 域「新任务归哪侧」闸门（TASK_SVC_OWNS_NEW_TASKS）的守卫。
//
// 为什么需要这道守卫：
//  1. 建任务入口有**四个**（单建 / 批量下发 / 批量执行 / 灰度创建），而它们分散在两个文件里，
//     首版实现只拦了前两个（peer 侧核验发现，本线补齐）——漏拦的后果是"开关只关了一半"：
//     面板上单建被拒，批量却照样建在被切走的一侧，且**无任何报错**。
//  2. 闸门与鉴权的**顺序**是本测试钉死的第二件事：必须先回答"有没有权限"（401/403），
//     再回答"该由谁做"（409）。顺序颠倒会把"无权"伪装成"归属冲突"，把排障指向错误方向。
//  3. 开关关着时必须**完全不影响**既有行为（默认 false ⇒ 行为与切流前逐字一致），否则
//     这个开关本身就成了新的事故源。
//  4. 闸门与**路由开关**联锁（缺口 2）：task 域被 OPSMESH_SERVICE_PROXY 停用时，
//     即便两个开关都打开也**不得** 409——否则单体"读得到任务、建不了任务"，
//     前端没有任何一条路可走（自伤式停摆）。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/cutover"
	"github.com/Levango7/OpsMesh/internal/controlplane/svcproxy"
)

// ownershipTestServer 造一个带 cutoverRouter 的 Server（demo 模式：requireAuth=false，
// 权限闸放行，从而单独考察归属闸门；鉴权顺序另用 requireAuth=true 的实例验）。
func ownershipTestServer(t *testing.T, requireAuth bool) *Server {
	t.Helper()
	s, _ := newBenchServer()
	s.requireAuth = requireAuth
	// 空名册（两个来源都空 = 合法）：task 闸门与名册无关，这里只要 router 存在。
	r, err := cutover.LoadRoster("", "")
	if err != nil {
		t.Fatalf("空名册应合法: %v", err)
	}
	s.cutoverRouter = cutover.NewRouter(r)
	return s
}

// entryPoints 四个建任务入口。body 刻意给最小合法 JSON：闸门在 body 解析**之前**，
// 因此即便 body 不合法也应当先被 409 拦下——这本身就是"闸门位置正确"的证据。
func taskCreateEntryPoints() []struct {
	name   string
	path   string
	handle func(*Server, http.ResponseWriter, *http.Request)
	body   string
} {
	return []struct {
		name   string
		path   string
		handle func(*Server, http.ResponseWriter, *http.Request)
		body   string
	}{
		{"单建", "/api/v1/tasks", (*Server).handleCreateTask, `{"agentID":"dev-1","type":"shell","command":"echo hi"}`},
		{"批量下发", "/api/v1/tasks/batch", (*Server).handleBatchCreateTasks, `{"targets":["dev-1"],"type":"shell","command":"echo hi"}`},
		{"批量执行", "/api/v1/tasks/batch-exec", (*Server).handleBatchExec, `{"agentIDs":["dev-1"],"type":"shell","command":"echo hi"}`},
		{"灰度创建", "/api/v1/tasks/canary", (*Server).handleCanaryCreate, `{"deviceIDs":["dev-1"],"type":"shell","command":"echo hi"}`},
	}
}

func doTaskCreate(s *Server, ep struct {
	name   string
	path   string
	handle func(*Server, http.ResponseWriter, *http.Request)
	body   string
}) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, ep.path, bytes.NewReader([]byte(ep.body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "u-admin")
	rec := httptest.NewRecorder()
	ep.handle(s, rec, req)
	return rec
}

// TestTaskOwnershipGateCoversAllFourEntryPoints 开关打开时，四个入口**全部**必须 409 + 指引。
// 少一个就是"开关只关了一半"——演练报告 R2/施工图 §6 第 3 步要闭合的正是这件事。
func TestTaskOwnershipGateCoversAllFourEntryPoints(t *testing.T) {
	t.Setenv("TASK_SVC_OWNS_NEW_TASKS", "true")
	t.Setenv("TASK_SVC_PROXY_ENABLED", "true") // 总闸（见 cutover.Router.TaskEnabled）
	t.Setenv(svcproxy.EnvKey, "")              // 联锁前提：task 域由对侧服务（见 taskDomainProxied）
	s := ownershipTestServer(t, false)

	for _, ep := range taskCreateEntryPoints() {
		rec := doTaskCreate(s, ep)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s 开关打开时应 409，实得 %d：%s", ep.name, rec.Code, strings.TrimSpace(rec.Body.String()))
			continue
		}
		if !strings.Contains(rec.Body.String(), "task-svc") {
			t.Errorf("%s 的 409 文案未指引去 task-svc 侧：%s", ep.name, rec.Body.String())
		}
	}
}

// TestTaskOwnershipGateOffChangesNothing 开关关着（默认）时，四个入口不得因本闸门改变行为：
// 断言"不是 409"即可——具体是 201 还是 400 取决于 body/设备存在性，与本闸门无关，
// 但**绝不能**是 409（那意味着默认配置被新开关改坏了）。
func TestTaskOwnershipGateOffChangesNothing(t *testing.T) {
	t.Setenv("TASK_SVC_OWNS_NEW_TASKS", "false")
	t.Setenv("TASK_SVC_PROXY_ENABLED", "true")
	s := ownershipTestServer(t, false)

	for _, ep := range taskCreateEntryPoints() {
		if rec := doTaskCreate(s, ep); rec.Code == http.StatusConflict {
			t.Errorf("%s 开关关着却返回 409（默认配置被改坏）：%s", ep.name, rec.Body.String())
		}
	}
}

// TestTaskOwnershipGateRunsAfterAuth 顺序判据：无权时必须是 401/403，而不是 409。
// 用 requireAuth=true 且无任何凭证的实例——requireProd 先拒，闸门不该有机会说话。
func TestTaskOwnershipGateRunsAfterAuth(t *testing.T) {
	t.Setenv("TASK_SVC_OWNS_NEW_TASKS", "true")
	t.Setenv("TASK_SVC_PROXY_ENABLED", "true")
	t.Setenv(svcproxy.EnvKey, "") // 联锁前提：task 域由对侧服务
	s := ownershipTestServer(t, true)

	for _, ep := range taskCreateEntryPoints() {
		rec := doTaskCreate(s, ep)
		if rec.Code == http.StatusConflict {
			t.Errorf("%s 无凭证却拿到 409（归属闸门跑到了鉴权前面——排障会被指向错误方向）", ep.name)
			continue
		}
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Errorf("%s 无凭证应 401/403，实得 %d：%s", ep.name, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}

// TestTaskOwnershipGateInterlockedWithRouting 联锁判据（缺口 2）：task 域路由被停用
// （OPSMESH_SERVICE_PROXY 含 task ⇒ 单体回落到本地实现）时，即便两个开关都打开，
// 四个入口也**不得** 409。否则单体「读得到任务、却拒绝建任务」，而聚合层
// /api/v1/task-svc/* 前缀同时不可达 ⇒ 前端没有任何一条路能建任务（自伤式停摆）。
func TestTaskOwnershipGateInterlockedWithRouting(t *testing.T) {
	t.Setenv("TASK_SVC_OWNS_NEW_TASKS", "true")
	t.Setenv("TASK_SVC_PROXY_ENABLED", "true")
	t.Setenv(svcproxy.EnvKey, "task") // 域停用 = 单体在服务任务
	s := ownershipTestServer(t, false)

	for _, ep := range taskCreateEntryPoints() {
		if rec := doTaskCreate(s, ep); rec.Code == http.StatusConflict {
			t.Errorf("%s 域停用时却 409（归属闸门未与路由联锁——自伤式停摆）：%s",
				ep.name, strings.TrimSpace(rec.Body.String()))
		}
	}
}
