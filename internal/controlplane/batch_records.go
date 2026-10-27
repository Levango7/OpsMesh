// batch_records.go 批量/灰度记录的持久化接线（TD-60 task 域 R6）。
//
// 分工：按 ID 查询时内存索引未命中 ⇒ **回落 store**（这是"重启后 batchID 仍可查"的路径）。
package controlplane

import (
	"context"
	"encoding/json"

	"github.com/Levango7/OpsMesh/internal/logx"
)

// 记录类型（与 task_records.kind 取值一致）。
const (
	taskRecordKindBatch  = "batch"
	taskRecordKindCanary = "canary"
)

// cloneBatchTask / cloneCanaryRelease 深拷贝（索引里的对象只读共享，出索引必须给副本：
// 否则调用方在锁外读到的切片可能正被并发写者改写）。
func cloneBatchTask(b *batchTask) *batchTask {
	if b == nil {
		return nil
	}
	cp := *b
	cp.Tasks = append([]batchTaskItem(nil), b.Tasks...)
	return &cp
}

func cloneCanaryRelease(c *canaryRelease) *canaryRelease {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Groups = append([]string(nil), c.Groups...)
	if c.Labels != nil {
		cp.Labels = make(map[string]string, len(c.Labels))
		for k, v := range c.Labels {
			cp.Labels[k] = v
		}
	}
	cp.Phases = make([]canaryPhase, len(c.Phases))
	for i, p := range c.Phases {
		cp.Phases[i] = p
		cp.Phases[i].DeviceIDs = append([]string(nil), p.DeviceIDs...)
		cp.Phases[i].Tasks = append([]batchTaskItem(nil), p.Tasks...)
	}
	return &cp
}

// batchSnapshot 取批次的**私有快照**：内存索引命中则锁内深拷贝，未命中回落持久化记录
// （R6：重启后 batchID 仍可查）。返回 nil 表示查不到（调用方走 404）。
// 刻意不把回落结果写回索引：查询路径不该改变索引状态（写回属并发下的另一套语义，
// 而索引本就是缓存，重启后按需读 store 足够）。
func (s *Server) batchSnapshot(tenantID, id string) *batchTask {
	s.batches.mu.RLock()
	bt := cloneBatchTask(s.batches.batches[id])
	s.batches.mu.RUnlock()
	if bt != nil {
		return bt
	}
	loaded := &batchTask{}
	if !s.loadTaskRecord(taskRecordKindBatch, tenantID, id, loaded) {
		return nil
	}
	return loaded
}

// canarySnapshot 取灰度的**私有快照**（语义同 batchSnapshot）。
func (s *Server) canarySnapshot(tenantID, id string) *canaryRelease {
	s.batches.mu.RLock()
	c := cloneCanaryRelease(s.batches.canaries[id])
	s.batches.mu.RUnlock()
	if c != nil {
		return c
	}
	loaded := &canaryRelease{}
	if !s.loadTaskRecord(taskRecordKindCanary, tenantID, id, loaded) {
		return nil
	}
	return loaded
}

// loadTaskRecord 从 store 读回一条记录并解到 out（内存索引未命中时的回落路径）。
// 返回 false 表示"查不到或载荷不可用"——调用方据此走 404，不把半条记录返回给用户。
func (s *Server) loadTaskRecord(kind, tenantID, id string, out any) bool {
	if s.store == nil {
		return false
	}
	rec := s.store.GetTaskRecord(tenantID, kind, id)
	if rec == nil || len(rec.Payload) == 0 {
		return false
	}
	if err := json.Unmarshal(rec.Payload, out); err != nil {
		logx.Warn(context.Background(), "任务记录载荷不可用（按查不到处理）",
			"kind", kind, "id", id, "err", err)
		return false
	}
	return true
}
