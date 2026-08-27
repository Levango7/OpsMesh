package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
)

// User represents a user in the store.
type User struct {
	ID                 string
	Username           string
	Email              string
	PasswordHash       string
	Status             string
	RoleIDs            []string
	CreatedAt          time.Time
	MustChangePassword bool
}

// Role represents a role in the store.
type Role struct {
	ID          string
	Name        string
	Description string
	Permissions []string
	CreatedAt   time.Time
}

// Permission represents a permission in the store.
type Permission struct {
	ID          string
	Name        string
	Description string
	Group       string
}

// RefreshToken represents a refresh token in the store.
type RefreshToken struct {
	TokenHash string
	UserID    string
	TenantID  string
	DeviceFP  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// MemoryStore is an in-memory implementation of the auth store.
type MemoryStore struct {
	mu           sync.RWMutex
	users        map[string]*User
	usersByName  map[string]string
	roles        map[string]*Role
	rolesByName  map[string]string
	permissions  map[string]*Permission
	refreshTokens map[string]*RefreshToken
	blacklist    map[string]time.Time
}

// NewMemoryStore creates a new MemoryStore with default permissions and admin user.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		users:         make(map[string]*User),
		usersByName:   make(map[string]string),
		roles:         make(map[string]*Role),
		rolesByName:   make(map[string]string),
		permissions:   make(map[string]*Permission),
		refreshTokens: make(map[string]*RefreshToken),
		blacklist:     make(map[string]time.Time),
	}
	s.seedPermissions()
	s.seedAdminRole()
	s.seedAdminUser()
	return s
}

// seedPermissions seeds the default permissions.
func (s *MemoryStore) seedPermissions() {
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
	for i := range perms {
		s.permissions[perms[i].ID] = &perms[i]
	}
}

// seedAdminRole seeds the admin role with all permissions.
func (s *MemoryStore) seedAdminRole() {
	perms := make([]string, 0, len(s.permissions))
	for _, p := range s.permissions {
		perms = append(perms, p.Name)
	}
	role := &Role{
		ID:          "role-admin",
		Name:        "admin",
		Description: "Administrator with full access",
		Permissions: perms,
		CreatedAt:   time.Now(),
	}
	s.roles[role.ID] = role
	s.rolesByName[role.Name] = role.ID
}

// seedAdminUser seeds the default admin user.
func (s *MemoryStore) seedAdminUser() {
	hash, _ := auth.HashPassword("admin123")
	user := &User{
		ID:                 "user-admin",
		Username:           "admin",
		Email:              "admin@opsmesh.io",
		PasswordHash:       hash,
		Status:             "active",
		RoleIDs:            []string{"role-admin"},
		CreatedAt:          time.Now(),
		MustChangePassword: true,
	}
	s.users[user.ID] = user
	s.usersByName[user.Username] = user.ID
}

// --- User operations ---

// GetUser returns a user by ID.
func (s *MemoryStore) GetUser(id string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil
	}
	cp := *u
	return &cp
}

// GetUserByUsername returns a user by username.
func (s *MemoryStore) GetUserByUsername(username string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.usersByName[username]
	if !ok {
		return nil
	}
	u, ok := s.users[id]
	if !ok {
		return nil
	}
	cp := *u
	return &cp
}

// ListUsers returns all users.
func (s *MemoryStore) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		cp := *u
		out = append(out, &cp)
	}
	return out
}

// CreateUser creates a user.
func (s *MemoryStore) CreateUser(u *User) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.usersByName[u.Username]; exists {
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
	s.users[u.ID] = u
	s.usersByName[u.Username] = u.ID
	return u, nil
}

// UpdateUser updates a user.
func (s *MemoryStore) UpdateUser(u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.users[u.ID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	if u.Email != "" {
		existing.Email = u.Email
	}
	if u.Status != "" {
		existing.Status = u.Status
	}
	if u.RoleIDs != nil {
		existing.RoleIDs = u.RoleIDs
	}
	return nil
}

// DeleteUser deletes a user.
func (s *MemoryStore) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return fmt.Errorf("user not found")
	}
	delete(s.usersByName, u.Username)
	delete(s.users, id)
	return nil
}

// ChangePassword changes a user's password.
func (s *MemoryStore) ChangePassword(userID, newHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.PasswordHash = newHash
	u.MustChangePassword = false
	return nil
}

// --- Role operations ---

// GetRole returns a role by ID.
func (s *MemoryStore) GetRole(id string) *Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.roles[id]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// GetRoleByName returns a role by name.
func (s *MemoryStore) GetRoleByName(name string) *Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.rolesByName[name]
	if !ok {
		return nil
	}
	r, ok := s.roles[id]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// ListRoles returns all roles.
func (s *MemoryStore) ListRoles() []*Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Role, 0, len(s.roles))
	for _, r := range s.roles {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// CreateRole creates a role.
func (s *MemoryStore) CreateRole(r *Role) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rolesByName[r.Name]; exists {
		return nil, fmt.Errorf("role name already exists")
	}
	if r.ID == "" {
		r.ID = "role-" + uuid.New().String()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	s.roles[r.ID] = r
	s.rolesByName[r.Name] = r.ID
	return r, nil
}

// UpdateRole updates a role.
func (s *MemoryStore) UpdateRole(r *Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.roles[r.ID]
	if !ok {
		return fmt.Errorf("role not found")
	}
	if r.Description != "" {
		existing.Description = r.Description
	}
	if r.Permissions != nil {
		existing.Permissions = r.Permissions
	}
	return nil
}

// DeleteRole deletes a role.
func (s *MemoryStore) DeleteRole(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.roles[id]
	if !ok {
		return fmt.Errorf("role not found")
	}
	delete(s.rolesByName, r.Name)
	delete(s.roles, id)
	return nil
}

// --- Permission operations ---

// ListPermissions returns all permissions.
func (s *MemoryStore) ListPermissions() []*Permission {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Permission, 0, len(s.permissions))
	for _, p := range s.permissions {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// --- Refresh token operations ---

// SaveRefreshToken saves a refresh token.
func (s *MemoryStore) SaveRefreshToken(rt *RefreshToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshTokens[rt.TokenHash] = rt
}

// GetRefreshToken returns a refresh token by hash.
func (s *MemoryStore) GetRefreshToken(tokenHash string) *RefreshToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rt, ok := s.refreshTokens[tokenHash]
	if !ok {
		return nil
	}
	cp := *rt
	return &cp
}

// DeleteRefreshToken deletes a refresh token.
func (s *MemoryStore) DeleteRefreshToken(tokenHash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.refreshTokens[tokenHash]; !ok {
		return false
	}
	delete(s.refreshTokens, tokenHash)
	return true
}

// ConsumeRefreshToken atomically reads and deletes a refresh token.
func (s *MemoryStore) ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.refreshTokens[tokenHash]
	if !ok {
		return nil, false
	}
	delete(s.refreshTokens, tokenHash)
	cp := *rt
	return &cp, true
}

// --- Blacklist operations ---

// BlacklistJTI blacklists a JWT JTI.
func (s *MemoryStore) BlacklistJTI(jti string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blacklist[jti] = time.Now().Add(ttl)
}

// IsBlacklisted checks if a JTI is blacklisted.
func (s *MemoryStore) IsBlacklisted(jti string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	expiry, ok := s.blacklist[jti]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		return false
	}
	return true
}

// PurgeBlacklist removes expired blacklist entries.
func (s *MemoryStore) PurgeBlacklist() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for jti, expiry := range s.blacklist {
		if now.After(expiry) {
			delete(s.blacklist, jti)
		}
	}
}
