# OpsMesh 网段运维中枢 — 交付说明

> 版本：MVP（自研 gRPC 管控通道）·  数据刷新 2026-08-24（行数/包数/依赖数/功能矩阵实测更新，对齐 14 个功能域）·  仓库：https://github.com/Levango7/OpsMesh

## 1. 产品定位

私有化单中心 B/S 自动化部署与运维平台。核心差异：服务部署后，**整段网络打通的设备自动纳管**，各设备可并行执行各自自动化任务。

**管控通道（已冻结决策：自研 gRPC，2026-07-27）**：MVP 管控通道 = **自研 gRPC（direct + proxy）**。原"蓝鲸 GSE 社区版底座 / GSE 级联纳管"**移出 MVP、降格为可选增强**（未来超大规模级联再独立立项）。跨网段规模化改为「每段一套控制面 + agent 集群 + 控制面联邦 / 任务跨段转发」。

## 2. 代码规模（实测 2026-08-30 更新，含 services/ 18 微服务 + GPU/AIOps/ChatOps 新域）

> 统计口径：`git ls-files` 实测。主模块 + operator 子模块 + `services/` 18 个微服务子模块（各自独立 go.mod，2026-08-29 起新增）。
> 前端按 `web/enterprise/src/` 下 `.js` + `.vue` 统计（不含 `.json` i18n 资源）。

| 指标 | 数值（实测 2026-08-30） |
|------|----------------------|
| 仓库文件合计 | 1,121 |
| Go 文件合计 | **714**（含 265 测试文件） |
| Go 行数合计 | ~221,900（其中测试 ~99,050，占比约 44.6%） |
| 前端文件（`.js` + `.vue`） | 145（`web/enterprise/src/`） |
| 微服务子模块 | 18 个（`services/`，独立 go.mod，与主模块双轨并存，见 README「services/ 微服务目录」） |

> 较 2026-08-24 版本（346 Go 文件 / ~51,700 行）大幅增长：新增 `services/` 微服务化拆分（约 8 万行）、GPU 资源管理、AIOps 引擎、ChatOps、成本分摊、Terraform provider 等能力；规模统计随 25 提交增量演进刷新。

直接依赖清单（主模块 `go.mod` 非 indirect）：
- 数据/消息：`go-sql-driver/mysql`、`redis/go-redis/v9`、`segmentio/kafka-go`
- RPC/序列化：`google.golang.org/grpc`、`google.golang.org/protobuf`（已启用 protobuf 代码生成，stub 在 `internal/grpcx/pb/`）
- 安全/认证：`golang-jwt/jwt/v5`、`golang.org/x/crypto`
- 系统采集：`shirou/gopsutil/v3`
- K8s 客户端：`k8s.io/api`、`k8s.io/apimachinery`、`k8s.io/client-go`

operator 子模块额外引入 `sigs.k8s.io/controller-runtime`（K8s CRD 控制器框架）。`services/` 各微服务子模块依赖独立声明于各自 go.mod。

## 3. 全量验证结果（go1.26.0）

> Go 版本要求：主模块 `go.mod` 声明 `go 1.26.0`（toolchain `go1.26.6`）；operator 子模块声明 `go 1.22.0`（toolchain `go1.23.4`，待对齐，见 `docs/tech-debt.md` TD-29/TD-30）。构建需本机安装 Go ≥ 1.26.0。

| 命令 | 结果 |
|------|------|
| `go build ./...` | ✅ RC=0 |
| `go vet ./...` | ✅ RC=0 |
| `go test ./...` | ✅ RC=0（测试包 `ok`，无 DSN 时 SQL 测试自动 Skip） |

## 4. 功能矩阵（均已落地，14 个功能域）

> 14 个功能域对齐 `README.md` 功能矩阵、`docs/feature-design.md` F1–F18 功能模块编号、`docs/product-roadmap.md` M1–M4 里程碑编号。

### 4.1 14 个功能域交付清单

