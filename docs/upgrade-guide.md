# OpsMesh 升级指南

> 适用：存量部署（数据卷已存在）升级到含 schema 迁移的新版本。
> 全新部署（空数据卷）不需要本指南：首启会自动应用全部迁移。
> 背景与实测数据：技术债登记册 TD-83、演练报告 §44（2026-10-07）。

## 1. 升级前必查：是否需要维护窗口

020/021 是"重建表 + 禁写"型迁移（`ci_items` 整表 COPY 重建 + FULLTEXT 索引），
耗时随数据量线性增长，且迁移窗口内 CMDB 写入不可用（实测最大写入间隔 19.8s）。
升级前先在**库上**查规模（不是猜）：

```sql
SELECT COUNT(*), SUM(LENGTH(CAST(attrs AS CHAR))) FROM ci_items;
```

| 规模 | 迁移耗时（§44 实测） | 处置 |
|---|---|---|
| ≤ 10 万行 | ≈ 41s | `deploy.sh migrate` 前置即可，up 预算内安全 |
| 10 万 ~ 20 万行 / 45 MiB | ≈ 41–82s | `deploy.sh migrate` 前置，并在 `.env` 放宽 `MIGRATION_WAIT_BUDGET`（如 300） |
| **> 20 万行 / 45 MiB** | 30 万行 ≈ 117s（120s 预算的 97%）；100 万行 ≈ 376s | **预约维护窗口**：窗口内停写（或停控制面），跑 `deploy.sh migrate`，确认完成后恢复写入 |

判据按 FT 建索引边际 ≈0.85 s/MiB 索引文本外推并留一倍余量。
实测载体是 32 核 NVMe + 1G 缓冲池，这些数字是**下界**，客户 VM 只会更慢。
阈值依据：mysqld 自报 STORED 生成列对 `ALGORITHM=INSTANT`/`INPLACE` 均返回
1845（必须整表 COPY），FULLTEXT 建索引对 `LOCK=NONE` 报 1846（requires a lock）——
即这类迁移无法在线无锁完成。

## 1.5 升级前必查：库里不能有在跑的事务（2026-10-27 实测新增）

`024–029` 这轮演练量到的不是"迁移慢"，而是**迁移等锁时把业务写入一起挂住**：

- 加列是 `ALGORITHM=INSTANT`（100 万行实测 44–83ms），建索引是 `INPLACE`（实测 2.6s，
  期间同表 150 次 INSERT 平均间隔 15.1ms vs 无 DDL 对照 16.4ms ⇒ **不阻塞 DML**）。
- 但反过来：只要有一个事务**已经持有该表的元数据锁**，迁移的 `ALTER` 就排队，
  而排在 `ALTER` 后面的写入全部等待。实测同库 `tasks` 上有事务时，
  **10 条 INSERT 耗时 23.0s**（基线约 0.4s）。
- 更糟的是默认没有上限：`@@global.lock_wait_timeout = 31536000`（1 年），
  而迁移连接**从未**显式设小它（`grep -rn lock_wait_timeout --include=*.go .` 实测 0 命中）
  ⇒ 一个卡住的事务能让迁移无限期挂住并连带冻结该表写入。

升级前执行（返回 0 行才继续）：

```sql
SELECT trx_mysql_thread_id, timestampdiff(second, trx_started, now(3)) AS age_s, trx_query
  FROM information_schema.innodb_trx
 WHERE timestampdiff(second, trx_started, now(3)) >= 1;
```

非空的处置顺序：**停控制面写入 → 等事务结束 → 或 `KILL <trx_mysql_thread_id>`**（后者会回滚该事务，
需业务方确认）。做不到就换窗口，不要硬升。

演练与判据本身已可复跑（含 ① 前置体检、④ "升级库 ≡ 新装库"结构等价不变式、⑤ 幂等重放）：

```bash
bash deploy/scripts/upgrade-rehearsal.sh --selftest   # 判据自测，不需要二进制
bash deploy/scripts/upgrade-rehearsal.sh --rows 200000
```

实测记录与未覆盖项见 `docs/upgrade-rehearsal-2026-10-27.md`。

## 1.6 `030`：把 `approval_required` 的列定义收敛（一次在线重建）

两条建库路径曾给出两种定义：新装走 `028` 得到 `TINYINT NOT NULL DEFAULT 0`，
老库由历史 `initSchemaExtra` 先补 `BOOLEAN DEFAULT 0`（= 可空 `tinyint(1)`），
此后 `028` 的 `ADD COLUMN` 见列已存在被幂等放行、改不了既有定义 ⇒ **升级库 ≠ 新装库**
（2026-10-27 演练 §4 的「升级库 ≡ 新装库」判据抓出）。`030` 负责收敛：先把历史
`NULL` 回填为 `0`（审批只有两态，`NULL` 无业务含义），再 `MODIFY` 到规范定义。

代价（本机 MySQL 8.4.11 / 32 核 NVMe / 20 万行合成表实测，客户 VM 只会更慢）：

| 库的状态 | 实测耗时 | 说明 |
|---|---|---|
| 已是规范定义（绝大多数新装库） | **37ms** | MySQL 判同定义即空操作，不重建 |
| 确实要改（`tinyint(1) NULL` → `tinyint NOT NULL`） | **5.5–7.1s** | 整表 INPLACE 重建，`LOCK=NONE`（重建期间读写不阻塞） |

即使发生重建也不必停写；配合迁移连接的 15s MDL 上限（`9bc25aa`），若恰有长事务持表，
迁移会快速失败而不是把 `tasks` 的写入挂住（处置见 §1.5）。升级后校验（应为 `tinyint` / `NO` / `0`）：

