// Package dataetl 提供 ETL 作业模型、内存存储与引擎适配器。
package dataetl

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
	ErrNotFound       = errors.New("dataetl: not found")
	ErrTenantMismatch = errors.New("dataetl: tenant mismatch")
	ErrInvalid        = errors.New("dataetl: invalid argument")
)

// MemoryStore 进程内 ETL 存储，按租户分桶。
type MemoryStore struct {
	mu   sync.RWMutex
	jobs map[string]map[string]EtlJob
	runs map[string]map[string]EtlRun
}

// NewMemoryStore 构造空存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs: make(map[string]map[string]EtlJob),
		runs: make(map[string]map[string]EtlRun),
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

func tenantBucketJobs(m map[string]map[string]EtlJob, tenantID string) map[string]EtlJob {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]EtlJob)
		m[tenantID] = b
	}
	return b
}

func tenantBucketRuns(m map[string]map[string]EtlRun, tenantID string) map[string]EtlRun {
	b := m[tenantID]
	if b == nil {
		b = make(map[string]EtlRun)
		m[tenantID] = b
	}
	return b
}

func ownedByOther[T any](m map[string]map[string]T, tenantID, id string) bool {
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

// CreateJob 新建作业；忽略调用方 ID 并由存储生成。
func (s *MemoryStore) CreateJob(job EtlJob) (EtlJob, error) {
	if strings.TrimSpace(job.TenantID) == "" {
		return EtlJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(job.Name) == "" {
		return EtlJob{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if !IsValidEngine(job.Engine) {
		return EtlJob{}, fmt.Errorf("%w: unsupported engine %q", ErrInvalid, job.Engine)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowUTC()
	job.ID = newID()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.MaxRetries <= 0 {
		job.MaxRetries = 3
	}
	if job.TimeoutSec <= 0 {
		job.TimeoutSec = 300
	}
	if job.Params == nil {
		job.Params = map[string]string{}
	}
	tenantBucketJobs(s.jobs, job.TenantID)[job.ID] = job
	return job, nil
}

// GetJob 按租户读取作业。
func (s *MemoryStore) GetJob(tenantID, id string) (EtlJob, error) {
	if strings.TrimSpace(tenantID) == "" {
		return EtlJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if job, ok := s.jobs[tenantID][id]; ok {
		return job, nil
	}
	if ownedByOther(s.jobs, tenantID, id) {
		return EtlJob{}, ErrTenantMismatch
	}
	return EtlJob{}, ErrNotFound
}

// ListJobs 列出租户作业，按 CreatedAt、ID 稳定排序。
func (s *MemoryStore) ListJobs(tenantID string) []EtlJob {
	if strings.TrimSpace(tenantID) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EtlJob, 0, len(s.jobs[tenantID]))
	for _, job := range s.jobs[tenantID] {
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// UpdateJob 更新作业。
func (s *MemoryStore) UpdateJob(job EtlJob) (EtlJob, error) {
	if strings.TrimSpace(job.TenantID) == "" {
		return EtlJob{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(job.ID) == "" {
		return EtlJob{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	if !IsValidEngine(job.Engine) {
		return EtlJob{}, fmt.Errorf("%w: unsupported engine %q", ErrInvalid, job.Engine)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.jobs[job.TenantID]
	existing, ok := bucket[job.ID]
	if !ok {
		if ownedByOther(s.jobs, job.TenantID, job.ID) {
			return EtlJob{}, ErrTenantMismatch
		}
		return EtlJob{}, ErrNotFound
	}
	job.CreatedAt = existing.CreatedAt
	job.UpdatedAt = nowUTC()
	if job.MaxRetries <= 0 {
		job.MaxRetries = existing.MaxRetries
	}
	if job.TimeoutSec <= 0 {
		job.TimeoutSec = existing.TimeoutSec
	}
	if job.Params == nil {
		job.Params = existing.Params
	}
	bucket[job.ID] = job
	return job, nil
}

// DeleteJob 删除作业。
func (s *MemoryStore) DeleteJob(tenantID, id string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.jobs[tenantID]
	if _, ok := bucket[id]; !ok {
		if ownedByOther(s.jobs, tenantID, id) {
			return ErrTenantMismatch
		}
		return ErrNotFound
	}
	delete(bucket, id)
	// 级联删除运行记录
	if rb := s.runs[tenantID]; rb != nil {
		for rid, run := range rb {
			if run.JobID == id {
				delete(rb, rid)
			}
		}
	}
	return nil
}

// CreateRun 新建运行记录。
func (s *MemoryStore) CreateRun(run EtlRun) (EtlRun, error) {
	if strings.TrimSpace(run.TenantID) == "" {
		return EtlRun{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(run.JobID) == "" {
		return EtlRun{}, fmt.Errorf("%w: jobId is required", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.ID == "" {
		run.ID = newID()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = nowUTC()
	}
	if run.Status == "" {
		run.Status = StatusPending
	}
	if !IsValidStatus(run.Status) {
		return EtlRun{}, fmt.Errorf("%w: invalid status %q", ErrInvalid, run.Status)
	}
	tenantBucketRuns(s.runs, run.TenantID)[run.ID] = run
	return run, nil
}

// GetRun 按租户读取运行记录。
func (s *MemoryStore) GetRun(tenantID, id string) (EtlRun, error) {
	if strings.TrimSpace(tenantID) == "" {
		return EtlRun{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if run, ok := s.runs[tenantID][id]; ok {
		return run, nil
	}
	if ownedByOther(s.runs, tenantID, id) {
		return EtlRun{}, ErrTenantMismatch
	}
	return EtlRun{}, ErrNotFound
}

// ListRuns 列出租户某作业的运行记录，按 StartedAt 倒序。
func (s *MemoryStore) ListRuns(tenantID, jobID string) []EtlRun {
	if strings.TrimSpace(tenantID) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EtlRun, 0)
	for _, run := range s.runs[tenantID] {
		if jobID != "" && run.JobID != jobID {
			continue
		}
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// UpdateRun 更新运行记录（状态/时间/日志/错误）。
func (s *MemoryStore) UpdateRun(run EtlRun) (EtlRun, error) {
	if strings.TrimSpace(run.TenantID) == "" {
		return EtlRun{}, fmt.Errorf("%w: tenantId is required", ErrInvalid)
	}
	if strings.TrimSpace(run.ID) == "" {
		return EtlRun{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	if !IsValidStatus(run.Status) && run.Status != "" {
		return EtlRun{}, fmt.Errorf("%w: invalid status %q", ErrInvalid, run.Status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.runs[run.TenantID]
	existing, ok := bucket[run.ID]
	if !ok {
		if ownedByOther(s.runs, run.TenantID, run.ID) {
			return EtlRun{}, ErrTenantMismatch
		}
		return EtlRun{}, ErrNotFound
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = existing.StartedAt
	}
	if run.EndedAt.IsZero() && run.Status == StatusSucceeded || run.Status == StatusFailed || run.Status == StatusCancelled {
		run.EndedAt = nowUTC()
	}
	if run.EndedAt.After(run.StartedAt) && run.Duration == "" {
		run.Duration = run.EndedAt.Sub(run.StartedAt).String()
	}
	bucket[run.ID] = run
	return run, nil
}
