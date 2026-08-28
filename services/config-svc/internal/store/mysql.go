package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of Store.
type MySQLStore struct {
	db            *sql.DB
	encryptionKey []byte
	maxHistory    int
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
	return &MySQLStore{db: db, encryptionKey: deriveKey("default-key"), maxHistory: 50}, nil
}

func ensureParseTime(dsn string) string {
	return dsn + "?parseTime=true"
}

func initSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS config_entries (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			key_name VARCHAR(255) NOT NULL,
			value TEXT,
			format VARCHAR(32),
			version INT DEFAULT 1,
			description TEXT,
			updated_by VARCHAR(64),
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE KEY uk_tenant_key (tenant_id, key_name),
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS config_history (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			key_name VARCHAR(255) NOT NULL,
			value TEXT,
			format VARCHAR(32),
			version INT,
			description TEXT,
			updated_by VARCHAR(64),
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant_key (tenant_id, key_name)
		)`,
		`CREATE TABLE IF NOT EXISTS config_secrets (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			key_name VARCHAR(255) NOT NULL,
			value TEXT,
			key_type VARCHAR(32),
			version INT DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE KEY uk_tenant_key (tenant_id, key_name),
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS notify_channels (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			type VARCHAR(32),
			config JSON,
			enabled TINYINT(1) DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS config_templates (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			content MEDIUMTEXT,
			variables JSON,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant (tenant_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// ===================== Config Operations =====================

func (s *MySQLStore) GetConfig(tenantID, key string) (*models.ConfigEntry, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, key_name, value, format, version, description, updated_by, created_at, updated_at FROM config_entries WHERE tenant_id=? AND key_name=?`, tenantID, key)
	e := &models.ConfigEntry{}
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&e.TenantID, &e.Key, &e.Value, &e.Format, &e.Version, &e.Description, &e.UpdatedBy, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetConfig 查询失败: %v", err)
		}
		return nil, false
	}
	if createdAt.Valid {
		e.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		e.UpdatedAt = updatedAt.Time
	}
	return e, true
}

func (s *MySQLStore) SetConfig(item *models.ConfigEntry) *models.ConfigEntry {
	if item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	existing, exists := s.getConfigInternal(ctx, item.TenantID, item.Key)
	if exists {
		item.Version = existing.Version + 1
		item.CreatedAt = existing.CreatedAt
		s.appendConfigHistory(ctx, existing)
	} else {
		item.Version = 1
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config_entries (tenant_id, key_name, value, format, version, description, updated_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value=VALUES(value), format=VALUES(format), version=VALUES(version),
		   description=VALUES(description), updated_by=VALUES(updated_by), updated_at=VALUES(updated_at)`,
		item.TenantID, item.Key, item.Value, item.Format, item.Version, item.Description, item.UpdatedBy, nullTime(item.CreatedAt), nullTime(item.UpdatedAt))
	if err != nil {
		log.Printf("[store] SetConfig 失败: %v", err)
	}
	return item
}

func (s *MySQLStore) getConfigInternal(ctx context.Context, tenantID, key string) (*models.ConfigEntry, bool) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, key_name, value, format, version, description, updated_by, created_at, updated_at FROM config_entries WHERE tenant_id=? AND key_name=?`, tenantID, key)
	e := &models.ConfigEntry{}
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&e.TenantID, &e.Key, &e.Value, &e.Format, &e.Version, &e.Description, &e.UpdatedBy, &createdAt, &updatedAt); err != nil {
		return nil, false
	}
	if createdAt.Valid {
		e.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		e.UpdatedAt = updatedAt.Time
	}
	return e, true
}

func (s *MySQLStore) appendConfigHistory(ctx context.Context, e *models.ConfigEntry) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config_history (tenant_id, key_name, value, format, version, description, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TenantID, e.Key, e.Value, e.Format, e.Version, e.Description, e.UpdatedBy, nullTime(e.CreatedAt), nullTime(e.UpdatedAt))
	if err != nil {
		log.Printf("[store] appendConfigHistory 失败: %v", err)
	}
}

func (s *MySQLStore) DeleteConfig(tenantID, key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM config_entries WHERE tenant_id=? AND key_name=?`, tenantID, key)
	if err != nil {
		log.Printf("[store] DeleteConfig 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *MySQLStore) ListConfigs(tenantID string) []*models.ConfigEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, key_name, value, format, version, description, updated_by, created_at, updated_at FROM config_entries`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY key_name ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListConfigs 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.ConfigEntry
	for rows.Next() {
		e := &models.ConfigEntry{}
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&e.TenantID, &e.Key, &e.Value, &e.Format, &e.Version, &e.Description, &e.UpdatedBy, &createdAt, &updatedAt); err != nil {
			continue
		}
		if createdAt.Valid {
			e.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			e.UpdatedAt = updatedAt.Time
		}
		out = append(out, e)
	}
	return out
}

func (s *MySQLStore) GetConfigHistory(tenantID, key string) []*models.ConfigEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, key_name, value, format, version, description, updated_by, created_at, updated_at FROM config_history WHERE tenant_id=? AND key_name=? ORDER BY version ASC`, tenantID, key)
	if err != nil {
		log.Printf("[store] GetConfigHistory 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.ConfigEntry
	for rows.Next() {
		e := &models.ConfigEntry{}
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&e.TenantID, &e.Key, &e.Value, &e.Format, &e.Version, &e.Description, &e.UpdatedBy, &createdAt, &updatedAt); err != nil {
			continue
		}
		if createdAt.Valid {
			e.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			e.UpdatedAt = updatedAt.Time
		}
		out = append(out, e)
	}
	return out
}

func (s *MySQLStore) RollbackConfig(tenantID, key string, version int) (*models.ConfigEntry, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	history := s.GetConfigHistory(tenantID, key)
	var target *models.ConfigEntry
	for _, h := range history {
		if h.Version == version {
			target = h
			break
		}
	}
	if target == nil {
		return nil, false
	}
	now := time.Now().UTC()
	newEntry := &models.ConfigEntry{
		TenantID:    target.TenantID,
		Key:         target.Key,
		Value:       target.Value,
		Format:      target.Format,
		Description: target.Description,
		CreatedAt:   target.CreatedAt,
		UpdatedAt:   now,
	}
	existing, exists := s.getConfigInternal(ctx, tenantID, key)
	if exists {
		newEntry.Version = existing.Version + 1
		s.appendConfigHistory(ctx, existing)
	} else {
		newEntry.Version = 1
	}
	s.SetConfig(newEntry)
	return newEntry, true
}

// ===================== Secret Operations =====================

func (s *MySQLStore) CreateSecret(item *models.SecretEntry) *models.SecretEntry {
	if item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	item.Version = 1
	item.CreatedAt = now
	item.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config_secrets (tenant_id, key_name, value, key_type, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value=VALUES(value), key_type=VALUES(key_type), version=VALUES(version)+1, updated_at=VALUES(updated_at)`,
		item.TenantID, item.Key, item.Value, item.KeyType, item.Version, nullTime(item.CreatedAt), nullTime(item.UpdatedAt))
	if err != nil {
		log.Printf("[store] CreateSecret 失败: %v", err)
	}
	return item
}

func (s *MySQLStore) GetSecret(tenantID, key string) (*models.SecretEntry, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, key_name, value, key_type, version, created_at, updated_at FROM config_secrets WHERE tenant_id=? AND key_name=?`, tenantID, key)
	e := &models.SecretEntry{}
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&e.TenantID, &e.Key, &e.Value, &e.KeyType, &e.Version, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetSecret 查询失败: %v", err)
		}
		return nil, false
	}
	if createdAt.Valid {
		e.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		e.UpdatedAt = updatedAt.Time
	}
	return e, true
}

func (s *MySQLStore) UpdateSecret(item *models.SecretEntry) *models.SecretEntry {
	if item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var existing models.SecretEntry
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, key_name, value, key_type, version, created_at, updated_at FROM config_secrets WHERE tenant_id=? AND key_name=?`, item.TenantID, item.Key,
	).Scan(&existing.TenantID, &existing.Key, &existing.Value, &existing.KeyType, &existing.Version, &createdAt, &updatedAt)
	if err != nil {
		log.Printf("[store] UpdateSecret 查询失败: %v", err)
		return nil
	}
	if createdAt.Valid {
		existing.CreatedAt = createdAt.Time
	}
	item.Version = existing.Version + 1
	item.CreatedAt = existing.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`UPDATE config_secrets SET value=?, key_type=?, version=?, updated_at=? WHERE tenant_id=? AND key_name=?`,
		item.Value, item.KeyType, item.Version, nullTime(item.UpdatedAt), item.TenantID, item.Key)
	if err != nil {
		log.Printf("[store] UpdateSecret 失败: %v", err)
	}
	return item
}

