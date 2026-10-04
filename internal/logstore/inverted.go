// Package logstore: inverted.go 实现倒排索引引擎，支持全文本检索。
//
// 特性：
//   - 中英文混合分词（中文按字，英文按词），转小写
//   - 倒排索引：Add/Remove/Search/SearchPrefix
//   - 短语查询、布尔(AND/OR/NOT)、通配符、TF-IDF 排序
//   - 并发安全（sync.RWMutex）
//
// 设计说明：
//   - 倒排索引作为 MemoryLogStore 的可选加速层，通过 NewMemoryWithIndex 启用
//   - Append 同步加入索引；环形裁剪同步移除旧文档
//   - 全文本检索通过 SearchFullText 方法；Query 保持原有逻辑（向后兼容）
//
// 实现下沉：分词与倒排索引的具体实现已移到 internal/fulltext——它不是日志专有能力，
// internal/cmdb 的 CI 检索复用同一份实现。本文件只保留类型别名与构造/分词转发，
// 使 logstore 对外 API（InvertedIndex / NewInvertedIndex / Tokenize）保持不变。
package logstore

import (
	"errors"

	"github.com/Levango7/OpsMesh/internal/fulltext"
)

// InvertedIndex 倒排索引引擎，文档 ID 为 int64（日志序列号）。
type InvertedIndex = fulltext.Index[int64]

// NewInvertedIndex 创建倒排索引。
func NewInvertedIndex() *InvertedIndex {
	return fulltext.NewIndex[int64]()
}

// Tokenize 中英文混合分词：中文按字，英文按词，转小写。
// 转发到 internal/fulltext，保证与 CMDB 检索的分词规则完全一致。
func Tokenize(text string) []string {
	return fulltext.Tokenize(text)
}

// ---------------------------------------------------------------------------
// 全文本检索查询条件与错误
// ---------------------------------------------------------------------------

// FullTextQuery 全文本检索条件。
// 六种搜索模式互斥，按优先级依次判断：Phrase > And > Or > Not > Wildcard > Term。
// Base 提供基础过滤（TenantID/DeviceID/AgentID/Level/Source/From/To），
// Base.Keyword 与 Base.Q 在搜索时被忽略（文本检索由本结构字段驱动）。
type FullTextQuery struct {
	Base     Query    // 基础过滤条件
	Term     string   // 单 term 搜索
	Phrase   string   // 短语查询
	And      []string // 布尔 AND：同时包含所有 term
	Or       []string // 布尔 OR：包含任一 term
	Not      string   // 布尔 NOT：不包含此 term
	Wildcard string   // 通配符查询（* 任意序列，? 单字符）
	Limit    int
}

// ErrIndexDisabled 倒排索引未启用错误。
var ErrIndexDisabled = errors.New("倒排索引未启用：请使用 NewMemoryWithIndex 构造")
