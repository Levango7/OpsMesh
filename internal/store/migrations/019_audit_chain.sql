-- 019_audit_chain.sql — 审计日志防篡改哈希链 + 归档保留（P1-3）
--
-- 背景（商用就绪评审 P1-3）：audit_log 是普通追加表，无 hash 链/签名——持有 DB 凭证
-- 即可静默改写历史行（等保三级对审计记录明确要求「防篡改」）；且全仓无保留/归档策略，
-- 只能靠「永不删除」满足留痕要求，表随时间无限增长。
--
-- 本迁移引入四件事：
--   1. audit_log.prev_hash / entry_hash：逐行哈希链
--      entry_hash = sha256(prev_hash || 规范化(tenant,user,action,target,detail,created_at,trace_id))
--      任一行内容被改写、删除或换序，后续行的 prev/entry 链接即对不上（校验见 VerifyAuditChain）。
--   2. audit_chain_head：单行链表头。链式写入在事务内对它 SELECT ... FOR UPDATE，
--      使多副本并发追加串行化、链不分叉（单行锁 = 天然序列化点）。
--   3. audit_log_archive / audit_archive_meta：归档表与边界元数据。保留策略
--      （--audit-retention-days）把超龄行搬入归档表再删除时，边界哈希用于把
--      「已归档段」与「在线段」的链接接上，使全历史仍可验证。
--   4. (tenant_id, created_at) 复合索引归入迁移（原由 sql.go 的 createIndexIfMissing 兜底创建，
--      此处显式落库，使全新库与存量库收敛到同一状态）；entry_hash 索引用于校验时按行定位。
--
-- 幂等性：MySQL 8 不支持 ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，
-- 重放安全由 applyMigration 的 tolerateIdempotentDDLError（1050/1060/1061 二次核实）兜底；
-- 数据初始化语句一律 INSERT IGNORE（重复执行不会因主键冲突失败）。
--
-- 回滚：见 019_audit_chain.down.sql（会丢弃链与归档表，须先确认不再需要防篡改能力）。

ALTER TABLE audit_log ADD COLUMN prev_hash CHAR(64) NULL;
ALTER TABLE audit_log ADD COLUMN entry_hash CHAR(64) NULL;
CREATE INDEX idx_audit_tenant_created ON audit_log (tenant_id, created_at DESC);
CREATE INDEX idx_audit_entry_hash ON audit_log (entry_hash);

CREATE TABLE IF NOT EXISTS audit_chain_head (
    id INT PRIMARY KEY,
    last_id BIGINT NOT NULL DEFAULT 0,
    last_hash CHAR(64) NOT NULL DEFAULT '',
    updated_at DATETIME
);
INSERT IGNORE INTO audit_chain_head (id, last_id, last_hash, updated_at) VALUES (1, 0, '', NOW());

CREATE TABLE IF NOT EXISTS audit_log_archive (
    id BIGINT PRIMARY KEY,
    tenant_id VARCHAR(64),
    user_id VARCHAR(64),
    action VARCHAR(64),
    target VARCHAR(128),
    detail TEXT,
    created_at DATETIME,
    trace_id VARCHAR(64),
    prev_hash CHAR(64),
    entry_hash CHAR(64),
    archived_at DATETIME,
    KEY idx_audit_archive_tenant_created (tenant_id, created_at)
);
CREATE TABLE IF NOT EXISTS audit_archive_meta (
    id INT PRIMARY KEY,
    archived_through_id BIGINT NOT NULL DEFAULT 0,
    boundary_hash CHAR(64) NOT NULL DEFAULT '',
    archived_rows BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME
);
INSERT IGNORE INTO audit_archive_meta (id, archived_through_id, boundary_hash, archived_rows, updated_at) VALUES (1, 0, '', 0, NOW());
