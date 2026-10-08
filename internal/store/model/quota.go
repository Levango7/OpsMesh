// quota.go 租户配额领域数据（TD-61，原 internal/store/store.go 搬迁）。
//
// 放在中性层的原因同 contract.go：QuotaStore 契约与 memory / sql 两个后端的
// GetQuota / SetQuota / CalculateUsage 方法签名都引用这两个类型。
package model

import "time"

// QuotaConfig 租户资源配额配置（多租户资源配额与计费）。
//
// 每个字段表示该租户对应资源允许的最大数量；0 表示不限制（无限配额）。
// 由 QuotaManager 在 CheckDevice/CheckTask/CheckAlert 时读取，
// 与 store 中当前用量比较，超额返回 ErrQuotaExceeded。
//
// 设计要点：
//   - 定义在 store 包而非 controlplane 包，避免 store→controlplane 循环依赖；
//   - 字段均为值类型，浅拷贝即深拷贝，便于并发安全返回拷贝；
//   - JSON 标签用于 API 响应序列化（GET /api/v1/quotas/{tenantID}）。
type QuotaConfig struct {
	MaxDevices int `json:"maxDevices"` // 最大设备数（0=不限）
	MaxTasks   int `json:"maxTasks"`   // 最大任务数（0=不限）
	MaxAlerts  int `json:"maxAlerts"`  // 最大告警数（0=不限）
}

// Usage 租户资源用量统计。
type Usage struct {
	TenantID     string    `json:"tenantID"`
	DeviceCount  int       `json:"deviceCount"`
	TaskCount    int       `json:"taskCount"`
	AlertCount   int       `json:"alertCount"`
	MetricsCount int       `json:"metricsCount"`
	CalculatedAt time.Time `json:"calculatedAt"`
}
