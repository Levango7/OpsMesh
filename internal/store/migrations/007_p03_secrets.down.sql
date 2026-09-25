-- 007_p03_secrets.down.sql — 007 的回滚脚本
--
-- 反向操作：删除密钥管理表 secrets（含全部历史版本）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险（严重）：密钥明文/密文全量丢失，且**不可恢复**——依赖这些密钥的部署、
--   集成与脚本将在回滚后立即失效。执行前务必备份 secrets 表。
DROP TABLE IF EXISTS secrets;
