-- 022_gateway_routes.sql — API 网关路由规则持久化表（TD-66 P1）
--
-- 背景：`/gw/` 数据面的路由规则此前只存进程内 map，**重启全丢**（已配的路由变 404）。
-- 本表把路由变成持久化资源：SQL 后端下重启后经 ListGatewayRoutes(tenantID) 恢复。
--
-- 键：联合唯一 (tenant_id, id) —— 路由是租户级资源，与 gatewayState 的
-- tenantID -> ruleID 双层索引同语义；查询一律带 tenant_id，防跨租户读到他人路由。
--
-- timeout_ms 是本表与 extension.RouteRule 的新增字段（TD-66 P4 超时治理）一并落地：
-- 存量行 NULL 时由读取侧归一为默认 30s，不需要额外回填迁移。

CREATE TABLE IF NOT EXISTS gateway_routes (
    id               VARCHAR(64)   NOT NULL COMMENT '规则 ID（gw- 前缀随机串）',
    tenant_id        VARCHAR(64)   NOT NULL DEFAULT 'default' COMMENT '归属租户（隔离键）',
    name             VARCHAR(255)  NOT NULL DEFAULT '' COMMENT '规则名（展示用）',
    path_prefix      VARCHAR(512)  NOT NULL DEFAULT '' COMMENT '匹配的路径前缀（已含 /gw 剥前缀）',
    target_backend   VARCHAR(512)  NOT NULL DEFAULT '' COMMENT '后端地址 scheme://host:port（http|https）',
    methods          JSON          NULL COMMENT '允许的 HTTP 方法列表；NULL=全部方法',
    rate_limit_per_sec INT         NOT NULL DEFAULT 0 COMMENT '路由级令牌桶速率；0=不限流',
    timeout_ms       INT           NOT NULL DEFAULT 30000 COMMENT '单路由转发超时（TD-66 P4）',
    enabled          TINYINT(1)    NOT NULL DEFAULT 1 COMMENT '是否启用（禁用则不参与匹配）',
    created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    PRIMARY KEY (tenant_id, id),
    KEY idx_gateway_routes_tenant (tenant_id),
    KEY idx_gateway_routes_target (target_backend)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='API 网关路由规则（TD-66 P1 持久化）';
