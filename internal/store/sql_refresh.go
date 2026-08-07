// sql_refresh.go 实现 SQLStore 的 RefreshTokenStore 子接口（task 111 刷新令牌，P0-1 生产就绪）。
//
// 表结构：refresh_tokens（token_hash/user_id/tenant_id/device_fp/expires_at/created_at）。
// initSchema 中幂等建表（CREATE TABLE IF NOT EXISTS）+ alterColumnIfMissing 兼容旧库
// + createIndexIfMissing 补二级索引（user_id / expires_at）。
//
// 设计要点（与 sql_k8s.go 风格一致）：
//   - token_hash 为明文 token 的 SHA-256 摘要（P1-F7 明文不落库），作主键；
//   - SaveRefreshToken 按 token_hash 幂等（INSERT ... ON DUPLICATE KEY UPDATE）；
//   - DB 不可用时返回零值（nil/false），不 panic，与 SQLStore 其他方法一致；
//   - 持久化失败上抛错误（调用方据此返回非 2xx，与 SaveK8sCluster 一致）。
package store

import (
	"context"
	"fmt"
	"log"
	"time"
)

// scanRefreshToken 从一行扫描出 *RefreshToken。无行或扫描失败返回 nil。
func scanRefreshToken(row rowScanner) *RefreshToken {
	var rt RefreshToken
	var expiresAt, createdAt time.Time
	if err := row.Scan(&rt.TokenHash, &rt.UserID, &rt.TenantID, &rt.DeviceFP, &expiresAt, &createdAt); err != nil {
		return nil
	}
	rt.ExpiresAt = expiresAt
	rt.CreatedAt = createdAt
	return &rt
}

// SaveRefreshToken 创建或更新 refresh token（按 token_hash 幂等）。
//
// 行为与 MemoryStore.SaveRefreshToken 一致：
//   - rt 为 nil 时直接返回（空操作）；
//   - TokenHash 为空时拒绝（主键不可空），返回错误；
//   - TenantID 为空时归一为 default；
//   - CreatedAt 为零值时填当前时间（新建场景）。
//
// 持久化失败上抛错误（task 92 范式：DB 失败不再假装成功）。
func (s *SQLStore) SaveRefreshToken(rt *RefreshToken) error {
	if rt == nil {
		return nil
	}
	if rt.TokenHash == "" {
		return errRefreshTokenHashRequired
	}
	// 租户隔离：空租户归一为 default（与 K8sCluster/Template 一致）。
	if rt.TenantID == "" {
		rt.TenantID = "default"
	}
	now := time.Now().UTC()
	if rt.CreatedAt.IsZero() {
		rt.CreatedAt = now
	}
	// INSERT ... ON DUPLICATE KEY UPDATE 实现 upsert（按 token_hash 幂等）。
	// token_hash 为明文 token 的 SHA-256 摘要（P1-F7 明文不落库）。
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO refresh_tokens (token_hash, user_id, tenant_id, device_fp, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE user_id=VALUES(user_id), tenant_id=VALUES(tenant_id),
		 device_fp=VALUES(device_fp), expires_at=VALUES(expires_at), created_at=VALUES(created_at)`,
		rt.TokenHash, rt.UserID, rt.TenantID, rt.DeviceFP, rt.ExpiresAt, rt.CreatedAt); err != nil {
		log.Printf("store: SaveRefreshToken 失败: %v", err)
		return fmt.Errorf("store: save refresh token: %w", err)
	}
	return nil
}

// GetRefreshToken 按 token_hash 返回单个 refresh token（不存在返回 nil）。
func (s *SQLStore) GetRefreshToken(tokenHash string) *RefreshToken {
	if tokenHash == "" {
		return nil
	}
	row := s.db.QueryRowContext(context.Background(),
		`SELECT token_hash, user_id, tenant_id, device_fp, expires_at, created_at FROM refresh_tokens WHERE token_hash=?`, tokenHash)
	return scanRefreshToken(row)
}

// DeleteRefreshToken 按 token_hash 删除 refresh token，返回是否删除成功（不存在返回 false）。
func (s *SQLStore) DeleteRefreshToken(tokenHash string) bool {
	if tokenHash == "" {
		return false
	}
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM refresh_tokens WHERE token_hash=?`, tokenHash)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
