# OpsMesh 灾难恢复 Runbook

> 版本：v0.1 · 编制日期：2026-08-07 · 适用基线：OpsMesh MVP（Helm Chart `deploy/helm/opsmesh`，含 MySQL 备份 CronJob）
>
> 本文档定义 OpsMesh 平台的备份策略、恢复操作、RTO/RPO 目标与故障切换流程，供值班 SRE 在故障或灾难场景下按图索骥执行。所有命令假设 Helm release 名为 `opsmesh`、命名空间为 `opsmesh`，按需替换。

---

## 第1章 概述

### 1.1 文档目的

为 OpsMesh 平台提供**可执行、可演练、可审计**的灾难恢复操作手册，确保在任何故障场景下，值班人员能在 RTO 内完成恢复并满足 RPO 约束。

### 1.2 适用范围

| 组件 | 是否覆盖 | 说明 |
|---|:---:|---|
| MySQL（业务库 `opsmesh`） | ✅ | 核心持久层，CronJob 定期 `mysqldump` |
| 控制面（controlplane） | ✅ | 无状态，恢复即重启；配置走 Helm values |
| Agent | ⚪ | 无状态，节点存活即自注册；失联仅影响该节点任务 |
| Redis | ⚪ | 纯缓存（任务租约/会话），丢失可重建，不纳入备份 |
| 前端静态资源 | ⚪ | `go:embed` 内嵌于控制面二进制，随控制面恢复 |

### 1.3 术语与角色

- **RTO**（Recovery Time Objective）：恢复服务可用所需最大时长。
- **RPO**（Recovery Point Objective）：可容忍的最大数据丢失时间窗口。
- **值班 SRE**：执行本 Runbook 的第一责任人。
- **OpsMesh Owner**：平台负责人，负责重大决策（如全量切换到灾备中心）。

### 1.4 系统拓扑与单点风险

```
单中心拓扑（MVP 基线）
┌──────────────────────────────────────────────┐
│  K8s 集群（单中心）                            │
│  ┌──────────────┐  ┌──────────────┐          │
│  │ controlplane │× │   mysql      │  ← 单副本 │
│  │  (N 副本 HA) │  │  StatefulSet │  ← 单点   │
│  └──────────────┘  └──────────────┘          │
│         │                  │                  │
│         └──── gRPC 9090 ───┤                  │
│                            ▼                  │
│  agent1 ... agentN（纳管设备，无状态）         │
└──────────────────────────────────────────────┘
```

**单点风险**：MySQL 为单副本 StatefulSet，节点故障即数据层不可用，是本 Runbook 的核心防护对象。

---

## 第2章 备份策略

### 2.1 备份对象与内容

| 对象 | 内容 | 方式 | 频率 |
|---|---|---|---|
| MySQL | 全库（含 `mysql` 系统库的用户授权 + `opsmesh` 业务库 + routines/triggers/events） | `mysqldump --single-transaction --all-databases` | 每天 02:00 |
| Helm 配置 | release values | `helm get values opsmesh -n opsmesh` 纳文本归档 | 随 MySQL 备份同节奏 |
| Secret | `opsmesh-secret`（jwt/provision/mysql 密码） | 外部 KMS 或 `kubectl get secret -o yaml` 加密导出 | 变更时即时 |

> **注意**：Secret 不进 CronJob 备份（避免明文密码落盘到 PVC）。Secret 恢复见 3.1。

### 2.2 MySQL 定期备份（CronJob）

由 Helm 模板 `templates/mysql-backup-cronjob.yaml` 渲染，启用条件：`mysql.enabled=true && controlplane.store=mysql && mysql.backup.enabled=true`。

**关键参数**（`values.yaml` 的 `mysql.backup` 段，均有默认值）：

```yaml:values.yaml
mysql:
  backup:
    enabled: true
    schedule: "0 2 * * *"        # 每天 02:00（节点本地时区）
    retentionDays: 7             # 本地保留 7 天
    historyLimit: 7              # 保留 7 个成功 Job 历史
    failedHistoryLimit: 3
    backoffLimit: 2
    activeDeadlineSeconds: 1800  # 单次最多跑 30 分钟
    storageSize: 20Gi
    storageClass: ""             # 空=集群默认
    resources:
      requests: { cpu: 100m, memory: 128Mi }
      limits:   { cpu: 500m, memory: 512Mi }
```

