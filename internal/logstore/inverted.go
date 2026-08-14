// Package logstore: inverted.go 实现倒排索引引擎，支持全文本检索。
//
// 特性：
//   - 中英文混合分词（中文按字，英文按词），转小写
//   - 倒排索引：Add/Remove/Search
//   - 短语查询、布尔(AND/OR/NOT)、通配符、TF-IDF 排序
//   - 并发安全（sync.RWMutex）
//
// 设计说明：
//   - 倒排索引作为 MemoryLogStore 的可选加速层，通过 NewMemoryWithIndex 启用
//   - Append 同步加入索引；环形裁剪同步移除旧文档
//   - 全文本检索通过 SearchFullText 方法；Query 保持原有逻辑（向后兼容）
package logstore

import (
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// ---------------------------------------------------------------------------
// 分词器
// ---------------------------------------------------------------------------

// Tokenize 中英文混合分词：中文按字，英文按词，转小写。
// 连续的英文字母/数字/下划线作为一个词；每个中文字符作为一个词。
// 标点和空白作为分隔符（丢弃）。
func Tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for _, r := range text {
		switch {
		case isASCIILetter(r) || isASCIIDigit(r) || r == '_':
			b.WriteRune(unicode.ToLower(r))
		case isCJK(r):
			flush()
			tokens = append(tokens, string(unicode.ToLower(r)))
		default:
			flush()
		}
	}
	flush()
	return tokens
}

// isASCIILetter 判断 ASCII 字母。
func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isASCIIDigit 判断 ASCII 数字。
func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// isCJK 判断中日韩统一表意字符（含扩展 A 与兼容表意）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一
		(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
		(r >= 0xF900 && r <= 0xFAFF) // CJK 兼容表意
}

// ---------------------------------------------------------------------------
// 倒排索引数据结构
// ---------------------------------------------------------------------------

// indexedDoc 已索引文档的元数据。
type indexedDoc struct {
	id     int64
	tokens []string       // 按顺序的 token 列表（短语查询用）
	tf     map[string]int // term -> 词频
	length int            // token 总数
}

// posting 单个 term 在某文档中的 posting。
type posting struct {
	docID     int64
	tf        int   // 词频
	positions []int // 在文档中的位置（短语查询用）
}

// postingsList 一个 term 的 postings（按 docID 索引）。
type postingsList struct {
	docs map[int64]*posting
}

// InvertedIndex 倒排索引引擎（并发安全）。
type InvertedIndex struct {
	mu       sync.RWMutex
	docs     map[int64]*indexedDoc
	postings map[string]*postingsList
	docCount int
}

// NewInvertedIndex 创建倒排索引。
func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		docs:     make(map[int64]*indexedDoc),
		postings: make(map[string]*postingsList),
	}
}

// Add 加入/更新文档。若 docID 已存在则先移除再加入。
func (idx *InvertedIndex) Add(docID int64, text string) {
	tokens := Tokenize(text)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, ok := idx.docs[docID]; ok {
		idx.removeLocked(docID)
	}
	doc := &indexedDoc{
		id:     docID,
		tokens: tokens,
		tf:     make(map[string]int, len(tokens)),
		length: len(tokens),
	}
	termPos := make(map[string][]int, len(tokens))
	for i, tok := range tokens {
		doc.tf[tok]++
		termPos[tok] = append(termPos[tok], i)
	}
	idx.docs[docID] = doc
	idx.docCount++
	for tok, positions := range termPos {
		pl := idx.postings[tok]
		if pl == nil {
			pl = &postingsList{docs: make(map[int64]*posting)}
			idx.postings[tok] = pl
		}
		pl.docs[docID] = &posting{docID: docID, tf: len(positions), positions: positions}
	}
}

// removeLocked 删除文档（调用方持写锁）。
func (idx *InvertedIndex) removeLocked(docID int64) {
	doc, ok := idx.docs[docID]
	if !ok {
		return
	}
	for tok := range doc.tf {
		if pl := idx.postings[tok]; pl != nil {
			delete(pl.docs, docID)
			if len(pl.docs) == 0 {
				delete(idx.postings, tok)
			}
		}
	}
	delete(idx.docs, docID)
	idx.docCount--
}

// Remove 删除文档。
func (idx *InvertedIndex) Remove(docID int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeLocked(docID)
}

// Size 返回已索引文档数。
func (idx *InvertedIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}

