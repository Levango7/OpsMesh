// sql_gateway_route.go 实现 SQLStore 的 GatewayRouteStore 子接口（TD-66 P1：路由持久化）。
//
// 表结构：gateway_routes（见 migrations/022_gateway_routes.sql），
// (tenant_id, id) 联合唯一；methods 以 JSON 字符串存储。
//
// 设计要点（与 sql_discovery.go 风格一致）：
//   - DB 不可用/无行返回零值，不 panic；sql.ErrNoRows 视为"不存在"；
//   - SaveGatewayRoute 用 INSERT ... ON DUPLICATE KEY UPDATE 幂等 upsert；
//   - methods JSON 反序列化失败保留 nil（不阻断读取——半条坏数据不应让整条路由消失）；
//   - 时间统一 time.Now().UTC()。
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// scanGatewayRoute 从一行扫描出 *GatewayRoute（methods 列为 JSON 文本）。
// 无行或扫描失败返回 nil。
func scanGatewayRoute(row rowScanner) *GatewayRoute {
	var r GatewayRoute
	// rate_limit_per_sec / timeout_ms / enabled / methods 均可空：迁移前的存量行没有
	// timeout_ms；前端也可能省略字段。裸目标扫描会让这类路由整行读不出来。
	var methodsStr sql.NullString
	var enabled sql.NullBool
	var rate, timeoutMs sql.NullInt64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&r.ID, &r.TenantID, &r.Name, &r.PathPrefix, &r.TargetBackend,
		&methodsStr, &rate, &timeoutMs, &enabled, &createdAt, &updatedAt); err != nil {
		return nil
	}
	r.RateLimitPerSec, r.TimeoutMs = int(rate.Int64), int(timeoutMs.Int64)
	r.Enabled = enabled.Bool
	if methodsStr.String != "" {
		// 反序列化失败保留 nil Methods（空方法列表=全部方法由 MatchRoute 语义兜底）。
		var methods []string
		if err := json.Unmarshal([]byte(methodsStr.String), &methods); err == nil {
			r.Methods = methods
		}
	}
	r.CreatedAt, r.UpdatedAt = createdAt, updatedAt
	return &r
}

// gatewayRouteColumns 读路径统一的列清单（含时间列）。列数与 scanGatewayRoute
// 的 Scan 目标数一一对应——这是 TD-90 列数对账门禁的要求。
const gatewayRouteColumns = `id, tenant_id, name, path_prefix, target_backend,
	methods, rate_limit_per_sec, timeout_ms, enabled, created_at, updated_at`

// SaveGatewayRoute 幂等 upsert 路由规则（按 tenant_id+id）。返回持久化错误。
func (s *SQLStore) SaveGatewayRoute(rule *model.GatewayRoute) error {
	if rule == nil || rule.TenantID == "" || rule.ID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	methods := "[]"
	if len(rule.Methods) > 0 {
		if b, err := json.Marshal(rule.Methods); err == nil {
			methods = string(b)
		} else {
			storefail.Record("[store] SaveGatewayRoute methods 序列化失败: %v", err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO gateway_routes
		 (id, tenant_id, name, path_prefix, target_backend, methods, rate_limit_per_sec, timeout_ms, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), path_prefix=VALUES(path_prefix),
		   target_backend=VALUES(target_backend), methods=VALUES(methods),
		   rate_limit_per_sec=VALUES(rate_limit_per_sec), timeout_ms=VALUES(timeout_ms),
		   enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		rule.ID, rule.TenantID, rule.Name, rule.PathPrefix, rule.TargetBackend, methods,
		rule.RateLimitPerSec, rule.TimeoutMs, rule.Enabled, rule.CreatedAt, rule.UpdatedAt); err != nil {
		storefail.Record("[store] SaveGatewayRoute 失败 %s/%s: %v", rule.TenantID, rule.ID, err)
		return err
	}
	return nil
}

// ListGatewayRoutes 返回指定租户的全部路由（按 ID 升序）。
func (s *SQLStore) ListGatewayRoutes(tenantID string) []*model.GatewayRoute {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+gatewayRouteColumns+" FROM gateway_routes WHERE tenant_id=? ORDER BY id ASC",
		tenantID)
	if err != nil {
		storefail.Record("[store] ListGatewayRoutes 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*model.GatewayRoute
	for rows.Next() {
		if r := scanGatewayRoute(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListGatewayRoutes 遍历失败: %v", err)
	}
	return out
}

// GetGatewayRoute 按租户 + ID 取单条（不存在返回 nil）。
func (s *SQLStore) GetGatewayRoute(tenantID, id string) *model.GatewayRoute {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		"SELECT "+gatewayRouteColumns+" FROM gateway_routes WHERE tenant_id=? AND id=?", tenantID, id)
	return scanGatewayRoute(row)
}

// DeleteGatewayRoute 按租户 + ID 删除。返回是否删除成功。
func (s *SQLStore) DeleteGatewayRoute(tenantID, id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM gateway_routes WHERE tenant_id=? AND id=?", tenantID, id)
	if err != nil {
		storefail.Record("[store] DeleteGatewayRoute 失败 %s/%s: %v", tenantID, id, err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		storefail.Record("[store] DeleteGatewayRoute RowsAffected 失败: %v", err)
		return false
	}
	return n > 0
}
