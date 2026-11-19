-- 036_abac_decision_logs.sql — ABAC 决策审计日志（M3：agent 决策回传）
--
-- 背景：设备端 ABAC 评估（含离线模式）产生的 allow/deny 决策需要回传控制面留痕，
-- 供租户级审计检索与保留期清理。agent 批量回传（<=500 条/批），控制面在 per-agent
-- HMAC 认证后按 agent 归属租户落库，agent 自报租户一律忽略。
--
-- 时间语义：
--   - occurred_at：agent 端实际评估时间（离线补传可能滞后）；
--   - received_at：控制面接收时间，保留期清理只依据此列（避免补传即被清理）。
--
-- 索引：租户级检索 (tenant_id, received_at)，agent 级排障 (agent_id, received_at)。

CREATE TABLE IF NOT EXISTS `abac_decision_logs` (
    `id`          BIGINT       NOT NULL AUTO_INCREMENT COMMENT '自增主键',
    `tenant_id`   VARCHAR(128) NOT NULL COMMENT '归属租户（控制面按 agent 身份回填）',
    `agent_id`    VARCHAR(128) NOT NULL COMMENT '上报 agent',
    `request_id`  VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'agent 侧请求 ID',
    `bundle_id`   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '评估所用策略包 ID',
    `policy_id`   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '命中策略 ID（空=default-deny）',
    `allowed`     TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '决策结果（1=allow）',
    `reason`      VARCHAR(512) NOT NULL DEFAULT '' COMMENT '决策原因（截断至 512 字符）',
    `mode`        VARCHAR(32)  NOT NULL COMMENT 'device / device-offline',
    `occurred_at` DATETIME     NOT NULL COMMENT 'agent 端评估时间（UTC）',
    `received_at` DATETIME     NOT NULL COMMENT '控制面接收时间（UTC，保留期依据）',
    PRIMARY KEY (`id`),
    KEY `idx_abac_decision_logs_tenant_received` (`tenant_id`, `received_at`),
    KEY `idx_abac_decision_logs_agent_received` (`agent_id`, `received_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='ABAC 决策审计日志（M3）';
