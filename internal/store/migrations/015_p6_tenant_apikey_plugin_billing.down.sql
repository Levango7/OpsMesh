-- 015_p6_tenant_apikey_plugin_billing.down.sql — 015 的回滚脚本
--
-- 反向操作：删除租户/API Key/插件/计费套餐/订阅/发票六张表。
-- 依赖方向（无外键约束，顺序为语义倒序）：invoices → subscriptions → billing_plans/plugins/api_keys → tenants。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险（严重）：租户注册表与 API Key 全量丢失——依赖 API Key 的集成将立即失效
--   （与 controlplane 用户中心不同，此处无其它租户来源）。计费与发票历史一并丢失。
--   注意：008/009/012 等迁移创建的表均带 tenant_id 列，但无外键约束，不受影响。
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS billing_plans;
DROP TABLE IF EXISTS plugins;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS tenants;
