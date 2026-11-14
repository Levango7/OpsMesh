package models

import (
	"sync"
	"time"
)

// IncidentStatus represents the lifecycle stage of an incident.
type IncidentStatus string

const (
	StatusDetected      IncidentStatus = "detected"
	StatusInvestigating IncidentStatus = "investigating"
	StatusMitigating    IncidentStatus = "mitigating"
	StatusResolved      IncidentStatus = "resolved"
	StatusClosed        IncidentStatus = "closed"
)

// Severity represents the severity level of an incident.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Incident represents a managed incident aggregating related alerts.
type Incident struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Severity    Severity          `json:"severity"`
	Status      IncidentStatus    `json:"status"`
	AlertIDs    []string          `json:"alert_ids"`
	DeviceIDs   []string          `json:"device_ids"`
	Assignee    string            `json:"assignee"`
	Tags        map[string]string `json:"tags"`
	// TenantID 是行级租户隔离键（D4）。语义与单体侧 alerts/tickets 一致：
	// 写入时必须钉住（空值拒绝），读取/列/改/删一律按它过滤——事件是"客户的故障单"，
	// 权限目录里 incident:read/write 也是租户级授权点（internal/store/model/perm.go）。
	TenantID string `json:"tenant_id,omitempty"`
	// OccurredAt 最早已知故障发生时刻（来自触发告警的触发时间或人工回填）。
	// 此前模型只有 DetectedAt 且创建时 DetectedAt==CreatedAt，导致 MTTD 结构恒 0
	//（TD-60 §5.10 取证）——MTTD = DetectedAt - OccurredAt，缺省时不计入统计。
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
	DetectedAt time.Time  `json:"detected_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// TimelineEvent represents a single event in an incident timeline.
type TimelineEvent struct {
	ID          string    `json:"id"`
	IncidentID  string    `json:"incident_id"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
}

// Alert represents an ingested alert that can be aggregated into incidents.
type Alert struct {
	ID string `json:"id"`
	// TenantID：告警所属租户（issue #27 裁决②）。ingest 契约要求推送方携带；
	// 缺失时 handler 会回退到网关注入的 X-Tenant-ID，两者都没有则 400——不落无主数据。
	TenantID  string            `json:"tenant_id"`
	Source    string            `json:"source"`
	DeviceID  string            `json:"device_id"`
	Severity  Severity          `json:"severity"`
	Message   string            `json:"message"`
	Metric    string            `json:"metric"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels"`
}

// Postmortem represents a generated postmortem document.
type Postmortem struct {
	IncidentID     string          `json:"incident_id"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary"`
	Impact         string          `json:"impact"`
	RootCause      string          `json:"root_cause"`
	Timeline       []TimelineEvent `json:"timeline"`
	LessonsLearned []string        `json:"lessons_learned"`
	ActionItems    []string        `json:"action_items"`
	MTTD           time.Duration   `json:"mttd"`
	MTTR           time.Duration   `json:"mttr"`
	GeneratedAt    time.Time       `json:"generated_at"`
}

// ResponseMetrics holds incident response metrics.
type ResponseMetrics struct {
	TotalIncidents    int           `json:"total_incidents"`
	ActiveIncidents   int           `json:"active_incidents"`
	ResolvedIncidents int           `json:"resolved_incidents"`
	AvgMTTD           time.Duration `json:"avg_mttd"`
	AvgMTTR           time.Duration `json:"avg_mttr"`
	AvgMTTF           time.Duration `json:"avg_mttf"`
}

// IncidentStore is the interface for incident persistence.
// IncidentStore 的每个读/改方法都**显式带租户参数**（D4 租户隔离）。
// 为什么不像单体那样只靠对象上的 TenantID：接口签名强制所有调用点传租户——漏传即编译不过，
// 比"忘了过滤"这类运行期缺陷便宜得多；写入侧再由 CreateIncident 拒绝空租户兜底。
type IncidentStore interface {
	CreateIncident(*Incident) *Incident
	GetIncident(id, tenantID string) *Incident
	UpdateIncident(inc *Incident, tenantID string) *Incident
	DeleteIncident(id, tenantID string) bool
	ListIncidents(status string, severity Severity, tenantID string) []*Incident
	AddTimelineEvent(ev *TimelineEvent, tenantID string) *TimelineEvent
	GetTimeline(incidentID, tenantID string) []TimelineEvent
	Incidents(tenantID string) []*Incident
}

