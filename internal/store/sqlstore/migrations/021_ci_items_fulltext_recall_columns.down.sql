-- 021_ci_items_fulltext_recall_columns.down.sql — 恢复到 020 的 3 列索引形态
--
-- 顺序与 up 对称：先删同名索引，再按 020 的定义重建。
--
-- **回滚是有代价的，必须写明**：回滚后索引不再覆盖 agent_id / device_id / source / id，
-- 那四类检索重新变成漏召回。代码侧不会报错——分流仍按 token 长度决定走哪条路径，
-- 而探测只看 index_name 与 ngram_token_size，认不出「列集合变窄」这件事。
-- 因此回滚 021 时必须同时回滚 internal/cmdb/sql.go 的 ciSearchFulltextCond
-- （改回 3 列），或把该版本的检索整体退回 LIKE；只回滚数据库而留下 7 列的 MATCH
-- 会直接报 1191，检索不可用。
--
-- 单向提示：ci_attrs_text 生成列仍由 020 拥有，本文件不动它。

ALTER TABLE ci_items DROP INDEX ft_ci_items_search;

ALTER TABLE ci_items
  ADD FULLTEXT INDEX ft_ci_items_search (name, ci_type, ci_attrs_text) WITH PARSER ngram;
