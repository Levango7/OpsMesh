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

// TestRecordTablesExistAfterNewMySQLStore 是 migrateRecordTables 的真库判据：
// NewMySQLStore 之后，任务周边五张表（结果/日志/批次/灰度）必须真的存在。
//
// 缺陷原型（2026-10-15 修复前）：NewMySQLStore 只建 tasks 与 schedules，另外五张表
// 在「服务自建表」路径下从来不存在——而写路径多为 best-effort（只打日志），
// SQL 模式（compose 出厂 TASK_SVC_STORE_TYPE=sql）下批次视图恒 404、执行结果
// 与日志静默不落库。这类"缺表"缺陷在单测层永远绿（sqlmock 不校验表存在），
// 只有真库能证。
//
// 门控：OPSMESH_TEST_MYSQL_DSN 缺失时 t.Skip 并打印原因。
func TestRecordTablesExistAfterNewMySQLStore(t *testing.T) {
	dsn, ok := os.LookupEnv("OPSMESH_TEST_MYSQL_DSN")
	if !ok {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open test mysql: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}

	st, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v", err)
	}
	defer st.Close()

	for _, table := range recordTables {
		var count int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			table,
		).Scan(&count); err != nil {
			t.Fatalf("information_schema 查询 %s 失败: %v", table, err)
		}
		if count != 1 {
			t.Errorf("NewMySQLStore 之后 %s 表不存在（count=%d）——该表上的读写会 Unknown table", table, count)
		}
	}
}

// TestCanaryRecordSurvivesRestartMySQL 灰度记录的真库重启判据（R6 任务侧）：
// 建 → 换一个 store 实例（= 重启）→ 仍按 canaryID 查回，且阶段结构完整；
// 推进后**再重启**，推进结果仍在（写穿生效）。
//
// 修前形态：canary store 由 main 无条件注入 MemoryStore，SQL 模式下灰度记录
// 重启即丢——批次视图能落的库，灰度落不下去。
func TestCanaryRecordSurvivesRestartMySQL(t *testing.T) {
	dsn, ok := os.LookupEnv("OPSMESH_TEST_MYSQL_DSN")
	if !ok {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}

	canaryID := "itest-canary-" + fmt.Sprintf("%06d", time.Now().UTC().UnixMicro()%1000000)
	cleanup := func(t *testing.T, db *sql.DB) {
		if _, err := db.Exec("DELETE FROM canary_releases WHERE canary_id = ?", canaryID); err != nil {
			t.Logf("cleanup 失败（不影响判据）: %v", err)
		}
	}

	st1, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore #1: %v", err)
	}
	defer st1.Close()
	created := sampleCanaryRecord(canaryID)
	if got := st1.CreateCanary(created); got == nil {
		t.Fatal("CreateCanary 返回 nil（写入失败）")
	}

	// 重启 #1：新 store 实例，内存索引为空
	st2, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore #2: %v", err)
	}
	defer st2.Close()
	back := st2.GetCanary(canaryID)
	if back == nil {
		t.Fatal("重启后灰度记录读不回（未落库或键不一致）")
	}
	if len(back.Phases) != 2 || back.Phases[0].Tasks[0].TaskID != "task-1" {
		t.Errorf("阶段结构不完整：%+v", back.Phases)
	}

	// 推进第一阶段，然后重启 #2：推进结果必须仍在
	back.Phases[0].Status = "done"
	back.Phases[1].Status = "running"
	st2.UpdateCanary(back)

	st3, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore #3: %v", err)
	}
	defer st3.Close()
	after := st3.GetCanary(canaryID)
	if after == nil || after.Phases[0].Status != "done" || after.Phases[1].Status != "running" {
		t.Errorf("推进结果未写穿（二次重启后倒回）：%+v", after)
	}
	cleanup(t, st3.db)

	// 跨租户隔离：另一个租户查不到这条记录（service 层同样校验，这里证明库层可按租户过滤）
	if got := st3.ListCanaries("other-tenant"); len(got) != 0 {
		t.Errorf("other-tenant 不应读到 t1 的灰度，实得 %d 条", len(got))
	}
}

// TestBatchRecordRoundTripMySQL 批次视图的真库往返（同一缺表缺陷的用户可见面）：
// 建批次 → 挂任务 → 查状态 → 改状态。修前 SQL 模式下 batches/batch_tasks 表不存在，
// CreateBatch 的 INSERT 静默失败（返回 nil）、GetBatchStatus 恒 404——
// 用户侧的形态是"批量执行接口说建好了，一查就 404"。
func TestBatchRecordRoundTripMySQL(t *testing.T) {
	dsn, ok := os.LookupEnv("OPSMESH_TEST_MYSQL_DSN")
	if !ok {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}

	st, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v", err)
	}
	defer st.Close()

	batchID := "itest-batch-" + fmt.Sprintf("%06d", time.Now().UTC().UnixMicro()%1000000)
	batch := &models.BatchTask{
		BatchID:      batchID,
		TenantID:     "t1",
		Name:         "r6 batch",
		TotalCount:   2,
		PendingCount: 2,
		Status:       models.BatchStatusPending,
		CreatedAt:    time.Now().UTC(),
	}
	if got := st.CreateBatch(batch); got == nil {
		t.Fatal("CreateBatch 返回 nil（batches 表缺失或写入失败）")
	}
	st.AddTaskToBatch(batchID, "task-a")
	st.AddTaskToBatch(batchID, "task-b")

	back := st.GetBatch(batchID)
	if back == nil || back.TotalCount != 2 || back.Status != models.BatchStatusPending {
		t.Fatalf("GetBatch 读回不符：%+v", back)
	}
	if ids := st.GetBatchTasks(batchID); len(ids) != 2 {
		t.Errorf("GetBatchTasks 应读到 2 个任务，实得 %d", len(ids))
	}
	back.SuccessCount, back.PendingCount, back.Status = 2, 0, models.BatchStatusDone
	st.UpdateBatch(back)
	if again := st.GetBatch(batchID); again == nil || again.Status != models.BatchStatusDone {
		t.Errorf("UpdateBatch 未生效：%+v", again)
	}
	if got := st.ListBatches("t1"); len(got) == 0 {
		t.Error("ListBatches 至少应含刚建的批次")
	}

	t.Cleanup(func() {
		_, _ = st.db.Exec("DELETE FROM batch_tasks WHERE batch_id = ?", batchID)
		_, _ = st.db.Exec("DELETE FROM batches WHERE batch_id = ?", batchID)
	})
}