func (s *MySQLStore) DeleteSecret(tenantID, key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM config_secrets WHERE tenant_id=? AND key_name=?`, tenantID, key)
	if err != nil {
		log.Printf("[store] DeleteSecret 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *MySQLStore) ListSecrets(tenantID string) []*models.SecretMeta {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, key_name, key_type, version, created_at, updated_at FROM config_secrets`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY key_name ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListSecrets 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.SecretMeta
	for rows.Next() {
		m := &models.SecretMeta{}
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&m.TenantID, &m.Key, &m.KeyType, &m.Version, &createdAt, &updatedAt); err != nil {
			continue
		}
		if createdAt.Valid {
			m.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			m.UpdatedAt = updatedAt.Time
		}
		out = append(out, m)
	}
	return out
}

func (s *MySQLStore) RotateSecret(tenantID, key, newValue string) *models.SecretMeta {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var existing models.SecretEntry
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, key_name, value, key_type, version, created_at, updated_at FROM config_secrets WHERE tenant_id=? AND key_name=?`, tenantID, key,
	).Scan(&existing.TenantID, &existing.Key, &existing.Value, &existing.KeyType, &existing.Version, &createdAt, &updatedAt)
	if err != nil {
		log.Printf("[store] RotateSecret 查询失败: %v", err)
		return nil
	}
	newVersion := existing.Version + 1
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`UPDATE config_secrets SET value=?, version=?, updated_at=? WHERE tenant_id=? AND key_name=?`,
		newValue, newVersion, now, tenantID, key)
	if err != nil {
		log.Printf("[store] RotateSecret 失败: %v", err)
		return nil
	}
	if createdAt.Valid {
		existing.CreatedAt = createdAt.Time
	}
	return &models.SecretMeta{
		ID:        existing.ID,
		TenantID:  existing.TenantID,
		Key:       existing.Key,
		KeyType:   existing.KeyType,
		Version:   newVersion,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: now,
	}
}

// ===================== Channel Operations =====================

func (s *MySQLStore) CreateChannel(item *models.ChannelEntry) *models.ChannelEntry {
	if item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notify_channels (id, tenant_id, name, type, config, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), name=VALUES(name), type=VALUES(type),
		   config=VALUES(config), enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		item.ID, item.TenantID, item.Name, item.Type, nullString(item.Config), boolToInt(item.Enabled), nullTime(item.CreatedAt), nullTime(item.UpdatedAt))
	if err != nil {
		log.Printf("[store] CreateChannel 失败: %v", err)
	}
	return item
}

func (s *MySQLStore) GetChannel(id string) *models.ChannelEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, type, config, enabled, created_at, updated_at FROM notify_channels WHERE id=?`, id)
	c := &models.ChannelEntry{}
	var createdAt, updatedAt sql.NullTime
	var config sql.NullString
	if err := row.Scan(&c.ID, &c.TenantID, &c.Name, &c.Type, &config, &c.Enabled, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetChannel 查询失败: %v", err)
		}
		return nil
	}
	c.Config = config.String
	if createdAt.Valid {
		c.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		c.UpdatedAt = updatedAt.Time
	}
	return c
}

