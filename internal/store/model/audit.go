// audit.go 审计链校验结果（TD-61，原 internal/store/sql_audit_chain.go 搬迁）。
//
// 定义方是 SQL 后端（链式校验），但消费面横跨：AuditStore 契约、MemoryStore 的
// VerifyAuditChain（返回 Supported=false 的桩）与 multi_schema 聚合层都引用它，
// 故上提中性层——否则 memory 子包要引用 sql 侧定义（跨后端反向依赖）。
package model

// AuditChainVerifyResult 审计链校验结果（对外 API 直接序列化）。
type AuditChainVerifyResult struct {
	// Supported 该存储后端是否支持链式校验（SQL 后端且已应用迁移 019 时为 true）。
	Supported bool `json:"supported"`
	// Scope 校验范围：platform=平台级（全链，逐行链接严格校验）；
	// tenant=租户视图（只读本租户行，行自洽 + 边界链接，逐行链接受跨租户交错限制）。
	// 两者结论强度不同，消费方（告警/合规报告）应据此判断。
	Scope string `json:"scope,omitempty"`
	// OK 为 true 表示窗口内所有行自洽、且窗口首行与前驱（在线前一行/归档边界/创世）链接一致。
	OK bool `json:"ok"`
	// Checked 本次实际校验的行数。
	Checked int `json:"checked"`
	// FromID / ToID 本次校验覆盖的行号区间（闭区间）。
	FromID int64 `json:"fromID"`
	ToID   int64 `json:"toID"`
	// FirstBadID / Reason 首个不一致的行号与原因（OK=true 时为空）。
	FirstBadID int64  `json:"firstBadID,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// ChainHeadID / ChainHeadHash 链头记录（最新一次成功追加）。
	ChainHeadID   int64  `json:"chainHeadID"`
	ChainHeadHash string `json:"chainHeadHash,omitempty"`
	// HeadConsistent 链头与在线最新行的 entry_hash 是否一致（false 说明尾部被删/链头过期）。
	HeadConsistent bool `json:"headConsistent"`
	// TailCovered 本次窗口是否覆盖到链尾（租户视图下窗口可能不覆盖链尾，此时 HeadConsistent 不做判定）。
	TailCovered bool `json:"tailCovered"`
	// ArchivedThroughID / ArchivedBoundaryHash 归档边界（0/空表示尚无归档）。
	ArchivedThroughID    int64  `json:"archivedThroughID,omitempty"`
	ArchivedBoundaryHash string `json:"archivedBoundaryHash,omitempty"`
	// LegacyRows 未纳入链的历史行数（本迁移上线前写入的行，entry_hash 为空）。
	LegacyRows int64 `json:"legacyRows"`
	// Note 人类可读的补充说明（如窗口截断、无归档、存在链前遗留行）。
	Note string `json:"note,omitempty"`
}
