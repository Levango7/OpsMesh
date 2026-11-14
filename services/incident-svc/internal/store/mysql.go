package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

// MySQLStore is a MySQL-backed implementation of IncidentStore.
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore creates a MySQLStore with connection pool.
func NewMySQLStore(dsn string) (*MySQLStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN")
	}
	db, err := sql.Open("mysql", ensureParseTime(dsn))
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		log.Printf("[store] mysql ping 失败（将延迟重连）: %v", err)
	}
	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &MySQLStore{db: db}, nil
}

// ensureParseTime 保证 DSN 带 parseTime=true（否则 DATETIME 列无法 Scan 进 time.Time），
// 且对「已带查询参数」的 DSN 幂等。
//
// 修复的线上缺陷（2026-09-25 实测）：原实现无条件在末尾追加 "?parseTime=true"。
// 而 compose 等生产清单给出的 DSN 本身已含 "?parseTime=true"，拼出
// "...?parseTime=true?parseTime=true"，驱动报 invalid bool value: true?parseTime=true，
// sql.Open 直接失败 → 服务【静默退回 memory store】（生产重启即丢数据：
// 实测 alert-svc / config-svc 运行在内存存储上，库内无任何表）。
func ensureParseTime(dsn string) string {
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true"
	}
	return dsn + "?parseTime=true"
}

func initSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS incidents (
			id VARCHAR(64) PRIMARY KEY,
			-- tenant_id：行级租户隔离键（D4 / issue #27 裁决②）；写入必钉，读取过滤
			tenant_id VARCHAR(64) NOT NULL DEFAULT '',
			title VARCHAR(255) NOT NULL,
			description TEXT,
			severity VARCHAR(16),
			status VARCHAR(32) DEFAULT 'detected',
			alert_ids JSON,
			device_ids JSON,
			assignee VARCHAR(64),
			tags JSON,
			occurred_at DATETIME,
			detected_at DATETIME,
			resolved_at DATETIME,
			closed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_status (status),
			INDEX idx_severity (severity),
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS timeline_events (
			id VARCHAR(64) PRIMARY KEY,
			incident_id VARCHAR(64) NOT NULL,
			timestamp DATETIME,
			type VARCHAR(64),
			description TEXT,
			author VARCHAR(64),
			INDEX idx_incident (incident_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return ensureIncidentColumns(db)
}

// ensureIncidentColumns 给**既有**表补上后加的列（occurred_at / tenant_id）。
//
// 背景（2026-10-01 复核出的部署路径依赖缺陷）：occurred_at（MTTD 起点）最初只加进了
// deploy/docker/scripts/init-mysql.sql，而本包 initSchema 的 CREATE TABLE IF NOT EXISTS
// 不含该列——于是在「服务自建表」的部署（K8s / 自备 MySQL，即本服务默认路径）里，
// 表根本没有 occurred_at，CreateIncident 的 INSERT 每次都以 Unknown column 失败；
// 而已有库上 IF NOT EXISTS 是 no-op，永远不会补列。tenant_id（D4 / issue #27 裁决②）
// 是同一形态的第二次出现：存量库若不会补列，升级后 CreateIncident/GetIncident 立即
// Unknown column 'tenant_id'。
//
// MySQL 8 无 ADD COLUMN IF NOT EXISTS，故先查 information_schema 再决定是否 ALTER
// （幂等：重复启动不会报 Duplicate column）。失败即返回错误——由 NewMySQLStore 向上抛，
// cmd 侧 fail-fast 阻断启动：声明了持久化却写不进去，比启动失败更难发现。
func ensureIncidentColumns(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// 两列各自独立判存在：任一列已存在都不能让另一列被跳过（早退会漏补）。
	if err := ensureColumn(ctx, db, "incidents", "occurred_at",
		`ALTER TABLE incidents ADD COLUMN occurred_at DATETIME NULL COMMENT '最早已知故障发生时刻（MTTD 起点，可空=未知不计入）'`); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "incidents", "tenant_id",
		`ALTER TABLE incidents ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '行级租户隔离键（D4 / issue #27 裁决②）'`); err != nil {
		return err
	}
	return nil
}

// ensureColumn 在列不存在时执行给定 DDL 补列；已存在则什么都不做（MySQL 8 无 ADD COLUMN IF NOT EXISTS）。
// 表名/列名只进查询条件，DDL 由调用方以字面量提供（不接受外部输入拼接）。
func ensureColumn(ctx context.Context, db *sql.DB, table, column, ddl string) error {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&n); err != nil {
		return fmt.Errorf("检查 %s.%s: %w", table, column, err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("补列 %s.%s: %w", table, column, err)
	}
	log.Printf("[store] %s 表已补列 %s（既有库迁移）", table, column)
	return nil
}

// CreateIncident 写入事件单。租户必须钉住（空租户拒绝写入，D4）。
// tenant_id 参与 INSERT，但**不参与** ON DUPLICATE KEY UPDATE——归属不允许被后续写覆盖。
func (m *MySQLStore) CreateIncident(inc *models.Incident) *models.Incident {
	if inc == nil || inc.TenantID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now().UTC()
	}
	inc.UpdatedAt = time.Now().UTC()
	alertIDs, _ := json.Marshal(inc.AlertIDs)
	deviceIDs, _ := json.Marshal(inc.DeviceIDs)
	tags, _ := json.Marshal(inc.Tags)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO incidents (id, tenant_id, title, description, severity, status, alert_ids, device_ids, assignee, tags, occurred_at, detected_at, resolved_at, closed_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE title=VALUES(title), description=VALUES(description), severity=VALUES(severity),
		   status=VALUES(status), alert_ids=VALUES(alert_ids), device_ids=VALUES(device_ids),
		   assignee=VALUES(assignee), tags=VALUES(tags), occurred_at=VALUES(occurred_at), detected_at=VALUES(detected_at),
		   resolved_at=VALUES(resolved_at), closed_at=VALUES(closed_at), updated_at=VALUES(updated_at)`,
		inc.ID, inc.TenantID, inc.Title, inc.Description, inc.Severity, inc.Status, alertIDs, deviceIDs, inc.Assignee,
		tags, nullTimePtr(inc.OccurredAt), nullTime(inc.DetectedAt), nullTimePtr(inc.ResolvedAt), nullTimePtr(inc.ClosedAt), nullTime(inc.CreatedAt), nullTime(inc.UpdatedAt))
	if err != nil {
		log.Printf("[store] CreateIncident 失败: %v", err)
	}
	return inc
}

// GetIncident 按 id + 租户取单；跨租户与不存在同样返回 nil（不泄露"该单存在"）。
func (m *MySQLStore) GetIncident(id, tenantID string) *models.Incident {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, title, description, severity, status, alert_ids, device_ids, assignee, tags, occurred_at, detected_at, resolved_at, closed_at, created_at, updated_at FROM incidents WHERE id=? AND tenant_id=?`, id, tenantID)
	inc := &models.Incident{}
	var alertIDs, deviceIDs, tags []byte
	var description, severity, assignee sql.NullString
	var occurredAt, detectedAt, resolvedAt, closedAt, createdAt, updatedAt sql.NullTime
	if err := row.Scan(&inc.ID, &inc.TenantID, &inc.Title, &description, &severity, &inc.Status, &alertIDs, &deviceIDs, &assignee, &tags, &occurredAt, &detectedAt, &resolvedAt, &closedAt, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetIncident 查询失败: %v", err)
		}
		return nil
	}
	inc.Description, inc.Assignee = description.String, assignee.String
	inc.Severity = models.Severity(severity.String)
	if len(alertIDs) > 0 {
		if err := json.Unmarshal(alertIDs, &inc.AlertIDs); err != nil {
			log.Printf("[store] scan Unmarshal: %v", err)
		}
	}
	if len(deviceIDs) > 0 {
		if err := json.Unmarshal(deviceIDs, &inc.DeviceIDs); err != nil {
			log.Printf("[store] scan Unmarshal: %v", err)
		}
	}
	if len(tags) > 0 {
		if err := json.Unmarshal(tags, &inc.Tags); err != nil {
			log.Printf("[store] scan Unmarshal: %v", err)
		}
	}
	if detectedAt.Valid {
		inc.DetectedAt = detectedAt.Time
	}
	if occurredAt.Valid {
		t := occurredAt.Time
		inc.OccurredAt = &t
	}
	if resolvedAt.Valid {
		inc.ResolvedAt = &resolvedAt.Time
	}
	if closedAt.Valid {
		inc.ClosedAt = &closedAt.Time
	}
	if createdAt.Valid {
		inc.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		inc.UpdatedAt = updatedAt.Time
	}
	return inc
}

