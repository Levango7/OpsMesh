package governance

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotFound 记录不存在。
	ErrNotFound = errors.New("governance: not found")
	// ErrTenantMismatch 记录属于其他租户。
	ErrTenantMismatch = errors.New("governance: tenant mismatch")
	// ErrInvalid 输入非法。
	ErrInvalid = errors.New("governance: invalid argument")
)

// MemoryStore 进程内治理存储，按租户分桶。
type MemoryStore struct {
	mu        sync.RWMutex
	rules     map[string]map[string]DataRule
	results   map[string][]RuleResult
	pii       map[string]map[string]PiiPolicy
	retention map[string]map[string]RetentionPolicy
	catalog   map[string]map[string]CatalogAsset
}

// NewMemoryStore 构造空存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rules:     make(map[string]map[string]DataRule),
		results:   make(map[string][]RuleResult),
		pii:       make(map[string]map[string]PiiPolicy),
		retention: make(map[string]map[string]RetentionPolicy),
		catalog:   make(map[string]map[string]CatalogAsset),
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func nowUTC() time.Time { return time.Now().UTC() }

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	return nil
}

func tenantBucket[T any](m map[string]map[string]T, tenantID string) map[string]T {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]T)
		m[tenantID] = b
	}
	return b
}

func ownedByOtherTenant[T any](m map[string]map[string]T, tenantID, id string) bool {
	for tid, bucket := range m {
		if tid == tenantID {
			continue
		}
		if _, ok := bucket[id]; ok {
			return true
		}
	}
	return false
}

func notFoundOrMismatch(found bool) error {
	if found {
		return ErrTenantMismatch
	}
	return ErrNotFound
}

