// sql_tasks.go 实现 SQLStore 的 TaskStore 子接口（任务调度 + 审批 + 结果收集）。
//
// 涵盖：GetTasks/TasksByParent/SubmitResult/releaseDeps/AllTasks/Results/
// CreateTask/ClaimTask/ReclaimStaleTasks/FireDueSchedules/PendingDepth/
// TaskResult/CancelTask/CancelledTaskIDs + 新增的 ApproveTask/RejectTask。
//
// 表结构：tasks、task_results、schedules（sql.go initSchema 中建表）。
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Levango7/OpsMesh/internal/cron"
	"github.com/Levango7/OpsMesh/internal/dag"
	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// claimEpochCond 返回 SubmitResult UPDATE 语句的 claim_epoch 校验片段。
// 防双跑 + 防伪造：ClaimTask 领取时 claim_epoch=claim_epoch+1，因此已领取任务的持有者令牌必 >0。
// UPDATE 无条件附带 AND claim_epoch=?，让上报携带的令牌与库中当前值精确相等才算数：
//   - 攻击者不带头令牌（发 ClaimEpoch=0）⇒ 与已领取任务的 claim_epoch>0 不匹配 ⇒ 拒绝；
//   - 旧持有者（任务被回收重派后 epoch 已 +1）⇒ 令牌过期 ⇒ 拒绝。
//
// 仅当任务从未被领取（claim_epoch=0）时零值令牌才被放行，此时不存在抢占他人任务的语义。
// 旧实现在 epoch<=0 时返回空串（跳过校验），那是一道可被 ClaimEpoch:0 直接绕过的后门。
//
// epoch 形参保留以维持调用点形状（三个 UPDATE 语句共用），不再参与分支。
func claimEpochCond(epoch int64) string {
	return ` AND claim_epoch=?`
}

// claimEpochArgs 返回 SubmitResult UPDATE 语句的参数列表（taskID + claim_epoch）。
// 与 claimEpochCond 配对使用：无条件追加 epoch 参数（含 epoch<=0）。
func claimEpochArgs(taskID string, epoch int64) []interface{} {
	return []interface{}{taskID, epoch}
}

// checkLeaderFence leader fencing：校验当前实例是否仍持有有效租约。
// fencing 通过返回 true；失败返回 false，且若自认 leader 时打日志告警并主动放弃 leader。
// 用于 ReclaimStaleTasks/FireDueSchedules/RetireStaleDevices 等 leader 周期协调任务，
// 防止 HA 双主窗口下旧 leader 仍执行写操作（租约已过期但 IsLeader 缓存未刷新）。
func (s *SQLStore) checkLeaderFence(ctx context.Context, op string) bool {
	var fenced bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM leader_lease WHERE id=1 AND holder=? AND expires_at > NOW())`,
		s.instanceID).Scan(&fenced); err != nil {
		storefail.Record("[store] %s fencing 检查失败: %v", op, err)
		return false
	}
	if !fenced && s.IsLeader() {
		storefail.Record("[store] %s fencing 失败：自认 leader 但租约不匹配/过期，主动放弃 leader", op)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
	}
	return fenced
}

// ApproveTask 审批通过任务：将 pending_approval 状态翻转回 pending，
// 记录审批人/审批时间。仅 pending_approval 状态可审批；其他状态返回 false。
// tenantID 非空时校验任务归属，越权返回 false。
func (s *SQLStore) ApproveTask(id, tenantID, approvedBy string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	q := `UPDATE tasks SET status='pending', approved_by=?, approved_at=? WHERE task_id=? AND status='pending_approval'`
	args := []interface{}{approvedBy, now, id}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] ApproveTask 失败 %s: %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	if n > 0 {
		s.publish(events.Event{Action: "task_approved", Target: id, TenantID: tenantID,
			Detail: "by=" + approvedBy, Level: events.LevelInfo})
	}
	return n > 0
}

// RejectTask 驳回任务：将 pending_approval 状态置为 rejected，
// 记录审批人/审批时间。被驳回任务永不进入 ClaimTask 队列。仅 pending_approval 状态可驳回。
// tenantID 非空时校验任务归属，越权返回 false。
func (s *SQLStore) RejectTask(id, tenantID, approvedBy string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	q := `UPDATE tasks SET status='rejected', approved_by=?, approved_at=? WHERE task_id=? AND status='pending_approval'`
	args := []interface{}{approvedBy, now, id}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] RejectTask 失败 %s: %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	if n > 0 {
		s.publish(events.Event{Action: "task_rejected", Target: id, TenantID: tenantID,
			Detail: "by=" + approvedBy, Level: events.LevelWarn})
	}
	return n > 0
}

// RetryDeadLetterTask 重新入队死信任务：DeadLetter=false, status=pending, retry_count=0，
// 清除 ClaimedBy/ClaimedAt，使任务可再次被 ClaimTask 领取。仅 DeadLetter=true 且 status=failed 的
// 任务且 tenant 匹配时生效，返回是否更新。
func (s *SQLStore) RetryDeadLetterTask(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `UPDATE tasks SET dead_letter=0, status='pending', retry_count=0, claimed_by=NULL, claimed_at=NULL
		WHERE task_id=? AND dead_letter=1 AND status='failed'`
	args := []interface{}{id}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] RetryDeadLetterTask 失败 %s: %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	if n > 0 {
		s.publish(events.Event{Action: "task_retry_dead_letter", Target: id, TenantID: tenantID,
			Detail: "dead letter requeued", Level: events.LevelInfo})
	}
	return n > 0
}

// DeleteTask 删除任务（死信清理等）：仅 tenant 匹配时生效，返回是否删除。
func (s *SQLStore) DeleteTask(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `DELETE FROM tasks WHERE task_id=?`
	args := []interface{}{id}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] DeleteTask 失败 %s: %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	if n > 0 {
		s.publish(events.Event{Action: "task_deleted", Target: id, TenantID: tenantID,
			Detail: "task purged", Level: events.LevelWarn})
	}
	return n > 0
}

// CreateTaskWithIdempotency 下发任务并做 (tenantID, idempotencyKey) 去重（Wave 4-B）。
func (s *SQLStore) CreateTaskWithIdempotency(ctx context.Context, t *proto.Task) (*proto.Task, error) {
	if t == nil || t.IdempotencyKey == "" {
		return s.CreateTask(t)
	}
	if existing := s.GetTaskByIdempotencyKey(t.TenantID, t.IdempotencyKey); existing != nil {
		return existing, nil
	}
	return s.CreateTask(t)
}

func (s *SQLStore) GetTasks(agentID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 列清单必须与 scanTaskListRow 的 13 个 Scan 目标逐一对应（claimed_at/approval_required 列，TD-90 列数对账门禁）。
	// script_body：多行脚本体（列由 migrations/029_tasks_script_body.sql 提供）——
	//   模板类任务（OS 优化 / 中间件部署）的脚本正文走这一列，agent 据此落临时文件执行。
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at, claimed_at, approval_required, priority, script_body, dead_letter, retry_count, max_retries FROM tasks WHERE agent_id=? AND (status IS NULL OR status='pending')`, agentID)
	if err != nil {
		storefail.Record("[store] GetTasks 查询失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()

	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		if err := scanTaskListRow(rows, &t); err != nil {
			storefail.Record("[store] GetTasks 扫描失败: %v", err)
			continue
		}
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] GetTasks 遍历失败 %s: %v", agentID, err)
	}
	return out
}

