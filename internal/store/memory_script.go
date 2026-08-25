
// memory_script.go 实现 MemoryStore 的 ScriptStore 子接口（Phase 5 自定义脚本）。
//
// 脚本内存实现：
//   - scripts / scriptExecutions 字段在 MemoryStore struct 中定义；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 6 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - ListScripts / ListScriptExecutions 返回深拷贝避免外部修改破坏内部状态；
//   - CreateScript 分配随机 ID（"script-" + 16 字节 hex）；
//   - ListScripts 按创建时间降序（最新优先）。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// randScriptID 生成随机脚本 ID（"script-" + 16 字节 hex）。
func randScriptID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("script-%d", time.Now().UnixNano())
	}
	return "script-" + hex.EncodeToString(b)
}

// randScriptExecutionID 生成随机脚本执行记录 ID（"script-exec-" + 16 字节 hex）。
func randScriptExecutionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("script-exec-%d", time.Now().UnixNano())
	}
	return "script-exec-" + hex.EncodeToString(b)
}

// cloneScript 返回 s 的深拷贝。
func cloneScript(s *Script) *Script {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// cloneScriptExecution 返回 e 的深拷贝（含 FinishedAt 指针）。
func cloneScriptExecution(e *ScriptExecution) *ScriptExecution {
	if e == nil {
		return nil
	}
	cp := *e
	if e.FinishedAt != nil {
		ft := *e.FinishedAt
		cp.FinishedAt = &ft
	}
	return &cp
}

// CreateScript 创建脚本（按 ID 幂等；ID 为空时分配随机 ID）。
//
// 行为：
//   - ID 为空时分配随机 ID（新建场景）；
//   - TenantID 为空时归一为 default；
//   - CreatedAt 为空时填当前时间；
//   - UpdatedAt 始终刷新为当前时间；
//   - Language 为空时默认 "shell"。
func (m *MemoryStore) CreateScript(tenantID string, s *Script) *Script {
	if s == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if s.ID == "" {
		s.ID = randScriptID()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.Language == "" {
		s.Language = "shell"
	}
	s.UpdatedAt = now
	m.scripts[s.ID] = s
	return cloneScript(s)
}

// GetScript 按 (tenantID, id) 返回单个脚本（深拷贝；不存在或租户不匹配返回 (nil, false)）。
func (m *MemoryStore) GetScript(tenantID, id string) (*Script, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.scripts[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && s.TenantID != tenantID {
		return nil, false
	}
	return cloneScript(s), true
}

// UpdateScript 更新脚本（按 s.ID 定位，校验 tenantID 归属）。
//
// 行为：
//   - 不存在或租户不匹配返回 (nil, false)；
//   - CreatedAt / TenantID 不可改（保留原值，防越权改归属）；
//   - UpdatedAt 始终刷新为当前时间；
//   - 返回更新后的脚本（深拷贝）。
func (m *MemoryStore) UpdateScript(tenantID string, s *Script) (*Script, bool) {
	if s == nil || s.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.scripts[s.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	s.ID = existing.ID
	s.TenantID = existing.TenantID
	s.CreatedAt = existing.CreatedAt
	s.UpdatedAt = time.Now()
	m.scripts[s.ID] = s
	return cloneScript(s), true
}

// ListScripts 返回指定租户的全部脚本（按创建时间降序）。
func (m *MemoryStore) ListScripts(tenantID string) []*Script {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Script, 0, len(m.scripts))
	for _, s := range m.scripts {
		if tenantID != "" && s.TenantID != tenantID {
			continue
		}
		out = append(out, cloneScript(s))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// DeleteScript 删除脚本，返回是否删除成功（不存在或租户不匹配返回 false）。
func (m *MemoryStore) DeleteScript(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.scripts[id]
	if !ok {
		return false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return false
	}
	delete(m.scripts, id)
	return true
}

// ListScriptExecutions 返回指定脚本的执行记录（按开始时间降序）。
// 不存在或租户不匹配返回空 slice。
func (m *MemoryStore) ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 校验脚本归属。
	s, ok := m.scripts[scriptID]
	if !ok {
		return []*ScriptExecution{}
	}
	if tenantID != "" && s.TenantID != tenantID {
		return []*ScriptExecution{}
	}
	out := make([]*ScriptExecution, 0)
	for _, e := range m.scriptExecutions {
		if e.ScriptID != scriptID {
			continue
		}
		if tenantID != "" && e.TenantID != tenantID {
			continue
		}
		out = append(out, cloneScriptExecution(e))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// RecordScriptExecution 记录一条脚本执行记录（供 controlplane execute handler 调用）。
// ID 为空时由 store 分配随机 ID；StartedAt 为零值时填当前时间。
// 返回深拷贝的执行记录。
func (m *MemoryStore) RecordScriptExecution(tenantID, scriptID, deviceID, status, stdout, stderr string, startedAt time.Time, finishedAt *time.Time) *ScriptExecution {
	if tenantID == "" {
		tenantID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	e := &ScriptExecution{
		ID:         randScriptExecutionID(),
		TenantID:   tenantID,
		ScriptID:   scriptID,
		DeviceID:   deviceID,
		Status:     status,
		Stdout:     stdout,
		Stderr:     stderr,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	m.scriptExecutions[e.ID] = e
	return cloneScriptExecution(e)
}