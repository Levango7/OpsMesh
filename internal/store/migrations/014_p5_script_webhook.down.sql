-- 014_p5_script_webhook.down.sql — 014 的回滚脚本
--
-- 反向操作：删除脚本库/脚本执行记录、Webhook 配置/投递记录四张表。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：自定义脚本与其执行历史、Webhook 配置与投递记录全部丢失。执行前务必备份。
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS script_executions;
DROP TABLE IF EXISTS scripts;