// TasksByParent 返回指定 parent_id 的全部任务（跨状态），用于 M5 工作流运行归组 / F4 模板血缘。

func (s *SQLStore) TasksByParent(parentID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, status, parent_id FROM tasks WHERE parent_id=?`, parentID)
	if err != nil {
		storefail.Record("[store] TasksByParent 查询失败 %s: %v", parentID, err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		// tasks 表除 task_id（主键）外全部列可空：migrations/001 未声明 NOT NULL，
		// 且 path/content/schedule/parent_id/depends_on/last_fired_at 等列历史上由
		// alterColumnIfMissing 补到已存在的表——补列时无 DEFAULT，存量行即 NULL。
		// 用标量目标扫描时任一列为 NULL 就让整行读不出来（任务凭空消失）。
		var agentID, tenantID, typ, command, status, parentID sql.NullString
		if err := rows.Scan(&t.TaskID, &agentID, &tenantID, &typ, &command, &status, &parentID); err != nil {
			storefail.Record("[store] TasksByParent 扫描失败: %v", err)
			continue
		}
		t.AgentID, t.TenantID, t.Type = agentID.String, tenantID.String, typ.String
		t.Command, t.Status, t.ParentID = command.String, status.String, parentID.String
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] TasksByParent 遍历失败 %s: %v", parentID, err)
	}
	return out
}

// SubmitResult 写入 task_results，按成功/失败处理任务终态（F2 重试/死信）并回写设备看板。
// 状态守卫（幂等）：仅接受任务处于 running 时的上报，防止迟到/重复上报破坏终态。

func (s *SQLStore) SubmitResult(res *proto.TaskResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO task_results (task_id, agent_id, exit_code, stdout, stderr, finished_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	exit_code=VALUES(exit_code), stdout=VALUES(stdout), stderr=VALUES(stderr), finished_at=VALUES(finished_at)
`, res.TaskID, res.AgentID, res.ExitCode, res.Stdout, res.Stderr, nullTime(res.FinishedAt)); err != nil {
		storefail.Record("[store] SubmitResult 写入 task_results 失败 %s: %v", res.TaskID, err)
	}

	success := res.ExitCode == 0
	// 读取任务当前状态/重试计数/上限，决定终态。
	// 状态守卫（幂等）：仅接受任务处于 running 时的上报——防止迟到上报把已取消任务翻回，
	// 防止重复失败上报反复累计 retry_count 造成假死信。UPDATE 附带 AND status='running' 防并发窗口；
	// RowsAffected==0 表示状态被并发改写 → 跳过后续事件/告警。
	// 防双跑 + 防伪造：UPDATE 无条件附带 AND claim_epoch=?，令牌必须与库中当前值相等。
	// ClaimTask 领取时 claim_epoch=claim_epoch+1，故已领取任务的令牌必 >0：
	//   - 攻击者不带头令牌（发 ClaimEpoch=0）⇒ 与已领取任务的 claim_epoch>0 不匹配 ⇒ 拒绝；
	//   - 旧持有者（任务被回收重派后 epoch 已 +1）⇒ 令牌过期 ⇒ 拒绝。
	// 仅当任务从未被领取（claim_epoch=0，如直接置 Status="running" 的既有测试/内部构造）时，
	// 零值令牌才被放行——此时任务未经任何 agent 认领，不存在"抢占他人任务"的语义。
	// 本仓库 agent 恒填充 res.ClaimEpoch = t.ClaimEpoch（见 internal/agent/agent.go execute()）。
	var tid string
	var tenantID, status sql.NullString
	var rc, mr sql.NullInt64
	accepted := false
	if err := s.db.QueryRowContext(ctx,
		`SELECT task_id, tenant_id, status, retry_count, max_retries FROM tasks WHERE task_id=?`, res.TaskID,
	).Scan(&tid, &tenantID, &status, &rc, &mr); err == nil && tid != "" {
		if status.String != "running" {
			log.Printf("[store] SubmitResult 忽略非 running 任务 %s (status=%s exitCode=%d)", res.TaskID, status.String, res.ExitCode)
		} else if success {
			if r, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='done' WHERE task_id=? AND status='running'`+claimEpochCond(res.ClaimEpoch),
				claimEpochArgs(res.TaskID, res.ClaimEpoch)...); uerr != nil {
				storefail.Record("[store] SubmitResult done 更新失败 %s: %v", res.TaskID, uerr)
			} else if n, raErr := r.RowsAffected(); raErr == nil && n > 0 {
				accepted = true
			} else {
				log.Printf("[store] SubmitResult 拒绝旧持有者上报 %s (claim_epoch=%d 不匹配)", res.TaskID, res.ClaimEpoch)
			}
		} else if rc.Int64 < mr.Int64 {
			r, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL, retry_count=retry_count+1 WHERE task_id=? AND status='running'`+claimEpochCond(res.ClaimEpoch),
				claimEpochArgs(res.TaskID, res.ClaimEpoch)...)
			if uerr != nil {
				storefail.Record("[store] SubmitResult retry 更新失败 %s: %v", res.TaskID, uerr)
			} else if n, raErr := r.RowsAffected(); raErr == nil && n > 0 {
				accepted = true
				s.publish(events.Event{Action: "task_retry", Target: res.TaskID, TenantID: tenantID.String,
					Detail: fmt.Sprintf("retry %d/%d", rc.Int64+1, mr.Int64), Level: events.LevelWarn})
			} else {
				log.Printf("[store] SubmitResult 拒绝旧持有者重试 %s (claim_epoch=%d 不匹配)", res.TaskID, res.ClaimEpoch)
			}
		} else {
			r, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='failed', dead_letter=1 WHERE task_id=? AND status='running'`+claimEpochCond(res.ClaimEpoch),
				claimEpochArgs(res.TaskID, res.ClaimEpoch)...)
			if uerr != nil {
				storefail.Record("[store] SubmitResult dead-letter 更新失败 %s: %v", res.TaskID, uerr)
			} else if n, raErr := r.RowsAffected(); raErr == nil && n > 0 {
				accepted = true
				s.addAlert(ctx, &proto.Alert{
					AlertID:   "alert-" + res.TaskID,
					TenantID:  tenantID.String,
					DeviceID:  "dev-" + res.AgentID,
					AgentID:   res.AgentID,
					Severity:  "critical",
					Message:   fmt.Sprintf("task %s dead-letter after %d retries (exitCode=%d)", res.TaskID, rc.Int64, res.ExitCode),
					CreatedAt: time.Now().UTC(),
				})
			} else {
				log.Printf("[store] SubmitResult 拒绝旧持有者死信 %s (claim_epoch=%d 不匹配)", res.TaskID, res.ClaimEpoch)
			}
		}
	}

	// 任务存在但上报被忽略（非 running）：结果已记录，不再回写看板/事件/依赖释放。
	if tid != "" && !accepted {
		return
	}

	// 回写设备 TaskState + LastResult（失败回写看板）。
	lastResult := "success"
	taskState := "done"
	if !success {
		lastResult = "failed"
		taskState = "idle"
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE devices SET task_state=?, last_result=?, last_result_at=? WHERE agent_id=?`,
		taskState, lastResult, time.Now().UTC(), res.AgentID); err != nil {
		storefail.Record("[store] SubmitResult 更新 devices 失败 %s: %v", res.AgentID, err)
	}

	lvl := events.LevelInfo
	if !success {
		lvl = events.LevelWarn
	}
	s.publish(events.Event{Action: "report_result", Target: res.TaskID, TenantID: "", Detail: fmt.Sprintf("exitCode=%d", res.ExitCode), Level: lvl})

	// M5 作业编排：本任务到达终态后重新评估依赖它的 blocked 任务。
	// 成功与失败都要调：上游失败时下游必须被显式置 failed + 死信，
	// 否则它永远停在 blocked（不可领取、不在死信视图、无人看护）。
	s.releaseDeps(ctx, res.AgentID, res.TaskID)
}

