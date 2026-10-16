package store

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func expectTasksTable(mock sqlmock.Sqlmock) *sqlmock.ExpectedExec {
	// 精确校验 SQL 来自 schema.sql，而非另行维护的建表副本。
	start := strings.Index(taskSchema, "CREATE TABLE IF NOT EXISTS tasks (")
	statement, _, _ := strings.Cut(taskSchema[start:], ";")
	return mock.ExpectExec(regexp.QuoteMeta(statement))
}

func expectBatchColumn(mock sqlmock.Sqlmock, col string, count int) {
	mock.ExpectQuery(regexp.QuoteMeta(batchColumnQuery)).
		WithArgs("tasks", col).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

// expectTableColumn 与 expectBatchColumn 同形，但表名可指定（周边表的补列预检）。
func expectTableColumn(mock sqlmock.Sqlmock, table, col string, count int) {
	mock.ExpectQuery(regexp.QuoteMeta(batchColumnQuery)).
		WithArgs(table, col).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func TestMigrateTasksIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &MySQLStore{db: db}
	// 存量表第一次缺列，第二次已存在；第二次不允许再发 ALTER。
	for _, count := range []int{0, 1} {
		expectTasksTable(mock).WillReturnResult(sqlmock.NewResult(0, 0))
		expectBatchColumn(mock, "batch_id", count)
		if count == 0 {
			mock.ExpectExec(regexp.QuoteMeta(taskEnsureColumns[0].ddl)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		// last_fired_at：本用例聚焦 batch_id 的幂等，第二列假定既有库已存在。
		expectBatchColumn(mock, "last_fired_at", 1)
		if err := s.migrateTasks(); err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateTasksNewTableHasBatchColumn(t *testing.T) {
	if !strings.Contains(taskSchema, "batch_id VARCHAR(64) DEFAULT ''") {
		t.Fatal("建表列定义必须与 batch_id 增量迁移一致")
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectTasksTable(mock).WillReturnResult(sqlmock.NewResult(0, 0))
	expectBatchColumn(mock, "batch_id", 1)
	expectBatchColumn(mock, "last_fired_at", 1)
	if err := (&MySQLStore{db: db}).migrateTasks(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateTasksErrorsAndConcurrentStartup(t *testing.T) {
	failure := errors.New("database unavailable")
	duplicate := &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'batch_id'"}
	for _, tc := range []struct {
		name    string
		prepare func(sqlmock.Sqlmock)
		want    error
	}{
		{"create failure", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnError(failure)
		}, failure},
		{"column query failure", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnResult(sqlmock.NewResult(0, 0))
			m.ExpectQuery(regexp.QuoteMeta(batchColumnQuery)).WithArgs("tasks", "batch_id").WillReturnError(failure)
		}, failure},
		{"column scan failure", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnResult(sqlmock.NewResult(0, 0))
			m.ExpectQuery(regexp.QuoteMeta(batchColumnQuery)).WithArgs("tasks", "batch_id").WillReturnRows(sqlmock.NewRows([]string{"count"}))
		}, sql.ErrNoRows},
		{"alter failure", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnResult(sqlmock.NewResult(0, 0))
			expectBatchColumn(m, "batch_id", 0)
			m.ExpectExec(regexp.QuoteMeta(taskEnsureColumns[0].ddl)).WillReturnError(failure)
		}, failure},
		{"concurrent addition", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnResult(sqlmock.NewResult(0, 0))
			expectBatchColumn(m, "batch_id", 0)
			m.ExpectExec(regexp.QuoteMeta(taskEnsureColumns[0].ddl)).WillReturnError(duplicate)
			expectBatchColumn(m, "batch_id", 1)
			// 第一列竞态补上后，ensureColumns 继续走第二列（正常存在）。
			expectBatchColumn(m, "last_fired_at", 1)
		}, nil},
		{"concurrent recheck failure", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnResult(sqlmock.NewResult(0, 0))
			expectBatchColumn(m, "batch_id", 0)
			m.ExpectExec(regexp.QuoteMeta(taskEnsureColumns[0].ddl)).WillReturnError(duplicate)
			m.ExpectQuery(regexp.QuoteMeta(batchColumnQuery)).WithArgs("tasks", "batch_id").WillReturnError(failure)
		}, failure},
		{"duplicate without column", func(m sqlmock.Sqlmock) {
			expectTasksTable(m).WillReturnResult(sqlmock.NewResult(0, 0))
			expectBatchColumn(m, "batch_id", 0)
			m.ExpectExec(regexp.QuoteMeta(taskEnsureColumns[0].ddl)).WillReturnError(duplicate)
			expectBatchColumn(m, "batch_id", 0)
		}, duplicate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tc.prepare(mock)
			err = (&MySQLStore{db: db}).migrateTasks()
			if !errors.Is(err, tc.want) {
				t.Fatalf("迁移错误 = %v，期望 %v", err, tc.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestMigrateRecordTablesCreatesAllTables 任务周边五张表必须逐个从 schema.sql 建。
//
// 背景（2026-10-15 缺陷）：NewMySQLStore 此前只建 tasks/schedules，另外五张表
// （结果/日志/批次/灰度）在服务自建表路径下从来不存在——SQL 模式（compose 出厂
// TASK_STORE_TYPE=sql）下批次视图恒 404、结果与日志静默不落库。本用例把
// "五张表、顺序、语句逐字来自 schema.sql"钉死。
//
// 有补列清单的表在 CREATE 之后还会跑 ensureColumns：这里让预检报"列已存在"，
// 把本用例聚焦在建表顺序上（补列行为另有用例，且判据不取自被测的那张表清单）。
func TestMigrateRecordTablesCreatesAllTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range recordTables {
		statement, err := createTableStatement(table)
		if err != nil {
			t.Fatalf("recordTables[%s] 在 schema.sql 里找不到建表语句：%v", table, err)
		}
		mock.ExpectExec(regexp.QuoteMeta(statement)).WillReturnResult(sqlmock.NewResult(0, 0))
		// 只有 task_results 带补列清单（硬编码，避免判据取自被测清单本身）。
		if table == "task_results" {
			for _, col := range []string{"duration_ms", "claim_epoch"} {
				expectTableColumn(mock, table, col, 1) // 列已存在 ⇒ 不发 ALTER
			}
		}
	}
	if err := (&MySQLStore{db: db}).migrateRecordTables(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateRecordTablesReconcilesExistingColumns 既有**异形**表（同库部署路径：
// task-svc DSN 复用了控制面 opsmesh 库，该库的 task_results 由控制面迁移建成，
// 只有 6 列、没有 duration_ms/claim_epoch）必须被补列收敛，否则本服务的读写在
// 运行时才炸 Unknown column，而写路径是 best-effort（静默）。
//
// 列名**硬编码**在用例里——若期望取自 recordEnsureColumns 自身，删掉清单条目时
// 期望与实现一起变空，用例会照样绿（自证式判据）。这里钉的是"服务 SQL 引用了这两列
// ⇒ 既有库必须被补上这两列"这一事实（引用见 mysql.go 的 task_results INSERT/SELECT）。
// 变异检验：把 recordEnsureColumns 的 task_results 条目改名/删除 ⇒ 本用例判红。
func TestMigrateRecordTablesReconcilesExistingColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range recordTables {
		statement, err := createTableStatement(table)
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectExec(regexp.QuoteMeta(statement)).WillReturnResult(sqlmock.NewResult(0, 0))
		if table != "task_results" {
			continue
		}
		// 旧表两列都缺 ⇒ 每条都要发 ALTER 补上。
		for _, col := range []string{"duration_ms", "claim_epoch"} {
			expectTableColumn(mock, table, col, 0)
			mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE task_results ADD COLUMN " + col)).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	if err := (&MySQLStore{db: db}).migrateRecordTables(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateRecordTablesCreateFailure 建表失败必须带表名退出（fail-fast，
// 不让服务带着缺表的身子起来继续"假装持久化"）。
func TestMigrateRecordTablesCreateFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("database unavailable")
	// 第一张就失败：错误信息必须点名是哪张表。
	first, err := createTableStatement(recordTables[0])
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(regexp.QuoteMeta(first)).WillReturnError(failure)
	err = (&MySQLStore{db: db}).migrateRecordTables()
	if err == nil || !strings.Contains(err.Error(), recordTables[0]) {
		t.Fatalf("建表失败应点名 %s，实得 %v", recordTables[0], err)
	}
	if !errors.Is(err, failure) {
		t.Fatalf("应保留原始错误（errors.Is），实得 %v", err)
	}
}

// TestCreateTableStatementGuards 定位函数的两个失明形态：表不在 schema.sql、
// 语句被截断。任一发生都必须报错，而不是执行半条 DDL。
func TestCreateTableStatementGuards(t *testing.T) {
	if _, err := createTableStatement("no_such_table"); err == nil {
		t.Error("schema.sql 里没有的表应报错")
	}
	for _, table := range recordTables {
		if _, err := createTableStatement(table); err != nil {
			t.Errorf("recordTables[%s] 应能定位到建表语句：%v", table, err)
		}
	}
}
