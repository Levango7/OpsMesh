package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/aggregate"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/timeline"
)

// Errors returned by the service.
var (
	ErrIncidentNotFound = errors.New("incident not found")
	ErrInvalidStatus    = errors.New("invalid status transition")
	ErrIncidentResolved = errors.New("incident already resolved")
)

// Service implements the incident management business logic.
type Service struct {
	store    models.IncidentStore
	engine   *aggregate.Engine
	timeline *timeline.Builder
}

// NewService creates a new Service.
func NewService(store models.IncidentStore, eng *aggregate.Engine) *Service {
	return &Service{
		store:    store,
		engine:   eng,
		timeline: timeline.NewBuilder(),
	}
}

// CreateIncidentInput 创建事故的入参。OccurredAt 为最早已知故障发生时刻
// （来自触发告警或人工回填）；缺省表示未知，MTTD 统计将不计入该条。
type CreateIncidentInput struct {
	Title       string
	Description string
	Severity    models.Severity
	DeviceIDs   []string
	OccurredAt  *time.Time
}

// CreateIncident creates a new incident.
func (s *Service) CreateIncident(in CreateIncidentInput) (*models.Incident, error) {
	if in.Title == "" {
		return nil, errors.New("incident title is required")
	}

	now := time.Now()
	inc := &models.Incident{
		ID:          uuid.New().String(),
		Title:       in.Title,
		Description: in.Description,
		Severity:    in.Severity,
		Status:      models.StatusDetected,
		DeviceIDs:   in.DeviceIDs,
		OccurredAt:  in.OccurredAt,
		DetectedAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        make(map[string]string),
	}

	s.store.CreateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "created",
		Description: "Incident created: " + in.Title,
		Author:      "system",
	})

	return inc, nil
}

// GetIncident retrieves an incident by ID.
func (s *Service) GetIncident(id string) (*models.Incident, error) {
	inc := s.store.GetIncident(id)
	if inc == nil {
		return nil, ErrIncidentNotFound
	}
	return inc, nil
}

// ListIncidents lists incidents filtered by status and severity.
func (s *Service) ListIncidents(status string, severity models.Severity) []*models.Incident {
	return s.store.ListIncidents(status, severity)
}

// UpdateIncident updates an existing incident.
func (s *Service) UpdateIncident(id, title, description, assignee string, severity models.Severity) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if inc.Status == models.StatusResolved || inc.Status == models.StatusClosed {
		return nil, ErrIncidentResolved
	}

	if title != "" {
		inc.Title = title
	}
	if description != "" {
		inc.Description = description
	}
	if assignee != "" {
		inc.Assignee = assignee
	}
	if severity != "" {
		inc.Severity = severity
	}

	s.store.UpdateIncident(inc)
	return inc, nil
}

// UpdateStatus transitions an incident's status via PUT {id}.
// 状态机约束：只允许沿 detected→investigating→mitigating→resolved→closed
// 向前迁移；任何回退（含 resolved→detected）与同状态重入（resolved/closed
// 再次 resolve）都拒绝为 ErrInvalidStatus。
func (s *Service) UpdateStatus(id string, status models.IncidentStatus) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if !validForwardTransition(inc.Status, status) {
		return nil, ErrInvalidStatus
	}

	now := time.Now()
	inc.Status = status
	switch status {
	case models.StatusResolved:
		if inc.ResolvedAt == nil {
			t := now
			inc.ResolvedAt = &t
		}
	case models.StatusClosed:
		if inc.ClosedAt == nil {
			t := now
			inc.ClosedAt = &t
		}
	}
	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "status_changed",
		Description: "Status changed to " + string(status),
		Author:      "system",
	})

	return inc, nil
}

// validForwardTransition reports whether from→to is a legal forward-only move.
func validForwardTransition(from, to models.IncidentStatus) bool {
	if to != models.StatusDetected && to != models.StatusInvestigating &&
		to != models.StatusMitigating && to != models.StatusResolved && to != models.StatusClosed {
		return false
	}
	switch from {
	case models.StatusDetected:
		return to == models.StatusInvestigating || to == models.StatusMitigating ||
			to == models.StatusResolved || to == models.StatusClosed
	case models.StatusInvestigating:
		return to == models.StatusMitigating || to == models.StatusResolved || to == models.StatusClosed
	case models.StatusMitigating:
		return to == models.StatusResolved || to == models.StatusClosed
	case models.StatusResolved:
		return to == models.StatusClosed
	case models.StatusClosed:
		return false
	default:
		return false
	}
}

// DeleteIncident deletes an incident.
func (s *Service) DeleteIncident(id string) error {
	if !s.store.DeleteIncident(id) {
		return ErrIncidentNotFound
	}
	return nil
}

