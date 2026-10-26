-- 026_audit_high_risk.sql — 审计事件高风险标记列（Wave 5-C 审计链加固）。
--
-- 背景：高风险操作（user_delete、firewall_add/del、service stop/start/restart、
-- pkg install/remove/upgrade、task create/approve/reject、secret rotate/delete）产出的
-- 审计事件需可单独检索并纳入哈希链防篡改校验，故 audit_log 需承载 high_risk 标记。
--
-- 方案：audit_log 与 audit_log_archive 表增加 high_risk TINYINT(1) 列，默认 0（非高风险）。
--   AuditEvent.HighRisk 由控制面 AuditHighRisk 写入时置 1；
--   QueryHighRiskAudits 按 high_risk=1 过滤，供 GET /api/v1/audit/high-risk；
--   auditEntryHash 将 high_risk 参与哈希，使高风险标记被篡改即链断。
--
-- 兼容性：TINYINT(1) NOT NULL DEFAULT 0，存量记录 high_risk=0，不影响既有逻辑；
--   老库未应用本迁移时 columnExists 返回 false，Audit/Query 降级为不读写 high_risk
--  （与 trace_id 列的向后兼容模式一致）。
ALTER TABLE audit_log ADD COLUMN high_risk TINYINT(1) NOT NULL DEFAULT 0;
CREATE INDEX idx_audit_high_risk ON audit_log (high_risk);
ALTER TABLE audit_log_archive ADD COLUMN high_risk TINYINT(1) NOT NULL DEFAULT 0;
