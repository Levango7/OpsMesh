package store

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
)

// TestTaskStoreLastFiredAtRoundTrip 是旧库无 last_fired_at 列时的真库往返校验。
//
// 验的是「列不存在 → ensureColumns 补上 → UpdateTask 回写 → AllTasks 读回」闭环，
// 而且读回值是写入时间（不是内存-only、不是回写前的零值）。
//
// 门控：OPSMESH_TEST_MYSQL_DSN 缺失时 t.Skip 并打印原因——CI 的 services/integration
// job 若没有起 mysql:8，这个用例就不该被误判为“跑过”。
func TestTaskStoreLastFiredAtRoundTrip(t *testing.T) {
	if _, ok := os.LookupEnv("OPSMESH_TEST_MYSQL_DSN"); !ok {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}

	db, err := sql.Open("mysql", os.Getenv("OPSMESH_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatalf("open test mysql: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}

	// 起见：先把 tasks 表drop掉（如果存在），这样每次都能从「列不存在」开始。
	if _, err := db.Exec("DROP TABLE IF EXISTS tasks"); err != nil {
		t.Fatalf("DROP TABLE IF EXISTS tasks: %v", err)
	}

	// 第 1 步：创建表（不含 last_fired_at）——纯靠 schema.sql 里的 CREATE TABLE。
	// 这个语义就是「旧库刚建表，还没补列」的起点。
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("CREATE TABLE tasks: %v", err)
	}

	// 第 2 步：确认此时列还不存在。
	if columnExists(t, db, "tasks", "last_fired_at") {
		t.Fatal("precondition failed: tasks.last_fired_at already exists after CREATE TABLE")
	}

	s := &MySQLStore{db: db}

	// 第 3 步：ensureColumns 应该补上 last_fired_at。
	if err := s.ensureColumns("tasks", taskEnsureColumns); err != nil {
		t.Fatalf("ensureColumns(tasks): %v", err)
	}
	if !columnExists(t, db, "tasks", "last_fired_at") {
		t.Fatal("after ensureColumns, tasks.last_fired_at still does not exist")
	}
	// 验证：ensureColumns 补列后，表里确实有 last_fired_at 这一列。
	// 这里再查一次，是为了确保我们读到的信息不是来自缓存/竞态。
	if count := columnCount(t, db, "tasks", "last_fired_at"); count != 1 {
		t.Fatalf("after ensureColumns, tasks.last_fired_at column count = %d (expected 1)", count)
	}

	// 第 4 步：写入一份任务（携带 last_fired_at），随后**原值回写**——这是「无变化
	// 更新」，MySQL 返回 RowsAffected=0。UpdateTask 必须返回 true：它的语义是
	// 「状态已在库中」，不是「行数有变化」。此处收紧的理由是调用方 main.go 的
	// fire/reclaim 循环把 false 计为失败并出 task_scheduled_fire_failures 指标
	// （见 mysql.go UpdateTask 末注释），无操作更新被误判会让指标说谎。
	firedAt := time.Now().UTC().Truncate(time.Minute).Add(5 * time.Minute)
	task := &models.Task{
		TaskID:      "itest-last-fired-at-" + fmt.Sprintf("%06d", time.Now().UTC().UnixMicro()%1000000),
		TenantID:    "t1",
		Type:        models.TaskTypeShell,
		Command:     "echo ok",
		DependsOn:   []string{},
		LastFiredAt: firedAt,
	}
	if _, err := s.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if !s.UpdateTask(task) {
		t.Fatalf("无变化回写应返回 true（RowsAffected=0 只表示值未变，不代表失败）；LastFiredAt=%v, DependsOn=%v", task.LastFiredAt, task.DependsOn)
	}

	// 第 5 步：真变化回写（把 last_fired_at 改成另一个值）→ true，且列仍然存在
	// （防「写到了不存在的列」这类幻影列形态）。
	firedAt2 := firedAt.Add(10 * time.Minute)
	task.LastFiredAt = firedAt2
	if !s.UpdateTask(task) {
		t.Fatalf("真变化回写应返回 true；LastFiredAt=%v", task.LastFiredAt)
	}
	if !columnExists(t, db, "tasks", "last_fired_at") {
		t.Fatal("after UpdateTask, tasks.last_fired_at does not exist - write may have hit a phantom column")
	}

	// 第 6 步：AllTasks() 必须把该列读回来。
	tasks := s.AllTasks()
	if len(tasks) == 0 {
		t.Fatal("AllTasks returned no rows after writes")
	}

	// 第 7 步：断言读回值等于**最后一次写入值**（证明不是内存-only 假读回）。
	found := false
	for _, tsk := range tasks {
		if tsk.TaskID == task.TaskID {
			found = true
			if tsk.LastFiredAt.IsZero() {
				t.Fatal("AllTasks read back last_fired_at as zero time - column was not really written/read")
			}
			if !tsk.LastFiredAt.Equal(firedAt2) {
				t.Fatalf("last_fired_at mismatch: wrote %v, read %v", firedAt2, tsk.LastFiredAt)
			}
			break
		}
	}
	if !found {
		t.Fatal("the row we wrote for last_fired_at roundtrip was not returned by AllTasks")
	}

	// 第 7b 步：行不存在 → false（false 的另半边语义：命中不到行才算失败）。
	if _, err := db.Exec("DELETE FROM tasks WHERE task_id = ?", task.TaskID); err != nil {
		t.Fatalf("cleanup DELETE: %v", err)
	}
	if s.UpdateTask(task) {
		t.Fatal("目标行已删除，UpdateTask 应返回 false（行不存在才算失败）")
	}

	// 第 8 步：信息模式断言——last_fired_at 落在 batch_id 之后。
	// 写这个断言是因为 ensureColumns 写入的 DDL 是按 taskEnsureColumns 顺序执行的，
	// 而该切片里 batch_id 在前、last_fired_at 在后——但 MySQL ADD COLUMN 的语义是“加到表的
	// 末尾”，所以最终列序应是 batch_id < last_fired_at。若补列语句执行了但目标是别的表，
	// 这个断言会挡住（列序不对或列不存在），从而防止“假补列成功”。
	orderAfter := columnOrder(t, db, "tasks")
	batchOrd, ok := orderAfter["batch_id"]
	if !ok {
		t.Fatal("information_schema: tasks.batch_id not found after migration")
	}
	lastOrd, ok := orderAfter["last_fired_at"]
	if !ok {
		t.Fatal("information_schema: tasks.last_fired_at not found after migration")
	}
	if lastOrd <= batchOrd {
		t.Fatalf("column order invariant violated: batch_id ordinal=%d, last_fired_at ordinal=%d (expected last_fired_at > batch_id)", batchOrd, lastOrd)
	}

	t.Logf("ROUNDTRIP_OK: last_fired_at add-column / write-back / read-back all pass; ordinal last_fired_at=%d; batch_id=%d", lastOrd, batchOrd)
}

