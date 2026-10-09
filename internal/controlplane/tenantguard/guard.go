// guard.go 多租户隔离的纯校验逻辑（TD-87 批 2 第二批：自父包 tenant_guard.go 迁入）。
//
// 判据语义（与父包 tenant_guard.go 的 Server 侧包装一致，且与既有正确实现
// server_tasks.go / middleware_deploy_handler.go / os_template_handlers.go / server_network.go 相同）：
// agent 不存在，或其 TenantID 与调用方租户不一致 → 拒绝；调用方租户为空（开放模式）时仅校验 agent 存在。
//
// 为什么单独成包：租户 ID 既流入多租户 schema 名（MultiSchemaStore 的 SchemaNamer）又进入各类
// 按租户过滤的 SQL 参数，是**唯一的写入入口校验点**；把它与 handler 分开，规则只有一处、测试可直接打。
//
// 父包保留三个同名薄包装（tenant_guard.go）⇒ 30 处调用点零改动。
package tenantguard

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// TenantOrDefault 将空租户归一为平台默认租户 "default"。
// 与 store 层 normalizeTenantID 同语义；控制面侧独立实现以免向 store 暴露内部细节。
func TenantOrDefault(tenantID string) string {
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

// ValidateTenantID 校验租户 ID 字面量是否合法（空值不合法，调用方应先归一）。
func ValidateTenantID(tenantID string) error {
	if tenantID == "" {
		return errors.New("tenantId is empty")
	}
	if !tenantIDPattern.MatchString(tenantID) {
		return fmt.Errorf("tenantId 含非法字符（仅允许字母/数字/下划线/点/连字符，最长 64）: %q", tenantID)
	}
	return nil
}

// TenantAgentIn 解析目标 agent 并校验其归属租户（包级实现，供非 *Server 持有者复用，
// 如 automationExecutor 这类无 *Server 引用的执行器）。
// tenantID 为空表示调用方无租户上下文（开放模式），此时仅校验 agent 存在。
func TenantAgentIn(st store.Store, agentID, tenantID string) (*proto.AgentInfo, error) {
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
