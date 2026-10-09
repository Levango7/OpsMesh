-- 009_p03_services.sql — P0.3 服务发现领域持久化表
--
-- services 表存储注册的服务实例（按 service_id 唯一）。
-- RegisterService 用 INSERT ... ON DUPLICATE KEY UPDATE 做幂等 upsert。
-- Metadata 以 JSON 字符串存储在 metadata 列。

CREATE TABLE IF NOT EXISTS services (
    service_id      VARCHAR(64)  NOT NULL,
    tenant_id       VARCHAR(64)  NOT NULL,
    service_name    VARCHAR(255) NOT NULL,
    address         VARCHAR(255),
    port            INT,
    metadata        TEXT,
    status          VARCHAR(16)  NOT NULL DEFAULT 'healthy',
    last_heartbeat  DATETIME     NOT NULL,
    created_at      DATETIME     NOT NULL,
    PRIMARY KEY (service_id),
    INDEX idx_services_tenant (tenant_id),
    INDEX idx_services_name (tenant_id, service_name)
);