-- gpu-svc schema.sql — MySQL schema for GPU service
-- Tables: gpu_nodes, gpu_workloads, gpu_models

CREATE TABLE IF NOT EXISTS gpu_nodes (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    address VARCHAR(255),
    gpus JSON,
    status VARCHAR(32) DEFAULT 'offline',
    labels JSON,
    total_vram_mb INT DEFAULT 0,
    used_vram_mb INT DEFAULT 0,
    gpu_errors INT DEFAULT 0,
    last_heartbeat DATETIME,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_status (status)
);

CREATE TABLE IF NOT EXISTS gpu_workloads (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    type VARCHAR(32),
    status VARCHAR(32) DEFAULT 'pending',
    gpu_request JSON,
    node_ids JSON,
    priority INT DEFAULT 0,
    image VARCHAR(255),
    command JSON,
    env JSON,
    replicas INT DEFAULT 1,
    model_name VARCHAR(255),
    created_at DATETIME,
    updated_at DATETIME,
    started_at DATETIME,
    finished_at DATETIME,
    error_message TEXT,
    INDEX idx_tenant (tenant_id),
    INDEX idx_status (status)
);

CREATE TABLE IF NOT EXISTS gpu_models (
    name VARCHAR(255) PRIMARY KEY,
    size_bytes BIGINT DEFAULT 0,
    parameter_count VARCHAR(64),
    quantized TINYINT(1) DEFAULT 0,
    serving TINYINT(1) DEFAULT 0,
    port INT DEFAULT 0,
    node_id VARCHAR(64),
    replicas INT DEFAULT 0,
    last_pulled DATETIME,
    INDEX idx_node (node_id)
);
