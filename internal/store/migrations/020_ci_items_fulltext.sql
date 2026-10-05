-- 020_ci_items_fulltext.sql — CMDB 检索的全文索引（见 docs/COORDINATION.md 跨线诉求）
--
-- 背景：ci_items 的检索此前走 LIKE 子串匹配 + 1000 条候选窗口（internal/cmdb/sql.go
-- 刻意「零 schema 变更」的取舍）。两个后果：
--   1. 候选过多时最相关的少数 CI 可能落在 1000 条窗口外（按 updated_at DESC 截断，
--      而更新的 CI 不一定更相关）——这是正确性问题，不只是性能问题。
--   2. LIKE '%x%' 无法走索引，全表扫描，CI 表增长后延迟线性劣化。
--
-- 为什么需要生成列：ci_items.attrs 是 JSON 类型，MySQL 的 FULLTEXT 索引**不能直接建在
-- JSON 列上**。故加一个 STORED 生成列把 JSON 展平成 TEXT 再建索引。本机 MySQL 8.0.46
-- 实测（2026-10-05）：`TEXT GENERATED ALWAYS AS (CAST(attrs AS CHAR)) STORED` +
-- `FULLTEXT INDEX … WITH PARSER ngram` 可建、可写入、布尔模式召回正确。
--
-- 为什么 WITH PARSER ngram：默认分词器按空白切词，ci_items 里大量中文属性值
-- （owner="张伟"、env="生产机房"）会被当成一个整词，搜「生产机房」命中不了只写了
-- 「生产环境」的行。ngram 按 ngram_token_size（默认 2）切中文，是 MySQL 内置插件
-- （无需安装）。
--
-- ！！召回口径的实测结论（改 sql.go 前必读，这条决定了分流规则不是可选项）！！
--   internal/cmdb 的分词器 fulltext.Tokenize 把中文按**单字**切（"生产机房" → 生/产/机/房），
--   而 ngram 索引按**双字**切。两个粒度不一致，导致单字 token 在 MATCH 下一律召回为空。
--   本机 MySQL 8.0.46 实测（2026-10-05，10 行中英混合语料，含 HEX 校验排除字符集干扰），
--   逐 token 对比「7 列 LIKE 子串」与「MATCH … AGAINST('+tok' IN BOOLEAN MODE)」的命中集合：
--
--     token 类别                            样本数   LIKE 命中      MATCH 命中     结论
--     长度 = 1（生/产/张/机/房/订/单/w/北…）   35       非空           **全为空**     MATCH 漏召回
--     长度 ≥ 2 且无下划线（web/db/生产/机房）  13       非空           ⊇ LIKE         可安全换召回
--     含下划线（server_prod）                 1        ci3            **空**         MATCH 漏召回
--
--   也就是说：**若把召回整体换成 MATCH，中文与单字符检索会全部返回空结果**，
--   这是把召回变窄的正确性回归，而非优化。因此 sql.go 采用按 token 分流：
--   长度 = 1 或含下划线的 token 走 LIKE，其余走 MATCH。
--
--   允许 MATCH 略放宽召回（多召回）的前提：精确判定与排序仍由 internal/cmdb/search.go 的
--   matchCI 负责，它按**前缀匹配**逐 token 复核，MATCH 的伪命中（如 ngram 跨字切分把
--   "订单1" 匹到 "订单服务"）会在那一层被过滤掉。反向（漏召回）则无法补救，故必须避免。
--
-- 覆盖列的选择：只索引 name / ci_type / attrs 三类可检索文本。
-- 不把 agent_id / device_id / id / source 放进全文索引有两个原因：
--   1. 它们是标识符，用户按前缀搜已由 matchCI 的前缀匹配覆盖；
--   2. 这几列只参与 LIKE 召回（走 B-Tree 更快），不进 MATCH。
-- 代价要说清楚：**含下划线的 token 无法从 MATCH 受益**（ngram 把下划线当字面量，
-- 而 LIKE 里下划线是单字符通配符，两者语义本就不同），这类 token 一律回退 LIKE。
--
-- 幂等性：MySQL 8 不支持 ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，
-- 重放安全由 applyMigration 的 tolerateIdempotentDDLError（1050/1060/1061 二次核实）兜底。
--
-- 不加数据初始化语句：本迁移纯结构变更，不动任何行。
--
-- 回滚：见 020_ci_items_fulltext.down.sql。

ALTER TABLE ci_items
  ADD COLUMN ci_attrs_text TEXT GENERATED ALWAYS AS (CAST(attrs AS CHAR)) STORED;

ALTER TABLE ci_items
  ADD FULLTEXT INDEX ft_ci_items_search (name, ci_type, ci_attrs_text) WITH PARSER ngram;