// releaseDeps M5 作业编排：任务到达终态后，重新评估同 agent 下依赖它的 blocked 任务。
//   - 前置依赖全部 done ⇒ 释放为 pending（进入可下发队列）；
//   - 任一前置依赖 failed/cancelled ⇒ 下游置 failed 并标记 dead_letter，
//     使其进入死信视图（可被 dead-letter 端点看到并人工重试），而不是永久 blocked。
//
// 调用方：SubmitResult（成功与失败都调，与内存后端一致）。
// 旧实现仅在 success 时调用且要求 AllDepsDone，上游一失败下游就永久停在 blocked。
func (s *SQLStore) releaseDeps(ctx context.Context, agentID, doneTaskID string) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, depends_on FROM tasks WHERE agent_id=? AND status='blocked'`, agentID)
	if err != nil {
		return
	}
	defer rows.Close()
	type rec struct {
		id   string
		deps string
	}
	var blocked []rec
	for rows.Next() {
		var r rec
		var deps sql.NullString
		if serr := rows.Scan(&r.id, &deps); serr != nil {
			continue
		}
		r.deps = deps.String
		blocked = append(blocked, r)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] releaseDeps 查询 blocked 任务遍历失败 %s: %v", agentID, err)
		return
	}
	if len(blocked) == 0 {
		return
	}
	// 取该 agent 全部任务状态，构建 byID 索引供 dag 判定。
	all, err := s.db.QueryContext(ctx,
		`SELECT task_id, status FROM tasks WHERE agent_id=?`, agentID)
	if err != nil {
		return
	}
	defer all.Close()
	byID := make(map[string]*proto.Task)
	for all.Next() {
		var id string
		var st sql.NullString
		if err := all.Scan(&id, &st); err != nil {
			continue
		}
		byID[id] = &proto.Task{TaskID: id, Status: st.String}
	}
	if err := all.Err(); err != nil {
		storefail.Record("[store] releaseDeps 查询任务状态遍历失败 %s: %v", agentID, err)
		return
	}

	for _, b := range blocked {
		var deps []string
		if b.deps != "" {
			if err := json.Unmarshal([]byte(b.deps), &deps); err != nil {
				storefail.Record("[store] 解析阻塞任务 %s 依赖 JSON 失败: %v", b.id, err)
			}
		}
		t := &proto.Task{TaskID: b.id, DependsOn: deps, Status: "blocked"}
		// 依赖未全部达终态 ⇒ 继续等待（其余任务上报后再次评估）。
		if !dag.AllDepsTerminal(t, byID) {
			continue
		}
		if failedDep, failed := dag.AnyDepFailed(t, byID); failed {
			// 上游失败/取消 ⇒ 下游不可能再执行：置 failed + 死信，使其可见可处置。
			if _, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='failed', dead_letter=1, claimed_by=NULL, claimed_at=NULL WHERE task_id=?`, b.id); uerr != nil {
				storefail.Record("[store] releaseDeps 标记失败 %s: %v", b.id, uerr)
				continue
			}
			reason := fmt.Sprintf("upstream dependency %s %s", failedDep, byID[failedDep].Status)
			s.addAlert(ctx, &proto.Alert{
				AlertID:   "alert-" + b.id,
				TenantID:  "",
				DeviceID:  "dev-" + agentID,
				AgentID:   agentID,
				Severity:  "critical",
				Message:   fmt.Sprintf("task %s failed: %s", b.id, reason),
				CreatedAt: time.Now().UTC(),
			})
			s.publish(events.Event{Action: "task_failed", Target: b.id, TenantID: "",
				Detail: reason + " (trigger=" + doneTaskID + ", dead-letter)", Level: events.LevelWarn})
			continue
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL WHERE task_id=?`, b.id); err == nil {
			s.publish(events.Event{Action: "task_released", Target: b.id, TenantID: "",
				Detail: "deps done (trigger=" + doneTaskID + ")", Level: events.LevelInfo})
		}
	}
}

// addAlert 写入一条告警（M7）。

func (s *SQLStore) AllTasks(tenantID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at, claimed_at, approval_required, priority, script_body, dead_letter, retry_count, max_retries FROM tasks`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		storefail.Record("[store] AllTasks 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		if err := scanTaskListRow(rows, &t); err != nil {
			storefail.Record("[store] AllTasks 扫描失败: %v", err)
			continue
		}
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] AllTasks 遍历失败: %v", err)
	}
	return out
}

