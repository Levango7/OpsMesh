package sqlstore

import (
	"strings"
	"testing"
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
		`CASE WHEN "leader_lease"."expires_at" < $4 OR "leader_lease"."holder" = EXCLUDED."holder"`,
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
