-- Device Service MySQL Schema

CREATE TABLE IF NOT EXISTS devices (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(256) NOT NULL,
    ip VARCHAR(64) DEFAULT '',
    mac VARCHAR(64) DEFAULT '',
    os VARCHAR(64) DEFAULT '',
    arch VARCHAR(64) DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'online',
    agent_id VARCHAR(64) DEFAULT '',
    tags JSON,
    labels JSON,
    `group` VARCHAR(128) DEFAULT '',
    lastHeartbeat TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_devices_tenant (tenant_id),
    INDEX idx_devices_status (status),
    INDEX idx_devices_agent (agent_id),
    INDEX idx_devices_group (`group`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS agents (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    device_id VARCHAR(64) DEFAULT '',
    hostname VARCHAR(256) DEFAULT '',
    version VARCHAR(64) DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'online',
    load_count INT NOT NULL DEFAULT 0,
    os VARCHAR(64) DEFAULT '',
    arch VARCHAR(64) DEFAULT '',
    addr VARCHAR(256) DEFAULT '',
    grpc_port INT DEFAULT 0,
    metrics_port INT DEFAULT 0,
    lastHeartbeat TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_agents_tenant (tenant_id),
    INDEX idx_agents_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ci_items (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    ci_type VARCHAR(64) NOT NULL,
    name VARCHAR(256) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    attributes JSON,
    source VARCHAR(128) DEFAULT '',
    agent_id VARCHAR(64) DEFAULT '',
    device_id VARCHAR(64) DEFAULT '',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_ci_tenant (tenant_id),
    INDEX idx_ci_type (ci_type),
    INDEX idx_ci_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ci_relations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    source_ci_id VARCHAR(64) NOT NULL,
    target_ci_id VARCHAR(64) NOT NULL,
    relation_type VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    attributes JSON,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_relations_source (source_ci_id),
    INDEX idx_relations_target (target_ci_id),
    INDEX idx_relations_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS discovery_jobs (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    cidr VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    total_hosts INT DEFAULT 0,
    scanned_hosts INT DEFAULT 0,
    found_devices INT DEFAULT 0,
    error_msg TEXT,
    started_at TIMESTAMP NULL,
    completed_at TIMESTAMP NULL,
    INDEX idx_discovery_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
