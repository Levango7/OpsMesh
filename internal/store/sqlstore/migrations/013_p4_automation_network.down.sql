-- 013_p4_automation_network.down.sql — 013 的回滚脚本
--
-- 反向操作：删除自动化规则/执行记录、网络设备/网络指标四张表。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：自动化规则与执行历史、网络设备纳管信息与指标全部丢失。
DROP TABLE IF EXISTS network_metrics;
DROP TABLE IF EXISTS network_devices;
DROP TABLE IF EXISTS automation_executions;
DROP TABLE IF EXISTS automation_rules;
