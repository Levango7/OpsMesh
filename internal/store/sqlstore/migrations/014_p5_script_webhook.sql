-- 014_p5_script_webhook.sql — P5 自定义脚本 + Webhook 两域持久化表
--
-- scripts 表存储自定义脚本（按 id 唯一）。enabled 为 TINYINT(1)（默认 1，新建即启用，
-- 与 memory 实现一致，避免零值 false 导致 execute 全部被拒）。
-- script_executions 表存储脚本执行记录（按 id 唯一）。finished_at 可空
-- （NULL 表示未结束），用 sql.NullTime 读写。
-- webhooks 表存储 Webhook 配置（按 id 唯一）。events / headers 为 JSON 文本列
-- （events: []string，headers: map[string]string）。enabled 为 TINYINT(1)（默认 1）。
-- webhook_deliveries 表存储 Webhook 投递记录（按 id 唯一）。
-- CreateScript / CreateWebhook 用 INSERT ... ON DUPLICATE KEY UPDATE 做幂等 upsert。
-- 所有查询带 tenant_id 条件实现租户隔离。

CREATE TABLE IF NOT EXISTS scripts (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    name        VARCHAR(255) NOT NULL,
    language    VARCHAR(32)  NOT NULL DEFAULT 'shell',
    content     LONGTEXT,
    params      TEXT,
    timeout_sec INT          NOT NULL DEFAULT 0,
    enabled     TINYINT(1)   NOT NULL DEFAULT 1,
    created_at  DATETIME     NOT NULL,
    updated_at  DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_scripts_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS script_executions (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    script_id   VARCHAR(64)  NOT NULL,
    device_id   VARCHAR(64),
    status      VARCHAR(32)  NOT NULL DEFAULT 'pending',
    stdout      LONGTEXT,
    stderr      LONGTEXT,
    started_at  DATETIME     NOT NULL,
    finished_at DATETIME,
    PRIMARY KEY (id),
    INDEX idx_script_exec_tenant (tenant_id),
    INDEX idx_script_exec_script (tenant_id, script_id),
    INDEX idx_script_exec_started (tenant_id, started_at)
);

CREATE TABLE IF NOT EXISTS webhooks (
    id                 VARCHAR(64)  NOT NULL,
    tenant_id          VARCHAR(64)  NOT NULL DEFAULT 'default',
    name               VARCHAR(255) NOT NULL,
    url                TEXT,
    events             TEXT,
    headers            TEXT,
    body_template      TEXT,
    enabled            TINYINT(1)   NOT NULL DEFAULT 1,
    retry_count        INT          NOT NULL DEFAULT 0,
    retry_interval_sec INT          NOT NULL DEFAULT 0,
    created_at         DATETIME     NOT NULL,
    updated_at         DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_webhooks_tenant (tenant_id),
    INDEX idx_webhooks_enabled (tenant_id, enabled)
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id           VARCHAR(64)  NOT NULL,
    tenant_id    VARCHAR(64)  NOT NULL DEFAULT 'default',
    webhook_id   VARCHAR(64)  NOT NULL,
    event        VARCHAR(255),
    payload      LONGTEXT,
    status_code  INT          NOT NULL DEFAULT 0,
    response     LONGTEXT,
    error        TEXT,
    delivered_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_webhook_deliv_tenant (tenant_id),
    INDEX idx_webhook_deliv_webhook (tenant_id, webhook_id),
    INDEX idx_webhook_deliv_delivered (tenant_id, delivered_at)
);