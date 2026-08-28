-- log-svc schema.sql — MySQL schema for log service
-- Tables: agent_logs, audit_logs

CREATE TABLE IF NOT EXISTS agent_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    device_id VARCHAR(64),
    agent_id VARCHAR(64),
    task_id VARCHAR(64),
    level VARCHAR(16),
    source VARCHAR(128),
    message TEXT,
    timestamp DATETIME,
    INDEX idx_tenant (tenant_id),
    INDEX idx_agent (agent_id),
    INDEX idx_level (level),
    INDEX idx_timestamp (timestamp)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64),
    action VARCHAR(128),
    target VARCHAR(255),
    detail TEXT,
    timestamp DATETIME,
    INDEX idx_tenant (tenant_id),
    INDEX idx_timestamp (timestamp)
);
