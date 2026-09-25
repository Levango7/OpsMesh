-- 009_p03_services.down.sql — 009 的回滚脚本
--
-- 反向操作：删除服务发现表 services。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：服务注册信息丢失（服务本身不受影响，回滚后重新注册即恢复）。
DROP TABLE IF EXISTS services;
