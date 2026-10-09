-- 002_add_claim_epoch.down.sql — 002 的回滚脚本
--
-- 反向操作：删除 tasks.claim_epoch 列（任务所有权令牌，ClaimTask/SubmitResult 防双跑校验用）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：列数据丢失。回滚后若不回退二进制，下次启动的正向迁移会重新补列（值重置为 0）。
ALTER TABLE tasks DROP COLUMN claim_epoch;
