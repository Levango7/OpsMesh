-- 010_p1_slo_ticket.sql — P1 SLO 管理 + 工单管理领域持久化表
--
-- slos 表存储服务级别目标（按 id 唯一）。SLIs 以 JSON 数组存储在 slis 列。
-- tickets 表存储工单（按 id 唯一）。Tags 以 JSON 数组存储在 tags 列；
-- resolved_at 可空（NULL 表示未解决）。
-- CreateSLO / CreateTicket 用 INSERT ... ON DUPLICATE KEY UPDATE 做幂等 upsert。
-- 所有查询带 tenant_id 条件实现租户隔离。

CREATE TABLE IF NOT EXISTS slos (
    id           VARCHAR(64)  NOT NULL,
    tenant_id    VARCHAR(64)  NOT NULL DEFAULT 'default',
    name         VARCHAR(255) NOT NULL,
    description  TEXT,
    service_name VARCHAR(255),
    target       DOUBLE       NOT NULL DEFAULT 0,
    window_spec  VARCHAR(32),  -- 不用 "window"：MySQL 8.0 保留字（WINDOW 子句），裸用必报 1064
    slis         TEXT,
    created_at   DATETIME     NOT NULL,
    updated_at   DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_slos_tenant (tenant_id),
    INDEX idx_slos_service (tenant_id, service_name)
);

CREATE TABLE IF NOT EXISTS tickets (
    id             VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL DEFAULT 'default',
    title          VARCHAR(255) NOT NULL,
    description    TEXT,
    status         VARCHAR(32)  NOT NULL DEFAULT 'open',
    priority       VARCHAR(32)  NOT NULL DEFAULT 'medium',
    category       VARCHAR(32)  NOT NULL DEFAULT 'incident',
    assignee_id    VARCHAR(64),
    creator_id     VARCHAR(64),
    related_device VARCHAR(64),
    related_task   VARCHAR(64),
    tags           TEXT,
    created_at     DATETIME     NOT NULL,
    updated_at     DATETIME     NOT NULL,
    resolved_at    DATETIME,
    PRIMARY KEY (id),
    INDEX idx_tickets_tenant (tenant_id),
    INDEX idx_tickets_status (tenant_id, status),
    INDEX idx_tickets_priority (tenant_id, priority),
    INDEX idx_tickets_assignee (tenant_id, assignee_id),
    INDEX idx_tickets_created (tenant_id, created_at)
);