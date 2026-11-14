-- 031_lifecycle_jobs.sql — K8s 生命周期作业持久化（补别线 d07b2d6 缺失的建表迁移）
--
-- 背景：internal/store/sqlstore/sql.go 已有 LifecycleJob 结构与 5 个读写方法
-- （Create/Get/Update/List/GetActiveLifecycleJob），但 migrations 里**没有** lifecycle_jobs
-- 的 CREATE TABLE ⇒ ①门禁 TestEveryWideScanSiteIsJudgedOrExempted 因「就近归属表=lifecycle_jobs
-- （归属错位或无表）」红灯（main 自 846208a 连红的另一半）；②真库上 CreateLifecycleJob
-- 运行时直接 no such table——升级/回滚/证书轮换的作业记录一处都存不下来。
--
-- 列口径与 sql.go 读写面逐列对齐（CreateLifecycleJob 的 INSERT 列集 + GetLifecycleJob 的
-- SELECT 列集）：
--   - error / lock_owner：读侧 Scan 进普通 string ⇒ 必须 NOT NULL；error 不在 INSERT 列集
--     （建作业时还没有失败原因）⇒ DEFAULT ''（TEXT 不能带字面默认值，故用 VARCHAR）；
--   - spec_json / result_json：读侧用 sql.NullString 承接 ⇒ 可空（JSON 列）；
--   - version：乐观锁计数，INSERT 不带（写死 0）⇒ NOT NULL DEFAULT 0；
--   - progress / current_step：INSERT 不带 ⇒ DEFAULT。

CREATE TABLE IF NOT EXISTS lifecycle_jobs (
    id           VARCHAR(64)   NOT NULL COMMENT '作业 ID',
    cluster_id   VARCHAR(64)   NOT NULL COMMENT '目标集群',
    kind         VARCHAR(32)   NOT NULL COMMENT '作业类型：upgrade | rollback | cert-rotate',
    tenant_id    VARCHAR(64)   NOT NULL DEFAULT 'default' COMMENT '归属租户（隔离键）',
    phase        VARCHAR(32)   NOT NULL COMMENT 'pending/precheck/running/.../completed/failed/cancelled',
    progress     INT           NOT NULL DEFAULT 0 COMMENT '进度百分比',
    current_step VARCHAR(128)  NOT NULL DEFAULT '' COMMENT '当前步骤',
    spec_json    JSON          NULL COMMENT '作业规格（创建时快照）',
    result_json  JSON          NULL COMMENT '执行结果（终态时写入）',
    error        VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '失败原因（读侧 Scan 进 string，须非 NULL）',
    lock_owner   VARCHAR(128)  NOT NULL DEFAULT '' COMMENT '乐观锁持有者（读侧 Scan 进 string，须非 NULL）',
    version      INT           NOT NULL DEFAULT 0 COMMENT '乐观锁版本号（UPDATE 时 +1）',
    created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    PRIMARY KEY (id),
    KEY idx_lifecycle_jobs_cluster_kind_created (cluster_id, kind, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='K8s 生命周期作业（升级/回滚/证书轮换）';
