-- alert-svc schema.sql — MySQL schema for alert service
-- Tables: alerts, alert_rules, silences

CREATE TABLE IF NOT EXISTS alerts (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    alert_id VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    device_id VARCHAR(64),
    agent_id VARCHAR(64),
    severity VARCHAR(16),
    message TEXT,
    metric VARCHAR(128),
    status VARCHAR(16) DEFAULT 'firing',
    acknowledged_by VARCHAR(64),
    silenced_until DATETIME,
    comment TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    UNIQUE KEY uk_alert_id (alert_id),
    INDEX idx_tenant (tenant_id),
    INDEX idx_status (status)
);

CREATE TABLE IF NOT EXISTS alert_rules (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    metric VARCHAR(128),
    op VARCHAR(8),
    threshold DOUBLE,
    for_duration INT,
    severity VARCHAR(16),
    message TEXT,
    enabled TINYINT(1) DEFAULT 1,
    created_at DATETIME,
    created_by VARCHAR(64),
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS silences (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    match_labels JSON,
    starts_at DATETIME,
    ends_at DATETIME,
    created_by VARCHAR(64),
    reason TEXT,
    created_at DATETIME,
    INDEX idx_tenant (tenant_id)
);
