// Package fulltext 提供与业务无关的全文本检索原语：中英文混合分词、倒排索引与 TF-IDF 排序。
//
// 存在意义：internal/logstore（日志检索）与 internal/cmdb（CI 检索）需要的是同一件事——
// 输入一段自然语言、主机名或属性值，找出相关记录。若两个模块各实现一份分词与打分，
// 分词规则（是否按 '_' 切分、中文是否按字切）或 TF-IDF 公式一旦漂移，同一查询在两个
// 模块会得到不同结果，而这种漂移既没有编译期信号也没有测试期信号，只能靠人工比对发现。
// 因此把这部分下沉为唯一实现，两个模块共用。
//
// 本包刻意不感知任何业务类型：文档 ID 用泛型 K 表示（logstore 传 int64 日志序号，
// cmdb 传 string 的 CI ID），文档正文只接受 string。
package fulltext

import (
	"cmp"
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
//
// 规则：
//   - 连续的 ASCII 字母/数字/下划线构成一个词（下划线保留，因为主机名与属性键常用它）；
//   - 每个中日韩统一表意字符（含扩展 A 与兼容区）单独构成一个词；
//   - 标点、空白及其它字符一律作为分隔符丢弃。
//
// 空输入返回 nil（而非空切片），调用方可用 len()==0 统一判断。
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
	tokens []string       // 按顺序的 token 列表（短语查询用）
	tf     map[string]int // term -> 词频
	length int            // token 总数
}

// posting 单个 term 在某文档中的 posting。
type posting[K cmp.Ordered] struct {
	docID     K
	tf        int   // 词频
	positions []int // 在文档中的位置（短语查询用）
}

// postingsList 一个 term 的 postings（按 docID 索引）。
type postingsList[K cmp.Ordered] struct {
	docs map[K]*posting[K]
}

// Index 倒排索引引擎（并发安全）。
//
// K 为文档 ID 类型，需可排序：排序是结果稳定的前提——TF-IDF 同分时必须有一个确定的
// 兜底次序，否则同一查询两次调用可能返回不同顺序，前端列表会无故跳动。
type Index[K cmp.Ordered] struct {
	mu       sync.RWMutex
	docs     map[K]*indexedDoc
	postings map[string]*postingsList[K]
	docCount int

	// sorted 是 postings 键的字典序缓存，供 SearchPrefix 二分定位前缀区间。
	// 只在持有 mu 写锁时读写，因此无需再加一把锁（加第二把锁会与 mu 形成 AB-BA 死锁）。
	sorted      []string
	sortedDirty bool
}

// NewIndex 创建倒排索引。
func NewIndex[K cmp.Ordered]() *Index[K] {
	return &Index[K]{
		docs:     make(map[K]*indexedDoc),
		postings: make(map[string]*postingsList[K]),
	}
}

// Add 加入/更新文档。若 docID 已存在则先移除再加入（保证 tf 与 postings 不重复累加）。
func (idx *Index[K]) Add(docID K, text string) {
	tokens := Tokenize(text)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, ok := idx.docs[docID]; ok {
		idx.removeLocked(docID)
	}
	doc := &indexedDoc{
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
			pl = &postingsList[K]{docs: make(map[K]*posting[K])}
			idx.postings[tok] = pl
			idx.sortedDirty = true // 新增 term → 字典序缓存失效
		}
		pl.docs[docID] = &posting[K]{docID: docID, tf: len(positions), positions: positions}
	}
}

// removeLocked 删除文档（调用方持写锁）。
func (idx *Index[K]) removeLocked(docID K) {
	doc, ok := idx.docs[docID]
	if !ok {
		return
	}
	for tok := range doc.tf {
		if pl := idx.postings[tok]; pl != nil {
			delete(pl.docs, docID)
			if len(pl.docs) == 0 {
				delete(idx.postings, tok)
				idx.sortedDirty = true // 删除 term → 字典序缓存失效
			}
		}
	}
	delete(idx.docs, docID)
	idx.docCount--
}

// Remove 删除文档。
func (idx *Index[K]) Remove(docID K) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeLocked(docID)
}

// Size 返回已索引文档数。
func (idx *Index[K]) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}

// Terms 返回所有索引 term（调试/测试用，按字典序）。
//
// 取写锁而非读锁：缓存可能需要在内部重建，读锁下写缓存字段本身就是数据竞争
// （两个读者同时重建会互相踩）。Terms 只用于调试和测试，串行化没有代价。
func (idx *Index[K]) Terms() []string {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.sortedTermsLocked()
}

// sortedTermsLocked 返回 term 的字典序切片（调用方持锁）。
//
// 缓存脏了才重建；调用方必须持有写锁才能安全重建（读锁下写缓存字段会与其它读者竞争）。
func (idx *Index[K]) sortedTermsLocked() []string {
	if idx.sortedDirty || idx.sorted == nil {
		idx.sorted = make([]string, 0, len(idx.postings))
		for t := range idx.postings {
			idx.sorted = append(idx.sorted, t)
		}
		sort.Strings(idx.sorted)
		idx.sortedDirty = false
	}
	out := make([]string, len(idx.sorted))
	copy(out, idx.sorted)
	return out
}

