-- 013_p4_automation_network.sql — P4 两个域持久化表
--
-- 本迁移为 Phase 4 两个领域建表：
--   1. automation_rules + automation_executions — 自动化闭环（域 8）
--   2. network_devices + network_metrics         — 网络管理（域 9）
--
-- 设计要点：
--   - 全部 CREATE TABLE IF NOT EXISTS，幂等可重入；
--   - automation_rules / automation_executions / network_devices 主键 id VARCHAR(64)，
--     由应用层 ID 生成函数填充（randAutomationRuleID / randAutomationExecID / randNetworkDeviceID）；
--   - network_metrics 主键 id BIGINT AUTO_INCREMENT（时序追加写，无需应用层 ID）；
--   - tenant_id 默认 'default'，所有 List 走 WHERE tenant_id=? 实现租户隔离；
--   - JSON 列（automation_rules.trigger_params / automation_rules.actions）
--     以 TEXT 存储，应用层用 encoding/json 序列化/反序列化；
--   - automation_executions.ended_at 可空（NULL 表示未结束）；
--   - automation_rules.enabled 为 TINYINT(1)，0/1 表示禁用/启用；
--   - 时间戳一律 DATETIME，应用层写入 time.Now().UTC()。

-- ============================================================================
-- 域 8: Automation 自动化闭环
-- ============================================================================

CREATE TABLE IF NOT EXISTS automation_rules (
    id             VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL DEFAULT 'default',
    name           VARCHAR(255) NOT NULL,
    description    TEXT,
    trigger_type   VARCHAR(32)  NOT NULL,
    trigger_params TEXT,                    -- JSON: map[string]string
    actions        TEXT,                    -- JSON: []AutomationAction
    enabled        TINYINT(1)   NOT NULL DEFAULT 0,
    created_at     DATETIME     NOT NULL,
    updated_at     DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_auto_rule_tenant (tenant_id),
    INDEX idx_auto_rule_enabled (tenant_id, enabled)
);

CREATE TABLE IF NOT EXISTS automation_executions (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    rule_id    VARCHAR(64)  NOT NULL,
    rule_name  VARCHAR(255),
    status     VARCHAR(32)  NOT NULL DEFAULT 'pending',
    detail     TEXT,
    started_at DATETIME     NOT NULL,
    ended_at   DATETIME,                -- NULL 表示未结束
    PRIMARY KEY (id),
    INDEX idx_auto_exec_tenant (tenant_id),
    INDEX idx_auto_exec_rule (tenant_id, rule_id),
    INDEX idx_auto_exec_started (tenant_id, started_at)
);

-- ============================================================================
-- 域 9: Network 网络管理
-- ============================================================================

CREATE TABLE IF NOT EXISTS network_devices (
    id             VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL DEFAULT 'default',
    name           VARCHAR(255) NOT NULL,
    type           VARCHAR(32),
    vendor         VARCHAR(128),
    model          VARCHAR(128),
    ip             VARCHAR(64),
    mask           VARCHAR(32),
    mac            VARCHAR(64),
    location       VARCHAR(255),
    snmp_community VARCHAR(255),
    status         VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    config         TEXT,
    created_at     DATETIME     NOT NULL,
    updated_at     DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_netdev_tenant (tenant_id),
    INDEX idx_netdev_ip (tenant_id, ip),
    INDEX idx_netdev_status (tenant_id, status)
);

CREATE TABLE IF NOT EXISTS network_metrics (
    id           BIGINT       NOT NULL AUTO_INCREMENT,
    device_id    VARCHAR(64)  NOT NULL,
    tenant_id    VARCHAR(64)  NOT NULL DEFAULT 'default',
    timestamp    DATETIME     NOT NULL,
    cpu_usage    DOUBLE       NOT NULL DEFAULT 0,
    memory_usage DOUBLE       NOT NULL DEFAULT 0,
    temperature  DOUBLE       NOT NULL DEFAULT 0,
    uptime       BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    INDEX idx_netmetrics_device (device_id, timestamp)
);