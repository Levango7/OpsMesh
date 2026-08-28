-- config-svc schema.sql — MySQL schema for config service
-- Tables: config_entries, config_history, config_secrets, notify_channels, config_templates

CREATE TABLE IF NOT EXISTS config_entries (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    value TEXT,
    format VARCHAR(32),
    version INT DEFAULT 1,
    description TEXT,
    updated_by VARCHAR(64),
    created_at DATETIME,
    updated_at DATETIME,
    UNIQUE KEY uk_tenant_key (tenant_id, key_name),
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS config_history (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    value TEXT,
    format VARCHAR(32),
    version INT,
    description TEXT,
    updated_by VARCHAR(64),
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant_key (tenant_id, key_name)
);

CREATE TABLE IF NOT EXISTS config_secrets (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    value TEXT,
    key_type VARCHAR(32),
    version INT DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    UNIQUE KEY uk_tenant_key (tenant_id, key_name),
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS notify_channels (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32),
    config JSON,
    enabled TINYINT(1) DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS config_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    content MEDIUMTEXT,
    variables JSON,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
);
