-- 030_tasks_approval_required_not_null.sql — 收敛 approval_required 的列定义到唯一形状
--
-- 缺陷（2026-10-27 存量库升级演练实证，docs/upgrade-rehearsal-2026-10-27.md §4）：
--   同一份代码在两条路径上建出了两种列定义——
--     · 新装：028 的 `TINYINT NOT NULL DEFAULT 0`（不可空）
--     · 老库升级：历史版本的 initSchemaExtra 先用 `BOOLEAN DEFAULT 0` 补了列
--       （= tinyint(1) NULL），之后 028 的 ADD COLUMN 撞 1060 被幂等放行 ⇒ 列一直可空
--   ⇒ 「升级上来的库」与「新装的库」结构不等价。028 自身修不了这件事：
--   ADD COLUMN 见列已存在就放行，改不了已有列的定义。
--
-- 方案：先把历史 NULL 回填为 0（列语义只有「要审批/不要审批」两态，NULL 没有业务含义），
--   再 MODIFY 到规范定义。MODIFY 要先保证无 NULL，MySQL 8 走 INPLACE 重建且不锁写
--   （LOCK=NONE，重建期间 tasks 的正常读写不阻塞）；配合迁移连接的 15s MDL 上限
--   （9bc25aa），长事务持表时快速失败而不是把整表写入挂住。
--
-- 幂等：列已是 NOT NULL 且无 NULL 行时，两条语句都是无副作用的重放。
UPDATE tasks SET approval_required = 0 WHERE approval_required IS NULL;
ALTER TABLE tasks MODIFY COLUMN approval_required TINYINT NOT NULL DEFAULT 0;
