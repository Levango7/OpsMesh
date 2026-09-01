// Package tenant provides multi-tenancy enforcement for OpsMesh services.
//
// Core capabilities:
//   - Quota enforcement: Per-tenant resource limits (devices, tasks, alerts, agents, etc.)
//   - Usage tracking: Record resource consumption per tenant
//   - Tenant extraction: Extract tenant identity from JWT claims and inject into context
//   - Middleware: HTTP middleware for tenant-aware request handling
//
// The package integrates with the auth JWT system (pkg/auth) and the store quota
// system (internal/store) to provide a unified multi-tenancy layer.
//
// Design principles:
//   - Zero-tenant safety: Empty tenantID defaults to "default" to avoid nil panics.
//   - Fail-open on errors: Quota checks that cannot be verified log a warning but
//     do not block the operation (prevents cascading failures).
//   - Context-first: Tenant identity flows through context.Context, not globals.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ResourceType identifies a tracked resource category.
type ResourceType string

const (
	ResourceDevices  ResourceType = "devices"
	ResourceTasks    ResourceType = "tasks"
	ResourceAlerts   ResourceType = "alerts"
	ResourceAgents   ResourceType = "agents"
	ResourceWebhooks ResourceType = "webhooks"
	ResourceAPIKeys  ResourceType = "api_keys"
)

// TenantUsage tracks current resource consumption for a single tenant.
type TenantUsage struct {
	TenantID string               `json:"tenantID"`
	Usage    map[ResourceType]int `json:"usage"`
	Quota    map[ResourceType]int `json:"quota"`
	mu       sync.RWMutex
}

// CanAllocate checks whether the tenant can allocate `amount` more of resourceType.
func (u *TenantUsage) CanAllocate(resourceType ResourceType, amount int) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()

	limit, hasLimit := u.Quota[resourceType]
	if !hasLimit || limit <= 0 {
		return true
	}
	current := u.Usage[resourceType]
	return current+amount <= limit
}

// AddUsage increments the usage counter for a resource type.
func (u *TenantUsage) AddUsage(resourceType ResourceType, amount int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.Usage[resourceType] += amount
}

// SetQuota sets the quota limit for a resource type.
func (u *TenantUsage) SetQuota(resourceType ResourceType, limit int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.Quota[resourceType] = limit
}

// GetUsage returns the current usage for a resource type.
func (u *TenantUsage) GetUsage(resourceType ResourceType) int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.Usage[resourceType]
}

// contextKey for storing tenant identity in context.
type tenantContextKey int

const (
	tenantIDKey tenantContextKey = iota
	tenantUsageKey
)

var (
	// ErrQuotaExceeded is returned when a tenant exceeds their resource quota.
	ErrQuotaExceeded = errors.New("tenant: quota exceeded")

	// ErrTenantNotFound is returned when the tenant cannot be identified.
	ErrTenantNotFound = errors.New("tenant: tenant not found in context")
)

// QuotaStore defines the interface for persisting and retrieving tenant quotas.
type QuotaStore interface {
	GetQuota(tenantID string) (map[ResourceType]int, error)
	SetQuota(tenantID string, resourceType ResourceType, limit int) error
}

// UsageStore defines the interface for tracking tenant resource usage.
type UsageStore interface {
	GetUsage(ctx context.Context, tenantID string) (*TenantUsage, error)
	TrackUsage(ctx context.Context, tenantID string, resourceType ResourceType, amount int) error
}

// Manager provides multi-tenancy enforcement for OpsMesh services.
type Manager struct {
	quotaStore QuotaStore
	usageStore UsageStore

	mu       sync.RWMutex
	usage    map[string]*TenantUsage
	defaults map[ResourceType]int
}

// NewManager creates a new tenant Manager.
//
// quotaStore may be nil if quota enforcement is not needed.
// usageStore may be nil if usage tracking is not needed.
// defaultQuota provides fallback quota limits when no per-tenant config exists.
func NewManager(quotaStore QuotaStore, usageStore UsageStore, defaultQuota map[ResourceType]int) *Manager {
	if defaultQuota == nil {
		defaultQuota = map[ResourceType]int{
			ResourceDevices:  100,
			ResourceTasks:    1000,
			ResourceAlerts:   100,
			ResourceAgents:   50,
			ResourceWebhooks: 10,
			ResourceAPIKeys:  5,
		}
	}
	return &Manager{
		quotaStore: quotaStore,
		usageStore: usageStore,
		usage:      make(map[string]*TenantUsage),
		defaults:   defaultQuota,
	}
}