// TaskByID 按 taskID 返回单条任务（不存在返回 nil）。供按 ID 直查场景（如租户归属校验）。

func (s *SQLStore) TaskByID(taskID string) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at, claimed_at, approval_required, priority, script_body, dead_letter, retry_count, max_retries FROM tasks WHERE task_id=?`, taskID)
	var t proto.Task
	if err := scanTaskListRow(row, &t); err != nil {
		if err != sql.ErrNoRows {
			storefail.Record("[store] TaskByID 查询失败 %s: %v", taskID, err)
		}
		return nil
	}
	return &t
}

// GetTaskByIdempotencyKey 按 (tenant_id, idempotency_key) 返回已存在任务（不存在返回 nil）（Wave 4-B）。
func (s *SQLStore) GetTaskByIdempotencyKey(tenantID, key string) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at, claimed_at, approval_required, priority, script_body, dead_letter, retry_count, max_retries FROM tasks WHERE tenant_id=? AND idempotency_key=? LIMIT 1`,
		tenantID, key)
	var t proto.Task
	if err := scanTaskListRow(row, &t); err != nil {
		if err != sql.ErrNoRows {
			storefail.Record("[store] GetTaskByIdempotencyKey 查询失败 %s/%s: %v", tenantID, key, err)
		}
		return nil
	}
	return &t
}

// ListTasksByPriority 按优先级降序分页列出任务（tenantID 非空时按租户过滤；返回列表与总数）（Wave 4-B）。
func (s *SQLStore) ListTasksByPriority(tenantID string, offset, limit int) ([]*proto.Task, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE (tenant_id=? OR ?='')`, tenantID, tenantID).Scan(&total); err != nil {
		storefail.Record("[store] ListTasksByPriority 计数失败: %v", err)
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return nil, total, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at, claimed_at, approval_required, priority, script_body, dead_letter, retry_count, max_retries FROM tasks WHERE (tenant_id=? OR ?='') ORDER BY priority DESC, created_at ASC, task_id ASC LIMIT ? OFFSET ?`,
		tenantID, tenantID, limit, offset)
	if err != nil {
		storefail.Record("[store] ListTasksByPriority 查询失败: %v", err)
		return nil, 0, err
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		if err := scanTaskListRow(rows, &t); err != nil {
			storefail.Record("[store] ListTasksByPriority 扫描失败: %v", err)
			continue
		}
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListTasksByPriority 遍历失败: %v", err)
	}
	return out, total, nil
}

