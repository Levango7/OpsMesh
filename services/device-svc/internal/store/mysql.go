package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
)

// MySQLStore is a MySQL-backed implementation of all device-svc stores.
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore creates a new MySQLStore with the given DSN.
func NewMySQLStore(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping mysql: %w", err)
	}

	return &MySQLStore{db: db}, nil
}

// Close closes the database connection.
func (s *MySQLStore) Close() error {
	return s.db.Close()
}

// NewStore creates a Store based on configuration.
// If dbDSN is non-empty, it returns a MySQLStore; otherwise a MemoryStore.
func NewStore(dbDSN string) (*MySQLStore, error) {
	if dbDSN != "" {
		return NewMySQLStore(dbDSN)
	}
	return nil, nil
}

// jsonString marshals a string slice to JSON.
func jsonString(v []string) string {
	if v == nil {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// jsonMap marshals a map to JSON.
func jsonMap(v map[string]string) string {
	if v == nil {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// scanStringSlice scans a JSON string into a string slice.
func scanStringSlice(data []byte) []string {
	var s []string
	if len(data) == 0 {
		return nil
	}
	_ = json.Unmarshal(data, &s)
	return s
}

// scanStringMap scans a JSON string into a map.
func scanStringMap(data []byte) map[string]string {
	var m map[string]string
	if len(data) == 0 {
		return nil
	}
	_ = json.Unmarshal(data, &m)
	return m
}

// === DeviceStore implementation ===

// RegisterDevice registers a new device.
func (s *MySQLStore) RegisterDevice(d *models.Device) *models.Device {
	if d == nil {
		return nil
	}
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.Status == "" {
		d.Status = "online"
	}
	d.UpdatedAt = now

	_, err := s.db.Exec(
		"INSERT INTO devices (id, tenant_id, name, ip, mac, os, arch, status, agent_id, tags, labels, `group`, lastHeartbeat, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		d.ID, d.TenantID, d.Name, d.IP, d.MAC, d.OS, d.Arch, d.Status, d.AgentID,
		jsonString(d.Tags), jsonMap(d.Labels), d.Group, d.LastHeartbeat, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	return d
}

// Device returns a device by ID.
func (s *MySQLStore) Device(id string) *models.Device {
	row := s.db.QueryRow(
		"SELECT id, tenant_id, name, ip, mac, os, arch, status, agent_id, tags, labels, `group`, lastHeartbeat, created_at, updated_at FROM devices WHERE id = ?",
		id,
	)
	return s.scanDevice(row)
}

func (s *MySQLStore) scanDevice(row *sql.Row) *models.Device {
	var d models.Device
	var tags, labels sql.RawBytes
	err := row.Scan(&d.ID, &d.TenantID, &d.Name, &d.IP, &d.MAC, &d.OS, &d.Arch, &d.Status, &d.AgentID,
		&tags, &labels, &d.Group, &d.LastHeartbeat, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil
	}
	d.Tags = scanStringSlice(tags)
	d.Labels = scanStringMap(labels)
	return &d
}

// ListDevices returns devices with optional filtering.
func (s *MySQLStore) ListDevices(tenantID, status, group string, limit int) []*models.Device {
	query := "SELECT id, tenant_id, name, ip, mac, os, arch, status, agent_id, tags, labels, `group`, lastHeartbeat, created_at, updated_at FROM devices WHERE 1=1"
	args := []interface{}{}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if group != "" {
		query += " AND `group` = ?"
		args = append(args, group)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return s.scanDevices(rows)
}

func (s *MySQLStore) scanDevices(rows *sql.Rows) []*models.Device {
	var devices []*models.Device
	for rows.Next() {
		var d models.Device
		var tags, labels sql.RawBytes
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Name, &d.IP, &d.MAC, &d.OS, &d.Arch, &d.Status, &d.AgentID,
			&tags, &labels, &d.Group, &d.LastHeartbeat, &d.CreatedAt, &d.UpdatedAt); err != nil {
			continue
		}
		d.Tags = scanStringSlice(tags)
		d.Labels = scanStringMap(labels)
		devices = append(devices, &d)
	}
	return devices
}

// UpdateDevice updates an existing device.
func (s *MySQLStore) UpdateDevice(d *models.Device) (*models.Device, bool) {
	if d == nil {
		return nil, false
	}
	d.UpdatedAt = time.Now()
	res, err := s.db.Exec(
		"UPDATE devices SET tenant_id = ?, name = ?, ip = ?, mac = ?, os = ?, arch = ?, status = ?, agent_id = ?, tags = ?, labels = ?, `group` = ?, lastHeartbeat = ?, updated_at = ? WHERE id = ?",
		d.TenantID, d.Name, d.IP, d.MAC, d.OS, d.Arch, d.Status, d.AgentID,
		jsonString(d.Tags), jsonMap(d.Labels), d.Group, d.LastHeartbeat, d.UpdatedAt, d.ID,
	)
	if err != nil {
		return nil, false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false
	}
	return d, true
}

// DeleteDevice removes a device.
func (s *MySQLStore) DeleteDevice(id string) bool {
	res, err := s.db.Exec("DELETE FROM devices WHERE id = ?", id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// Heartbeat updates device heartbeat timestamp.
func (s *MySQLStore) Heartbeat(deviceID, status string) bool {
	now := time.Now()
	query := "UPDATE devices SET lastHeartbeat = ?, updated_at = ?"
	args := []interface{}{now, now}
	if status != "" {
		query += ", status = ?"
		args = append(args, status)
	}
	query += " WHERE id = ?"
	args = append(args, deviceID)

	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// GetDeviceStatus returns device status.
func (s *MySQLStore) GetDeviceStatus(deviceID string) *models.DeviceStatus {
	var d models.Device
	var status string
	var lastHeartbeat time.Time
	err := s.db.QueryRow(
		"SELECT id, status, lastHeartbeat FROM devices WHERE id = ?", deviceID,
	).Scan(&d.ID, &status, &lastHeartbeat)
	if err != nil {
		return nil
	}
	return &models.DeviceStatus{
		DeviceID:      d.ID,
		Status:        status,
		Reachable:     status == "online",
		LastHeartbeat: lastHeartbeat,
	}
}

// DevicesByAgent returns devices managed by a specific agent.
func (s *MySQLStore) DevicesByAgent(agentID string) []*models.Device {
	rows, err := s.db.Query(
		"SELECT id, tenant_id, name, ip, mac, os, arch, status, agent_id, tags, labels, `group`, lastHeartbeat, created_at, updated_at FROM devices WHERE agent_id = ?",
		agentID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return s.scanDevices(rows)
}

// === AgentStore implementation ===

// RegisterAgent registers a new agent.
func (s *MySQLStore) RegisterAgent(a *models.Agent) *models.Agent {
	if a == nil {
		return nil
	}
	now := time.Now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.Status == "" {
		a.Status = "online"
	}
	a.UpdatedAt = now

	_, err := s.db.Exec(
		"INSERT INTO agents (id, tenant_id, device_id, hostname, version, status, load_count, os, arch, addr, grpc_port, metrics_port, lastHeartbeat, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		a.ID, a.TenantID, a.DeviceID, a.Hostname, a.Version, a.Status, a.Load,
		a.OS, a.Arch, a.Addr, a.GRPCPort, a.MetricsPort, a.LastHeartbeat, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	return a
}

// Agent returns an agent by ID.
func (s *MySQLStore) Agent(id string) *models.Agent {
	row := s.db.QueryRow(
		"SELECT id, tenant_id, device_id, hostname, version, status, load_count, os, arch, addr, grpc_port, metrics_port, lastHeartbeat, created_at, updated_at FROM agents WHERE id = ?",
		id,
	)
	return s.scanAgent(row)
}

func (s *MySQLStore) scanAgent(row *sql.Row) *models.Agent {
	var a models.Agent
	err := row.Scan(&a.ID, &a.TenantID, &a.DeviceID, &a.Hostname, &a.Version, &a.Status,
		&a.Load, &a.OS, &a.Arch, &a.Addr, &a.GRPCPort, &a.MetricsPort, &a.LastHeartbeat, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil
	}
	return &a
}

// ListAgents returns agents with optional filtering.
func (s *MySQLStore) ListAgents(tenantID, status string, limit int) []*models.Agent {
	query := "SELECT id, tenant_id, device_id, hostname, version, status, load_count, os, arch, addr, grpc_port, metrics_port, lastHeartbeat, created_at, updated_at FROM agents WHERE 1=1"
	args := []interface{}{}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var agents []*models.Agent
	for rows.Next() {
		var a models.Agent
		if err := rows.Scan(&a.ID, &a.TenantID, &a.DeviceID, &a.Hostname, &a.Version, &a.Status,
			&a.Load, &a.OS, &a.Arch, &a.Addr, &a.GRPCPort, &a.MetricsPort, &a.LastHeartbeat, &a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
		}
		agents = append(agents, &a)
	}
	return agents
}

// UpdateAgentStatus updates agent status and load.
func (s *MySQLStore) UpdateAgentStatus(agentID, status string, load int) (*models.Agent, bool) {
	now := time.Now()
	res, err := s.db.Exec(
		"UPDATE agents SET status = ?, load_count = ?, lastHeartbeat = ?, updated_at = ? WHERE id = ?",
		status, load, now, now, agentID,
	)
	if err != nil {
		return nil, false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false
	}
	return s.Agent(agentID), true
}

// AgentHeartbeat updates agent heartbeat.
func (s *MySQLStore) AgentHeartbeat(agentID, status string, load int) bool {
	now := time.Now()
	query := "UPDATE agents SET lastHeartbeat = ?, load_count = ?, updated_at = ?"
	args := []interface{}{now, load, now}
	if status != "" {
		query += ", status = ?"
		args = append(args, status)
	}
	query += " WHERE id = ?"
	args = append(args, agentID)

	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// === CiStore implementation ===

// CreateCI creates a new CI.
func (s *MySQLStore) CreateCI(ci *models.CI) *models.CI {
	if ci == nil {
		return nil
	}
	now := time.Now()
	if ci.CreatedAt.IsZero() {
		ci.CreatedAt = now
	}
	if ci.Status == "" {
		ci.Status = "active"
	}
	ci.Version = 1
	ci.UpdatedAt = now

	_, err := s.db.Exec(
		"INSERT INTO ci_items (id, tenant_id, ci_type, name, status, attributes, source, agent_id, device_id, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		ci.ID, ci.TenantID, ci.CiType, ci.Name, ci.Status, jsonMap(ci.Attributes),
		ci.Source, ci.AgentID, ci.DeviceID, ci.Version, ci.CreatedAt, ci.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	return ci
}

// GetCI returns a CI by ID.
func (s *MySQLStore) GetCI(id, tenantID string) *models.CI {
	var row *sql.Row
	if tenantID != "" {
		row = s.db.QueryRow(
			"SELECT id, tenant_id, ci_type, name, status, attributes, source, agent_id, device_id, version, created_at, updated_at FROM ci_items WHERE id = ? AND tenant_id = ?",
			id, tenantID,
		)
	} else {
		row = s.db.QueryRow(
			"SELECT id, tenant_id, ci_type, name, status, attributes, source, agent_id, device_id, version, created_at, updated_at FROM ci_items WHERE id = ?",
			id,
		)
	}
	return s.scanCI(row)
}

func (s *MySQLStore) scanCI(row *sql.Row) *models.CI {
	var ci models.CI
	var attrs sql.RawBytes
	err := row.Scan(&ci.ID, &ci.TenantID, &ci.CiType, &ci.Name, &ci.Status, &attrs,
		&ci.Source, &ci.AgentID, &ci.DeviceID, &ci.Version, &ci.CreatedAt, &ci.UpdatedAt)
	if err != nil {
		return nil
	}
	ci.Attributes = scanStringMap(attrs)
	return &ci
}

// UpdateCI updates an existing CI.
func (s *MySQLStore) UpdateCI(ci *models.CI) (*models.CI, bool) {
	if ci == nil {
		return nil, false
	}

	var old models.CI
	var attrs sql.RawBytes
	err := s.db.QueryRow(
		"SELECT id, tenant_id, ci_type, name, status, attributes, source, agent_id, device_id, version, created_at, updated_at FROM ci_items WHERE id = ?",
		ci.ID,
	).Scan(&old.ID, &old.TenantID, &old.CiType, &old.Name, &old.Status, &attrs,
		&old.Source, &old.AgentID, &old.DeviceID, &old.Version, &old.CreatedAt, &old.UpdatedAt)
	if err != nil {
		return nil, false
	}

	ci.Version = old.Version + 1
	ci.CreatedAt = old.CreatedAt
	ci.UpdatedAt = time.Now()

	_, err = s.db.Exec(
		"UPDATE ci_items SET tenant_id = ?, ci_type = ?, name = ?, status = ?, attributes = ?, source = ?, agent_id = ?, device_id = ?, version = ?, updated_at = ? WHERE id = ?",
		ci.TenantID, ci.CiType, ci.Name, ci.Status, jsonMap(ci.Attributes),
		ci.Source, ci.AgentID, ci.DeviceID, ci.Version, ci.UpdatedAt, ci.ID,
	)
	if err != nil {
		return nil, false
	}
	return ci, true
}

// DeleteCI removes a CI.
func (s *MySQLStore) DeleteCI(id, tenantID string) bool {
	var res sql.Result
	var err error
	if tenantID != "" {
		res, err = s.db.Exec("DELETE FROM ci_items WHERE id = ? AND tenant_id = ?", id, tenantID)
	} else {
		res, err = s.db.Exec("DELETE FROM ci_items WHERE id = ?", id)
	}
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ListCIs returns CIs with optional filtering.
func (s *MySQLStore) ListCIs(tenantID, ciType, status string, limit int) []*models.CI {
	query := "SELECT id, tenant_id, ci_type, name, status, attributes, source, agent_id, device_id, version, created_at, updated_at FROM ci_items WHERE 1=1"
	args := []interface{}{}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	if ciType != "" {
		query += " AND ci_type = ?"
		args = append(args, ciType)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var cis []*models.CI
	for rows.Next() {
		var ci models.CI
		var attrs sql.RawBytes
		if err := rows.Scan(&ci.ID, &ci.TenantID, &ci.CiType, &ci.Name, &ci.Status, &attrs,
			&ci.Source, &ci.AgentID, &ci.DeviceID, &ci.Version, &ci.CreatedAt, &ci.UpdatedAt); err != nil {
			continue
		}
		ci.Attributes = scanStringMap(attrs)
		cis = append(cis, &ci)
	}
	return cis
}

// CreateRelation creates a CI relationship.
func (s *MySQLStore) CreateRelation(rel *models.CIRelation) *models.CIRelation {
	if rel == nil {
		return nil
	}
	rel.CreatedAt = time.Now()

	res, err := s.db.Exec(
		"INSERT INTO ci_relations (source_ci_id, target_ci_id, relation_type, tenant_id, attributes, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		rel.SourceCIID, rel.TargetCIID, rel.RelationType, rel.TenantID, jsonMap(rel.Attributes), rel.CreatedAt,
	)
	if err != nil {
		return nil
	}
	rel.ID, _ = res.LastInsertId()
	return rel
}

// GetCIRelations returns relations for a CI.
func (s *MySQLStore) GetCIRelations(ciID, tenantID string) []*models.CIRelation {
	query := "SELECT id, source_ci_id, target_ci_id, relation_type, tenant_id, attributes, created_at FROM ci_relations WHERE (source_ci_id = ? OR target_ci_id = ?)"
	args := []interface{}{ciID, ciID}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var rels []*models.CIRelation
	for rows.Next() {
		var rel models.CIRelation
		var attrs sql.RawBytes
		if err := rows.Scan(&rel.ID, &rel.SourceCIID, &rel.TargetCIID, &rel.RelationType, &rel.TenantID, &attrs, &rel.CreatedAt); err != nil {
			continue
		}
		rel.Attributes = scanStringMap(attrs)
		rels = append(rels, &rel)
	}
	return rels
}

// === DiscoveryStore implementation ===

// CreateJob creates a new discovery job.
func (s *MySQLStore) CreateJob(job *models.DiscoveryJob) *models.DiscoveryJob {
	if job == nil {
		return nil
	}
	if job.StartedAt.IsZero() {
		job.StartedAt = time.Now()
	}
	if job.Status == "" {
		job.Status = "pending"
	}

	_, err := s.db.Exec(
		"INSERT INTO discovery_jobs (id, tenant_id, cidr, status, total_hosts, scanned_hosts, found_devices, error_msg, started_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		job.ID, job.TenantID, job.CIDR, job.Status, job.TotalHosts, job.ScannedHosts, job.FoundDevices, job.Error, job.StartedAt, job.CompletedAt,
	)
	if err != nil {
		return nil
	}
	return job
}

// GetJob returns a discovery job by ID.
func (s *MySQLStore) GetJob(id string) *models.DiscoveryJob {
	var job models.DiscoveryJob
	err := s.db.QueryRow(
		"SELECT id, tenant_id, cidr, status, total_hosts, scanned_hosts, found_devices, error_msg, started_at, completed_at FROM discovery_jobs WHERE id = ?",
		id,
	).Scan(&job.ID, &job.TenantID, &job.CIDR, &job.Status, &job.TotalHosts, &job.ScannedHosts, &job.FoundDevices, &job.Error, &job.StartedAt, &job.CompletedAt)
	if err != nil {
		return nil
	}
	return &job
}

// ListJobs returns discovery jobs for a tenant.
func (s *MySQLStore) ListJobs(tenantID string) []*models.DiscoveryJob {
	query := "SELECT id, tenant_id, cidr, status, total_hosts, scanned_hosts, found_devices, error_msg, started_at, completed_at FROM discovery_jobs"
	args := []interface{}{}
	if tenantID != "" {
		query += " WHERE tenant_id = ?"
		args = append(args, tenantID)
	}
	query += " ORDER BY started_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var jobs []*models.DiscoveryJob
	for rows.Next() {
		var job models.DiscoveryJob
		if err := rows.Scan(&job.ID, &job.TenantID, &job.CIDR, &job.Status, &job.TotalHosts, &job.ScannedHosts, &job.FoundDevices, &job.Error, &job.StartedAt, &job.CompletedAt); err != nil {
			continue
		}
		jobs = append(jobs, &job)
	}
	return jobs
}

// UpdateJob updates a discovery job.
func (s *MySQLStore) UpdateJob(job *models.DiscoveryJob) (*models.DiscoveryJob, bool) {
	if job == nil {
		return nil, false
	}
	res, err := s.db.Exec(
		"UPDATE discovery_jobs SET tenant_id = ?, cidr = ?, status = ?, total_hosts = ?, scanned_hosts = ?, found_devices = ?, error_msg = ?, started_at = ?, completed_at = ? WHERE id = ?",
		job.TenantID, job.CIDR, job.Status, job.TotalHosts, job.ScannedHosts, job.FoundDevices, job.Error, job.StartedAt, job.CompletedAt, job.ID,
	)
	if err != nil {
		return nil, false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, false
	}
	return job, true
}
