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
