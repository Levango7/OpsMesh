-- 023_task_records.down.sql — 回滚 023（删记录表）。
-- 注意：删表即丢批量/灰度记录（任务实例在 tasks 表，不受影响）；仅在确认不再需要
-- "按 batchID/canaryID 回溯记录"时执行。
DROP TABLE IF EXISTS task_records;
