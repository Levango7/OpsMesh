package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var taskSchema string

const batchColumnQuery = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`

// ensureColumns 既有库补列：MySQL 8 没有 ADD COLUMN IF NOT EXISTS，
// information_schema 预检 + ALTER + 1060 竞态处理（另一个副本可能已并发补上）。
// 列清单一处声明，migrateTasks 对 tasks 逐列执行——防止「DDL 与查询列清单漂移」
// 时既有库永远拿不到新列（occurred_at / batch_id 同族缺陷的根治面）。
var taskEnsureColumns = []struct{ name, ddl string }{
	{"batch_id", "ALTER TABLE tasks ADD COLUMN batch_id VARCHAR(64) DEFAULT ''"},
	// AllTasks() 与 fire/reclaim 闭包读写该列（fire 的同分钟去重依赖它回写）；
	// 旧库若拿不到这一列，AllTasks 静默失败 → 调度整轮空转（AllTasks 不打日志）。
	{"last_fired_at", "ALTER TABLE tasks ADD COLUMN last_fired_at TIMESTAMP NULL"},
}

func (s *MySQLStore) ensureColumns(table string, cols []struct{ name, ddl string }) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, col := range cols {
		var count int
		if err := s.db.QueryRowContext(ctx, batchColumnQuery, table, col.name).Scan(&count); err != nil {
			return fmt.Errorf("check %s.%s: %w", table, col.name, err)
		}
		if count > 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, col.ddl); err != nil {
			var mysqlErr *mysql.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1060 {
				if checkErr := s.db.QueryRowContext(ctx, batchColumnQuery, table, col.name).Scan(&count); checkErr != nil {
					return fmt.Errorf("recheck %s.%s after concurrent migration: %w", table, col.name, checkErr)
				}
				if count > 0 {
					continue
				}
			}
			return fmt.Errorf("add %s.%s: %w", table, col.name, err)
		}
	}
	return nil
}

// migrateTasks 只初始化 tasks 表并补 batch_id，不修改共享库的其他表。
// CREATE 使用嵌入的 schema.sql，避免维护第二份易产生差异的建表定义。
func (s *MySQLStore) migrateTasks() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const prefix = "CREATE TABLE IF NOT EXISTS tasks ("
	start := strings.Index(taskSchema, prefix)
	if start < 0 {
		return fmt.Errorf("tasks schema statement not found")
	}
	statement, _, ok := strings.Cut(taskSchema[start:], ";")
	if !ok {
		return fmt.Errorf("tasks schema statement is incomplete")
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create tasks table: %w", err)
	}

	return s.ensureColumns("tasks", taskEnsureColumns)
}

// migrateSchedules 初始化 schedules 表。
// CREATE 使用嵌入的 schema.sql，避免维护第二份易产生差异的建表定义。
// 与 migrateTasks 同风格：从 schema.sql 中提取 schedules 的 CREATE TABLE 语句执行。
func (s *MySQLStore) migrateSchedules() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const prefix = "CREATE TABLE IF NOT EXISTS schedules ("
	start := strings.Index(taskSchema, prefix)
	if start < 0 {
		return fmt.Errorf("schedules schema statement not found")
	}
	statement, _, ok := strings.Cut(taskSchema[start:], ";")
	if !ok {
		return fmt.Errorf("schedules schema statement is incomplete")
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create schedules table: %w", err)
	}
	return nil
}
