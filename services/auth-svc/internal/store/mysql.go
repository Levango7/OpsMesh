package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
)

// Store is the interface for auth persistence.
type Store interface {
	GetUser(id string) *User
	GetUserByUsername(username string) *User
	ListUsers() []*User
	CreateUser(u *User) (*User, error)
	UpdateUser(u *User) error
	DeleteUser(id string) error
	ChangePassword(userID, newHash string) error
	GetRole(id string) *Role
	GetRoleByName(name string) *Role
	ListRoles() []*Role
	CreateRole(r *Role) (*Role, error)
	UpdateRole(r *Role) error
	DeleteRole(id string) error
	ListPermissions() []*Permission
	SaveRefreshToken(rt *RefreshToken)
	GetRefreshToken(tokenHash string) *RefreshToken
	DeleteRefreshToken(tokenHash string) bool
	ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool)
	BlacklistJTI(jti string, ttl time.Duration)
	IsBlacklisted(jti string) bool
	PurgeBlacklist()
	Close() error
}

// MySQLStore is a MySQL-backed implementation of Store.
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

	s := &MySQLStore{db: db}
	if err := s.seedDefaults(); err != nil {
		return nil, fmt.Errorf("failed to seed defaults: %w", err)
	}
	return s, nil
}

// seedDefaults seeds default permissions, admin role, and admin user if they don't exist.
func (s *MySQLStore) seedDefaults() error {
	perms := []Permission{
		{ID: "perm-1", Name: "user:read", Description: "Read users", Group: "user"},
		{ID: "perm-2", Name: "user:write", Description: "Create/update users", Group: "user"},
		{ID: "perm-3", Name: "user:delete", Description: "Delete users", Group: "user"},
		{ID: "perm-4", Name: "role:read", Description: "Read roles", Group: "role"},
		{ID: "perm-5", Name: "role:write", Description: "Create/update roles", Group: "role"},
		{ID: "perm-6", Name: "role:delete", Description: "Delete roles", Group: "role"},
		{ID: "perm-7", Name: "role:assign", Description: "Assign roles to users", Group: "role"},
		{ID: "perm-8", Name: "permission:read", Description: "Read permissions", Group: "permission"},
	}
	for _, p := range perms {
		_, err := s.db.Exec(
			"INSERT IGNORE INTO permissions (id, name, description, perm_group) VALUES (?, ?, ?, ?)",
			p.ID, p.Name, p.Description, p.Group,
		)
		if err != nil {
			return fmt.Errorf("seed permission %s: %w", p.Name, err)
		}
	}

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM roles WHERE name = ?", "admin").Scan(&count)
	if err != nil {
		return fmt.Errorf("check admin role: %w", err)
	}
	if count == 0 {
		_, err = s.db.Exec(
			"INSERT INTO roles (id, name, description, created_at) VALUES (?, ?, ?, ?)",
			"role-admin", "admin", "Administrator with full access", time.Now(),
		)
		if err != nil {
			return fmt.Errorf("seed admin role: %w", err)
		}
		rows, err := s.db.Query("SELECT name FROM permissions")
		if err != nil {
			return fmt.Errorf("query permissions for admin: %w", err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return fmt.Errorf("scan permission name: %w", err)
			}
			_, err = s.db.Exec(
				"INSERT INTO role_permissions (role_id, permission_name) VALUES (?, ?)",
				"role-admin", name,
			)
			if err != nil {
				rows.Close()
				return fmt.Errorf("seed role_permission: %w", err)
			}
		}
		rows.Close()
	}

	err = s.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", "admin").Scan(&count)
	if err != nil {
		return fmt.Errorf("check admin user: %w", err)
	}
	if count == 0 {
		hash, _ := auth.HashPassword("admin123")
		_, err = s.db.Exec(
			"INSERT INTO users (id, username, email, password_hash, status, created_at, must_change_password) VALUES (?, ?, ?, ?, ?, ?, ?)",
			"user-admin", "admin", "admin@opsmesh.io", hash, "active", time.Now(), true,
		)
		if err != nil {
			return fmt.Errorf("seed admin user: %w", err)
		}
		_, err = s.db.Exec(
			"INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
			"user-admin", "role-admin",
		)
		if err != nil {
			return fmt.Errorf("seed admin user_role: %w", err)
		}
	}

	return nil
}

