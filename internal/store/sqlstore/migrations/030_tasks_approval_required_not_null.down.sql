-- 030_tasks_approval_required_not_null.down.sql — 放松回可空（只回滚约束，不回滚数据）
--
-- 反向操作：把列改回可空。注意这不等于「恢复原状」——两条路径定义不一致本身就是本次修复
-- 要消灭的缺陷，回滚后升级库会重新与新装库不等价。只有在需要把二进制退回本迁移之前的
-- 版本（版本门禁要求 schema 不高于二进制已知最高版本）时才应执行。
ALTER TABLE tasks MODIFY COLUMN approval_required TINYINT NULL DEFAULT 0;
