// Package lakehouse 提供湖仓表维护（Iceberg/Delta/Hudi）与批/流统一作业模型。
package lakehouse

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
	ErrNotFound       = errors.New("lakehouse: not found")
	ErrTenantMismatch = errors.New("lakehouse: tenant mismatch")
	ErrInvalid        = errors.New("lakehouse: invalid argument")
)

// MemoryStore 进程内湖仓存储，按租户分桶。
type MemoryStore struct {
	mu          sync.RWMutex
	tables      map[string]map[string]TableConfig
	maintJobs   map[string]map[string]MaintenanceJob
	unifiedJobs map[string]map[string]UnifiedJob
	jobRuns     map[string]map[string]JobRun
}

// NewMemoryStore 构造空存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tables:      make(map[string]map[string]TableConfig),
		maintJobs:   make(map[string]map[string]MaintenanceJob),
		unifiedJobs: make(map[string]map[string]UnifiedJob),
		jobRuns:     make(map[string]map[string]JobRun),
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

func tenantBucketT(m map[string]map[string]TableConfig, tenantID string) map[string]TableConfig {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]TableConfig)
		m[tenantID] = b
	}
	return b
}

func tenantBucketM(m map[string]map[string]MaintenanceJob, tenantID string) map[string]MaintenanceJob {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]MaintenanceJob)
		m[tenantID] = b
	}
	return b
}

func tenantBucketU(m map[string]map[string]UnifiedJob, tenantID string) map[string]UnifiedJob {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]UnifiedJob)
		m[tenantID] = b
	}
	return b
}

func tenantBucketR(m map[string]map[string]JobRun, tenantID string) map[string]JobRun {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]JobRun)
		m[tenantID] = b
	}
	return b
}