// Close closes the database connection.
func (s *MySQLStore) Close() error {
	return s.db.Close()
}

// --- User operations ---

// GetUser returns a user by ID.
func (s *MySQLStore) GetUser(id string) *User {
	row := s.db.QueryRow(
		"SELECT id, username, email, password_hash, status, created_at, must_change_password FROM users WHERE id = ?",
		id,
	)
	return s.scanUser(row)
}

// GetUserByUsername returns a user by username.
func (s *MySQLStore) GetUserByUsername(username string) *User {
	row := s.db.QueryRow(
		"SELECT id, username, email, password_hash, status, created_at, must_change_password FROM users WHERE username = ?",
		username,
	)
	return s.scanUser(row)
}

func (s *MySQLStore) scanUser(row *sql.Row) *User {
	var u User
	var mustChange int
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.CreatedAt, &mustChange)
	if err != nil {
		return nil
	}
	u.MustChangePassword = mustChange != 0
	u.RoleIDs = s.getUserRoleIDs(u.ID)
	return &u
}

func (s *MySQLStore) getUserRoleIDs(userID string) []string {
	rows, err := s.db.Query("SELECT role_id FROM user_roles WHERE user_id = ?", userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// ListUsers returns all users.
func (s *MySQLStore) ListUsers() []*User {
	rows, err := s.db.Query("SELECT id, username, email, password_hash, status, created_at, must_change_password FROM users")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		var u User
		var mustChange int
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &u.CreatedAt, &mustChange); err != nil {
			continue
		}
		u.MustChangePassword = mustChange != 0
		u.RoleIDs = s.getUserRoleIDs(u.ID)
		users = append(users, &u)
	}
	return users
}

