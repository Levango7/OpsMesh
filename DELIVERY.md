# OpsMesh 网段运维中枢 — 交付说明

> 版本：MVP（ADR-001 Option A）·  数据刷新 2026-08-11（行数/包数/依赖数/功能矩阵实测校准）·  仓库：https://github.com/Levango7/OpsMesh

## 1. 产品定位

私有化单中心 B/S 自动化部署与运维平台。核心差异：服务部署后，**整段网络打通的设备自动纳管**，各设备可并行执行各自自动化任务。

**管控通道（已冻结决策 ADR-001 Option A，2026-07-27）**：MVP 管控通道 = **自研 gRPC（direct + proxy）**。原"蓝鲸 GSE 社区版底座 / GSE 级联纳管"**移出 MVP、降格为可选增强**（未来超大规模级联再独立立项）。跨网段规模化改为「每段一套控制面 + agent 集群 + 控制面联邦 / 任务跨段转发」。

## 2. 代码规模（实测 2026-08-12 更新，含六轮重构收敛 + 个人版前端移除）

> 统计口径：排除 `.gocache`、`node_modules`、`internal/controlplane/web/`（个人版前端已于收敛为引导页，不再计入 Go 源码）；按 Go 模块分别统计后合计。主模块 `opsmesh`（go.mod 根）+ operator 子模块 `opsmesh/operator`（独立 go.mod，K8s Operator）。

| 指标 | 主模块 opsmesh | operator 子模块 | 合计 |
|------|---------------|----------------|------|
| Go 包 | 30（1 cmd + 29 internal） | 3 | 33 |
| 源码文件 | 155 | 6 | 161 |
| 源码行数 | 43,573 | 1,141 | 44,714 |
| 测试文件 | 96 | 1 | 97 |
| 测试行数 | 29,654 | 164 | 29,818（占比约 39.8%） |
| 直接依赖 | 11 | 4（3 个与主模块共享） | 12（去重） |

> 数值较 2026-08-11 版本增长：新增 SSE 契约守护测试、e2e-real spec、OS/中间件模板拆域等；同时个人版前端 1.3 万行 JS 已从仓库移除（不计入 Go 口径）。

直接依赖清单（主模块 `go.mod` 非 indirect）：
- 数据/消息：`go-sql-driver/mysql`、`redis/go-redis/v9`、`segmentio/kafka-go`
- RPC/序列化：`google.golang.org/grpc`、`google.golang.org/protobuf`（已启用 protobuf 代码生成，stub 在 `internal/grpcx/pb/`）
- 安全/认证：`golang-jwt/jwt/v5`、`golang.org/x/crypto`
- 系统采集：`shirou/gopsutil/v3`
- K8s 客户端：`k8s.io/api`、`k8s.io/apimachinery`、`k8s.io/client-go`

operator 子模块额外引入 `sigs.k8s.io/controller-runtime`（K8s CRD 控制器框架）。

## 3. 全量验证结果（go1.26.0）

> Go 版本要求：主模块 `go.mod` 声明 `go 1.26.0`；operator 子模块声明 `go 1.22.0`（toolchain `go1.23.4`）。构建需本机安装 Go ≥ 1.26.0。

| 命令 | 结果 |
|------|------|
| `go build ./...` | ✅ RC=0 |
| `go vet ./...` | ✅ RC=0 |
| `go test ./...` | ✅ RC=0（测试包 `ok`，无 DSN 时 SQL 测试自动 Skip） |

## 4. 功能矩阵（均已落地）

### 4.1 六大运维模块（MVP 基线）

| # | 模块 | 说明 |
|---|------|------|
| ① | 运维中枢（任务执行） | 下发 / 批量 / 生命周期 / 租约回收 / 取消 |
| ② | 配置库 CMDB | Phase1 模型+CRUD+SQL+采集 |
| ③ | 服务部署 M3 | 计划 + fan-out 执行 + Reconcile + Rollback |
| ④ | 日志检索 M6 | logstore 双后端(Memory/SQL) + 查询 API + offset 分页 |
| ⑤ | 监控告警 M7 | 规则引擎 + alert(Status/Ack/Silence) + ack/silence 端点 + Webhook/飞书/钉钉 |
| ⑥ | 作业编排 M5 | DAG 引擎 + store 阻塞→释放链路 + 画布 |

### 4.2 运维场景化能力（增量）

