package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// 本文件测试 sql.go / sql_rbac.go / sql_k8s.go 中的关键查询逻辑。
//
// 测试分两层：
//  1. 纯逻辑层（无需 MySQL）：ensureParseTime / boolToInt / nullString / nullTime /
//     hashToken / verifyTokenMAC 等 SQL 构造与 token 签名辅助函数；
//  2. 行扫描层（mock rowScanner）：scanUser / scanRole / scanK8sCluster / scanAlertRule，
//     验证从 *sql.Rows 扫描出领域对象的字段映射与 JSON 列解析正确性。
//
// 真实 MySQL 集成测试见 TestSQLStore_TenantIsolation（需 OPSMESH_TEST_MYSQL_DSN）。

// ============================================================================
// mockRowScanner：模拟 *sql.Row / *sql.Rows 的 Scan 接口
// ============================================================================

// mockRowScanner 实现 rowScanner 接口，按顺序将预置值赋给 Scan 的 dest 指针。
// err 非空时 Scan 直接返回该错误（模拟数据库扫描失败）。
type mockRowScanner struct {
	vals []interface{}
	err  error
}

func (m *mockRowScanner) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	if len(dest) != len(m.vals) {
		return errors.New("mockRowScanner: dest 与 vals 长度不匹配")
	}
	for i, d := range dest {
		// 用 reflect 将 m.vals[i] 赋给 dest[i] 指向的变量。
		rv := reflect.ValueOf(d).Elem()
		rv.Set(reflect.ValueOf(m.vals[i]))
	}
	return nil
}

// ============================================================================
// ensureParseTime：DSN 改写（保证 parseTime=true）
// ============================================================================

func TestEnsureParseTime_AlreadyHasParseTime(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/db?parseTime=true&charset=utf8"
	got := ensureParseTime(dsn)
	if got != dsn {
		t.Fatalf("已含 parseTime=true 时应原样返回；got=%q", got)
	}
}

func TestEnsureParseTime_AppendToExistingQuery(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/db?charset=utf8"
	got := ensureParseTime(dsn)
	if !strings.Contains(got, "parseTime=true") {
		t.Fatalf("应追加 parseTime=true；got=%q", got)
	}
	if !strings.Contains(got, "charset=utf8") {
		t.Fatalf("应保留原有参数；got=%q", got)
	}
}

func TestEnsureParseTime_AddNewQuery(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/db"
	got := ensureParseTime(dsn)
	if !strings.Contains(got, "?parseTime=true") {
		t.Fatalf("无 query 时应以 ? 起始追加；got=%q", got)
	}
}

// ============================================================================
// boolToInt / nullString / nullTime：SQL 参数归一化辅助函数
// ============================================================================

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Fatal("boolToInt(true) 应为 1")
	}
	if boolToInt(false) != 0 {
		t.Fatal("boolToInt(false) 应为 0")
	}
}

func TestNullString(t *testing.T) {
	if nullString("") != nil {
		t.Fatal("nullString(\"\") 应为 nil")
	}
	if nullString("abc") != "abc" {
		t.Fatal("nullString(\"abc\") 应为 \"abc\"")
	}
}

func TestNullTime(t *testing.T) {
	if nullTime(time.Time{}) != nil {
		t.Fatal("nullTime(零值) 应为 nil")
	}
	now := time.Now()
	got := nullTime(now)
	if gt, ok := got.(time.Time); !ok || !gt.Equal(now) {
		t.Fatalf("nullTime(非零值) 应返回原时间；got=%v", got)
	}
}

// ============================================================================
// hashToken：SHA-256 摘要（确定性、不可逆）
// ============================================================================

func TestHashToken_Deterministic(t *testing.T) {
	tok := "abc.def|ghi|123|nonce"
	a := hashToken(tok)
	b := hashToken(tok)
	if a != b {
		t.Fatalf("相同输入应产生相同摘要；a=%q b=%q", a, b)
	}
}

func TestHashToken_DifferentInput(t *testing.T) {
	if hashToken("token-a") == hashToken("token-b") {
		t.Fatal("不同输入应产生不同摘要")
	}
}

func TestHashToken_NotPlaintext(t *testing.T) {
	tok := "sensitive-install-token-value"
	h := hashToken(tok)
	if strings.Contains(h, tok) {
		t.Fatal("摘要不应包含明文 token")
	}
	if len(h) != 64 { // SHA-256 hex = 32 bytes = 64 chars
		t.Fatalf("SHA-256 hex 长度应为 64；got=%d", len(h))
	}
}

// ============================================================================
// verifyTokenMAC：HMAC-SHA256 签名校验
// ============================================================================

// makeSignedToken 用 secret 签发一个合法 token（与 issueTokenLocked 同格式）。
// 用于 verifyTokenMAC 正例。
func makeSignedToken(secret, tenantID, deviceID string, expiresAt time.Time, nonce string) string {
	payload := strings.Join([]string{tenantID, deviceID, strconv.FormatInt(expiresAt.Unix(), 10), nonce}, "|")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil)) + "." + payload
}

