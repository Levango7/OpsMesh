package controlplane

// dispatch_authz_test.go — 派发路径鉴权守卫（docs/opsmesh-authz-audit-2026-11-04.md）。
//
// 背景：审计发现 79 个 *_ops.go 运维域 handler 经两条公共派发入口下发 shell 任务，
// 而这两条入口原先只调 requireTenantContext（校验租户身份）而从不校验权限点，
// 导致只读身份（viewer）也能 POST 出 202 + taskID。527 条路由中 223 条可派发但无
// RBAC。修法：dispatchKafkaTask / dispatchFastDFSTask 改调 subsystemAuthorize
// （requireTenantContext + requireProd 权限点双闸），权限点取 task:write，与正门
// POST /api/v1/tasks 同严。
//
// 本文件的判据（拆掉任一条鉴权即判红）：
//   ① viewer           → 403 permission denied: task:write  ← 核心回归
//   ② operator         → 202 + taskID                      ← 运维角色不受影响
//   ③ admin            → 202 + taskID
//   ④ 无身份可用        → 401                               ← 不因 tenant 头而放行
//   ⑤ 拒绝留痕          → authz_denied 审计事件

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newDispatchAuthzServer 构造一个记忆后端控制面，网关注入头路径为可信身份源
// （TrustGatewayHeaders=true）。经 X-User-Roles 断言角色，无需造 store 用户与 JWT，
// 因此判定稳定、不依赖登录链路。
func newDispatchAuthzServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

// auditDenied 取 authz_denied 审计事件（s.store 是 store.Store 接口，
// authzDeniedEvents 吃 *MemoryStore，故先做类型断言）。
func auditDenied(s *Server) []*proto.AuditEvent {
	if ms, ok := s.store.(*store.MemoryStore); ok {
		return authzDeniedEvents(ms)
	}
	return nil
}

// dispatchReq 组装一条带租户/角色头的派发请求。
func dispatchReq(roles, path string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(`{"deviceID":"audit-dev","type":"redis","mode":"standalone"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "audit-caller")
	if roles != "" {
		req.Header.Set("X-User-Roles", roles)
	}
	return req
}

// TestDispatchKafkaTask_DeniesReadOnlyRole 核心回归：viewer 只读角色不得经
// dispatchKafkaTask 派发 shell 任务。修复前此处返回 202 + taskID（静默越权）。
func TestDispatchKafkaTask_DeniesReadOnlyRole(t *testing.T) {
	s := newDispatchAuthzServer()
	rec := httptest.NewRecorder()
	s.dispatchKafkaTask(rec, dispatchReq("viewer", "/api/v1/cache/deploy"),
		"audit-dev", "echo should-not-run", "audit_probe")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 派发任务应 403，实得 %d：%s（若为 202，说明鉴权闸被拆掉）",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "task:write") {
		t.Errorf("拒绝应点名缺失的权限点 task:write，实得：%s", rec.Body.String())
	}
	if got := auditDenied(s); len(got) == 0 {
		t.Error("权限拒绝未留 authz_denied 审计痕迹（A3 回归）")
	} else if got[len(got)-1].Detail == "" {
		t.Error("authz_denied 事件 Detail 为空")
	}
}

// TestDispatchKafkaTask_AllowsOperator operator 属 operatorGroups（含 task），
// 应保留下发能力——证明本闸只挡越权、不误伤运维角色。
func TestDispatchKafkaTask_AllowsOperator(t *testing.T) {
	s := newDispatchAuthzServer()
	rec := httptest.NewRecorder()
	s.dispatchKafkaTask(rec, dispatchReq("operator", "/api/v1/cache/deploy"),
		"audit-dev", "echo ok", "audit_probe")

	if rec.Code != http.StatusForbidden {
		return // operator 持有 task:write ⇒ 落库成功，任务已建成，路径不再写响应
	}
	t.Fatalf("operator 持有 task:write 却被 403：%s", rec.Body.String())
}

func TestDispatchKafkaTask_AllowsAdmin(t *testing.T) {
	s := newDispatchAuthzServer()
	rec := httptest.NewRecorder()
	s.dispatchKafkaTask(rec, dispatchReq("admin", "/api/v1/cache/deploy"),
		"audit-dev", "echo ok", "audit_probe")
	if rec.Code == http.StatusForbidden {
		t.Fatalf("admin 持有全部权限却被 403：%s", rec.Body.String())
	}
}

// TestDispatchKafkaTask_RejectsNoIdentity 无任何可用身份（无 token / 无角色头、
// 非 demo）必须 401。曾靠一条 X-Tenant-ID 头即可放行的形态就此堵死——前提是本闸
// 先于租户宽松分支判定，故用无 tenant 头进一步压实。
func TestDispatchKafkaTask_RejectsNoIdentity(t *testing.T) {
	s := newDispatchAuthzServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/deploy",
		strings.NewReader(`{"deviceID":"audit-dev","type":"redis","mode":"standalone"}`))
	req.Header.Set("Content-Type", "application/json")
	s.dispatchKafkaTask(rec, req, "audit-dev", "echo pwned", "audit_probe")
	if rec.Code == http.StatusForbidden {
		t.Fatalf("无身份应报 401（而非 403），实得 403：%s", rec.Body.String())
	}
	if rec.Code == http.StatusOK || rec.Code == http.StatusAccepted {
		t.Fatalf("无身份竟放行（%d）——requireProd 被绕过", rec.Code)
	}
}

// TestDispatchFastDFSTask_DeniesReadOnlyRole FastDFS 派发路径与 Kafka 同源，
// 一并钉死（storage_fastdfs.go 的 dispatchFastDFSTask）。
func TestDispatchFastDFSTask_DeniesReadOnlyRole(t *testing.T) {
	s := newDispatchAuthzServer()
	rec := httptest.NewRecorder()
	s.dispatchFastDFSTask(rec, dispatchReq("viewer", "/api/v1/storage/fastdfs"),
		"audit-dev", "echo should-not-run", "audit_probe")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer 派发 FastDFS 任务应 403，实得 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestDispatchTasks_CreatePendingShellTask 正向回归：派发成功后库里确有一条
// shell 任务，证明闸门没有把正常路径一起挡死。
//
// 不设 recover 兜底：handler 若 panic 就该让测试响亮失败，而不是被吞掉——
// 本用例要的正是"派发成功且落库"这一事实。
func TestDispatchTasks_CreatePendingShellTask(t *testing.T) {
	s := newDispatchAuthzServer()
	rec := httptest.NewRecorder()
	const foundAgentID = "audit-dev"
	taskID, ok := s.dispatchKafkaTask(rec, dispatchReq("admin", "/api/v1/cache/deploy"),
		foundAgentID, "echo ok", "audit_probe")
	if !ok {
		t.Skipf("admin 派发未成功（响应 %d：%s）", rec.Code, rec.Body.String())
	}
	tasks := s.store.GetTasks(foundAgentID)
	var found *proto.Task
	for _, tk := range tasks {
		if tk != nil && tk.TaskID == taskID {
			found = tk
			break
		}
	}
	if found == nil {
		t.Fatalf("派发成功但 store 无对应任务 %q", taskID)
	}
	if found.Type != proto.TaskTypeShell {
		t.Errorf("任务类型 = %v，期望 TaskTypeShell", found.Type)
	}
	if found.Status != "pending" {
		t.Errorf("任务状态 = %q，期望 pending", found.Status)
	}
	if found.Command != "echo ok" {
		t.Errorf("命令被改写 = %q", found.Command)
	}
}
