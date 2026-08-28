// Package migrate provides database schema migration management for OpsMesh.
//
// Migrations are SQL files stored in a directory, named with a numeric prefix
// followed by an underscore and a description (e.g., 001_initial.sql). Each
// migration file may contain multiple SQL statements separated by semicolons.
//
// The package tracks applied migrations in a _migrations table and supports
// forward migration to the latest version and rollback to a specific version.
//
// Design principles:
//   - Idempotent: RunMigrations can be called multiple times safely.
//   - Transactional: Each migration runs in its own transaction.
//   - Versioned: Schema version is tracked in the _migrations table.
//   - SQL-only: No ORM dependency; plain SQL migrations.
package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// migration represents a single SQL migration file.
type migration struct {
	Version int
	Name    string
	Path    string
}

//Migrator handles database schema migrations.
type Migrator struct {
	db        *sql.DB
	schemaDir string
	tableName string
}

// NewMigrator creates a new Migrator for the given database and schema directory.
func NewMigrator(db *sql.DB, schemaDir string) *Migrator {
	return &Migrator{
		db:        db,
		schemaDir: schemaDir,
		tableName: "_migrations",
	}
}

// RunMigrations reads SQL migration files from schemaDir and applies any
// that have not yet been applied, in ascending version order.
//
// Each migration file must be named "<version>_<description>.sql" where
// <version> is a zero-padded integer (e.g., "001_initial.sql").
//
// Migrations are applied sequentially. If any migration fails, the process
// stops and the error is returned. Already-applied migrations are skipped.
func (m *Migrator) RunMigrations() error {
	if m.db == nil {
		return fmt.Errorf("migrate: database connection is nil")
	}

	if err := m.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("migrate: failed to create migrations table: %w", err)
	}

	files, err := m.loadMigrationFiles()
	if err != nil {
		return err
	}

	currentVersion, err := m.GetVersion()
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.Version <= currentVersion {
			continue
		}
		if err := m.applyMigration(f); err != nil {
			return fmt.Errorf("migrate: failed to apply migration %d (%s): %w", f.Version, f.Name, err)
		}
	}

	return nil
}

// Rollback reverts migrations down to and including the specified version.
//
// Rollback files should be named "<version>_<description>.down.sql".
// If no down file exists for a migration, an error is returned.
//
// Rollback is applied in descending version order (newest first).
func (m *Migrator) Rollback(targetVersion int) error {
	if m.db == nil {
		return fmt.Errorf("migrate: database connection is nil")
	}

	currentVersion, err := m.GetVersion()
	if err != nil {
		return err
	}

	if targetVersion >= currentVersion {
		return nil
	}

	downFiles, err := m.loadDownMigrationFiles()
	if err != nil {
		return err
	}

	for v := currentVersion; v > targetVersion; v-- {
		downFile, ok := downFiles[v]
		if !ok {
			return fmt.Errorf("migrate: no rollback file for version %d", v)
		}
		if err := m.applyMigration(migration{
			Version: v,
			Name:    downFile.name,
			Path:    downFile.path,
		}); err != nil {
			return fmt.Errorf("migrate: failed to rollback migration %d: %w", v, err)
		}
	}

	return nil
}

// GetVersion returns the current schema version (highest applied migration).
// Returns 0 if no migrations have been applied.
func (m *Migrator) GetVersion() (int, error) {
	if m.db == nil {
		return 0, fmt.Errorf("migrate: database connection is nil")
	}

	var version int
	err := m.db.QueryRow(
		fmt.Sprintf(`SELECT COALESCE(MAX(version), 0) FROM %s`, m.tableName),
	).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("migrate: failed to query schema version: %w", err)
	}
	return version, nil
}

