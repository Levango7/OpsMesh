// parent_dsn_test.go SQL 集成测试的 DSN 工具（自 sqlstore/migration_test.go 复制，TD-61 批次 3-sql）。
//
// 为什么复制：跨包测试无法共享 _test.go helper；parent 侧仅 multi_schema_smoke_test.go 的 MySQL 分支在用。
package store

import (
	"database/sql"
	"strings"
)

// stripDBName 从 DSN 中去掉 dbname，保留 ?params，用于连 mysql 不指定库。
// user:pass@tcp(host:port)/dbname?params → user:pass@tcp(host:port)/?params
// 注意：go-sql-driver 要求 dbname 分隔符 "/" 必须存在（空库名也要保留），
// 否则报 "missing the slash separating the database name"。
func stripDBName(dsn string) string {
	idx := strings.LastIndex(dsn, "/")
	if idx == -1 {
		return dsn
	}
	head := dsn[:idx]
	tail := dsn[idx+1:]
	qIdx := strings.Index(tail, "?")
	if qIdx == -1 {
		return head + "/"
	}
	return head + "/" + tail[qIdx:]
}

// dropTestDB 删除测试用临时库。
func dropTestDB(adminDSN, dbName string) {
	db, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec("DROP DATABASE IF EXISTS " + dbName)
}
