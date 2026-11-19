# ABAC soak 报表工具

`soak-report.py` 读取控制面 `abac_decision_logs`，生成 ABAC M4 7 天 soak 的每日 Markdown 报表。**只读**：脚本只发 `SELECT`，不执行任何 `INSERT`/`UPDATE`/`DELETE`/DDL，也不使用 `INTO OUTFILE`/`LOAD DATA`。

- 计划与门禁：[`docs/projects/abac-m4-soak-plan.md`](../../docs/projects/abac-m4-soak-plan.md)
- 依赖：Python 3.8+、MySQL 8 客户端（`mysql` 在 `PATH`，或 `--mysql <path>`）；**不需要 pip 安装任何包**
- 表结构来源：`internal/store/sqlstore/migrations/036_abac_decision_logs.sql`

## 快速开始

```bash
# 1) 只读 DSN（推荐放环境变量，避免密码进 shell history）
export OPSMESH_SOAK_DSN='mysql://opsmesh_soak:CHANGE_ME@127.0.0.1:3306/opsmesh'
export OPSMESH_SOAK_BASE_URL='http://127.0.0.1:8080'   # 可选：附抓 /metrics

# 2) 最近 7 天，stdout 输出 Markdown
python3 scripts/abac/soak-report.py

# 3) 最近 7 天，写入文件
python3 scripts/abac/soak-report.py --out /var/lib/opsmesh/soak/$(date -u +%F).md

# 4) 指定窗口（UTC；--until 为开区间）
python3 scripts/abac/soak-report.py --since 2026-11-19 --until 2026-11-26

# 5) 分参数连接（覆盖 DSN）
python3 scripts/abac/soak-report.py \
  --host 127.0.0.1 --port 3306 --user opsmesh_soak --database opsmesh
```

`--print-sql` 只打印将要执行的只读 SQL 并退出，用于评审/审计，不连接数据库：

```bash
python3 scripts/abac/soak-report.py --print-sql
```

## 只读权限

创建专用账号，只授予单表 `SELECT`：

```sql
CREATE USER 'opsmesh_soak'@'%' IDENTIFIED BY '<strong-password>';
GRANT SELECT ON opsmesh.abac_decision_logs TO 'opsmesh_soak'@'%';
FLUSH PRIVILEGES;
```

- 不要授予 `INSERT`/`UPDATE`/`DELETE`/`CREATE`/`DROP`/`FILE`。
- 脚本会在执行前断言每条 SQL 以 `SELECT` 或 `WITH` 开头；即便误改脚本，也只会被 MySQL 拒绝（账号无写权限）。
- 密码不会出现在 `mysql` argv 中：脚本把它放进子进程的 `MYSQL_PWD` 环境变量。仍建议用 `OPSMESH_SOAK_DSN` 环境变量或受控配置文件提供 DSN，不要写在命令行参数里。

## 报表内容

| 区块 | 说明 |
|---|---|
| Summary | 窗口内总行数、allow/deny、deny 率、`shadow:` 前缀行、非 shadow 行、agent 数、distinct `bundle_id`、首末 `received_at` |
| Daily counts by mode | 按日 + `mode`（`device` / `device-offline`）的 allow/deny、deny 率、shadow 行、agent 数、bundle 数 |
| Daily counts by agent and mode | 按日 + `agent_id` + `mode` 的明细，用于定位单设备异常 |
| Upload lag | `TIMESTAMPDIFF(SECOND, occurred_at, received_at)` 的 p50/p95/min/max（按日 + mode） |
| Error-ish deny reasons | `allowed=0` 且 reason 命中 `error/expired/no bundle/missing/verify/invalid/fail` 的 Top N |
| Controlplane /metrics | 可选，抓取 `--base-url` 的 `/metrics`，只列名字含 `abac` 的计数器 |

## 口径与解读