```sql
SELECT column_type, is_nullable, column_default FROM information_schema.columns
 WHERE table_schema=database() AND table_name='tasks' AND column_name='approval_required';
```

## 2. 标准升级流程

```bash
# 0. 备份（既有备份流程，如 opsmesh backup 子命令）
# 1. 查规模（第 1 节的 SQL），按判据决定是否需要维护窗口
# 2. 前置迁移（一次性容器，与启动内联同一条代码路径，报耗时并核对库内版本）
./scripts/deploy.sh migrate
# 3. 正常部署（此时启动内迁移已幂等跳过，健康预算不再被 DDL 占用）
./scripts/deploy.sh up
# 4. 冒烟
./scripts/deploy.sh smoke
```

`migrate` 子命令幂等可重放：迁移失败排除故障后重跑即可。
它还会核对**库内已应用版本号 = 二进制携带的迁移文件最大号**
（判据不信退出码——进程跑完不代表迁移真的落库）。

预算旋钮（`.env`）：`MIGRATION_WAIT_BUDGET`（默认 120s）。
它同时控制两处：`up` 时 controlplane 的健康等待上限，与 `migrate`
一次性容器内的迁移 ctx（`OPSMESH_MIGRATION_BUDGET_SEC`）——
大表场景放宽后两处同时生效，不会口径错位。

## 3. §44 演练曲线的复现方法（取数命令）

曲线数据（1 千行 7.9s ｜ 1 万行 12.5s ｜ 10 万行 41.2s/48.0s ｜
30 万行 117.0s ｜ 100 万行 375.6s；`DROP INDEX` 恒 0.8–1.1s 与行数无关）
用一次性容器实测取得。复现要点（避免重踩）：

```bash
# 载体：出厂 mysql:8.0 + 仓库监控配置。
# 两个坑（2026-10-07 实测）：
#  1. Windows bind-mount 会让 mysqld 静默忽略挂进去的 cnf
#     （"World-writable config file" 被忽略），必须 docker cp 进容器；
#  2. cnf 必须在**首启前**就位：先以默认配置初始化数据目录、
#     再换 redo 容量重启，InnoDB 会判 "data files are corrupt"
#     （redo 日志按首启配置创建）。故用 create → cp → start 序列，
#     不用 run → cp → restart。
# 仓库 cnf 在 git 中是 644，docker cp 保模式，mysqld 接受（非 world-writable）。
docker create --name opsmesh-mig-rehearsal \
  -e MYSQL_ROOT_PASSWORD=rehearsal -e MYSQL_DATABASE=opsmesh \
  -p 13306:3306 mysql:8.0
docker cp deploy/monitoring/mysql.cnf opsmesh-mig-rehearsal:/etc/mysql/conf.d/rehearsal.cnf
docker start opsmesh-mig-rehearsal
# 确认 cnf 真的生效（判据不信"没报错"）：
docker exec opsmesh-mig-rehearsal mysql -uroot -prehearsal \
  -N -e "SELECT @@innodb_redo_log_capacity, @@innodb_buffer_pool_size"
# 期望：268435456  1073741824

# 应用 001..019（逐文件执行，以 mysql 自身退码为准）
for f in internal/store/migrations/0[01][0-9]_*.sql; do
  docker exec -i opsmesh-mig-rehearsal mysql -uroot -prehearsal opsmesh < "$f" || break
done

# 阶梯灌数据后取数（规模判据的两个维度：行数 + 索引文本字节数）
docker exec opsmesh-mig-rehearsal mysql -uroot -prehearsal opsmesh \
  -e "SELECT COUNT(*), SUM(LENGTH(CAST(attrs AS CHAR))) FROM ci_items;"

# 计时 020/021：逐条重放迁移文件并计时（time + 重定向进 mysql 客户端）
time docker exec -i opsmesh-mig-rehearsal mysql -uroot -prehearsal opsmesh \
  < internal/store/migrations/020_ci_items_fulltext.sql
```

中文语料写入必须显式 `--default-character-set=utf8mb4`（客户端默认
latin1_swedish_ci，写入即损坏），校验落库字节用 `HEX()`。

## 4. `--multi-schema` 部署的首触说明

`--multi-schema`（默认 `false`，`internal/config/config.go`）启用时，
per-tenant store 是**懒创建**的：首个请求在 `m.mu` 写锁内建 schema 并跑
全部迁移（`internal/store/multischema/multi_schema.go` 的 `storeFor`）。
实测每 schema 固定成本 ≈7.9s（1 千行规模）——升级后每个租户的
**首个请求承担整段 DDL**，同租户并发首访与其他冷租户会被写锁挡在身后
（症状："健康检查早过了，但首访超时"）。

处置二选一：
- 接受首触延迟（租户少、访问稀疏的部署无感）；
- 升级后按租户串行预热：`SELECT DISTINCT tenant_id FROM users;` 枚举租户，
  逐租户发一个轻请求（如 `GET /api/v1/health` 带租户上下文），把 DDL
  从用户请求路径挪到运维窗口。

## 5. 其他已知坑

- **MySQL 口令与数据卷**：MySQL 只在数据目录为空时初始化口令。复用存量卷时，
  新 `.env` 的口令与卷内旧口令不匹配会导致全部服务 `Access denied`
  （MySQL 自身仍 healthy）。`deploy.sh up` 的 `verify_mysql_app_auth`
  会在基础设施阶段提前拦下并给出两条出路（恢复旧口令 / 丢弃数据重开）。
- **回滚**：18 个 `.down.sql` **永不自动执行**；回滚是 4 步手工流程，
  见 `docs/operations.md` §6.3.2。
- **微服务侧迁移**：各微服务（task-svc 等）有自己的迁移机制，随服务容器
  启动内联执行；本指南的 `migrate` 子命令只覆盖控制面主库（`opsmesh`）。