// EnforceQuota checks whether tenantID is allowed to allocate `amount` of resourceType.
//
// Returns ErrQuotaExceeded if the allocation would exceed the tenant's quota.
// Returns nil if the allocation is permitted or if quota enforcement is unavailable.
func (m *Manager) EnforceQuota(ctx context.Context, tenantID string, resourceType ResourceType, amount int) error {
	tenantID = normalizeTenantID(tenantID)

	usage := m.getOrCreateUsage(tenantID)

	if m.quotaStore != nil {
		limits, err := m.quotaStore.GetQuota(tenantID)
		if err == nil && limits != nil {
			for k, v := range limits {
				usage.SetQuota(k, v)
			}
		}
	}

	// 快照读取（锁内）：CanAllocate 返回后 RLock 已释放，若直接在错误
	// 信息里裸读 usage.Quota/GetUsage，会与上方 quotaStore 并发 SetQuota
	// 写同一 map 构成 data race（CI -race -count=3 实测捕获）。
	// 判定语义与 CanAllocate 完全一致：无限额（limit<=0）一律放行。
	current, limit, hasLimit := usage.snapshotUsageAndQuota(resourceType)
	if hasLimit && limit > 0 && current+amount > limit {
		return fmt.Errorf("%w: tenant=%s resource=%s amount=%d current=%d limit=%d",
			ErrQuotaExceeded, tenantID, resourceType, amount,
			current, limit)
	}

	return nil
}

// snapshotUsageAndQuota 锁内一次性读取用量与配额（供超限判定与错误信息构造）。
// hasLimit=false 或 limit<=0 表示无限额（与 CanAllocate 的判定口径一致）。
func (u *TenantUsage) snapshotUsageAndQuota(resourceType ResourceType) (current, limit int, hasLimit bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	limit, hasLimit = u.Quota[resourceType]
	return u.Usage[resourceType], limit, hasLimit
}

// TrackUsage records the consumption of a resource by a tenant.
func (m *Manager) TrackUsage(ctx context.Context, tenantID string, resourceType ResourceType, amount int) error {
	tenantID = normalizeTenantID(tenantID)

	if m.usageStore != nil {
		if err := m.usageStore.TrackUsage(ctx, tenantID, resourceType, amount); err != nil {
			return fmt.Errorf("tenant: failed to track usage: %w", err)
		}
	}

	usage := m.getOrCreateUsage(tenantID)
	usage.AddUsage(resourceType, amount)
	return nil
}

// GetUsage returns the current TenantUsage for a tenantID.
//
// If the tenant has no recorded usage, a zero-value TenantUsage is returned.
func (m *Manager) GetUsage(ctx context.Context, tenantID string) (*TenantUsage, error) {
	tenantID = normalizeTenantID(tenantID)

	if m.usageStore != nil {
		usage, err := m.usageStore.GetUsage(ctx, tenantID)
		if err == nil && usage != nil {
			return usage, nil
		}
	}

	m.mu.RLock()
	usage, ok := m.usage[tenantID]
	m.mu.RUnlock()

	if !ok {
		usage = &TenantUsage{
			TenantID: tenantID,
			Usage:    make(map[ResourceType]int),
			Quota:    make(map[ResourceType]int),
		}
		for k, v := range m.defaults {
			usage.Quota[k] = v
		}
	}

	return usage, nil
}

// SetDefaultQuota updates the default quota for a resource type.
func (m *Manager) SetDefaultQuota(resourceType ResourceType, limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults[resourceType] = limit
}

// getOrCreateUsage returns the TenantUsage for tenantID, creating one if needed.
func (m *Manager) getOrCreateUsage(tenantID string) *TenantUsage {
	m.mu.Lock()
	defer m.mu.Unlock()

	usage, ok := m.usage[tenantID]
	if !ok {
		usage = &TenantUsage{
			TenantID: tenantID,
			Usage:    make(map[ResourceType]int),
			Quota:    make(map[ResourceType]int),
		}
		for k, v := range m.defaults {
			usage.Quota[k] = v
		}
		m.usage[tenantID] = usage
	}
	return usage
}

// normalizeTenantID returns "default" for empty tenant IDs.
func normalizeTenantID(tenantID string) string {
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

// --- Context helpers ---

// WithTenantID injects a tenant ID into the context.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, normalizeTenantID(tenantID))
}

// TenantIDFromContext extracts the tenant ID from the context.
// Returns ErrTenantNotFound if no tenant is set.
func TenantIDFromContext(ctx context.Context) (string, error) {
	v, ok := ctx.Value(tenantIDKey).(string)
	if !ok || v == "" {
		return "", ErrTenantNotFound
	}
	return v, nil
}

// WithTenantUsage injects TenantUsage into the context.
func WithTenantUsage(ctx context.Context, usage *TenantUsage) context.Context {
	return context.WithValue(ctx, tenantUsageKey, usage)
}

