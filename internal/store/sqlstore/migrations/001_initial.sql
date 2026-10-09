-- 001_initial.sql — 初始 schema 快照
--
-- 本文件是 internal/store/sql.go 中 initSchema 历史上所有 CREATE TABLE IF NOT EXISTS
-- 语句的逐字提取，作为版本化迁移框架（runMigrations）的 001 号迁移。
--
-- 说明：
--   - 所有语句保持 IF NOT EXISTS 语义，对已存在库幂等。
--   - schema_migrations 表本身不在此文件中：runMigrations 在执行任何迁移前先确保
--     该表存在，用于记录已应用版本号。
--   - 历史上为兼容老库而存在的增量补列（alterColumnIfMissing）与补索引
--     （createIndexIfMissing）逻辑保留在 Go 代码 runMigrations 末尾调用，
--     作为向后兼容补丁；后续新增列应直接以 002_xxx.sql 迁移形式纳入。

-- agents：agent 注册信息（gRPC 身份绑定 secret 列）
CREATE TABLE IF NOT EXISTS agents (
    agent_id VARCHAR(64) PRIMARY KEY,
    hostname VARCHAR(255),
    segment VARCHAR(64),
    tenant_id VARCHAR(64),
    addr VARCHAR(255),
    grpc_port INT,
    metrics_port INT,
    status VARCHAR(16),
    `load` INT,
    last_seen DATETIME
);

-- devices：被纳管设备
CREATE TABLE IF NOT EXISTS devices (
    device_id VARCHAR(64) PRIMARY KEY,
    segment VARCHAR(64),
    tenant_id VARCHAR(64),
    ip VARCHAR(64),
    agent_id VARCHAR(64),
    state VARCHAR(16),
    task_state VARCHAR(16),
    managed BOOLEAN DEFAULT 0,
    last_result VARCHAR(16),
    last_result_at DATETIME,
    retired BOOLEAN DEFAULT 0
);

-- tasks：任务定义（含调度/重试/审批/依赖字段）
CREATE TABLE IF NOT EXISTS tasks (
    task_id VARCHAR(64) PRIMARY KEY,
    agent_id VARCHAR(64),
    tenant_id VARCHAR(64),
    type VARCHAR(32),
    command TEXT,
    content MEDIUMTEXT,
    path VARCHAR(512),
    status VARCHAR(16),
    claimed_by VARCHAR(64),
    claimed_at DATETIME,
    created_at DATETIME,
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 0,
    dead_letter BOOLEAN DEFAULT 0,
    schedule VARCHAR(64),
    parent_id VARCHAR(64),
    last_fired_at DATETIME,
    depends_on TEXT,
    timeout INT DEFAULT 0,
    retry_delay INT DEFAULT 0
);

-- task_results：任务执行结果
CREATE TABLE IF NOT EXISTS task_results (
    task_id VARCHAR(64) PRIMARY KEY,
    agent_id VARCHAR(64),
    exit_code INT,
    stdout MEDIUMTEXT,
    stderr MEDIUMTEXT,
    finished_at DATETIME
);

-- audit_log：审计日志
CREATE TABLE IF NOT EXISTS audit_log (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64),
    user_id VARCHAR(64),
    action VARCHAR(64),
    target VARCHAR(128),
    detail TEXT,
    created_at DATETIME
);

-- leader_lease：选主租约表（单行 id=1）
CREATE TABLE IF NOT EXISTS leader_lease (
    id INT PRIMARY KEY DEFAULT 1,
    holder VARCHAR(128),
    expires_at DATETIME,
    updated_at DATETIME
);

-- install_tokens：自动纳管 install token 登记表
CREATE TABLE IF NOT EXISTS install_tokens (
    token VARCHAR(512) PRIMARY KEY,
    device_id VARCHAR(64),
    tenant_id VARCHAR(64),
    expires_at DATETIME,
    consumed BOOLEAN DEFAULT 0
);

-- users：用户中心
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    email VARCHAR(255),
    password_hash VARCHAR(255),
    status VARCHAR(16) DEFAULT 'active',
    role_ids JSON,
    created_at DATETIME
);

