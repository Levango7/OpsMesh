-- 024_cloud_accounts.down.sql — 回滚 024（删 cloud_accounts / cloud_resources 表）。
-- 注意：删表即丢云账号与已发现云资源数据；仅在确认不再需要时执行。
DROP TABLE IF EXISTS cloud_resources;
DROP TABLE IF EXISTS cloud_accounts;