// TenantUsageFromContext extracts TenantUsage from the context.
func TenantUsageFromContext(ctx context.Context) (*TenantUsage, bool) {
	usage, ok := ctx.Value(tenantUsageKey).(*TenantUsage)
	return usage, ok
}

// --- gRPC Interceptor ---

// GRPCInterceptor returns a gRPC UnaryServerInterceptor that extracts
// the tenant ID from the JWT in the Authorization metadata and injects
// it into the context.
func GRPCInterceptor() func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		tenantID := tenantIDFromGRPCContext(ctx)
		ctx = WithTenantID(ctx, tenantID)
		return handler(ctx, req)
	}
}

// tenantIDFromGRPCContext extracts the tenant ID from gRPC metadata.
func tenantIDFromGRPCContext(ctx context.Context) string {
	// Check for X-Tenant-ID in metadata.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-tenant-id"); len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	return "default"
}

// --- HTTP Middleware ---

// Middleware extracts tenant from JWT and injects it into the request context.
//
// The middleware looks for the tenant ID in the following order:
//  1. "X-Tenant-ID" HTTP header
//  2. "tenant_id" claim from the JWT in the Authorization header (if authSecret is non-empty)
//
// If neither is present, the tenant defaults to "default".
func Middleware(authSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := extractTenantFromRequest(r, authSecret)
			// Middleware 语义：无法确定租户时兜底 default（宽松模式）。
			// RequireTenant 才是严格模式（无租户 403）——两模式差异由这里兜底
			// 实现：extractTenantFromRequest 不再自带兜底，否则 RequireTenant
			// 的 403 分支永不可达（"stricter"宣称形同虚设，测试补齐时实测确认）。
			if tenantID == "" {
				tenantID = "default"
			}
			ctx := WithTenantID(r.Context(), tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireTenant is a stricter middleware that rejects requests without a valid tenant.
// Unlike Middleware, it returns 403 if no tenant can be determined.
func RequireTenant(authSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := extractTenantFromRequest(r, authSecret)
			if tenantID == "" {
				http.Error(w, "tenant: missing tenant identity", http.StatusForbidden)
				return
			}
			ctx := WithTenantID(r.Context(), tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// QuotaMiddleware wraps an HTTP handler with quota enforcement for a specific resource type.
// The amount parameter specifies how much of the resource this endpoint consumes.
func (m *Manager) QuotaMiddleware(resourceType ResourceType, amount int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, err := TenantIDFromContext(r.Context())
			if err != nil {
				http.Error(w, ErrTenantNotFound.Error(), http.StatusForbidden)
				return
			}

			if err := m.EnforceQuota(r.Context(), tenantID, resourceType, amount); err != nil {
				if errors.Is(err, ErrQuotaExceeded) {
					http.Error(w, err.Error(), http.StatusTooManyRequests)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Non-fatal: usage tracking failure must not block the request —
			// quota enforcement above already passed, so the request proceeds.
			_ = m.TrackUsage(r.Context(), tenantID, resourceType, amount)

			next.ServeHTTP(w, r)
		})
	}
}

// extractTenantFromRequest extracts the tenant ID from headers and JWT.
// 无租户身份时返回空串（兜底策略由调用方决定：Middleware 兜 default，
// RequireTenant 拒绝 403）。
func extractTenantFromRequest(r *http.Request, authSecret string) string {
	// 1. Check X-Tenant-ID header.
	if tid := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); tid != "" {
		return tid
	}

	// 2. Try to extract from JWT using the auth package.
	if authSecret != "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			if tid := extractTenantFromToken(token, authSecret); tid != "" {
				return tid
			}
		}
	}

	return ""
}

// extractTenantFromToken parses a JWT token and extracts the tenant_id claim.
// Returns empty string if the token is invalid or has no tenant_id.
func extractTenantFromToken(tokenString, secret string) string {
	if tokenString == "" || secret == "" {
		return ""
	}

	// Use the auth package's ValidateServiceToken for proper JWT validation.
	// This is imported lazily to keep pkg/tenant independent of pkg/auth's internals.
	claims, err := validateTokenForTenant(tokenString, secret)
	if err != nil {
		return ""
	}
	return claims
}

// validateTokenForTenant is a function variable that can be overridden in tests.
// By default it delegates to the auth package.
var validateTokenForTenant = func(tokenString, secret string) (string, error) {
	return defaultValidateToken(tokenString, secret)
}

// defaultValidateToken uses the auth package to validate and extract tenant_id.
func defaultValidateToken(tokenString, secret string) (string, error) {
	// Import auth package at function level to avoid import cycle at package init.
	// Since pkg/auth imports nothing from pkg/tenant, this is safe.
	return authExtractTenant(tokenString, secret)
}

// Ensure time is used (for potential future timestamp-based features).
var _ = time.Now
