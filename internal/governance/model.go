// Package governance 提供数据治理领域模型、内存存储与纯函数评估引擎。
//
// 覆盖数据质量规则、PII 掩码策略、数据保留策略与数据目录/血缘。
package governance

import "time"

// 规则类型。
const (
	RuleKindNull      = "null"
	RuleKindUnique    = "unique"
	RuleKindRange     = "range"
	RuleKindRegex     = "regex"
	RuleKindFreshness = "freshness"
	RuleKindRowCount  = "rowcount"
)

// 严重级别。
const (
	SeverityInfo     = "info"
	SeverityWarn     = "warn"
	SeverityCritical = "critical"
)

// PII 类型。
const (
	PIIKindEmail  = "email"
	PIIKindPhone  = "phone"
	PIIKindIDCard = "idcard"
	PIIKindName   = "name"
	PIIKindCustom = "custom"
)

// 保留动作。
const (
	RetentionActionDelete    = "delete"
	RetentionActionArchive   = "archive"
	RetentionActionAnonymize = "anonymize"
)

// 资产类型。
const (
	AssetKindTable  = "table"
	AssetKindTopic  = "topic"
	AssetKindPath   = "path"
	AssetKindStream = "stream"
)

// PII 等级。
const (
	PIILevelNone = "none"
	PIILevelLow  = "low"
	PIILevelHigh = "high"
)

// DataRule 单列/行级数据质量规则。Expr 编码各类型的参数：
//   - null: "maxNulls=N"（默认 0）
//   - unique: 无参数
//   - range: "min=0,max=100" 或 "0,100"
//   - regex: 正则表达式
//   - freshness: "maxAgeHours=24" 或 "24"
//   - rowcount: "min=1,max=1000"
type DataRule struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Dataset   string    `json:"dataset"`
	Column    string    `json:"column,omitempty"`
	Kind      string    `json:"kind"`
	Expr      string    `json:"expr,omitempty"`
	Severity  string    `json:"severity"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RuleResult 一次规则评估结果。
type RuleResult struct {
	ID       string    `json:"id"`
	RuleID   string    `json:"ruleId"`
	TenantID string    `json:"tenantId"`
	Passed   bool      `json:"passed"`
	Observed string    `json:"observed"`
	Detail   string    `json:"detail,omitempty"`
	RunAt    time.Time `json:"runAt"`
}

// PiiPolicy 列级 PII 脱敏策略。
type PiiPolicy struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Dataset   string    `json:"dataset"`
	Column    string    `json:"column"`
	Kind      string    `json:"kind"`
	Mask      string    `json:"mask,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RetentionPolicy 数据集保留策略。
type RetentionPolicy struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenantId"`
	Dataset       string    `json:"dataset"`
	RetentionDays int       `json:"retentionDays"`
	Action        string    `json:"action"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// CatalogAsset 数据目录资产。
type CatalogAsset struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	PIILevel  string    `json:"piiLevel"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GovernanceStore 数据治理存储。所有方法按租户隔离；
// ListRuleResults 的 tenantID 为空表示平台读取（跨租户汇总）。
type GovernanceStore interface {
	CreateRule(rule DataRule) (DataRule, error)
	UpdateRule(rule DataRule) (DataRule, error)
	DeleteRule(tenantID, id string) error
	GetRule(tenantID, id string) (DataRule, error)
	ListRules(tenantID string) []DataRule
	AppendRuleResult(result RuleResult) RuleResult
	ListRuleResults(tenantID, ruleID string) []RuleResult

	CreatePIIPolicy(policy PiiPolicy) (PiiPolicy, error)
	UpdatePIIPolicy(policy PiiPolicy) (PiiPolicy, error)
	DeletePIIPolicy(tenantID, id string) error
	GetPIIPolicy(tenantID, id string) (PiiPolicy, error)
	ListPIIPolicies(tenantID string) []PiiPolicy

	CreateRetentionPolicy(policy RetentionPolicy) (RetentionPolicy, error)
	UpdateRetentionPolicy(policy RetentionPolicy) (RetentionPolicy, error)
	DeleteRetentionPolicy(tenantID, id string) error
	GetRetentionPolicy(tenantID, id string) (RetentionPolicy, error)
	ListRetentionPolicies(tenantID string) []RetentionPolicy

	CreateCatalogAsset(asset CatalogAsset) (CatalogAsset, error)
	UpdateCatalogAsset(asset CatalogAsset) (CatalogAsset, error)
	DeleteCatalogAsset(tenantID, id string) error
	GetCatalogAsset(tenantID, id string) (CatalogAsset, error)
	ListCatalogAssets(tenantID string) []CatalogAsset
}
