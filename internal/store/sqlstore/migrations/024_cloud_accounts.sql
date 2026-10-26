-- 024_cloud_accounts.sql — Wave 2-B 云账号与云资源表（OpsMesh 多云基础设施）。
--
-- cloud_accounts：控制面接入云厂商（aliyun/aws/azure/tencent/huawei）的
-- 凭据引用与同步元信息。明文凭据不落库，credentials_ref 仅指向 SecretItem。
-- cloud_resources：由云账号同步/发现落库，按 (cloud_account_id, resource_type,
-- resource_id) 去重，按 tenant_id 隔离。
CREATE TABLE IF NOT EXISTS cloud_accounts (
    id              VARCHAR(64) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL DEFAULT 'default',
    workspace_id    VARCHAR(64) NOT NULL DEFAULT '',
    name            VARCHAR(128) NOT NULL,
    provider        VARCHAR(32) NOT NULL,
    region          VARCHAR(64) NOT NULL DEFAULT '',
    auth_type       VARCHAR(32) NOT NULL,
    credentials_ref VARCHAR(256) NOT NULL DEFAULT '',
    status          VARCHAR(32) NOT NULL DEFAULT 'active',
    sync_status     VARCHAR(32) NOT NULL DEFAULT 'pending',
    last_sync_at    TIMESTAMP NULL DEFAULT NULL,
    discovered_count INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    created_by      VARCHAR(128) NOT NULL DEFAULT '',
    INDEX idx_cloud_accounts_tenant (tenant_id),
    INDEX idx_cloud_accounts_provider (provider),
    INDEX idx_cloud_accounts_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='云账号（Wave 2-B 多云基础设施）';

CREATE TABLE IF NOT EXISTS cloud_resources (
    id              VARCHAR(64) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL DEFAULT 'default',
    cloud_account_id VARCHAR(64) NOT NULL,
    provider        VARCHAR(32) NOT NULL,
    region          VARCHAR(64) NOT NULL DEFAULT '',
    resource_type   VARCHAR(64) NOT NULL,
    resource_id     VARCHAR(256) NOT NULL,
    resource_name   VARCHAR(256) NOT NULL DEFAULT '',
    status          VARCHAR(32) NOT NULL DEFAULT 'unknown',
    tags            TEXT NOT NULL,
    private_ip      VARCHAR(64) NOT NULL DEFAULT '',
    public_ip       VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_cloud_resources_tenant (tenant_id),
    INDEX idx_cloud_resources_account (cloud_account_id),
    INDEX idx_cloud_resources_type (resource_type),
    UNIQUE KEY uk_cloud_resources_account_res (cloud_account_id, resource_type, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='云资源（Wave 2-B 多云基础设施）';
