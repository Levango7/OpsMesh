package controlplane

import (
	"net/http"
	"testing"
)

// TestTaskProxyPermissionMatrix 钉住 TD-60 task 域 HTTP 代理的权限分级。
//
// 代理层此前对所有方法统一要求 task:read：只读凭证可建/取消/审批任务
// （权限放大），仅持写权限的调用方反而 403。判据必须与本地 handler
// （server_tasks.go）逐条一致，切流只换处理侧、不改变权限边界。
func TestTaskProxyPermissionMatrix(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/api/v1/tasks", "task:read"},
		{http.MethodGet, "/api/v1/tasks/t-1", "task:read"},
		{http.MethodGet, "/api/v1/tasks/t-1/result", "task:read"},
		{http.MethodPost, "/api/v1/tasks", "task:write"},
		{http.MethodPost, "/api/v1/tasks/t-1/retry", "task:write"},
		{http.MethodPost, "/api/v1/tasks/t-1/cancel", "task:cancel"},
		{http.MethodPost, "/api/v1/tasks/t-1/approve", "task:approve"},
		{http.MethodPost, "/api/v1/tasks/t-1/reject", "task:approve"},
		{http.MethodDelete, "/api/v1/tasks/t-1/dead-letter", "task:write"},
	}
	for _, c := range cases {
		if got := taskProxyPermission(c.method, c.path); got != c.want {
			t.Errorf("taskProxyPermission(%s %s) = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}