// taskRowScanner 是 *sql.Row 与 *sql.Rows 的共同最小子集，
// 使共用同一列清单的读侧（GetTasks/AllTasks/TaskByID）只写一遍 Scan 目标映射。
type taskRowScanner interface {
	Scan(dest ...any) error
}

// scanTaskListRow 读 tasks 的 13 列子集：
// task_id, agent_id, tenant_id, type, command, content, path, status,
// created_at, claimed_at, approval_required, priority, script_body
//
// claimed_at：任务超时监控（server_tasks.go monitorTaskTimeouts）按 ClaimedAt 计超时，
// 历史上本列不在 SELECT 里，SQL 后端 AllTasks 恒返回零值 ClaimedAt，
// 超时判定静默退化为 CreatedAt（排队 5 分钟、Timeout=60s 的任务一开跑就被取消）。
//
// approval_required：审批标记，使 SQL 后端的读写侧与内存后端一致（CreateTask 落库）。
//
// script_body：多行脚本体（列由 migrations/029_tasks_script_body.sql 提供）。
//   模板类任务的脚本正文只存在这一列——漏读会让 agent 拿不到脚本正文，
//   表现为「任务被领取但什么都没执行」。

// 除 task_id（主键）外全部列历史上可空（含迁移 028 前的存量行），故一律用 sql.Null* 承接——
// 裸 string/time.Time 目标遇 NULL 会让整行读不出来（"converting NULL to string is
// unsupported"），表现为任务在列表与直查里同时消失。
func scanTaskListRow(row taskRowScanner, t *proto.Task) error {
	var agentID, tenantID, typ, command, content, path, status sql.NullString
	var createdAt, claimedAt sql.NullTime
	var approvalRequired, priority, deadLetter, retryCount, maxRetries sql.NullInt64
	var scriptBody sql.NullString
	if err := row.Scan(&t.TaskID, &agentID, &tenantID, &typ, &command, &content, &path, &status, &createdAt, &claimedAt, &approvalRequired, &priority, &scriptBody, &deadLetter, &retryCount, &maxRetries); err != nil {
		return err
	}
	t.AgentID, t.TenantID, t.Type = agentID.String, tenantID.String, typ.String
	t.Command, t.Content, t.Path = command.String, content.String, path.String
	t.Status, t.CreatedAt, t.ClaimedAt = status.String, createdAt.Time, claimedAt.Time
	t.ApprovalRequired = approvalRequired.Int64 != 0
	t.Priority = int(priority.Int64)
	t.ScriptBody = scriptBody.String
	t.DeadLetter = deadLetter.Int64 != 0
	t.RetryCount = int(retryCount.Int64)
	t.MaxRetries = int(maxRetries.Int64)
	return nil
}

// Device 按 deviceID 返回单台设备（供设备详情端点）。

func (s *SQLStore) Results(agentID string) []*proto.TaskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, exit_code, stdout, stderr, finished_at FROM task_results WHERE agent_id=? ORDER BY finished_at DESC`, agentID)
	if err != nil {
		storefail.Record("[store] Results 查询失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()
	var out []*proto.TaskResult
	for rows.Next() {
		var r proto.TaskResult
		if err := scanTaskResultRow(rows, &r); err != nil {
			storefail.Record("[store] Results 扫描失败: %v", err)
			continue
		}
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] Results 遍历失败 %s: %v", agentID, err)
	}
	return out
}

// Agents 返回已注册 agent（tenantID 非空时按租户过滤）。

// CreateTask 落库一个任务。
//
// 2026-10-12 缺口 3：INSERT 失败从"记 storefail 后返回入参装作成功"改为**返回 error**。
// 旧行为的后果：handler 给客户端 201，库里没有这条任务，agent 永远领不到——客户端
// 拿着一个不存在的 taskID 轮询结果，只会得到永远的 404，而服务端日志里那行
// storefail 没人看。失败必须响亮地传到调用方。
func (s *SQLStore) CreateTask(t *proto.Task) (*proto.Task, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if t.AgentID == "" {
		return t, nil // 调用方需保证 agentID 非空（既有契约，不在此收紧）
	}
	if t.TaskID == "" {
		t.TaskID = "task-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	// M5 作业编排：含前置依赖的任务初始为 blocked，待依赖 done 后释放为 pending。
	if len(t.DependsOn) > 0 {
		t.Status = "blocked"
	}
	// 任务审批：高风险任务（ApprovalRequired=true）初始为 pending_approval，
	// 不进入 ClaimTask 队列（ClaimTask 仅领取 pending）；待 ApproveTask 翻转为 pending 后才可下发。
	// 与内存后端 memory.go CreateTask 保持逐字一致的判定（ApprovalRequired && Status=="pending"）。
	// 历史上本分支缺失：SQL 生产库里高风险任务一落库就是 pending 可被立即领取，
	// ApproveTask/RejectTask 的 WHERE status='pending_approval' 匹配不到行 → 审批 API 静默 404。
	if t.ApprovalRequired && t.Status == "pending" {
		t.Status = "pending_approval"
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	// approval_required：持久化审批标记，使 ApproveTask/RejectTask 在 SQL 后端可找到目标行
	// （列由 migrations/028_tasks_approval_required.sql 提供）。
	approvalRequired := 0
	if t.ApprovalRequired {
		approvalRequired = 1
	}
	// M5 作业编排：depends_on 必须落库（JSON 数组，releaseDeps 由此列读依赖边）。
	// 缺此列写入 ⇒ 依赖边在 SQL 后端静默丢失：releaseDeps 把含依赖的 blocked 任务当无依赖释放，
	// 上游失败时下游 pending 卡死（sql_tasks_fixes_test FIX 5 抓出的假装持久化，TD-68 同族）。
	var dependsOnJSON interface{}
	if len(t.DependsOn) > 0 {
		if b, jerr := json.Marshal(t.DependsOn); jerr == nil {
			dependsOnJSON = string(b)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, retry_count, max_retries, dead_letter, schedule, parent_id, created_at, timeout, retry_delay, priority, idempotency_key, approval_required, script_body, depends_on)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE type=VALUES(type), command=VALUES(command), content=VALUES(content),
		   path=VALUES(path), status=VALUES(status), schedule=VALUES(schedule), parent_id=VALUES(parent_id),
		   max_retries=VALUES(max_retries), timeout=VALUES(timeout), retry_delay=VALUES(retry_delay),
		   priority=VALUES(priority), idempotency_key=VALUES(idempotency_key), approval_required=VALUES(approval_required),
		   script_body=VALUES(script_body), depends_on=VALUES(depends_on)`,
		t.TaskID, t.AgentID, t.TenantID, t.Type, t.Command, t.Content, t.Path, t.Status,
		t.RetryCount, t.MaxRetries, t.DeadLetter, t.Schedule, t.ParentID, t.CreatedAt,
		t.Timeout, t.RetryDelay, t.Priority, t.IdempotencyKey, approvalRequired, t.ScriptBody, dependsOnJSON); err != nil {
		storefail.Record("[store] CreateTask 失败 %s: %v", t.TaskID, err)
		return nil, fmt.Errorf("[store] CreateTask 落库失败 task=%s: %w", t.TaskID, err)
	}
	s.publish(events.Event{Action: "create_task", Target: t.TaskID, TenantID: t.TenantID, Detail: t.Command, Level: events.LevelInfo})
	return t, nil
}

