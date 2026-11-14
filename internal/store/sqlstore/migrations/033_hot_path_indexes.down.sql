-- 033_hot_path_indexes.down.sql — 回滚热路径复合索引

DROP INDEX idx_tasks_tenant_status_created ON tasks;
DROP INDEX idx_tasks_agent_status ON tasks;
DROP INDEX idx_tasks_status_claimed ON tasks;
DROP INDEX idx_devices_tenant_retired_segment ON devices;
DROP INDEX idx_audit_log_tenant_created ON audit_log;