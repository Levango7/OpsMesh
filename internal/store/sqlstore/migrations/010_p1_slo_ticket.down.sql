-- 010_p1_slo_ticket.down.sql — 010 的回滚脚本
--
-- 反向操作：删除 SLO 表与工单表（tickets 先于 slos，语义上工单引用 SLO）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：SLO 定义与工单记录全部丢失。执行前务必备份。
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS slos;
