-- 004_add_audit_trace_id.sql — M1-4 分布式可观测性：审计日志关联 trace_id
--
-- 问题：审计日志（audit_log）无法与 OTel 链路追踪/日志/SSE 事件关联，
--   故障排查时无法从一条审计记录反查完整的请求链路（agent→控制面→store）。
--
-- 方案：audit_log 表增加 trace_id 列（VARCHAR(64)，存 32 字符 hex trace_id），
--   AuditEvent.TraceID 由控制面 handler / gRPC handler 从 ctx 提取注入。
--   查询时可在 WHERE trace_id=? 按 trace_id 检索同一条链路的全部审计事件。
--
-- 兼容性：VARCHAR(64) NULL，老记录 trace_id=NULL，不影响现有逻辑；
--   新审计记录 trace_id 可空（无 OTel 场景）或 32 字符 hex（有 OTel 场景）。
--   索引 idx_audit_trace 加速按 trace_id 检索（高基数索引，仅在有审计检索需求时启用）。
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS trace_id VARCHAR(64);

-- 按 trace_id 检索索引（M1-4：从 trace_id 反查同链路全部审计事件）。
-- CREATE INDEX IF NOT EXISTS 在 MySQL 8.0+ 支持；老版本由 createIndexIfMissing 兜底。
CREATE INDEX IF NOT EXISTS idx_audit_trace ON audit_log (trace_id);