// Terms 返回所有索引 term（调试/测试用，按字典序）。
func (idx *InvertedIndex) Terms() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]string, 0, len(idx.postings))
	for t := range idx.postings {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// tfidfLocked 计算 TF-IDF（调用方持读锁）。
// TF = 1 + log10(tf)（tf>1 时）；IDF = log10(N / df)。
func (idx *InvertedIndex) tfidfLocked(tf, df int) float64 {
	if idx.docCount == 0 || df == 0 {
		return 0
	}
	tfVal := 1.0
	if tf > 1 {
		tfVal = 1.0 + math.Log10(float64(tf))
	}
	idfVal := math.Log10(float64(idx.docCount) / float64(df))
	return tfVal * idfVal
}

// ---------------------------------------------------------------------------
// 搜索：单 term
// ---------------------------------------------------------------------------

// Search 单 term 搜索，返回按 TF-IDF 降序排序的 docID 列表。
// 同分时按 docID 升序（稳定）。
func (idx *InvertedIndex) Search(term string) []int64 {
	term = strings.ToLower(term)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	pl := idx.postings[term]
	if pl == nil || len(pl.docs) == 0 {
		return nil
	}
	hits := make([]scoredDoc, 0, len(pl.docs))
	df := len(pl.docs)
	for docID, p := range pl.docs {
		hits = append(hits, scoredDoc{docID: docID, score: idx.tfidfLocked(p.tf, df)})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// ---------------------------------------------------------------------------
// 搜索：短语
// ---------------------------------------------------------------------------

// SearchPhrase 短语查询：返回包含完整短语的 docID 列表（按 TF-IDF 排序）。
// 短语需分词后位置连续出现。
func (idx *InvertedIndex) SearchPhrase(phrase string) []int64 {
	tokens := Tokenize(phrase)
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) == 1 {
		return idx.Search(tokens[0])
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	first := idx.postings[tokens[0]]
	if first == nil {
		return nil
	}
	hits := make([]scoredDoc, 0, len(first.docs))
	df := len(first.docs)
	for docID, p0 := range first.docs {
		if !idx.hasPhraseLocked(docID, tokens) {
			continue
		}
		hits = append(hits, scoredDoc{docID: docID, score: idx.tfidfLocked(p0.tf, df)})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// hasPhraseLocked 判断 docID 是否包含完整短语（位置连续，调用方持读锁）。
func (idx *InvertedIndex) hasPhraseLocked(docID int64, tokens []string) bool {
	positions := make([][]int, len(tokens))
	for i, tok := range tokens {
		pl := idx.postings[tok]
		if pl == nil {
			return false
		}
		p := pl.docs[docID]
		if p == nil {
			return false
		}
		positions[i] = p.positions
	}
	for _, start := range positions[0] {
		ok := true
		for i := 1; i < len(tokens); i++ {
			if !containsInt(positions[i], start+i) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// containsInt 判断切片是否包含某值。
func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 搜索：布尔 AND / OR / NOT
// ---------------------------------------------------------------------------

// SearchAnd 布尔 AND：返回同时包含所有 terms 的 docID 列表（按 TF-IDF 之和排序）。
func (idx *InvertedIndex) SearchAnd(terms []string) []int64 {
	if len(terms) == 0 {
		return nil
	}
	norm := make([]string, len(terms))
	for i, t := range terms {
		norm[i] = strings.ToLower(t)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	first := idx.postings[norm[0]]
	if first == nil {
		return nil
	}
	hits := make([]scoredDoc, 0, len(first.docs))
	for docID := range first.docs {
		var score float64
		ok := true
		for _, tok := range norm {
			pl := idx.postings[tok]
			if pl == nil {
				ok = false
				break
			}
			p := pl.docs[docID]
			if p == nil {
				ok = false
				break
			}
			score += idx.tfidfLocked(p.tf, len(pl.docs))
		}
		if ok {
			hits = append(hits, scoredDoc{docID: docID, score: score})
		}
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// SearchOr 布尔 OR：返回包含任一 term 的 docID 列表（按 TF-IDF 之和排序）。
func (idx *InvertedIndex) SearchOr(terms []string) []int64 {
	if len(terms) == 0 {
		return nil
	}
	norm := make([]string, len(terms))
	for i, t := range terms {
		norm[i] = strings.ToLower(t)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	scores := make(map[int64]float64)
	for _, tok := range norm {
		pl := idx.postings[tok]
		if pl == nil {
			continue
		}
		df := len(pl.docs)
		for docID, p := range pl.docs {
			scores[docID] += idx.tfidfLocked(p.tf, df)
		}
	}
	hits := make([]scoredDoc, 0, len(scores))
	for docID, score := range scores {
		hits = append(hits, scoredDoc{docID: docID, score: score})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// SearchNot 布尔 NOT：返回不包含 term 的所有 docID 列表（按 docID 升序）。
func (idx *InvertedIndex) SearchNot(term string) []int64 {
	term = strings.ToLower(term)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	excluded := idx.postings[term]
	out := make([]int64, 0, idx.docCount)
	for docID := range idx.docs {
		if excluded == nil || excluded.docs[docID] == nil {
			out = append(out, docID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ---------------------------------------------------------------------------
// 搜索：通配符
// ---------------------------------------------------------------------------

// SearchWildcard 通配符查询：* 匹配任意序列，? 匹配单字符。
// 返回匹配的 docID 列表（按 TF-IDF 排序）。
func (idx *InvertedIndex) SearchWildcard(pattern string) []int64 {
	pattern = strings.ToLower(pattern)
	if pattern == "" {
		return nil
	}
	if !strings.ContainsAny(pattern, "*?") {
		return idx.Search(pattern)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	hits := make([]scoredDoc, 0)
	for term, pl := range idx.postings {
		if !matchWildcard(pattern, term) {
			continue
		}
		df := len(pl.docs)
		for docID, p := range pl.docs {
			hits = append(hits, scoredDoc{docID: docID, score: idx.tfidfLocked(p.tf, df)})
		}
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// matchWildcard 判断 s 是否匹配通配符 pattern（* 任意序列，? 单字符）。
func matchWildcard(pattern, s string) bool {
	return wildcardMatch([]rune(pattern), 0, []rune(s), 0)
}

// wildcardMatch 递归通配符匹配。
func wildcardMatch(p []rune, pi int, t []rune, ti int) bool {
	for pi < len(p) {
		switch p[pi] {
		case '*':
			// 跳过连续 *。
			for pi < len(p) && p[pi] == '*' {
				pi++
			}
			if pi == len(p) {
				return true
			}
			for ti <= len(t) {
				if wildcardMatch(p, pi, t, ti) {
					return true
				}
				ti++
			}
			return false
		case '?':
			if ti >= len(t) {
				return false
			}
			pi++
			ti++
		default:
			if ti >= len(t) || p[pi] != t[ti] {
				return false
			}
			pi++
			ti++
		}
	}
	return ti == len(t)
}

// ---------------------------------------------------------------------------
// 排序辅助
// ---------------------------------------------------------------------------

// scoredDoc 带得分的文档（用于 TF-IDF 排序）。
type scoredDoc struct {
	docID int64
	score float64
}

// sortScored 按得分降序，同分按 docID 升序。
func sortScored(hits []scoredDoc) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].docID < hits[j].docID
	})
}

// scoredToIDs 提取 docID 列表。
func scoredToIDs(hits []scoredDoc) []int64 {
	out := make([]int64, len(hits))
	for i, h := range hits {
		out[i] = h.docID
	}
	return out
}

// ---------------------------------------------------------------------------
// 全文本检索查询条件与错误
// ---------------------------------------------------------------------------

// FullTextQuery 全文本检索条件。
// 六种搜索模式互斥，按优先级依次判断：Phrase > And > Or > Not > Wildcard > Term。
// Base 提供基础过滤（TenantID/DeviceID/AgentID/Level/Source/From/To），
// Base.Keyword 与 Base.Q 在搜索时被忽略（文本检索由本结构字段驱动）。
type FullTextQuery struct {
	Base     Query   // 基础过滤条件
	Term     string  // 单 term 搜索
	Phrase   string  // 短语查询
	And      []string // 布尔 AND：同时包含所有 term
	Or       []string // 布尔 OR：包含任一 term
	Not      string  // 布尔 NOT：不包含此 term
	Wildcard string  // 通配符查询（* 任意序列，? 单字符）
	Limit    int
}

// ErrIndexDisabled 倒排索引未启用错误。
var ErrIndexDisabled = errors.New("倒排索引未启用：请使用 NewMemoryWithIndex 构造")