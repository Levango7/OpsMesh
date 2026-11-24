package sqlstore

import (
	"context"
	"strings"
	"testing"
	"time"
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
