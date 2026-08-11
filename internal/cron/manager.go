
// manager.go 提供显式定时任务管理（ScheduleEntry CRUD + 暂停/恢复）。
//
// 与 schedule.go 的 Scheduler 协同：
//   - Scheduler 周期扫描 store 中带 Schedule 的 Task 模板并派生实例；
//   - Manager 维护 ScheduleEntry 索引（含状态/下次执行时间），供 API 层 CRUD。
//
// 设计原则：
//   - Manager 不直接派生任务实例（那是 Scheduler 的职责），仅维护元数据；
//   - 线程安全通过 sync.RWMutex 保护 entries 索引；
//   - 不依赖 store.Store，仅内存索引，重启后从 store.AllTasks 重建（由调用方负责）。
package cron

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// EntryStatus 定时任务状态。
type EntryStatus string

const (
	EntryActive   EntryStatus = "active"   // 活跃（按 cron 派生）
	EntryPaused   EntryStatus = "paused"   // 暂停（不派生）
	EntryDeleted  EntryStatus = "deleted"  // 已删除（软删，保留历史）
)

// ScheduleEntry 定时任务元数据。
//
// 与 proto.Task 模板关联：TaskID 指向 store 中的模板任务（Schedule 字段非空）。
// NextRunAt 由 NextRun 计算填充，供 API 层展示"下次执行时间"。
type ScheduleEntry struct {
	ID         string       // entry ID（全局唯一，由 Manager 分配）
	TaskID     string       // 关联的模板任务 ID（store 中的 Task）
	TenantID   string       // 租户 ID
	Name       string       // 任务名称（用户可读）
	CronExpr   string       // cron 表达式（5 字段）
	Status     EntryStatus  // 状态：active/paused/deleted
	CreatedAt  time.Time    // 创建时间
	UpdatedAt  time.Time    // 最近更新时间
	LastRunAt  time.Time    // 上次执行时间
	NextRunAt  time.Time    // 下次预计执行时间（由 NextRun 计算）
	CreatedBy  string       // 创建人 userID
}

// Validate 校验 entry 合法性。
func (e *ScheduleEntry) Validate() error {
	if e.TaskID == "" {
		return errors.New("cron/manager: TaskID is required")
	}
	if e.CronExpr == "" {
		return errors.New("cron/manager: CronExpr is required")
	}
	// 校验 cron 表达式语法（不评估具体时间）。
	if _, err := Match(e.CronExpr, time.Now()); err != nil {
		return errors.New("cron/manager: invalid cron expression: " + err.Error())
	}
	return nil
}

// ErrEntryNotFound entry 不存在。
var ErrEntryNotFound = errors.New("cron/manager: entry not found")

// ErrEntryExists entry ID 已存在。
var ErrEntryExists = errors.New("cron/manager: entry already exists")

// Manager 定时任务管理器：维护 ScheduleEntry 索引。
//
// 线程安全：所有公共方法通过 mu 保护 entries 索引。
type Manager struct {
	mu      sync.RWMutex
	entries map[string]*ScheduleEntry // entryID -> entry
	counter uint64                    // ID 生成计数器
	now     func() time.Time
}

// NewManager 构造管理器。
func NewManager() *Manager {
	return &Manager{
		entries: make(map[string]*ScheduleEntry),
		now:     time.Now,
	}
}

// SetNow 注入时间函数（测试用）。
func (m *Manager) SetNow(fn func() time.Time) {
	m.mu.Lock()
	m.now = fn
	m.mu.Unlock()
}

// nowTime 返回当前时间。
func (m *Manager) nowTime() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

// nextID 生成下一个 entry ID（entry-<n>）。调用方须持锁。
func (m *Manager) nextID() string {
	m.counter++
	return "entry-" + itoa(m.counter)
}

