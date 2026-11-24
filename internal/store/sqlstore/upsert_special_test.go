package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// TestSpecialUpsertSQL 钉死三个语义特例的 SQL 形态：
// leader_lease 条件抢占（MySQL 三占位 IF 条件 / PG 基于旧行的 CASE WHEN）、
// abac_policies 版本自增（version=version+1）、lifecycle_jobs 隐式
// updated_at 刷新（PG 显式补 now()）。
// 另逐组校验占位符数量与迁移前参数顺序一致：
// leader 6（instanceID, exp, now, now, now, now）、abac 8、lifecycle 9。
func TestSpecialUpsertSQL(t *testing.T) {
	mysql := upsertLeaderLease(mysqlDialect{})
	if !strings.Contains(mysql, "holder=IF(expires_at < ? OR holder=VALUES(holder)") {
		t.Fatalf("MySQL leader lease 特例丢失: %s", mysql)
	}
	pg := upsertLeaderLease(postgresDialect{})
	for _, token := range []string{
		`ON CONFLICT ("id") DO UPDATE SET`,
		// 三个 CASE 条件必须分别绑定 $4/$5/$6（holder/expires_at/updated_at），
		// 不得复用同一参数下标——否则语句只引用 $1-$4，与
		// RenewLeadership 的 6 实参不匹配，pgx 扩展协议会拒绝。
		`CASE WHEN "leader_lease"."expires_at" < $4 OR "leader_lease"."holder" = EXCLUDED."holder"`,
		`CASE WHEN "leader_lease"."expires_at" < $5 OR "leader_lease"."holder" = EXCLUDED."holder"`,
		`CASE WHEN "leader_lease"."expires_at" < $6 OR "leader_lease"."holder" = EXCLUDED."holder"`,
		`THEN EXCLUDED."holder" ELSE "leader_lease"."holder" END`,
	} {
		if !strings.Contains(pg, token) {
			t.Fatalf("PG leader lease 缺少 %q: %s", token, pg)
		}
	}
	if !strings.Contains(upsertABACPolicy(postgresDialect{}), `"version" = "abac_policies"."version" + 1`) {
		t.Fatal("PG ABAC 版本自增丢失")
	}
	if !strings.Contains(upsertLifecycleJob(postgresDialect{}), `"updated_at"=now()`) {
		t.Fatal("PG lifecycle 更新时间丢失")
	}
	for _, tc := range []struct {
		name  string
		mysql string
		pg    string
		want  int
	}{
		{"leader", mysql, pg, 6},
		{"abac", upsertABACPolicy(mysqlDialect{}), upsertABACPolicy(postgresDialect{}), 8},
		{"lifecycle", upsertLifecycleJob(mysqlDialect{}), upsertLifecycleJob(postgresDialect{}), 9},
	} {
		if got := strings.Count(tc.mysql, "?"); got != tc.want {
			t.Fatalf("%s MySQL 占位符=%d, want %d", tc.name, got, tc.want)
		}
		if got := strings.Count(tc.pg, "$"); got != tc.want {
			t.Fatalf("%s PG 占位符=%d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestPostgresRenewLeadership PG 真库往返：RenewLeadership 抢占/续租/拒绝
// 三态语义（经 newPostgresTestStore，未设 OPSMESH_TEST_PG_DSN 则跳过）。
func TestPostgresRenewLeadership(t *testing.T) {
	s, _, cleanup := newPostgresTestStore(t)
	defer cleanup()
	ctx := context.Background()

	instA := "renew-pg-a"
	instB := "renew-pg-b"
	ttl := 10 * time.Second

	s.instanceID = instA
	if !s.RenewLeadership(ttl) {
		t.Fatal("首次 RenewLeadership 应抢占成功(true)")
	}
	var holder string
	var exp1, upd1 time.Time
	if err := s.db.QueryRowContext(ctx,
		`SELECT "holder", "expires_at", "updated_at" FROM "leader_lease" WHERE "id"=1`).
		Scan(&holder, &exp1, &upd1); err != nil {
		t.Fatalf("读回 leader_lease: %v", err)
	}
	if holder != instA {
		t.Fatalf("holder=%q, want %q", holder, instA)
	}
	if !exp1.After(time.Now().UTC()) {
		t.Fatalf("expires_at=%v 应在未来", exp1)
	}

	time.Sleep(50 * time.Millisecond)
	if !s.RenewLeadership(ttl) {
		t.Fatal("同实例第二次 RenewLeadership 应续租成功(true)")
	}
	var holder2 string
	var exp2, upd2 time.Time
	if err := s.db.QueryRowContext(ctx,
		`SELECT "holder", "expires_at", "updated_at" FROM "leader_lease" WHERE "id"=1`).
		Scan(&holder2, &exp2, &upd2); err != nil {
		t.Fatalf("读回 leader_lease(续租后): %v", err)
	}
	if holder2 != instA {
		t.Fatalf("续租后 holder=%q, want %q", holder2, instA)
	}
	if !exp2.After(exp1) {
		t.Fatalf("续租后 expires_at=%v 未延长(旧 %v)", exp2, exp1)
	}

	// 活跃租约被他人拒绝：不接管，holder/expires_at/updated_at 保持旧值。
	s.instanceID = instB
	if s.RenewLeadership(ttl) {
		t.Fatal("活跃租约被他人抢占应返回 false")
	}
	var holder3 string
	var exp3, upd3 time.Time
	if err := s.db.QueryRowContext(ctx,
		`SELECT "holder", "expires_at", "updated_at" FROM "leader_lease" WHERE "id"=1`).
		Scan(&holder3, &exp3, &upd3); err != nil {
		t.Fatalf("读回 leader_lease(拒绝后): %v", err)
	}
	if holder3 != instA {
		t.Fatalf("拒绝后 holder 被篡改为 %q, want %q", holder3, instA)
	}
	if !exp3.Equal(exp2) {
		t.Fatalf("拒绝后 expires_at 变为 %v, want 保持 %v", exp3, exp2)
	}
	if !upd3.Equal(upd2) {
		t.Fatalf("拒绝后 updated_at 变为 %v, want 保持 %v", upd3, upd2)
	}
}

// ----------------------------------------------------------------------------
// Task 11：并发特例测试
// ----------------------------------------------------------------------------

// waitForDBTimestampTick 轮询数据库时钟直到其推进到 after+tick 之后。
//
// MySQL 用 SELECT NOW(6)，PG 用 SELECT clock_timestamp()；每 5ms 轮询，
// 最多 3s。只有数据库时间达到 after+tick 才返回；超时或查询失败
// t.Fatal。宿主侧固定 sleep 不得冒充跨库时钟条件。
func waitForDBTimestampTick(t *testing.T, s *SQLStore, d Dialect, after time.Time, tick time.Duration) {
	t.Helper()
	var clockQuery string
	switch d.DriverName() {
	case "pgx":
		clockQuery = "SELECT clock_timestamp()"
	default:
		clockQuery = "SELECT NOW(6)"
	}
	target := after.Add(tick)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var dbNow time.Time
		if err := s.db.QueryRowContext(context.Background(), clockQuery).Scan(&dbNow); err != nil {
			t.Fatalf("查询数据库时钟(%s)失败: %v", clockQuery, err)
		}
		if dbNow.After(target) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("数据库时钟未在 3s 内推进到 %v 之后: 库时钟=%v, 目标=%v (after=%v, tick=%v)",
				target, dbNow, target, after, tick)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// newSharedMySQLStores 在同一临时 MySQL 库上建 n 个 SQLStore（不同
// instanceID），供并发选主/写穿测试共享。返回实例列表与 cleanup。
func newSharedMySQLStores(t *testing.T, n int) ([]*SQLStore, func()) {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping migration integration test")
	}
	adminDSN := stripDBName(dsn)
	dbName := fmt.Sprintf("test_migration_%d", time.Now().UnixNano())

	adminDB, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + dbName); err != nil {
		adminDB.Close()
		t.Fatalf("create temp db %s: %v", dbName, err)
	}
	adminDB.Close()

	stores := make([]*SQLStore, 0, n)
	cleanup := func() {
		for _, s := range stores {
			_ = s.db.Close()
		}
		dropTestDB(adminDSN, dbName)
	}
	for i := 0; i < n; i++ {
		s, err := NewSQLStore(withDBName(dsn, dbName), "", "")
		if err != nil {
			cleanup()
			t.Fatalf("NewSQLStore: %v", err)
		}
		s.instanceID = fmt.Sprintf("renew-multi-my-%d-%d", time.Now().UnixNano(), i)
		stores = append(stores, s)
	}
	return stores, cleanup
}

// newSharedPGStores 与 newSharedMySQLStores 同构，面向 PostgreSQL。
func newSharedPGStores(t *testing.T, n int) ([]*SQLStore, func()) {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_PG_DSN not set; skipping postgres integration test")
	}
	adminDSN := pgAdminDSN(t, dsn)
	dbName := fmt.Sprintf("test_pg_%d", time.Now().UnixNano())

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := adminDB.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
		adminDB.Close()
		t.Fatalf("create temp db %s: %v", dbName, err)
	}
	adminDB.Close()

	stores := make([]*SQLStore, 0, n)
	cleanup := func() {
		for _, s := range stores {
			_ = s.db.Close()
		}
		dropPGTestDB(t, adminDSN, dbName)
	}
	for i := 0; i < n; i++ {
		s, err := NewSQLStore(pgTestDSN(t, dsn, dbName), "", "")
		if err != nil {
			cleanup()
			t.Fatalf("NewSQLStore(pg): %v", err)
		}
		s.instanceID = fmt.Sprintf("renew-multi-pg-%d-%d", time.Now().UnixNano(), i)
		stores = append(stores, s)
	}
	return stores, cleanup
}

// resetExpiredLease 把 leader_lease 预置为「他人持有且已过期」状态，
// 供并发抢占用例使用。
func resetExpiredLease(t *testing.T, s *SQLStore, d Dialect) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Minute)
	if _, err := s.db.ExecContext(context.Background(),
		d.ConvertPlaceholders(`UPDATE leader_lease SET holder='stale', expires_at=?, updated_at=? WHERE id=1`),
		past, past); err != nil {
		t.Fatalf("预置过期租约失败: %v", err)
	}
}

