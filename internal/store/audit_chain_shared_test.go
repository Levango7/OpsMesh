// audit_chain_shared_test.go 为审计链集成用例提供「包内共享临时库」与退出清理。
//
// 为什么共享：每个集成用例各建一个临时库都要跑全部 19 个迁移，实测约 10s/用例
// （单用例 11.9s 中 10s 是建库+迁移开销），8 个用例≈80s 纯开销。CI 的 integration job
// 在 -race 下跑整个 store 包（-timeout 900s），这份开销是实打实的超时风险。
// 共享后只首次建库+跑迁移，用例之间用 TRUNCATE 复位审计相关 4 张表。
// 清理：TestMain 退出时 DROP 共享库（不留测试残留）。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

var (
	auditChainSharedOnce  sync.Once
	auditChainSharedStore *SQLStore
	auditChainSharedName  string // 共享临时库名（清理用）
	auditChainSharedAdmin string // 去掉库名的 DSN（DROP DATABASE 用）
	auditChainSharedErr   error
)

// TestMain 在本包测试全部结束后清理共享临时库。
func TestMain(m *testing.M) {
	code := m.Run()
	if auditChainSharedName != "" {
		dropTestDB(auditChainSharedAdmin, auditChainSharedName)
	}
	os.Exit(code)
}

// auditChainTestStore 返回包内共享的审计链测试库，并把 4 张审计相关表复位到初始态。
//
// 复位用 TRUNCATE（同时重置 AUTO_INCREMENT，使各用例的行号断言彼此独立）；
// audit_chain_head / audit_archive_meta 的单行由被测代码的 INSERT IGNORE 惰性重建，
// 因此直接清空即可——这也顺带验证了「链头缺失时可自愈初始化」。
func auditChainTestStore(t *testing.T) *SQLStore {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping audit chain integration test")
	}
	auditChainSharedOnce.Do(func() {
		auditChainSharedStore, auditChainSharedName, auditChainSharedAdmin, auditChainSharedErr =
			newSharedAuditChainStore(dsn)
	})
	if auditChainSharedErr != nil {
		t.Fatalf("初始化共享审计测试库失败: %v", auditChainSharedErr)
	}
	for _, tbl := range []string{"audit_log", "audit_log_archive", "audit_chain_head", "audit_archive_meta"} {
		if _, err := auditChainSharedStore.db.Exec("TRUNCATE TABLE " + tbl); err != nil {
			t.Fatalf("复位表 %s 失败: %v", tbl, err)
		}
	}
	return auditChainSharedStore
}

// newSharedAuditChainStore 建一个进程内共享的临时库并跑完迁移。
func newSharedAuditChainStore(dsn string) (*SQLStore, string, string, error) {
	adminDSN := stripDBName(dsn)
	dbName := fmt.Sprintf("test_auditchain_%d", time.Now().UnixNano())
	adminDB, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return nil, "", "", fmt.Errorf("open admin db: %w", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + dbName); err != nil {
		adminDB.Close()
		return nil, "", "", fmt.Errorf("create temp db %s: %w", dbName, err)
	}
	adminDB.Close()
	s, err := NewSQLStore(withDBName(dsn, dbName), "", "")
	if err != nil {
		dropTestDB(adminDSN, dbName)
		return nil, "", "", fmt.Errorf("NewSQLStore: %w", err)
	}
	return s, dbName, adminDSN, nil
}