-- roles：角色
CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    description VARCHAR(255),
    permissions JSON,
    created_at DATETIME
);

-- permissions：权限
CREATE TABLE IF NOT EXISTS permissions (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    description VARCHAR(255),
    group_name VARCHAR(64)
);

-- alerts：告警（M7 ack/silence 扩展字段）
CREATE TABLE IF NOT EXISTS alerts (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64),
    device_id VARCHAR(64),
    agent_id VARCHAR(64),
    severity VARCHAR(16),
    message TEXT,
    created_at DATETIME,
    alert_id VARCHAR(64),
    status VARCHAR(16),
    acknowledged_by VARCHAR(64),
    silenced_until DATETIME,
    comment TEXT,
    updated_at DATETIME
);

-- ci_types：CMDB CI 类型字典（Phase 1）
CREATE TABLE IF NOT EXISTS ci_types (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(64),
    builtin BOOLEAN DEFAULT 1,
    created_at DATETIME
);

-- ci_items：CMDB CI 实例（Phase-3 审批流 approval_status 列）
CREATE TABLE IF NOT EXISTS ci_items (
    id VARCHAR(64) PRIMARY KEY,
    ci_type VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) DEFAULT 'active',
    approval_status VARCHAR(16) DEFAULT 'approved',
    attrs JSON,
    source VARCHAR(32) DEFAULT 'manual',
    agent_id VARCHAR(64),
    device_id VARCHAR(64),
    version INT DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME
);

-- ci_relations：CMDB CI 关系
CREATE TABLE IF NOT EXISTS ci_relations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    source_ci_id VARCHAR(64) NOT NULL,
    target_ci_id VARCHAR(64) NOT NULL,
    relation_type VARCHAR(32) NOT NULL,
    tenant_id VARCHAR(64),
    attributes JSON,
    created_at DATETIME,
    UNIQUE KEY uq_rel (source_ci_id, target_ci_id, relation_type)
);

-- ci_attr_templates：CMDB CI 属性模板
CREATE TABLE IF NOT EXISTS ci_attr_templates (
    id INT AUTO_INCREMENT PRIMARY KEY,
    ci_type VARCHAR(64) NOT NULL,
    attr_key VARCHAR(64) NOT NULL,
    label VARCHAR(128) NOT NULL,
    attr_type VARCHAR(32) DEFAULT 'string',
    required BOOLEAN DEFAULT 0,
    default_value TEXT,
    tenant_id VARCHAR(64),
    created_at DATETIME,
    UNIQUE KEY uq_tmpl (ci_type, attr_key)
);

-- k8s_clusters：Phase 3 K8s 集群管理
CREATE TABLE IF NOT EXISTS k8s_clusters (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    name VARCHAR(255) NOT NULL,
    server VARCHAR(255),
    kubeconfig TEXT,
    status VARCHAR(16) DEFAULT 'unknown',
    created_at DATETIME,
    updated_at DATETIME
);

-- alert_rules：告警规则
CREATE TABLE IF NOT EXISTS alert_rules (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    metric VARCHAR(128),
    op VARCHAR(8),
    threshold DOUBLE,
    for_duration INT DEFAULT 0,
    severity VARCHAR(16),
    message TEXT,
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME
);

-- os_templates：OS 安装模板
CREATE TABLE IF NOT EXISTS os_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    name VARCHAR(255) NOT NULL,
    os VARCHAR(64),
    version VARCHAR(64),
    arch VARCHAR(32),
    install_url VARCHAR(512),
    config TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

-- middleware_templates：中间件部署模板
CREATE TABLE IF NOT EXISTS middleware_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    name VARCHAR(255) NOT NULL,
    type VARCHAR(64),
    version VARCHAR(64),
    config TEXT,
    created_at DATETIME,
    updated_at DATETIME
);

-- refresh_tokens：刷新令牌
CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64),
    tenant_id VARCHAR(64),
    device_fp VARCHAR(255),
    expires_at DATETIME,
    created_at DATETIME
);