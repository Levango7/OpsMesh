// MySQL 选主后端：为 K8sLeaseElector 状态机提供 Acquire/Renew/Release 的 SQL 实现。
//
// 为什么不直接用 K8s client-go（TD-60 A-2 决策的延续）：task-svc 的生产形态是
// compose/Helm 双轨，私有化交付常常没有 K8s；而 StoreType=sql 时 MySQL 一定在。
// 借用 K8sLeaseOps 的三个语义（获取/续租/释放），把"租约"落成一行 SQL，
// 乐观锁由 InnoDB 行锁 + 条件 UPDATE 保证——零新增依赖，单机与 K8s 部署通吃。
//
// 时钟基准刻意用 MySQL 服务器时间（NOW(3)）而不是各 Pod 本地时钟：多副本
// 选主最怕的就是机器间时钟漂移把未过期租约判成已过期，DB 时间是唯一共识源。
package leader

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MySQLLeaseOps 基于 MySQL 的租约操作，满足 K8sLeaseOps 接口。
// leaseName 在这里是一把"锁名"（如 task-svc-leader），不是 K8s 资源名——
// 状态机对后端无感知。
type MySQLLeaseOps struct {
	db *sql.DB
}

// NewMySQLLeaseOps 构造 MySQL 租约操作（db 的生命周期归调用方管理）。
func NewMySQLLeaseOps(db *sql.DB) *MySQLLeaseOps {
	return &MySQLLeaseOps{db: db}
}

// EnsureTable 建租约表（幂等）。main 在选主模式启用时启动期调用一次。
func (m *MySQLLeaseOps) EnsureTable(ctx context.Context) error {
	const ddl = `CREATE TABLE IF NOT EXISTS task_svc_leader_lease (
		lease_name      VARCHAR(128) NOT NULL PRIMARY KEY,
		holder_identity VARCHAR(256) NOT NULL DEFAULT '',
		lease_until     DATETIME(3)  NOT NULL,
		updated_at      DATETIME(3)  NOT NULL
	) ENGINE=InnoDB`
	if _, err := m.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create task_svc_leader_lease: %w", err)
	}
	return nil
}

// Acquire 尝试获取租约（K8sLeaseOps 语义）：
//   - 表中无记录 → INSERT（成功）；
//   - 租约已过期、当前**无持有者**（已被 Release 清空），或本就由我持有 → 条件 UPDATE 改写持有者（成功）；
//   - 被其他持有者持有且未过期 → 条件不命中、UPDATE 零行（失败）。
//
// 条件更新在 InnoDB 行锁下原子完成，多副本并发 Acquire 只有一个赢家；
// 受影响行数 ≥1 即获胜（ON DUPLICATE KEY UPDATE：INSERT=1，更新且变更=2）。
//
// 「无持有者」这一支是优雅退出的接管前提：Release 把 lease_until 写成 NOW(3)
// （DATETIME(3) 毫秒精度），若只判 `lease_until < NOW(3)`，同一毫秒内的接管尝试
// 会因边界相等而落空——「不必等自然过期即可接管」在释放那一刻不成立。
func (m *MySQLLeaseOps) Acquire(ctx context.Context, leaseName, holderID string, ttl time.Duration) bool {
	const free = "holder_identity = '' OR lease_until < NOW(3) OR holder_identity = VALUES(holder_identity)"
	const q = `INSERT INTO task_svc_leader_lease (lease_name, holder_identity, lease_until, updated_at)
		VALUES (?, ?, DATE_ADD(NOW(3), INTERVAL ? MICROSECOND), NOW(3))
		ON DUPLICATE KEY UPDATE
			holder_identity = IF(` + free + `, VALUES(holder_identity), holder_identity),
			lease_until     = IF(` + free + `, VALUES(lease_until), lease_until),
			updated_at      = NOW(3)`
	res, err := m.db.ExecContext(ctx, q, leaseName, holderID, ttl.Microseconds())
	if err != nil {
		return false
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false
	}
	return m.heldBy(ctx, leaseName, holderID)
}

// Renew 续租：仅当租约仍由我持有且未过期时延长 lease_until。
// 这一条同时是"失去 leader"的判定：持有权被别人拿走后，Renew 零行命中 → false。
func (m *MySQLLeaseOps) Renew(ctx context.Context, leaseName, holderID string, ttl time.Duration) bool {
	const q = `UPDATE task_svc_leader_lease
		SET lease_until = DATE_ADD(NOW(3), INTERVAL ? MICROSECOND), updated_at = NOW(3)
		WHERE lease_name = ? AND holder_identity = ? AND lease_until > NOW(3)`
	res, err := m.db.ExecContext(ctx, q, ttl.Microseconds(), leaseName, holderID)
	if err != nil {
		return false
	}
	n, err := res.RowsAffected()
	return err == nil && n > 0
}

// Release 释放租约（仅当由我持有时清空持有者）。优雅退出时调用，
// 让其余副本不必等 lease_until 自然过期即可接管。
func (m *MySQLLeaseOps) Release(ctx context.Context, leaseName, holderID string) error {
	const q = `UPDATE task_svc_leader_lease
		SET holder_identity = '', lease_until = NOW(3), updated_at = NOW(3)
		WHERE lease_name = ? AND holder_identity = ?`
	_, err := m.db.ExecContext(ctx, q, leaseName, holderID)
	return err
}

func (m *MySQLLeaseOps) heldBy(ctx context.Context, leaseName, holderID string) bool {
	var n int
	if err := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_svc_leader_lease WHERE lease_name = ? AND holder_identity = ? AND lease_until > NOW(3)`,
		leaseName, holderID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
