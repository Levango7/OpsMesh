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
const addBatchColumn = `ALTER TABLE tasks ADD COLUMN batch_id VARCHAR(64) DEFAULT ''`

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

	var count int
	if err := s.db.QueryRowContext(ctx, batchColumnQuery, "tasks", "batch_id").Scan(&count); err != nil {
		return fmt.Errorf("check tasks.batch_id: %w", err)
	}
	if count > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, addBatchColumn); err != nil {
		// 检查和 DDL 之间可能被另一个启动中的副本补列。
		// 仅将已确认存在的重复列视为成功，不吞权限或其他迁移错误。
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1060 {
			if checkErr := s.db.QueryRowContext(ctx, batchColumnQuery, "tasks", "batch_id").Scan(&count); checkErr != nil {
				return fmt.Errorf("recheck tasks.batch_id after concurrent migration: %w", checkErr)
			}
			if count > 0 {
				return nil
			}
		}
		return fmt.Errorf("add tasks.batch_id: %w", err)
	}
	return nil
}
