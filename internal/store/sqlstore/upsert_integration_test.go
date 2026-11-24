// upsert_integration_test.go 全调用点真库 PREPARE + 行为回归。
//
// A3-3 把 45 处通用 upsert 迁移到 Dialect.Upsert 后（Task 6-8），
// 剩余三个语义特例保留专用构建器（upsert_special.go，Task 9）。
// Task 10 对两者输出的 SQL 在 MySQL 与 PostgreSQL 真库上执行
// PREPARE（PrepareContext）：服务端解析成功即证明方言化输出
// 可被双库接受（表/列/冲突目标/占位符数量均合法）。
//
// 本文件（Task 11 重做轮）追加行为测试，均在 MySQL/PG 双库
// 真库执行，验证方言化 UPSERT 的运行期数据行为：
//  1. TestUpsertInsertUpdateReadback    —— 插入/更新/读回，created_at 不被覆盖
//  2. TestUpsertCompositeConflict       —— 复合冲突键写穿
//  3. TestUpsertConstantsAndHeartbeat   —— 常量列覆盖 + 心跳推进
//  4. TestUpsertCrossUniqueKeyIsStrictOnPostgres —— PG 跨唯一键严格性
//  5. TestSubmitResultStateTransitions  —— success/retry/dead-letter 状态机
//     （MySQL-only 公共方法测试；PG 子测试明示 skip 原因）
//  6. TestUpsertTaskResultsWritePath    —— task_results 生成 SQL 双库写路径
//
// 绑定裁决（A3-3 write-path-only）：本轮只方言化 UPSERT 写路径。
// 涉及周围裸 `?`/反引号 A3-2 读路径的公共方法（SubmitResult 等）
// 不得在 PG 上经公共方法执行——改为直接执行生成的 UPSERT/特例
// 构建器并用 ConvertPlaceholders 读回，或降级为 MySQL-only 并
// 明示 skip 理由。禁止修改生产读路径。
//
// 门控：未设置 OPSMESH_TEST_MYSQL_DSN / OPSMESH_TEST_PG_DSN 时
// 对应子测试 skip（沿用 newTestSQLStore/newPostgresTestStore 契约）；
// 设置 DSN 时不得 skip。
//
// 不变量：
//   - 从不断言 UPSERT RowsAffected（跨方言无共同口径）；
//   - 时间条件一律以数据库时钟为准（waitForDBTimestampTick），
//     不以宿主固定 sleep 冒充。
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestUpsertPrepareAllCallsites 覆盖 45 通用 + 3 特例，共 48 条
// UPSERT 语句，在两个真实数据库上逐条 PREPARE。
func TestUpsertPrepareAllCallsites(t *testing.T) {
	calls := collectUpsertCalls(t, sqlstoreProductionFiles(t))
	if len(calls) != 45 {
		t.Fatalf("通用 UPSERT 调用数=%d, want 45", len(calls))
	}

	t.Run("mysql", func(t *testing.T) {
		s, cleanup := newTestSQLStore(t)
		defer cleanup()
		prepareAll(t, s, mysqlDialect{}, calls)
		prepareSpecials(t, s, mysqlDialect{})
	})

	t.Run("postgres", func(t *testing.T) {
		s, _, cleanup := newPostgresTestStore(t)
		defer cleanup()
		prepareAll(t, s, postgresDialect{}, calls)
		prepareSpecials(t, s, postgresDialect{})
	})
}

// prepareAll 对每个收集到的调用点按方言生成 UPSERT 并 PREPARE。
func prepareAll(t *testing.T, s *SQLStore, d Dialect, calls []upsertCallSpec) {
	t.Helper()
	for _, c := range calls {
		query := d.Upsert(c.Table, c.Cols, c.Values, c.Conflict, c.UpdateCols)
		stmt, err := s.db.PrepareContext(context.Background(), query)
		if err != nil {
			t.Fatalf("%s:%s PREPARE %s: %v", c.File, c.Pos, c.Table, err)
		}
		defer stmt.Close()
	}
}

