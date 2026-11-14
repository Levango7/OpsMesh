// netsec.go 提供 device-svc 的扩展网络安全运维 REST 端点（TD-60 device 域下沉）。
//
// 设计：
//   - 脚本构建复用 pkg/netsec（与 controlplane 同一权威实现，零漂移）
//   - 任务下发经 TaskDispatcher 接口注入（生产由 task-svc gRPC 实现；
//     未注入时端点返回 503，绝不静默假成功）
//   - 路由前缀 /api/v1/netsec/{ips,siem,honeypot,traffic,vpn,zerotrust}
//     与控制面同名端点契约一致；controlplane 经 service_proxy 以
//     /api/v1/device-svc/netsec/* 前缀转发
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Levango7/OpsMesh/pkg/netsec"
)

// ErrNoTaskDispatcher 未配置任务下发通道（TASK_SVC_GRPC_ADDR 为空或连接失败）。
var ErrNoTaskDispatcher = errors.New("task dispatcher not configured")

// TaskDispatcher 下发 shell 任务到指定设备，返回任务 ID。
// 生产实现：gRPC 调 task-svc TaskService.CreateTask（agent_id=deviceID）。
type TaskDispatcher interface {
	DispatchTask(ctx context.Context, deviceID, script, taskType string) (taskID string, err error)
}

// SetTaskDispatcher 注入任务下发通道（可选；main 启动时调用）。
// 未注入时 netsec 端点返回 503 —— 显式失败优先于假装成功。
func (g *Gateway) SetTaskDispatcher(td TaskDispatcher) {
	g.taskDispatcher = td
}

// RegisterNetSecRoutes 注册六个 netsec 端点（在 RegisterRoutes 内调用）。
func (g *Gateway) RegisterNetSecRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	handlers := map[string]netsec.Capability{
		"/api/v1/netsec/ips":       netsec.CapIPSIDS,
		"/api/v1/netsec/siem":      netsec.CapSIEM,
		"/api/v1/netsec/honeypot":  netsec.CapHoneypot,
		"/api/v1/netsec/traffic":   netsec.CapTrafficShaping,
		"/api/v1/netsec/vpn":       netsec.CapVPN,
		"/api/v1/netsec/zerotrust": netsec.CapZeroTrust,
	}
	for path, cap := range handlers {
		c := cap
		mux.Handle(path, auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g.handleNetSec(w, r, c)
		})))
	}
}

// handleNetSec 解析 {deviceID, action} → pkg/netsec 取脚本 → TaskDispatcher 下发。
func (g *Gateway) handleNetSec(w http.ResponseWriter, r *http.Request, cap netsec.Capability) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req netsec.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "deviceID is required")
		return
	}

	script, ok := netsec.Build(cap, req.Action)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown action: "+req.Action)
		return
	}

	if g.taskDispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, ErrNoTaskDispatcher.Error())
		return
	}

	taskID, err := g.taskDispatcher.DispatchTask(r.Context(), req.DeviceID, script, netsec.TaskAction(cap, req.Action))
	if err != nil {
		writeError(w, http.StatusBadGateway, "dispatch task failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"taskID": taskID,
		"action": req.Action,
	})
}
