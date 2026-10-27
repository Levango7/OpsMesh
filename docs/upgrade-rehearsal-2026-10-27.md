# 存量库升级演练报告（v0.16.0 → main，2026-10-27）

> 触发：切版前置项（任务 #76 / 板上"存量库演练"）。`5c6f3af` 一笔带入 **6 条新迁移（024–029）**，
> 而 `v0.16.0` 已经发布——也就是说下一版对**已有客户库**的影响此前从未演练过。
> 本报告只写实测到的数字；静态推断与未验项都单列，不当结论用。

## 0. 环境与口径（复算必读）

| 项 | 值 |
|---|---|
| 被测二进制 | `opsmesh migrate` 两个版本：`v0.16.0`（`git clone --branch v0.16.0` 后 `go build ./cmd/opsmesh`）与 `main` = **`a8e13e9`** |
| 数据库 | `mysql:8` 官方镜像容器（报告内实跑版本 **8.4.11**），宿主 Windows + Docker Desktop，端口只绑 `127.0.0.1:13306` |
| 隔离 | 容器名 `opsmesh-rehearse-mysql-qoder`、库名 `opsmesh_rehearse_*`；**未触碰**正在运行的 `opsmesh-*` 生产样例栈 |
| 数据量 | `audit_log` 10 万 / 20 万 / **100 万** 三档 + `tasks` 500 / 2 万；种子 id 段固定偏移便于复算 |
| 驱动命令 | `bash deploy/scripts/upgrade-rehearsal.sh`（本报告同时是该脚本的首次实跑记录） |
| 耗时抖动 | 同一迁移在 20 万行下两次分别为 **4s** 与 **14s**（含进程启动、RBAC 种子写、宿主磁盘状态）⇒ 本报告只把耗时当**量级**用，不当 SLA |

> ⚠️ 这些数字是"本机容器 + 单线程探针"下量出来的。客户环境的磁盘/缓冲池/并发不同，
> 绝对值不可照抄；可照抄的是**判据形状**（能不能升、数据有没有动、结构是否等价、谁被锁住）。

## 1. 结论速览

| 问题 | 结论 | 证据 |
|---|---|---|
| v0.16.0 存量库能不能升到 main？ | **能**。6 条迁移全部应用，水位 23 → 29，`rc=0` | ③ 段日志；`SUMMARY legacy=23 upgraded=29` |
| 会不会丢数据？ | **没有**。迁移前后 `audit_log` 行数与 CRC32 指纹逐字节同值 | 200000 行 / 指纹 `429911150602403`（前后一致） |
| 撞重复列会不会炸？ | **不炸**。`028` 在存量库上必撞 `Error 1060 Duplicate column name`，被 `tolerateIdempotentDDLError` 判"已处于目标状态"放行 | ③ 日志行 `[store] 迁移 28 … 幂等放行（column tasks.approval_required 已处于目标状态）: Error 1060 …`，`tolerated=1` |
| 能不能重复执行？ | **能**。第二次 `migrate` 零 DDL、`rc=0`、约 1.7s | ⑤ 段"重放零 DDL" |
| 升级后的库 == 全新安装的库吗？ | **不等价（1 处）** ⇒ 本次演练唯一的真缺陷，见 §4 | ④ 段"定义漂移" |
| 迁移会不会卡住生产写入？ | **会，且默认无上限**。见 §3——这条对交付最要命 | 23.0s 写入停顿实测 |

## 2. DDL 本身的代价（100 万行 `audit_log`，服务端 `NOW(3)` 夹逼）

| DDL | 算法 | 实测 |
|---|---|---|
| `tasks ADD COLUMN priority INT DEFAULT 0` | `ALGORITHM=INSTANT`（服务端接受） | **44ms** |
| `tasks ADD COLUMN script_body MEDIUMTEXT` | `INSTANT` | **59ms** |
| `audit_log ADD COLUMN high_risk TINYINT(1) NOT NULL DEFAULT 0` | `INSTANT` | **76ms** |
| `audit_log ADD COLUMN <同形状>`（默认算法） | 默认即 INSTANT | **83ms** |
| `CREATE INDEX … ON audit_log(high_risk)` | `ALGORITHM=INPLACE, LOCK=NONE`（服务端接受） | **2585ms** |

