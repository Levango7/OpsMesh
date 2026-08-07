# OpsMesh 部署指南

本文档描述 OpsMesh 在各类环境下的部署方式，从零依赖快速体验到生产级高可用集群。

## 目录

- [快速开始（docker-compose 一键启动）](#快速开始docker-compose-一键启动)
- [Docker 部署](#docker-部署)
- [Systemd 部署](#systemd-部署)
- [Helm 部署（Kubernetes）](#helm-部署kubernetes)
- [K8s Operator 部署](#k8s-operator-部署)
- [生产环境检查清单](#生产环境检查清单)
- [多租户部署](#多租户部署)
- [联邦部署](#联邦部署)

---

## 快速开始（docker-compose 一键启动）

仓库根目录 `docker-compose.yaml` 提供 controlplane + agent + mysql + redis 一键起环境，适合开发/演示。

### 前置条件

- Docker 20.10+ 与 Docker Compose v2
- 端口 8080 / 9090 / 9091 未被占用

### 启动

```bash
# 在仓库根目录
docker compose up -d

# 查看状态
docker compose ps

# 访问仪表盘
# 浏览器打开 http://localhost:8080
```

### 服务拓扑

```
controlplane (HTTP 8080 / gRPC 9090 / metrics 9091) → mysql:3306 + redis:6379
agent → controlplane:8080
```

### 配置说明

`docker-compose.yaml` 关键配置项（开发默认值，生产请用 Helm + values-production.yaml）：

| 配置 | 默认值 | 说明 |
|------|--------|------|
| `--store` | mysql | 持久化后端 |
| `--mysql-dsn` | opsmesh:opsmesh@tcp(mysql:3306)/opsmesh | MySQL 连接 |
| `--redis-addr` | redis:6379 | Redis 地址 |
| `--advertise-addr` | http://controlplane:8080 | 控制面对外地址 |
| `OPSMESH_JWT_SECRET` | `${OPSMESH_JWT_SECRET:-}` | JWT 密钥（空=dev 随机兜底） |
| `OPSMESH_COOKIE_SECURE` | false | Cookie Secure（开发明文 HTTP 需要 false） |
| `OPSMESH_PUBLIC_REGISTER` | true | 公开注册（开发开放） |
| `OPSMESH_ALLOW_PUBLIC_REGISTER` | false | 免审批注册（安全基线关闭） |

### 停止与清理

```bash
docker compose down          # 停止服务
docker compose down -v       # 停止并清理数据卷
```

### 自定义 JWT 密钥

```bash
export OPSMESH_JWT_SECRET=$(openssl rand -hex 32)
docker compose up -d
```

---

## Docker 部署

### Dockerfile 说明

仓库提供两个多阶段 Dockerfile：

| 文件 | 用途 | Runtime 基础镜像 | 特点 |
|------|------|------------------|------|
| `Dockerfile` | 控制面 | `gcr.io/distroless/static-debian12` | 无 shell、无包管理器，攻击面最小；以 nonroot(65532) 运行；内置 `--health` 子命令探活 |
| `Dockerfile.agent` | agent | `base-debian12`（含 sh） | agent 需 sh 执行 shell/service 任务 |

### 构建与运行

```bash
# 构建控制面镜像
docker build -t opsmesh/controlplane:latest .

# 构建含 kafka 事件总线的镜像
docker build --build-arg BUILD_TAGS=kafka -t opsmesh/controlplane:kafka .

# 构建 agent 镜像
docker build -f Dockerfile.agent -t opsmesh/agent:latest .

# 运行控制面（零依赖 memory store）
docker run -d --name opsmesh-cp \
  -p 8080:8080 -p 9090:9090 -p 9091:9091 \
  opsmesh/controlplane:latest \
  --mode=controlplane --store=memory

# 运行 agent
docker run -d --name opsmesh-agent \
  --network host \
  opsmesh/agent:latest \
  --mode=agent --control-addr=http://127.0.0.1:8080 --segment=seg-a
```

### 健康检查

distroless 镜像无 curl/wget，使用二进制内置 `--health` 子命令探活：

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/opsmesh", "--health"]
```

`--health` 在 config.Load 之前短路，GET `http://localhost:8080/healthz`，200 → exit 0，否则 exit 1。

### TLS 证书挂载

```bash
docker run -d --name opsmesh-cp \
  -p 8080:8080 -p 9090:9090 \
  -v ./tls/server.crt:/etc/opsmesh/tls/server.crt:ro \
  -v ./tls/server.key:/etc/opsmesh/tls/server.key:ro \
  opsmesh/controlplane:latest \
  --mode=controlplane --store=mysql \
  --mysql-dsn="user:pass@tcp(mysql:3306)/opsmesh" \
  --tls-cert=/etc/opsmesh/tls/server.crt \
  --tls-key=/etc/opsmesh/tls/server.key \
  --production
```

---

## Systemd 部署

适合物理机/VM 裸金属部署。仓库提供 `deploy/systemd/` 下的 service 文件与环境变量模板。

### 文件清单

| 文件 | 说明 |
|------|------|
| `opsmesh-controlplane.service` | 控制面 systemd unit |
| `opsmesh-controlplane.env` | 控制面环境变量模板（EnvironmentFile 加载） |
| `opsmesh-agent.service` | agent systemd unit |

### 安装步骤

```bash
# 1. 创建用户与目录
useradd -r -s /usr/sbin/nologin opsmesh
install -d -m 0750 /etc/opsmesh /var/lib/opsmesh /var/log/opsmesh

# 2. 放置二进制
install -m 0755 opsmesh /usr/local/bin/opsmesh

# 3. 放置配置
install -m 0640 deploy/systemd/opsmesh-controlplane.env /etc/opsmesh/
install -m 0644 deploy/systemd/opsmesh-controlplane.service /etc/systemd/system/
install -m 0644 deploy/systemd/opsmesh-agent.service /etc/systemd/system/

# 4. 编辑环境变量（填入实际 MySQL/Redis/JWT 等）
vim /etc/opsmesh/opsmesh-controlplane.env

# 5. 启用并启动
systemctl daemon-reload
systemctl enable --now opsmesh-controlplane
```

### opsmesh-controlplane.service 要点

```ini
[Unit]
Description=OpsMesh ControlPlane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/opsmesh --mode=controlplane
EnvironmentFile=/etc/opsmesh/opsmesh-controlplane.env
Restart=always
RestartSec=5
LimitNOFILE=65536
User=opsmesh
Group=opsmesh

[Install]
WantedBy=multi-user.target
```

### opsmesh-controlplane.env 关键配置

环境变量模板已注释全部安全相关项，按需取消注释并填值：

```bash
# 存储后端
OPSMESH_STORE=mysql
OPSMESH_MYSQL_DSN=opsmesh:opsmesh@tcp(127.0.0.1:3306)/opsmesh?parseTime=true
OPSMESH_REDIS_ADDR=127.0.0.1:6379

# 生产模式
OPSMESH_PRODUCTION=true
OPSMESH_REQUIRE_AUTH=true

# TLS
OPSMESH_TLS_CERT=/etc/opsmesh/tls/server.crt
OPSMESH_TLS_KEY=/etc/opsmesh/tls/server.key

# JWT 密钥（生产必须注入，openssl rand -hex 32）
OPSMESH_JWT_SECRET=

# 对外通告地址
OPSMESH_ADVERTISE_ADDR=http://0.0.0.0:8080

# 可选安全加固项（见模板注释）：
# OPSMESH_COOKIE_SECURE=true
# OPSMESH_PUBLIC_REGISTER=false
# OPSMESH_FEDERATION_SECRET= / OPSMESH_FEDERATION_PEERS=
# OPSMESH_PROVISION_SECRET=
# OPSMESH_GRPC_REQUIRE_SIGNATURE=true
# OPSMESH_TRUST_PROXY=false
# OPSMESH_JWT_PUBLIC_KEY= / OPSMESH_JWT_ISSUER=
# OPSMESH_CLIENT_CA=
# OPSMESH_METRICS_ALLOW_CIDR=10.0.0.0/8
```

> **注意**：systemd 的 `EnvironmentFile` 不支持 `${VAR:-default}` 展开语法，所有变量须直接填值（与 docker-compose 不同）。

### Agent 部署

```bash
# 创建 agent 环境变量文件
cat > /etc/opsmesh/opsmesh-agent.env << 'EOF'
OPSMESH_CONTROL_ADDR=http://controlplane-host:8080
OPSMESH_SEGMENT=seg-a
OPSMESH_DATA_DIR=/var/lib/opsmesh
EOF

# 启用并启动
systemctl enable --now opsmesh-agent
```

---

## Helm 部署（Kubernetes）

仓库自带完整 Helm Chart（`deploy/helm/opsmesh/`），含 `Chart.yaml` / `values.yaml` / `values-production.yaml` / `templates/` 全套模板，可一键部署控制面 + agent DaemonSet + MySQL + Redis。

### Chart 结构

```
deploy/helm/opsmesh/
├── Chart.yaml
├── values.yaml              # 开发/体验默认值
├── values-production.yaml   # 生产 overlay
└── templates/               # 14 个模板
```

### 开发/体验部署

```bash
# 单副本 + memory store（零外部依赖）
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace

# 查看
kubectl get all -n opsmesh
```

### 生产部署

```bash
# 3 副本 + mysql 持久化 + TLS + require-auth
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.provisionSecret=$(openssl rand -hex 32) \
  --set controlplane.jwtSecret=$(openssl rand -hex 32)
```

### 升级到生产配置

```bash
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  -f deploy/helm/opsmesh/values-production.yaml
```

> `helm upgrade` 不轮换 `jwtSecret` / `provisionSecret`（通过 `lookup` 复用已存在 Secret）。换密钥须先 `kubectl delete secret <对应 Secret>` 再 upgrade。

### values.yaml 关键配置

| 路径 | 默认 | 说明 |
|------|------|------|
| `controlplane.replicaCount` | 1 | 副本数；>1 须 `store=mysql` |
| `controlplane.store` | memory | 持久化后端 |
| `controlplane.production` | false | 生产模式 |
| `controlplane.requireAuth` | false | 强制鉴权 |
| `controlplane.tls.enabled` | false | TLS/mTLS |
| `controlplane.tls.secretName` | opsmesh-tls | 证书 Secret（键 tls.crt/tls.key/ca.crt） |
| `controlplane.taskLeaseSec` | 300 | 任务租约秒 |
| `controlplane.taskMaxRetries` | 3 | 任务重试上限 |
| `controlplane.leaderTTLSec` | 15 | 选主租约 |
| `controlplane.leaderTickSec` | 5 | 选主续租周期 |
| `controlplane.archiveAgeMin` | 1440 | 离线归档阈值 |
| `controlplane.provisionSecret` | "" | install token HMAC 密钥 |
| `controlplane.jwtSecret` | "" | JWT 签发密钥（空=首次随机生成并固化） |
| `controlplane.cookieSecure` | false | Cookie Secure |
| `controlplane.alertWebhookURL` | "" | 告警 Webhook |
| `controlplane.extraEnv` | [] | 额外环境变量（透传 OPSMESH_*） |
| `agent.enabled` | true | 是否部署 agent DaemonSet |
| `agent.segment` | default | agent 网段 |
| `agent.workerConcurrency` | 4 | worker 池并发度 |
| `agent.demo` | false | 演示模式 |
| `mysql.enabled` | true | 是否部署 MySQL StatefulSet |
| `mysql.persistence.size` | 10Gi | MySQL PV 大小 |
| `redis.enabled` | true | 是否部署 Redis StatefulSet |
| `ingress.enabled` | false | 是否创建 Ingress |

### values-production.yaml 关键差异

| 路径 | 生产值 | vs 开发 |
|------|--------|---------|
| `controlplane.replicaCount` | **3** | 1 → 3（HA） |
| `controlplane.store` | **mysql** | memory → mysql |
| `controlplane.production` | **true** | false → true |
| `controlplane.requireAuth` | **true** | false → true |
| `controlplane.tls.enabled` | **true** | false → true |
| `controlplane.cookieSecure` | **true** | false → true |
| `controlplane.resources.requests.cpu` | **500m** | 100m → 500m |
| `controlplane.resources.limits.cpu` | **2000m** | 500m → 2000m |
| `controlplane.resources.limits.memory` | **2Gi** | 512Mi → 2Gi |
| `controlplane.affinity` | **podAntiAffinity** | 空 → 反亲和（跨节点分散） |
| `agent.workerConcurrency` | **8** | 4 → 8 |
| `agent.taskTimeout` | **300s** | 120s → 300s |
| `mysql.persistence.size` | **100Gi** | 10Gi → 100Gi |
| `redis.persistence.size` | **20Gi** | 5Gi → 20Gi |
| `podAnnotations` | **prometheus scrape** | 空 → 开启 Prometheus 采集 |

### Chart 资源清单

- 控制面 `Deployment`（`replicaCount` 可调）+ `PodDisruptionBudget`（minAvailable=1）
- Agent `DaemonSet`（每节点一个，自动注册）
- MySQL / Redis `StatefulSet`（持久化 PV）+ `Secret`（provision-secret / mysql-dsn / jwt-secret）
- `Service`（控制面 ClusterIP + agent headless）
- `ServiceAccount` + 可选 `Ingress`
- TLS 证书走 `controlplane.tls.secretName` 预置 Secret

### 进阶开关注入

`--metrics-allow-cidr`、`--federation-*` 等未在 values 显式列出的 flag，通过 `controlplane.extraEnv` 注入对应 `OPSMESH_*` 环境变量：

```yaml
controlplane:
  extraEnv:
    - name: OPSMESH_METRICS_ALLOW_CIDR
      value: "10.0.0.0/8"
    - name: OPSMESH_FEDERATION_PEERS
      value: "http://peer1:8080,http://peer2:8080"
    - name: OPSMESH_FEDERATION_SECRET
      valueFrom:
        secretKeyRef:
          name: federation-secret
          key: secret
```

---

## K8s Operator 部署

仓库提供 K8s Operator（`operator/`），基于 controller-runtime，通过 CRD `OpsMeshInstance` 声明式管理完整 OpsMesh 部署。

### Operator 结构

```
operator/
├── api/v1alpha1/
│   ├── opsmeshinstance_types.go      # CRD 定义（OpsMeshInstance Spec/Status）
│   └── zz_generated.deepcopy.go
├── cmd/main.go                        # manager 入口
├── internal/controller/
│   ├── opsmeshinstance_controller.go  # Reconcile 逻辑
│   └── builders.go                    # Deployment/DaemonSet/StatefulSet 构造
├── config/                            # Kustomize manifest
├── Dockerfile
└── Makefile
```

### CRD Spec

```yaml
apiVersion: ops.opsmesh.io/v1alpha1
kind: OpsMeshInstance
metadata:
  name: my-opsmesh
spec:
  replicas: 3                    # 控制面副本数
  image: opsmesh/opsmesh:latest  # 控制面镜像
  agentImage: opsmesh/opsmesh-agent:latest  # agent 镜像
  store: mysql                   # memory | mysql
  production: true               # 生产模式
  tlsEnabled: true               # mTLS
  segmentCIDR: "10.244.0.0/16"   # agent 网段
  mysql:
    enabled: true
    storage: "10Gi"
    password: "change-me"        # 生产应走 Secret
  redis:
    enabled: true
    storage: "1Gi"
```

### 部署 Operator

```bash
cd operator
make install      # 安装 CRD
make run          # 本地运行 controller（开发用）

# 或部署到集群
make docker-build IMG=opsmesh/operator:latest
make deploy IMG=opsmesh/operator:latest
```

### 创建 OpsMesh 实例

```bash
kubectl apply -f - <<EOF
apiVersion: ops.opsmesh.io/v1alpha1
kind: OpsMeshInstance
metadata:
  name: prod-instance
spec:
  replicas: 3
  store: mysql
  production: true
  tlsEnabled: true
  segmentCIDR: "10.30.0.0/24"
  mysql:
    enabled: true
    storage: "100Gi"
  redis:
    enabled: true
    storage: "20Gi"
EOF
```

Operator 会自动 Reconcile 出控制面 Deployment、agent DaemonSet、MySQL/Redis StatefulSet 及关联 Service。

---

## 生产环境检查清单

上线前逐项确认，缺一不可。

### 安全配置

- [ ] `--production=true`（开启生产模式：require-auth 默认开、cookie-secure 默认开、grpc-require-signature 默认开）
- [ ] `--require-auth=true`（缺失 X-Tenant-ID 头拒绝）
- [ ] `--store=mysql`（memory 多副本数据分裂）
- [ ] `--jwt-secret` 已注入 ≥32 字节强随机密钥（`openssl rand -hex 32`），多副本一致
- [ ] `--provision-secret` 已注入强随机密钥，多副本一致
- [ ] `--tls-cert` / `--tls-key` 已配置 gRPC TLS 证书
- [ ] `--client-ca` 已配置 mTLS 客户端 CA（强制客户端持证）
- [ ] `--cookie-secure=true`（HTTPS 环境防中间人窃取会话）
- [ ] `--public-register=false`（关闭公开注册，仅管理员创建用户）
- [ ] `--allow-public-register=false`（关闭免审批注册）
- [ ] `--grpc-require-signature=true`（强制 agent HMAC 签名，防冒领任务）
- [ ] `--metrics-allow-cidr` 已配置内网监控网段（如 10.0.0.0/8）
- [ ] `--agent-shell-whitelist` 已配置命令白名单（限制 agent 可执行命令）
- [ ] `--agent-file-root-whitelist` 已配置文件任务根目录白名单
- [ ] `--provision-ssh-known-hosts` 已配置（关闭 InsecureIgnoreHostKey）
- [ ] `--demo` 已关闭（避免污染生产任务）

### 性能配置

- [ ] `--worker-concurrency` 按机器规格调整（生产建议 8+）
- [ ] `--task-timeout` 按任务类型调整（长任务建议 300s+）
- [ ] `--max-procs` / `--max-files` / `--max-memory-mb` 已设置 agent 资源限额
- [ ] MySQL 连接池大小合理
- [ ] Redis 已启用持久化（如需会话容灾）
- [ ] `--event-bus=kafka` 时 Kafka 集群可用且 `--kafka-brokers` / `--kafka-topic` 已配置

### 高可用配置

- [ ] `--replicas>=2`（控制面多副本）
- [ ] `--store=mysql`（多副本共享同一 MySQL）
- [ ] `--leader-ttl-sec` 与 `--leader-tick-sec` 比例合理（tick 建议 ttl 的 1/3）
- [ ] `--control-addrs` 配置多控制面地址（agent 端 failover）
- [ ] MySQL 高可用（主从/集群）
- [ ] Redis 高可用（哨兵/集群）
- [ ] Pod 反亲和（跨节点分散控制面副本）
- [ ] `PodDisruptionBudget` 已配置（minAvailable=1）

### 可观测配置

- [ ] `--metrics-port` 已暴露，Prometheus 已配置采集
- [ ] `--alert-webhook-url` 已配置告警通知
- [ ] `--alert-notifier-type` 按团队工具选择（feishu/dingtalk/slack/企业微信）
- [ ] `--alert-email-*` 邮件通道已配置（如需邮件告警）
- [ ] `--log-backend` 按规模选择（小规模 memory/sql，大规模 loki/es）
- [ ] `--log-backend=loki` 时 `--loki-endpoint` 已配置
- [ ] `--log-backend=es` 时 `--es-endpoint` / `--es-index` 已配置

---

## 多租户部署

M4-4C 多租户 schema 隔离：每租户路由到独立 MySQL schema（database），物理级数据隔离。

### 启用方式

```bash
./opsmesh --mode=controlplane \
  --store=mysql \
  --mysql-dsn="user:pass@tcp(mysql:3306)/opsmesh?parseTime=true" \
  --multi-schema=true \
  --schema-prefix="opsmesh_tenant_"
```

### 工作原理

- `--multi-schema=true` 开启后，store 层使用 `MultiSchemaStore` 而非单个 `SQLStore`
- 每个请求按 `X-Tenant-ID` 头路由到对应 schema：`<schema-prefix><tenant-id>`
- 例：`--schema-prefix=opsmesh_tenant_` + `X-Tenant-ID: t1` → schema `opsmesh_tenant_t1`
- 各租户 schema 独立建表，数据物理隔离

### 约束

- 仅 `--store=mysql` 时生效（`Validate` 中校验，memory store 下报错）
- 首次访问某租户 schema 时自动建表（幂等）
- 租户间不共享数据，跨租户查询须走联邦或上层聚合

---

## 联邦部署

M4-4D 控制面联邦：企业多终端环境按网段割裂为多个控制面，联邦支持跨网段任务转发与联邦设备视图。联邦通道硬化为 mTLS + HMAC 签名，防伪造/防重放。

### 拓扑

```
┌─────────────────┐     mTLS + HMAC     ┌─────────────────┐
│  控制面 A (段1)   │◄──────────────────►│  控制面 B (段2)   │
│  peer: B          │                    │  peer: A          │
│  agent: 段1设备    │                    │  agent: 段2设备    │
└─────────────────┘                    └─────────────────┘
```

### 控制面 A（段 1）配置

```bash
./opsmesh --mode=controlplane --store=mysql \
  --mysql-dsn="user:pass@tcp(mysql-a:3306)/opsmesh" \
  --federation-peers="http://controlplane-b:8080" \
  --federation-secret="$(openssl rand -hex 32)" \
  --federation-port=9092 \
  --federation-tls-cert=/etc/opsmesh/fed.crt \
  --federation-tls-key=/etc/opsmesh/fed.key \
  --federation-ca=/etc/opsmesh/fed-ca.crt \
  --production
```

### 控制面 B（段 2）配置

```bash
# federation-secret 必须与 A 完全一致
./opsmesh --mode=controlplane --store=mysql \
  --mysql-dsn="user:pass@tcp(mysql-b:3306)/opsmesh" \
  --federation-peers="http://controlplane-a:9092" \
  --federation-secret="<与 A 完全一致>" \
  --federation-port=9092 \
  --federation-tls-cert=/etc/opsmesh/fed.crt \
  --federation-tls-key=/etc/opsmesh/fed.key \
  --federation-ca=/etc/opsmesh/fed-ca.crt \
  --production
```

### 配置说明

| Flag | 说明 |
|------|------|
| `--federation-peers` | peer 控制面地址列表（逗号分隔） |
| `--federation-secret` | 联邦共享 HMAC 密钥（所有 peer 须一致） |
| `--federation-port` | 联邦独立 mTLS 监听端口（>0 启用，强制对端持证）；0=复用主 HTTP |
| `--federation-tls-cert/key` | 联邦 mTLS 证书（独立于 gRPC 的 --tls-cert） |
| `--federation-ca` | 联邦 mTLS 对端 CA |

### 安全机制

- **入站**：`--federation-port>0` 时起独立 mTLS 监听，`RequireAndVerifyClientCert`，物理隔离联邦流量与公网 B/S
- **验签**：仅对携带 `X-Federation-Forwarded: 1` 的请求验签；签名 = HMAC-SHA256(method + path + 时间戳 + 身份头)，时间戳偏差窗 ±5min 防重放
- **出站**：`FederationManager.ForwardTask` / `fetchPeerDevices` 主动签名 + 联邦 TLS 客户端配置
- **容错**：peer 不可达不影响本地服务，联邦 API 返回可用部分 + 不可达标记

### 联邦 API

- `GET /api/v1/federation/peers` — peer 列表与可达性
- `POST /api/v1/federation/forward/task` — 跨段转发任务
- `GET /api/v1/federation/devices` — 联邦设备视图（聚合所有 peer 设备）

详见 [API 参考文档 - 联邦 API](./api-reference.md#联邦-api)。

### 约束

- 启用独立监听（`--federation-port>0`）须同时配置联邦 TLS 证书，否则 `Validate()` 报错
- 明文联邦（不配 TLS）仅限内网可信场景
- 所有 peer 须共享同一 `--federation-secret`