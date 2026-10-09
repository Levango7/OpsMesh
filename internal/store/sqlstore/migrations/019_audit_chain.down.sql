-- 019_audit_chain.down.sql — 019_audit_chain.sql 的回滚脚本（P1-3）
--
-- 回滚动作：删除哈希链列与相关索引、链表头、归档表与归档元数据。
--
-- 真实回滚须知：
--   - 回滚即放弃「审计防篡改」能力：已写入的 prev_hash/entry_hash 一并丢弃，
--     历史行不再可验证（审计留痕本身仍保留在 audit_log 中，内容不受影响）。
--   - audit_log_archive / audit_archive_meta 中的【已归档行会被永久删除】：
--     若客户已开启 --audit-retention-days 并跑过归档，这些行是唯一副本。
--     需要保留时先手工导出：SELECT ... INTO OUTFILE / mysqldump audit_log_archive。
--   - 回滚须与二进制版本同步：先回退到不含链式写入的控制面版本，
--     否则写入路径会因缺列而报 Unknown column（降级为普通写入并打印告警）。
--   - 执行方式：手工执行（runMigrations 只跑正向迁移，migrationFiles 显式跳过 .down.sql）。
ALTER TABLE audit_log DROP INDEX idx_audit_entry_hash;
ALTER TABLE audit_log DROP COLUMN entry_hash;
ALTER TABLE audit_log DROP COLUMN prev_hash;
DROP TABLE IF EXISTS audit_archive_meta;
DROP TABLE IF EXISTS audit_log_archive;
DROP TABLE IF EXISTS audit_chain_head;
-- idx_audit_tenant_created 刻意保留：它是通用查询索引（非本迁移引入的语义对象），
-- 由 sql.go 的 createIndexIfMissing 统一维护，删除会拖慢按租户的审计检索。
