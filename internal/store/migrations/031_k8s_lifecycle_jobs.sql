-- 031_k8s_lifecycle_jobs.sql — K8s 生命周期作业持久化表
--
-- 为 K8s 集群升级/回滚/证书轮换提供持久化作业状态机。
-- 对应 docs/design/k8s-lifecycle-execution-charter.md §6 数据模型。

CREATE TABLE `lifecycle_jobs` (
    `id` VARCHAR(64) NOT NULL COMMENT '作业 ID: {cluster}-{kind}-{timestamp}',
    `cluster_id` VARCHAR(64) NOT NULL COMMENT '集群 ID',
    `kind` VARCHAR(16) NOT NULL COMMENT '作业类型: upgrade/rollback/cert-rotate',
    `tenant_id` VARCHAR(64) NOT NULL COMMENT '租户 ID，鉴权与审计对齐',
    `phase` VARCHAR(32) NOT NULL DEFAULT 'pending' COMMENT '状态机阶段: pending/precheck/running/completed/failed/cancelled',
    `progress` INT NOT NULL DEFAULT 0 COMMENT '进度百分比 0-100',
    `current_step` VARCHAR(64) DEFAULT '' COMMENT '当前执行步骤',
    `spec_json` JSON NOT NULL COMMENT '入参快照（不含凭据）',
    `result_json` JSON DEFAULT NULL COMMENT '执行结果/输出',
    `error` TEXT DEFAULT NULL COMMENT '失败原因',
    `lock_owner` VARCHAR(64) DEFAULT '' COMMENT '乐观锁持有者，防并发冲突',
    `version` INT NOT NULL DEFAULT 0 COMMENT '乐观锁版本号',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_cluster_kind_active` (`cluster_id`, `kind`, `phase`) 
        COMMENT '同一集群同一类型同一时刻只允许一个非终态作业',
    KEY `idx_cluster_id` (`cluster_id`),
    KEY `idx_tenant_id` (`tenant_id`),
    KEY `idx_phase` (`phase`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='K8s 生命周期作业表';

-- 约束：同一 cluster_id+kind 同时最多 1 条非终态作业
-- 通过 uk_cluster_kind_active + phase 非终态值约束实现
-- 终态: completed/failed/cancelled 不计入唯一约束（可通过部分索引或应用层约束）

-- 兼容性说明：
-- phase 值: pending/precheck/running/control_plane_upgrading/worker_upgrading/verifying/completed/failed/cancelled
-- kind 值: upgrade/rollback/cert-rotate