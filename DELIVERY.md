# OpsMesh 网段运维中枢 — 交付说明

> 版本：MVP（ADR-001 Option A）  ·  交付日期：2026-07-28  ·  仓库：https://github.com/Levango7/OpsMesh

## 1. 产品定位

私有化单中心 B/S 自动化部署与运维平台。核心差异：服务部署后，**整段网络打通的设备自动纳管**，各设备可并行执行各自自动化任务。

**管控通道（已冻结决策 ADR-001 Option A，2026-07-27）**：MVP 管控通道 = **自研 gRPC（direct + proxy）**。原"蓝鲸 GSE 社区版底座 / GSE 级联纳管"**移出 MVP、降格为可选增强**（未来超大规模级联再独立立项）。跨网段规模化改为「每段一套控制面 + agent 集群 + 控制面联邦 / 任务跨段转发」。

## 2. 代码规模（实测 2026-07-28）

| 指标 | 数值 |
|------|------|
| Go 包 | 19（1 cmd + 18 internal） |
| 源码文件 | 36（约 6,848 行） |
| 测试文件 | 21（约 2,372 行，占比 25.7%） |
| 外部依赖 | 5（mysql / redis / kafka / grpc / crypto），零 protobuf 代码生成 |

## 3. 全量验证结果（go1.22.12）

| 命令 | 结果 |
|------|------|
| `go build ./...` | ✅ RC=0 |
| `go vet ./...` | ✅ RC=0 |
| `go test ./...` | ✅ RC=0（17 个测试包 `ok`，4 个无测试文件；无 DSN 时 SQL 测试自动 Skip） |

## 4. 六大运维模块（均已落地）

| # | 模块 | 说明 |
|---|------|------|
| ① | 运维中枢（任务执行） | 下发 / 批量 / 生命周期 / 租约回收 / 取消 |
| ② | 配置库 CMDB | Phase1 模型+CRUD+SQL+采集 |
| ③ | 服务部署 M3 | 计划 + fan-out 执行 + Reconcile + Rollback |
| ④ | 日志检索 M6 | logstore 双后端(Memory/SQL) + 查询 API + offset 分页 |
| ⑤ | 监控告警 M7 | 规则引擎 + alert(Status/Ack/Silence) + ack/silence 端点 + Webhook/飞书/钉钉 |
| ⑥ | 作业编排 M5 | DAG 引擎 + store 阻塞→释放链路 + 画布 |

其余已落地能力：定时/周期调度、失败重试+死信队列、设备自动退役(F5)、B1 自动纳管令牌闭环、A3 多副本保护+agent 多控制面 failover+真 HA leader 选举、A4 生产基线、前端壳层重构、Prometheus 指标 + /healthz、多阶段 Dockerfile + CI(gosec/Trivy)、Helm Charts + Argo CD。

## 5. 网络分区（CIDR）

```
mgmt-net  10.20.0.0/24
data-net  10.21.0.0/24
soc-net   10.22.0.0/24
seg-net   10.30.0.0/16
```

## 6. 构建 / 运行 / 部署

```bash
# 构建（双模式二进制：controlplane / agent，由子命令或参数切换）
go build -o opsmesh ./cmd/opsmesh

# 运行控制面
./opsmesh controlplane --config config/controlplane.yaml

# 运行 agent（向控制面注册）
./opsmesh agent --controlplane <addr> --token <provision-token>

# 完整校验
go build ./... && go vet ./... && go test ./...
```

- 容器：`Dockerfile`（多阶段，已含 gosec/Trivy 扫描门禁）
- 编排：`opsmesh-gitops/`（Helm Charts：controlplane / middleware）+ Argo CD

## 7. CI 状态

`.github/workflows/ci.yml` 已配置：
- `integration` job：mysql:8 + redis:7 service container，注入 `OPSMESH_TEST_MYSQL_DSN` / `OPSMESH_TEST_REDIS_ADDR` 跑 `go test`（SQL 集成测试真正执行）
- `security` job：gosec + Trivy 安全扫描
- `image` job：构建镜像并把 chart `global.image.tag` 钉死为本 commit sha（含占位符守卫）

⚠️ **当前状态：CI 集成测试 + 安全扫描需 GitHub Actions runner 真跑，沙箱无法验证，标记为「阻塞·待外部」**。CI yaml 已静态核查合理，待下次 push 触发 GitHub Actions 生效。

## 8. 仓库

- 远端：`github.com/Levango7/OpsMesh`，分支 `main`
- 根提交链：`889c05f`（初始 README）→ `0ead461`（合并提交，含全部 90 文件）
- 提交内容：19 包源码 + 21 测试 + Dockerfile + README + `.github/ci.yml` + `.gitignore`

---
*本说明随 MVP 交付生成；后续若启用蓝鲸 GSE 级联增强，将单独立项并更新本文档。*
