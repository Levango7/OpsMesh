package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of PluginStore.
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

func ensureParseTime(dsn string) string {
	return dsn + "?parseTime=true"
}

func initSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS plugins (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			version VARCHAR(64),
			description TEXT,
			author VARCHAR(255),
			type VARCHAR(32),
			category VARCHAR(64),
			tags JSON,
			download_url VARCHAR(512),
			checksum VARCHAR(128),
			status VARCHAR(32) DEFAULT 'pending',
			installed TINYINT(1) DEFAULT 0,
			enabled TINYINT(1) DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_status (status),
			INDEX idx_type (type)
		)`,
		`CREATE TABLE IF NOT EXISTS plugin_versions (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			plugin_id VARCHAR(64) NOT NULL,
			version VARCHAR(64) NOT NULL,
			checksum VARCHAR(128),
			download_url VARCHAR(512),
			released_at DATETIME,
			changelog TEXT,
			INDEX idx_plugin (plugin_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) List() []*models.Plugin {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, name, version, description, author, type, category, tags, download_url, checksum, status, installed, enabled, created_at, updated_at FROM plugins`)
	if err != nil {
		log.Printf("[store] List 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.Plugin
	for rows.Next() {
		p := &models.Plugin{}
		var createdAt, updatedAt sql.NullTime
		var tags []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &p.Description, &p.Author, &p.Type, &p.Category, &tags, &p.DownloadURL, &p.Checksum, &p.Status, &p.Installed, &p.Enabled, &createdAt, &updatedAt); err != nil {
			continue
		}
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &p.Tags)
		}
		if createdAt.Valid {
			p.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			p.UpdatedAt = updatedAt.Time
		}
		out = append(out, p)
	}
	return out
}

func (m *MySQLStore) Get(id string) (*models.Plugin, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, name, version, description, author, type, category, tags, download_url, checksum, status, installed, enabled, created_at, updated_at FROM plugins WHERE id=?`, id)
	p := &models.Plugin{}
	var createdAt, updatedAt sql.NullTime
	var tags []byte
	if err := row.Scan(&p.ID, &p.Name, &p.Version, &p.Description, &p.Author, &p.Type, &p.Category, &tags, &p.DownloadURL, &p.Checksum, &p.Status, &p.Installed, &p.Enabled, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Get 查询失败: %v", err)
		}
		return nil, false
	}
	if len(tags) > 0 {
		_ = json.Unmarshal(tags, &p.Tags)
	}
	if createdAt.Valid {
		p.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		p.UpdatedAt = updatedAt.Time
	}
	return p, true
}

func (m *MySQLStore) Create(p *models.Plugin) *models.Plugin {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	tags, _ := json.Marshal(p.Tags)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO plugins (id, name, version, description, author, type, category, tags, download_url, checksum, status, installed, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), version=VALUES(version), description=VALUES(description),
		   author=VALUES(author), type=VALUES(type), category=VALUES(category), tags=VALUES(tags),
		   download_url=VALUES(download_url), checksum=VALUES(checksum), status=VALUES(status),
		   installed=VALUES(installed), enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		p.ID, p.Name, p.Version, p.Description, p.Author, p.Type, p.Category, tags,
		p.DownloadURL, p.Checksum, p.Status, p.Installed, p.Enabled, nullTime(p.CreatedAt), nullTime(p.UpdatedAt))
	if err != nil {
		log.Printf("[store] Create 失败: %v", err)
	}
	return p
}

func (m *MySQLStore) Update(p *models.Plugin) (*models.Plugin, bool) {
	if p == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tags, _ := json.Marshal(p.Tags)
	res, err := m.db.ExecContext(ctx,
		`UPDATE plugins SET name=?, version=?, description=?, author=?, type=?, category=?, tags=?, download_url=?, checksum=?, status=?, installed=?, enabled=?, updated_at=? WHERE id=?`,
		p.Name, p.Version, p.Description, p.Author, p.Type, p.Category, tags,
		p.DownloadURL, p.Checksum, p.Status, p.Installed, p.Enabled, time.Now().UTC(), p.ID)
	if err != nil {
		log.Printf("[store] Update 失败: %v", err)
		return nil, false
	}
	n, _ := res.RowsAffected()
	return p, n > 0
}

func (m *MySQLStore) Delete(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM plugins WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] Delete 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, _ = m.db.ExecContext(ctx, `DELETE FROM plugin_versions WHERE plugin_id=?`, id)
	}
	return n > 0
}

func (m *MySQLStore) AddVersion(v *models.PluginVersion) {
	if v == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if v.ReleasedAt.IsZero() {
		v.ReleasedAt = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO plugin_versions (plugin_id, version, checksum, download_url, released_at, changelog) VALUES (?, ?, ?, ?, ?, ?)`,
		v.PluginID, v.Version, v.Checksum, v.DownloadURL, nullTime(v.ReleasedAt), v.Changelog)
	if err != nil {
		log.Printf("[store] AddVersion 失败: %v", err)
	}
}

func (m *MySQLStore) Versions(pluginID string) []*models.PluginVersion {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT plugin_id, version, checksum, download_url, released_at, changelog FROM plugin_versions WHERE plugin_id=? ORDER BY released_at DESC`, pluginID)
	if err != nil {
		log.Printf("[store] Versions 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.PluginVersion
	for rows.Next() {
		v := &models.PluginVersion{}
		var releasedAt sql.NullTime
		if err := rows.Scan(&v.PluginID, &v.Version, &v.Checksum, &v.DownloadURL, &releasedAt, &v.Changelog); err != nil {
			continue
		}
		if releasedAt.Valid {
			v.ReleasedAt = releasedAt.Time
		}
		out = append(out, v)
	}
	return out
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
