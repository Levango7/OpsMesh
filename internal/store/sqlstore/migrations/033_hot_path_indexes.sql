-- 033_hot_path_indexes.sql — 高并发热路径复合索引（批次26）
--
-- 背景：列表/认领/回收/审计查询是最高频 SQL，此前只有主键索引，
-- 高 TPS 下会退化为全表扫描。本迁移只加索引、不改数据，可在线执行。

-- 任务列表：按租户 + 状态过滤，created_at 排序
CREATE INDEX idx_tasks_tenant_status_created ON tasks (tenant_id, status, created_at);

-- agent 认领/轮询：按 agent + 状态（pending/running）
CREATE INDEX idx_tasks_agent_status ON tasks (agent_id, status);

-- 失联任务回收：按状态 + claimed_at 超时扫描
CREATE INDEX idx_tasks_status_claimed ON tasks (status, claimed_at);

-- 设备列表：租户 + 未退役 + 分段
CREATE INDEX idx_devices_tenant_retired_segment ON devices (tenant_id, retired, segment);

-- 审计查询：租户 + 时间倒序
CREATE INDEX idx_audit_log_tenant_created ON audit_log (tenant_id, created_at);