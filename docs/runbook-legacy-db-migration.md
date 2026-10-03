# 存量库升级手册：TD-60 转正相关列（2026-10-03）

> 适用场景：**存量 MySQL 卷**从 ≤0.10.0 升到 0.11.0+ 时，转正三域所需的两张新表与一个新列不会自动建——
> `deploy/docker/scripts/init-mysql.sql` 只在**全新卷初始化**时执行一次（docker-entrypoint 的
> `/docker-entrypoint-initdb.d` 语义）。存量卷必须手工补齐，否则：
> - `incident-svc`（storeType=sql）启动时自举建表会补上自己的表（服务内 initSchema），但
>   **旧 incidents 表不会有 occurred_at 列**（见下"已知守卫"）；
> - `runbook-svc`（storeType=sql）依赖 `opsmesh_runbook` 库——**该库不存在**时连接直接失败，
>   启动即 fail-fast（这是有意的设计：声明 sql 却跑内存 = 重启丢数据）。
>
> 本手册为独立文档而非并入 `docs/operations.md`：2026-10-03 时该文件处于另一条工作线的
> 进行中现场，按纪律不碰；合并点登记在 `docs/roadmap-2026-10-03.md` 批次 A3。

## 0. 前置检查

```bash
# 1) 确认当前卷与版本
docker exec opsmesh-mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e "SHOW DATABASES LIKE 'opsmesh%';"

# 2) 确认 incident 表是否缺 occurred_at（v0.10.0 建的老表会缺）
docker exec opsmesh-mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" opsmesh -e \
  "SHOW COLUMNS FROM incidents LIKE 'occurred_at';"
```

## 1. 补建 runbook 库（必须）

`init-databases.sql` 已含该库定义，但存量卷没执行过。手工补（与脚本同款字符集与授权）：

```sql
CREATE DATABASE IF NOT EXISTS opsmesh_runbook CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
GRANT ALL PRIVILEGES ON opsmesh_runbook.* TO 'opsmesh'@'%';
-- incident 库（若 0.10.0 之前已部署 incident-svc 则已存在，IF NOT EXISTS 幂等）
CREATE DATABASE IF NOT EXISTS opsmesh_incident CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
GRANT ALL PRIVILEGES ON opsmesh_incident.* TO 'opsmesh'@'%';
FLUSH PRIVILEGES;
```

`runbooks` / `runbook_executions` 两张表**不必手工建**：`runbook-svc` 启动时 `initSchema`
幂等自举（CREATE TABLE IF NOT EXISTS）。

## 2. 补 occurred_at 列（若上面第 2 步无输出）

```sql
ALTER TABLE incidents
  ADD COLUMN occurred_at DATETIME NULL COMMENT '最早已知故障发生时刻（MTTD 起点，可空=未知不计入）'
  AFTER tags;
```

- 列可空、历史行不填 → MTTD 统计自动跳过旧事故（`mttdCount` 只计有值的），无需回填。
- 已建列则跳过（MySQL 1060 duplicate column）。

## 3. 服务侧开关（compose 已给默认值，按需覆盖 .env）

```bash
INCIDENT_STORE_TYPE=sql    # 已有默认
RUNBOOK_STORE_TYPE=sql     # 新增：runbook 重启不丢数据的转正前置
```

## 4. 验收

```bash
# 1) 两服务的健康端点（compose healthcheck 已探 /api/v1/health）
curl -fsS http://127.0.0.1:8104/api/v1/health   # incident
curl -fsS http://127.0.0.1:8110/api/v1/health   # runbook

# 2) 重启后数据仍在（runbook 持久化的真实验收）
#    建一条 runbook → 重启容器 → 仍在，才算转正持久化生效

# 3) 集合形状巡检（新门禁，顺带确认转正域无 null 返回）
bash deploy/scripts/probe-collection-shapes.sh
```

## 5. 出问题怎么回退

- runbook 改回内存存储：`RUNBOOK_STORE_TYPE=memory` 重启即回退（代价：重启丢数据，仅应急）。
- occurred_at 列**无需回退**：可空列，服务端缺列时仅 MTTD 不计该行；确需回退：
  `ALTER TABLE incidents DROP COLUMN occurred_at;`（期间写入的数据丢失，历史行不受影响）。