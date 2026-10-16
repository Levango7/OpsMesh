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

// recordEnsureColumns 是「任务周边表」的补列清单（本服务建表语句的列集合 ⊇ 这几张表
// 在本服务 SQL 里的引用列）。
//
// 为什么 task_results 需要它（2026-10-15 复核实测）：task-svc 的 DSN 有两条部署路径，库不同——
//   - compose：专用库 opsmesh_task（表由本服务建，schema.sql 全量列）；
//   - Helm 生产：deploy/helm/opsmesh/templates/microservices.yaml 把 <前缀>_DSN 渲染自
//     控制面的 mysql-dsn Secret（与控制面**同库** opsmesh）。
//
// 同库那一路，控制面的迁移 internal/store/sqlstore/migrations/001_initial.sql 早已建了
// task_results，但只有 6 列（task_id/agent_id/exit_code/stdout/stderr/finished_at），
// 没有 duration_ms 与 claim_epoch。于是本服务的 CREATE TABLE IF NOT EXISTS task_results
// **静默让位**给那张旧表（正是 deploy/docker/scripts/init-databases.sql:9-12 警告过的形态），
// 而本服务的读写在运行时才炸 Unknown column——SaveResult 是 best-effort（只打日志），
// 执行结果就此静默不落库。补列把「新建库」与「与旧表共存」两种形态收敛到同一张表。
//
// 列 DDL 带 NOT NULL DEFAULT：同库旧表可能已有历史行，加列必须给它们一个合法初值
// （0 = 未记录时长/未携带 claim epoch，与 schema.sql 的默认值一致）。
var recordEnsureColumns = map[string][]struct{ name, ddl string }{
	"task_results": {
		{"duration_ms", "ALTER TABLE task_results ADD COLUMN duration_ms BIGINT NOT NULL DEFAULT 0"},
		{"claim_epoch", "ALTER TABLE task_results ADD COLUMN claim_epoch BIGINT NOT NULL DEFAULT 0"},
	},
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

// recordTables 是 migrateRecordTables 负责建的表。顺序即 schema.sql 中的出现顺序
// （表间无外键依赖，顺序只影响失败时的可读性）。
var recordTables = []string{"task_results", "task_logs", "batches", "batch_tasks", "canary_releases"}

// migrateRecordTables 初始化「任务周边」五张表：结果 / 日志 / 批次 / 灰度。
//
// 为什么需要它（2026-10-15 实测登记）：NewMySQLStore 此前只建 tasks 与 schedules，
// 而 schema.sql 里另外五张表在**服务自建表**的部署路径下（opsmesh_task 由
// init-databases.sql 只建库不建表）从来不存在——INSERT/SELECT 一律 Unknown table，
// 且写路径多为 best-effort（SaveResult/SaveLogs/AddTaskToBatch 只打日志）、
// CreateBatch 直接返回 nil ⇒ SQL 模式（compose 出厂 TASK_STORE_TYPE=sql）下
// 批次视图恒 404、执行结果与日志静默不落库、灰度只活在内存索引里。
// 这与 TD-68「声明持久化其实没落库」是同族形态，故按 migrateTasks 的同风格补齐，
// 而不是留给使用方自行建表。
func (s *MySQLStore) migrateRecordTables() error {
	for _, table := range recordTables {
		statement, err := createTableStatement(table)
		if err != nil {
			return err
		}
		// 每张表一个独立 30s 预算（与 migrateTasks/migrateSchedules 一致）：
		// 五张表共用一个预算会让每张只有 ~6s，MDL 争用下后面的表超时即整体启动失败。
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err = s.db.ExecContext(ctx, statement)
		cancel()
		if err != nil {
			return fmt.Errorf("create %s table: %w", table, err)
		}
		// 既有库补列（同库形态，见 recordEnsureColumns 文件头）：CREATE ... IF NOT EXISTS
		// 对已存在的**异形**表是静默 no-op，只有补列能把两种形态收敛到同一张表。
		if cols, ok := recordEnsureColumns[table]; ok {
			if err := s.ensureColumns(table, cols); err != nil {
				return fmt.Errorf("reconcile %s columns: %w", table, err)
			}
		}
	}
	return nil
}

// createTableStatement 从 schema.sql 切出指定表的 CREATE TABLE 语句。
// 建表定义只保留 schema.sql 一份（与 migrateTasks/migrateSchedules 同一原则），
// 这里只做定位与截取，不内联任何 DDL。
func createTableStatement(table string) (string, error) {
	prefix := "CREATE TABLE IF NOT EXISTS " + table + " ("
	start := strings.Index(taskSchema, prefix)
	if start < 0 {
		return "", fmt.Errorf("%s schema statement not found", table)
	}
	statement, _, ok := strings.Cut(taskSchema[start:], ";")
	if !ok {
		return "", fmt.Errorf("%s schema statement is incomplete", table)
	}
	return statement, nil
}