// prepareSpecials 对三个语义特例构建器输出的语句执行相同的
// PrepareContext 验证（与通用调用点同一断言强度）。
func prepareSpecials(t *testing.T, s *SQLStore, d Dialect) {
	t.Helper()
	specials := []struct {
		name  string
		query string
	}{
		{"leader_lease", upsertLeaderLease(d)},
		{"abac_policies", upsertABACPolicy(d)},
		{"lifecycle_jobs", upsertLifecycleJob(d)},
	}
	for _, sp := range specials {
		stmt, err := s.db.PrepareContext(context.Background(), sp.query)
		if err != nil {
			t.Fatalf("%s PREPARE 失败: %v\nSQL: %s", sp.name, err, sp.query)
		}
		defer stmt.Close()
	}
}

// runOnBothDialects 在 MySQL 与 PG 真库上各执行一次 fn。
// DSN 未设置时对应子测试按夹具契约 skip；设置后不得 skip。
func runOnBothDialects(t *testing.T, fn func(t *testing.T, s *SQLStore, d Dialect)) {
	t.Helper()
	t.Run("mysql", func(t *testing.T) {
		s, cleanup := newTestSQLStore(t)
		defer cleanup()
		fn(t, s, mysqlDialect{})
	})
	t.Run("postgres", func(t *testing.T) {
		s, _, cleanup := newPostgresTestStore(t)
		defer cleanup()
		fn(t, s, postgresDialect{})
	})
}

// ----------------------------------------------------------------------------
// 1. TestUpsertInsertUpdateReadback
// ----------------------------------------------------------------------------

// TestUpsertInsertUpdateReadback 用 alert_rules 的生成 SQL 插入固定 ID，
// 重复执行修改 metric/message/enabled，读回断言第二次值生效且
// created_at 未被覆盖。
func TestUpsertInsertUpdateReadback(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()
		ruleID := "rule-upsert-readback-1"
		tenantID := "t-readback"
		createdAt := time.Now().UTC().Add(-24 * time.Hour)

		query := d.Upsert(
			"alert_rules",
			[]string{"id", "tenant_id", "metric", "message", "enabled", "created_at"},
			[]string{"?", "?", "?", "?", "?", "?"},
			[]string{"id"},
			[]string{"metric", "message", "enabled"},
		)

		exec := func(metric, message string, enabled int) {
			if _, err := s.db.ExecContext(ctx, query,
				ruleID, tenantID, metric, message, enabled, createdAt); err != nil {
				t.Fatalf("UPSERT alert_rules 失败: %v", err)
			}
		}
		exec("cpu", "cpu high", 1)

		// 首次读回，作为 created_at 的基准（列精度可能截断传入值）。
		read := func() (metric, message string, enabled int, createdAt time.Time) {
			err := s.db.QueryRowContext(ctx,
				d.ConvertPlaceholders("SELECT metric, message, enabled, created_at FROM alert_rules WHERE id=?"),
				ruleID).Scan(&metric, &message, &enabled, &createdAt)
			if err != nil {
				t.Fatalf("读回 alert_rules 失败: %v", err)
			}
			return
		}
		m1, msg1, en1, created1 := read()
		if m1 != "cpu" || msg1 != "cpu high" || en1 != 1 {
			t.Fatalf("首次插入读回 = (%q,%q,%d), want (cpu,cpu high,1)", m1, msg1, en1)
		}

		// 第二次写入修改 metric/message/enabled。
		exec("mem", "mem high", 0)
		m2, msg2, en2, created2 := read()
		if m2 != "mem" || msg2 != "mem high" || en2 != 0 {
			t.Fatalf("第二次更新读回 = (%q,%q,%d), want (mem,mem high,0)", m2, msg2, en2)
		}
		if !created2.Equal(created1) {
			t.Fatalf("created_at 被覆盖: 首次=%v 第二次=%v", created1, created2)
		}
	})
}

