-- deploy-svc schema.sql — MySQL schema for deploy service
-- Tables: deployments, deploy_templates, canaries

CREATE TABLE IF NOT EXISTS deployments (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32),
    repo_url VARCHAR(512),
    content MEDIUMTEXT,
    path VARCHAR(512),
    target_ids JSON,
    status VARCHAR(32) DEFAULT 'pending',
    strategy VARCHAR(32),
    canary_weight INT DEFAULT 0,
    auto_rollback TINYINT(1) DEFAULT 0,
    created_by VARCHAR(64),
    error_message TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id),
    INDEX idx_status (status)
);

CREATE TABLE IF NOT EXISTS deploy_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    type VARCHAR(32),
    repo_url VARCHAR(512),
    content MEDIUMTEXT,
    path VARCHAR(512),
    parameters JSON,
    created_by VARCHAR(64),
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS canaries (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    deployment_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    weight INT DEFAULT 0,
    status VARCHAR(32) DEFAULT 'pending',
    success_count INT DEFAULT 0,
    failure_count INT DEFAULT 0,
    created_by VARCHAR(64),
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id),
    INDEX idx_deployment (deployment_id),
    INDEX idx_status (status)
);