// GetMigrationHistory returns all applied migrations ordered by version.
func (m *Migrator) GetMigrationHistory() ([]MigrationRecord, error) {
	if m.db == nil {
		return nil, fmt.Errorf("migrate: database connection is nil")
	}

	rows, err := m.db.Query(fmt.Sprintf(
		`SELECT version, name, applied_at FROM %s ORDER BY version ASC`, m.tableName,
	))
	if err != nil {
		return nil, fmt.Errorf("migrate: failed to query migration history: %w", err)
	}
	defer rows.Close()

	var records []MigrationRecord
	for rows.Next() {
		var r MigrationRecord
		if err := rows.Scan(&r.Version, &r.Name, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("migrate: failed to scan migration record: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// MigrationRecord represents an applied migration entry in the _migrations table.
type MigrationRecord struct {
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	AppliedAt time.Time `json:"appliedAt"`
}

// ensureMigrationsTable creates the _migrations tracking table if it does not exist.
func (m *Migrator) ensureMigrationsTable() error {
	_, err := m.db.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
			version INT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, m.tableName,
	))
	return err
}

// loadMigrationFiles reads and parses .sql migration files from schemaDir.
func (m *Migrator) loadMigrationFiles() ([]migration, error) {
	entries, err := os.ReadDir(m.schemaDir)
	if err != nil {
		return nil, fmt.Errorf("migrate: failed to read schema directory %s: %w", m.schemaDir, err)
	}

	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".down.sql") {
			continue
		}
		v, desc, err := parseMigrationFilename(name)
		if err != nil {
			continue
		}
		migrations = append(migrations, migration{
			Version: v,
			Name:    desc,
			Path:    filepath.Join(m.schemaDir, name),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

// downMigration holds a rollback file path.
type downMigration struct {
	name string
	path string
}

// loadDownMigrationFiles reads and parses .down.sql rollback files.
func (m *Migrator) loadDownMigrationFiles() (map[int]downMigration, error) {
	entries, err := os.ReadDir(m.schemaDir)
	if err != nil {
		return nil, fmt.Errorf("migrate: failed to read schema directory %s: %w", m.schemaDir, err)
	}

	downFiles := make(map[int]downMigration)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".down.sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".down.sql")
		v, _, err := parseMigrationFilename(base + ".sql")
		if err != nil {
			continue
		}
		downFiles[v] = downMigration{
			name: base,
			path: filepath.Join(m.schemaDir, name),
		}
	}
	return downFiles, nil
}

// applyMigration executes a single migration file and records it.
func (m *Migrator) applyMigration(mg migration) error {
	content, err := os.ReadFile(mg.Path)
	if err != nil {
		return fmt.Errorf("failed to read migration file %s: %w", mg.Path, err)
	}

	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	sql := string(content)
	if _, err := tx.Exec(sql); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	if mg.Version > 0 {
		_, err := tx.Exec(
			fmt.Sprintf(`INSERT INTO %s (version, name, applied_at) VALUES (?, ?, ?)`, m.tableName),
			mg.Version, mg.Name, time.Now().UTC(),
		)
		if err != nil {
			return fmt.Errorf("failed to record migration: %w", err)
		}
	} else {
		_, err := tx.Exec(
			fmt.Sprintf(`DELETE FROM %s WHERE version = ?`, m.tableName),
			-mg.Version,
		)
		if err != nil {
			return fmt.Errorf("failed to record rollback: %w", err)
		}
	}

	return tx.Commit()
}

// parseMigrationFilename parses a migration filename into version and description.
// Expected format: "<version>_<description>.sql"
func parseMigrationFilename(filename string) (int, string, error) {
	base := strings.TrimSuffix(filename, ".sql")
	idx := strings.IndexByte(base, '_')
	if idx < 0 {
		return 0, "", fmt.Errorf("invalid migration filename: %s", filename)
	}
	version, err := strconv.Atoi(base[:idx])
	if err != nil {
		return 0, "", fmt.Errorf("invalid version in migration filename %s: %w", filename, err)
	}
	desc := base[idx+1:]
	return version, desc, nil
}
