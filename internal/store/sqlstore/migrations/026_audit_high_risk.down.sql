-- 026_audit_high_risk.down.sql — 回滚 high_risk 列（谨慎：会丢失高风险标记）。
ALTER TABLE audit_log DROP COLUMN high_risk;
ALTER TABLE audit_log_archive DROP COLUMN high_risk;