// itoa 简易 uint64 → string（避免引入 strconv 增加依赖）。
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Create 创建定时任务 entry。ID 为空时由 Manager 分配。
func (m *Manager) Create(e *ScheduleEntry) (*ScheduleEntry, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	now := m.nowTime()
	cp := *e
	if cp.ID == "" {
		m.mu.Lock()
		cp.ID = m.nextID()
		m.mu.Unlock()
	} else {
		m.mu.RLock()
		_, exists := m.entries[cp.ID]
		m.mu.RUnlock()
		if exists {
			return nil, ErrEntryExists
		}
	}
	if cp.Status == "" {
		cp.Status = EntryActive
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	cp.NextRunAt = NextRun(cp.CronExpr, now)

	m.mu.Lock()
	m.entries[cp.ID] = &cp
	m.mu.Unlock()
	return &cp, nil
}

// Get 按 ID 查询 entry。不存在返回 ErrEntryNotFound。
func (m *Manager) Get(id string) (*ScheduleEntry, error) {
	m.mu.RLock()
	e, ok := m.entries[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrEntryNotFound
	}
	cp := *e
	return &cp, nil
}

// List 列出指定租户的 entry（tenantID 空则全部），按 ID 升序。
// status 空则返回所有状态（含 deleted）；非空则按状态过滤。
func (m *Manager) List(tenantID string, status EntryStatus) []*ScheduleEntry {
	m.mu.RLock()
	out := make([]*ScheduleEntry, 0, len(m.entries))
	for _, e := range m.entries {
		if tenantID != "" && e.TenantID != tenantID {
			continue
		}
		if status != "" && e.Status != status {
			continue
		}
		cp := *e
		out = append(out, &cp)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Update 更新 entry。不存在返回 ErrEntryNotFound。
// 仅允许更新 Name/CronExpr/Status；TaskID/TenantID/CreatedAt 不可变。
func (m *Manager) Update(id string, patch *ScheduleEntry) (*ScheduleEntry, error) {
	if patch != nil && patch.CronExpr != "" {
		if _, err := Match(patch.CronExpr, time.Now()); err != nil {
			return nil, errors.New("cron/manager: invalid cron expression: " + err.Error())
		}
	}
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok {
		m.mu.Unlock()
		return nil, ErrEntryNotFound
	}
	if patch != nil {
		if patch.Name != "" {
			e.Name = patch.Name
		}
		if patch.CronExpr != "" {
			e.CronExpr = patch.CronExpr
		}
		if patch.Status != "" {
			e.Status = patch.Status
		}
	}
	e.UpdatedAt = m.nowTime()
	if e.Status == EntryActive {
		e.NextRunAt = NextRun(e.CronExpr, e.UpdatedAt)
	} else {
		e.NextRunAt = time.Time{} // 暂停/删除清空下次执行
	}
	cp := *e
	m.mu.Unlock()
	return &cp, nil
}

// Delete 软删除 entry（标记 deleted）。不存在返回 ErrEntryNotFound。
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok {
		m.mu.Unlock()
		return ErrEntryNotFound
	}
	e.Status = EntryDeleted
	e.UpdatedAt = m.nowTime()
	e.NextRunAt = time.Time{}
	m.mu.Unlock()
	return nil
}

// Pause 暂停 entry。不存在返回 ErrEntryNotFound；已暂停为幂等。
func (m *Manager) Pause(id string) (*ScheduleEntry, error) {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok {
		m.mu.Unlock()
		return nil, ErrEntryNotFound
	}
	e.Status = EntryPaused
	e.UpdatedAt = m.nowTime()
	e.NextRunAt = time.Time{}
	cp := *e
	m.mu.Unlock()
	return &cp, nil
}

// Resume 恢复 entry。不存在返回 ErrEntryNotFound；已活跃为幂等。
func (m *Manager) Resume(id string) (*ScheduleEntry, error) {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok {
		m.mu.Unlock()
		return nil, ErrEntryNotFound
	}
	e.Status = EntryActive
	e.UpdatedAt = m.nowTime()
	e.NextRunAt = NextRun(e.CronExpr, e.UpdatedAt)
	cp := *e
	m.mu.Unlock()
	return &cp, nil
}

// MarkFired 标记 entry 已在 at 时刻派生实例，更新 LastRunAt 与 NextRunAt。
// 由 Scheduler 在派生实例后调用。不存在或非 active 静默跳过。
func (m *Manager) MarkFired(id string, at time.Time) {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok || e.Status != EntryActive {
		m.mu.Unlock()
		return
	}
	e.LastRunAt = at
	e.NextRunAt = NextRun(e.CronExpr, at)
	m.mu.Unlock()
}