**执行流程**：

1. CronJob 触发，Pod 挂载 `opsmesh-mysql-backup` PVC 到 `/backup`。
2. `mysqldump --single-transaction`（InnoDB MVCC 一致性快照，不锁表）管道 `gzip` 写 `/backup/opsmesh-<UTC时间戳>.sql.gz`。
3. `gzip -t` 完整性自检，失败即删档并 `exit 1`（Job 标 Failed）。
4. `find /backup -mtime +7 -delete` 清理超龄文件。

### 2.3 备份存储与保留

- **主副本**：集群内 PVC `opsmesh-mysql-backup`（ReadWriteOnce），保留 7 天。
- **Job 历史**：`successfulJobsHistoryLimit=7`，可用 `kubectl logs job/<name>` 回看最近 7 次执行日志。
- **保留周期双重保险**：PVC 内文件清理（`find -mtime`）+ Job 历史清理，二者独立，任一生效即可。

### 2.4 异地副本与加密

集群内 PVC 与 MySQL 同集群，**单集群全损场景下两者一起丢失**。生产环境必须叠加异地副本：

- **方案 A（推荐）**：CronJob 末尾追加 `kubectl cp` 或 `rclone copy` 将当日备份推到对象存储（S3/OBS），生命周期规则设 30 天。
- **方案 B**：Velero 整集群备份（含 PVC 快照）到对象存储，每日 1 次。
- **加密**：对象存储启用服务端加密（SSE-S3 / OBS 服务端 KMS）；如需客户端加密，在 gzip 后叠加 `openssl enc -aes-256-cbc`。

异地副本脚本示例（追加到 CronJob command 末尾，需在容器内预置 rclone 配置）：

```bash:代码示例：异地副本推送（Shell）
# 仅推送当日产物，失败不阻断主备份成功状态
rclone copy "/backup/${MYSQL_DATABASE}-${TS}.sql.gz" \
  "opsmesh-backup:opsmesh-mysql/$(date -u +%Y/%m/%d)/" \
  --transfers 4 --quiet || echo "[backup] WARN rclone push failed (non-fatal)"
```

### 2.5 备份监控与告警

| 监控项 | 方法 | 告警阈值 |
|---|---|---|
| Job 成功 | `kube_job_status_succeeded{job_name=~"opsmesh-mysql-backup.*"}` | 最近 25 小时内无成功 → |
| Job 失败 | `kube_job_status_failed` | 单次 Failed → ；连续 2 次 Failed → |
| 备份体积 | PVC 已用容量 `kubelet_volume_stats_used_bytes` | 较昨日下降 >50% → P2（疑似空库/中断） |
| 备份龄期 | 最近成功 Job 的 `status.completionTime` | 距今 >26h → |

Prometheus 告警规则示例：

```yaml:prometheus-rules.yaml
- alert: OpsMeshMySQLBackupStale
  expr: time() - max(kube_job_status_completion_time{job_name=~"opsmesh-mysql-backup.*",condition="Complete"}) > 93600
  for: 5m
  labels: { severity: p1 }
  annotations:
    summary: "OpsMesh MySQL 备份已超过 26 小时未成功"
```

---

## 第3章 恢复步骤

### 3.1 恢复前置条件

1. **确认故障范围**：MySQL Pod 是否存在？PVC 是否还在？备份 PVC `opsmesh-mysql-backup` 是否可挂载？
2. **取最近一份备份**：
   - 若备份 PVC 可用：`kubectl exec` 进任一挂载该 PVC 的 Pod，`ls -lt /backup` 取最新。
   - 若 PVC 不可用：从异地副本（对象存储）拉取最新 `.sql.gz` 到任一可挂载目录。
