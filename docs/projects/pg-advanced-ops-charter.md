# PostgreSQL 高级运维立项书（扩展 / 自定义类型 / MVCC 与表膨胀 / 分区 / FDW / 复制槽 / 逻辑复制 / HA / 监控 + PostGIS）

> 本文按 `docs/projects/README.md` 的统一模板编写。事实以 `file:line` 或文档引用标注；
> 未落地的设计参数明确写「待复核」，不把提案写成现状。Owner 决策 required；状态 = 已立项。

## 1. 一句话定义

为 OpsMesh 控制面补齐 **PostgreSQL 高级运维能力面**：扩展生命周期、自定义类型、MVCC/表膨胀诊断与维护、
分区表、外部数据封装器（FDW）、复制槽、逻辑复制、HA 工具（Patroni/repmgr/pg_auto_failover）、
监控（pg_stat_* / exporter）以及 GIS（PostGIS）；执行类走 shell 模板下发 agent，查询类走结构化 API，
与现有 `db_ops.go` / `db_extended_ops.go` 能力划分原则一致。

## 2. 背景与问题证据

### 2.1 现有能力只覆盖部署/启停/实例

| 事实 | 证据 |
|---|---|
| 数据库运维主入口：部署、启停重启、实例 create/drop/list | `internal/controlplane/db_ops.go` `handleDBDeploy` / `handleDBAction` / `handleDBInstance` |
| 扩展数据库（含 TimescaleDB）仅覆盖 status/deploy/backup/restore/hypertable | `internal/controlplane/db_extended_ops.go` `handleDBTimescaleDB` |
| 路由注册：`registerDBRoutes` / `registerDBExtendedRoutes` | `internal/controlplane/db_ops.go:609`；`internal/controlplane/db_extended_ops.go:516`；`internal/controlplane/server_lifecycle.go:248-249` |
| 能力划分原则：执行类走 shell/模板，查询类走结构化 API；二进制/大文件传输与高风险需审批操作单独写能力 | `internal/controlplane/db_ops.go` 头注释 |

### 2.2 高级运维缺口

| 缺口 | 后果 |
|---|---|
| 扩展生命周期（install/upgrade/drop/info/list）无统一 handler | 运维需登录节点手工 `CREATE EXTENSION`，无法审计与回滚 |
| MVCC/bloat 诊断无结构化入口 | 表膨胀、dead tuples、freeze age 无法定时观测 |
| 分区 / FDW / 逻辑复制 / 复制槽无模板 | HA 与数据集成场景只能手写脚本，注入风险高 |
| HA 工具（Patroni/repmgr）无收口 | 故障切换不可观测、不可审计 |
| PostGIS 空间能力无入口 | GIS 场景无法纳入统一运维面 |

## 3. 目标（可验证）

1. **扩展生命周期**：`POST /api/v1/db/postgresql/extensions` 支持 `list-installed` / `list-available` / `install` / `upgrade` / `drop` / `info`。
2. **MVCC/bloat 维护**：`POST /api/v1/db/postgresql/mvcc` 支持 `bloat-estimate` / `vacuum` / `vacuum-full` / `analyze` / `autovacuum-settings` / `freeze-age` / `reindex` / `dead-tuples`。
3. **严格校验**：action 与扩展名白名单；库/表/模式名走既有 `opsDBName` 或等价白名单；版本号走版本白名单；所有下发值经 `shellQuote`。
4. **审计**：每次执行写 `proto.AuditEvent`，含 tenant/user/action/target/detail。
5. **PostGIS**：作为扩展生命周期的首个落地扩展，提供安装/升级/信息查询模板与 GIS 常用诊断（版本、空间参考、几何列）。

## 4. 非目标

- 不做 PostgreSQL 内核源码修改或自定义 C 扩展编译链路。
- 不做全量 SQL 客户端/ORM 替代；自由 SQL 仍走现有 SQL 面。
- 不做跨集群联邦复制编排（仅单集群内逻辑复制模板）。
- 不替代 `db_ops.go` 的部署/启停/实例能力；本项仅新增高级运维面。

## 5. 工作分解与里程碑

