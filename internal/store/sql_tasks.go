// sql_tasks.go 实现 SQLStore 的 TaskStore 子接口（任务调度 + 审批 + 结果收集）。
//
// 涵盖：GetTasks/TasksByParent/SubmitResult/releaseDeps/AllTasks/Results/
// CreateTask/ClaimTask/ReclaimStaleTasks/FireDueSchedules/PendingDepth/
// TaskResult/CancelTask/CancelledTaskIDs + task 100 新增的 ApproveTask/RejectTask。
//
// 表结构：tasks、task_results、schedules（sql.go initSchema 中建表）。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"opsmesh/internal/cron"
	"opsmesh/internal/dag"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// claimEpochCond 返回 SubmitResult UPDATE 语句的 claim_epoch 校验片段。
// A-1 防双跑：epoch > 0 时追加 AND claim_epoch=? 校验持有者令牌；
// epoch == 0 时返回空串（兼容旧 agent/测试，跳过校验）。
func claimEpochCond(epoch int64) string {
	if epoch > 0 {
		return ` AND claim_epoch=?`
	}
	return ""
}

// claimEpochArgs 返回 SubmitResult UPDATE 语句的参数列表（taskID + 可选 claim_epoch）。
// 与 claimEpochCond 配对使用：epoch > 0 时追加 epoch 参数。
func claimEpochArgs(taskID string, epoch int64) []interface{} {
	if epoch > 0 {
		return []interface{}{taskID, epoch}
	}
	return []interface{}{taskID}
}

// checkLeaderFence A-2 leader fencing：校验当前实例是否仍持有有效租约。
// fencing 通过返回 true；失败返回 false，且若自认 leader 时打日志告警并主动放弃 leader。
// 用于 ReclaimStaleTasks/FireDueSchedules/RetireStaleDevices 等 leader 周期协调任务，
// 防止 HA 双主窗口下旧 leader 仍执行写操作（租约已过期但 IsLeader 缓存未刷新）。
func (s *SQLStore) checkLeaderFence(ctx context.Context, op string) bool {
	var fenced bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM leader_lease WHERE id=1 AND holder=? AND expires_at > NOW())`,
		s.instanceID).Scan(&fenced); err != nil {
		log.Printf("[store] %s fencing 检查失败: %v", op, err)
		return false
	}
	if !fenced && s.IsLeader() {
		log.Printf("[store] %s fencing 失败：自认 leader 但租约不匹配/过期，主动放弃 leader", op)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
	}
	return fenced
}

// ApproveTask 审批通过任务（task 100）：将 pending_approval 状态翻转回 pending，
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
		log.Printf("[store] ApproveTask 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.publish(events.Event{Action: "task_approved", Target: id, TenantID: tenantID,
			Detail: "by=" + approvedBy, Level: events.LevelInfo})
	}
	return n > 0
}

// RejectTask 驳回任务（task 100）：将 pending_approval 状态置为 rejected，
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
		log.Printf("[store] RejectTask 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.publish(events.Event{Action: "task_rejected", Target: id, TenantID: tenantID,
			Detail: "by=" + approvedBy, Level: events.LevelWarn})
	}
	return n > 0
}

func (s *SQLStore) GetTasks(agentID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, type, command, content, path, status, created_at FROM tasks WHERE agent_id=? AND (status IS NULL OR status='pending')`, agentID)
	if err != nil {
		log.Printf("[store] GetTasks 查询失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()

	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		var content, path sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.Type, &t.Command, &content, &path, &t.Status, &createdAt); err != nil {
			log.Printf("[store] GetTasks 扫描失败: %v", err)
			continue
		}
		t.Content = content.String
		t.Path = path.String
		t.CreatedAt = createdAt
		out = append(out, &t)
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
		log.Printf("[store] TasksByParent 查询失败 %s: %v", parentID, err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &t.Status, &t.ParentID); err != nil {
			log.Printf("[store] TasksByParent 扫描失败: %v", err)
			continue
		}
		out = append(out, &t)
	}
	return out
}

// SubmitResult 写入 task_results，按成功/失败处理任务终态（F2 重试/死信）并回写设备看板（B2）。
// 状态守卫（幂等）：仅接受任务处于 running 时的上报，防止迟到/重复上报破坏终态。