3. **确认 Secret 存在**：`kubectl get secret opsmesh-secret -n opsmesh`。若 Secret 丢失，需从 KMS/离线归档恢复（密码丢失则旧备份无法解密 MySQL 数据，**必须保留 Secret 离线副本**）。

### 3.2 MySQL 全量恢复

**场景**：MySQL 数据损坏或误删，但 StatefulSet 与 PVC `data` 仍可重建。

命令示例：MySQL 全量恢复

```bash:命令示例：MySQL全量恢复
# 1) 取最新备份文件名（在挂载 backup PVC 的 Pod 内，或先 cp 到本地）
LATEST=$(kubectl exec -n opsmesh job/opsmesh-mysql-backup-<ts> -- ls -1t /backup | head -1)
echo "restore from: ${LATEST}"

# 2) 缩容控制面，停止写入（避免恢复期间新数据冲突）
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=0

# 3) 等待 MySQL Pod Ready（单副本，确保无并发写入）
kubectl wait pod -n opsmesh -l app.kubernetes.io/component=mysql \
  --for=condition=Ready --timeout=120s

# 4) 执行恢复：解压灌库（覆盖式，先清库再灌）
kubectl exec -n opsmesh -c mysql opsmesh-mysql-0 -- sh -c '
  set -e
  gunzip -c /backup/'"${LATEST}"' | mysql -uroot -p"$MYSQL_ROOT_PASSWORD"
'

# 5) 校验关键表行数
kubectl exec -n opsmesh -c mysql opsmesh-mysql-0 -- mysql -uroot -p"$MYSQL_ROOT_PASSWORD" \
  -e "USE opsmesh; SELECT COUNT(*) AS agents FROM agents; SELECT COUNT(*) AS tasks FROM tasks;"

# 6) 扩容控制面恢复服务
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=3
```

> **注意**：`mysql` 系统库恢复会覆盖授权表。若 MySQL 实例已重建且 root 密码来自新 Secret，需在恢复后 `FLUSH PRIVILEGES` 或确认 Secret 与备份时一致。

### 3.3 控制面恢复

控制面无状态，恢复即重新调度。仅需保证：

1. ConfigMap/Secret 存在（`kubectl get cm opsmesh-config -n opsmesh`、`kubectl get secret opsmesh-secret -n opsmesh`）。
2. MySQL 可连（3.2 已完成）。
3. `kubectl rollout restart deploy/opsmesh-controlplane -n opsmesh`，等待 Rollout 完成。

### 3.4 Agent 重新纳管

Agent 无状态，控制面恢复后自动重新注册（gRPC 注册通道）。无需手动操作。验证：

```bash:命令示例：验证Agent重新纳管
kubectl get pods -n opsmesh -l app.kubernetes.io/component=agent -o wide
# 控制面日志应出现 agent register 心跳
kubectl logs -n opsmesh deploy/opsmesh-controlplane --tail=100 | grep -i register
```

### 3.5 恢复后验证

| 验证项 | 命令 | 期望 |
|---|---|---|
| 控制面健康 | `curl http://<controlplane>:8080/healthz` | `ok` |
| gRPC 端口 | `kubectl exec ... -- nc -zv opsmesh-controlplane 9090` | succeeded |
| MySQL 连接 | 控制面日志无 `dial tcp ...:3306` 错误 | 无错误 |
| Agent 在线数 | 仪表盘设备页 | 与故障前一致 |
| 任务可下发 | 下发一个 `echo ok` 测试任务 | 成功执行 |
| 审计留痕 | 查审计表最近恢复后记录 | 恢复操作有记录 |

---

## 第4章 RTO / RPO 目标

### 4.1 指标定义

- **RPO** = 最近一次成功备份时间点 − 故障时间点。最坏情况为备份周期（24h）。
- **RTO** = 故障时刻 − 服务恢复时刻。含检测 + 决策 + 恢复执行 + 验证。

### 4.2 目标值与达成条件

表：RTO/RPO 目标对照表

