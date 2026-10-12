// sql_task_record.go 实现 SQLStore 的 TaskRecordStore（TD-60 task 域 R6）。
//
// 表结构见 migrations/023_task_records.sql。**内容整段进 payload（JSON）**的设计取舍见
// model/task_record.go 头注释：这两份记录只按 ID 整条读写，没有按状态筛选的消费方。
//
// 设计要点（与 sql_gateway_route.go 同风格）：
//   - DB 不可用/无行返回零值，不 panic；sql.ErrNoRows 视为"不存在"；
//   - 幂等 upsert（INSERT ... ON DUPLICATE KEY UPDATE）；失败经 storefail 留痕并返回错误；
//   - 读侧遇坏载荷按"记录不可用"返回 nil（不返回半条、不 panic）。
package sqlstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// taskRecordColumns 读路径列清单与 scanTaskRecord 的 Scan 目标数一一对应（TD-90 列数对账门禁）。
const taskRecordColumns = `kind, id, tenant_id, created_at, payload`

func scanTaskRecord(row rowScanner) *TaskRecord {
	var r TaskRecord
	var payload sql.NullString
	if err := row.Scan(&r.Kind, &r.ID, &r.TenantID, &r.CreatedAt, &payload); err != nil {
		return nil
	}
	if payload.String == "" {
		// 空载荷 = 记录不可用（写侧不该产生这种行）——返回 nil 而不是空 Payload 的壳，
		// 让调用方走"查不到"分支，避免把空壳当有效记录返回给用户。
		return nil
	}
	r.Payload = []byte(payload.String)
	return &r
}

// SaveTaskRecord 幂等 upsert（按 tenant_id + kind + id）。
func (s *SQLStore) SaveTaskRecord(rec *TaskRecord) error {
	if rec == nil || rec.TenantID == "" || rec.Kind == "" || rec.ID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	payload := rec.Payload
	if len(payload) == 0 {
		payload = []byte("null")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO task_records (kind, id, tenant_id, created_at, payload)
		 VALUES (?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE payload=VALUES(payload)`,
		rec.Kind, rec.ID, rec.TenantID, rec.CreatedAt.UTC(), string(payload)); err != nil {
		storefail.Record("[store] SaveTaskRecord 失败 %s/%s/%s: %v", rec.TenantID, rec.Kind, rec.ID, err)
		return err
	}
	return nil
}

// GetTaskRecord 按租户 + kind + ID 取单条（不存在/载荷不可用返回 nil）。
func (s *SQLStore) GetTaskRecord(tenantID, kind, id string) *TaskRecord {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx,
		"SELECT "+taskRecordColumns+" FROM task_records WHERE tenant_id=? AND kind=? AND id=?",
		tenantID, kind, id)
	return scanTaskRecord(row)
}
