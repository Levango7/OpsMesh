-- 003_legacy_tables.sql — 历史上由 Go 代码 initSchemaExtra 创建的四张表 DDL 迁移
--
-- 本文件将原 internal/store/sql_legacy.go initSchemaExtra 中的四张表
-- CREATE TABLE IF NOT EXISTS 语句（alert_rules / os_templates /
-- middleware_templates / refresh_tokens）正式纳入版本化迁移框架。
--
-- 背景（task G5 / B-7 建表逻辑双写治理）：
--   历史上这四张表的 DDL 同时存在于两处：
--     1. migrations/001_initial.sql（初始 schema 快照已收录）
--     2. internal/store/sql_legacy.go initSchemaExtra（运行期 Go 代码建表）
--   两处重复定义违反单一真相原则，且 Go 代码路径绕过 schema_migrations 版本记录，
--   无法被迁移框架的 checksum 校验覆盖。本迁移以独立版本号 003 显式标记这批
--   "legacy Go 代码建表" 的退役，使后续审计可追溯。
--
-- 幂等性：
--   - 所有语句保持 IF NOT EXISTS 语义，对已存在库（无论由 001_initial.sql
--     还是由历史 Go 代码创建）均幂等。
--   - 增量补列（alterColumnIfMissing）与补索引（createIndexIfMissing）
--     仍保留在 Go 代码 initSchemaExtra 中，作为对老库的向后兼容补丁，
--     不在本迁移中重复。
--
-- 后续：
--   - 本迁移应用后，sql_legacy.go initSchemaExtra 中的 CREATE TABLE 语句
--     已删除，仅保留 ALTER TABLE 补列与 CREATE INDEX 兼容逻辑。
--   - 对应回滚占位见 003_legacy_tables.down.sql。

-- alert_rules：task 100 告警规则表
CREATE TABLE IF NOT EXISTS alert_rules (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    metric VARCHAR(128),
    op VARCHAR(8),
    threshold DOUBLE,
    for_duration INT DEFAULT 0,
    severity VARCHAR(16),
    message TEXT,
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME
);

-- os_templates：task 100 OS 安装模板表
CREATE TABLE IF NOT EXISTS os_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    name VARCHAR(255) NOT NULL,
    os VARCHAR(64),
    version VARCHAR(64),
    arch VARCHAR(32),
    install_url VARCHAR(512),
    config TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

-- middleware_templates：task 100 中间件部署模板表
CREATE TABLE IF NOT EXISTS middleware_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    name VARCHAR(255) NOT NULL,
    type VARCHAR(64),
    version VARCHAR(64),
    config TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

-- refresh_tokens：task 111 刷新令牌表
-- （tokenHash / user / tenant / deviceFP / expires / created）
CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64),
    tenant_id VARCHAR(64),
    device_fp VARCHAR(255),
    expires_at DATETIME,
    created_at DATETIME
);