-- 015_p6_tenant_apikey_plugin_billing.sql — P6 四个域持久化表
--
-- 本迁移为 Phase 6 四个领域建表：
--   1. tenants                       — 租户管理（域 12，全局共享，本身就是租户）
--   2. api_keys                      — API Key 管理（域 13，按 tenant_id 隔离）
--   3. plugins                       — 插件市场（域 14，全局共享）
--   4. billing_plans + subscriptions + invoices — 计费（域 15，计划全局共享，
--      订阅/账单按 ID 定位、List 按 tenant_id 过滤）
--
-- 设计要点：
--   - 全部 CREATE TABLE IF NOT EXISTS，幂等可重入；
--   - 主键 id VARCHAR(64)，由应用层 ID 生成函数填充：
--       randTenantID()（tenant-）/ randAPIKeyID()（apikey-）/ randPluginID()（plugin-）
--       / randBillingID("plan"|"sub"|"inv")（plan-/sub-/inv-）；
--   - tenants / plugins / billing_plans 全局共享，无 tenant_id 列；
--   - api_keys.tenant_id 默认 'default'，空 tenantID 时 List 返回全部供 ValidateKey 扫描；
--   - subscriptions / invoices 按 ID 全局唯一（Get/Delete 不带 tenant_id），List 按 tenant_id 过滤；
--   - JSON 列（tenants.quota/usage, api_keys.scopes, billing_plans.features/resource_limits,
--     invoices.items）以 TEXT 存储，应用层用 encoding/json 序列化/反序列化；
--   - api_keys.expires_at / last_used_at 可空（NULL 表示永不过期/未使用）；
--   - bool 列（api_keys.enabled 默认 1, plugins.installed/enabled 默认 0）为 TINYINT(1)；
--   - 时间戳一律 DATETIME，应用层写入 time.Now().UTC()。

-- ============================================================================
-- 域 12: Tenant 租户管理（全局共享，无 tenant_id）
-- ============================================================================

CREATE TABLE IF NOT EXISTS tenants (
    id           VARCHAR(64)  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    display_name VARCHAR(255),
    status       VARCHAR(32)  NOT NULL DEFAULT 'active',
    quota        TEXT,                    -- JSON: TenantQuota
    usage_data   TEXT,                    -- JSON: ResourceUsage（列名不用 usage：MySQL 8.0 保留字）
    created_at   DATETIME     NOT NULL,
    updated_at   DATETIME     NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_tenants_name (name),
    KEY idx_tenants_status (status)
);

-- ============================================================================
-- 域 13: APIKey API Key 管理（按 tenant_id 隔离）
-- ============================================================================

CREATE TABLE IF NOT EXISTS api_keys (
    id                 VARCHAR(64)  NOT NULL,
    tenant_id          VARCHAR(64)  NOT NULL DEFAULT 'default',
    name               VARCHAR(255) NOT NULL,
    key_hash           VARCHAR(128) NOT NULL,           -- SHA-256 hash（避免与 SQL 关键字 key 冲突）
    scopes             TEXT,                            -- JSON: []string
    rate_limit_per_sec INT          NOT NULL DEFAULT 0,
    expires_at         DATETIME,                       -- NULL 表示永不过期
    last_used_at       DATETIME,                       -- NULL 表示未使用
    enabled            TINYINT(1)   NOT NULL DEFAULT 1,
    created_at         DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_apikeys_tenant (tenant_id),
    KEY idx_apikeys_hash (key_hash),
    KEY idx_apikeys_enabled (tenant_id, enabled)
);

-- ============================================================================
-- 域 14: Plugin 插件市场（全局共享，无 tenant_id）
-- ============================================================================

CREATE TABLE IF NOT EXISTS plugins (
    id           VARCHAR(64)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    version      VARCHAR(64),
    description  TEXT,
    author       VARCHAR(255),
    type         VARCHAR(32)  NOT NULL DEFAULT 'agent',
    download_url TEXT,
    checksum     VARCHAR(128),
    installed    TINYINT(1)   NOT NULL DEFAULT 0,
    enabled      TINYINT(1)   NOT NULL DEFAULT 0,
    created_at   DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_plugins_type (type),
    KEY idx_plugins_name (name)
);

-- ============================================================================
-- 域 15: Billing 计费（billing_plans 全局共享；subscriptions/invoices 按 ID 定位、List 按 tenant_id 过滤）
-- ============================================================================

CREATE TABLE IF NOT EXISTS billing_plans (
    id              VARCHAR(64)  NOT NULL,
    name            VARCHAR(255) NOT NULL,
    price           INT          NOT NULL DEFAULT 0,
    interval_spec   VARCHAR(32)  NOT NULL DEFAULT 'monthly',  -- 列名不用 interval：MySQL 8.0 保留字
    features        TEXT,                    -- JSON: []string
    resource_limits TEXT,                    -- JSON: TenantQuota
    created_at      DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_billing_plans_interval (interval_spec)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id         VARCHAR(64)  NOT NULL,
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default',
    plan_id    VARCHAR(64)  NOT NULL,
    status     VARCHAR(32)  NOT NULL DEFAULT 'active',
    started_at DATETIME     NOT NULL,
    expires_at DATETIME     NOT NULL,
    created_at DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_subscriptions_tenant (tenant_id),
    KEY idx_subscriptions_plan (tenant_id, plan_id),
    KEY idx_subscriptions_status (tenant_id, status)
);

CREATE TABLE IF NOT EXISTS invoices (
    id              VARCHAR(64)  NOT NULL,
    tenant_id       VARCHAR(64)  NOT NULL DEFAULT 'default',
    subscription_id VARCHAR(64)  NOT NULL,
    amount          INT          NOT NULL DEFAULT 0,
    period_start    DATETIME     NOT NULL,
    period_end      DATETIME     NOT NULL,
    status          VARCHAR(32)  NOT NULL DEFAULT 'pending',
    items           TEXT,                    -- JSON: []InvoiceItem
    created_at      DATETIME     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_invoices_tenant (tenant_id),
    KEY idx_invoices_subscription (tenant_id, subscription_id),
    KEY idx_invoices_status (tenant_id, status),
    KEY idx_invoices_created (tenant_id, created_at)
);