// UpdateIncident 跨租户不可改；SET 里**不含** tenant_id（归属变更=越权改挂）。
func (m *MySQLStore) UpdateIncident(inc *models.Incident, tenantID string) *models.Incident {
	if inc == nil || tenantID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inc.UpdatedAt = time.Now().UTC()
	alertIDs, _ := json.Marshal(inc.AlertIDs)
	deviceIDs, _ := json.Marshal(inc.DeviceIDs)
	tags, _ := json.Marshal(inc.Tags)
	_, err := m.db.ExecContext(ctx,
		`UPDATE incidents SET title=?, description=?, severity=?, status=?, alert_ids=?, device_ids=?, assignee=?, tags=?, detected_at=?, resolved_at=?, closed_at=?, updated_at=? WHERE id=? AND tenant_id=?`,
		inc.Title, inc.Description, inc.Severity, inc.Status, alertIDs, deviceIDs, inc.Assignee,
		tags, nullTime(inc.DetectedAt), nullTimePtr(inc.ResolvedAt), nullTimePtr(inc.ClosedAt), nullTime(inc.UpdatedAt), inc.ID, tenantID)
	if err != nil {
		log.Printf("[store] UpdateIncident 失败: %v", err)
	}
	return inc
}

// DeleteIncident 跨租户删不掉（WHERE 双条件；RowsAffected=0 即"不是你的或不存在"）。
func (m *MySQLStore) DeleteIncident(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM incidents WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteIncident 失败: %v", err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] RowsAffected: %v", err)
	}
	if n > 0 {
		if _, err := m.db.ExecContext(ctx, `DELETE FROM timeline_events WHERE incident_id=?`, id); err != nil {
			log.Printf("[store] best-effort exec: %v", err)
		}
	}
	return n > 0
}

