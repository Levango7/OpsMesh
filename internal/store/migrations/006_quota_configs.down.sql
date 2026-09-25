-- 006_quota_configs.down.sql — 006 的回滚脚本
--
-- 反向操作：删除租户资源配额表 quota_configs。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：各租户配额配置丢失，回滚后按默认（不限）执行。
DROP TABLE IF EXISTS quota_configs;
