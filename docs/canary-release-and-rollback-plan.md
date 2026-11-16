# OpsMesh 灰度发布与回滚预案

> 版本：v1.0 · 编制：2026-11-16 · 责任线：kilo（lead）
> 适用范围：控制面（Helm release `opsmesh` / 命名空间 `opsmesh`）、任务域灰度、联邦链路
> 配套：[upgrade-guide.md](./upgrade-guide.md)（迁移与维护窗口）、[dr-runbook.md](./dr-runbook.md)（备份恢复）、[upgrade-rehearsal-2026-10-27.md](./upgrade-rehearsal-2026-10-27.md)（演练实测）

---

## 1. 两层灰度模型

OpsMesh 的"灰度"有两个独立维度，必须分开决策，不允许混用同一个百分比：

| 层 | 对象 | 控制面 | 生效粒度 | 回滚速度 |
|---|---|---|---|---|
| **L1 应用内灰度** | 任务/批量任务（canary release） | `POST /api/v1/canary/{id}/traffic-split` | 按任务流量百分比 | 秒级 |
| **L2 部署层灰度** | controlplane / agent 二进制 | Helm rolling update / 副本分批 | 按 Pod 副本 | 分钟级 |

L1 用于"新执行逻辑只影响 X% 任务"；L2 用于"新版本控制面只承载 X% 副本流量"。
两者同时进行时必须先冻结 L1（百分比保持不变），只调 L2，避免故障归因不可分离。

---

## 2. 发布前准入判据（全部满足才允许进入灰度）

### 2.1 代码与构建
- [ ] `go build ./...`、`go vet ./...`、`gofmt -l .` 全绿
- [ ] `go test ./...` 全绿（含 `-race` CI 腿）
- [ ] `npx eslint src`、`npm run build`、`npx vitest run` 全绿
- [ ] 发布物回核：`bash deploy/scripts/verify-release-artifacts.sh` 通过

### 2.2 数据与迁移
- [ ] 已完成备份（`dr-runbook.md` 第 2 章流程；最近一次备份时间 < 24h）
- [ ] 迁移规模判据已核对（`upgrade-guide.md` §1：`ci_items` 行数/字节数）
- [ ] 无长事务持表（`upgrade-guide.md` §1.5 的 `information_schema.innodb_trx` 查询返回 0 行）
- [ ] 迁移演练通过：`bash deploy/scripts/upgrade-rehearsal.sh --selftest`

### 2.3 可观测性
- [ ] 基线已采样（发布前 30 分钟）：5xx 率、P99、任务失败率、agent 失联率
- [ ] 告警通道在线（SSE/告警页可收到测试告警）
- [ ] 回滚负责人、观察窗口、决策时间点已书面确认

---

## 3. L1 应用内灰度（任务域）

### 3.1 创建与推进

```bash
# 1) 创建灰度发布（返回 canary id；任务域路由）
curl -sS -X POST "$CP/api/v1/tasks/canary" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT" \
  -d '{"name":"release-<ver>","baseline":"<old-image>","canary":"<new-image>"}'

# 2) 设置初始流量（建议从 5% 开始，不要直接 30%）
curl -sS -X POST "$CP/api/v1/canary/$CANARY_ID/traffic-split" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT" \
  -d '{"percentage":5}'

# 3) 观察指标（基准 vs 灰度）
curl -sS "$CP/api/v1/canary/$CANARY_ID/metrics" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT"

# 4) 通过判据后逐级推进：5% → 20% → 50% → 100%
#    每次推进后至少观察一个完整业务周期（默认 30 分钟）
curl -sS -X POST "$CP/api/v1/tasks/canary/$CANARY_ID/advance" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT"
```

> 鉴权：灰度接口要求 `task:write`；读取指标要求 `task:read`。跨租户访问一律 404（不泄露存在性）。

### 3.2 推进/回滚判据

| 指标 | 推进（继续加比例） | 暂停（保持当前比例） | 回滚（percentage → 0） |
|---|---|---|---|
| 灰度组任务失败率 | ≤ 基线 + 1pp | 基线 +1~3pp 且持续 10min | > 基线 +3pp 或出现死信激增 |
| P99 任务时延 | ≤ 基线 ×1.2 | 基线 ×1.2~1.5 | > 基线 ×1.5 |
| 5xx 率 | ≤ 0.1% | 0.1%~0.5% | > 0.5% |
| agent 失联率 | 无变化 | 上升 < 2pp | 上升 ≥ 2pp |
| 审计异常（authz_denied 突增） | 无变化 | 上升但可解释 | 无法解释的突增 |

回滚动作（秒级，不重启）：

```bash
curl -sS -X POST "$CP/api/v1/canary/$CANARY_ID/traffic-split" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT" \
  -d '{"percentage":0}'
```

回滚后必须：保留 canary 记录 ≥ 24h、导出 metrics 快照、在事故记录中登记时间线。

---

## 4. L2 部署层灰度（控制面 / Agent）

### 4.1 分批策略