// CreateRule 新建规则；忽略调用方 ID 并由存储生成。
func (s *MemoryStore) CreateRule(rule DataRule) (DataRule, error) {
	if err := requireTenant(rule.TenantID); err != nil {
		return DataRule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	rule.ID = newID()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	tenantBucket(s.rules, rule.TenantID)[rule.ID] = rule
	return rule, nil
}

// UpdateRule 更新规则；CreatedAt 保留，UpdatedAt 刷新。
func (s *MemoryStore) UpdateRule(rule DataRule) (DataRule, error) {
	if err := requireTenant(rule.TenantID); err != nil {
		return DataRule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.rules[rule.TenantID]
	existing, ok := bucket[rule.ID]
	if !ok {
		return DataRule{}, notFoundOrMismatch(ownedByOtherTenant(s.rules, rule.TenantID, rule.ID))
	}
	rule.CreatedAt = existing.CreatedAt
	rule.UpdatedAt = nowUTC()
	bucket[rule.ID] = rule
	return rule, nil
}

// DeleteRule 删除规则。
func (s *MemoryStore) DeleteRule(tenantID, id string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.rules[tenantID]
	if _, ok := bucket[id]; !ok {
		return notFoundOrMismatch(ownedByOtherTenant(s.rules, tenantID, id))
	}
	delete(bucket, id)
	return nil
}

// GetRule 按租户读取规则。
func (s *MemoryStore) GetRule(tenantID, id string) (DataRule, error) {
	if err := requireTenant(tenantID); err != nil {
		return DataRule{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rule, ok := s.rules[tenantID][id]; ok {
		return rule, nil
	}
	return DataRule{}, notFoundOrMismatch(ownedByOtherTenant(s.rules, tenantID, id))
}

// ListRules 列出租户规则，按 CreatedAt、ID 稳定排序。
func (s *MemoryStore) ListRules(tenantID string) []DataRule {
	if err := requireTenant(tenantID); err != nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DataRule, 0, len(s.rules[tenantID]))
	for _, rule := range s.rules[tenantID] {
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// AppendRuleResult 追加评估结果。
func (s *MemoryStore) AppendRuleResult(result RuleResult) RuleResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.ID == "" {
		result.ID = newID()
	}
	if result.RunAt.IsZero() {
		result.RunAt = nowUTC()
	}
	s.results[result.TenantID] = append(s.results[result.TenantID], result)
	return result
}

// ListRuleResults 列出评估结果；tenantID 为空表示平台读取全部租户。
// ruleID 非空时按其过滤。按 RunAt、ID 稳定排序。
func (s *MemoryStore) ListRuleResults(tenantID, ruleID string) []RuleResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	collect := func(tid string) []RuleResult {
		return s.results[tid]
	}
	var source []RuleResult
	if tenantID == "" {
		for _, results := range s.results {
			source = append(source, results...)
		}
	} else {
		source = collect(tenantID)
	}
	out := make([]RuleResult, 0, len(source))
	for _, result := range source {
		if ruleID != "" && result.RuleID != ruleID {
			continue
		}
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RunAt.Equal(out[j].RunAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].RunAt.Before(out[j].RunAt)
	})
	return out
}

// CreatePIIPolicy 新建 PII 策略。
func (s *MemoryStore) CreatePIIPolicy(policy PiiPolicy) (PiiPolicy, error) {
	if err := requireTenant(policy.TenantID); err != nil {
		return PiiPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	policy.ID = newID()
	policy.CreatedAt = now
	policy.UpdatedAt = now
	tenantBucket(s.pii, policy.TenantID)[policy.ID] = policy
	return policy, nil
}

// UpdatePIIPolicy 更新 PII 策略。
func (s *MemoryStore) UpdatePIIPolicy(policy PiiPolicy) (PiiPolicy, error) {
	if err := requireTenant(policy.TenantID); err != nil {
		return PiiPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.pii[policy.TenantID]
	existing, ok := bucket[policy.ID]
	if !ok {
		return PiiPolicy{}, notFoundOrMismatch(ownedByOtherTenant(s.pii, policy.TenantID, policy.ID))
	}
	policy.CreatedAt = existing.CreatedAt
	policy.UpdatedAt = nowUTC()
	bucket[policy.ID] = policy
	return policy, nil
}

// DeletePIIPolicy 删除 PII 策略。
func (s *MemoryStore) DeletePIIPolicy(tenantID, id string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.pii[tenantID]
	if _, ok := bucket[id]; !ok {
		return notFoundOrMismatch(ownedByOtherTenant(s.pii, tenantID, id))
	}
	delete(bucket, id)
	return nil
}

// GetPIIPolicy 读取 PII 策略。
func (s *MemoryStore) GetPIIPolicy(tenantID, id string) (PiiPolicy, error) {
	if err := requireTenant(tenantID); err != nil {
		return PiiPolicy{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if policy, ok := s.pii[tenantID][id]; ok {
		return policy, nil
	}
	return PiiPolicy{}, notFoundOrMismatch(ownedByOtherTenant(s.pii, tenantID, id))
}

// ListPIIPolicies 列出租户 PII 策略。
func (s *MemoryStore) ListPIIPolicies(tenantID string) []PiiPolicy {
	if err := requireTenant(tenantID); err != nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PiiPolicy, 0, len(s.pii[tenantID]))
	for _, policy := range s.pii[tenantID] {
		out = append(out, policy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// CreateRetentionPolicy 新建保留策略。
func (s *MemoryStore) CreateRetentionPolicy(policy RetentionPolicy) (RetentionPolicy, error) {
	if err := requireTenant(policy.TenantID); err != nil {
		return RetentionPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	policy.ID = newID()
	policy.CreatedAt = now
	policy.UpdatedAt = now
	tenantBucket(s.retention, policy.TenantID)[policy.ID] = policy
	return policy, nil
}

// UpdateRetentionPolicy 更新保留策略。
func (s *MemoryStore) UpdateRetentionPolicy(policy RetentionPolicy) (RetentionPolicy, error) {
	if err := requireTenant(policy.TenantID); err != nil {
		return RetentionPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.retention[policy.TenantID]
	existing, ok := bucket[policy.ID]
	if !ok {
		return RetentionPolicy{}, notFoundOrMismatch(ownedByOtherTenant(s.retention, policy.TenantID, policy.ID))
	}
	policy.CreatedAt = existing.CreatedAt
	policy.UpdatedAt = nowUTC()
	bucket[policy.ID] = policy
	return policy, nil
}

// DeleteRetentionPolicy 删除保留策略。
func (s *MemoryStore) DeleteRetentionPolicy(tenantID, id string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.retention[tenantID]
	if _, ok := bucket[id]; !ok {
		return notFoundOrMismatch(ownedByOtherTenant(s.retention, tenantID, id))
	}
	delete(bucket, id)
	return nil
}

// GetRetentionPolicy 读取保留策略。
func (s *MemoryStore) GetRetentionPolicy(tenantID, id string) (RetentionPolicy, error) {
	if err := requireTenant(tenantID); err != nil {
		return RetentionPolicy{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if policy, ok := s.retention[tenantID][id]; ok {
		return policy, nil
	}
	return RetentionPolicy{}, notFoundOrMismatch(ownedByOtherTenant(s.retention, tenantID, id))
}

// ListRetentionPolicies 列出租户保留策略。
func (s *MemoryStore) ListRetentionPolicies(tenantID string) []RetentionPolicy {
	if err := requireTenant(tenantID); err != nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RetentionPolicy, 0, len(s.retention[tenantID]))
	for _, policy := range s.retention[tenantID] {
		out = append(out, policy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// CreateCatalogAsset 新建目录资产。
func (s *MemoryStore) CreateCatalogAsset(asset CatalogAsset) (CatalogAsset, error) {
	if err := requireTenant(asset.TenantID); err != nil {
		return CatalogAsset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	asset.ID = newID()
	asset.UpdatedAt = nowUTC()
	tenantBucket(s.catalog, asset.TenantID)[asset.ID] = asset
	return asset, nil
}

// UpdateCatalogAsset 更新目录资产。
func (s *MemoryStore) UpdateCatalogAsset(asset CatalogAsset) (CatalogAsset, error) {
	if err := requireTenant(asset.TenantID); err != nil {
		return CatalogAsset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.catalog[asset.TenantID]
	if _, ok := bucket[asset.ID]; !ok {
		return CatalogAsset{}, notFoundOrMismatch(ownedByOtherTenant(s.catalog, asset.TenantID, asset.ID))
	}
	asset.UpdatedAt = nowUTC()
	bucket[asset.ID] = asset
	return asset, nil
}

// DeleteCatalogAsset 删除目录资产。
func (s *MemoryStore) DeleteCatalogAsset(tenantID, id string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.catalog[tenantID]
	if _, ok := bucket[id]; !ok {
		return notFoundOrMismatch(ownedByOtherTenant(s.catalog, tenantID, id))
	}
	delete(bucket, id)
	return nil
}

// GetCatalogAsset 读取目录资产。
func (s *MemoryStore) GetCatalogAsset(tenantID, id string) (CatalogAsset, error) {
	if err := requireTenant(tenantID); err != nil {
		return CatalogAsset{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if asset, ok := s.catalog[tenantID][id]; ok {
		return asset, nil
	}
	return CatalogAsset{}, notFoundOrMismatch(ownedByOtherTenant(s.catalog, tenantID, id))
}

// ListCatalogAssets 列出目录资产，按 UpdatedAt、ID 稳定排序。
func (s *MemoryStore) ListCatalogAssets(tenantID string) []CatalogAsset {
	if err := requireTenant(tenantID); err != nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CatalogAsset, 0, len(s.catalog[tenantID]))
	for _, asset := range s.catalog[tenantID] {
		out = append(out, asset)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.Before(out[j].UpdatedAt)
	})
	return out
}