| # | 能力 | 落地文件 / 入口 | 说明 |
|---|------|----------------|------|
| ⑦ | OS 基础环境优化 | `internal/controlplane/os_optimize.go` | 预置模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户）+ 在线 CRUD + 在指定 agent 执行；模板 store 持久化，幂等 seed |
| ⑧ | 中间件部署 | `internal/controlplane/middleware_deploy.go` | 10+ 中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/MongoDB/RabbitMQ/Elasticsearch）× docker/systemd 双模式 + CRUD + 实例查询 |
| ⑨ | K8s 集群与资源管理 | `internal/controlplane/k8s_cluster.go`、`k8s_manage.go`、`internal/k8s/` | 集群增删查 + 测试连接；资源只读/写（namespace/pod/deployment/service/configmap/secret/node）+ scale/restart/rollback；基于 client-go，无 kubectl 依赖；租户隔离 |
| ⑩ | 灰度发布 | `internal/deploy/`（model/handler/sql/store） | 三策略：rolling / canary（按权重分流）/ bluegreen（蓝绿切流）；发布门禁 Gate（失败率/延迟阈值）+ 自动回滚 + Promote 晋级 |
| ⑪ | 告警规则 | `internal/store/`（AlertRule CRUD）、`internal/controlplane/` | 基于指标阈值的告警触发规则（metric/op/threshold/for/severity/message）+ CRUD + 按租户隔离 |
| ⑫ | 作业审批 | `internal/controlplane/server.go`（handleApproveTask/handleRejectTask）、`internal/store/sql.go` | 高风险任务 `pending_approval` 状态 + approve/reject 端点 + 审批人记录 + 越权防护 |
| ⑬ | 用户注册审批 | `internal/controlplane/auth.go`、`internal/config/config.go` | P1-7 注册安全：公开注册开关 + pending 审批流程 + approve/reject + 失败锁账号 |

### 4.3 K8s Operator（独立子模块）

`operator/`（独立 `go.mod`，module `opsmesh/operator`）：基于 `controller-runtime` 的 K8s CRD 控制器，将 OpsMesh 自定义资源纳入 K8s 原生 Reconcile 循环。

### 4.4 其余已落地能力

定时/周期调度、失败重试+死信队列、设备自动退役(F5)、B1 自动纳管令牌闭环、A3 多副本保护+agent 多控制面 failover+真 HA leader 选举、A4 生产基线、前端壳层重构、Prometheus 指标 + /healthz、多阶段 Dockerfile + CI(gosec/Trivy/golangci-lint/-race)。**Helm Chart（`deploy/helm/opsmesh/`）已落地可用**，Argo CD ApplicationSet 网段批量渲染仍属规划中。

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
- 根提交链：55 commits（初始 README → 内核实现 → 六大运维模块 → CI/容器加固 → 文档同步）
- 提交内容：28 包源码（主模块 25 + operator 3）+ 62 测试 + Dockerfile/Dockerfile.agent + docker-compose + README + DELIVERY + `.github/ci.yml` + `.gitignore`

---
## 9. 生产安全加固（P0/P1）

在 MVP 基线之上，针对"上线即崩 / 越权 / 爆破 / 伪造"风险追加了企业级加固（代码位于 `internal/controlplane`、`internal/tlsutil`、`internal/store`）：

| 编号 | 加固项 | 落地文件 / 入口 |
|---|---|---|
| P0-1 | RBAC 持久化三表 + 种子（修复 mysql 后端启动即 panic、HA 多副本身份一致） | `internal/store/sql.go`（`initSchema` 建表 + `seedRBAC`）、`internal/store/sql_rbac.go` |
| P0-2 | HTTP / gRPC 兜底恢复（handler panic 不再拖垮控制面） | `internal/controlplane/server.go`（`recoveryMiddleware` + `grpcRecoveryInterceptor`） |
| P1-2 | 请求体限流（1 MiB，统一 `decodeJSONBody`） | `internal/controlplane/server.go`（`MaxBytesReader`）、`auth.go` 全部解码调用 |
| P1-3 | 登录防爆破 + 失败锁账号（令牌桶 + 5 次锁 15min） | `internal/controlplane/auth.go`（`loginGuard`） |
| P1-5 | metrics CIDR 白名单 + bootstrap 端点审计 | `internal/controlplane/server.go`（`metricsAllowed`）、`config.go`（`--metrics-allow-cidr`） |
| P1-6 | 联邦 mTLS + HMAC 转发签名验签（防伪造/重放） | `internal/controlplane/server.go`（`buildFederationServer`/`verifyFederationRequest`）、`federation.go`（`signFederationRequest`）、`tlsutil.go`（`HTTPServerTLSConfig`/`HTTPClientTLSConfig`）、`config.go`（6 个 `--federation-*` flag） |

### 验证状态

- ⚠️ **本地构建/测试**：以上改动需在用户本机（Go ≥ 1.26.0）或 CI runner 执行：
  ```bash
  go build ./... && go vet ./... && go test -timeout 300s ./...
  ```
  其中 `internal/controlplane/federation_test.go` 已同步更新为新四参数签名 `NewFederationManager(peers, store, secret, tlsConfig)`。
- ⚠️ **CI 集成/安全扫描**：`integration`（`-race` + MySQL/Redis service container）、`security`（gosec/Trivy）、`lint`（golangci-lint）仍需 GitHub Actions runner 真跑，标记「阻塞·待外部」。

---

*本说明随 MVP 交付生成；后续若启用蓝鲸 GSE 级联增强，将单独立项并更新本文档。*