func TestVerifyTokenMAC_Valid(t *testing.T) {
	secret := "test-secret-key"
	tok := makeSignedToken(secret, "t1", "dev-1", time.Now().Add(time.Hour), "nonce-abc")
	if !verifyTokenMAC(secret, tok) {
		t.Fatal("合法签名 token 应校验通过")
	}
}

func TestVerifyTokenMAC_WrongSecret(t *testing.T) {
	secret := "test-secret-key"
	tok := makeSignedToken(secret, "t1", "dev-1", time.Now().Add(time.Hour), "nonce-abc")
	if verifyTokenMAC("wrong-secret", tok) {
		t.Fatal("用错误 secret 校验应失败")
	}
}

func TestVerifyTokenMAC_EmptyInputs(t *testing.T) {
	if verifyTokenMAC("", "some.token") {
		t.Fatal("空 secret 应校验失败")
	}
	if verifyTokenMAC("secret", "") {
		t.Fatal("空 token 应校验失败")
	}
}

func TestVerifyTokenMAC_MalformedToken(t *testing.T) {
	if verifyTokenMAC("secret", "no-dot-payload") {
		t.Fatal("无分隔符的 token 应校验失败")
	}
	if verifyTokenMAC("secret", ".payload-only") {
		t.Fatal("空签名部分应校验失败")
	}
	if verifyTokenMAC("secret", "sig-only.") {
		t.Fatal("空 payload 部分应校验失败")
	}
}

func TestVerifyTokenMAC_TamperedPayload(t *testing.T) {
	secret := "test-secret-key"
	tok := makeSignedToken(secret, "t1", "dev-1", time.Now().Add(time.Hour), "nonce-abc")
	// 篡改 payload 部分（签名不变）
	parts := strings.SplitN(tok, ".", 2)
	tampered := parts[0] + ".tampered-tenant|dev-1|999|nonce-abc"
	if verifyTokenMAC(secret, tampered) {
		t.Fatal("篡改 payload 后签名校验应失败")
	}
}

// ============================================================================
// scanUser：用户行扫描（sql_rbac.go）
// ============================================================================

func TestScanUser_Happy(t *testing.T) {
	created := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	roleIDsJSON, _ := json.Marshal([]string{"role-admin", "role-ops"})
	row := &mockRowScanner{vals: []interface{}{
		"u-1", "alice", "alice@example.com", "$2a$10$hash", "active",
		roleIDsJSON, created, true,
	}}
	u := scanUser(row)
	if u == nil {
		t.Fatal("scanUser 返回 nil")
	}
	if u.ID != "u-1" || u.Username != "alice" || u.Email != "alice@example.com" {
		t.Fatalf("基础字段映射错误: %+v", u)
	}
	if u.Status != "active" || !u.MustChangePassword {
		t.Fatalf("状态/改密标记错误: %+v", u)
	}
	if len(u.RoleIDs) != 2 || u.RoleIDs[0] != "role-admin" || u.RoleIDs[1] != "role-ops" {
		t.Fatalf("RoleIDs 解析错误: %+v", u.RoleIDs)
	}
	if !u.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", u.CreatedAt, created)
	}
}

func TestScanUser_EmptyRoles(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"u-2", "bob", "", "$2a$10$hash", "pending",
		[]byte{}, time.Time{}, false,
	}}
	u := scanUser(row)
	if u == nil {
		t.Fatal("scanUser 返回 nil")
	}
	if len(u.RoleIDs) != 0 {
		t.Fatalf("空 roleIDs JSON 应解析为空切片；got=%+v", u.RoleIDs)
	}
}

func TestScanUser_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db connection lost")}
	if u := scanUser(row); u != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// scanRole：角色行扫描（sql_rbac.go）
// ============================================================================

func TestScanRole_Happy(t *testing.T) {
	created := time.Date(2026, 2, 20, 8, 0, 0, 0, time.UTC)
	permsJSON, _ := json.Marshal([]string{"device:read", "task:write", "alert:read"})
	row := &mockRowScanner{vals: []interface{}{
		"role-ops", "ops", "运维角色", permsJSON, created,
	}}
	r := scanRole(row)
	if r == nil {
		t.Fatal("scanRole 返回 nil")
	}
	if r.ID != "role-ops" || r.Name != "ops" || r.Description != "运维角色" {
		t.Fatalf("基础字段映射错误: %+v", r)
	}
	if len(r.Permissions) != 3 || r.Permissions[1] != "task:write" {
		t.Fatalf("Permissions 解析错误: %+v", r.Permissions)
	}
	if !r.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", r.CreatedAt, created)
	}
}

func TestScanRole_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("column mismatch")}
	if r := scanRole(row); r != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// scanK8sCluster：K8s 集群行扫描（sql_k8s.go）
// ============================================================================

