-- OpsMesh Unified MySQL Schema
-- Auto-generated: combined from all service schema.sql files
-- Services: auth, device, task, alert, config, gpu, portal, log, workflow, incident, autoscaler, plugin

CREATE DATABASE IF NOT EXISTS opsmesh CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE opsmesh;

-- =============================================================================
-- auth-svc :: users, roles, permissions, user_roles, role_permissions, refresh_tokens, jti_blacklist
-- =============================================================================

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(128) NOT NULL UNIQUE,
    email VARCHAR(256) NOT NULL,
    password_hash VARCHAR(256) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    must_change_password TINYINT(1) NOT NULL DEFAULT 0,
    INDEX idx_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    description VARCHAR(512) DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_roles_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS permissions (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    description VARCHAR(512) DEFAULT '',
    perm_group VARCHAR(128) DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_roles (
    user_id VARCHAR(64) NOT NULL,
    role_id VARCHAR(64) NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id VARCHAR(64) NOT NULL,
    permission_name VARCHAR(128) NOT NULL,
    PRIMARY KEY (role_id, permission_name),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash VARCHAR(256) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) DEFAULT '',
    device_fp VARCHAR(256) DEFAULT '',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_refresh_tokens_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS jti_blacklist (
    jti VARCHAR(64) PRIMARY KEY,
    expires_at TIMESTAMP NOT NULL,
    INDEX idx_jti_blacklist_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- device-svc :: devices, agents, ci_items, ci_relations, discovery_jobs
-- =============================================================================

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

-- =============================================================================
-- task-svc :: tasks, task_results, task_logs, schedules, batches, batch_tasks
-- =============================================================================

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

-- =============================================================================
-- alert-svc :: alerts, alert_rules, silences
-- =============================================================================

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- config-svc :: config_entries, config_history, config_secrets, notify_channels, config_templates
-- =============================================================================

CREATE TABLE IF NOT EXISTS config_entries (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    value TEXT,
    format VARCHAR(32),
    version INT DEFAULT 1,
    description TEXT,
    updated_by VARCHAR(64),
    created_at DATETIME,
    updated_at DATETIME,
    UNIQUE KEY uk_tenant_key (tenant_id, key_name),
    INDEX idx_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS config_history (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    value TEXT,
    format VARCHAR(32),
    version INT,
    description TEXT,
    updated_by VARCHAR(64),
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant_key (tenant_id, key_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS config_secrets (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    value TEXT,
    key_type VARCHAR(32),
    version INT DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    UNIQUE KEY uk_tenant_key (tenant_id, key_name),
    INDEX idx_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS notify_channels (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32),
    config JSON,
    enabled TINYINT(1) DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS config_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    content MEDIUMTEXT,
    variables JSON,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- gpu-svc :: gpu_nodes, gpu_workloads, gpu_models
-- =============================================================================

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- portal-svc :: resource_requests, quotas, budgets, cost_recommendations, utilizations, activities
-- =============================================================================

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS quotas (
    tenant_id VARCHAR(64) PRIMARY KEY,
    max_cpu INT DEFAULT 0,
    max_memory_gb INT DEFAULT 0,
    max_storage_gb INT DEFAULT 0,
    max_requests INT DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS budgets (
    tenant_id VARCHAR(64) PRIMARY KEY,
    monthly_limit DOUBLE DEFAULT 0,
    current_spend DOUBLE DEFAULT 0,
    alert_threshold DOUBLE DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cost_recommendations (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    category VARCHAR(64),
    resource_id VARCHAR(64),
    description TEXT,
    savings DOUBLE DEFAULT 0,
    priority VARCHAR(32),
    INDEX idx_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS utilizations (
    tenant_id VARCHAR(64) PRIMARY KEY,
    cpu_usage DOUBLE DEFAULT 0,
    memory_usage DOUBLE DEFAULT 0,
    storage_usage DOUBLE DEFAULT 0,
    idle_count INT DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- log-svc :: agent_logs, audit_logs
-- =============================================================================

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- workflow-svc :: workflows, executions
-- =============================================================================

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- incident-svc :: incidents, timeline_events
-- =============================================================================

CREATE TABLE IF NOT EXISTS incidents (
    id VARCHAR(64) PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    severity VARCHAR(16),
    status VARCHAR(32) DEFAULT 'detected',
    alert_ids JSON,
    device_ids JSON,
    assignee VARCHAR(64),
    tags JSON,
    detected_at DATETIME,
    resolved_at DATETIME,
    closed_at DATETIME,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_status (status),
    INDEX idx_severity (severity)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS timeline_events (
    id VARCHAR(64) PRIMARY KEY,
    incident_id VARCHAR(64) NOT NULL,
    timestamp DATETIME,
    type VARCHAR(64),
    description TEXT,
    author VARCHAR(64),
    INDEX idx_incident (incident_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- autoscaler-svc :: scaling_rules, scaling_decisions
-- =============================================================================

CREATE TABLE IF NOT EXISTS scaling_rules (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    deployment VARCHAR(255),
    namespace VARCHAR(255),
    metric VARCHAR(128),
    scale_up_threshold DOUBLE,
    scale_down_threshold DOUBLE,
    min_replicas INT,
    max_replicas INT,
    cooldown_up BIGINT,
    cooldown_down BIGINT,
    enabled TINYINT(1) DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_deployment (deployment)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS scaling_decisions (
    id VARCHAR(64) PRIMARY KEY,
    rule_id VARCHAR(64) NOT NULL,
    deployment VARCHAR(255),
    namespace VARCHAR(255),
    action VARCHAR(32),
    from_replicas INT,
    to_replicas INT,
    reason TEXT,
    metric_value DOUBLE,
    timestamp DATETIME,
    INDEX idx_rule (rule_id),
    INDEX idx_timestamp (timestamp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =============================================================================
-- plugin-svc :: plugins, plugin_versions
-- =============================================================================

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS plugin_versions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    plugin_id VARCHAR(64) NOT NULL,
    version VARCHAR(64) NOT NULL,
    checksum VARCHAR(128),
    download_url VARCHAR(512),
    released_at DATETIME,
    changelog TEXT,
    INDEX idx_plugin (plugin_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
