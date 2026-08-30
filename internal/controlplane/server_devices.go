// server_devices.go 设备相关 HTTP handler。
//
// 从 server.go 拆分而来（按路由域拆分巨型 server.go）。
// 包含设备列表/详情/退役/纳管等端点，逻辑未做任何修改。
package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/domain"
	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
	"opsmesh/internal/provision"
)

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 联邦入站验签：带转发标记的请求必须验签，防跨不可信网段伪造租户身份。
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	snap := s.store.Snapshot(actx.TenantID)
	// 修复 3：分页（向后兼容：不传 page 返回全量 map）。
	page, pageSize := paginate.ParsePagination(r.URL.Query())
	if page == 0 {
		paginate.WriteJSON(w, http.StatusOK, snap)
		return
	}
	// 展平所有 segment 的设备后分页。
	var allDevs []proto.DeviceInfo
	for _, devs := range snap {
		allDevs = append(allDevs, devs...)
	}
	total := len(allDevs)
	start := (page - 1) * pageSize
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paginate.WriteJSON(w, http.StatusOK, paginate.PaginateResult{
		Data: allDevs[start:end], Total: total, Page: page, PageSize: pageSize, HasMore: end < total,
	})
}

// handleAgents 处理 GET /api/v1/agents，按网关注入租户返回已注册 agent 列表（供前端下拉框）。
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	// G1 鉴权修复：agent 列表需 device:read 权限（原无 RBAC，任意匿名可枚举全部 agent）。
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	out := make([]map[string]string, 0)
	for _, a := range s.store.Agents(actx.TenantID) {
		out = append(out, map[string]string{
			"agentID":  a.AgentID,
			"hostname": a.Hostname,
			"segment":  a.Segment,
			"status":   a.Status,
		})
	}
	// 修复 3：分页（向后兼容：不传 page 返回全量）。
	page, pageSize := paginate.ParsePagination(r.URL.Query())
	if page == 0 {
		paginate.WriteJSON(w, http.StatusOK, out)
		return
	}
	total := len(out)
	start := (page - 1) * pageSize
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paginate.WriteJSON(w, http.StatusOK, paginate.PaginateResult{
		Data: out[start:end], Total: total, Page: page, PageSize: pageSize, HasMore: end < total,
	})
}

// handleMe 返回网关注入的当前身份上下文（供 B/S 仪表盘渲染身份、租户、角色）。
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// G1 鉴权修复：携带了用户身份 token（Bearer JWT / HttpOnly Cookie）但无效/过期 → 401，
	// 不允许无效 token 静默回落到网关注入身份造成身份混淆。
	// 未携带 token（纯网关注入头）或 API Key（Bearer om_*）不在此列，继续走身份头解析。
	if s.hasUserIdentityToken(r) {
		if _, err := s.userFromToken(r); err != nil {
			paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"tenantID": actx.TenantID,
		"userID":   actx.UserID,
		"roles":    actx.Roles,
		"mode":     "gateway-injected", // 内核不自鉴权，身份由前置网关注入
	})
}

// handleDeviceDetail 处理 GET /api/v1/devices/{id}：返回设备详情 + 其任务与最近执行结果（租户隔离）。
func (s *Server) handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "device id required")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	dev := s.store.Device(id)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	type deviceDetail struct {
		Device  *domain.Device       `json:"device"`
		Tasks   []*domain.Task       `json:"tasks"`
		Results []*domain.TaskResult `json:"results"`
	}
	dd := deviceDetail{Device: domain.DeviceFromProto(dev)}
	for _, t := range s.store.AllTasks(actx.TenantID) {
		if t.AgentID == dev.AgentID {
			dd.Tasks = append(dd.Tasks, domain.TaskFromProto(t))
		}
	}
	for _, res := range s.store.Results(dev.AgentID) {
		dd.Results = append(dd.Results, domain.TaskResultFromProto(res))
	}
	paginate.WriteJSON(w, http.StatusOK, dd)
}

// lookupAgent 按 agentID 直接查（O(1) 直查，修复线性扫描）。
func (s *Server) lookupAgent(id string) *proto.AgentInfo {
	return s.store.Agent(id)
}