// readLeaseHolder 读回当前 holder（可空）。
func readLeaseHolder(t *testing.T, s *SQLStore) (string, bool) {
	t.Helper()
	var holder sql.NullString
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT holder FROM leader_lease WHERE id=1`).Scan(&holder); err != nil {
		t.Fatalf("读回 leader_lease holder 失败: %v", err)
	}
	return holder.String, holder.Valid
}

// readLeaseExpires 读回当前 expires_at（可空）。
func readLeaseExpires(t *testing.T, s *SQLStore) (time.Time, bool) {
	t.Helper()
	var expiresAt sql.NullTime
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT expires_at FROM leader_lease WHERE id=1`).Scan(&expiresAt); err != nil {
		t.Fatalf("读回 leader_lease expires_at 失败: %v", err)
	}
	return expiresAt.Time, expiresAt.Valid
}

// TestRenewLeadershipConcurrentClaim 多实例共享同一库并发抢占 leader_lease。
//
// 子用例覆盖：表为空时插入并持租；本实例未过期续租；其他实例未过期
// 不能抢占；预置过期租约可抢占；holder/expires_at 为 NULL 时不误判。
// 并发子用例预置过期租约，N 个实例同时 RenewLeadership；每轮恰好一个
// 返回 true；读回 holder 与获胜者一致；连续 10 轮不双主。
func TestRenewLeadershipConcurrentClaim(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()
		ttl := 10 * time.Second

		// 1) 表为空时插入并持租。
		s.instanceID = "renew-seq-a"
		if !s.RenewLeadership(ttl) {
			t.Fatal("空表首次 RenewLeadership 应抢占成功(true)")
		}
		if holder, valid := readLeaseHolder(t, s); !valid || holder != "renew-seq-a" {
			t.Fatalf("抢占后 holder=%q valid=%v, want renew-seq-a/true", holder, valid)
		}

		// 2) 本实例未过期续租。
		exp1, _ := readLeaseExpires(t, s)
		if !s.RenewLeadership(ttl) {
			t.Fatal("本实例续租应返回 true")
		}
		exp2, _ := readLeaseExpires(t, s)
		if exp2.Before(exp1) {
			t.Fatalf("续租后 expires_at 回退: %v -> %v", exp1, exp2)
		}
		if d.DriverName() == "pgx" && !exp2.After(exp1) {
			t.Fatalf("PG 续租后 expires_at 应严格延长: %v -> %v", exp1, exp2)
		}
		// MySQL expires_at 为 DATETIME（秒精度），同一秒内两次续租
		// 可能存出相同值——只要求不回退（既有行为），不强制严格延长。

		// 3) 其他实例未过期不能抢占。
		s.instanceID = "renew-seq-b"
		if s.RenewLeadership(ttl) {
			t.Fatal("活跃租约被他人抢占应返回 false")
		}
		if holder, _ := readLeaseHolder(t, s); holder != "renew-seq-a" {
			t.Fatalf("拒绝后 holder=%q, want renew-seq-a", holder)
		}

		// 4) 预置过期租约可抢占。
		resetExpiredLease(t, s, d)
		s.instanceID = "renew-seq-c"
		if !s.RenewLeadership(ttl) {
			t.Fatal("过期租约应可被抢占(true)")
		}
		if holder, _ := readLeaseHolder(t, s); holder != "renew-seq-c" {
			t.Fatalf("抢占后 holder=%q, want renew-seq-c", holder)
		}

		// 5) holder/expires_at 为 NULL 时不误判。
		if _, err := s.db.ExecContext(ctx, d.ConvertPlaceholders(
			`UPDATE leader_lease SET holder=NULL, expires_at=NULL, updated_at=NULL WHERE id=1`)); err != nil {
			t.Fatalf("置空租约失败: %v", err)
		}
		if s.RenewLeadership(ttl) {
			t.Fatal("NULL 租约不应被误判为本实例持有")
		}
		if holder, valid := readLeaseHolder(t, s); valid {
			t.Fatalf("NULL holder 被改写: %q", holder)
		}
		if exp, valid := readLeaseExpires(t, s); valid {
			t.Fatalf("NULL expires_at 被改写: %v", exp)
		}
	})

	// 并发子用例：预置过期租约，N 个实例同时 RenewLeadership。
	runConcurrentLeadershipRounds(t, "mysql", newSharedMySQLStores)
	runConcurrentLeadershipRounds(t, "postgres", newSharedPGStores)
}

