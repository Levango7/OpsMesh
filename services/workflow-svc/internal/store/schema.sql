-- workflow-svc schema.sql — MySQL schema for workflow service
-- Tables: workflows, executions

CREATE TABLE IF NOT EXISTS workflows (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status VARCHAR(32) DEFAULT 'draft',
    nodes JSON,
    edges JSON,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_status (status)
);

CREATE TABLE IF NOT EXISTS executions (
    id VARCHAR(64) PRIMARY KEY,
    workflow_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) DEFAULT 'running',
    node_states JSON,
    context JSON,
    started_at DATETIME,
    completed_at DATETIME,
    error_message TEXT,
    INDEX idx_workflow (workflow_id),
    INDEX idx_status (status)
);