// handleDeviceRouting 统一分派 /api/v1/devices/{id}... 子路径：
//   - GET    /api/v1/devices/{id}：设备详情（设备+任务+结果）
//   - DELETE /api/v1/devices/{id}：退役/下线设备（F5）
//   - POST   /api/v1/devices/{id}/provision：触发自动纳管推送
//   - GET    /api/v1/devices/{id}/metrics：返回设备最新监控指标
func (s *Server) handleDeviceRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "device id required")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleDeviceDetail(w, r)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.handleRetireDevice(w, r, id)
	case len(parts) == 2 && parts[1] == "provision" && r.Method == http.MethodPost:
		s.handleProvision(w, r, id)
	case len(parts) == 2 && parts[1] == "metrics" && r.Method == http.MethodGet:
		s.handleDeviceMetrics(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// handleRetireDevice 处理 DELETE /api/v1/devices/{id}：退役/下线设备（F5）。
// 标记 retired，退出活跃清单但仍可查归档；租户隔离。
func (s *Server) handleRetireDevice(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:delete"); !ok {
		return
	}
	tenant := actx.TenantID
	if !s.store.RetireDevice(id, tenant) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found or tenant mismatch"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "retire_device", Target: id, Detail: "retired via HTTP",
	})

	// SSE：通知前端设备已下线（设备表移除/置灰）
	// 租户隔离：携带 tenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "device_offline", tenant, map[string]string{
		"deviceID": id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "retired", "deviceID": id})
}

// handleProvision 处理 POST /api/v1/devices/{id}/provision：触发自动纳管。
// 签发一次性 install token + 构造可直接复制粘贴的 bootstrap curl|sh 命令，
// 经此命令在候选设备上安装 agent 后，agent 携带 token 回注册完成闭环。
func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "provision:execute"); !ok {
		return
	}
	tenant := actx.TenantID
	dev := s.store.Device(id)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if tenant != "" && dev.TenantID != tenant {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	token, _, err := s.store.Provision(id, dev.IP, tenant)
	if err != nil {
		// TOCTOU 窗口补偿：store 层可能返回"device not found"（设备在本 handler 前置校验
		// 与 Provision 之间被删除）。安全：映射为 404 而非 500。
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		} else {
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": errMsg})
		}
		return
	}
	// 安全：bootstrap 地址用运维显式配置的 advertise-addr，绝不能用请求方可控的 r.Host
	// （Host 头注入可让 bootstrap 指向攻击者服务器→供应链 RCE）。空则回退本机（仅开发）。
	advertise := strings.TrimRight(s.cfg.AdvertiseAddr, "/")
	if advertise == "" {
		advertise = fmt.Sprintf("http://127.0.0.1:%d", s.httpPort)
		logx.Warn(r.Context(), "advertise-addr 未配置，bootstrap 回退本机地址（仅开发，生产务必配置 --advertise-addr）")
	}
	bootstrap := fmt.Sprintf("curl -sSL %s/install.sh | sh -s -- --token=%s", advertise, token)
	// B1 SSH 自动推送：若配置了 SSH 私钥，自动通过 SSH 在候选设备上执行 bootstrap。
	if s.cfg.ProvisionSSHKey != "" {
		sshAddr := fmt.Sprintf("%s:22", dev.IP)
		go func(addr, cmd, device string) {
			// 5 分钟超时，防 SSH 推送阻塞导致 goroutine 泄漏。
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			logx.Info(ctx, "SSH 自动推送 agent", "device", device, "sshAddr", addr)
			out, err := provision.PushAndExec(ctx, addr, s.cfg.ProvisionSSHUser, s.cfg.ProvisionSSHKey, s.cfg.ProvisionSSHKP, s.cfg.ProvisionSSHKnownHosts, cmd)
			if err != nil {
				logx.Error(ctx, "SSH 推送失败", err, "device", device, "sshAddr", addr, "output", out)
			} else {
				logx.Info(ctx, "SSH 推送成功", "device", device, "output", out)
			}
		}(sshAddr, bootstrap, id)
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "provision_agent", Target: id, Detail: "token issued via HTTP",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{
		"status":       "provisioning",
		"deviceID":     id,
		"installToken": token,
		"bootstrap":    bootstrap,
	})
}
