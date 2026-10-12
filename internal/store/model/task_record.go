// task_record.go — 批量执行 / 灰度发布记录的持久化信封（TD-60 task 域 R6）。
//
// 背景：这两份状态此前**只在进程内存**（controlplane server_batch.go 的 batchStore），
// 重启即丢批次聚合视图——任务实例本身在 tasks 表里持久化，但用户手里的 batchID/canaryID
// 重启后查不到（404）。切流后两侧各有内存索引，同一批次"创建"与"查询"落到不同侧/不同副本
// 也查不到（演练报告 R6）。
//
// **为什么是「信封 + JSON 载荷」而不是逐字段建列**（设计取舍，值得写清楚）：
//   - 这两份记录**只按 ID 整条读写**——没有任何按状态/时间筛选的消费方，SQL 列带来的可查询性
//     在这里没有服务对象；
//   - 逐字段建列等于把 server_batch.go 的 20 多个内部字段抄一遍，且两边必须同步演进
//     （加一个字段要改模型+迁移+读写转换三处）——这是必然漂移的结构；
//   - 整段 JSON 天然无损：内部结构增删字段不需要迁移，也不会漏字段。
//
// 代价与边界（如实写在这里，避免下一个人误解）：
//   - Payload 是**控制面内部结构**的序列化，**不是对外契约**，不承诺跨版本稳定；
//     读取侧解不出来时按"记录不可用"处理（返回 nil，不 panic、不返回半条）；
//   - 想按批次状态做统计/筛选时，需要重新设计（届时按需把这些字段提升为列）。
package model

import "time"

// TaskRecord 批量/灰度记录的持久化信封。
type TaskRecord struct {
	Kind      string    `json:"kind"` // "batch" | "canary"
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantID"`
	CreatedAt time.Time `json:"createdAt"`
	Payload   []byte    `json:"payload"` // JSON（控制面内部结构的序列化）
}

// CloneTaskRecord 深拷贝（读路径对外一律给副本，防外部改内部状态）。
func CloneTaskRecord(r *TaskRecord) *TaskRecord {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Payload != nil {
		cp.Payload = append([]byte(nil), r.Payload...)
	}
	return &cp
}
