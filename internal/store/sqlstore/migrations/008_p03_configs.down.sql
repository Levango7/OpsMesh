-- 008_p03_configs.down.sql — 008 的回滚脚本
--
-- 反向操作：删除配置中心两张表：config_history（历史版本）与 configs（当前版本）。
-- 先删历史表再删当前表（无外键约束，顺序仅为语义清晰）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：配置项及其历史版本全部丢失。执行前务必备份。
DROP TABLE IF EXISTS config_history;
DROP TABLE IF EXISTS configs;
