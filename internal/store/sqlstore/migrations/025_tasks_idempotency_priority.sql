-- Wave 4-B：任务优先级（priority）与幂等键（idempotency_key）持久化列。
-- MySQL 不支持 ADD COLUMN IF NOT EXISTS；迁移框架的 tolerateIdempotentDDLError 会对
-- 重复列（1060 ER_DUP_FIELDNAME）做 information_schema 二次核实后幂等放行，
-- 因此本文件可重复重放、安全再执行。
ALTER TABLE tasks ADD COLUMN priority INT DEFAULT 0;
ALTER TABLE tasks ADD COLUMN idempotency_key VARCHAR(128);