// sampleCanaryRecord 造一条两阶段灰度记录（与单测同形，避免两份构造漂移）。
func sampleCanaryRecord(canaryID string) *models.CanaryRelease {
	now := time.Now().UTC()
	return &models.CanaryRelease{
		CanaryID:   canaryID,
		TenantID:   "t1",
		TaskType:   models.TaskTypeShell,
		Command:    "echo r6",
		Strategy:   "percentage",
		Percentage: 50,
		CreatedAt:  now,
		CreatedBy:  "u-admin",
		Phases: []models.CanaryPhase{
			{Phase: 1, DeviceIDs: []string{"dev-0"}, Status: "running",
				Tasks: []models.BatchTaskItem{{DeviceID: "dev-0", TaskID: "task-1", Status: "running"}}},
			{Phase: 2, DeviceIDs: []string{"dev-1"}, Status: "pending"},
		},
	}
}

// TestRecordTablesReconcileSharedDBShapeMySQL 同库部署路径的真库判据：
// 先用**控制面形态**的 task_results（6 列、无 duration_ms/claim_epoch，且已有历史行）
// 建表，再跑 NewMySQLStore（= task-svc 启动），断言两列被补上、历史行仍在且新列为默认 0。
//
// 为什么必须真库：CREATE TABLE IF NOT EXISTS 对异形既有表是**静默 no-op**，
// sqlmock 不校验表存在与列存在，只有真库能把"补列没发生 ⇒ 运行时 Unknown column"照出来。
func TestRecordTablesReconcileSharedDBShapeMySQL(t *testing.T) {
	dsn, ok := os.LookupEnv("OPSMESH_TEST_MYSQL_DSN")
	if !ok {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open test mysql: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}

	// 控制面 internal/store/sqlstore/migrations/001_initial.sql 的 task_results 形态。
	if _, err := db.Exec("DROP TABLE IF EXISTS task_results"); err != nil {
		t.Fatalf("drop task_results: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE task_results (
		task_id VARCHAR(64) PRIMARY KEY,
		agent_id VARCHAR(64),
		exit_code INT,
		stdout MEDIUMTEXT,
		stderr MEDIUMTEXT,
		finished_at DATETIME
	)`); err != nil {
		t.Fatalf("create controlplane-shaped task_results: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO task_results (task_id, agent_id, exit_code) VALUES (?, ?, ?)",
		"cp-legacy-row", "agent-cp", 0,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	for _, col := range []string{"duration_ms", "claim_epoch"} {
		if columnExists(t, db, "task_results", col) {
			t.Fatalf("precondition failed: task_results.%s already exists", col)
		}
	}

	// task-svc 启动路径
	st, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore（应在既有异形表上补列而不是失败）: %v", err)
	}
	defer st.Close()

	for _, col := range []string{"duration_ms", "claim_epoch"} {
		if !columnExists(t, db, "task_results", col) {
			t.Errorf("补列未发生：task_results.%s 仍不存在（同库部署下本服务的读写会在运行时 Unknown column）", col)
		}
	}
	// 历史行必须还在，且新列取到默认 0（补列 DDL 带 NOT NULL DEFAULT 的依据）。
	var duration, epoch int64
	if err := db.QueryRow(
		"SELECT duration_ms, claim_epoch FROM task_results WHERE task_id = ?", "cp-legacy-row",
	).Scan(&duration, &epoch); err != nil {
		t.Fatalf("既有行读不出（补列把行丢了？）: %v", err)
	}
	if duration != 0 || epoch != 0 {
		t.Errorf("既有行新列初值应为 0，实得 duration_ms=%d claim_epoch=%d", duration, epoch)
	}
	// 补列后本服务能正常写入并读回（原先 Unknown column 的路径）。
	r := &models.TaskResult{TaskID: "cp-legacy-row", AgentID: "agent-cp", ExitCode: 0, DurationMs: 1234, ClaimEpoch: 7}
	st.SaveResult(r)
	got := st.GetTaskResult("cp-legacy-row")
	if got == nil || got.DurationMs != 1234 || got.ClaimEpoch != 7 {
		t.Errorf("补列后写入/读回不符：%+v", got)
	}

	// 零值 FinishedAt（任务未跑完的形态）必须能落库：driver 会把 time.Time{} 序列化成
	// '0000-00-00'，MySQL 严格模式报 1292 ⇒ 整行写不进去（SaveResult 只留痕 ⇒ 静默）。
	// 这里显式钉住"零值也要写得进"。
	zero := &models.TaskResult{TaskID: "cp-legacy-row-zero", AgentID: "agent-cp", DurationMs: 5}
	st.SaveResult(zero)
	if gz := st.GetTaskResult("cp-legacy-row-zero"); gz == nil || gz.DurationMs != 5 {
		t.Errorf("零值 FinishedAt 的结果未落库（被 1292 挡掉）：%+v", gz)
	}

	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS task_results") })
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