func (s *MySQLStore) UpdateChannel(item *models.ChannelEntry) bool {
	if item == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE notify_channels SET tenant_id=?, name=?, type=?, config=?, enabled=?, updated_at=? WHERE id=?`,
		item.TenantID, item.Name, item.Type, nullString(item.Config), boolToInt(item.Enabled), time.Now().UTC(), item.ID)
	if err != nil {
		log.Printf("[store] UpdateChannel 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *MySQLStore) DeleteChannel(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res sql.Result
	var err error
	if tenantID != "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM notify_channels WHERE id=? AND tenant_id=?`, id, tenantID)
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM notify_channels WHERE id=?`, id)
	}
	if err != nil {
		log.Printf("[store] DeleteChannel 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *MySQLStore) ListChannels(tenantID string) []*models.ChannelEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, name, type, config, enabled, created_at, updated_at FROM notify_channels`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY name ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListChannels 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.ChannelEntry
	for rows.Next() {
		c := &models.ChannelEntry{}
		var createdAt, updatedAt sql.NullTime
		var config sql.NullString
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Type, &config, &c.Enabled, &createdAt, &updatedAt); err != nil {
			continue
		}
		c.Config = config.String
		if createdAt.Valid {
			c.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			c.UpdatedAt = updatedAt.Time
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ===================== Template Operations =====================

func (s *MySQLStore) CreateTemplate(item *models.TemplateEntry) *models.TemplateEntry {
	if item == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	vars, _ := json.Marshal(item.Variables)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config_templates (id, tenant_id, name, description, content, variables, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), name=VALUES(name), description=VALUES(description),
		   content=VALUES(content), variables=VALUES(variables), updated_at=VALUES(updated_at)`,
		item.ID, item.TenantID, item.Name, item.Description, item.Content, vars, nullTime(item.CreatedAt), nullTime(item.UpdatedAt))
	if err != nil {
		log.Printf("[store] CreateTemplate 失败: %v", err)
	}
	return item
}

func (s *MySQLStore) GetTemplate(id string) *models.TemplateEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, description, content, variables, created_at, updated_at FROM config_templates WHERE id=?`, id)
	t := &models.TemplateEntry{}
	var createdAt, updatedAt sql.NullTime
	var vars []byte
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.Content, &vars, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetTemplate 查询失败: %v", err)
		}
		return nil
	}
	if len(vars) > 0 {
		_ = json.Unmarshal(vars, &t.Variables)
	}
	if createdAt.Valid {
		t.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		t.UpdatedAt = updatedAt.Time
	}
	return t
}

