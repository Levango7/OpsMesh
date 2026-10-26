-- 027_secret_access_log.sql — 密钥访问审计日志表
--
-- secret_access_logs 记录谁、何时、对哪个密钥做了什么操作（get/rotate/delete）。
-- 按 tenant_id/key_name/accessed_at 建立索引，支持租户隔离的分页查询。

CREATE TABLE IF NOT EXISTS secret_access_logs (
    id          VARCHAR(64)  PRIMARY KEY,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    key_name    VARCHAR(256) NOT NULL,
    accessed_by VARCHAR(128) NOT NULL DEFAULT '',
    action      VARCHAR(32)  NOT NULL DEFAULT 'get',
    accessed_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_secret_access_logs_tenant (tenant_id),
    INDEX idx_secret_access_logs_key (key_name),
    INDEX idx_secret_access_logs_time (accessed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