func (s *SQLStore) SubmitResult(res *proto.TaskResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO task_results (task_id, agent_id, exit_code, stdout, stderr, finished_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
	exit_code=VALUES(exit_code), stdout=VALUES(stdout), stderr=VALUES(stderr), finished_at=VALUES(finished_at)
`, res.TaskID, res.AgentID, res.ExitCode, res.Stdout, res.Stderr, res.FinishedAt.UTC()); err != nil {
		log.Printf("[store] SubmitResult 写入 task_results 失败 %s: %v", res.TaskID, err)
	}

	success := res.ExitCode == 0
	// 读取任务当前状态/重试计数/上限，决定终态。
	// 状态守卫（幂等，task 82）：仅接受任务处于 running 时的上报——防止迟到上报把已取消任务翻回，
	// 防止重复失败上报反复累计 retry_count 造成假死信。UPDATE 附带 AND status='running' 防并发窗口；
	// RowsAffected==0 表示状态被并发改写 → 跳过后续事件/告警。
	// A-1 防双跑：UPDATE 附带 AND claim_epoch=? 校验持有者令牌，拒绝旧持有者（任务被回收重派后）上报。
	// res.ClaimEpoch > 0 时校验（新 agent 填充）；= 0 时跳过（兼容旧 agent/测试）。
	var tid, tenantID, status string
	var rc, mr int
	accepted := false
	if err := s.db.QueryRowContext(ctx,
		`SELECT task_id, tenant_id, status, retry_count, max_retries FROM tasks WHERE task_id=?`, res.TaskID,
	).Scan(&tid, &tenantID, &status, &rc, &mr); err == nil && tid != "" {
		if status != "running" {
			log.Printf("[store] SubmitResult 忽略非 running 任务 %s (status=%s exitCode=%d)", res.TaskID, status, res.ExitCode)
		} else if success {
			if r, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='done' WHERE task_id=? AND status='running'`+claimEpochCond(res.ClaimEpoch),
				claimEpochArgs(res.TaskID, res.ClaimEpoch)...); uerr != nil {
				log.Printf("[store] SubmitResult done 更新失败 %s: %v", res.TaskID, uerr)
			} else if n, _ := r.RowsAffected(); n > 0 {
				accepted = true
			} else if res.ClaimEpoch > 0 {
				log.Printf("[store] SubmitResult 拒绝旧持有者上报 %s (claim_epoch=%d 不匹配)", res.TaskID, res.ClaimEpoch)
			}
		} else if rc < mr {
			r, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL, retry_count=retry_count+1 WHERE task_id=? AND status='running'`+claimEpochCond(res.ClaimEpoch),
				claimEpochArgs(res.TaskID, res.ClaimEpoch)...)
			if uerr != nil {
				log.Printf("[store] SubmitResult retry 更新失败 %s: %v", res.TaskID, uerr)
			} else if n, _ := r.RowsAffected(); n > 0 {
				accepted = true
				s.publish(events.Event{Action: "task_retry", Target: res.TaskID, TenantID: tenantID,
					Detail: fmt.Sprintf("retry %d/%d", rc+1, mr), Level: events.LevelWarn})
			} else if res.ClaimEpoch > 0 {
				log.Printf("[store] SubmitResult 拒绝旧持有者重试 %s (claim_epoch=%d 不匹配)", res.TaskID, res.ClaimEpoch)
			}
		} else {
			r, uerr := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='failed', dead_letter=1 WHERE task_id=? AND status='running'`+claimEpochCond(res.ClaimEpoch),
				claimEpochArgs(res.TaskID, res.ClaimEpoch)...)
			if uerr != nil {
				log.Printf("[store] SubmitResult dead-letter 更新失败 %s: %v", res.TaskID, uerr)
			} else if n, _ := r.RowsAffected(); n > 0 {
				accepted = true
				s.addAlert(ctx, &proto.Alert{
					AlertID:   "alert-" + res.TaskID,
					TenantID:  tenantID,
					DeviceID:  "dev-" + res.AgentID,
					AgentID:   res.AgentID,
					Severity:  "critical",
					Message:   fmt.Sprintf("task %s dead-letter after %d retries (exitCode=%d)", res.TaskID, rc, res.ExitCode),
					CreatedAt: time.Now().UTC(),
				})
			} else if res.ClaimEpoch > 0 {
				log.Printf("[store] SubmitResult 拒绝旧持有者死信 %s (claim_epoch=%d 不匹配)", res.TaskID, res.ClaimEpoch)
			}
		}
	}

	// 任务存在但上报被忽略（非 running）：结果已记录，不再回写看板/事件/依赖释放。
	if tid != "" && !accepted {
		return
	}

	// 回写设备 TaskState + LastResult（B2 失败回写看板）。
	lastResult := "success"
	taskState := "done"
	if !success {
		lastResult = "failed"
		taskState = "idle"
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE devices SET task_state=?, last_result=?, last_result_at=? WHERE agent_id=?`,
		taskState, lastResult, time.Now().UTC(), res.AgentID); err != nil {
		log.Printf("[store] SubmitResult 更新 devices 失败 %s: %v", res.AgentID, err)
	}

	lvl := events.LevelInfo
	if !success {
		lvl = events.LevelWarn
	}
	s.publish(events.Event{Action: "report_result", Target: res.TaskID, TenantID: "", Detail: fmt.Sprintf("exitCode=%d", res.ExitCode), Level: lvl})

	// M5 作业编排：本任务 done 后释放依赖它的 blocked 任务（同 agent 任务图内）。
	if success {
		s.releaseDeps(ctx, res.AgentID, res.TaskID)
	}
}

// releaseDeps M5 作业编排：任务 done 后，释放同 agent 下依赖它的 blocked 任务。
// 仅当某 blocked 任务的全部 DependsOn 均已 done 时，将其置为 pending（进入可下发队列）。

func (s *SQLStore) releaseDeps(ctx context.Context, agentID, doneTaskID string) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, depends_on FROM tasks WHERE agent_id=? AND status='blocked'`, agentID)
	if err != nil {
		return
	}
	type rec struct {
		id   string
		deps string
	}
	var blocked []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.deps); err != nil {
			continue
		}
		blocked = append(blocked, r)
	}
	rows.Close()
	if len(blocked) == 0 {
		return
	}
	// 取该 agent 全部任务状态，构建 byID 索引供 dag 判定。
	all, err := s.db.QueryContext(ctx,
		`SELECT task_id, status FROM tasks WHERE agent_id=?`, agentID)
	if err != nil {
		return
	}
	byID := make(map[string]*proto.Task)
	for all.Next() {
		var id, st string
		if err := all.Scan(&id, &st); err != nil {
			continue
		}
		byID[id] = &proto.Task{TaskID: id, Status: st}
	}
	all.Close()

	for _, b := range blocked {
		var deps []string
		if b.deps != "" {
			if err := json.Unmarshal([]byte(b.deps), &deps); err != nil {
				log.Printf("[store] 解析阻塞任务 %s 依赖 JSON 失败: %v", b.id, err)
			}
		}
		t := &proto.Task{TaskID: b.id, DependsOn: deps, Status: "blocked"}
		if dag.AllDepsDone(t, byID) {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL WHERE task_id=?`, b.id); err == nil {
				s.publish(events.Event{Action: "task_released", Target: b.id, TenantID: "",
					Detail: "deps done (trigger=" + doneTaskID + ")", Level: events.LevelInfo})
			}
		}
	}
}

// addAlert 写入一条告警（M7）。

func (s *SQLStore) AllTasks(tenantID string) []*proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at FROM tasks`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] AllTasks 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		var t proto.Task
		var content, path sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &content, &path, &t.Status, &createdAt); err != nil {
			log.Printf("[store] AllTasks 扫描失败: %v", err)
			continue
		}
		t.Content = content.String
		t.Path = path.String
		t.CreatedAt = createdAt
		out = append(out, &t)
	}
	return out
}