// ListIncidents 先按租户收窄，再叠加 status/severity 过滤。
func (m *MySQLStore) ListIncidents(status string, severity models.Severity, tenantID string) []*models.Incident {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, title, description, severity, status, alert_ids, device_ids, assignee, tags, occurred_at, detected_at, resolved_at, closed_at, created_at, updated_at FROM incidents`
	args := []interface{}{tenantID}
	where := []string{"tenant_id=?"}
	if status != "" {
		where = append(where, "status=?")
		args = append(args, status)
	}
	if severity != "" {
		where = append(where, "severity=?")
		args = append(args, severity)
	}
	if len(where) > 0 {
		q += " WHERE " + joinWhere(where)
	}
	q += " ORDER BY created_at DESC"
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListIncidents 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.Incident
	for rows.Next() {
		inc := &models.Incident{}
		var alertIDs, deviceIDs, tags []byte
		var description, severity, assignee sql.NullString
		var occurredAt, detectedAt, resolvedAt, closedAt, createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&inc.ID, &inc.TenantID, &inc.Title, &description, &severity, &inc.Status, &alertIDs, &deviceIDs, &assignee, &tags, &occurredAt, &detectedAt, &resolvedAt, &closedAt, &createdAt, &updatedAt); err != nil {
			continue
		}
		inc.Description, inc.Assignee = description.String, assignee.String
		inc.Severity = models.Severity(severity.String)
		if len(alertIDs) > 0 {
			if err := json.Unmarshal(alertIDs, &inc.AlertIDs); err != nil {
				log.Printf("[store] scan Unmarshal: %v", err)
			}
		}
		if len(deviceIDs) > 0 {
			if err := json.Unmarshal(deviceIDs, &inc.DeviceIDs); err != nil {
				log.Printf("[store] scan Unmarshal: %v", err)
			}
		}
		if len(tags) > 0 {
			if err := json.Unmarshal(tags, &inc.Tags); err != nil {
				log.Printf("[store] scan Unmarshal: %v", err)
			}
		}
		if detectedAt.Valid {
			inc.DetectedAt = detectedAt.Time
		}
		if occurredAt.Valid {
			t := occurredAt.Time
			inc.OccurredAt = &t
		}
		if resolvedAt.Valid {
			inc.ResolvedAt = &resolvedAt.Time
		}
		if closedAt.Valid {
			inc.ClosedAt = &closedAt.Time
		}
		if createdAt.Valid {
			inc.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			inc.UpdatedAt = updatedAt.Time
		}
		out = append(out, inc)
	}
	return out
}

// AddTimelineEvent 归属由**父事件单**决定：父单不属于该租户 ⇒ 拒收（拿不到别人的单的写入权）。
func (m *MySQLStore) AddTimelineEvent(ev *models.TimelineEvent, tenantID string) *models.TimelineEvent {
	if ev == nil {
		return nil
	}
	if m.GetIncident(ev.IncidentID, tenantID) == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO timeline_events (id, incident_id, timestamp, type, description, author) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.IncidentID, nullTime(ev.Timestamp), ev.Type, ev.Description, ev.Author)
	if err != nil {
		log.Printf("[store] AddTimelineEvent 失败: %v", err)
	}
	return ev
}

// GetTimeline 先校验父单归属；不属于该租户 ⇒ 返回 nil（不泄露「该单存在」）。
func (m *MySQLStore) GetTimeline(incidentID, tenantID string) []models.TimelineEvent {
	if m.GetIncident(incidentID, tenantID) == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, incident_id, timestamp, type, description, author FROM timeline_events WHERE incident_id=? ORDER BY timestamp ASC`, incidentID)
	if err != nil {
		log.Printf("[store] GetTimeline 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []models.TimelineEvent
	for rows.Next() {
		ev := models.TimelineEvent{}
		var ts sql.NullTime
		var evType, evDesc, author sql.NullString
		if err := rows.Scan(&ev.ID, &ev.IncidentID, &ts, &evType, &evDesc, &author); err != nil {
			continue
		}
		ev.Type, ev.Description, ev.Author = evType.String, evDesc.String, author.String
		if ts.Valid {
			ev.Timestamp = ts.Time
		}
		out = append(out, ev)
	}
	return out
}

// Incidents 只返回该租户的事件单。
func (m *MySQLStore) Incidents(tenantID string) []*models.Incident {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, tenant_id, title, description, severity, status, alert_ids, device_ids, assignee, tags, occurred_at, detected_at, resolved_at, closed_at, created_at, updated_at FROM incidents WHERE tenant_id=?`, tenantID)
	if err != nil {
		log.Printf("[store] Incidents 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.Incident
	for rows.Next() {
		inc := &models.Incident{}
		var alertIDs, deviceIDs, tags []byte
		var description, severity, assignee sql.NullString
		var occurredAt, detectedAt, resolvedAt, closedAt, createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&inc.ID, &inc.TenantID, &inc.Title, &description, &severity, &inc.Status, &alertIDs, &deviceIDs, &assignee, &tags, &occurredAt, &detectedAt, &resolvedAt, &closedAt, &createdAt, &updatedAt); err != nil {
			continue
		}
		inc.Description, inc.Assignee = description.String, assignee.String
		inc.Severity = models.Severity(severity.String)
		if len(alertIDs) > 0 {
			if err := json.Unmarshal(alertIDs, &inc.AlertIDs); err != nil {
				log.Printf("[store] scan Unmarshal: %v", err)
			}
		}
		if len(deviceIDs) > 0 {
			if err := json.Unmarshal(deviceIDs, &inc.DeviceIDs); err != nil {
				log.Printf("[store] scan Unmarshal: %v", err)
			}
		}
		if len(tags) > 0 {
			if err := json.Unmarshal(tags, &inc.Tags); err != nil {
				log.Printf("[store] scan Unmarshal: %v", err)
			}
		}
		if detectedAt.Valid {
			inc.DetectedAt = detectedAt.Time
		}
		if occurredAt.Valid {
			t := occurredAt.Time
			inc.OccurredAt = &t
		}
		if resolvedAt.Valid {
			inc.ResolvedAt = &resolvedAt.Time
		}
		if closedAt.Valid {
			inc.ClosedAt = &closedAt.Time
		}
		if createdAt.Valid {
			inc.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			inc.UpdatedAt = updatedAt.Time
		}
		out = append(out, inc)
	}
	return out
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullTimePtr(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}

func joinWhere(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

// 编译期断言：MySQLStore 实现 models.IncidentStore 接口。
var _ models.IncidentStore = (*MySQLStore)(nil)