// columnExists 从 information_schema.columns 读指定表是否存在某列。
// 走 raw SQL 是为了不让业务封装层把“查不到”伪装成其它语义，从而掩盖
// “列真的没补上”这个失败形态。
func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, col,
	).Scan(&count)
	if err != nil {
		t.Fatalf("columnExists(%s.%s) query failed: %v", table, col, err)
	}
	return count > 0
}

// columnCount 返回指定表里名为 col 的列出现次数（信息模式语义下应为 0 或 1）。
func columnCount(t *testing.T, db *sql.DB, table, col string) int {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, col,
	).Scan(&count)
	if err != nil {
		t.Fatalf("columnCount(%s.%s) query failed: %v", table, col, err)
	}
	return count
}

// columnOrder 返回指定表的 information_schema 列序映射（列名 → ordinal_position）。
// 用于断言补列语义是否符合预期（例如 last_fired_at 是否真的落在 batch_id 之后）。
func columnOrder(t *testing.T, db *sql.DB, table string) map[string]int {
	rows, err := db.Query(
		"SELECT column_name, ordinal_position FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position",
		table,
	)
	if err != nil {
		t.Fatalf("columnOrder(%s) query failed: %v", table, err)
	}
	defer rows.Close()

	m := make(map[string]int)
	for rows.Next() {
		var name string
		var pos int
		if err := rows.Scan(&name, &pos); err != nil {
			t.Fatalf("columnOrder(%s) scan failed: %v", table, err)
		}
		m[name] = pos
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("columnOrder(%s) rows iteration failed: %v", table, err)
	}
	return m
}

// schemaSQL 是仅含 tasks 表建表的 SQL（从 schema.sql 里切出来的）。
// 测试里 drop 完表之后，就用它把表再建起来——不经过 migration 层，
// 这样能确保每次都从「旧库刚建表，还没补列」的起点出发。
const schemaSQL = `CREATE TABLE IF NOT EXISTS tasks (
    task_id VARCHAR(64) PRIMARY KEY,
    agent_id VARCHAR(64) DEFAULT '',
    tenant_id VARCHAR(64) NOT NULL,
    type VARCHAR(32) NOT NULL,
    command TEXT,
    content TEXT,
    path VARCHAR(512) DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    claimed_by VARCHAR(64) DEFAULT '',
    claimed_at TIMESTAMP NULL,
    claim_epoch BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    dead_letter TINYINT(1) NOT NULL DEFAULT 0,
    timeout INT DEFAULT 0,
    retry_delay INT DEFAULT 0,
    schedule VARCHAR(64) DEFAULT '',
    parent_id VARCHAR(64) DEFAULT '',
    depends_on JSON,
    approval_required TINYINT(1) NOT NULL DEFAULT 0,
    approved_by VARCHAR(64) DEFAULT '',
    approved_at TIMESTAMP NULL,
    batch_id VARCHAR(64) DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`
