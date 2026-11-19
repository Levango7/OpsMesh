// abac_policy.go ABAC 策略的持久化形态（批次47：ABAC 策略持久化）。
//
// 与控制面 abacPolicy 镜像的关系：store 是**事实源**，进程内 abacPolicies 是评估缓存。
// 持久化字段刻意少于评估形态（不含 condition）：本批只固化策略的匹配骨架
// (subject/resource/action/effect)，与 handleABAC 当前可创建的策略一致。
//
// 版本语义：每次 Upsert 对已存在记录 version+1（不存在则为 1），供多副本 5s 刷新
// 只重建有变化的租户，未变化即跳过；created_at 在更新时保持不变。
package model

import "time"

// ABACPolicyRecord 控制面 ABAC 策略的持久化记录。按 (TenantID, ID) 唯一。
type ABACPolicyRecord struct {
	TenantID  string    `json:"tenantID"`
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Resource  string    `json:"resource"`
	Action    string    `json:"action"`
	Effect    string    `json:"effect"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
