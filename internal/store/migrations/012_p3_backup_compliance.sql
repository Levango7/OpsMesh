-- 012_p3_backup_compliance.sql — P3 两个域持久化表
--
-- 本迁移为 Phase 3 两个领域建表：
--   1. backup_records     — 灾备备份记录（域 6）
--   2. compliance_reports — 安全合规报告（域 7）
--
-- 设计要点：
--   - 全部 CREATE TABLE IF NOT EXISTS，幂等可重入；
--   - 主键 id VARCHAR(64)，由应用层 ID 生成函数填充
--     （randBackupID/randComplianceID，前缀 backup-/compliance-）；
--   - tenant_id 默认 'default'，所有 List 走 WHERE tenant_id=? 实现租户隔离，
--     Get/Delete 走 WHERE id=? AND tenant_id=? 双重隔离；
--   - 两表均只有 created_at（无 updated_at）：
--       * backup_records 由灾备 API 创建后状态机推进，但本域不暴露 Update 方法；
--       * compliance_reports 报告不可改，重新扫描生成新 ID；
--   - compliance_reports.results 为 JSON 文本列（[]ComplianceResult 数组），
--     应用层用 encoding/json 序列化/反序列化；
--   - 时间戳一律 DATETIME，应用层写入 time.Now().UTC()。

-- ============================================================================
-- 域 6: 灾备备份记录
-- ============================================================================

CREATE TABLE IF NOT EXISTS backup_records (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    type       VARCHAR(32)  NOT NULL DEFAULT 'full',
    status     VARCHAR(32)  NOT NULL DEFAULT 'creating',
    size       BIGINT       NOT NULL DEFAULT 0,
    path       TEXT,
    created_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_backup_tenant (tenant_id),
    INDEX idx_backup_created (tenant_id, created_at)
);

-- ============================================================================
-- 域 7: 安全合规报告
-- ============================================================================

CREATE TABLE IF NOT EXISTS compliance_reports (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    device_id  VARCHAR(64),
    results    TEXT,
    score      INT          NOT NULL DEFAULT 0,
    created_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_compliance_tenant (tenant_id),
    INDEX idx_compliance_device (tenant_id, device_id),
    INDEX idx_compliance_created (tenant_id, created_at)
);