// TaskByID 按 taskID 返回单条任务（不存在返回 nil）。供按 ID 直查场景（如租户归属校验）。

func (s *SQLStore) TaskByID(taskID string) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, status, created_at FROM tasks WHERE task_id=?`, taskID)
	var t proto.Task
	var content, path sql.NullString
	var createdAt time.Time
	if err := row.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &content, &path, &t.Status, &createdAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] TaskByID 查询失败 %s: %v", taskID, err)
		}
		return nil
	}
	t.Content = content.String
	t.Path = path.String
	t.CreatedAt = createdAt
	return &t
}

// Device 按 deviceID 返回单台设备（供设备详情端点）。

func (s *SQLStore) Results(agentID string) []*proto.TaskResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, agent_id, exit_code, stdout, stderr, finished_at FROM task_results WHERE agent_id=? ORDER BY finished_at DESC`, agentID)
	if err != nil {
		log.Printf("[store] Results 查询失败 %s: %v", agentID, err)
		return nil
	}
	defer rows.Close()
	var out []*proto.TaskResult
	for rows.Next() {
		var r proto.TaskResult
		var finishedAt time.Time
		if err := rows.Scan(&r.TaskID, &r.AgentID, &r.ExitCode, &r.Stdout, &r.Stderr, &finishedAt); err != nil {
			log.Printf("[store] Results 扫描失败: %v", err)
			continue
		}
		r.FinishedAt = finishedAt
		out = append(out, &r)
	}
	return out
}