func TestScanK8sCluster_Happy(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"k8s-1", "t1", "prod-cluster", "https://10.0.0.1:6443",
		"apiVersion: v1\nclusters: [...]", "online", created, updated,
	}}
	c := scanK8sCluster(row)
	if c == nil {
		t.Fatal("scanK8sCluster 返回 nil")
	}
	if c.ID != "k8s-1" || c.TenantID != "t1" || c.Name != "prod-cluster" {
		t.Fatalf("基础字段映射错误: %+v", c)
	}
	if c.Server != "https://10.0.0.1:6443" || c.Status != "online" {
		t.Fatalf("Server/Status 错误: %+v", c)
	}
	if !c.CreatedAt.Equal(created) || !c.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", c.CreatedAt, c.UpdatedAt)
	}
}

func TestScanK8sCluster_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if c := scanK8sCluster(row); c != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// scanAlertRule：告警规则行扫描（sql.go）
// ============================================================================

func TestScanAlertRule_Happy(t *testing.T) {
	created := time.Date(2026, 4, 10, 9, 15, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"ar-1", "t1", "cpu_usage", ">", 90.5,
		5, "critical", "CPU 超过 90.5%", true, created, sql.NullString{Valid: true, String: "user-1"},
	}}
	r := scanAlertRule(row)
	if r == nil {
		t.Fatal("scanAlertRule 返回 nil")
	}
	if r.ID != "ar-1" || r.TenantID != "t1" || r.Metric != "cpu_usage" {
		t.Fatalf("基础字段映射错误: %+v", r)
	}
	if r.Op != ">" || r.Threshold != 90.5 || r.ForDuration != 5 {
		t.Fatalf("阈值条件错误: %+v", r)
	}
	if r.Severity != "critical" || !r.Enabled {
		t.Fatalf("严重度/启用状态错误: %+v", r)
	}
	if !r.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt 错误: got=%v want=%v", r.CreatedAt, created)
	}
	if r.CreatedBy != "user-1" {
		t.Fatalf("CreatedBy 错误: got=%q want=%q", r.CreatedBy, "user-1")
	}
}

func TestScanAlertRule_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if r := scanAlertRule(row); r != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// SQL 查询列常量完整性（防列顺序漂移导致 scan 错位）
// ============================================================================

func TestUserColumns_ContainsMustChangePassword(t *testing.T) {
	// 安全债：userColumns 必须含 must_change_password 列，否则 scanUser 列错位。
	if !strings.Contains(userColumns, "must_change_password") {
		t.Fatal("userColumns 缺少 must_change_password 列")
	}
	// 列顺序须与 scanUser 的 Scan 顺序一致：id, username, email, password_hash, status, role_ids, created_at, must_change_password
	expected := "id, username, email, password_hash, status, role_ids, created_at, must_change_password"
	if userColumns != expected {
		t.Fatalf("userColumns 顺序漂移；got=%q want=%q", userColumns, expected)
	}
}

func TestAlertRuleColumns_Completeness(t *testing.T) {
	// alertRuleColumns 须含全部 scanAlertRule 所需列。
	required := []string{"id", "tenant_id", "metric", "op", "threshold",
		"for_duration", "severity", "message", "enabled", "created_at"}
	for _, col := range required {
		if !strings.Contains(alertRuleColumns, col) {
			t.Fatalf("alertRuleColumns 缺少列 %q", col)
		}
	}
}

// ============================================================================
// randAlertRuleID：告警规则 ID 生成
// ============================================================================

func TestRandAlertRuleID_Format(t *testing.T) {
	id := randAlertRuleID()
	if !strings.HasPrefix(id, "alert-rule-") {
		t.Fatalf("ID 应以 alert-rule- 开头；got=%q", id)
	}
}

func TestRandAlertRuleID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := randAlertRuleID()
		if seen[id] {
			t.Fatalf("ID 重复: %q", id)
		}
		seen[id] = true
	}
}

// ============================================================================
// 真实 MySQL 集成测试（默认跳过）
// ============================================================================

// TestSQLStore_TenantIsolation 需要真实 MySQL + Redis，默认跳过。
// 运行：
//
//	OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//	OPSMESH_TEST_REDIS_ADDR=127.0.0.1:6379 \
//	go test ./internal/store/ -run TestSQLStore -v
//
// 验证 数据本地化下，SQLStore 的租户隔离与 MemoryStore 行为一致。
func TestSQLStore_TenantIsolation(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL store test")
	}
	redisAddr := os.Getenv("OPSMESH_TEST_REDIS_ADDR")
	s, err := NewSQLStore(dsn, redisAddr)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}

	s.Register(&proto.AgentInfo{AgentID: "sql-a1", Segment: "seg-a", TenantID: "t1"})
	s.Register(&proto.AgentInfo{AgentID: "sql-a2", Segment: "seg-a", TenantID: "t2"})

	if got := s.Agents("t1"); len(got) != 1 || got[0].AgentID != "sql-a1" {
		t.Fatalf("SQL Agents(t1) = %+v, want exactly sql-a1", got)
	}
	if got := s.Agents("t2"); len(got) != 1 || got[0].AgentID != "sql-a2" {
		t.Fatalf("SQL Agents(t2) = %+v, want exactly sql-a2", got)
	}
	if n := countDevices(s.Snapshot("t1")); n != 1 {
		t.Fatalf("SQL Snapshot(t1) device count = %d, want 1", n)
	}
}