// ClaimTask 原子领取该 agent 的下一条 pending 任务（FOR UPDATE 行锁保证多副本不双领）。
// 防双跑：领取时 claim_epoch=claim_epoch+1，返回的 Task 带 ClaimEpoch；
// agent 上报结果时携带 ClaimEpoch，SubmitResult 校验持有者是否仍为当前 epoch。
//
// 租户隔离（P0-6，第二道闸）：领取条件额外要求任务的 tenant_id 与「agent 自身租户」一致。
// 下发侧已在各 HTTP/执行器路径校验目标 agent 归属（tenant_guard.go），此处是数据层兜底——
// 即便未来新增的下发路径漏校验，跨租户任务也永远领不出去（不会落到对方 agent 上执行）。
// 容忍任务侧 tenant_id 为空/NULL（迁移 011 之前的存量任务），避免历史任务被静默饿死。
func (s *SQLStore) ClaimTask(agentID string) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// agent 自身租户（agent 未注册/无租户时为空 → 跳过租户过滤，保持既有行为）。
	agentTenant := ""
	if a := s.Agent(agentID); a != nil {
		agentTenant = a.TenantID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		storefail.Record("[store] ClaimTask begin 失败 %s: %v", agentID, err)
		return nil
	}
	defer tx.Rollback()

	var taskID string
	var tenantID, typ, command, content, path sql.NullString
	var createdAt sql.NullTime
	var claimEpoch sql.NullInt64
	var timeout, retryDelay sql.NullInt64
	var priority sql.NullInt64
	var scriptBody sql.NullString
	// (tenant_id IS NULL OR tenant_id='' OR tenant_id=?)：空租户任务视为「无租户标记」放行，
	// 与下发侧对存量数据的兼容策略一致。
	// 同理 status/schedule 也可能是 NULL——查询显式把 NULL 视为 pending，
	// 扫描却必须用 sql.Null* 承接，否则这些行会被驱动判成转换错误而整行丢弃（agent 领不到任务）。
	// Wave 4-B：ORDER BY priority DESC, created_at ASC，高优先级任务先被领取，同优先级按下发先后。
	if err := tx.QueryRowContext(ctx,
		`SELECT task_id, tenant_id, type, command, content, path, created_at, claim_epoch, timeout, retry_delay, priority, script_body FROM tasks
		 WHERE agent_id=? AND (status IS NULL OR status='pending') AND (schedule IS NULL OR schedule='')
		   AND (tenant_id IS NULL OR tenant_id='' OR tenant_id=?)
		 ORDER BY priority DESC, created_at ASC LIMIT 1 FOR UPDATE`,
		agentID, agentTenant).Scan(&taskID, &tenantID, &typ, &command, &content, &path, &createdAt, &claimEpoch, &timeout, &retryDelay, &priority, &scriptBody); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		storefail.Record("[store] ClaimTask 查询失败 %s: %v", agentID, err)
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status='running', claimed_by=?, claimed_at=?, claim_epoch=claim_epoch+1 WHERE task_id=?`,
		agentID, time.Now().UTC(), taskID); err != nil {
		storefail.Record("[store] ClaimTask 更新失败 %s: %v", taskID, err)
		return nil
	}
	if err := tx.Commit(); err != nil {
		storefail.Record("[store] ClaimTask commit 失败 %s: %v", taskID, err)
		return nil
	}
	return &proto.Task{
		TaskID: taskID, AgentID: agentID, TenantID: tenantID.String, Type: typ.String, Command: command.String,
		Content: content.String, Path: path.String,
		ScriptBody: scriptBody.String,
		Status:     "running", CreatedAt: createdAt.Time, ClaimedBy: agentID, ClaimedAt: time.Now().UTC(),
		ClaimEpoch: claimEpoch.Int64 + 1,
		Timeout:    int(timeout.Int64),
		RetryDelay: int(retryDelay.Int64),
		Priority:   int(priority.Int64),
	}
}

// ReclaimStaleTasks 复位超期未完成的 running 任务为 pending（任务必达）。
// agent 经 ClaimTask 领取（claimed_at 写当前时间）后若失联、超过 maxAge 仍未上报结果，
// 该任务将永远卡在 running；此处周期性调用把它复位，重新进入调度队列。
//
// 防双跑：增加 agent 心跳校验——仅当任务的 claimed_by 对应 agent 心跳也超时（last_seen < cutoff）
// 才回收。心跳正常的 agent 仍可能在执行长任务，不回收避免双跑。
// 计入重试预算：每次回收 retry_count+1，达 MaxRetries 后按 SubmitResult 的死信规则置
// failed + dead_letter（毒任务有限次复活后进死信，可被 dead-letter 端点看到并人工重试）。
// leader fencing：仅当前 leader_lease 持有者可执行回收，防 HA 双主窗口下重复回收。

func (s *SQLStore) ReclaimStaleTasks(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// leader fencing：非有效租约持有者跳过，防双主同时回收。
	if !s.checkLeaderFence(ctx, "ReclaimStaleTasks") {
		return 0
	}
	cutoff := time.Now().UTC().Add(-maxAge)
	// 计入重试预算：每次回收都等价于一次执行失败（agent 失联），retry_count+1。
	// 达上限（retry_count+1 >= max_retries）的任务按 SubmitResult 的死信规则置
	// failed + dead_letter=1，使其进入死信视图；有余量的任务复位为 pending 重新入队。
	// 旧实现只复位不计数：MaxRetries 形同虚设，毒任务无限复活且永远不可见（无人看护）。
	// 语义与内存后端 memory.go ReclaimStaleTasks 逐条对齐。
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET
		   status = CASE WHEN retry_count + 1 >= max_retries THEN 'failed' ELSE 'pending' END,
		   retry_count = retry_count + 1,
		   dead_letter = CASE WHEN retry_count + 1 >= max_retries THEN 1 ELSE dead_letter END,
		   claimed_by = NULL,
		   claimed_at = NULL
		 WHERE status='running' AND claimed_at < ?
		   AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.agent_id = tasks.claimed_by AND a.last_seen > ?)`,
		cutoff, cutoff)
	if err != nil {
		storefail.Record("[store] ReclaimStaleTasks 失败: %v", err)
		return 0
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] ReclaimStaleTasks RowsAffected: %v", rowsErr)
		return 0
	}
	return int(n)
}

