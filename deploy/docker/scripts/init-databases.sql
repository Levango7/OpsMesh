-- OpsMesh 生产部署 —— 仅建库与授权（不含任何建表语句）
--
-- 挂载点：/docker-entrypoint-initdb.d/ ，由官方 mysql 镜像在「数据目录为空」的首次
-- 初始化时以 root 自动执行。已有数据卷不会重跑（属预期：建库是一次性动作）。
--
-- 为什么只建库、不建表：
--   1. 微服务各自在启动时用自身 schema.sql / initSchema 建表（device/task/alert/
--      config/log 均如此），建表职责归服务，重复定义必然产生漂移。
--   2. `opsmesh` 主库的 users/roles/devices/agents/ci_items 等表，控制面主模块与
--      auth/gpu/portal 等微服务的定义并不一致。若在此处抢先建表，控制面启动时
--      CREATE TABLE IF NOT EXISTS 会静默让位给结构不同的既有表，随后查询/写入
--      因列不匹配而失败——这类故障排查成本极高，必须避免。
--      deploy/docker/scripts/init-mysql.sql 是「单库版」合并脚本，含全量表 DDL，
--      不适用于本部署形态，切勿挂载到 initdb.d。
--
-- 授权范围：仅授予本次部署所需的 6 个库。opsmesh 主库由 MYSQL_DATABASE/MYSQL_USER
-- 环境变量在建库阶段自动授权，此处补齐微服务独立库（否则微服务连库即
-- "Access denied for user 'opsmesh'"）。

CREATE DATABASE IF NOT EXISTS opsmesh_device CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_task   CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_alert  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_config CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS opsmesh_log    CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

GRANT ALL PRIVILEGES ON opsmesh_device.* TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_task.*   TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_alert.*  TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_config.* TO 'opsmesh'@'%';
GRANT ALL PRIVILEGES ON opsmesh_log.*    TO 'opsmesh'@'%';
FLUSH PRIVILEGES;
