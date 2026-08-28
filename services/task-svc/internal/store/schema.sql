-- Task Service MySQL Schema

CREATE TABLE IF NOT EXISTS tasks (
    task_id VARCHAR(64) PRIMARY KEY,
    agent_id VARCHAR(64) DEFAULT '',
    tenant_id VARCHAR(64) NOT NULL,
    type VARCHAR(32) NOT NULL,
    command TEXT,
    content TEXT,
    path VARCHAR(512) DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    claimed_by VARCHAR(64) DEFAULT '',
    claimed_at TIMESTAMP NULL,
    claim_epoch BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    dead_letter TINYINT(1) NOT NULL DEFAULT 0,
    timeout INT DEFAULT 0,
    retry_delay INT DEFAULT 0,
    schedule VARCHAR(64) DEFAULT '',
    parent_id VARCHAR(64) DEFAULT '',
    depends_on JSON,
    approval_required TINYINT(1) NOT NULL DEFAULT 0,
    approved_by VARCHAR(64) DEFAULT '',
    approved_at TIMESTAMP NULL,
    batch_id VARCHAR(64) DEFAULT '',
    INDEX idx_tasks_tenant (tenant_id),
    INDEX idx_tasks_status (status),
    INDEX idx_tasks_agent (agent_id),
    INDEX idx_tasks_batch (batch_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS task_results (
    task_id VARCHAR(64) PRIMARY KEY,
    agent_id VARCHAR(64) DEFAULT '',
    exit_code INT NOT NULL DEFAULT 0,
    stdout LONGTEXT,
    stderr LONGTEXT,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    finished_at TIMESTAMP NULL,
    claim_epoch BIGINT NOT NULL DEFAULT 0,
    INDEX idx_results_agent (agent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS task_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id VARCHAR(64) NOT NULL,
    log_timestamp TIMESTAMP NOT NULL,
    level VARCHAR(16) DEFAULT '',
    message TEXT,
    INDEX idx_logs_task (task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS schedules (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(256) NOT NULL,
    cron_expr VARCHAR(128) NOT NULL,
    task_type VARCHAR(32) NOT NULL,
    command TEXT,
    content TEXT,
    path VARCHAR(512) DEFAULT '',
    agent_id VARCHAR(64) DEFAULT '',
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    last_fired_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_schedules_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS batches (
    batch_id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(256) NOT NULL,
    total_count INT NOT NULL DEFAULT 0,
    success_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    pending_count INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_batches_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS batch_tasks (
    batch_id VARCHAR(64) NOT NULL,
    task_id VARCHAR(64) NOT NULL,
    PRIMARY KEY (batch_id, task_id),
    INDEX idx_batch_tasks_batch (batch_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