| 阶段 | 交付物 | 退出判据 | 预估 |
|---|---|---|---|
| **M1 扩展生命周期** | `internal/controlplane/db_pg_ext_ops.go`；handler `handleDBPGExtensions`；路由注册 | 6 个 action 可下发；注入用例通过 | 2~3 人日 |
| **M2 MVCC/bloat 维护** | `internal/controlplane/db_pg_mvcc_ops.go`；handler `handleDBPGMVCC`；路由注册 | 8 个 action 可下发；注入用例通过 | 2~3 人日 |
| **M3 分区 / FDW / 复制槽 / 逻辑复制** | 新增 action 或独立 handler（待 owner 拍板） | 模板可执行、可审计 | 3~5 人日 |
| **M4 HA 工具 / 监控 / PostGIS** | Patroni/repmgr/pg_auto_failover 模板；pg_stat_* 诊断；PostGIS 安装与空间诊断 | 验收脚本通过 | 3~5 人日 |

## 6. 验收标准 DoD

### 6.1 可执行命令

```bash
go build ./...
go vet ./internal/controlplane/...
gofmt -l internal/controlplane
go test ./internal/controlplane/ -run "DBPGExt|DBPGMVCC" -count=1
go test ./internal/controlplane/ -count=1
```

### 6.2 功能判据

- 未知 action ⇒ 400；缺失 deviceID ⇒ 400；跨租户 ⇒ 403；合法 ⇒ 202 + taskID。
- 注入载荷（shell 元字符）对白名单字段一律 400；自由值字段必须经 `shellQuote` 包裹。
- 每次成功下发写审计事件。

### 6.3 监控/PostGIS 判据（M4）

- PostGIS 版本与空间参考查询可下发；几何列诊断可下发。
- HA 工具状态查询可下发；所有脚本可审计。

## 7. 依赖与前置条件

| 依赖 / 前置 | 现状 | 需要谁 | 阻塞里程碑 |
|---|---|---|---|
| `psql` / `pg_ctl` 在目标节点可用 | 运维节点标准安装 | 运维 | M1 |
| `internal/controlplane` 现有 `shellQuote` / `opsDBName` / `opsSQLString` | 已落地 | 开发 | M1/M2 |
| PostGIS 包（`postgis` / `postgis_topology`） | 目标库安装 | 运维 | M4 |
| HA 工具（Patroni/repmgr） | 节点安装 | 运维 | M4 |
| Owner 对 M3/M4 范围与排期拍板 | 待批准 | owner | M3 |

需要 owner 明确决策的项：

1. M3（分区/FDW/复制槽/逻辑复制）是否在本立项内交付，还是拆分为独立 charter。
2. HA 工具支持矩阵（Patroni vs repmgr vs pg_auto_failover）。
3. PostGIS 是否作为默认扩展模板随 M1 交付，还是延后到 M4。
4. 审批门禁：`vacuum-full` / `reindex` / 扩展 drop 是否要求管理员审批。

## 8. 风险与缓解（含回滚）

| 风险 | 触发信号 | 缓解 | 回滚 |
|---|---|---|---|
| 注入载荷进入 shell 脚本 | 脚本含未转义元字符 | 白名单 + `shellQuote`；注入用例进门禁 | 关闭对应 handler |
| `VACUUM FULL` / `REINDEX` 锁表 | 业务超时 | 审批门禁 + 低峰执行 + 文档 | 取消任务 |
| 扩展升级不兼容 | 应用报错 | `info` 先行 + 备份 + 回滚版本 | `drop` + 重装旧版 |
| 跨租户下发 | device 租户不一致 | device 租户校验 ⇒ 403 | 无（防越权） |

**全局回滚**：移除路由注册或关闭 feature flag 即可；不涉及数据迁移。

## 9. 工作量估算（人日 + 关键路径）

| 阶段 | 人日 |
|---|---|
| M1 扩展生命周期 | 2~3 |
| M2 MVCC/bloat 维护 | 2~3 |
| M3 分区/FDW/复制槽/逻辑复制 | 3~5 |
| M4 HA/监控/PostGIS | 3~5 |
| **合计** | **10~16 人日** |

- **关键路径**：M1 的严格校验与注入守卫 → M4 的 HA/PostGIS 模板。
- **不确定性**：HA 工具矩阵与 PostGIS 安装环境待 owner 拍板。

## 10. 状态与决策记录

**当前状态：已立项**
**负责人：待定**

| 日期 | 决策 / 事件 | 出处 |
|---|---|---|
| 2026-11-21 | 立项提交，状态「已立项」，owner 决策 required | 本文 |
| 待定 | M3/M4 范围、HA 矩阵、PostGIS 交付时点、审批门禁 | 本文 §7 |
EOF