| 批次 | 副本比例 | 观察窗口 | 准入 |
|---|---|---|---|
| B0 | 1 个副本（金丝雀 Pod） | 30 min | 健康检查 + 冒烟 + 无 5xx 突增 |
| B1 | 25% | 30 min | B0 判据 + 任务成功率达到基线 |
| B2 | 50% | 30 min | B1 判据 |
| B3 | 100% | 持续观察 2h | 全量判据 |

Helm 分批（示例，具体副本数按 values-production.yaml）：

```bash
# 预演（不落地）
helm upgrade opsmesh deploy/helm/opsmesh -n opsmesh \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.image.tag=<new-ver> --dry-run

# B0：先只升级 1 个副本（maxSurge=1 / maxUnavailable=0）
helm upgrade opsmesh deploy/helm/opsmesh -n opsmesh \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.image.tag=<new-ver> \
  --set controlplane.replicaCount=1 --wait --timeout 10m

# 校验
kubectl rollout status deploy/opsmesh-controlplane -n opsmesh
bash deploy/scripts/verify-runtime.sh   # 运行时健康/接口抽检
```

### 4.2 部署层回滚判据

出现以下任一条件，立即执行第 5 章 R2：

- 新 Pod readiness 连续失败 ≥ 5 分钟
- `/readyz` 因选主/存储失败返回 503 且无法自愈
- 5xx 率 > 0.5% 持续 5 分钟
- 任务领取停滞（队列 pending 增长且 agent 在线）
- 联邦验签失败率突增（nonce/skew 相关 403）

---

## 5. 回滚预案（按影响面从小到大）

### R1 流量回滚（秒级，首选）
适用于：L1 灰度逻辑异常、单任务类型失败率升高。
动作：`traffic-split` → `0`；确认新任务不再进入灰度组；保留现场。

### R2 版本回滚（分钟级）
适用于：L2 部署后控制面自身异常（崩溃、readyz 失败、5xx 激增）。

```bash
# 查看历史版本
helm history opsmesh -n opsmesh

# 回滚到上一个稳定 revision
helm rollback opsmesh <REVISION> -n opsmesh --wait --timeout 10m

# 或仅回滚 Deployment（若使用 GitOps 管理，走 Git revert 由 ArgoCD 同步）
kubectl rollout undo deploy/opsmesh-controlplane -n opsmesh
kubectl rollout status  deploy/opsmesh-controlplane -n opsmesh

# 回滚后校验
bash deploy/scripts/verify-runtime.sh
```

判据：回滚后 10 分钟内 `/readyz` 200、5xx 回落至基线、任务队列恢复吞吐。

### R3 数据层回滚（最后手段，须 Owner 批准）
OpsMesh 迁移是**前向修复**模型：不提供 down migration。

1. **优先前向修复**：新版本写入的数据若与旧版本不兼容，发布 hotfix 而不是回退库。
2. **确需恢复**：按 `dr-runbook.md` 第 3 章从最近备份恢复；恢复前必须：
   - 停止控制面写入（防止恢复期间双写）
   - 记录当前 `schema_migrations` 版本号
   - 评估恢复点之后的数据丢失（RPO）
3. 恢复后重放迁移：`bash deploy/scripts/upgrade-rehearsal.sh` 同款判据验证"升级库 ≡ 新装库"。

> 禁止：直接 `DROP` 新版本新增列/表来"回退 schema"。这会破坏后续前向迁移的幂等性。

### R4 联邦链路熔断（秒级）
适用于：跨控制面调用异常、对端不可信、验签异常风暴。

```bash
# 关闭出站联邦：清空 peers 并重启（federation 路由随之为 404）
helm upgrade opsmesh deploy/helm/opsmesh -n opsmesh \
  --set controlplane.federation.peers="" --reuse-values
```

效果：本端不再向对端转发；对端入站调用因签名校验被拒（403）。恢复时按原 peers 列表回填。

---

## 6. 演练与复验

| 演练 | 命令/方式 | 频率 | 通过判据 |
|---|---|---|---|
| 升级演练（迁移/幂等/结构等价） | `bash deploy/scripts/upgrade-rehearsal.sh --rows 200000` | 每次发布前 | rc=0，四段判据全 PASS |
| 运行时健康 | `bash deploy/scripts/verify-runtime.sh` | 每批次后 | 全接口 PASS |
| 部署资产校验 | `bash deploy/scripts/validate-deploy-assets.sh` | 每次发布前 | PASS（当前基线 79 项） |
| L1 灰度回滚 | 演练环境把 percentage 从 30 → 0 | 每季度 | 新任务 0% 进入灰度组，延迟 < 5s |
| R2 版本回滚 | 演练环境 `helm rollback` 前后各一版 | 每季度 | 10 分钟内恢复 |
| 联邦熔断 | 清空 peers 后重启 | 每半年 | 联邦路由 404，主链路不受影响 |

---

## 7. 记录与复盘

每次灰度/回滚必须留存：

1. 发布版本、镜像 digest、Helm revision
2. 各阶段比例与时间戳
3. 指标快照（灰度 vs 基准）
4. 决策人与决策依据（命中哪条判据）
5. 若回滚：根因、影响面、后续修复项（登记到 `docs/tech-debt.md` 或 issue）

复盘在回滚后 48h 内完成，结论回写本文件"判据"章节（阈值不合理就改阈值，不靠口头约定）。