// AddTimelineEvent adds an event to an incident's timeline.
func (s *Service) AddTimelineEvent(incidentID, eventType, description, author string) (*models.TimelineEvent, error) {
	inc, err := s.GetIncident(incidentID)
	if err != nil {
		return nil, err
	}

	ev := &models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   time.Now(),
		Type:        eventType,
		Description: description,
		Author:      author,
	}

	s.store.AddTimelineEvent(ev)
	return ev, nil
}

// GetTimeline returns the full timeline for an incident.
func (s *Service) GetTimeline(incidentID string) ([]models.TimelineEvent, error) {
	_, err := s.GetIncident(incidentID)
	if err != nil {
		return nil, err
	}

	events := s.store.GetTimeline(incidentID)
	return s.timeline.Build(events), nil
}

// ResolveIncident transitions an incident to resolved.
func (s *Service) ResolveIncident(id, author string) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if inc.Status == models.StatusResolved || inc.Status == models.StatusClosed {
		return nil, ErrIncidentResolved
	}

	now := time.Now()
	inc.Status = models.StatusResolved
	inc.ResolvedAt = &now
	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "resolved",
		Description: "Incident resolved",
		Author:      author,
	})

	return inc, nil
}

// CloseIncident transitions an incident to closed.
func (s *Service) CloseIncident(id, author string) (*models.Incident, error) {
	inc, err := s.GetIncident(id)
	if err != nil {
		return nil, err
	}

	if inc.Status == models.StatusClosed {
		return nil, ErrInvalidStatus
	}

	now := time.Now()
	inc.Status = models.StatusClosed
	inc.ClosedAt = &now
	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   now,
		Type:        "closed",
		Description: "Incident closed",
		Author:      author,
	})

	return inc, nil
}

// GeneratePostmortem generates a postmortem document for a resolved incident.
func (s *Service) GeneratePostmortem(incidentID string) (*models.Postmortem, error) {
	inc, err := s.GetIncident(incidentID)
	if err != nil {
		return nil, err
	}

	events := s.store.GetTimeline(incidentID)
	sortedEvents := s.timeline.Build(events)

	var mttd, mttr time.Duration
	if !inc.DetectedAt.IsZero() && inc.ResolvedAt != nil {
		mttr = inc.ResolvedAt.Sub(inc.DetectedAt)
	}

	pm := &models.Postmortem{
		IncidentID:  inc.ID,
		Title:       "Postmortem: " + inc.Title,
		Summary:     fmt.Sprintf("Incident %s occurred on %s with severity %s.", inc.ID, inc.DetectedAt.Format(time.RFC3339), inc.Severity),
		Impact:      fmt.Sprintf("Affected devices: %v", inc.DeviceIDs),
		RootCause:   "Pending root cause analysis",
		Timeline:    sortedEvents,
		MTTD:        mttd,
		MTTR:        mttr,
		GeneratedAt: time.Now(),
		LessonsLearned: []string{
			"Review monitoring coverage",
			"Validate alert thresholds",
		},
		ActionItems: []string{
			"Update runbooks",
			"Improve detection time",
		},
	}

	return pm, nil
}

// IngestAlert ingests an alert and auto-aggregates it into an incident.
// aggregationKeyTag 把"聚合键 → 事故"这层关系记在**事故自己身上**，而不是记在服务内存里。
//
// 为什么不用 map[groupingKey]incidentID：进程内 map 在重启后是空的（于是重启后第一条
// 同类告警又开一个新事故），多副本部署下每个副本各一份、互相看不见。放进 Tags 之后，
// 归并关系与事故同生命周期，SQL 后端下跨重启、跨副本都成立。
const aggregationKeyTag = "opsmesh.aggregation_key"

// findOpenIncidentByGroup 在未结束的事故里找同聚合键的那一个。
//
// resolved / closed 不参与归并：已结案的事故不该把复发的告警吞进去——那会让新一轮故障
// 看起来"早就在处理了"。
func (s *Service) findOpenIncidentByGroup(key string) *models.Incident {
	for _, inc := range s.store.Incidents() {
		if inc == nil {
			continue
		}
		if inc.Status == models.StatusResolved || inc.Status == models.StatusClosed {
			continue
		}
		if inc.Tags[aggregationKeyTag] == key {
			return inc
		}
	}
	return nil
}