func ownedByOtherGeneric[T any](m map[string]map[string]T, tenantID, id string) bool {
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

// --- TableConfig ---

func (s *MemoryStore) CreateTable(t TableConfig) (TableConfig, error) {
	if strings.TrimSpace(t.TenantID) == "" {
		return TableConfig{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(t.Name) == "" {
		return TableConfig{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if !IsValidFormat(t.Format) {
		return TableConfig{}, fmt.Errorf("%w: unsupported format %q", ErrInvalid, t.Format)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	t.ID = newID()
	t.CreatedAt = now
	t.UpdatedAt = now
	tenantBucketT(s.tables, t.TenantID)[t.ID] = t
	return t, nil
}

func (s *MemoryStore) GetTable(tenantID, id string) (TableConfig, error) {
	if strings.TrimSpace(tenantID) == "" {
		return TableConfig{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.tables[tenantID][id]; ok {
		return t, nil
	}
	if ownedByOtherGeneric(s.tables, tenantID, id) {
		return TableConfig{}, ErrTenantMismatch
	}
	return TableConfig{}, ErrNotFound
}

func (s *MemoryStore) ListTables(tenantID string) []TableConfig {
	if strings.TrimSpace(tenantID) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TableConfig, 0, len(s.tables[tenantID]))
	for _, t := range s.tables[tenantID] {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *MemoryStore) UpdateTable(t TableConfig) (TableConfig, error) {
	if strings.TrimSpace(t.TenantID) == "" {
		return TableConfig{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(t.ID) == "" {
		return TableConfig{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.tables[t.TenantID]
	existing, ok := bucket[t.ID]
	if !ok {
		if ownedByOtherGeneric(s.tables, t.TenantID, t.ID) {
			return TableConfig{}, ErrTenantMismatch
		}
		return TableConfig{}, ErrNotFound
	}
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = nowUTC()
	bucket[t.ID] = t
	return t, nil
}

func (s *MemoryStore) DeleteTable(tenantID, id string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.tables[tenantID]
	if _, ok := bucket[id]; !ok {
		if ownedByOtherGeneric(s.tables, tenantID, id) {
			return ErrTenantMismatch
		}
		return ErrNotFound
	}
	delete(bucket, id)
	return nil
}

// --- MaintenanceJob ---

func (s *MemoryStore) CreateMaintenanceJob(j MaintenanceJob) (MaintenanceJob, error) {
	if strings.TrimSpace(j.TenantID) == "" {
		return MaintenanceJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if !IsValidFormat(j.Format) {
		return MaintenanceJob{}, fmt.Errorf("%w: unsupported format %q", ErrInvalid, j.Format)
	}
	if !IsValidAction(j.Action) {
		return MaintenanceJob{}, fmt.Errorf("%w: unsupported action %q", ErrInvalid, j.Action)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	j.ID = newID()
	j.Status = "pending"
	j.CreatedAt = now
	tenantBucketM(s.maintJobs, j.TenantID)[j.ID] = j
	return j, nil
}

func (s *MemoryStore) GetMaintenanceJob(tenantID, id string) (MaintenanceJob, error) {
	if strings.TrimSpace(tenantID) == "" {
		return MaintenanceJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if j, ok := s.maintJobs[tenantID][id]; ok {
		return j, nil
	}
	if ownedByOtherGeneric(s.maintJobs, tenantID, id) {
		return MaintenanceJob{}, ErrTenantMismatch
	}
	return MaintenanceJob{}, ErrNotFound
}

func (s *MemoryStore) ListMaintenanceJobs(tenantID, tableID string) []MaintenanceJob {
	if strings.TrimSpace(tenantID) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MaintenanceJob, 0)
	for _, j := range s.maintJobs[tenantID] {
		if tableID != "" && j.TableID != tableID {
			continue
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryStore) UpdateMaintenanceJob(j MaintenanceJob) (MaintenanceJob, error) {
	if strings.TrimSpace(j.TenantID) == "" {
		return MaintenanceJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(j.ID) == "" {
		return MaintenanceJob{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.maintJobs[j.TenantID]
	existing, ok := bucket[j.ID]
	if !ok {
		if ownedByOtherGeneric(s.maintJobs, j.TenantID, j.ID) {
			return MaintenanceJob{}, ErrTenantMismatch
		}
		return MaintenanceJob{}, ErrNotFound
	}
	j.CreatedAt = existing.CreatedAt
	bucket[j.ID] = j
	return j, nil
}

// --- UnifiedJob ---

func (s *MemoryStore) CreateUnifiedJob(j UnifiedJob) (UnifiedJob, error) {
	if strings.TrimSpace(j.TenantID) == "" {
		return UnifiedJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(j.Name) == "" {
		return UnifiedJob{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if !IsValidJobKind(j.Kind) {
		return UnifiedJob{}, fmt.Errorf("%w: unsupported kind %q", ErrInvalid, j.Kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	j.ID = newID()
	j.CreatedAt = now
	j.UpdatedAt = now
	if j.MaxRetries <= 0 {
		j.MaxRetries = 3
	}
	if j.TimeoutSec <= 0 {
		j.TimeoutSec = 300
	}
	if j.Params == nil {
		j.Params = map[string]string{}
	}
	tenantBucketU(s.unifiedJobs, j.TenantID)[j.ID] = j
	return j, nil
}

func (s *MemoryStore) GetUnifiedJob(tenantID, id string) (UnifiedJob, error) {
	if strings.TrimSpace(tenantID) == "" {
		return UnifiedJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if j, ok := s.unifiedJobs[tenantID][id]; ok {
		return j, nil
	}
	if ownedByOtherGeneric(s.unifiedJobs, tenantID, id) {
		return UnifiedJob{}, ErrTenantMismatch
	}
	return UnifiedJob{}, ErrNotFound
}

func (s *MemoryStore) ListUnifiedJobs(tenantID string) []UnifiedJob {
	if strings.TrimSpace(tenantID) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UnifiedJob, 0, len(s.unifiedJobs[tenantID]))
	for _, j := range s.unifiedJobs[tenantID] {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *MemoryStore) UpdateUnifiedJob(j UnifiedJob) (UnifiedJob, error) {
	if strings.TrimSpace(j.TenantID) == "" {
		return UnifiedJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(j.ID) == "" {
		return UnifiedJob{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.unifiedJobs[j.TenantID]
	existing, ok := bucket[j.ID]
	if !ok {
		if ownedByOtherGeneric(s.unifiedJobs, j.TenantID, j.ID) {
			return UnifiedJob{}, ErrTenantMismatch
		}
		return UnifiedJob{}, ErrNotFound
	}
	j.CreatedAt = existing.CreatedAt
	j.UpdatedAt = nowUTC()
	if j.MaxRetries <= 0 {
		j.MaxRetries = existing.MaxRetries
	}
	if j.TimeoutSec <= 0 {
		j.TimeoutSec = existing.TimeoutSec
	}
	if j.Params == nil {
		j.Params = existing.Params
	}
	bucket[j.ID] = j
	return j, nil
}

func (s *MemoryStore) DeleteUnifiedJob(tenantID, id string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.unifiedJobs[tenantID]
	if _, ok := bucket[id]; !ok {
		if ownedByOtherGeneric(s.unifiedJobs, tenantID, id) {
			return ErrTenantMismatch
		}
		return ErrNotFound
	}
	delete(bucket, id)
	return nil
}

// --- JobRun ---

func (s *MemoryStore) CreateJobRun(r JobRun) (JobRun, error) {
	if strings.TrimSpace(r.TenantID) == "" {
		return JobRun{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(r.JobID) == "" {
		return JobRun{}, fmt.Errorf("%w: jobId is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = nowUTC()
	}
	if r.Status == "" {
		r.Status = JobStatusPending
	}
	if !IsValidJobStatus(r.Status) {
		return JobRun{}, fmt.Errorf("%w: invalid status %q", ErrInvalid, r.Status)
	}
	tenantBucketR(s.jobRuns, r.TenantID)[r.ID] = r
	return r, nil
}

func (s *MemoryStore) GetJobRun(tenantID, id string) (JobRun, error) {
	if strings.TrimSpace(tenantID) == "" {
		return JobRun{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.jobRuns[tenantID][id]; ok {
		return r, nil
	}
	if ownedByOtherGeneric(s.jobRuns, tenantID, id) {
		return JobRun{}, ErrTenantMismatch
	}
	return JobRun{}, ErrNotFound
}

func (s *MemoryStore) ListJobRuns(tenantID, jobID string) []JobRun {
	if strings.TrimSpace(tenantID) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]JobRun, 0)
	for _, r := range s.jobRuns[tenantID] {
		if jobID != "" && r.JobID != jobID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

func (s *MemoryStore) UpdateJobRun(r JobRun) (JobRun, error) {
	if strings.TrimSpace(r.TenantID) == "" {
		return JobRun{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(r.ID) == "" {
		return JobRun{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.jobRuns[r.TenantID]
	existing, ok := bucket[r.ID]
	if !ok {
		if ownedByOtherGeneric(s.jobRuns, r.TenantID, r.ID) {
			return JobRun{}, ErrTenantMismatch
		}
		return JobRun{}, ErrNotFound
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = existing.StartedAt
	}
	if r.EndedAt.IsZero() && (r.Status == JobStatusSucceeded || r.Status == JobStatusFailed || r.Status == JobStatusCancelled) {
		r.EndedAt = nowUTC()
	}
	if r.EndedAt.After(r.StartedAt) && r.Duration == "" {
		r.Duration = r.EndedAt.Sub(r.StartedAt).String()
	}
	bucket[r.ID] = r
	return r, nil
}
