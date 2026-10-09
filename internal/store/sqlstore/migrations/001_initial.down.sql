-- 001_initial.down.sql — 001_initial.sql 的回滚脚本
--
-- 回滚动作：删除 001 创建的 20 张核心表（等价于卸载 OpsMesh 的持久层）。
--
-- 警告（破坏性）：本脚本 DROP 掉全部业务数据表——设备/任务/结果/审计/用户/角色/
--   K8s 集群/告警规则/模板/刷新令牌等**全部丢失且不可恢复**。
--   仅在以下场景使用：
--     - 开发/测试环境彻底重置；
--     - 已确认业务数据另有备份、且需要把库交还其它系统使用。
--   生产环境的「升级回滚」应针对具体迁移版本回滚（002/004/.../018 的 .down.sql），
--   或从备份恢复，而不是执行本脚本。
--
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 说明：无外键约束，DROP 顺序仅为可读性（先从属数据表、后主体表）。
--   schema_migrations 表由 runMigrations 硬编码维护，不在此处删除——
--   删掉它等于让下次启动把整条迁移链在空库上重跑一遍。
DROP TABLE IF EXISTS task_results;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS install_tokens;
DROP TABLE IF EXISTS leader_lease;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS alert_rules;
DROP TABLE IF EXISTS ci_relations;
DROP TABLE IF EXISTS ci_items;
DROP TABLE IF EXISTS ci_attr_templates;
DROP TABLE IF EXISTS ci_types;
DROP TABLE IF EXISTS k8s_clusters;
DROP TABLE IF EXISTS os_templates;
DROP TABLE IF EXISTS middleware_templates;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
