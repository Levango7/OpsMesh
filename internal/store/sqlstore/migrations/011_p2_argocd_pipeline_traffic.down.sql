-- 011_p2_argocd_pipeline_traffic.down.sql — 011 的回滚脚本
--
-- 反向操作：删除 ArgoCD 应用、流水线模板/运行记录、流量策略四张表。
-- pipeline_runs 先于 pipeline_templates（运行记录引用模板）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：流水线模板与运行历史、ArgoCD 集成配置、流量策略全部丢失。
--   注意：016 迁移为 pipeline_templates 补了 agent_id 列，倒序回滚时 016 先执行，顺序自洽。
DROP TABLE IF EXISTS traffic_policies;
DROP TABLE IF EXISTS pipeline_runs;
DROP TABLE IF EXISTS pipeline_templates;
DROP TABLE IF EXISTS argocd_apps;