// CreateUser creates a user.
func (s *MySQLStore) CreateUser(u *User) (*User, error) {
	var existing int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", u.Username).Scan(&existing)
	if err != nil {
		return nil, fmt.Errorf("check username: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("username already exists")
	}
	if u.ID == "" {
		u.ID = "user-" + uuid.New().String()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	if u.Status == "" {
		u.Status = "active"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT INTO users (id, username, email, password_hash, status, created_at, must_change_password) VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.Username, u.Email, u.PasswordHash, u.Status, u.CreatedAt, u.MustChangePassword,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	for _, roleID := range u.RoleIDs {
		_, err = tx.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", u.ID, roleID)
		if err != nil {
			return nil, fmt.Errorf("insert user_role: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return u, nil
}

// UpdateUser updates a user.
func (s *MySQLStore) UpdateUser(u *User) error {
	var existing int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", u.ID).Scan(&existing)
	if err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if existing == 0 {
		return fmt.Errorf("user not found")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if u.Email != "" {
		_, err = tx.Exec("UPDATE users SET email = ? WHERE id = ?", u.Email, u.ID)
		if err != nil {
			return fmt.Errorf("update email: %w", err)
		}
	}
	if u.Status != "" {
		_, err = tx.Exec("UPDATE users SET status = ? WHERE id = ?", u.Status, u.ID)
		if err != nil {
			return fmt.Errorf("update status: %w", err)
		}
	}
	if u.RoleIDs != nil {
		_, err = tx.Exec("DELETE FROM user_roles WHERE user_id = ?", u.ID)
		if err != nil {
			return fmt.Errorf("delete user_roles: %w", err)
		}
		for _, roleID := range u.RoleIDs {
			_, err = tx.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", u.ID, roleID)
			if err != nil {
				return fmt.Errorf("insert user_role: %w", err)
			}
		}
	}

	return tx.Commit()
}

// DeleteUser deletes a user.
func (s *MySQLStore) DeleteUser(id string) error {
	var existing int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", id).Scan(&existing)
	if err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if existing == 0 {
		return fmt.Errorf("user not found")
	}
	_, err = s.db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

// ChangePassword changes a user's password.
func (s *MySQLStore) ChangePassword(userID, newHash string) error {
	res, err := s.db.Exec(
		"UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?",
		newHash, userID,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// --- Role operations ---

// GetRole returns a role by ID.
func (s *MySQLStore) GetRole(id string) *Role {
	row := s.db.QueryRow("SELECT id, name, description, created_at FROM roles WHERE id = ?", id)
	return s.scanRole(row)
}

// GetRoleByName returns a role by name.
func (s *MySQLStore) GetRoleByName(name string) *Role {
	row := s.db.QueryRow("SELECT id, name, description, created_at FROM roles WHERE name = ?", name)
	return s.scanRole(row)
}

func (s *MySQLStore) scanRole(row *sql.Row) *Role {
	var r Role
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.CreatedAt)
	if err != nil {
		return nil
	}
	r.Permissions = s.getRolePermissions(r.ID)
	return &r
}

func (s *MySQLStore) getRolePermissions(roleID string) []string {
	rows, err := s.db.Query("SELECT permission_name FROM role_permissions WHERE role_id = ?", roleID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			perms = append(perms, name)
		}
	}
	return perms
}

// ListRoles returns all roles.
func (s *MySQLStore) ListRoles() []*Role {
	rows, err := s.db.Query("SELECT id, name, description, created_at FROM roles")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var roles []*Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.CreatedAt); err != nil {
			continue
		}
		r.Permissions = s.getRolePermissions(r.ID)
		roles = append(roles, &r)
	}
	return roles
}

// CreateRole creates a role.
func (s *MySQLStore) CreateRole(r *Role) (*Role, error) {
	var existing int
	err := s.db.QueryRow("SELECT COUNT(*) FROM roles WHERE name = ?", r.Name).Scan(&existing)
	if err != nil {
		return nil, fmt.Errorf("check role name: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("role name already exists")
	}
	if r.ID == "" {
		r.ID = "role-" + uuid.New().String()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT INTO roles (id, name, description, created_at) VALUES (?, ?, ?, ?)",
		r.ID, r.Name, r.Description, r.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert role: %w", err)
	}

	for _, perm := range r.Permissions {
		_, err = tx.Exec("INSERT INTO role_permissions (role_id, permission_name) VALUES (?, ?)", r.ID, perm)
		if err != nil {
			return nil, fmt.Errorf("insert role_permission: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return r, nil
}

// UpdateRole updates a role.
func (s *MySQLStore) UpdateRole(r *Role) error {
	var existing int
	err := s.db.QueryRow("SELECT COUNT(*) FROM roles WHERE id = ?", r.ID).Scan(&existing)
	if err != nil {
		return fmt.Errorf("check role: %w", err)
	}
	if existing == 0 {
		return fmt.Errorf("role not found")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if r.Description != "" {
		_, err = tx.Exec("UPDATE roles SET description = ? WHERE id = ?", r.Description, r.ID)
		if err != nil {
			return fmt.Errorf("update description: %w", err)
		}
	}
	if r.Permissions != nil {
		_, err = tx.Exec("DELETE FROM role_permissions WHERE role_id = ?", r.ID)
		if err != nil {
			return fmt.Errorf("delete role_permissions: %w", err)
		}
		for _, perm := range r.Permissions {
			_, err = tx.Exec("INSERT INTO role_permissions (role_id, permission_name) VALUES (?, ?)", r.ID, perm)
			if err != nil {
				return fmt.Errorf("insert role_permission: %w", err)
			}
		}
	}

	return tx.Commit()
}

// DeleteRole deletes a role.
func (s *MySQLStore) DeleteRole(id string) error {
	var existing int
	err := s.db.QueryRow("SELECT COUNT(*) FROM roles WHERE id = ?", id).Scan(&existing)
	if err != nil {
		return fmt.Errorf("check role: %w", err)
	}
	if existing == 0 {
		return fmt.Errorf("role not found")
	}
	_, err = s.db.Exec("DELETE FROM roles WHERE id = ?", id)
	return err
}

// --- Permission operations ---

// ListPermissions returns all permissions.
func (s *MySQLStore) ListPermissions() []*Permission {
	rows, err := s.db.Query("SELECT id, name, description, perm_group FROM permissions")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var perms []*Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Group); err != nil {
			continue
		}
		perms = append(perms, &p)
	}
	return perms
}

// --- Refresh token operations ---

// SaveRefreshToken saves a refresh token.
func (s *MySQLStore) SaveRefreshToken(rt *RefreshToken) {
	_, _ = s.db.Exec(
		"INSERT INTO refresh_tokens (token_hash, user_id, tenant_id, device_fp, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE expires_at = ?, user_id = ?",
		rt.TokenHash, rt.UserID, rt.TenantID, rt.DeviceFP, rt.ExpiresAt, rt.CreatedAt,
		rt.ExpiresAt, rt.UserID,
	)
}

// GetRefreshToken returns a refresh token by hash.
func (s *MySQLStore) GetRefreshToken(tokenHash string) *RefreshToken {
	var rt RefreshToken
	err := s.db.QueryRow(
		"SELECT token_hash, user_id, tenant_id, device_fp, expires_at, created_at FROM refresh_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&rt.TokenHash, &rt.UserID, &rt.TenantID, &rt.DeviceFP, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil
	}
	return &rt
}

// DeleteRefreshToken deletes a refresh token.
func (s *MySQLStore) DeleteRefreshToken(tokenHash string) bool {
	res, err := s.db.Exec("DELETE FROM refresh_tokens WHERE token_hash = ?", tokenHash)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ConsumeRefreshToken atomically reads and deletes a refresh token.
func (s *MySQLStore) ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool) {
	rt := s.GetRefreshToken(tokenHash)
	if rt == nil {
		return nil, false
	}
	s.DeleteRefreshToken(tokenHash)
	return rt, true
}

// --- Blacklist operations ---

// BlacklistJTI blacklists a JWT JTI.
func (s *MySQLStore) BlacklistJTI(jti string, ttl time.Duration) {
	expiresAt := time.Now().Add(ttl)
	_, _ = s.db.Exec(
		"INSERT INTO jti_blacklist (jti, expires_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE expires_at = ?",
		jti, expiresAt, expiresAt,
	)
}

// IsBlacklisted checks if a JTI is blacklisted.
func (s *MySQLStore) IsBlacklisted(jti string) bool {
	var expiresAt time.Time
	err := s.db.QueryRow("SELECT expires_at FROM jti_blacklist WHERE jti = ?", jti).Scan(&expiresAt)
	if err != nil {
		return false
	}
	return time.Now().Before(expiresAt)
}

// PurgeBlacklist removes expired blacklist entries.
func (s *MySQLStore) PurgeBlacklist() {
	_, _ = s.db.Exec("DELETE FROM jti_blacklist WHERE expires_at < ?", time.Now())
}

// Ensure MemoryStore implements Store.
var _ Store = (*MemoryStore)(nil)

// Ensure MySQLStore implements Store.
var _ Store = (*MySQLStore)(nil)

// NewStore creates a Store based on configuration.
// If dbDSN is non-empty, it returns a MySQLStore; otherwise a MemoryStore.
func NewStore(dbDSN string) (Store, error) {
	if dbDSN != "" {
		return NewMySQLStore(dbDSN)
	}
	return NewMemoryStore(), nil
}

// jsonSlice is a helper for scanning string slices from JSON columns.
func jsonSlice(data []byte) []string {
	var s []string
	_ = json.Unmarshal(data, &s)
	return s
}
