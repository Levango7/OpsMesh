-- alert-svc schema.sql — MySQL schema for alert service
-- Tables: alerts, alert_rules, silences
--
-- ⚠️ 存量库不要靠这个文件升级：服务启动时 initSchema 会 CREATE TABLE IF NOT EXISTS，
-- 并对后加的列做 information_schema 幂等 ALTER（见 mysql.go 的 ensureColumn）。
-- 本文件与那段内联 DDL 的一致性由 TestInitSchemaMatchesSchemaSQL 钉住。

CREATE TABLE IF NOT EXISTS alerts (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    alert_id VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    rule_id VARCHAR(64),
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