// runConcurrentLeadershipRounds 在共享库上连续 10 轮并发抢占：
// 每轮恰好一个实例返回 true，读回 holder 与获胜者一致，全程不双主。
func runConcurrentLeadershipRounds(t *testing.T, dialect string, newStores func(*testing.T, int) ([]*SQLStore, func())) {
	t.Helper()
	stores, cleanup := newStores(t, 5)
	defer cleanup()
	ttl := 10 * time.Second
	rounds := 10

	for round := 1; round <= rounds; round++ {
		// 每轮预置过期租约，保证所有实例从同一起跑线竞争。
		resetExpiredLease(t, stores[0], stores[0].dialect)

		results := make([]bool, len(stores))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, st := range stores {
			wg.Add(1)
			go func(i int, st *SQLStore) {
				defer wg.Done()
				<-start
				results[i] = st.RenewLeadership(ttl)
			}(i, st)
		}
		close(start)
		wg.Wait()

		winners := 0
		var winnerID string
		for i, won := range results {
			if won {
				winners++
				winnerID = stores[i].instanceID
			}
		}
		if winners != 1 {
			t.Fatalf("%s 第 %d 轮: 获胜实例数=%d, want 恰好 1 (results=%v)",
				dialect, round, winners, results)
		}
		holder, valid := readLeaseHolder(t, stores[0])
		if !valid || holder != winnerID {
			t.Fatalf("%s 第 %d 轮: holder=%q valid=%v, want 获胜者 %q",
				dialect, round, holder, valid, winnerID)
		}
	}
}

