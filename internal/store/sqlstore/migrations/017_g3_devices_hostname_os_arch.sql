-- 017_g3_devices_hostname_os_arch.sql — devices/agents 补列正式迁移（fixup 前置化）
--
-- 背景（E2E-real 首次带 MySQL 起栈实测暴露）：
--   sql_devices.go 的 Register INSERT / Snapshot SELECT 引用 devices.hostname/os/arch，
--   sql.go applyLegacyColumnFixups 早已能补这些列——但 fixup 在 runMigrations **成功之后**
--   才执行（sql.go step 5）；当迁移链中途失败（如 015 保留字 1064）时 fixup 不跑，
--   控制面带着缺列 schema 继续运行，设备注册报 Error 1054 Unknown column。
--
-- 本迁移把这些列的真相固化进迁移链（fixup 注释自述"待后续转为正式迁移"，此即其转正）：
--   - devices.hostname/os/arch（设备元信息，agent 注册上报）
--   - agents.secret（gRPC 身份绑定 HMAC 密钥，此前仅 fixup 兜底）
--
-- 与 fixup 的顺序安全性：runMigrations 先应用本迁移（加列）→ step 5 fixup 的
-- alterColumnIfMissing 查 information_schema 发现列已存在即跳过——两者幂等兼容。
-- MySQL 8 不支持 ADD COLUMN IF NOT EXISTS；幂等由 schema_migrations 版本记录保证。
ALTER TABLE devices ADD COLUMN hostname VARCHAR(255);
ALTER TABLE devices ADD COLUMN os VARCHAR(64);
ALTER TABLE devices ADD COLUMN arch VARCHAR(32);
ALTER TABLE agents ADD COLUMN secret VARCHAR(64);