| 场景 | RPO 目标 | RTO 目标 | 达成条件 |
|---|:---:|:---:|---|
| MySQL 误操作（库还在） | ≤24h | ≤30min | 3.2 流程，备份可挂载 |
| MySQL Pod 故障（PVC 还在） | 0 | ≤5min | StatefulSet 重调度，PVC 复用 |
| MySQL 节点故障（PVC 丢失） | ≤24h | ≤45min | 从异地副本拉取 + 3.2 灌库 |
| 控制面全副本故障 | 0 | ≤2min | K8s 重调度，无状态 |
| 单中心全损 | ≤24h | ≤4h | 异地备份 + 新集群 helm install + 灌库 |

> **说明**：MVP 基线 RPO=24h 由每日备份周期决定。如需更小 RPO，需引入 MySQL binlog 流式备份或主从复制（超出 MVP 范围，见 7.3 变更记录）。

### 4.3 降级矩阵

当无法满足目标时的降级策略：

| 降级触发 | 降级动作 | 影响 |
|---|---|---|
| 异地副本未配置 | 单中心 PVC 备份仍生效，但单集群全损时数据丢失 | RPO=∞（全损场景） |
| 备份 PVC 满 | 自动清理最旧文件，可能保留不足 7 天 | 历史回溯窗口缩短 |
| 恢复超 RTO | 先恢复只读控制面（查询可用），后台修任务调度 | 任务下发暂停 |

---

## 第5章 故障切换

### 5.1 控制面主副本故障

控制面多副本 + leader 选举（`leaderTTLSec=15`），主副本故障自动切换：

```bash:命令示例：控制面主副本故障处置
# 1) 确认 leader 已切换（日志）
kubectl logs -n opsmesh deploy/opsmesh-controlplane --tail=50 | grep -iE "leader|elect"

# 2) 确认副本数符合预期
kubectl get deploy opsmesh-controlplane -n opsmesh -o jsonpath='{.status.readyReplicas}'

# 3) 若未自动切，强制重启故障 Pod
kubectl delete pod -n opsmesh <faulty-controlplane-pod>
```

预期 RTO ≤ 15s（leader TTL）。无需数据恢复。

### 5.2 MySQL 单点故障

MySQL 单副本，故障即数据层不可用。按 PVC 是否存活分流：

```bash:命令示例：MySQL单点故障处置
# 1) 诊断 PVC 状态
kubectl get pvc -n opsmesh -l app.kubernetes.io/component=mysql
kubectl get pvc opsmesh-mysql-backup -n opsmesh

# 2a) PVC data 还在：直接重启 StatefulSet
kubectl rollout restart statefulset opsmesh-mysql -n opsmesh

# 2b) PVC data 丢失：走 3.2 全量恢复流程
```

### 5.3 Agent 失联

单台 agent 失联不影响平台和其他 agent。处置：

```bash:命令示例：Agent失联处置
# 查失联 agent
kubectl get pods -n opsmesh -l app.kubernetes.io/component=agent --field-selector status.phase!=Running

# 节点级故障：K8s 重调度 DaemonSet 自动恢复
# 节点存活但 agent 异常：kubectl delete pod <agent-pod> 触发重建
```

失联期间该节点任务置 `failed`（租约超期回收），可在 agent 恢复后手动重投。

### 5.4 全中心灾难（切换到灾备中心）

**前置**：灾备中心已建好 K8s 集群、Helm Chart 已就绪、异地备份可访问。

命令示例：全中心切换到灾备中心

```bash:命令示例：全中心切换到灾备中心
# 在灾备中心执行
# 1) 拉取最近备份
rclone copy opsmesh-backup:opsmesh-mysql/$(date -u +%Y/%m/%d)/ /tmp/restore/

# 2) 安装 Helm release（用与主中心一致的 values）
helm install opsmesh ./deploy/helm/opsmesh \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.replicaCount=3 \
  -n opsmesh --create-namespace

# 3) 灌库（参考 3.2，将 /tmp/restore/*.sql.gz 灌入新 MySQL）

# 4) 更新 DNS / Ingress 指向灾备中心控制面

# 5) Agent 重新指向新控制面（bootstrap 或更新 agent 配置的 controlplane 地址）
```

