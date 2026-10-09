-- 020_ci_items_fulltext.down.sql — 020_ci_items_fulltext.sql 的回滚脚本
--
-- 回滚动作：删除全文索引与 JSON 展平生成列。
--
-- 真实回滚须知：
--   - 回滚后检索**不会失效**，只是全部退回 LIKE 召回：internal/cmdb/sql.go 对本索引
--     存在与否做运行时探测（isCISearchFulltextReady），探不到就继续用 LIKE + 1000 条窗口。
--     也就是说 020 与代码的先后顺序不敏感——先跑迁移代码会用索引，
--     后跑迁移代码退回 LIKE，两种顺序都能正常工作。
--   - 回滚即放弃「候选窗口」这个正确性保证：CI 表大到命中数超过 1000 时，
--     最相关的行可能落在窗口外。这是 TD-77 之前的已知取舍，不是新引入的风险。
--   - 生成列是 STORED 的（落盘存储），删除它会**重写整张表**：大表上耗时以小时计，
--     且期间占用大量 IO。生产环境回滚前先评估表规模。
--   - 执行方式：手工执行（runMigrations 只跑正向迁移，migrationFiles 显式跳过 .down.sql）。

ALTER TABLE ci_items DROP INDEX ft_ci_items_search;
ALTER TABLE ci_items DROP COLUMN ci_attrs_text;
