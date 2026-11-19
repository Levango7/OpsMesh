// abac_decision.go ABAC 决策审计日志的持久化形态（M3：agent 决策回传）。
//
// 一次 ABAC 评估产出一条决策（allowed + 命中策略 + 原因），agent 在离线期间本地缓冲，
// 恢复连接后批量回传。身份字段（TenantID/AgentID）由控制面在 per-agent HMAC 认证后回填，
// **绝不**取自上报体，避免 agent 自报租户越权写穿。
//
// 两个时间语义刻意分开：
//   - OccurredAt：agent 端实际评估时间，离线补传时可能显著滞后；
//   - ReceivedAt：控制面接收时间，是保留期清理的唯一依据。
//
// 若用 OccurredAt 做保留期，积压一周的离线决策会在入库后的首轮清理里被整批删掉。
package model

import "time"

// ABACDecisionLog 单条 ABAC 决策审计记录。SQL 后端按自增 ID 存储，内存后端按追加序号分配。
type ABACDecisionLog struct {
	ID         int64     `json:"id"`
	TenantID   string    `json:"tenantID"`
	AgentID    string    `json:"agentID"`
	RequestID  string    `json:"requestId"`
	BundleID   string    `json:"bundleId"`
	PolicyID   string    `json:"policyId"`
	Allowed    bool      `json:"allowed"`
	Reason     string    `json:"reason"`
	Mode       string    `json:"mode"`
	OccurredAt time.Time `json:"occurredAt"`
	ReceivedAt time.Time `json:"receivedAt"`
}