预期 RTO ≤ 4h（含拉取备份 + helm install + 灌库 + DNS 切换 + agent 重纳管）。

---

## 第6章 演练与维护

### 6.1 定期演练计划

| 演练项 | 频率 | 负责人 | 通过标准 |
|---|---|---|---|
| 备份可恢复性 | 每月 | SRE | 取最近备份灌入临时库，校验关键表行数 |
| MySQL 恢复 | 每季度 | SRE + Owner | 在演练环境完整执行 3.2，RTO 达标 |
| 全中心切换 | 每年 | Owner | 执行 5.4，业务可用且数据一致 |

### 6.2 演练步骤

命令示例：月度备份可恢复性演练

```bash:命令示例：月度备份可恢复性演练
# 在演练命名空间
NS=opsmesh-drill
kubectl create ns ${NS}

# 1) 起临时 MySQL
kubectl run mysql-drill -n ${NS} --image=mysql:8 \
  --env=MYSQL_ROOT_PASSWORD=drill --port=3306
kubectl wait pod -n ${NS} mysql-drill --for=condition=Ready --timeout=120s

# 2) 取最近备份灌入
LATEST=$(kubectl exec -n opsmesh job/opsmesh-mysql-backup-<ts> -- ls -1t /backup | head -1)
kubectl exec -n opsmesh job/opsmesh-mysql-backup-<ts> -- cat /backup/${LATEST} \
  | kubectl exec -n ${NS} -i mysql-drill -- sh -c 'gunzip | mysql -uroot -pdrill'

# 3) 校验
kubectl exec -n ${NS} mysql-drill -- mysql -uroot -pdrill \
  -e "SELECT COUNT(*) FROM opsmesh.agents;"

# 4) 清理
kubectl delete ns ${NS}
```

### 6.3 日常巡检

- 每日：确认最近一次备份 Job 成功（`kubectl get jobs -n opsmesh | grep mysql-backup`）。
- 每周：确认备份 PVC 已用容量合理、未满。
- 每月：执行 6.2 演练并记录结果到运维台账。

---

## 第7章 附录

### 7.1 命令速查

表：常用命令速查表

| 用途 | 命令 |
|---|---|
| 查最近备份 Job | `kubectl get jobs -n opsmesh -l app.kubernetes.io/component=mysql-backup \| tail -5` |
| 查备份文件列表 | `kubectl exec -n opsmesh <job-pod> -- ls -lt /backup` |
| 查 CronJob 配置 | `kubectl get cronjob opsmesh-mysql-backup -n opsmesh -o yaml` |
| 手动触发备份 | `kubectl create job --from=cronjob/opsmesh-mysql-backup -n opsmesh manual-backup-$(date +%s)` |
| 查备份日志 | `kubectl logs -n opsmesh job/manual-backup-<ts>` |
| 缩容控制面 | `kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=0` |

### 7.2 联系人与升级路径

```
P3（备份 Job 单次失败）
  └─ SRE 自行处理，记录台账
P2（连续失败 / 备份体积异常）
  └─ SRE → 通知 OpsMesh Owner，1h 内响应
P1（备份超 26h 未成功 / MySQL 不可用）
  └─ 立即通知 Owner + DBA，启动 3.2 恢复，30min 内响应
S1（全中心灾难）
  └─ 启动 5.4 全中心切换，Owner 决策，4h 内恢复
```

> 联系人信息维护在团队通讯录（本文档不内嵌，避免过期）。

### 7.3 变更记录

| 版本 | 日期 | 变更 | 作者 |
|---|---|---|---|
| v0.1 | 2026-08-07 | 初版：基于 Helm Chart 备份 CronJob 建立备份/恢复/RTO-RPO/故障切换全流程 | OpsMesh Team |

**未来增强（规划中，非现网能力）**：

- MySQL 主从复制 + binlog 流式备份，RPO 从 24h 降至秒级。
- Velero 整集群备份，覆盖 PVC + 资源清单，简化全中心切换。
- 跨中心 active-active，消除单中心 RTO。