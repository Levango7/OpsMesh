package cmdb

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/fulltext"
)

// MemoryCiStore 内存实现 CMDB 存储（单机 MVP）。
type MemoryCiStore struct {
	mu        sync.RWMutex
	types     map[string]CiType    // name -> type
	items     map[string]CiItem    // id -> item
	rels      map[int64]CiRelation // id -> relation
	relSeq    int64
	templates map[int]CiAttrTemplate // id -> template
	tmplSeq   int
	seq       int

	// idx 是 CI 全文检索的倒排索引，键为 CI ID。
	// 只做候选召回（精确判定由 matchCI 负责），因此它与 items 短暂不一致时
	// 最坏结果是"漏召回"——SearchCIs 会在发现条目数对不上时全量重建来兜底。
	// 它有自己的锁，不与 s.mu 嵌套持有（避免与写入路径形成 AB-BA）。
	idx *fulltext.Index[string]
}

// NewMemoryCiStore 构造内存 CMDB 存储并初始化内置 CI 类型。
func NewMemoryCiStore() *MemoryCiStore {
	s := &MemoryCiStore{
		types:     make(map[string]CiType),
		items:     make(map[string]CiItem),
		rels:      make(map[int64]CiRelation),
		templates: make(map[int]CiAttrTemplate),
		idx:       fulltext.NewIndex[string](),
	}
	now := time.Now()
	for _, t := range []struct{ name, display string }{
		{"machine", "物理机"},
		{"os", "操作系统"},
		{"service", "系统服务"},
		{"app", "应用"},
		{"cluster", "集群"},
	} {
		s.types[t.name] = CiType{
			ID: len(s.types) + 1, Name: t.name, DisplayName: t.display,
			Builtin: true, CreatedAt: now,
		}
	}
	return s
}

func (s *MemoryCiStore) CiTypes(_ context.Context, tenantID string) ([]CiType, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CiType, 0, len(s.types))
	for _, t := range s.types {
		out = append(out, t)
	}
	return out, nil
}

// CreateCiType 创建自定义（非内置）CI 类型。
func (s *MemoryCiStore) CreateCiType(_ context.Context, t *CiType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Name == "" {
		return fmt.Errorf("ci type name required")
	}
	if _, ok := s.types[t.Name]; ok {
		return fmt.Errorf("ci type %s already exists", t.Name)
	}
	s.seq++
	t.ID = s.seq
	t.Builtin = false
	if t.DisplayName == "" {
		t.DisplayName = t.Name
	}
	t.CreatedAt = time.Now()
	s.types[t.Name] = *t
	return nil
}