// ----------------------------------------------------------------------------
// 2. TestUpsertCompositeConflict
// ----------------------------------------------------------------------------

// TestUpsertCompositeConflict 用 configs 生成 SQL，同一
// (tenant_id,key_name) 连续写 version=1、version=2，断言只有一行
// 且 version=2。
func TestUpsertCompositeConflict(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()
		tenantID := "t-configs-conflict"
		keyName := "conflict-key"

		query := d.Upsert(
			"configs",
			[]string{"tenant_id", "key_name", "value", "version", "updated_at"},
			[]string{"?", "?", "?", "?", "?"},
			[]string{"tenant_id", "key_name"},
			[]string{"value", "version", "updated_at"},
		)
		write := func(value string, version int) {
			if _, err := s.db.ExecContext(ctx, query,
				tenantID, keyName, value, version, time.Now().UTC()); err != nil {
				t.Fatalf("UPSERT configs 失败: %v", err)
			}
		}
		write("v1", 1)
		write("v2", 2)

		var count int
		err := s.db.QueryRowContext(ctx,
			d.ConvertPlaceholders("SELECT COUNT(*) FROM configs WHERE tenant_id=? AND key_name=?"),
			tenantID, keyName).Scan(&count)
		if err != nil {
			t.Fatalf("COUNT configs 失败: %v", err)
		}
		if count != 1 {
			t.Fatalf("configs 行数=%d, want 1", count)
		}

		var value string
		var version int
		err = s.db.QueryRowContext(ctx,
			d.ConvertPlaceholders("SELECT value, version FROM configs WHERE tenant_id=? AND key_name=?"),
			tenantID, keyName).Scan(&value, &version)
		if err != nil {
			t.Fatalf("读回 configs 失败: %v", err)
		}
		if value != "v2" {
			t.Fatalf("configs value=%q, want %q", value, "v2")
		}
		if version != 2 {
			t.Fatalf("configs version=%d, want 2", version)
		}
	})
}

// ----------------------------------------------------------------------------
// 3. TestUpsertConstantsAndHeartbeat
// ----------------------------------------------------------------------------

