-- 011_p2_argocd_pipeline_traffic.sql — P2 三个域持久化表
--
-- 本迁移为 Phase 2 三个领域建表：
--   1. argocd_apps      — ArgoCD 应用管理（域 3）
--   2. pipeline_templates + pipeline_runs — CI/CD 流水线（域 4）
--   3. traffic_policies — 流量治理策略（域 5）
--
-- 设计要点：
--   - 全部 CREATE TABLE IF NOT EXISTS，幂等可重入；
--   - 主键 id VARCHAR(64)，由应用层 ID 生成函数填充（randArgoCDID/randPipelineID/randRunID/randTrafficID）；
--   - tenant_id 默认 'default'，所有 List 走 WHERE tenant_id=? 实现租户隔离；
--   - JSON 列（pipeline_templates.parameters / pipeline_runs.parameters / traffic_policies.canary_weights）
--     以 TEXT 存储，应用层用 encoding/json 序列化/反序列化；
--   - pipeline_runs.started_at / finished_at 可空（NULL 表示未启动/未结束）；
--   - 时间戳一律 DATETIME，应用层写入 time.Now().UTC()。

-- ============================================================================
-- 域 3: ArgoCD 应用管理
-- ============================================================================

CREATE TABLE IF NOT EXISTS argocd_apps (
    id              VARCHAR(64)  NOT NULL,
    tenant_id       VARCHAR(64)  NOT NULL DEFAULT 'default',
    name            VARCHAR(255) NOT NULL,
    namespace       VARCHAR(255),
    repo_url        TEXT,
    path            VARCHAR(512),
    target_revision VARCHAR(64),
    cluster_url     TEXT,
    sync_policy     VARCHAR(32)  NOT NULL DEFAULT 'manual',
    status          VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    health_status   VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    created_at      DATETIME     NOT NULL,
    updated_at      DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_argocd_tenant (tenant_id),
    INDEX idx_argocd_name (tenant_id, name)
);

-- ============================================================================
-- 域 4: CI/CD 流水线（模板 + 运行记录）
-- ============================================================================

CREATE TABLE IF NOT EXISTS pipeline_templates (
    id          VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL DEFAULT 'default',
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    type        VARCHAR(32)  NOT NULL DEFAULT 'tekton',
    yaml        TEXT,
    parameters  TEXT,
    created_at  DATETIME     NOT NULL,
    updated_at  DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_pipeline_tpl_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id            VARCHAR(64)  NOT NULL,
    tenant_id     VARCHAR(64)  NOT NULL DEFAULT 'default',
    template_id   VARCHAR(64)  NOT NULL,
    template_name VARCHAR(255),
    status        VARCHAR(32)  NOT NULL DEFAULT 'pending',
    parameters    TEXT,
    logs          LONGTEXT,
    started_at    DATETIME,
    finished_at   DATETIME,
    created_at    DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_pipeline_run_tenant (tenant_id),
    INDEX idx_pipeline_run_template (tenant_id, template_id),
    INDEX idx_pipeline_run_status (tenant_id, status)
);

-- ============================================================================
-- 域 5: 流量治理策略
-- ============================================================================

CREATE TABLE IF NOT EXISTS traffic_policies (
    id             VARCHAR(64)  NOT NULL,
    tenant_id       VARCHAR(64)  NOT NULL DEFAULT 'default',
    name            VARCHAR(255) NOT NULL,
    service_name    VARCHAR(255),
    type            VARCHAR(32)  NOT NULL,
    canary_weights  TEXT,
    mirror_percent  INT          NOT NULL DEFAULT 0,
    timeout         VARCHAR(32),
    retries         INT          NOT NULL DEFAULT 0,
    retry_timeout   VARCHAR(32),
    max_conns       INT          NOT NULL DEFAULT 0,
    max_requests    INT          NOT NULL DEFAULT 0,
    status          VARCHAR(32)  NOT NULL DEFAULT 'inactive',
    created_at      DATETIME     NOT NULL,
    updated_at      DATETIME     NOT NULL,
    PRIMARY KEY (id),
    INDEX idx_traffic_tenant (tenant_id),
    INDEX idx_traffic_service (tenant_id, service_name),
    INDEX idx_traffic_status (tenant_id, status)
);