1. **`mode=device` 不等于“已强制”**：`shadow` 决策也会以 `mode=device` 回传，只在 `reason` 前加 `shadow: ` 前缀。报表用 `shadow rows` 列区分；非 shadow 行才接近 enforce 行。判定是否真的阻断，还要看任务结果 `stderr` 是否以 `abac denied:` 开头（见 soak 计划 §3.1）。
2. **在线与离线滞后必须分开看**：`device-offline` 是断网补传，`occurred_at` 可能很旧，不能并入在线 p95 门禁。在线目标 p95 ≤30s；负值说明设备时钟领先控制面，需单独排查。
3. **deny 率包含 would-deny**：shadow 期间的 deny 是“如果 enforce 会拒绝”，不是实际失败。关键路径 would-deny ≥1% 时按 owner 决策 2 不得继续放量。
4. **distinct bundle_id**：稳态每个 agent 应为 1；无策略变更时持续 >1 通常意味着 ETag/规范化漂移或策略重签。
5. **`/metrics` 可能没有设备侧计数器**：当前构建最多导出 `opsmesh_abac_shadow_decisions_total` 与 `opsmesh_abac_enforce_decisions_total`；bundle 拉取 200/304、agent 回传 accepted/failed/dropped 尚未作为指标导出，需要结合控制面访问日志与 agent 日志（soak 计划 §3.2 的 D0 观测前置项）。指标抓取失败只是 warning，不影响数据库报表。
6. **窗口**：默认“最近 N 天到当前时刻”，不是自然日；首尾日可能不完整。需要完整自然日时显式传 `--since`/`--until`（UTC）。`received_at` 是 DATETIME（UTC），保留期清理也以它为准。
7. **数据库里只有 accepted 决策**：`abac_decision_logs` 是成功回传并落库的记录；`failed`/`dropped` 不会出现在表中，必须从控制面访问日志与 agent `Stats()`/日志获取。因此本脚本的 lag 统计天然只覆盖 accepted 决策。
8. **多租户**：脚本不按租户过滤，会汇总库内窗口内所有租户的 `abac_decision_logs`。soak 专用库/账号建议只暴露目标租户数据；混租户部署需在 SQL 层另行加 `tenant_id` 过滤（本脚本不提供该参数，避免误用）。

## 定时任务示例

Linux cron（每天 01:17 UTC；注意 `%` 需转义为 `\%`）：

```cron
17 1 * * * cd /opt/opsmesh && OPSMESH_SOAK_DSN='mysql://opsmesh_soak:CHANGE_ME@127.0.0.1:3306/opsmesh' OPSMESH_SOAK_BASE_URL='http://127.0.0.1:8080' /usr/bin/python3 scripts/abac/soak-report.py --days 7 --out /var/lib/opsmesh/soak/$(date -u +\%F).md >> /var/log/opsmesh/soak-report.log 2>&1
```

Windows 任务计划程序：

```powershell
python scripts\abac\soak-report.py --days 7 --out "D:\opsmesh\soak\$(Get-Date -Format yyyy-MM-dd).md"
```

cron/任务计划只负责生成报表；**晋升/回滚由人按 soak 计划 §4/§5 签字执行**，脚本不会修改任何配置。

## 退出码与无 MySQL 环境

| 退出码 | 含义 |
|---|---|
| `0` | 报表已生成（`/metrics` 抓取失败仅 warning） |
| `2` | 参数错误、找不到 `mysql` CLI、连接/查询失败、`--out` 写入失败 |

若机器上没有 `mysql` CLI，脚本会打印明确提示并以 **2** 退出，例如：

```text
soak-report: 'mysql' CLI not found ('mysql'). Install the MySQL 8 client or pass --mysql <path>. No database connection was attempted.
```

`--help` 与 `--print-sql` 不连接数据库，分别以 0 退出。

## 验证

```bash
python -m py_compile scripts/abac/soak-report.py
python scripts/abac/soak-report.py --help
python scripts/abac/soak-report.py --print-sql
```

无 MySQL 时直接运行（不带 `--help`/`--print-sql`）应打印上面的提示并返回 2；这是预期行为，不是脚本故障。
