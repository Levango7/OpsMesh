-- 034_budgets.sql — AI 成本预算：TokenBudget + BudgetAlert 持久化（TD-97 ①）
--
-- 表结构说明：
--   - budgets：租户级 Token 预算（tenant_id 主键，upsert）。
--   - budget_alerts：预算告警历史（自增 id）。
--
-- 兼容性：CREATE TABLE IF NOT EXISTS 幂等。

CREATE TABLE IF NOT EXISTS budgets (
    tenant_id      VARCHAR(128) PRIMARY KEY,
    daily_limit    INT NOT NULL DEFAULT 0,
    monthly_limit  INT NOT NULL DEFAULT 0,
    used_today     INT NOT NULL DEFAULT 0,
    used_month     INT NOT NULL DEFAULT 0,
    alert_threshold DOUBLE NOT NULL DEFAULT 0.8,
    last_reset     DATETIME NULL,
    timezone       VARCHAR(64) NOT NULL DEFAULT 'UTC',
    month_reset    DATETIME NULL,
    alerted        TEXT NULL,
    updated_at     DATETIME
);

CREATE TABLE IF NOT EXISTS budget_alerts (
    id         BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id  VARCHAR(128) NOT NULL,
    type       VARCHAR(32) NOT NULL,
    threshold  DOUBLE NOT NULL DEFAULT 0,
    used       INT NOT NULL DEFAULT 0,
    limit_val  INT NOT NULL DEFAULT 0,
    timestamp  DATETIME NOT NULL,
    KEY idx_budget_alerts_tenant (tenant_id),
    KEY idx_budget_alerts_ts (timestamp)
);