func (s *MemoryCiStore) GetCIs(_ context.Context, ciType, status, tenantID string) ([]CiItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CiItem, 0)
	for _, item := range s.items {
		if ciType != "" && item.CiType != ciType {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		if tenantID != "" && item.TenantID != tenantID {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemoryCiStore) GetCI(_ context.Context, id, tenantID string) (*CiItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("CI %s not found", id)
	}
	if tenantID != "" && item.TenantID != tenantID {
		return nil, fmt.Errorf("CI %s not found", id)
	}
	return &item, nil
}

func (s *MemoryCiStore) CreateCI(_ context.Context, ci *CiItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.types[ci.CiType]; !ok {
		return fmt.Errorf("unknown CI type: %s", ci.CiType)
	}
	ci.Version = 1
	if ci.Attrs == nil {
		ci.Attrs = make(map[string]string)
	}
	if ci.ApprovalStatus == "" {
		ci.ApprovalStatus = ApprovalApproved
	}
	s.items[ci.ID] = *ci
	s.indexCILocked(ci)
	return nil
}

// indexCILocked 同步一条 CI 到检索索引（调用方持 s.mu 写锁）。
// 索引为 nil 时（例如测试里直接组装 store 而没走构造函数）跳过，
// 由 SearchCIs 发现条目数不一致后全量重建。
func (s *MemoryCiStore) indexCILocked(ci *CiItem) {
	if s.idx == nil {
		return
	}
	s.idx.Add(ci.ID, ciSearchText(ci))
}
func (s *MemoryCiStore) GetCIsByApproval(_ context.Context, approvalStatus, tenantID string) ([]CiItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CiItem, 0)
	for _, item := range s.items {
		if approvalStatus != "" && item.ApprovalStatus != approvalStatus {
			continue
		}
		if tenantID != "" && item.TenantID != tenantID {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// SetApproval 设置单个 CI 的审批状态。
func (s *MemoryCiStore) SetApproval(_ context.Context, id, tenantID, approvalStatus string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return fmt.Errorf("CI %s not found", id)
	}
	if tenantID != "" && item.TenantID != tenantID {
		return fmt.Errorf("CI %s not found", id)
	}
	item.ApprovalStatus = approvalStatus
	item.UpdatedAt = time.Now()
	s.items[id] = item
	return nil
}

func (s *MemoryCiStore) UpdateCI(_ context.Context, ci *CiItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.items[ci.ID]
	if !ok {
		return fmt.Errorf("CI %s not found", ci.ID)
	}
	if ci.TenantID != "" && existing.TenantID != ci.TenantID {
		return fmt.Errorf("CI %s not found", ci.ID)
	}
	ci.Version = existing.Version + 1
	ci.CreatedAt = existing.CreatedAt
	ci.UpdatedAt = time.Now()
	if ci.Attrs == nil {
		ci.Attrs = existing.Attrs
	}
	s.items[ci.ID] = *ci
	// 更新必须重新入索引：名称/属性变了，旧的 posting 会让检索命中已不存在的内容。
	s.indexCILocked(ci)
	return nil
}

func (s *MemoryCiStore) DeleteCI(_ context.Context, id, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return fmt.Errorf("CI %s not found", id)
	}
	if tenantID != "" && item.TenantID != tenantID {
		return fmt.Errorf("CI %s not found", id)
	}
	item.Status = "deleted"
	s.items[id] = item
	return nil
}

func (s *MemoryCiStore) GetCIHistory(ctx context.Context, ciID, tenantID string, limit int) ([]CiItem, error) {
	// MVP：memory 只返回当前版本（不含历史）
	item, err := s.GetCI(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	return []CiItem{*item}, nil
}

// === 全文检索 ===

// SearchCIs 按检索词做全文本检索（见 internal/cmdb/search.go 的设计说明）。
//
// 两段式：先用倒排索引前缀展开召回候选（超集），再用 matchCI 精确判定与排序。
func (s *MemoryCiStore) SearchCIs(_ context.Context, tenantID string, q CiSearchQuery) ([]CiSearchHit, error) {
	tokens, mode, limit, status := normalizeCiSearch(q)
	if len(tokens) == 0 {
		return []CiSearchHit{}, nil
	}
	s.mu.RLock()
	if s.idx == nil || s.idx.Size() != len(s.items) {
		s.mu.RUnlock()
		// 索引缺失，或条目数与 items 对不上（有写入路径绕过了索引维护）。
		// 升级为写锁全量重建：漏召回比慢一次严重得多。
		s.mu.Lock()
		s.rebuildSearchIndexLocked()
		defer s.mu.Unlock()
	} else {
		defer s.mu.RUnlock()
	}
	return s.searchCIsLocked(tokens, mode, limit, tenantID, q.CiType, status), nil
}

// rebuildSearchIndexLocked 按当前 items 全量重建检索索引（调用方持 s.mu 写锁）。
func (s *MemoryCiStore) rebuildSearchIndexLocked() {
	idx := fulltext.NewIndex[string]()
	for _, it := range s.items {
		idx.Add(it.ID, ciSearchText(&it))
	}
	s.idx = idx
}

// searchCIsLocked 在持锁状态下完成"召回 → 过滤 → 打分 → 排序 → 截断"。
func (s *MemoryCiStore) searchCIsLocked(tokens []string, mode CiSearchMode, limit int, tenantID, ciType, status string) []CiSearchHit {
	ids := s.candidateIDsLocked(tokens)
	hits := make([]CiSearchHit, 0, len(ids))
	for _, id := range ids {
		it, ok := s.items[id]
		if !ok {
			continue
		}
		if !ciMatchesFilter(&it, tenantID, ciType, status) {
			continue
		}
		score, matched := matchCI(&it, tokens, mode)
		if score <= 0 {
			continue
		}
		hits = append(hits, CiSearchHit{CiItem: it, Score: score, Matched: matched})
	}
	sortCiSearchHits(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// candidateIDsLocked 用倒排索引做候选召回：各检索词前缀展开后取并集。
//
// 并集对 all / any / phrase 三种模式都是命中集合的超集（交集 ⊆ 并集），
// 所以一种召回逻辑即可服务全部模式，最终判定交给 matchCI。
func (s *MemoryCiStore) candidateIDsLocked(tokens []string) []string {
	if s.idx == nil {
		// 无索引：退化为全量扫描，宁可慢也不能漏结果。
		out := make([]string, 0, len(s.items))
		for id := range s.items {
			out = append(out, id)
		}
		sort.Strings(out)
		return out
	}
	seen := make(map[string]struct{})
	for _, tok := range tokens {
		for _, id := range s.idx.SearchPrefix(tok) {
			seen[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out) // 候选有序，配合 sortCiSearchHits 的 ID 兜底保证结果稳定
	return out
}

// === Phase 2: 关系拓扑 ===

func (s *MemoryCiStore) CreateRelation(_ context.Context, rel *CiRelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relSeq++
	rel.ID = s.relSeq
	now := time.Now()
	rel.CreatedAt = now
	s.rels[rel.ID] = *rel
	return nil
}

func (s *MemoryCiStore) DeleteRelation(_ context.Context, id int64, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel, ok := s.rels[id]
	if !ok {
		return fmt.Errorf("relation %d not found", id)
	}
	if tenantID != "" && rel.TenantID != tenantID {
		return fmt.Errorf("relation %d not found", id)
	}
	delete(s.rels, id)
	return nil
}

func (s *MemoryCiStore) GetCIRelations(_ context.Context, ciID, tenantID string) ([]CiRelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CiRelation, 0)
	for _, rel := range s.rels {
		if rel.SourceCIID != ciID && rel.TargetCIID != ciID {
			continue
		}
		if tenantID != "" && rel.TenantID != tenantID {
			continue
		}
		out = append(out, rel)
	}
	return out, nil
}

func (s *MemoryCiStore) GetCIRelationGraph(ctx context.Context, ciID, tenantID string) (*CIRelationGraph, error) {
	center, err := s.GetCI(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	rels, err := s.GetCIRelations(ctx, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	withTargets := make([]RelationWithTarget, 0, len(rels))
	for _, rel := range rels {
		var targetName, targetType string
		var sourceName string
		targetID := rel.TargetCIID
		sourceID := rel.SourceCIID
		if tgt, ok := s.items[targetID]; ok {
			targetName = tgt.Name
			targetType = tgt.CiType
		}
		if src, ok := s.items[sourceID]; ok {
			sourceName = src.Name
		}
		withTargets = append(withTargets, RelationWithTarget{
			CiRelation: rel,
			SourceName: sourceName,
			TargetName: targetName,
			TargetType: targetType,
		})
	}
	return &CIRelationGraph{CenterCI: center, Relations: withTargets}, nil
}

// === Phase 2: 属性模板 ===

func (s *MemoryCiStore) CreateAttrTemplate(_ context.Context, tmpl *CiAttrTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tmplSeq++
	tmpl.ID = s.tmplSeq
	tmpl.CreatedAt = time.Now()
	s.templates[tmpl.ID] = *tmpl
	return nil
}

func (s *MemoryCiStore) GetAttrTemplates(_ context.Context, ciType, tenantID string) ([]CiAttrTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CiAttrTemplate, 0)
	for _, t := range s.templates {
		if ciType != "" && t.CiType != ciType {
			continue
		}
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *MemoryCiStore) UpdateAttrTemplate(_ context.Context, tmpl *CiAttrTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.templates[tmpl.ID]
	if !ok {
		return fmt.Errorf("template %d not found", tmpl.ID)
	}
	if tmpl.TenantID != "" && existing.TenantID != tmpl.TenantID {
		return fmt.Errorf("template %d not found", tmpl.ID)
	}
	tmpl.CreatedAt = existing.CreatedAt
	s.templates[tmpl.ID] = *tmpl
	return nil
}

func (s *MemoryCiStore) DeleteAttrTemplate(_ context.Context, id int, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmpl, ok := s.templates[id]
	if !ok {
		return fmt.Errorf("template %d not found", id)
	}
	if tenantID != "" && tmpl.TenantID != tenantID {
		return fmt.Errorf("template %d not found", id)
	}
	delete(s.templates, id)
	return nil
}