即：**四条加列都是元数据级操作（几十毫秒），唯一的重活是 `026` 那条索引，100 万行约 2.6 秒。**

并发写入实测（同表被建索引期间）：150 次 `INSERT INTO audit_log` 平均间隔 **15.1ms**、最大 **33.0ms**；
无 DDL 对照（40 次）平均 **16.4ms**、最大 **37.0ms** ⇒ **建索引不阻塞同表 DML**（INPLACE 语义，实测吻合）。

## 3. 真正的隐患：迁移等元数据锁时，会把该表的写入一起挂住

上面那条"不阻塞"只覆盖了 DML→DDL 的方向。反方向才是交付事故形态：

```
会话 A：START TRANSACTION; SELECT count(*) FROM tasks; SELECT SLEEP(25); COMMIT;   -- 持有 tasks 的 MDL
会话 B：opsmesh-head.exe migrate --store=mysql …                                     -- 028/029 的 ALTER 排队等锁
会话 C：10 条 INSERT INTO tasks                                                       -- 排在 B 后面
```

实测：**会话 C 的 10 条写入耗时 23.0s**（≈ 会话 A 的持锁时长）。基线同样 10 条约 400ms。

放大这条隐患的三个事实：

| 事实 | 实测出处 |
|---|---|
| `@@global.lock_wait_timeout = 31536000`（**1 年**） | 容器内 `select @@global.lock_wait_timeout` |
| 仓库代码**从未**设置过它 | `grep -rn "lock_wait_timeout" --include=*.go --include=*.sh --include=*.yml .` ⇒ **0 命中** |
| 因此迁移连接等锁 = **无限期**，且期间该表写入全排队 | 上表 23.0s 停顿 + 默认值 |

把会话 B 的锁等待显式设成 8s（`SET SESSION lock_wait_timeout=8`）后，ALTER **8s 后 `ERROR 1205` 退出**，
写入立即解冻，且失败是原子的——事后查 `information_schema.columns` 确认没留半截列（实得 0 行）。
⇒ **有界的锁等待 + 明确报错，远好于无限期挂住。**

运维口径（已写入 `docs/upgrade-guide.md`）：升级窗口内先让业务空闲或停控制面，
再用 `select trx_mysql_thread_id, trx_query from information_schema.innodb_trx` 确认没有在跑的事务。
演练脚本已把这条做成 **① 前置体检**（≥1s 的事务即判红）。

## 4. 本次演练发现的真实缺陷：升级库与新装库结构不等价

```
定义漂移 tasks.approval_required [升级库=tinyint(1)? def=0 | 新装=tinyint! def=0]
```

- `v0.16.0` 的建表路径已经把 `approval_required` 建成**可空**列，所以 `028` 的
  `ADD COLUMN approval_required TINYINT NOT NULL DEFAULT 0` 在存量库上撞 1060 后被幂等放行，
  **列的空性没有被对齐**；全新安装走 `001` 建表路径，拿到的是 **NOT NULL**。
- 现状影响：**潜伏**。存量 500 行里 `approval_required IS NULL` 计数实测为 **0**（旧代码从不写该列，默认 0）。
  一旦有人显式写 NULL，升级库接受、新装库拒绝 ⇒ 同一份代码在两类客户库里行为不同。
- 修法（二选一，属 `internal/store/sqlstore/` 领地，**本线不代改**）：
  `028` 追加 `MODIFY COLUMN approval_required TINYINT NOT NULL DEFAULT 0`；或把新装路径的同列改为可空。

这条发现是④段那条**通用不变式**（"升级后的库 ≡ 全新安装的库"）自动抓出来的，
不依赖硬编码列名——以后再新增迁移，脚本不需要改。

## 5. 判据自证（新门必须有射程）

`upgrade-rehearsal.sh --selftest`：**18 个红/绿样本 + 1 项 SQL 可解析性自证**，通过=19 失败=0。
每条判据都同时有"该红的红"和"该绿的绿"，防止空转门与误伤。

窄变异（打在判定维度上，不是整文件回退）：

| 变异 | 期望 | 实测 |
|---|---|---|
| `judge_trx` 阈值 `≥1` → `≥5000` | 长事务样本漏判 | `[FAIL] 样本「trx 25s 长事务」期望判 red、实得 green` |
| `judge_equiv` 去掉索引比对 | 索引单边样本漏判 | `[FAIL] 样本「equiv 索引单边存在」期望判 red、实得 green` |
| `judge_data` 只比行数、丢掉指纹 | 静默改写样本漏判 | `[FAIL] 样本「data 行数不变但指纹变」期望判 red、实得 green` |

