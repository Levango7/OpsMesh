-- 008_p03_configs.sql — P0.3 配置中心领域持久化表
--
-- configs 表存储当前版本配置（按 tenant_id + key_name 唯一）。
-- config_history 表存储历史版本（每次 SetConfig 把前版本写入历史）。
-- SetConfig 用事务：INSERT 旧版本到 config_history + UPSERT configs。

CREATE TABLE IF NOT EXISTS configs (
    tenant_id   VARCHAR(64)  NOT NULL,
    key_name    VARCHAR(255) NOT NULL,
    value       TEXT,
    format      VARCHAR(16)  NOT NULL DEFAULT 'text',
    version     INT          NOT NULL DEFAULT 1,
    description TEXT,
    updated_by  VARCHAR(64),
    updated_at  DATETIME     NOT NULL,
    published_at DATETIME,
    PRIMARY KEY (tenant_id, key_name),
    INDEX idx_configs_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS config_history (
    tenant_id   VARCHAR(64)  NOT NULL,
    key_name    VARCHAR(255) NOT NULL,
    version     INT          NOT NULL,
    value       TEXT,
    format      VARCHAR(16),
    description TEXT,
    updated_by  VARCHAR(64),
    updated_at  DATETIME     NOT NULL,
    PRIMARY KEY (tenant_id, key_name, version),
    INDEX idx_config_history_tenant (tenant_id)
);