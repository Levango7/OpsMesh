-- 022_gateway_routes.down.sql — 回退 022：删除路由持久化表
--
-- 顺序与 up 对称：先删索引再删表（MySQL InnoDB 在 DROP TABLE 时自动连带索引，
-- 显式写出顺序只为与 up 的 CREATE 语句对账，避免读者猜）。

DROP TABLE IF EXISTS gateway_routes;