三条变异后均 `selftest 退出码=1`；还原后 `sha256` 与变异前一致（`ff74938a…`）。

`judge_trx` 特意**不读** `trx_timer_wait`：该列在 MySQL 8.4 已被移除（实测
`Unknown column 'trx_timer_wait'`），照抄它会让查询报错→取到空串→被读成"没有长事务"，
门就空转判绿了。自测的最后一项就是钉这条：龄期查询必须在目标服务器上解析出数字。

阈值从最初写的 5s 收紧到 1s，是**被实测打了一巴掌**之后改的：
第一轮端到端跑（外层 `timeout 40`）里，一条 4s 龄的事务同样让后续迁移等到了超时——
年龄只是代理指标，真正前提是业务空闲。

## 6. 这次演练**没有**覆盖什么（别把结论用过头）

1. **只演到 SQL 层**：走的是 `opsmesh migrate` 子命令，没起完整控制面容器。
   迁移后 `GET /api/v1/tasks` 能否读回新列、审批工作流是否真的从 404 变成可用，需要活体栈另验。
2. **只演 MySQL**：MariaDB / 达梦 / 金仓 / 人大金仓这些宣称支持的方言路径未演练。
   `ADD COLUMN` 幂等策略、`INSTANT` 支持度在各方言上不同，是独立的一条腿。
3. **没验新迁移背后的能力**：`024` 云账号、`026` 高风险审计、`027` 密钥访问日志的**功能可用性**
   不在本演练射程（表建起来 ≠ 能力可用）。
4. **① 前置体检是全局口径**：落在无关库上的事务也会判红，属保守误报；
   精确核对用 `performance_schema.metadata_locks`。
5. **耗时只作量级**：4s / 14s 的差值不足以说明任何优化效果，条件见 §0。
6. **未接 CI**：演练需要两个版本的二进制 + 一个 MySQL，接线要动 `.github/workflows/`
   （禁区，且裁定②触发前不动 workflows）——留给 lead 开口。

## 7. 建议与归属

| # | 建议 | 归属 | 优先级 |
|---|---|---|---|
| 1 | `028` 补 `MODIFY COLUMN` 消除升级/新装漂移 | `internal/store/sqlstore/`（kilo） | **P1（切版前）** |
| 2 | 迁移连接显式 `SET SESSION lock_wait_timeout`（建议 15s）并在报错里点名"MDL 等待" | `internal/store/sqlstore/`（kilo） | **P1** |
| 3 | 升级 runbook 增加"业务空闲前置 + 演练命令"两行 | `docs/upgrade-guide.md` | 本线已做 |
| 4 | 演练脚本进 CI（需 MySQL service + 上一版二进制工件） | `.github/workflows/` | 待 lead 开口 |
| 5 | 存量卷活体升级演练（容器 + `GET /version`）另做一次 | 交付/验证 | P2 |

## 8. 复现命令

```bash
# 1) 两个二进制
git clone --branch v0.16.0 <repo> /tmp/legacy && (cd /tmp/legacy && go build -o /tmp/opsmesh-0160.exe ./cmd/opsmesh)
(cd <repo> && go build -o /tmp/opsmesh-head.exe ./cmd/opsmesh)

# 2) 一次性演练库（只绑环回）
docker run -d --name opsmesh-rehearse-mysql-qoder --rm \
  -e MYSQL_ROOT_PASSWORD=<scratch> -p 127.0.0.1:13306:3306 mysql:8

# 3) 演练（含判据自测）
BASE_DSN='root:<scratch>@tcp(127.0.0.1:13306)/' \
MYSQL_CLI='docker exec -i opsmesh-rehearse-mysql-qoder mysql -uroot -p<scratch> -N' \
OPSMESH_LEGACY_BIN=/tmp/opsmesh-0160.exe OPSMESH_HEAD_BIN=/tmp/opsmesh-head.exe \
bash deploy/scripts/upgrade-rehearsal.sh --rows 200000
```

凭据是本机一次性演练用的临时口令，**不是**任何环境的真实凭据。
