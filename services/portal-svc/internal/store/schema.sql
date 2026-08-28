-- portal-svc schema.sql — MySQL schema for portal service
-- Tables: resource_requests, quotas, budgets, cost_recommendations, utilizations, activities

CREATE TABLE IF NOT EXISTS resource_requests (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    requester VARCHAR(255),
    title VARCHAR(255) NOT NULL,
    description TEXT,
    resource_type VARCHAR(64),
    cpu INT DEFAULT 0,
    memory_gb INT DEFAULT 0,
    storage_gb INT DEFAULT 0,
    cost_estimate DOUBLE DEFAULT 0,
    status VARCHAR(32) DEFAULT 'draft',
    approver VARCHAR(64),
    approval_note TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id),
    INDEX idx_status (status)
);

CREATE TABLE IF NOT EXISTS quotas (
    tenant_id VARCHAR(64) PRIMARY KEY,
    max_cpu INT DEFAULT 0,
    max_memory_gb INT DEFAULT 0,
    max_storage_gb INT DEFAULT 0,
    max_requests INT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS budgets (
    tenant_id VARCHAR(64) PRIMARY KEY,
    monthly_limit DOUBLE DEFAULT 0,
    current_spend DOUBLE DEFAULT 0,
    alert_threshold DOUBLE DEFAULT 0
);

CREATE TABLE IF NOT EXISTS cost_recommendations (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    category VARCHAR(64),
    resource_id VARCHAR(64),
    description TEXT,
    savings DOUBLE DEFAULT 0,
    priority VARCHAR(32),
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS utilizations (
    tenant_id VARCHAR(64) PRIMARY KEY,
    cpu_usage DOUBLE DEFAULT 0,
    memory_usage DOUBLE DEFAULT 0,
    storage_usage DOUBLE DEFAULT 0,
    idle_count INT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS activities (
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
