// sql_tokens.go - SQLStore TokenStore methods (install token lifecycle).
package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

func (s *SQLStore) Provision(deviceID, host, tenantID string) (token, bootstrap string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 安全（F15）：payload 用 | 分隔，deviceID/tenantID 含 | 导致解析歧义，直接拒绝。
	if strings.Contains(deviceID, "|") || strings.Contains(tenantID, "|") {
		return "", "", fmt.Errorf("deviceID 或 tenantID 含非法字符 |")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET state='provisioning', ip=? WHERE device_id=? AND (tenant_id=? OR ?='')`,
		host, deviceID, tenantID, tenantID)
	if err != nil {
		return "", "", fmt.Errorf("Provision 失败 %s: %w", deviceID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", "", fmt.Errorf("device %s not found or tenant mismatch", deviceID)
	}
	tok, e := s.issueToken(ctx, deviceID, tenantID, 15*time.Minute)
	if e != nil {
		return "", "", e
	}
	// bootstrap 为占位模板，真实控制面地址由 HTTP handler 按请求 host 重写。
	boot := fmt.Sprintf("curl -sSL http://<control-plane>:8080/install.sh | sh -s -- --token=%s", tok)
	return tok, boot, nil
}

// issueToken 在已持 ctx 下签发一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)），
// 落 install_tokens 表（ON DUPLICATE 重置消费态，幂等重推）。
// 安全（P1-F7）：token 列只存 SHA-256 摘要，不存明文 token——DB 只读账号/备份泄露不等于活体 token 泄露。

func (s *SQLStore) issueToken(ctx context.Context, deviceID, tenantID string, ttl time.Duration) (string, error) {
	if s.secret == "" {
		s.secret = mustRandHex(32) // 兜底，正常构造时已置随机密钥
	}
	nonce := randHex(16)
	expiresAt := time.Now().UTC().Add(ttl)
	payload := strings.Join([]string{tenantID, deviceID, strconv.FormatInt(expiresAt.Unix(), 10), nonce}, "|")
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(payload))
	tok := hex.EncodeToString(mac.Sum(nil)) + "." + payload
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO install_tokens (token, device_id, tenant_id, expires_at, consumed)
		 VALUES (?, ?, ?, ?, 0)
		 ON DUPLICATE KEY UPDATE device_id=VALUES(device_id), tenant_id=VALUES(tenant_id), expires_at=VALUES(expires_at), consumed=0`,
		hashToken(tok), deviceID, tenantID, expiresAt); err != nil {
		return "", fmt.Errorf("issueToken 失败: %w", err)
	}
	return tok, nil
}

// IssueToken 生成并登记一个一次性 install token（B1）。

func (s *SQLStore) IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if deviceID == "" {
		return "", fmt.Errorf("deviceID required")
	}
	return s.issueToken(ctx, deviceID, tenantID, ttl)
}

// ConsumeToken 校验并消费 token：限时、未用过才返回设备与租户并置 consumed；否则返回 ok=false。
// 安全（P0-F2）：原子条件 UPDATE（consumed=0 AND 未过期）+ RowsAffected==1 判定，
// 消除 check-then-act TOCTOU 竞态，多副本并发下同一 token 只会被消费一次。

func (s *SQLStore) ConsumeToken(token string) (deviceID, tenantID string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 安全（F8）：先验 HMAC 签名（防 DB 写权限伪造），签名不对直接拒绝。
	if !verifyTokenMAC(s.secret, token) {
		return "", "", false
	}
	hash := hashToken(token) // P1-F7：库存摘要，按摘要匹配
	// 原子抢占：仅当未被消费且未过期时翻转 consumed=0→1，RowsAffected==1 即消费成功。
	res, err := s.db.ExecContext(ctx,
		`UPDATE install_tokens SET consumed=1 WHERE token=? AND consumed=0 AND expires_at > ?`,
		hash, time.Now().UTC())
	if err != nil {
		log.Printf("[store] ConsumeToken 抢占失败: %v", err)
		return "", "", false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", "", false // 已被消费 / 已过期 / 不存在
	}
	// 消费成功后读回设备与租户（token 行此时已唯一锁定为本实例）。
	if err := s.db.QueryRowContext(ctx,
		`SELECT device_id, tenant_id FROM install_tokens WHERE token=?`, hash,
	).Scan(&deviceID, &tenantID); err != nil {
		log.Printf("[store] ConsumeToken 读回失败: %v", err)
		return "", "", false
	}
	return deviceID, tenantID, true
}

// Alerts 返回活跃告警（M7）；tenantID 非空时按租户过滤。

func (s *SQLStore) CleanupTokens(batch int) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `DELETE FROM install_tokens WHERE expires_at < ?`
	var args []interface{}
	args = append(args, time.Now().UTC())
	if batch > 0 {
		q += ` LIMIT ?`
		args = append(args, batch)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] CleanupTokens 失败: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// RetireStaleDevices F5 离线超龄自动归档：最后心跳早于 maxAge 的 agent 所对应设备
// （或已无 agent 的孤儿设备）批量标记 retired。返回归档数。
