-- 023_task_records.sql — 批量执行 / 灰度发布记录持久化（TD-60 task 域 R6）
--
-- 背景：这两份状态此前**只在进程内存**（controlplane server_batch.go 的 batchStore，文件头自述
-- "仅内存索引（重启后丢失）"）。任务实例本身在 tasks 表持久化，丢的是**批次聚合视图**：
-- 重启后用户手里的 batchID/canaryID 查不到（404）。切流后两侧各有内存索引 ⇒ 同一批次的
-- "创建"与"查询"落到不同侧/不同副本也查不到。
--
-- **为什么只有 5 列**（设计取舍，见 model/task_record.go 头注释）：这两份记录只按 ID 整条读写，
-- 没有按状态/时间筛选的消费方；逐字段建列等于把控制面内部 20 多个字段抄一遍并强制两边同步演进
-- （必然漂移）。故：身份与租户成列（供隔离与索引），**内容整段进 payload**。
-- payload 是控制面内部结构的序列化，不是对外契约、不承诺跨版本稳定；读侧解不出按"记录不可用"处理。
--
-- 键：联合唯一 (tenant_id, kind, id)。kind 进主键而不是建两张表：两类记录**读写语义完全相同**
-- （整条 upsert / 按 ID 取），分表只会让同一套代码写两遍。将来若某一类长出独立查询面，
-- 再按需把它拆出去。

CREATE TABLE IF NOT EXISTS task_records (
    kind       VARCHAR(16)  NOT NULL COMMENT '记录类型：batch | canary',
    id         VARCHAR(64)  NOT NULL COMMENT '记录 ID（batch- / canary- 前缀）',
    tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'default' COMMENT '归属租户（隔离键）',
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    payload    JSON         NOT NULL COMMENT '记录内容（控制面内部结构序列化；非对外契约）',
    PRIMARY KEY (tenant_id, kind, id),
    KEY idx_task_records_kind_created (kind, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='批量/灰度记录（TD-60 task 域 R6 持久化）';