| # | 功能域 | 落地文件 / 入口 | 关键能力 |
|---|---|---|---|
| 1 | **设备管理** | `internal/controlplane/server_devices.go`、`internal/discover/`、`internal/provision/` | Agent 即设备 / 网段发现 / 候选纳管 / 退役归档 / SSH 推送 / 设备指纹 |
| 2 | **任务执行** | `internal/controlplane/server_tasks.go`、`internal/agent/`、`internal/cron/` | Shell/svc/file / 超时 / 重试+死信 / 取消 / 定时 cron / 批量 / 租约回收 / 审批门禁 |
| 3 | **监控告警** | `internal/controlplane/server_alerts.go`、`internal/alertengine/`、`internal/notify/` | 死信告警 / 规则引擎 / 静默+抑制+聚合 / 6 通道 / 通知模板 |
| 4 | **CMDB** | `internal/cmdb/`、`internal/controlplane/cmdb_*.go` | 模型+实例 CRUD+SQL+采集 / 关系图谱 / 变更审批 / 倒排索引 |
| 5 | **日志检索** | `internal/logstore/` | 双后端(Memory/SQL)+Loki/ES / offset 分页 / 倒排索引 / gRPC 上报 |
| 6 | **编排部署** | `internal/deploy/`、`internal/controlplane/server_deploy.go` | 计划+fan-out+Reconcile+Rollback / 三策略 / 灰度自适应 / 多集群联邦发布 |
| 7 | **OS 优化** | `internal/controlplane/os_optimize.go` | 14+ 预置模板 / 在线 CRUD / 在 agent 执行 / 幂等 seed |
| 8 | **中间件部署** | `internal/controlplane/middleware_deploy.go` | 10+ 中间件 × docker/systemd / CRUD / 实例查询 / 卸载 |
| 9 | **K8s 管理** | `internal/controlplane/k8s_cluster.go`、`k8s_manage.go`、`internal/k8s/` | 多集群接入 / 资源 CRUD / scale/restart/rollback / kubeconfig 加密 |
| 10 | **用户中心** | `internal/controlplane/auth*.go` | 注册/登录/RBAC / JWT 双 Token / 防爆破 / 注册审批 / 首登改密 |
| 11 | **审计日志** | `internal/controlplane/server_audits.go` | 100% 留痕 / 检索 / 等保三级 ≥6 月 / bootstrap 审计 |
| 12 | **联邦** | `internal/controlplane/federation.go`、`internal/deploy/federation.go` | 跨网段转发 / 设备视图聚合 / mTLS / HMAC 签名 / 多集群联邦发布 |
| 13 | **SSE 实时推送** | `internal/controlplane/sse.go`、`docs/sse-protocol.md` | 9 事件 / 心跳保活 / 契约守护测试 / 替代 5s 轮询 |
| 14 | **工作流** | `internal/orchestration/`、`internal/dag/` | DAG 引擎 / 子工作流 / 条件分支 / 节点级超时重试 / 执行历史回放 / 画布 |

### 4.2 运维场景化能力（增量，对应功能域）

| # | 能力 | 对应功能域 | 落地文件 / 入口 | 说明 |
|---|------|-----------|----------------|------|
| ⑦ | OS 基础环境优化 | 7 | `internal/controlplane/os_optimize.go` | 预置模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户）+ 在线 CRUD + 在指定 agent 执行；模板 store 持久化，幂等 seed |
| ⑧ | 中间件部署 | 8 | `internal/controlplane/middleware_deploy.go` | 10+ 中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/MongoDB/RabbitMQ/Elasticsearch）× docker/systemd 双模式 + CRUD + 实例查询 |
| ⑨ | K8s 集群与资源管理 | 9 | `internal/controlplane/k8s_cluster.go`、`k8s_manage.go`、`internal/k8s/` | 集群增删查 + 测试连接；资源只读/写（namespace/pod/deployment/service/configmap/secret/node）+ scale/restart/rollback；基于 client-go，无 kubectl 依赖；租户隔离 |
| ⑩ | 灰度发布 | 6 | `internal/deploy/`（model/handler/sql/store） | 三策略：rolling / canary（按权重分流）/ bluegreen（蓝绿切流）；发布门禁 Gate（失败率/延迟阈值）+ 自动回滚 + Promote 拥级 |
| ⑪ | 告警规则 | 3 | `internal/store/`（AlertRule CRUD）、`internal/controlplane/` | 基于指标阈值的告警触发规则（metric/op/threshold/for/severity/message）+ CRUD + 按租户隔离 |
| ⑫ | 作业审批 | 2、14 | `internal/controlplane/server.go`（handleApproveTask/handleRejectTask）、`internal/store/sql.go` | 高风险任务 `pending_approval` 状态 + approve/reject 端点 + 审批人记录 + 越权防护 |
| ⑬ | 用户注册审批 | 10 | `internal/controlplane/auth.go`、`internal/config/config.go` | 注册安全：公开注册开关 + pending 审批流程 + approve/reject + 失败锁账号 |

### 4.3 K8s Operator（独立子模块）

`operator/`（独立 `go.mod`，module `opsmesh/operator`）：基于 `controller-runtime` 的 K8s CRD 控制器，将 OpsMesh 自定义资源纳入 K8s 原生 Reconcile 循环。

### 4.4 其余已落地能力（横切）

定时/周期调度、失败重试+死信队列、设备自动退役(F5)、自动纳管令牌闭环、多副本保护+agent 多控制面 failover+真 HA leader 选举、生产基线、前端壳层重构、Prometheus 指标 + /healthz + /readyz、OTel 链路追踪、多阶段 Dockerfile + CI(gosec/Trivy/golangci-lint/-race)。**Helm Chart（`deploy/helm/opsmesh/`）已落地可用**，systemd 部署资产齐全（`deploy/systemd/`），docker-compose 一键起栈，Argo CD ApplicationSet 网段批量渲染仍属规划中。

## 5. 网络分区（CIDR）

```
mgmt-net  10.20.0.0/24
data-net  10.21.0.0/24
soc-net   10.22.0.0/24
seg-net   10.30.0.0/16
```

## 6. 构建 / 运行 / 部署