func (s *MySQLStore) UpdateTemplate(item *models.TemplateEntry) bool {
	if item == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vars, _ := json.Marshal(item.Variables)
	res, err := s.db.ExecContext(ctx,
		`UPDATE config_templates SET tenant_id=?, name=?, description=?, content=?, variables=?, updated_at=? WHERE id=?`,
		item.TenantID, item.Name, item.Description, item.Content, vars, time.Now().UTC(), item.ID)
	if err != nil {
		log.Printf("[store] UpdateTemplate 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *MySQLStore) DeleteTemplate(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res sql.Result
	var err error
	if tenantID != "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM config_templates WHERE id=? AND tenant_id=?`, id, tenantID)
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM config_templates WHERE id=?`, id)
	}
	if err != nil {
		log.Printf("[store] DeleteTemplate 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *MySQLStore) ListTemplates(tenantID string) []*models.TemplateEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, name, description, content, variables, created_at, updated_at FROM config_templates`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY name ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListTemplates 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.TemplateEntry
	for rows.Next() {
		t := &models.TemplateEntry{}
		var createdAt, updatedAt sql.NullTime
		var vars []byte
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.Content, &vars, &createdAt, &updatedAt); err != nil {
			continue
		}
		if len(vars) > 0 {
			_ = json.Unmarshal(vars, &t.Variables)
		}
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			t.UpdatedAt = updatedAt.Time
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
