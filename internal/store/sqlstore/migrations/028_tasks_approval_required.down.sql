-- 028_tasks_approval_required.down.sql — 回滚 approval_required 列（谨慎：列数据丢失）。
--
-- 反向操作：删除 tasks.approval_required 列。回滚后 SQLStore.CreateTask 又不会写该列，
-- 高风险任务重新落成 pending（审批工作流再次失效）——因此回滚必须与应用了新版本
-- 控制面二进制的动作配对，单独回滚数据库会把审批语义退回本迁移前的缺陷形态。
ALTER TABLE tasks DROP COLUMN approval_required;
