-- OpsMesh MySQL 引导脚本（**只建库 + 授权，不含任何建表语句**）
--
-- 表结构的事实源（单一归属，2026-10-01 收敛）：
--   * `opsmesh` 主库 —— 控制面主模块的版本化迁移 `internal/store/migrations/*.sql`
--     （启动时执行；有 schema_migrations 版本表 + sha256 checksum 校验 + 迁移锁）。
--   * 各微服务库 —— 服务自己的 schema：
--       - auth-svc / device-svc / task-svc：embed `internal/store/schema.sql`（各自 migration.go）；
--       - alert / config / log / gpu / portal / incident / runbook / autoscaler：内联 initSchema
--         （`services/<svc>/internal/store/mysql.go`）。
--
-- 为什么本文件不能含建表语句（2026-10-01 实测取证）：
--   它原是一份「单库版合并脚本」，含 43 张表的 DDL。这些副本与代码里的定义已经漂移，
--   而 MySQL 的 CREATE TABLE IF NOT EXISTS 会让**先落地的那一份静默胜出**，后到者不报错：
--     - `agents`：deploy 版 15 列（id/device_id/os/arch/…）vs 控制面迁移版
--       （agent_id/hostname/segment/tenant_id/addr/grpc_port/metrics_port/status/`load`/last_seen/secret）
--       ⇒ 控制面 `INSERT INTO agents (…)`（internal/store/sql_devices.go:83）Unknown column；
--     - `devices` / `ci_items` / `permissions` / `roles` / `users` / `tasks` 同类，
--       共 7 张表存在「只由代码 CREATE 定义、且没有任何 ALTER 兜底」的列，会永久缺失。
--   故一旦被挂载或执行，控制面/微服务就会因列不匹配而失败，且排查成本极高。
--   （compose 生产栈早已绕开：只挂 `scripts/init-databases.sql`，理由见 docker-compose.prod.yml:39-42。）
--
-- 保留本文件的原因：给「不走 compose、手工起库」的场景用
-- （deploy/docker/scripts/run-mysql-init.ps1）。它与 init-databases.sql 的分工是：
--   * compose：`opsmesh` 主库由 MYSQL_DATABASE/MYSQL_USER 环境变量建库并授权，
--     故 init-databases.sql 只补微服务独立库；
--   * 手工：没有该 env，故这里连 `opsmesh` 主库一起建。
--   两者的库集合必须与各服务 DSN（docker-compose.prod.yml 的 `*_SVC_DSN`）一一对应——
--   由 deploy/scripts/validate-deploy-assets.sh 第 13 节守着。

CREATE DATABASE IF NOT EXISTS opsmesh CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 微服务独立库（表名冲突规避：主库与微服务的 devices/users/agents/alerts/ci_items
-- 等表名相同但结构不一致，故必须分库）。
CREATE DATABASE IF NOT EXISTS opsmesh_device   CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_task     CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_alert    CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_config   CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_log      CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_incident CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_runbook  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 微服务库授权（opsmesh 业务账号；容器初始化时以 root 执行本脚本）。
GRANT ALL PRIVILEGES ON opsmesh_device.*   TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_task.*     TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_alert.*    TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_config.*   TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_log.*      TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_incident.* TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_runbook.*  TO 'opsmesh'@'%';
FLUSH PRIVILEGES;
