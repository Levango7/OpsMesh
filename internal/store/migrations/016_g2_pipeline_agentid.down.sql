-- 016_g2_pipeline_agentid.down.sql — 016 的回滚脚本
--
-- 反向操作：删除 pipeline_templates.agent_id 列（默认执行 agent）。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险：模板上的默认执行 agent 配置丢失，回滚后触发 run 将不再指定 AgentID
--   （任务 AgentID 为空、无人领取），直至重新配置。回滚后请勿再启动当前版本二进制
--   （否则正向迁移会重新补列，且 pipeline 写入该列可能失败于半迁移状态）。
ALTER TABLE pipeline_templates DROP COLUMN agent_id;