// MemoryStore is an in-memory implementation of IncidentStore.
type MemoryStore struct {
	mu        sync.RWMutex
	incidents map[string]*Incident
	timeline  map[string][]TimelineEvent
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		incidents: make(map[string]*Incident),
		timeline:  make(map[string][]TimelineEvent),
	}
}

// CreateIncident stores a new incident. 空租户拒绝写入（D4：无主数据不许落库）。
func (m *MemoryStore) CreateIncident(inc *Incident) *Incident {
	if inc == nil || inc.TenantID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// ID 冲突时若归属不同租户，视为不可见（不覆盖别人的数据，也不泄露"该 ID 已存在"）。
	if existing, ok := m.incidents[inc.ID]; ok && existing.TenantID != inc.TenantID {
		return nil
	}
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now()
	}
	if inc.UpdatedAt.IsZero() {
		inc.UpdatedAt = inc.CreatedAt
	}
	m.incidents[inc.ID] = inc
	return inc
}

// GetIncident retrieves an incident by ID. 跨租户一律当作不存在（不区分"没有"与"不是你的"）。
func (m *MemoryStore) GetIncident(id, tenantID string) *Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.incidents[id]
	if !ok || inc.TenantID != tenantID {
		return nil
	}
	return inc
}

// UpdateIncident updates an existing incident. 跨租户不可改；且**不允许改租户**（归属变更=越权改挂）。
func (m *MemoryStore) UpdateIncident(inc *Incident, tenantID string) *Incident {
	if inc == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.incidents[inc.ID]
	if !ok || existing.TenantID != tenantID {
		return nil
	}
	inc.TenantID = existing.TenantID
	inc.CreatedAt = existing.CreatedAt
	inc.UpdatedAt = time.Now()
	m.incidents[inc.ID] = inc
	return inc
}

// DeleteIncident removes an incident. 跨租户一律 false。
func (m *MemoryStore) DeleteIncident(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.incidents[id]
	if !ok || existing.TenantID != tenantID {
		return false
	}
	delete(m.incidents, id)
	delete(m.timeline, id)
	return true
}

// ListIncidents returns incidents filtered by status and severity（先按租户收窄）。
func (m *MemoryStore) ListIncidents(status string, severity Severity, tenantID string) []*Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		if inc.TenantID != tenantID {
			continue
		}
		if status != "" && string(inc.Status) != status {
			continue
		}
		if severity != "" && inc.Severity != severity {
			continue
		}
		out = append(out, inc)
	}
	return out
}

// AddTimelineEvent adds an event to an incident's timeline.
// 归属由**事件的父事件单**决定：父单不属于该租户 ⇒ 拒收（调用方拿不到"别人的单"的写入权）。
func (m *MemoryStore) AddTimelineEvent(ev *TimelineEvent, tenantID string) *TimelineEvent {
	if ev == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	parent, ok := m.incidents[ev.IncidentID]
	if !ok || parent.TenantID != tenantID {
		return nil
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	m.timeline[ev.IncidentID] = append(m.timeline[ev.IncidentID], *ev)
	return ev
}

// GetTimeline returns the timeline for an incident. 父单不属于该租户 ⇒ 返回空（不泄露"该单存在"）。
func (m *MemoryStore) GetTimeline(incidentID, tenantID string) []TimelineEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	parent, ok := m.incidents[incidentID]
	if !ok || parent.TenantID != tenantID {
		return nil
	}
	events := m.timeline[incidentID]
	out := make([]TimelineEvent, len(events))
	copy(out, events)
	return out
}

// Incidents returns all incidents of the tenant.
func (m *MemoryStore) Incidents(tenantID string) []*Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		if inc.TenantID != tenantID {
			continue
		}
		out = append(out, inc)
	}
	return out
}
