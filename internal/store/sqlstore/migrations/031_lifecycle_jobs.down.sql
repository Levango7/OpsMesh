-- 031_lifecycle_jobs.down.sql — 回滚：删表（作业记录是可再生数据，升级重跑即可）
DROP TABLE IF EXISTS lifecycle_jobs;
