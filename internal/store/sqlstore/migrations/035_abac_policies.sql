-- 035_abac_policies.sql — ABAC 策略持久化（批次47）
--
-- 背景：ABAC 策略此前只存在控制面进程内镜像，重启即丢（default-deny 静默生效），
-- 且多副本各自维护、创建/删除互不可见。本表把策略变成持久化资源：
-- 启动时全量装载，写穿时落库，5s 周期按 version 对齐其他副本的变更。
--
-- 键：(tenant_id, id) 联合主键。id 由控制面按 tenantID|action|subject|resource 生成，
-- 已内含租户分量；tenant_id 单独成列用于租户分区查询与隔离。
--
-- version：每次 upsert 自增（INSERT 首次为 1），created_at 更新时保持不变。

CREATE TABLE IF NOT EXISTS `abac_policies` (
    `tenant_id`  VARCHAR(128)  NOT NULL COMMENT '归属租户（隔离键）',
    `id`         VARCHAR(512)  NOT NULL COMMENT '策略 ID（tenantID|action|subject|resource）',
    `subject`    VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '主体匹配式（支持 * / prefix*）',
    `resource`   VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '资源匹配式（支持 * / prefix*）',
    `action`     VARCHAR(128)  NOT NULL DEFAULT '' COMMENT '动作匹配式（支持 * / prefix*）',
    `effect`     VARCHAR(16)   NOT NULL DEFAULT 'allow' COMMENT 'allow / deny（deny-overrides）',
    `version`    BIGINT        NOT NULL DEFAULT 1 COMMENT '写穿版本号（多副本刷新比对）',
    `created_at` DATETIME      NOT NULL COMMENT '创建时间（更新不变）',
    `updated_at` DATETIME      NOT NULL COMMENT '最后更新时间',
    PRIMARY KEY (`tenant_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='ABAC 策略（批次47 持久化）';