// FireDueSchedules 评估所有模板任务（parent_id 为空且 schedule 非空），
// 对到点（cron 匹配 now 且 last_fired_at 早于本分钟）的模板派生一个 pending 实例并回写 last_fired_at。
// 返回本批次派生的实例数（F4 定时/周期调度；控制面 scheduleLoop 周期调用）。
// 注意：SQL 路径依赖 live MySQL，仅在 CI 集成测试（env 门控）中真正运行。
//
// 派生原子化：SELECT→INSERT→UPDATE 包在同一事务内，失败 Rollback，
// 避免派生实例已写入但 last_fired_at 未回写导致下一轮重复派生。
// leader fencing：仅当前 leader_lease 持有者可派生，防 HA 双主窗口下重复派生。

func (s *SQLStore) FireDueSchedules(now time.Time) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// leader fencing：非有效租约持有者跳过，防双主同时派生。
	if !s.checkLeaderFence(ctx, "FireDueSchedules") {
		return 0
	}
	minuteStart := now.Truncate(time.Minute)
	// 派生原子化：整批 SELECT→INSERT→UPDATE 包在单事务内，失败 Rollback。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		storefail.Record("[store] FireDueSchedules begin 失败: %v", err)
		return 0
	}
	defer tx.Rollback()
	// FOR UPDATE 锁模板行：并发事务串行化，先到者更新 last_fired_at 后，
	// 后到者重新读到 last_fired_at >= minuteStart 跳过，避免重复派生。
	rows, err := tx.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, max_retries, schedule, last_fired_at, timeout, retry_delay, script_body
		 FROM tasks WHERE (parent_id IS NULL OR parent_id='') AND schedule <> '' AND (status IS NULL OR status='pending')
		 FOR UPDATE`)
	if err != nil {
		storefail.Record("[store] FireDueSchedules 查询失败: %v", err)
		return 0
	}
	defer rows.Close()
	type tpl struct {
		id, agentID, tenantID, typ, command, content, path, schedule string
		maxRetries, timeout, retryDelay                              int
		lastFiredAt                                                  time.Time
		scriptBody                                                   string
	}
	due := make([]tpl, 0)
	for rows.Next() {
		var tp tpl
		var lf sql.NullTime
		// content/command/path 用 NullString 承接：tasks 表 content TEXT 允许 NULL
		//（migrations/001 无 NOT NULL），裸 string 扫描遇 NULL 即炸
		// "converting NULL to string is unsupported"（双轨观察栈实测：
		// shadow-seed 模板 content=NULL 时 FireDueSchedules 每轮静默失败）。
		// 与本文件 PendingTasks/:138、ListTasks/:376、GetTask/:401 的处理一致。
		var content, command, path, agentID, tenantID, typ, schedule sql.NullString
		var maxRetries, timeout, retryDelay sql.NullInt64
		var scriptBody sql.NullString
		if err := rows.Scan(&tp.id, &agentID, &tenantID, &typ, &command, &content, &path, &maxRetries, &schedule, &lf, &timeout, &retryDelay, &scriptBody); err != nil {
			storefail.Record("[store] FireDueSchedules 扫描失败: %v", err)
			continue
		}
		tp.agentID, tp.tenantID, tp.typ = agentID.String, tenantID.String, typ.String
		tp.maxRetries, tp.schedule = int(maxRetries.Int64), schedule.String
		tp.timeout, tp.retryDelay = int(timeout.Int64), int(retryDelay.Int64)
		if content.Valid {
			tp.content = content.String
		}
		if command.Valid {
			tp.command = command.String
		}
		if path.Valid {
			tp.path = path.String
		}
		// script_body：多行脚本体随模板任务继承到派生实例
		// （与内存后端 memory.go FireDueSchedules 的继承口径一致）。
		if scriptBody.Valid {
			tp.scriptBody = scriptBody.String
		}
		if lf.Valid {
			tp.lastFiredAt = lf.Time
		}
		ok, merr := cron.Match(tp.schedule, now)
		if merr != nil || !ok {
			continue
		}
		// 本分钟已触发过则跳过，避免重复派生。
		if !tp.lastFiredAt.IsZero() && !tp.lastFiredAt.Before(minuteStart) {
			continue
		}
		due = append(due, tp)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] FireDueSchedules 遍历失败: %v", err)
		return 0
	}
	fired := 0
	for _, tp := range due {
		instID := fmt.Sprintf("task-%d-%s", now.UnixNano(), tp.id)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, retry_count, max_retries, schedule, parent_id, created_at, timeout, retry_delay, script_body)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, '', ?, ?, ?, ?, ?)`,
			instID, tp.agentID, tp.tenantID, tp.typ, tp.command, tp.content, tp.path, tp.maxRetries, tp.id, now.UTC(), tp.timeout, tp.retryDelay, tp.scriptBody); err != nil {
			storefail.Record("[store] FireDueSchedules 派生实例失败 %s: %v", instID, err)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET last_fired_at=? WHERE task_id=?`, now.UTC(), tp.id); err != nil {
			storefail.Record("[store] FireDueSchedules 回写 last_fired_at 失败 %s: %v", tp.id, err)
			continue
		}
		fired++
		s.publish(events.Event{Action: "schedule_fire", Target: instID, TenantID: tp.tenantID,
			Detail: "parent=" + tp.id + " cron=" + tp.schedule, Level: events.LevelInfo})
	}
	if err := tx.Commit(); err != nil {
		storefail.Record("[store] FireDueSchedules commit 失败: %v", err)
		return 0
	}
	return fired
}

// UpsertDevice 写入/更新一台纳管设备（真实网段发现 用；按 device_id 幂等）。

func (s *SQLStore) PendingDepth() int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status IS NULL OR status='pending'`).Scan(&n); err != nil {
		storefail.Record("[store] PendingDepth 失败: %v", err)
		return 0
	}
	return n
}