// TestUpsertConstantsAndHeartbeat 用 devices 的 onboard 生成 SQL 验证
// state/task_state/managed 常量覆盖；用 controlplane_instances 生成 SQL
// 记录首次 started_at/last_heartbeat，等数据库时钟推进 1s 后再写新
// heartbeat，断言 started_at 不变、last_heartbeat 推进。
func TestUpsertConstantsAndHeartbeat(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()

		// devices onboard：state/task_state/managed 为受控常量。
		devicesQuery := d.Upsert(
			"devices",
			[]string{"device_id", "segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"},
			[]string{"?", "?", "?", "?", "?", "'online'", "'idle'", "1", "?", "?", "?"},
			[]string{"device_id"},
			[]string{"segment", "tenant_id", "ip", "agent_id", "state", "task_state", "managed", "hostname", "os", "arch"},
		)
		deviceID := "dev-const-heartbeat"
		writeDevice := func(segment, hostname string) {
			if _, err := s.db.ExecContext(ctx, devicesQuery,
				deviceID, segment, "t-const", "10.0.0.1", "agent-const", hostname, "linux", "amd64"); err != nil {
				t.Fatalf("UPSERT devices 失败: %v", err)
			}
		}
		writeDevice("seg-1", "host-1")
		writeDevice("seg-2", "host-2")

		var state, taskState string
		var managed int
		err := s.db.QueryRowContext(ctx,
			d.ConvertPlaceholders("SELECT state, task_state, managed FROM devices WHERE device_id=?"),
			deviceID).Scan(&state, &taskState, &managed)
		if err != nil {
			t.Fatalf("读回 devices 失败: %v", err)
		}
		if state != "online" || taskState != "idle" || managed != 1 {
			t.Fatalf("devices 常量覆盖失效: state=%q task_state=%q managed=%d, want online/idle/1",
				state, taskState, managed)
		}

		// controlplane_instances：首次记录 started_at/last_heartbeat。
		cpQuery := d.Upsert(
			"controlplane_instances",
			[]string{"instance_id", "hostname", "http_port", "grpc_port", "role", "version", "started_at", "last_heartbeat"},
			[]string{"?", "?", "?", "?", "?", "?", "?", "NOW()"},
			[]string{"instance_id"},
			[]string{"hostname", "http_port", "grpc_port", "role", "version", "last_heartbeat"},
		)
		instanceID := "inst-const-heartbeat"
		startedAt := time.Now().UTC().Add(-time.Minute)
		writeInstance := func(hostname, role string) {
			if _, err := s.db.ExecContext(ctx, cpQuery,
				instanceID, hostname, 8080, 9090, role, "v0", startedAt); err != nil {
				t.Fatalf("UPSERT controlplane_instances 失败: %v", err)
			}
		}
		writeInstance("host-a", "leader")

		var firstStarted, firstHeartbeat time.Time
		err = s.db.QueryRowContext(ctx,
			d.ConvertPlaceholders("SELECT started_at, last_heartbeat FROM controlplane_instances WHERE instance_id=?"),
			instanceID).Scan(&firstStarted, &firstHeartbeat)
		if err != nil {
			t.Fatalf("读回 controlplane_instances 失败: %v", err)
		}

		// 等数据库时钟推进超过首次心跳 + 1s（宿主 sleep 不可替代）。
		waitForDBTimestampTick(t, s, d, firstHeartbeat, time.Second)

		writeInstance("host-b", "follower")

		var secondStarted, secondHeartbeat time.Time
		err = s.db.QueryRowContext(ctx,
			d.ConvertPlaceholders("SELECT started_at, last_heartbeat FROM controlplane_instances WHERE instance_id=?"),
			instanceID).Scan(&secondStarted, &secondHeartbeat)
		if err != nil {
			t.Fatalf("读回 controlplane_instances(第二次) 失败: %v", err)
		}
		if !secondStarted.Equal(firstStarted) {
			t.Fatalf("started_at 被覆盖: 首次=%v 第二次=%v", firstStarted, secondStarted)
		}
		if !secondHeartbeat.After(firstHeartbeat) {
			t.Fatalf("last_heartbeat 未推进: 首次=%v 第二次=%v", firstHeartbeat, secondHeartbeat)
		}
	})
}

// ----------------------------------------------------------------------------
// 4. TestUpsertCrossUniqueKeyIsStrictOnPostgres
// ----------------------------------------------------------------------------

