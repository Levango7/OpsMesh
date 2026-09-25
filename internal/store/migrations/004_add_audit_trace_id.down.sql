-- 004_add_audit_trace_id.down.sql — 004 的回滚脚本
--
-- 反向操作：删除 audit_log.trace_id 列及其索引 idx_audit_trace。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：审计记录与链路的关联信息丢失（审计记录本身保留）。
DROP INDEX idx_audit_trace ON audit_log;
ALTER TABLE audit_log DROP COLUMN trace_id;
