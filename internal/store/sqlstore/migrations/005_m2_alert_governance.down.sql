-- 005_m2_alert_governance.down.sql — 005 的回滚脚本
--
-- 反向操作：删除本迁移新增的三张表（静默规则/通知渠道/通知模板），并移除
--   alert_rules.created_by 列（该列由本迁移新增；alert_rules 表本身归 001 所有，不删表）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：静默规则/通知渠道/通知模板配置全部丢失；alert_rules.created_by 审计信息丢失。
--   注意 alert_rules.created_by 同时会被 applyLegacyColumnFixups 兜底补列——
--   回滚后若继续运行当前版本二进制，下次启动该列会被再次补上（如需彻底移除须一并回退二进制）。
DROP TABLE IF EXISTS notify_templates;
DROP TABLE IF EXISTS notify_channels;
DROP TABLE IF EXISTS alert_silences;
ALTER TABLE alert_rules DROP COLUMN created_by;
