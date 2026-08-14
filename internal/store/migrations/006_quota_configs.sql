-- 006_quota_configs.sql — P2-B5 多租户资源配额：租户级资源配额配置持久化
--
-- 任务 274：将 controlplane.QuotaManager 中的内存 map 实现升级为 SQL 持久化，
-- 使 --store=mysql 时重启不丢失，多副本 HA 下配额配置一致。
--
-- 表结构说明：
--   - tenant_id：租户标识（主键，与 devices/tasks/alerts 的 tenant_id 同语义）。
--   - max_devices / max_tasks / max_alerts：对应 QuotaConfig 字段，0=不限。
--   - updated_at：最近一次配额变更时间，供运维审计。
--
-- 兼容性：
--   - CREATE TABLE IF NOT EXISTS 对已存在库幂等。
--   - 与 005_m2_alert_governance.sql 风格一致（tenant_id 索引加速按租户过滤）。

CREATE TABLE IF NOT EXISTS quota_configs (
    tenant_id  VARCHAR(64) PRIMARY KEY,
    max_devices INT NOT NULL DEFAULT 0,
    max_tasks   INT NOT NULL DEFAULT 0,
    max_alerts  INT NOT NULL DEFAULT 0,
    updated_at  DATETIME
);