package controlplane

// operator_verbs_test.go — §3.12 的用户症状级守卫（2026-10-14 用户裁定：RBAC
// 矩阵细化——operator 补 task:cancel / alert:ack / alert:silence，不走归属授权）。
//
// 演练症状（三个人设独立复现）：operator 建得了任务（POST /tasks 201），取消自己
// 建的任务 403 `task:cancel`；告警确认/静默同样 403。矩阵洞修好后，本文件把
// 用户可见症状钉住：同一个人、同一个会话、这三件事必须做得了。
//
// 同时钉**边界**：审批类（task:approve）仍 admin-only——补作业动词不等于把管理
// 动词一起放开；viewer 仍无取消权（403）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// operatorVerbServer 造一个非 demo、信任网关头、requireAuth=false 的控制面：
// X-User-Roles 即权威角色声明（与演练人设的请求形态一致），RBAC 展开走
// requireProd → RolePermissions() 单一来源。
func operatorVerbServer(t *testing.T) (*Server, *store.MemoryStore) {
	t.Helper()
	st := store.NewMemoryStore()
	st.Register(&proto.AgentInfo{AgentID: "dev-op", TenantID: "t1", Status: "online", Segment: "seg-a"})
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}, st
}

func doWithRoles(t *testing.T, s *Server, method, path, roles, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "op-1")
	req.Header.Set("X-User-Roles", roles)
	rec := httptest.NewRecorder()
	switch {
	case strings.HasPrefix(path, "/api/v1/tasks"):
		s.handleTaskRouting(rec, req)
	case strings.HasPrefix(path, "/api/v1/alerts"):
		s.handleAlertRouting(rec, req)
	default:
		t.Fatalf("未覆盖的路由：%s", path)
	}
	return rec
}

// TestOperator_CanCancelTaskAndAckAlert §3.12 主判据：operator 取消任务、
// 确认告警、静默告警都不得再是 403（权限缺失）。
func TestOperator_CanCancelTaskAndAckAlert(t *testing.T) {
	s, st := operatorVerbServer(t)
	st.CreateTask(&proto.Task{AgentID: "dev-op", TenantID: "t1", Type: proto.TaskTypeShell, Command: "echo hi"})
	st.AddAlert(&proto.Alert{AlertID: "al-1", TenantID: "t1", Severity: "critical", Message: "cpu"})

	tasks := st.AllTasks("t1")
	if len(tasks) != 1 {
		t.Fatalf("前置失败：应有 1 个任务，实得 %d", len(tasks))
	}
	alerts := st.Alerts("t1")
	if len(alerts) != 1 {
		t.Fatalf("前置失败：应有 1 个告警，实得 %d", len(alerts))
	}

	if rec := doWithRoles(t, s, http.MethodPost, "/api/v1/tasks/"+tasks[0].TaskID+"/cancel", "operator", ""); rec.Code != http.StatusOK {
		t.Errorf("operator 取消任务应 200（§3.12 原状：403 task:cancel），实得 %d：%s", rec.Code, rec.Body.String())
	}
	if rec := doWithRoles(t, s, http.MethodPost, "/api/v1/alerts/al-1/ack", "operator", ""); rec.Code != http.StatusOK {
		t.Errorf("operator 确认告警应 200（原状：403 alert:ack），实得 %d：%s", rec.Code, rec.Body.String())
	}
	if rec := doWithRoles(t, s, http.MethodPost, "/api/v1/alerts/al-1/silence", "operator", `{"durationMinutes":60}`); rec.Code != http.StatusOK {
		t.Errorf("operator 静默告警应 200（原状：403 alert:silence），实得 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestOperator_StillCannotApproveTasks 边界：审批类动作仍 admin-only
// （补作业动词 ≠ 放开管理动词）。
func TestOperator_StillCannotApproveTasks(t *testing.T) {
	s, st := operatorVerbServer(t)
	st.CreateTask(&proto.Task{
		AgentID: "dev-op", TenantID: "t1", Type: proto.TaskTypeShell, Command: "echo hi",
		ApprovalRequired: true, Status: "pending_approval",
	})
	tasks := st.AllTasks("t1")
	if len(tasks) != 1 {
		t.Fatalf("前置失败：应有 1 个待审任务，实得 %d", len(tasks))
	}
	if rec := doWithRoles(t, s, http.MethodPost, "/api/v1/tasks/"+tasks[0].TaskID+"/approve", "operator", ""); rec.Code != http.StatusForbidden {
		t.Errorf("operator 审批任务应仍 403（task:approve 是管理动作），实得 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestViewer_StillCannotCancel 边界：viewer 无取消权（403）——放宽只针对 operator。
func TestViewer_StillCannotCancel(t *testing.T) {
	s, st := operatorVerbServer(t)
	st.CreateTask(&proto.Task{AgentID: "dev-op", TenantID: "t1", Type: proto.TaskTypeShell, Command: "echo hi"})
	tasks := st.AllTasks("t1")
	if len(tasks) != 1 {
		t.Fatalf("前置失败：应有 1 个任务，实得 %d", len(tasks))
	}
	if rec := doWithRoles(t, s, http.MethodPost, "/api/v1/tasks/"+tasks[0].TaskID+"/cancel", "viewer", ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer 取消任务应仍 403（最小权限边界），实得 %d：%s", rec.Code, rec.Body.String())
	}
}
