package store

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
)

// TestConfigStoreMySQLRoundTrip 是 config-svc MySQL 后端的真库往返校验（TD-65「集成测试补齐」）。
//
// 验的不是「构造函数没报错」，而是**数据真的落库**：写入走一个 store 实例，读回走
// **另一个全新实例**（新连接、无实例内状态）——「接线成功但数据只在内存里」的假绿在这一步必然暴露。
//
// 覆盖：配置项 upsert + 版本递增 + 历史表落账 + 删除语义 + 机密「密文落库、明文读回」。
// 门控：OPSMESH_TEST_MYSQL_DSN 缺失时 skip 并打印原因（CI 的 services job 已注入该变量）。
func TestConfigStoreMySQLRoundTrip(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}
	const key = "itest-encryption-key-0123456789abcdef"

	w, err := NewMySQLStore(dsn, key, 50)
	if err != nil {
		t.Fatalf("NewMySQLStore(writer): %v", err)
	}
	defer func() { _ = w.db.Close() }()

	// 每次运行用独立 tenant，保证可重复跑且不与其他用例/历史数据互相污染。
	tenant := fmt.Sprintf("itest-cfg-%d", time.Now().UnixNano())
	cfgKey := "roundtrip-key"
	secKey := "roundtrip-secret"
	defer func() {
		for _, q := range []string{
			"DELETE FROM config_entries WHERE tenant_id = ?",
			"DELETE FROM config_history WHERE tenant_id = ?",
			"DELETE FROM config_secrets WHERE tenant_id = ?",
		} {
			if _, err := w.db.Exec(q, tenant); err != nil {
				t.Logf("cleanup %q: %v", q, err)
			}
		}
	}()

	// 1) 首次写入 → 版本 1。
	first := w.SetConfig(&models.ConfigEntry{
		TenantID: tenant, Key: cfgKey, Value: "v1", Format: "text", UpdatedBy: "itest",
	})
	if first == nil {
		t.Fatal("SetConfig 首次写入返回 nil")
	}
	if first.Version != 1 {
		t.Fatalf("首次写入版本 = %d, want 1", first.Version)
	}

	// 2) 覆写 → 版本递增，且旧值进 config_history（历史表是独立落库路径，必须验）。
	second := w.SetConfig(&models.ConfigEntry{
		TenantID: tenant, Key: cfgKey, Value: "v2", Format: "text", UpdatedBy: "itest",
	})
	if second == nil {
		t.Fatal("SetConfig 覆写返回 nil")
	}
	if second.Version != 2 {
		t.Fatalf("覆写后版本 = %d, want 2", second.Version)
	}

	// 3) 换一个全新实例读回：值/版本必须来自库，而不是写入实例的内存。
	r, err := NewMySQLStore(dsn, key, 50)
	if err != nil {
		t.Fatalf("NewMySQLStore(reader): %v", err)
	}
	defer func() { _ = r.db.Close() }()

	got, ok := r.GetConfig(tenant, cfgKey)
	if !ok || got == nil {
		t.Fatal("新实例 GetConfig 未命中——数据没有真的落库")
	}
	if got.Value != "v2" || got.Version != 2 {
		t.Fatalf("新实例读回 = {value:%q version:%d}, want {v2 2}", got.Value, got.Version)
	}

	hist := r.GetConfigHistory(tenant, cfgKey)
	if len(hist) == 0 {
		t.Fatal("GetConfigHistory 为空——覆写前的旧值没有进 config_history")
	}
	if hist[0].Value != "v1" {
		t.Fatalf("历史首条 = %q, want %q（覆写前的值）", hist[0].Value, "v1")
	}

	// 4) 机密加密落库：库里必须是密文，读回必须能解出明文（config-svc 的 EncryptionKey 路径）。
	sec := w.CreateSecret(&models.SecretEntry{
		TenantID: tenant, Key: secKey, Value: "plain-secret-value", KeyType: "aes",
	})
	if sec == nil {
		t.Fatal("CreateSecret 返回 nil")
	}
	var atRest string
	if err := w.db.QueryRow(
		"SELECT value FROM config_secrets WHERE tenant_id = ? AND key_name = ?", tenant, secKey,
	).Scan(&atRest); err != nil {
		t.Fatalf("读取 config_secrets.value 原始列失败: %v", err)
	}
	if atRest == "plain-secret-value" {
		t.Fatal("机密以明文落库——加密路径没生效（这正是 TD-65 关注的「接线成功 ≠ 真落库」形态）")
	}
	if !strings.HasPrefix(atRest, "enc:v1:") {
		t.Fatalf("机密密文缺少版本前缀 enc:v1:（读侧靠它区分密文与历史明文存量）: %q", atRest)
	}
	back, ok := r.GetSecret(tenant, secKey)
	if !ok || back == nil {
		t.Fatal("新实例 GetSecret 未命中")
	}
	if back.Value != "plain-secret-value" {
		t.Fatalf("机密读回 = %q, want 明文 %q（密文解不开说明 ENCRYPTION_KEY 不一致）", back.Value, "plain-secret-value")
	}

	// 5) 删除语义：删掉后同一实例与新实例都必须查不到。
	if !w.DeleteConfig(tenant, cfgKey) {
		t.Fatal("DeleteConfig 返回 false")
	}
	if _, ok := r.GetConfig(tenant, cfgKey); ok {
		t.Fatal("删除后仍能查到配置项——DELETE 没有落到库")
	}
	if !w.DeleteSecret(tenant, secKey) {
		t.Fatal("DeleteSecret 返回 false")
	}
	if _, ok := r.GetSecret(tenant, secKey); ok {
		t.Fatal("删除后仍能查到机密")
	}

	// 6) 升级兼容：库里存在的「历史明文存量」（无 enc:v1: 前缀）必须可读（原样返回），
	//    且一次轮换后就变成密文落库——否则升级当天所有旧机密都会读不出来。
	legacyKey := "legacy-plain-secret"
	if _, err := w.db.Exec(
		`INSERT INTO config_secrets (tenant_id, key_name, value, key_type, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, NOW(), NOW())`, tenant, legacyKey, "legacy-plaintext-value", "aes", 1,
	); err != nil {
		t.Fatalf("插入历史明文存量失败: %v", err)
	}
	lg, ok := r.GetSecret(tenant, legacyKey)
	if !ok || lg == nil {
		t.Fatal("历史明文存量读不出来——升级会打断所有老机密")
	}
	if lg.Value != "legacy-plaintext-value" {
		t.Fatalf("历史明文读回 = %q, want 原样返回", lg.Value)
	}
	if meta := w.RotateSecret(tenant, legacyKey, "rotated-value"); meta == nil {
		t.Fatal("RotateSecret 返回 nil")
	}
	var rotatedAtRest string
	if err := w.db.QueryRow(
		"SELECT value FROM config_secrets WHERE tenant_id = ? AND key_name = ?", tenant, legacyKey,
	).Scan(&rotatedAtRest); err != nil {
		t.Fatalf("轮换后读原始列失败: %v", err)
	}
	if !strings.HasPrefix(rotatedAtRest, "enc:v1:") {
		t.Fatalf("轮换后仍未加密落库: %q", rotatedAtRest)
	}
	if after, ok := r.GetSecret(tenant, legacyKey); !ok || after == nil || after.Value != "rotated-value" {
		t.Fatalf("轮换后读回 = %+v, want rotated-value", after)
	}

	// 7) 错密钥必须硬失败（返回 not-found），绝不能把密文当明文交给调用方。
	wrong, err := NewMySQLStore(dsn, "a-totally-different-key-9876543210", 50)
	if err != nil {
		t.Fatalf("NewMySQLStore(wrong key): %v", err)
	}
	defer func() { _ = wrong.db.Close() }()
	if v, ok := wrong.GetSecret(tenant, secKey); ok || v != nil {
		t.Fatalf("错密钥竟然读出了机密（密文被当明文返回）: %+v", v)
	}

	t.Logf("ROUNDTRIP_OK: config_entries/config_history/config_secrets 三表 + 密文前缀 + 历史明文兼容 + 错密钥硬失败")
}