// TestUpsertCrossUniqueKeyIsStrictOnPostgres 钉死跨唯一键严格性：
// MySQL 侧 CreateTenant 命中已存在 name 保持既有行为（不新增报错断言）；
// PG 侧 conflict (id) 命中 name 唯一键、cloud_resources 自然键冲突
// 必须返回唯一键错误（23505）。
func TestUpsertCrossUniqueKeyIsStrictOnPostgres(t *testing.T) {
	t.Run("mysql", func(t *testing.T) {
		s, cleanup := newTestSQLStore(t)
		defer cleanup()

		first := &Tenant{ID: "t-cross-my-1", Name: "dup-name-cross", DisplayName: "first"}
		if s.CreateTenant(first) == nil {
			t.Fatal("首次 CreateTenant 应成功")
		}
		// 命中已存在 name：保持既有行为（MySQL 的 ON DUPLICATE KEY
		// UPDATE 在任意唯一键冲突时触发，既有行为是把原行覆盖更新，
		// 不新增报错断言）。只断言行为不退化：不产生第二行、原 ID 可读。
		s.CreateTenant(&Tenant{ID: "t-cross-my-2", Name: "dup-name-cross", DisplayName: "second"})

		var nameCount int
		if err := s.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM tenants WHERE name='dup-name-cross'`).Scan(&nameCount); err != nil {
			t.Fatalf("统计同名租户失败: %v", err)
		}
		if nameCount != 1 {
			t.Fatalf("同名租户数=%d, want 1（不得新增第二行）", nameCount)
		}
		got, ok := s.GetTenant("t-cross-my-1")
		if !ok || got == nil {
			t.Fatal("原租户 ID 应仍可读")
		}
		if got.Name != "dup-name-cross" {
			t.Fatalf("原租户 name 被篡改: %q", got.Name)
		}
	})

	t.Run("postgres", func(t *testing.T) {
		s, _, cleanup := newPostgresTestStore(t)
		defer cleanup()
		ctx := context.Background()
		d := postgresDialect{}

		// 种子租户。
		seedQuery := d.Upsert(
			"tenants",
			[]string{"id", "name", "display_name", "status", "created_at", "updated_at"},
			[]string{"?", "?", "?", "'active'", "?", "?"},
			[]string{"id"},
			[]string{"display_name", "status", "updated_at"},
		)
		if _, err := s.db.ExecContext(ctx, seedQuery,
			"t-cross-pg-1", "dup-name-pg", "first", time.Now().UTC(), time.Now().UTC()); err != nil {
			t.Fatalf("种子租户失败: %v", err)
		}

		// 子用例 A：conflict (id) 但 name 命中已存在唯一键 ⇒ 必须报唯一键错误。
		dupNameQuery := d.Upsert(
			"tenants",
			[]string{"id", "name", "display_name", "status", "created_at", "updated_at"},
			[]string{"?", "?", "?", "'active'", "?", "?"},
			[]string{"id"},
			[]string{"display_name", "status", "updated_at"},
		)
		_, err := s.db.ExecContext(ctx, dupNameQuery,
			"t-cross-pg-2", "dup-name-pg", "second", time.Now().UTC(), time.Now().UTC())
		assertPGUniqueViolation(t, err, "tenants conflict(id) 命中 name 唯一键")

		// 子用例 B：cloud_resources 自然键冲突。
		// 先落两行不同自然键，再以相同 id、不同自然键写穿 ⇒ 必须报唯一键错误。
		crQuery := d.Upsert(
			"cloud_resources",
			[]string{"id", "tenant_id", "cloud_account_id", "provider", "region", "resource_type", "resource_id", "resource_name", "status", "tags", "created_at", "updated_at"},
			[]string{"?", "?", "?", "'aws'", "'us-east-1'", "?", "?", "?", "'active'", "'{}'", "?", "?"},
			[]string{"id"},
			[]string{"tenant_id", "cloud_account_id", "resource_type", "resource_id", "resource_name", "status", "tags", "updated_at"},
		)
		now := time.Now().UTC()
		for _, r := range []struct {
			id, rtype, rid string
		}{
			{"cr-cross-1", "vm", "res-1"},
			{"cr-cross-2", "vm", "res-2"},
		} {
			if _, err := s.db.ExecContext(ctx, crQuery,
				r.id, "t-cross-pg", "acc-1", r.rtype, r.rid, r.id, now, now); err != nil {
				t.Fatalf("种子 cloud_resources %s 失败: %v", r.id, err)
			}
		}
		// 相同 id=cr-cross-1，不同自然键 (acc-1, vm, res-2) 命中 cr-cross-2 的自然键。
		_, err = s.db.ExecContext(ctx, crQuery,
			"cr-cross-1", "t-cross-pg", "acc-1", "vm", "res-2", "cr-cross-1", now, now)
		assertPGUniqueViolation(t, err, "cloud_resources 自然键冲突")
	})
}

// assertPGUniqueViolation 断言 err 为 PG 唯一键冲突（SQLSTATE 23505）。
func assertPGUniqueViolation(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: 未返回错误, want 唯一键错误", what)
	}
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		t.Fatalf("%s: 错误非 PG 错误: %v", what, err)
	}
	if pe.Code != "23505" {
		t.Fatalf("%s: SQLSTATE=%s, want 23505 (unique_violation), err=%v", what, pe.Code, err)
	}
}

// ----------------------------------------------------------------------------
// 5. TestSubmitResultStateTransitions
// ----------------------------------------------------------------------------

// TestSubmitResultStateTransitions 为 success/retry/dead-letter 各造一条
// running task，调 SubmitResult，用转换后的 SELECT 验证状态、retry_count、
// dead_letter。不断言 UPSERT 的 RowsAffected。
//
// MySQL-only 公共方法测试：SubmitResult 周围 tasks SELECT/UPDATE 与
// devices writeback 仍是 A3-2 裸 `?`/反引号读路径，本批不方言化
// （write-path-only 裁决），故 PG 子测试显式 skip，不静默跳过。
func TestSubmitResultStateTransitions(t *testing.T) {
	t.Run("mysql", func(t *testing.T) {
		s, cleanup := newTestSQLStore(t)
		defer cleanup()
		ctx := context.Background()
		d := mysqlDialect{}
		agentID := "agent-submit-result"

		insertTask := d.ConvertPlaceholders(
			`INSERT INTO tasks (task_id, agent_id, tenant_id, status, retry_count, max_retries, dead_letter, created_at)
			 VALUES (?, ?, 't-submit', 'running', ?, ?, 0, ?)`)
		seed := func(taskID string, retryCount, maxRetries int) {
			if _, err := s.db.ExecContext(ctx, insertTask,
				taskID, agentID, retryCount, maxRetries, time.Now().UTC()); err != nil {
				t.Fatalf("种子 task %s 失败: %v", taskID, err)
			}
		}
		seed("task-submit-success", 0, 3)
		seed("task-submit-retry", 0, 3)
		seed("task-submit-dead", 3, 3)

		s.SubmitResult(&proto.TaskResult{
			TaskID: "task-submit-success", AgentID: agentID, ExitCode: 0,
			Stdout: "ok", FinishedAt: time.Now().UTC(),
		})
		s.SubmitResult(&proto.TaskResult{
			TaskID: "task-submit-retry", AgentID: agentID, ExitCode: 1,
			Stderr: "transient", FinishedAt: time.Now().UTC(),
		})
		s.SubmitResult(&proto.TaskResult{
			TaskID: "task-submit-dead", AgentID: agentID, ExitCode: 1,
			Stderr: "permanent", FinishedAt: time.Now().UTC(),
		})

		// 状态守卫：重复上报不得翻转终态。
		s.SubmitResult(&proto.TaskResult{
			TaskID: "task-submit-success", AgentID: agentID, ExitCode: 1,
			Stderr: "late", FinishedAt: time.Now().UTC(),
		})

		type taskState struct {
			status     string
			retryCount int
			deadLetter int
		}
		read := func(taskID string) taskState {
			var st taskState
			var status sql.NullString
			var rc, dl sql.NullInt64
			err := s.db.QueryRowContext(ctx,
				d.ConvertPlaceholders("SELECT status, retry_count, dead_letter FROM tasks WHERE task_id=?"),
				taskID).Scan(&status, &rc, &dl)
			if err != nil {
				t.Fatalf("读回 task %s 失败: %v", taskID, err)
			}
			st.status = status.String
			st.retryCount = int(rc.Int64)
			st.deadLetter = int(dl.Int64)
			return st
		}

		if got := read("task-submit-success"); got.status != "done" {
			t.Fatalf("success 转移: status=%q, want done", got.status)
		}
		if got := read("task-submit-retry"); got.status != "pending" || got.retryCount != 1 {
			t.Fatalf("retry 转移: status=%q retry_count=%d, want pending/1", got.status, got.retryCount)
		}
		if got := read("task-submit-dead"); got.status != "failed" || got.deadLetter != 1 {
			t.Fatalf("dead-letter 转移: status=%q dead_letter=%d, want failed/1", got.status, got.deadLetter)
		}
		// 迟到上报被状态守卫拒绝：success 任务保持 done。
		if got := read("task-submit-success"); got.status != "done" {
			t.Fatalf("迟到上报破坏终态: status=%q, want done", got.status)
		}
	})

	t.Run("postgres", func(t *testing.T) {
		t.Skip("SubmitResult 周围读路径属 A3-2，不在 A3-3 范围")
	})
}

// ----------------------------------------------------------------------------
// 6. TestUpsertTaskResultsWritePath
// ----------------------------------------------------------------------------

// TestUpsertTaskResultsWritePath 直接用 task_results 的生成 SQL 在
// MySQL/PG 双库执行 success/retry/dead-letter 三种写路径，读回用
// ConvertPlaceholders 转换后的 SELECT（不触达 SubmitResult 的 A3-2
// 读路径）。不断言 RowsAffected。
func TestUpsertTaskResultsWritePath(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()

		query := d.Upsert(
			"task_results",
			[]string{"task_id", "agent_id", "exit_code", "stdout", "stderr", "finished_at"},
			[]string{"?", "?", "?", "?", "?", "?"},
			[]string{"task_id"},
			[]string{"exit_code", "stdout", "stderr", "finished_at"},
		)
		finished := time.Now().UTC()
		write := func(taskID, agentID string, exitCode int, stdout, stderr string) {
			if _, err := s.db.ExecContext(ctx, query,
				taskID, agentID, exitCode, stdout, stderr, finished); err != nil {
				t.Fatalf("UPSERT task_results(%s) 失败: %v", taskID, err)
			}
		}
		// 三种写路径：success / retry / dead-letter。
		write("tr-success", "agent-tr", 0, "done", "")
		write("tr-retry", "agent-tr", 1, "", "transient")
		write("tr-dead", "agent-tr", 2, "", "permanent")
		// 同一 task_id 覆盖写：验证写穿（last-write-wins 读回）。
		write("tr-success", "agent-tr", 0, "done-again", "")

		type resultRow struct {
			taskID     string
			agentID    string
			exitCode   int
			stdout     string
			stderr     string
			finishedAt time.Time
		}
		read := func(taskID string) resultRow {
			var r resultRow
			err := s.db.QueryRowContext(ctx,
				d.ConvertPlaceholders("SELECT task_id, agent_id, exit_code, stdout, stderr, finished_at FROM task_results WHERE task_id=?"),
				taskID).Scan(&r.taskID, &r.agentID, &r.exitCode, &r.stdout, &r.stderr, &r.finishedAt)
			if err != nil {
				t.Fatalf("读回 task_results %s 失败: %v", taskID, err)
			}
			return r
		}

		if got := read("tr-success"); got.exitCode != 0 || got.stdout != "done-again" {
			t.Fatalf("success 写路径读回 = %+v, want exit_code=0 stdout=done-again", got)
		}
		if got := read("tr-retry"); got.exitCode != 1 || got.stderr != "transient" {
			t.Fatalf("retry 写路径读回 = %+v, want exit_code=1 stderr=transient", got)
		}
		if got := read("tr-dead"); got.exitCode != 2 || got.stderr != "permanent" {
			t.Fatalf("dead-letter 写路径读回 = %+v, want exit_code=2 stderr=permanent", got)
		}

		var count int
		if err := s.db.QueryRowContext(ctx,
			d.ConvertPlaceholders("SELECT COUNT(*) FROM task_results WHERE agent_id=?"),
			"agent-tr").Scan(&count); err != nil {
			t.Fatalf("COUNT task_results 失败: %v", err)
		}
		if count != 3 {
			t.Fatalf("task_results 行数=%d, want 3", count)
		}
	})
}