// Agents 返回已注册 agent（tenantID 非空时按租户过滤）。

func (s *SQLStore) CreateTask(t *proto.Task) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if t.AgentID == "" {
		return t // 调用方需保证 agentID 非空
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
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, retry_count, max_retries, dead_letter, schedule, parent_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE type=VALUES(type), command=VALUES(command), content=VALUES(content),
		   path=VALUES(path), status=VALUES(status), schedule=VALUES(schedule), parent_id=VALUES(parent_id), max_retries=VALUES(max_retries)`,
		t.TaskID, t.AgentID, t.TenantID, t.Type, t.Command, t.Content, t.Path, t.Status,
		t.RetryCount, t.MaxRetries, t.DeadLetter, t.Schedule, t.ParentID, t.CreatedAt); err != nil {
		log.Printf("[store] CreateTask 失败 %s: %v", t.TaskID, err)
	}
	s.publish(events.Event{Action: "create_task", Target: t.TaskID, TenantID: t.TenantID, Detail: t.Command, Level: events.LevelInfo})
	return t
}

// ClaimTask 原子领取该 agent 的下一条 pending 任务（FOR UPDATE 行锁保证多副本不双领，P1-1）。
// A-1 防双跑：领取时 claim_epoch=claim_epoch+1，返回的 Task 带 ClaimEpoch；
// agent 上报结果时携带 ClaimEpoch，SubmitResult 校验持有者是否仍为当前 epoch。

func (s *SQLStore) ClaimTask(agentID string) *proto.Task {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[store] ClaimTask begin 失败 %s: %v", agentID, err)
		return nil
	}
	defer tx.Rollback()

	var taskID, typ, command string
	var tenantID, content, path sql.NullString
	var createdAt time.Time
	var claimEpoch int64
	if err := tx.QueryRowContext(ctx,
		`SELECT task_id, tenant_id, type, command, content, path, created_at, claim_epoch FROM tasks
		 WHERE agent_id=? AND (status IS NULL OR status='pending') AND (schedule IS NULL OR schedule='') ORDER BY created_at LIMIT 1 FOR UPDATE`,
		agentID).Scan(&taskID, &tenantID, &typ, &command, &content, &path, &createdAt, &claimEpoch); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		log.Printf("[store] ClaimTask 查询失败 %s: %v", agentID, err)
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status='running', claimed_by='controlplane', claimed_at=?, claim_epoch=claim_epoch+1 WHERE task_id=?`,
		time.Now().UTC(), taskID); err != nil {
		log.Printf("[store] ClaimTask 更新失败 %s: %v", taskID, err)
		return nil
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[store] ClaimTask commit 失败 %s: %v", taskID, err)
		return nil
	}
	return &proto.Task{
		TaskID: taskID, AgentID: agentID, TenantID: tenantID.String, Type: typ, Command: command,
		Content: content.String, Path: path.String,
		Status: "running", CreatedAt: createdAt, ClaimedBy: "controlplane", ClaimedAt: time.Now().UTC(),
		ClaimEpoch: claimEpoch + 1,
	}
}

// ReclaimStaleTasks 复位超期未完成的 running 任务为 pending（P0-1 任务必达）。
// agent 经 ClaimTask 领取（claimed_at 写当前时间）后若失联、超过 maxAge 仍未上报结果，
// 该任务将永远卡在 running；此处周期性调用把它复位，重新进入调度队列。
//
// A-1 防双跑：增加 agent 心跳校验——仅当任务的 claimed_by 对应 agent 心跳也超时（last_seen < cutoff）
// 才回收。心跳正常的 agent 仍可能在执行长任务，不回收避免双跑。
// A-2 leader fencing：仅当前 leader_lease 持有者可执行回收，防 HA 双主窗口下重复回收。

