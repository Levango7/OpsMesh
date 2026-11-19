// sql_abac_decisions.go SQLStore 的 ABACDecisionStore 实现（M3：决策审计日志）。
//
// 表结构：abac_decision_logs（migrations/036_abac_decision_logs.sql）。
// 写入按批事务提交：任一条失败整批回滚并返回 error，调用方按「未持久化」处理
// （不能只吞一半还报 accepted）。查询按 received_at 倒序，保留期删除也只按
// received_at，避免离线补传的旧 occurred_at 记录入库即被清理。
//
// 风格与 sql_abac_policy.go / sql_budget.go 一致：DB 不可用返回错误/空；失败经
// storefail 记录；时间统一 UTC。
package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

const (
	// abacDecisionListCap 单次查询最多返回条数（与内存后端 ABACDecisionLogsListCap 同值）。
	abacDecisionListCap = 1000
)

// AppendABACDecisionLogs 批量追加决策日志（单事务，全成或全败），返回接收条数。
func (s *SQLStore) AppendABACDecisionLogs(logs []model.ABACDecisionLog) (int, error) {
	if len(logs) == 0 {
		return 0, nil
	}
	if s.db == nil {
		return 0, fmt.Errorf("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		storefail.Record("[store] AppendABACDecisionLogs 开启事务失败: %v", err)
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO `abac_decision_logs` (`tenant_id`, `agent_id`, `request_id`, `bundle_id`, `policy_id`, "+
			"`allowed`, `reason`, `mode`, `occurred_at`, `received_at`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		_ = tx.Rollback()
		storefail.Record("[store] AppendABACDecisionLogs 预编译失败: %v", err)
		return 0, err
	}
	defer stmt.Close()
	now := time.Now().UTC()
	for _, l := range logs {
		received := l.ReceivedAt
		if received.IsZero() {
			received = now
		} else {
			received = received.UTC()
		}
		occurred := l.OccurredAt
		if occurred.IsZero() {
			occurred = received
		} else {
			occurred = occurred.UTC()
		}
		if _, err := stmt.ExecContext(ctx,
			l.TenantID, l.AgentID, l.RequestID, l.BundleID, l.PolicyID,
			l.Allowed, l.Reason, l.Mode, occurred, received); err != nil {
			_ = tx.Rollback()
			storefail.Record("[store] AppendABACDecisionLogs 写入失败 (tenant=%s agent=%s): %v",
				l.TenantID, l.AgentID, err)
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		storefail.Record("[store] AppendABACDecisionLogs 提交失败: %v", err)
		return 0, err
	}
	return len(logs), nil
}

// ListABACDecisionLogs 返回决策日志：tenantID 为空返回全部租户。
// 按 received_at 倒序（最新在前），limit<=0 或超过 1000 时按 1000 处理。
func (s *SQLStore) ListABACDecisionLogs(tenantID string, limit int) []model.ABACDecisionLog {
	if s.db == nil {
		return nil
	}
	if limit <= 0 || limit > abacDecisionListCap {
		limit = abacDecisionListCap
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := "SELECT `id`, `tenant_id`, `agent_id`, `request_id`, `bundle_id`, `policy_id`, `allowed`, `reason`, `mode`, `occurred_at`, `received_at` FROM `abac_decision_logs`"
	args := make([]interface{}, 0, 2)
	if tenantID != "" {
		query += " WHERE `tenant_id`=?"
		args = append(args, tenantID)
	}
	query += " ORDER BY `received_at` DESC, `id` DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		storefail.Record("[store] ListABACDecisionLogs 查询失败 (tenant=%s): %v", tenantID, err)
		return nil
	}
	defer rows.Close()
	out := make([]model.ABACDecisionLog, 0)
	for rows.Next() {
		var l model.ABACDecisionLog
		if err := rows.Scan(&l.ID, &l.TenantID, &l.AgentID, &l.RequestID, &l.BundleID, &l.PolicyID,
			&l.Allowed, &l.Reason, &l.Mode, &l.OccurredAt, &l.ReceivedAt); err != nil {
			storefail.Record("[store] ListABACDecisionLogs 扫描失败: %v", err)
			continue
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListABACDecisionLogs 迭代失败: %v", err)
	}
	return out
}

// DeleteABACDecisionLogsBefore 删除 received_at 早于 cutoff 的记录，返回删除条数。
func (s *SQLStore) DeleteABACDecisionLogsBefore(cutoff time.Time) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM `abac_decision_logs` WHERE `received_at` < ?", cutoff.UTC())
	if err != nil {
		storefail.Record("[store] DeleteABACDecisionLogsBefore 失败 (cutoff=%s): %v", cutoff.UTC(), err)
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		storefail.Record("[store] DeleteABACDecisionLogsBefore RowsAffected 失败: %v", err)
		return 0, err
	}
	return int(n), nil
}