func (s *Service) IngestAlert(alert *models.Alert) (*models.Incident, error) {
	if alert == nil {
		return nil, errors.New("alert is nil")
	}

	if alert.ID == "" {
		alert.ID = uuid.New().String()
	}
	if alert.Timestamp.IsZero() {
		alert.Timestamp = time.Now()
	}

	result := s.engine.Aggregate(alert)
	if !result.Matched {
		return nil, errors.New("alert did not match any aggregation rule")
	}

	// ⚠️ result.IncidentID 装的是**聚合键**（aggregate.go 拼的是 deviceID+"-"+ruleID），
	// 不是事故 ID。修前这里把它当 ID 直接 GetIncident，于是永远查不到 ⇒
	// **每条告警都新建一个事故**，"聚合"这件事从来没成立过（10 条同类告警 = 10 个事故），
	// 顺带让按告警数升级级别的条件永远只看得到 1 条。
	groupingKey := result.IncidentID
	inc := s.findOpenIncidentByGroup(groupingKey)
	if inc == nil {
		title := fmt.Sprintf("Incident for %s", alert.DeviceID)
		firedAt := alert.Timestamp
		created, err := s.CreateIncident(CreateIncidentInput{
			Title:       title,
			Description: alert.Message,
			Severity:    alert.Severity,
			DeviceIDs:   []string{alert.DeviceID},
			OccurredAt:  &firedAt, // 告警触发时刻即最早已知故障时刻，MTTD 由此起算
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create incident: %w", err)
		}
		inc = created
		if inc.Tags == nil {
			inc.Tags = make(map[string]string)
		}
		inc.Tags[aggregationKeyTag] = groupingKey
	}

	inc.AlertIDs = append(inc.AlertIDs, alert.ID)

	// 事故升级：按"已累计告警数 + 当前最高严重度"决定级别，**只升不降**。
	//
	// 修前 aggregate.ShouldEscalate 只有单元测试、全仓没有任何调用点，于是
	// "事故自动升级"这条对外能力实际不存在：无论后续进来多少条 critical 告警，
	// 事故级别永远停在第一条告警的级别上，值班按级别排序就会漏看真正在恶化的事故。
	maxSeverity := inc.Severity
	if aggregate.SeverityRank(alert.Severity) > aggregate.SeverityRank(maxSeverity) {
		maxSeverity = alert.Severity
	}
	target := aggregate.ShouldEscalate(len(inc.AlertIDs), maxSeverity)
	escalated := aggregate.SeverityRank(target) > aggregate.SeverityRank(inc.Severity)
	from := inc.Severity
	if escalated {
		inc.Severity = target
	}

	s.store.UpdateIncident(inc)

	_ = s.store.AddTimelineEvent(&models.TimelineEvent{
		ID:          uuid.New().String(),
		IncidentID:  inc.ID,
		Timestamp:   time.Now(),
		Type:        "alert_ingested",
		Description: "Alert ingested: " + alert.Message,
		Author:      "system",
	})
	if escalated {
		// 时间线单独记一条：升级是值班与复盘要看得到的事件，不是悄悄改字段。
		_ = s.store.AddTimelineEvent(&models.TimelineEvent{
			ID:         uuid.New().String(),
			IncidentID: inc.ID,
			Timestamp:  time.Now(),
			Type:       "escalated",
			Description: fmt.Sprintf("Severity escalated %s → %s（累计 %d 条告警，最高 %s）",
				from, target, len(inc.AlertIDs), maxSeverity),
			Author: "system",
		})
	}

	return inc, nil
}

// GetResponseMetrics calculates response metrics across all incidents.
func (s *Service) GetResponseMetrics() *models.ResponseMetrics {
	incidents := s.store.Incidents()

	metrics := &models.ResponseMetrics{
		TotalIncidents: len(incidents),
	}

	var totalMTTD, totalMTTR time.Duration
	var mttdCount, mttrCount int

	for _, inc := range incidents {
		switch inc.Status {
		case models.StatusDetected, models.StatusInvestigating, models.StatusMitigating:
			metrics.ActiveIncidents++
		case models.StatusResolved, models.StatusClosed:
			metrics.ResolvedIncidents++
		}

		if inc.ResolvedAt != nil {
			mttr := inc.ResolvedAt.Sub(inc.DetectedAt)
			if mttr > 0 {
				totalMTTR += mttr
				mttrCount++
			}
		}

		// MTTD = 检测时刻 - 故障发生时刻。OccurredAt 缺省（历史数据/未回填）时不
		// 计入统计——此前 totalMTTD 声明后从未累加、以 `_ =` 吞掉的正是这里。
		if inc.OccurredAt != nil {
			mttd := inc.DetectedAt.Sub(*inc.OccurredAt)
			if mttd > 0 {
				totalMTTD += mttd
				mttdCount++
			}
		}
	}

	if mttrCount > 0 {
		metrics.AvgMTTR = totalMTTR / time.Duration(mttrCount)
	}
	if mttdCount > 0 {
		metrics.AvgMTTD = totalMTTD / time.Duration(mttdCount)
	}

	return metrics
}
