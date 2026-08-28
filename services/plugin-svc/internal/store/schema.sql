-- plugin-svc schema.sql — MySQL schema for plugin service
-- Tables: plugins, plugin_versions

CREATE TABLE IF NOT EXISTS plugins (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    version VARCHAR(64),
    description TEXT,
    author VARCHAR(255),
    type VARCHAR(32),
    category VARCHAR(64),
    tags JSON,
    download_url VARCHAR(512),
    checksum VARCHAR(128),
    status VARCHAR(32) DEFAULT 'pending',
    installed TINYINT(1) DEFAULT 0,
    enabled TINYINT(1) DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_status (status),
    INDEX idx_type (type)
);

CREATE TABLE IF NOT EXISTS plugin_versions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    plugin_id VARCHAR(64) NOT NULL,
    version VARCHAR(64) NOT NULL,
    checksum VARCHAR(128),
    download_url VARCHAR(512),
    released_at DATETIME,
    changelog TEXT,
    INDEX idx_plugin (plugin_id)
);
