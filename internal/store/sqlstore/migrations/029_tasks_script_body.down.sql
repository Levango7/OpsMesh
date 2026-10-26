-- 029_tasks_script_body.down.sql — 回滚 script_body 列（谨慎：列数据丢失）。
--
-- 反向操作：删除 tasks.script_body 列。回滚后多行脚本任务退化为「只有 command 的
-- 单行任务」——agent 端会因 command 为 `sh {script}` 占位符而找不到脚本文件，
-- 脚本类任务执行失败。故回滚必须与应用了新版本控制面二进制的动作配对，
-- 单独回滚数据库会把多行脚本能力退回本迁移前的缺陷形态。
ALTER TABLE tasks DROP COLUMN script_body;