func (s *SQLStore) ReclaimStaleTasks(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A-2 leader fencing：非有效租约持有者跳过，防双主同时回收。
	if !s.checkLeaderFence(ctx, "ReclaimStaleTasks") {
		return 0
	}
	cutoff := time.Now().UTC().Add(-maxAge)
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status='pending', claimed_by=NULL, claimed_at=NULL
		 WHERE status='running' AND claimed_at < ?
		   AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.agent_id = tasks.claimed_by AND a.last_seen > ?)`,
		cutoff, cutoff)
	if err != nil {
		log.Printf("[store] ReclaimStaleTasks 失败: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// FireDueSchedules 评估所有模板任务（parent_id 为空且 schedule 非空），
// 对到点（cron 匹配 now 且 last_fired_at 早于本分钟）的模板派生一个 pending 实例并回写 last_fired_at。
// 返回本批次派生的实例数（F4 定时/周期调度；控制面 scheduleLoop 周期调用）。
// 注意：SQL 路径依赖 live MySQL，仅在 CI 集成测试（env 门控）中真正运行。
//
// B-2 派生原子化：SELECT→INSERT→UPDATE 包在同一事务内，失败 Rollback，
// 避免派生实例已写入但 last_fired_at 未回写导致下一轮重复派生。
// A-2 leader fencing：仅当前 leader_lease 持有者可派生，防 HA 双主窗口下重复派生。

func (s *SQLStore) FireDueSchedules(now time.Time) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A-2 leader fencing：非有效租约持有者跳过，防双主同时派生。
	if !s.checkLeaderFence(ctx, "FireDueSchedules") {
		return 0
	}
	minuteStart := now.Truncate(time.Minute)
	// B-2 派生原子化：整批 SELECT→INSERT→UPDATE 包在单事务内，失败 Rollback。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[store] FireDueSchedules begin 失败: %v", err)
		return 0
	}
	defer tx.Rollback()
	// FOR UPDATE 锁模板行：并发事务串行化，先到者更新 last_fired_at 后，
	// 后到者重新读到 last_fired_at >= minuteStart 跳过，避免重复派生。
	rows, err := tx.QueryContext(ctx,
		`SELECT task_id, agent_id, tenant_id, type, command, content, path, max_retries, schedule, last_fired_at
		 FROM tasks WHERE (parent_id IS NULL OR parent_id='') AND schedule <> '' AND (status IS NULL OR status='pending')
		 FOR UPDATE`)
	if err != nil {
		log.Printf("[store] FireDueSchedules 查询失败: %v", err)
		return 0
	}
	type tpl struct {
		id, agentID, tenantID, typ, command, content, path, schedule string
		maxRetries                                                   int
		lastFiredAt                                                  time.Time
	}
	due := make([]tpl, 0)
	for rows.Next() {
		var tp tpl
		var lf sql.NullTime
		if err := rows.Scan(&tp.id, &tp.agentID, &tp.tenantID, &tp.typ, &tp.command, &tp.content, &tp.path, &tp.maxRetries, &tp.schedule, &lf); err != nil {
			log.Printf("[store] FireDueSchedules 扫描失败: %v", err)
			continue
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
	rows.Close()
	fired := 0
	for _, tp := range due {
		instID := fmt.Sprintf("task-%d-%s", now.UnixNano(), tp.id)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, retry_count, max_retries, schedule, parent_id, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, '', ?, ?)`,
			instID, tp.agentID, tp.tenantID, tp.typ, tp.command, tp.content, tp.path, tp.maxRetries, tp.id, now.UTC()); err != nil {
			log.Printf("[store] FireDueSchedules 派生实例失败 %s: %v", instID, err)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET last_fired_at=? WHERE task_id=?`, now.UTC(), tp.id); err != nil {
			log.Printf("[store] FireDueSchedules 回写 last_fired_at 失败 %s: %v", tp.id, err)
			continue
		}
		fired++
		s.publish(events.Event{Action: "schedule_fire", Target: instID, TenantID: tp.tenantID,
			Detail: "parent=" + tp.id + " cron=" + tp.schedule, Level: events.LevelInfo})
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[store] FireDueSchedules commit 失败: %v", err)
		return 0
	}
	return fired
}

// UpsertDevice 写入/更新一台纳管设备（真实网段发现 P0-2 用；按 device_id 幂等）。

func (s *SQLStore) PendingDepth() int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status IS NULL OR status='pending'`).Scan(&n); err != nil {
		log.Printf("[store] PendingDepth 失败: %v", err)
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
	var finishedAt time.Time
	if err := row.Scan(&r.TaskID, &r.AgentID, &r.ExitCode, &r.Stdout, &r.Stderr, &finishedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] TaskResult 查询失败 %s: %v", taskID, err)
		}
		return nil
	}
	r.FinishedAt = finishedAt
	return &r
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
		log.Printf("[store] CancelTask 失败 %s: %v", id, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// RetireDevice 退役/下线设备（F5）：标记 retired；tenantID 非空时校验租户。

func (s *SQLStore) CancelledTaskIDs(agentID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id FROM tasks WHERE agent_id=? AND status='cancelled'`, agentID)
	if err != nil {
		log.Printf("[store] CancelledTaskIDs 失败 %s: %v", agentID, err)
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
	return out
}

// CleanupTokens 清理过期/已消费的 install token（F9 无界增长防护）。