// TestABACPolicyConcurrentVersion ABAC 策略写穿版本并发回归：
// 重复写穿 version 自增、字段覆盖、created_at 不变；同一 (tenant_id,id)
// 起 N 个 goroutine 并发写穿，全部成功后 version=N+2 且 created_at 仍不变。
func TestABACPolicyConcurrentVersion(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()
		tenantID := "t-abac-conc"
		policyID := "p-abac-conc"

		// 首次插入 version=1。
		if _, ok := s.UpsertABACPolicyRecord(model.ABACPolicyRecord{
			TenantID: tenantID, ID: policyID,
			Subject: "user:alice", Resource: "res:*", Action: "read", Effect: "allow",
		}); !ok {
			t.Fatal("首次 UpsertABACPolicyRecord 应成功")
		}

		read := func() model.ABACPolicyRecord {
			t.Helper()
			var r model.ABACPolicyRecord
			err := s.db.QueryRowContext(ctx,
				d.ConvertPlaceholders("SELECT tenant_id, id, subject, resource, action, effect, version, created_at, updated_at FROM abac_policies WHERE tenant_id=? AND id=?"),
				tenantID, policyID).Scan(&r.TenantID, &r.ID, &r.Subject, &r.Resource, &r.Action, &r.Effect, &r.Version, &r.CreatedAt, &r.UpdatedAt)
			if err != nil {
				t.Fatalf("读回 abac_policies 失败: %v", err)
			}
			return r
		}
		first := read()
		if first.Version != 1 {
			t.Fatalf("首次插入 version=%d, want 1", first.Version)
		}

		// 重复写穿并修改字段：version=2、字段覆盖、created_at 不变。
		second, ok := s.UpsertABACPolicyRecord(model.ABACPolicyRecord{
			TenantID: tenantID, ID: policyID,
			Subject: "user:bob", Resource: "res:prod", Action: "write", Effect: "deny",
		})
		if !ok {
			t.Fatal("第二次 UpsertABACPolicyRecord 应成功")
		}
		if second.Version != 2 {
			t.Fatalf("写穿后 version=%d, want 2", second.Version)
		}
		if second.Subject != "user:bob" || second.Resource != "res:prod" ||
			second.Action != "write" || second.Effect != "deny" {
			t.Fatalf("字段未覆盖: %+v", second)
		}
		if !second.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("created_at 被改写: 首次=%v 二次=%v", first.CreatedAt, second.CreatedAt)
		}

		// 并发写穿：N 个 goroutine 同一 (tenant_id,id)。
		const n = 8
		var wg sync.WaitGroup
		oks := make([]bool, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, ok := s.UpsertABACPolicyRecord(model.ABACPolicyRecord{
					TenantID: tenantID, ID: policyID,
					Subject:  fmt.Sprintf("user:worker-%d", i),
					Resource: "res:prod", Action: "write", Effect: "allow",
				})
				oks[i] = ok
			}(i)
		}
		wg.Wait()
		for i, ok := range oks {
			if !ok {
				t.Fatalf("并发写穿 goroutine %d 失败", i)
			}
		}

		final := read()
		if final.Version != int64(n+2) {
			t.Fatalf("并发写穿后 version=%d, want %d", final.Version, n+2)
		}
		if !final.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("并发写穿后 created_at 被改写: 首次=%v 最终=%v", first.CreatedAt, final.CreatedAt)
		}
	})
}

