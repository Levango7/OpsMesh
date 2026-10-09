-- 017_g3_devices_hostname_os_arch.down.sql — 017 的回滚脚本
--
-- 反向操作：删除 devices.hostname/os/arch 与 agents.secret 四列。
-- 执行方式：本仓无自动回滚执行器（migrationFiles 显式跳过 .down.sql 文件），
--   运维须按版本倒序手工执行，见 docs/operations.md「schema 迁移与回滚」。
-- 风险（严重）：
--   - agents.secret 是 gRPC agent 身份绑定的 HMAC 密钥列，删除后 agent 身份校验
--     依赖的密钥缓存无法回源查询，需回退到 --require-auth=false 或恢复备份；
--   - devices.hostname/os/arch 删除后设备列表/详情的元信息列缺失（查询报 1054）。
--   这四列同时被 applyLegacyColumnFixups 兜底补列——回滚后若继续运行当前版本
--   二进制，下次启动会被重新补上。彻底回滚须「回退二进制 + 回滚 schema」同时进行。
ALTER TABLE devices DROP COLUMN arch;
ALTER TABLE devices DROP COLUMN os;
ALTER TABLE devices DROP COLUMN hostname;
ALTER TABLE agents DROP COLUMN secret;
