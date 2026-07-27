package cmdb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryCiStore 内存实现 CMDB 存储（单机 MVP）。
type MemoryCiStore struct {
	mu        sync.RWMutex
	types     map[string]CiType              // name -> type
	items     map[string]CiItem              // id -> item
	rels      map[int64]CiRelation           // id -> relation
	relSeq    int64
	templates map[int]CiAttrTemplate         // id -> template
	tmplSeq   int
	seq       int
}

// NewMemoryCiStore 构造内存 CMDB 存储并初始化内置 CI 类型。
func NewMemoryCiStore() *MemoryCiStore {
	s := &MemoryCiStore{
		types:     make(map[string]CiType),
		items:     make(map[string]CiItem),
		rels:      make(map[int64]CiRelation),
		templates: make(map[int]CiAttrTemplate),
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
	return nil
}

// GetCIsByApproval 按审批状态列出 CI（Phase-3 待审列表）。
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

func (s *MemoryCiStore) GetCIHistory(_ context.Context, ciID, tenantID string, limit int) ([]CiItem, error) {
	// MVP：memory 只返回当前版本（不含历史）
	item, err := s.GetCI(nil, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	return []CiItem{*item}, nil
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
