-- 007_p03_secrets.sql — P0.3 密钥管理领域持久化表
--
-- secrets 表存储密钥的全部版本（按 tenant_id + key_name + version 唯一）。
-- GetSecret 通过 MAX(version) 子查询定位当前版本。
-- 生产环境 value 列须应用层加密（KMS/信封加密），DBA 不可见明文。

CREATE TABLE IF NOT EXISTS secrets (
    tenant_id   VARCHAR(64)  NOT NULL,
    key_name    VARCHAR(255) NOT NULL,
    version     INT          NOT NULL,
    value       TEXT,
    key_type    VARCHAR(32)  NOT NULL DEFAULT 'passphrase',
    created_at  DATETIME     NOT NULL,
    updated_at  DATETIME     NOT NULL,
    PRIMARY KEY (tenant_id, key_name, version),
    INDEX idx_secrets_tenant (tenant_id)
);