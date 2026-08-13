// Package logstore 实现 M6 日志检索：集中采集 agent/任务/系统日志，
// 支持按租户 / 设备 / 时间 / 关键字检索。双后端（Memory 环形缓冲 / SQL）。
package logstore

import "time"

// Entry 是一条日志。
type Entry struct {
	ID        int64     `json:"id"`
	TenantID  string    `json:"tenantID"`         // 行级隔离
	DeviceID  string    `json:"deviceID"`         // 来源设备（dev-<ip>）
	AgentID   string    `json:"agentID"`          // 来源 agent
	TaskID    string    `json:"taskID,omitempty"` // 关联任务（可选）
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`  // info / warn / error
	Source    string    `json:"source"` // agent / task / system
	Message   string    `json:"message"`
}

// Query 是日志检索条件。
type Query struct {
	TenantID string    // 必填（行级隔离）
	DeviceID string    // 可选
	AgentID  string    // 可选
	Level    string    // 可选（info/warn/error）
	Source   string    // 可选
	Keyword  string    // 可选：message LIKE（向后兼容）
	Q        string    // 可选：结构化查询语法（KQL/Lucene 风格，非空时优先于 Keyword）
	From     time.Time // 可选：起始
	To       time.Time // 可选：结束
	Limit    int       // 可选：默认 200，上限 1000
	Offset   int       // 可选：跳过的命中条数（分页游标，>=0）
}