// tfidfLocked 计算 TF-IDF（调用方持锁）。
// TF = 1 + log10(tf)（tf>1 时）；IDF = log10(N / df)。
func (idx *Index[K]) tfidfLocked(tf, df int) float64 {
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

// scoredDoc 带得分的文档（用于 TF-IDF 排序）。
type scoredDoc[K cmp.Ordered] struct {
	docID K
	score float64
}

// sortScored 按得分降序，同分按 docID 升序。
func sortScored[K cmp.Ordered](hits []scoredDoc[K]) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].docID < hits[j].docID
	})
}

// scoredToIDs 提取 docID 列表。
func scoredToIDs[K cmp.Ordered](hits []scoredDoc[K]) []K {
	out := make([]K, len(hits))
	for i, h := range hits {
		out[i] = h.docID
	}
	return out
}

// ---------------------------------------------------------------------------
// 搜索：单 term
// ---------------------------------------------------------------------------

// Search 单 term 精确搜索，返回按 TF-IDF 降序排序的 docID 列表。
// 同分时按 docID 升序（稳定）。
func (idx *Index[K]) Search(term string) []K {
	term = strings.ToLower(term)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	pl := idx.postings[term]
	if pl == nil || len(pl.docs) == 0 {
		return nil
	}
	hits := make([]scoredDoc[K], 0, len(pl.docs))
	df := len(pl.docs)
	for docID, p := range pl.docs {
		hits = append(hits, scoredDoc[K]{docID: docID, score: idx.tfidfLocked(p.tf, df)})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// SearchPrefix 前缀搜索：返回含任一以 prefix 开头的 term 的 docID 列表（按 TF-IDF 之和排序）。
//
// 前缀语义是 CMDB 检索的关键——用户敲 "web" 时期望命中 "web-01" 与 "webserver"，
// 而后者分词后是一个完整 term "webserver"，精确匹配搜不到。
//
// 实现：持写锁重建/复用 term 字典序缓存后二分定位前缀区间。持写锁（而非读锁）是因为
// 缓存字段需要在锁内写；代价是并发检索串行化，收益是不用引入第二把锁（会与 mu 死锁）。
func (idx *Index[K]) SearchPrefix(prefix string) []K {
	prefix = strings.ToLower(prefix)
	if prefix == "" {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	terms := idx.sortedTermsLocked()
	start := sort.SearchStrings(terms, prefix)
	scores := make(map[K]float64)
	for i := start; i < len(terms); i++ {
		if !strings.HasPrefix(terms[i], prefix) {
			break // 已按字典序，第一个不匹配之后都不匹配
		}
		pl := idx.postings[terms[i]]
		if pl == nil {
			continue
		}
		df := len(pl.docs)
		for docID, p := range pl.docs {
			scores[docID] += idx.tfidfLocked(p.tf, df)
		}
	}
	if len(scores) == 0 {
		return nil
	}
	hits := make([]scoredDoc[K], 0, len(scores))
	for docID, score := range scores {
		hits = append(hits, scoredDoc[K]{docID: docID, score: score})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// ---------------------------------------------------------------------------
// 搜索：短语
// ---------------------------------------------------------------------------

// SearchPhrase 短语查询：返回包含完整短语的 docID 列表（按 TF-IDF 排序）。
// 短语需分词后位置连续出现。
func (idx *Index[K]) SearchPhrase(phrase string) []K {
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
	hits := make([]scoredDoc[K], 0, len(first.docs))
	df := len(first.docs)
	for docID, p0 := range first.docs {
		if !idx.hasPhraseLocked(docID, tokens) {
			continue
		}
		hits = append(hits, scoredDoc[K]{docID: docID, score: idx.tfidfLocked(p0.tf, df)})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// hasPhraseLocked 判断 docID 是否包含完整短语（位置连续，调用方持读锁）。
func (idx *Index[K]) hasPhraseLocked(docID K, tokens []string) bool {
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
func (idx *Index[K]) SearchAnd(terms []string) []K {
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
	hits := make([]scoredDoc[K], 0, len(first.docs))
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
			hits = append(hits, scoredDoc[K]{docID: docID, score: score})
		}
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// SearchOr 布尔 OR：返回包含任一 term 的 docID 列表（按 TF-IDF 之和排序）。
func (idx *Index[K]) SearchOr(terms []string) []K {
	if len(terms) == 0 {
		return nil
	}
	norm := make([]string, len(terms))
	for i, t := range terms {
		norm[i] = strings.ToLower(t)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	scores := make(map[K]float64)
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
	hits := make([]scoredDoc[K], 0, len(scores))
	for docID, score := range scores {
		hits = append(hits, scoredDoc[K]{docID: docID, score: score})
	}
	sortScored(hits)
	return scoredToIDs(hits)
}

// SearchNot 布尔 NOT：返回不包含 term 的所有 docID 列表（按 docID 升序）。
func (idx *Index[K]) SearchNot(term string) []K {
	term = strings.ToLower(term)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	excluded := idx.postings[term]
	out := make([]K, 0, idx.docCount)
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
func (idx *Index[K]) SearchWildcard(pattern string) []K {
	pattern = strings.ToLower(pattern)
	if pattern == "" {
		return nil
	}
	if !strings.ContainsAny(pattern, "*?") {
		return idx.Search(pattern)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	hits := make([]scoredDoc[K], 0)
	for term, pl := range idx.postings {
		if !matchWildcard(pattern, term) {
			continue
		}
		df := len(pl.docs)
		for docID, p := range pl.docs {
			hits = append(hits, scoredDoc[K]{docID: docID, score: idx.tfidfLocked(p.tf, df)})
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
