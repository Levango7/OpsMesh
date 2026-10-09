-- 012_p3_backup_compliance.down.sql — 012 的回滚脚本
--
-- 反向操作：删除备份记录表与合规报告表。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：备份索引与合规报告历史丢失（**已生成的备份文件本身不受影响**，
--   但失去索引后无法从控制面检索/恢复，建议先导出这两张表）。
DROP TABLE IF EXISTS compliance_reports;
DROP TABLE IF EXISTS backup_records;