// TestLifecycleUpsertTimestamp 生命周期作业 upsert 的 updated_at 语义：
// 首次创建后读 updated_at；等数据库时钟推进后同 ID 更新 phase/progress，
// PG 必须推进（显式 now()），MySQL 保持列定义 ON UPDATE CURRENT_TIMESTAMP(3)
// 的隐式刷新行为。MySQL no-op 子用例（字段完全相同再写一次）记录并对照
// 更新时间，保持既有行为，不要求两库 no-op 行数或时间相同。
func TestLifecycleUpsertTimestamp(t *testing.T) {
	runOnBothDialects(t, func(t *testing.T, s *SQLStore, d Dialect) {
		ctx := context.Background()
		jobID := "job-lifecycle-ts"
		newJob := func(phase string, progress int) *LifecycleJob {
			return &LifecycleJob{
				ID:          jobID,
				ClusterID:   "cluster-ts",
				Kind:        "upgrade",
				TenantID:    "t-lifecycle-ts",
				Phase:       phase,
				Progress:    progress,
				CurrentStep: "step-1",
				SpecJSON:    map[string]any{"image": "img:1"},
				LockOwner:   "owner-ts",
			}
		}

		if err := s.CreateLifecycleJob(ctx, newJob("pending", 0)); err != nil {
			t.Fatalf("CreateLifecycleJob 失败: %v", err)
		}
		first, err := s.GetLifecycleJob(ctx, jobID)
		if err != nil {
			t.Fatalf("GetLifecycleJob 失败: %v", err)
		}
		if first == nil {
			t.Fatal("CreateLifecycleJob 后读不到作业")
		}
		firstUpdated := first.UpdatedAt

		// 以数据库时钟为准等待推进（宿主固定 sleep 不可替代）。
		waitForDBTimestampTick(t, s, d, firstUpdated, time.Millisecond)

		if err := s.CreateLifecycleJob(ctx, newJob("running", 50)); err != nil {
			t.Fatalf("CreateLifecycleJob(更新) 失败: %v", err)
		}
		second, err := s.GetLifecycleJob(ctx, jobID)
		if err != nil {
			t.Fatalf("GetLifecycleJob(更新后) 失败: %v", err)
		}
		if second == nil {
			t.Fatal("更新后读不到作业")
		}
		if second.Phase != "running" || second.Progress != 50 {
			t.Fatalf("phase/progress 未更新: %q/%d, want running/50", second.Phase, second.Progress)
		}
		if d.DriverName() == "pgx" {
			if !second.UpdatedAt.After(firstUpdated) {
				t.Fatalf("PG updated_at 未推进: 首次=%v 更新后=%v", firstUpdated, second.UpdatedAt)
			}
		} else {
			// MySQL 保持原隐式 ON UPDATE CURRENT_TIMESTAMP(3) 行为：
			// 值变化时刷新，不得回退。
			if second.UpdatedAt.Before(firstUpdated) {
				t.Fatalf("MySQL updated_at 回退: 首次=%v 更新后=%v", firstUpdated, second.UpdatedAt)
			}
		}

		// MySQL no-op 子用例：字段完全相同再写一次，记录并对照更新时间。
		noopBefore := second.UpdatedAt
		if err := s.CreateLifecycleJob(ctx, newJob("running", 50)); err != nil {
			t.Fatalf("CreateLifecycleJob(no-op) 失败: %v", err)
		}
		after, err := s.GetLifecycleJob(ctx, jobID)
		if err != nil {
			t.Fatalf("GetLifecycleJob(no-op 后) 失败: %v", err)
		}
		if after == nil {
			t.Fatal("no-op 后读不到作业")
		}
		if d.DriverName() == "pgx" {
			// PG 显式 now()：no-op 也会刷新，仅记录对照，不断言。
			t.Logf("PG no-op: updated_at %v -> %v（显式 now() 刷新，既有不要求）",
				noopBefore, after.UpdatedAt)
		} else {
			// MySQL 既有行为：值未变化不触发 ON UPDATE，updated_at 不变。
			if !after.UpdatedAt.Equal(noopBefore) {
				t.Fatalf("MySQL no-op updated_at 变化: %v -> %v, want 保持不变",
					noopBefore, after.UpdatedAt)
			}
		}
	})
}
