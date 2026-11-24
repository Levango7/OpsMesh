// upsert_special.go 三个通用 Dialect.Upsert 无法表达的语义特例。
//
// A3-3 把 45 处通用 upsert 迁移到 Dialect.Upsert 后，剩余三处因
// 语义特例保留专用构建器（本文件）：
//   - leader_lease 抢占：条件更新（租约已过期 OR 本人持有才接管），
//     MySQL 依赖 ON DUPLICATE KEY UPDATE 从左到右求值（后续 IF 引用的
//     holder 是第 1 行赋值后的新值）；PG 用 CASE WHEN 逐列表达，
//     三个条件一律基于旧行（"leader_lease".*），不引用 EXCLUDED。
//   - abac_policies 写穿：version 自增（`version`=`version`+1），
//     created_at 保持不变；PG 输出 "version" = "abac_policies"."version" + 1。
//   - lifecycle_jobs 幂等创建：MySQL 依赖列定义
//     ON UPDATE CURRENT_TIMESTAMP(3) 隐式刷新 updated_at；PG 侧无触发器
//     （001 DDL 不建 ON UPDATE 触发器），须在更新列后显式补
//     "updated_at"=now()。
//
// 三者统一 switch d.DriverName()："pgx" 走 PG 分支，其余走 MySQL 分支
// （与 postgresDialect.DriverName() 的返回值一致）。PG 分支返回前
// 必须调用 d.ConvertPlaceholders(query) 保证 $n 连续；MySQL 分支
// 原样返回 ? SQL。
//
// 参数顺序与个数与迁移前逐字一致，不做任何重排：
//   - leader lease：instanceID, exp, now, now, now, now（6 占位）
//   - ABAC 策略：tenantID, id, subject, resource, action, effect,
//     createdAt, now（8 占位）
//   - 生命周期作业：id, clusterID, kind, tenantID, phase, progress,
//     currentStep, specJSON, lockOwner（9 占位）
package sqlstore

// upsertLeaderLease 生成 leader_lease 幂等抢占 UPSERT。
//
// 语义：已存在且租约未过期且持有者非本实例 ⇒ 拒绝接管（仅刷新
// updated_at）；其余情形（无行/已过期/本人持有）⇒ 接管并续租。
// MySQL 原样保留三占位条件与从左到右求值；PG 用 CASE WHEN，
// 三个条件都基于旧行，ELSE 保留旧列。
// 参数顺序固定：instanceID, exp, now, now, now, now
// （VALUES 3 个：holder/expires_at/updated_at，id 为常量 1；
// ON 侧条件 3 个 now）。
func upsertLeaderLease(d Dialect) string {
	switch d.DriverName() {
	case "pgx":
		query := `INSERT INTO "leader_lease" ("id", "holder", "expires_at", "updated_at")
		VALUES (1, ?, ?, ?)
		ON CONFLICT ("id") DO UPDATE SET
			"holder" = CASE WHEN "leader_lease"."expires_at" < $4 OR "leader_lease"."holder" = EXCLUDED."holder"
				THEN EXCLUDED."holder" ELSE "leader_lease"."holder" END,
			"expires_at" = CASE WHEN "leader_lease"."expires_at" < $4 OR "leader_lease"."holder" = EXCLUDED."holder"
				THEN EXCLUDED."expires_at" ELSE "leader_lease"."expires_at" END,
			"updated_at" = CASE WHEN "leader_lease"."expires_at" < $4 OR "leader_lease"."holder" = EXCLUDED."holder"
				THEN EXCLUDED."updated_at" ELSE "leader_lease"."updated_at" END`
		return d.ConvertPlaceholders(query)
	default:
		return `INSERT INTO leader_lease (id, holder, expires_at, updated_at)
			VALUES (1, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				holder=IF(expires_at < ? OR holder=VALUES(holder), VALUES(holder), holder),
				expires_at=IF(expires_at < ? OR holder=VALUES(holder), VALUES(expires_at), expires_at),
				updated_at=IF(expires_at < ? OR holder=VALUES(holder), VALUES(updated_at), updated_at)`
	}
}

// upsertABACPolicy 生成 abac_policies 写穿 UPSERT。
//
// 语义：按 (tenant_id, id) 幂等写穿；已存在则 version 自增
// （version=version+1）且 created_at 保持不变，否则首次插入
// version=1。更新集不含 created_at/tenant_id/id/version。
// conflict 为 ("tenant_id", "id")。
func upsertABACPolicy(d Dialect) string {
	switch d.DriverName() {
	case "pgx":
		query := `INSERT INTO "abac_policies" ("tenant_id", "id", "subject", "resource", "action", "effect", "version", "created_at", "updated_at")
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT ("tenant_id", "id") DO UPDATE SET
			"version" = "abac_policies"."version" + 1,
			"subject" = EXCLUDED."subject",
			"resource" = EXCLUDED."resource",
			"action" = EXCLUDED."action",
			"effect" = EXCLUDED."effect",
			"updated_at" = EXCLUDED."updated_at"`
		return d.ConvertPlaceholders(query)
	default:
		return "INSERT INTO `abac_policies` (" + abacPolicyColumns + ") VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?) " +
			"ON DUPLICATE KEY UPDATE `version`=`version`+1, `subject`=VALUES(`subject`), " +
			"`resource`=VALUES(`resource`), `action`=VALUES(`action`), `effect`=VALUES(`effect`), " +
			"`updated_at`=VALUES(`updated_at`)"
	}
}

// upsertLifecycleJob 生成 lifecycle_jobs 幂等创建 UPSERT。
//
// 语义：同一 cluster+kind 仅允许一个非终态；冲突时整行重写并
// 刷新 version。MySQL 依赖列定义 ON UPDATE CURRENT_TIMESTAMP(3)
// 隐式刷新 updated_at；PG 无触发器，须在原更新列后显式补
// "updated_at"=now()。conflict 为 ("id")。
func upsertLifecycleJob(d Dialect) string {
	switch d.DriverName() {
	case "pgx":
		query := `INSERT INTO "lifecycle_jobs" ("id", "cluster_id", "kind", "tenant_id", "phase", "progress", "current_step", "spec_json", "lock_owner", "version")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT ("id") DO UPDATE SET
			"id"=EXCLUDED."id", "phase"=EXCLUDED."phase", "progress"=EXCLUDED."progress",
			"current_step"=EXCLUDED."current_step", "spec_json"=EXCLUDED."spec_json",
			"lock_owner"=EXCLUDED."lock_owner", "version"=EXCLUDED."version",
			"updated_at"=now()`
		return d.ConvertPlaceholders(query)
	default:
		return `INSERT INTO lifecycle_jobs (id, cluster_id, kind, tenant_id, phase, progress, current_step, spec_json, lock_owner, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
			ON DUPLICATE KEY UPDATE
				id=VALUES(id), phase=VALUES(phase), progress=VALUES(progress), current_step=VALUES(current_step),
				spec_json=VALUES(spec_json), lock_owner=VALUES(lock_owner), version=VALUES(version)`
	}
}