```bash
# 构建（双模式二进制：controlplane / agent，由 --mode 切换）
go build -o opsmesh ./cmd/opsmesh

# 运行控制面
./opsmesh --mode=controlplane

# 运行 agent（向控制面注册）
./opsmesh --mode=agent --control-addr=<addr> --install-token=<token>

# 完整校验
go build ./... && go vet ./... && go test ./...
```

- 容器：`Dockerfile`（controlplane，多阶段）+ `Dockerfile.agent`（agent，多阶段），已含 gosec/Trivy 扫描门禁
- 编排：**Helm Chart 已提供**（`deploy/helm/opsmesh/`，含 values / values-production overlay），可直接 `helm install`；Argo CD GitOps 仓库规划中。非 Helm 路径仍可用 Dockerfile + docker-compose（controlplane 用 `Dockerfile`，agent 用 `Dockerfile.agent`）

## 7. CI 状态

`.github/workflows/ci.yml` 已加固配置：
- `integration` job：mysql:8 + redis:7 service container，注入 `OPSMESH_TEST_MYSQL_DSN` / `OPSMESH_TEST_REDIS_ADDR` 跑 `go test -race`（启用竞态检测，SQL 集成测试真正执行）
- `lint` job：`golangci-lint`（启用 revive/govet/staticcheck/errcheck 等），失败即阻断
- `security` job：`gosec`（钉版本，避免上游 breaking）+ `Trivy`（`--exit-code 1`，发现漏洞即失败）
- `image` job：构建镜像并把 chart `global.image.tag` 钉死为本 commit sha（含占位符守卫）
- 覆盖率：`go test -cover` 产出 coverage profile，上传为 CI artifact（阈值门禁规划中）

⚠️ **当前状态：CI 集成测试 + 安全扫描 + lint + race 检测需 GitHub Actions runner 真跑，沙箱无法验证，标记为「阻塞·待外部」**。CI yaml 已静态核查合理，待下次 push 触发 GitHub Actions 生效。

## 8. 仓库

- 远端：`github.com/Levango7/OpsMesh`，分支 `main`
- 根提交链：以 `git rev-list --count HEAD` 实测为准（初始 README → 内核实现 → 六大运维模块 → CI/容器加固 → 文档同步，持续演进中）
- 提交内容：34 包源码（主模块 31 + operator 3）+ 346 Go 文件（179 源码 + 167 测试）+ 84 前端文件 + Dockerfile/Dockerfile.agent + docker-compose + Helm Chart + systemd unit + README + DELIVERY + CHANGELOG + 23 个设计文档 + `.github/ci.yml` + `.gitignore`

---
## 9. 生产安全加固

在 MVP 基线之上，针对"上线即崩 / 越权 / 爆破 / 伪造"风险追加了企业级加固（代码位于 `internal/controlplane`、`internal/tlsutil`、`internal/store`）：

| 编号 | 加固项 | 落地文件 / 入口 |
|---|---|---|
| | RBAC 持久化三表 + 种子（修复 mysql 后端启动即 panic、HA 多副本身份一致） | `internal/store/sql.go`（`initSchema` 建表 + `seedRBAC`）、`internal/store/sql_rbac.go` |
| | HTTP / gRPC 兜底恢复（handler panic 不再拖垮控制面） | `internal/controlplane/server.go`（`recoveryMiddleware` + `grpcRecoveryInterceptor`） |
| | 请求体限流（1 MiB，统一 `decodeJSONBody`） | `internal/controlplane/server.go`（`MaxBytesReader`）、`auth.go` 全部解码调用 |
| | 登录防爆破 + 失败锁账号（令牌桶 + 5 次锁 15min） | `internal/controlplane/auth.go`（`loginGuard`） |
| | metrics CIDR 白名单 + bootstrap 端点审计 | `internal/controlplane/server.go`（`metricsAllowed`）、`config.go`（`--metrics-allow-cidr`） |
| | 联邦 mTLS + HMAC 转发签名验签（防伪造/重放） | `internal/controlplane/server.go`（`buildFederationServer`/`verifyFederationRequest`）、`federation.go`（`signFederationRequest`）、`tlsutil.go`（`HTTPServerTLSConfig`/`HTTPClientTLSConfig`）、`config.go`（6 个 `--federation-*` flag） |

### 验证状态

- ⚠️ **本地构建/测试**：以上改动需在用户本机（Go ≥ 1.26.0）或 CI runner 执行：
  ```bash
  go build ./... && go vet ./... && go test -timeout 300s ./...
  ```
  其中 `internal/controlplane/federation_test.go` 已同步更新为新四参数签名 `NewFederationManager(peers, store, secret, tlsConfig)`。
- ⚠️ **CI 集成/安全扫描**：`integration`（`-race` + MySQL/Redis service container）、`security`（gosec/Trivy）、`lint`（golangci-lint）仍需 GitHub Actions runner 真跑，标记「阻塞·待外部」。

---

*本说明随 MVP 交付生成；后续若启用蓝鲸 GSE 级联增强，将单独立项并更新本文档。*