// TaskResult 按 taskID 返回单条执行结果（A5/F7 结果查询 API）。

func (s *SQLStore) TaskResult(taskID string) *proto.TaskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT task_id, agent_id, exit_code, stdout, stderr, finished_at FROM task_results WHERE task_id=?`, taskID)
	var r proto.TaskResult
	if err := scanTaskResultRow(row, &r); err != nil {
		if err != sql.ErrNoRows {
			storefail.Record("[store] TaskResult 查询失败 %s: %v", taskID, err)
		}
		return nil
	}
	return &r
}

// scanTaskResultRow 读 task_results 的 6 列：
// task_id, agent_id, exit_code, stdout, stderr, finished_at
//
// task_id 是主键，其余列在 migrations/001 里均未声明 NOT NULL；
// SubmitResult 对空 stdout/stderr 与零值 FinishedAt 是照写的（agent 上报允许空输出），
// 裸目标扫描会让这类行整行读不出来（执行结果凭空消失）。
func scanTaskResultRow(row taskRowScanner, r *proto.TaskResult) error {
	var agentID, stdout, stderr sql.NullString
	var exitCode sql.NullInt64
	var finishedAt sql.NullTime
	if err := row.Scan(&r.TaskID, &agentID, &exitCode, &stdout, &stderr, &finishedAt); err != nil {
		return err
	}
	r.AgentID, r.ExitCode = agentID.String, int(exitCode.Int64)
	r.Stdout, r.Stderr, r.FinishedAt = stdout.String, stderr.String, finishedAt.Time
	return nil
}

// CancelTask 取消任务（F3）：pending/running -> cancelled；已 done/failed/cancelled 不可取消。

func (s *SQLStore) CancelTask(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status='cancelled' WHERE task_id=? AND (status='pending' OR status='running')
		 AND (tenant_id=? OR ?='')`,
		id, tenantID, tenantID)
	if err != nil {
		storefail.Record("[store] CancelTask 失败 %s: %v", id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	if n > 0 {
		// M5 作业编排：取消也是终态，须重新评估依赖它的 blocked 下游
		// （上游被取消 ⇒ 下游不可能执行，应置 failed + 死信而不是永久 blocked）。
		var agentID sql.NullString
		if err := s.db.QueryRowContext(ctx,
			`SELECT agent_id FROM tasks WHERE task_id=?`, id).Scan(&agentID); err == nil {
			s.releaseDeps(ctx, agentID.String, id)
		}
	}
	return n > 0
}

// RetireDevice 退役/下线设备（F5）：标记 retired；tenantID 非空时校验租户。

func (s *SQLStore) CancelledTaskIDs(agentID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id FROM tasks WHERE agent_id=? AND status='cancelled'`, agentID)
	if err != nil {
		storefail.Record("[store] CancelledTaskIDs 失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] CancelledTaskIDs 遍历失败 %s: %v", agentID, err)
	}
	return out
}

// CleanupTokens 清理过期/已消费的 install token（F9 无界增长防护）。
