# OpsMesh 运维手册

> 版本：v0.1 · 编制日期：2026-08-17 · 适用基线：OpsMesh MVP（Helm Chart `deploy/helm/opsmesh`、systemd unit `deploy/systemd/`、docker-compose 一键体验环境）
>
> 本文档为 OpsMesh 平台运维工程师（SRE/Ops）提供从部署、配置、日常运维到故障处置、巡检 SOP、升级与性能调优的完整操作手册。所有命令假设 Helm release 名为 `opsmesh`、命名空间为 `opsmesh`、systemd 单元名为 `opsmesh-controlplane` / `opsmesh-agent`，按实际环境替换。
>
> 配套文档：
> - 部署细节见 [deployment-guide.md](./deployment-guide.md)
> - 灾难恢复与备份见 [dr-runbook.md](./dr-runbook.md)
> - 配置 flag 矩阵见 [flag-matrix.md](./flag-matrix.md)
> - 安全机制见 [security-mechanism.md](./security-mechanism.md)

---

## 第1章 部署指南

OpsMesh 控制面与 agent 共用同一份二进制 `opsmesh`，通过 `--mode=controlplane|agent` 切换角色。本章给出四种典型部署方式的落地步骤，按场景选择即可。

| 部署方式 | 适用场景 | 数据持久化 | 高可用 | 推荐环境 |
|---|---|---|---|---|
| Docker Compose | 本地开发/演示/快速体验 | mysql 容器卷 | 否 | dev/demo |
| Helm Chart | Kubernetes 集群生产部署 | MySQL StatefulSet + PV | 控制面多副本 + leader 选举 | prod/staging |
| systemd | 物理机/VM 裸金属 | 外部托管 MySQL/Redis | 由部署方保障 | prod（无 K8s） |
| 裸金属（手动二进制） | 边缘节点/受限环境 | 外部 MySQL/Redis | 单实例或外部 LB | edge/air-gap |

### 1.1 Docker Compose 部署

仓库根目录 `docker-compose.yaml` 提供 controlplane + agent + mysql + redis 一键起环境，适合开发/演示。

#### 1.1.1 前置条件

- Docker 20.10+ 与 Docker Compose v2
- 端口 8080 / 9090 / 9091 未被占用
- 仓库根目录可写（构建镜像与挂载卷）

#### 1.1.2 启动与停止

命令示例：Docker Compose 启停

```bash
# 启动（首次会构建镜像）
docker compose up -d

# 查看状态
docker compose ps

# 访问仪表盘：浏览器打开 http://localhost:8080

# 停止服务
docker compose down

# 停止并清理数据卷（彻底重置）
docker compose down -v
```

#### 1.1.3 自定义 JWT 密钥

默认 `OPSMESH_JWT_SECRET` 为空（dev 随机兜底）。若需固定密钥（如 CI 复用会话）：

```bash
export OPSMESH_JWT_SECRET=$(openssl rand -hex 32)
docker compose up -d
```

#### 1.1.4 服务拓扑

```
controlplane (HTTP 8080 / gRPC 9090 / metrics 9091) → mysql:3306 + redis:6379
agent → controlplane:8080
```

> **注意**：docker-compose.yaml 默认开启 `--demo`，保持 `admin/admin123` 可登录。生产部署严禁使用本方式，请改用 Helm + `values-production.yaml`。

### 1.2 Helm Chart 部署（Kubernetes）

仓库自带完整 Helm Chart（`deploy/helm/opsmesh/`），含 `Chart.yaml` / `values.yaml` / `values-production.yaml` / `templates/` 全套模板，可一键部署控制面 + agent DaemonSet + MySQL + Redis + 备份 CronJob。

#### 1.2.1 开发/体验部署

命令示例：Helm 一键部署开发环境

```bash
# 单副本 + memory store（零外部依赖）
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace

# 查看
kubectl get all -n opsmesh
```

#### 1.2.2 生产部署

命令示例：Helm 生产部署

```bash
# 3 副本 + mysql 持久化 + TLS + require-auth
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.provisionSecret=$(openssl rand -hex 32) \
  --set controlplane.jwtSecret=$(openssl rand -hex 32)
```

#### 1.2.3 升级到生产配置

```bash
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  -f deploy/helm/opsmesh/values-production.yaml
```

> **注意**：`helm upgrade` 不轮换 `jwtSecret` / `provisionSecret`（通过 `lookup` 复用已存在 Secret）。换密钥须先 `kubectl delete secret <对应 Secret>` 再 upgrade。

#### 1.2.4 关键 values 差异

表：values.yaml 与 values-production.yaml 关键差异对照表

| 路径 | 开发默认 | 生产值 | 说明 |
|---|---|---|---|
| `controlplane.replicaCount` | 1 | **3** | HA 副本数 |
| `controlplane.store` | memory | **mysql** | 多副本必须 mysql |
| `controlplane.production` | false | **true** | 生产模式 |
| `controlplane.requireAuth` | false | **true** | 强制鉴权 |
| `controlplane.tls.enabled` | false | **true** | gRPC TLS/mTLS |
| `controlplane.cookieSecure` | false | **true** | Cookie Secure |
| `controlplane.resources.limits.cpu` | 500m | **2000m** | 资源放大 |
| `controlplane.resources.limits.memory` | 512Mi | **2Gi** | 资源放大 |
| `controlplane.affinity` | {} | **podAntiAffinity** | 跨节点分散 |
| `agent.workerConcurrency` | 4 | **8** | worker 池并发度 |
| `agent.taskTimeout` | 120s | **300s** | 单任务超时 |
| `mysql.persistence.size` | 10Gi | **100Gi** | 数据卷扩容 |
| `redis.persistence.size` | 5Gi | **20Gi** | 缓存卷扩容 |
| `observability.serviceMonitor.enabled` | false | **true** | Prometheus 采集 |
| `observability.prometheusRule.enabled` | false | **true** | 内置告警规则 |

### 1.3 systemd 部署（裸金属/VM）

适合物理机/VM 裸金属部署。仓库提供 `deploy/systemd/` 下的 service 文件与环境变量模板。

#### 1.3.1 文件清单

| 文件 | 说明 |
|---|---|
| `opsmesh-controlplane.service` | 控制面 systemd unit（含安全加固） |
| `opsmesh-controlplane.env` | 控制面环境变量模板（EnvironmentFile 加载） |
| `opsmesh-agent.service` | agent systemd unit（含安全加固） |

#### 1.3.2 安装步骤

命令示例：systemd 安装控制面

```bash
# 1) 创建专用用户与目录
useradd -r -s /usr/sbin/nologin opsmesh
install -d -m 0750 /etc/opsmesh /var/lib/opsmesh /var/log/opsmesh

# 2) 放置二进制
install -m 0755 opsmesh /usr/local/bin/opsmesh

# 3) 放置配置
install -m 0640 deploy/systemd/opsmesh-controlplane.env /etc/opsmesh/
install -m 0644 deploy/systemd/opsmesh-controlplane.service /etc/systemd/system/
install -m 0644 deploy/systemd/opsmesh-agent.service /etc/systemd/system/

# 4) 编辑环境变量（填入实际 MySQL/Redis/JWT 等）
vim /etc/opsmesh/opsmesh-controlplane.env

# 5) 启用并启动
systemctl daemon-reload
systemctl enable --now opsmesh-controlplane
```

#### 1.3.3 Agent 部署

命令示例：systemd 安装 agent

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

#### 1.3.4 安全加固要点

systemd unit 已内置以下安全指令（无需手动配置）：

- `NoNewPrivileges=true`：禁止提权
- `ProtectSystem=strict`：`/usr`、`/boot`、`/etc` 只读
- `ProtectHome=true`：`/home`、`/root` 不可见
- `PrivateTmp=true` / `PrivateDevices=true`：私有 `/tmp` 与 `/dev`
- `ProtectKernelTunables=true` / `ProtectKernelModules=true`：禁止改内核参数/加载模块
- `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`：仅允许 IPv4/IPv6/Unix 套接字
- `SystemCallFilter=@system-service`：限制系统调用集合
- `CapabilityBoundingSet=` / `AmbientCapabilities=`：清空所有 Linux capabilities
- `User=opsmesh` / `Group=opsmesh`：非 root 运行

> **注意**：systemd 的 `EnvironmentFile` 不支持 `${VAR:-default}` 展开语法，所有变量须直接填值（与 docker-compose 不同）。

### 1.4 裸金属手动部署（无 systemd）

适用于边缘节点、air-gap 环境或临时验证。

命令示例：裸金属手动起控制面

```bash
# 1) 准备二进制与配置（环境变量或 flag）
export OPSMESH_STORE=mysql
export OPSMESH_MYSQL_DSN="user:pass@tcp(mysql:3306)/opsmesh?parseTime=true"
export OPSMESH_REDIS_ADDR=redis:6379
export OPSMESH_JWT_SECRET=$(openssl rand -hex 32)
export OPSMESH_PRODUCTION=true
export OPSMESH_TLS_CERT=/etc/opsmesh/tls/server.crt
export OPSMESH_TLS_KEY=/etc/opsmesh/tls/server.key

# 2) 前台启动（生产应配合 nohup/setsid/tmux 持久化）
./opsmesh --mode=controlplane --advertise-addr=http://0.0.0.0:8080

# 3) agent 端
./opsmesh --mode=agent \
  --control-addr=http://controlplane:8080 \
  --segment=edge-a \
  --data-dir=/var/lib/opsmesh
```

裸金属部署建议叠加 `systemd` 或 `supervisor` 做进程托管，避免裸进程意外退出后无人拉起。

### 1.5 部署后自检

部署完成后逐项验证：

| 验证项 | 命令 | 期望 |
|---|---|---|
| 控制面健康 | `curl http://<cp>:8080/healthz` | `ok` |
| gRPC 端口 | `nc -zv <cp> 9090` | succeeded |
| metrics 端口 | `curl http://<cp>:9091/metrics \| head -5` | Prometheus 文本 |
| 仪表盘 | 浏览器 `http://<cp>:8080` | 登录页 |
| Agent 在线 | 仪表盘设备页 / `opsmesh_agents_total` | ≥1 |
| MySQL 连接 | 控制面日志无 `dial tcp ...:3306` 错误 | 无错误 |
| 备份 CronJob | `kubectl get cronjob -n opsmesh` | opsmesh-mysql-backup |

---

## 第2章 配置说明

OpsMesh 采用「flag 优先、环境变量兜底」的统一配置模型（`internal/config/config.go`）。同一份二进制通过 `--mode` 切换 controlplane / agent 角色，所有配置项均同时支持 flag 与 `OPSMESH_*` 环境变量。

### 2.1 配置加载优先级

```
显式 flag > 环境变量 OPSMESH_* > 代码默认值
```

- 显式传入的 flag 永远生效（不会被环境变量覆盖）
- 未显式传 flag 时，环境变量兜底
- 二者皆未设置则使用代码默认值

> **注意**：systemd `EnvironmentFile` 不支持 `${VAR:-default}` 展开，所有变量须直接填值。docker-compose 支持 `${VAR:-default}`。

### 2.2 模式与角色

表：模式与角色配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--mode` | `OPSMESH_MODE` | controlplane | 运行角色：controlplane \| agent |
| `--production` | `OPSMESH_PRODUCTION` | false | 生产模式：默认开启 require-auth、cookie-secure、grpc-require-signature，强校验 TLS/JWT/EncryptionKey |
| `--demo` | `OPSMESH_DEMO` | false | 演示模式：每个 agent 注册预置示例任务；强制关闭 grpc-require-signature；保持 public-register=true |
| `--replicas` | `OPSMESH_REPLICAS` | 1 | 控制面副本数（>1 须 `--store=mysql`） |

#### 2.2.1 demo vs production 模式对照

表：demo 与 production 模式行为对照表

| 行为 | demo 模式 | production 模式 |
|---|---|---|
| `require-auth` | 不强制 | **强制开启**（除非显式 false） |
| `cookie-secure` | false | **true**（除非显式 false） |
| `grpc-require-signature` | **强制关闭** | **默认开启**（除非显式 false） |
| `public-register` | **强制 true**（接口开放） | 默认 false（关闭公开注册） |
| `allow-public-register` | false（仍走审批） | false |
| TLS 校验 | 不强制 | **强制**（`--tls-cert` 必填，否则启动失败） |
| `jwt-secret` 校验 | 不强制 | **强制 ≥32 字节**，否则启动失败 |
| `encryption-key` 校验 | 不强制 | **强制非空**，否则启动失败 |
| `store=memory` | 允许 | 告警（多副本分裂） |
| admin 密码 | 固定 `admin/admin123` | 随机化（首次启动日志输出） |

### 2.3 网络与端口

表：网络与端口配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--http-port` | `OPSMESH_HTTP_PORT` | 8080 | 控制面 HTTP(B/S) 端口 |
| `--grpc-port` | `OPSMESH_GRPC_PORT` | 9090 | gRPC 端口（agent 注册通道） |
| `--metrics-port` | `OPSMESH_METRICS_PORT` | 9091 | Prometheus metrics 端口 |
| `--advertise-addr` | `OPSMESH_ADVERTISE_ADDR` | `http://127.0.0.1:<http-port>` | 控制面对外可达地址（拼接 bootstrap 命令） |
| `--control-addr` | `OPSMESH_CONTROL_ADDR` | `http://127.0.0.1:8080` | agent 单控制面地址 |
| `--control-addrs` | `OPSMESH_CONTROL_ADDRS` | 空 | 多控制面地址（逗号分隔，HA failover） |
| `--controlplane-endpoints` | `OPSMESH_CONTROLPLANE_ENDPOINTS` | 空 | 服务发现入口（与 `--control-addrs` 同义，优先级更高） |
| `--lb-strategy` | `OPSMESH_LB_STRATEGY` | failover | 负载均衡策略：round-robin \| failover |
| `--segment` | `OPSMESH_SEGMENT` | default | agent 所属网段（分桶键） |
| `--trust-proxy` | `OPSMESH_TRUST_PROXY` | false | 信任反向代理（取 X-Forwarded-For 首段为 clientIP） |

### 2.4 持久化与存储

表：持久化与存储配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--store` | `OPSMESH_STORE` | memory | 持久化后端：memory \| mysql |
| `--mysql-dsn` | `OPSMESH_MYSQL_DSN` | 空 | MySQL DSN（go-sql-driver/mysql 格式） |
| `--redis-addr` | `OPSMESH_REDIS_ADDR` | 空 | Redis 地址（缓存/会话/消息） |
| `--session-store` | `OPSMESH_SESSION_STORE` | 空 | 会话状态后端：空=进程内 \| `redis://host:port`（多副本 HA） |
| `--multi-schema` | `OPSMESH_MULTI_SCHEMA` | false | 多租户 schema 隔离（仅 mysql 生效） |
| `--schema-prefix` | `OPSMESH_SCHEMA_PREFIX` | `opsmesh_tenant_` | 多 schema 名前缀 |
| `--encryption-key` | `OPSMESH_ENCRYPTION_KEY` | 空 | kubeconfig AES-256-GCM 加密密钥（base64 编码 32 字节） |

### 2.5 安全与认证

表：安全与认证配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--require-auth` | `OPSMESH_REQUIRE_AUTH` | false | 强制鉴权（缺失 X-Tenant-ID 拒绝） |
| `--tls-cert` | `OPSMESH_TLS_CERT` | 空 | gRPC TLS 服务端证书路径 |
| `--tls-key` | `OPSMESH_TLS_KEY` | 空 | gRPC TLS 私钥路径 |
| `--client-ca` | `OPSMESH_CLIENT_CA` | 空 | mTLS 客户端 CA |
| `--tls-watch` | `OPSMESH_TLS_WATCH` | false | TLS 证书热重载（fsnotify） |
| `--jwt-secret` | `OPSMESH_JWT_SECRET` | 空 | 用户中心 JWT 签发密钥（HS256，≥32 字节） |
| `--jwt-public-key` | `OPSMESH_JWT_PUBLIC_KEY` | 空 | JWT 验签公钥（RS256，网关剥离 + 内核二次校验） |
| `--jwt-issuer` | `OPSMESH_JWT_ISSUER` | 空 | 预期 JWT issuer |
| `--provision-secret` | `OPSMESH_PROVISION_SECRET` | 空 | install token HMAC 密钥（多副本须一致） |
| `--grpc-require-signature` | `OPSMESH_GRPC_REQUIRE_SIGNATURE` | false | 强制 agent HMAC 签名 |
| `--grpc-signature-key` | `OPSMESH_GRPC_SIGNATURE_KEY` | 空 | gRPC 预共享 HMAC 密钥 |
| `--cookie-secure` | `OPSMESH_COOKIE_SECURE` | false | Cookie Secure 标志 |
| `--public-register` | `OPSMESH_PUBLIC_REGISTER` | true | 公开注册接口开关（新用户须审批） |
| `--allow-public-register` | `OPSMESH_ALLOW_PUBLIC_REGISTER` | false | 公开注册免审批 |
| `--metrics-allow-cidr` | `OPSMESH_METRICS_ALLOW_CIDR` | 空 | metrics CIDR 白名单 |
| `--agent-shell-whitelist` | `OPSMESH_AGENT_SHELL_WHITELIST` | 空 | agent shell 命令白名单 |
| `--agent-file-root-whitelist` | `OPSMESH_AGENT_FILE_ROOT_WHITELIST` | 空 | agent 文件任务根目录白名单 |
| `--webhook-allow-private` | `OPSMESH_WEBHOOK_ALLOW_PRIVATE` | false | 允许内网 webhook（SSRF 防护） |
| `--provision-cidr-whitelist` | `OPSMESH_PROVISION_CIDR_WHITELIST` | 空 | autoProvision 扫描网段白名单 |
| `--device-fp-deadline` | `OPSMESH_DEVICE_FP_DEADLINE` | 空 | DeviceFP 强制非空截止时间（RFC3339） |

### 2.6 联邦配置

表：联邦配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--federation-peers` | `OPSMESH_FEDERATION_PEERS` | 空 | peer 控制面地址列表（逗号分隔） |
| `--federation-secret` | `OPSMESH_FEDERATION_SECRET` | 空 | 联邦共享 HMAC 密钥（所有 peer 须一致） |
| `--federation-port` | `OPSMESH_FEDERATION_PORT` | 0 | 联邦独立 mTLS 监听端口（>0 启用） |
| `--federation-tls-cert` | `OPSMESH_FEDERATION_TLS_CERT` | 空 | 联邦 mTLS 服务端证书 |
| `--federation-tls-key` | `OPSMESH_FEDERATION_TLS_KEY` | 空 | 联邦 mTLS 私钥 |
| `--federation-ca` | `OPSMESH_FEDERATION_CA` | 空 | 联邦 mTLS 对端 CA |

### 2.7 任务调度与运行时

表：任务调度与运行时配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--task-lease-sec` | `OPSMESH_TASK_LEASE_SEC` | 300 | 任务租约秒（超期重调度） |
| `--task-max-retries` | `OPSMESH_TASK_MAX_RETRIES` | 3 | 任务重试上限 |
| `--task-timeout` | `OPSMESH_TASK_TIMEOUT` | 120s | agent 单任务执行超时 |
| `--shutdown-timeout` | `OPSMESH_SHUTDOWN_TIMEOUT` | 15s | SIGTERM 优雅退出窗口 |
| `--leader-ttl-sec` | `OPSMESH_LEADER_TTL_SEC` | 15 | 选主租约秒 |
| `--leader-tick-sec` | `OPSMESH_LEADER_TICK_SEC` | 5 | 选主续租周期 |
| `--archive-age-min` | `OPSMESH_ARCHIVE_AGE_MIN` | 1440 | 离线归档阈值（分钟，<=0 关闭） |
| `--worker-concurrency` | `OPSMESH_WORKER_CONCURRENCY` | 4 | agent worker 池并发度 |
| `--max-procs` | `OPSMESH_MAX_PROCS` | 256 | agent RLIMIT_NPROC |
| `--max-files` | `OPSMESH_MAX_FILES` | 4096 | agent RLIMIT_NOFILE |
| `--max-memory-mb` | `OPSMESH_MAX_MEMORY_MB` | 0 | agent RLIMIT_AS（MB，0=不限） |
| `--data-dir` | `OPSMESH_DATA_DIR` | `./data` | agent 身份文件目录 |

### 2.8 监控告警与通知

表：监控告警与通知配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--alert-webhook-url` | `OPSMESH_ALERT_WEBHOOK_URL` | 空 | 告警 Webhook URL |
| `--alert-notifier-type` | `OPSMESH_ALERT_NOTIFIER_TYPE` | generic | 通知类型：generic/feishu/dingtalk |
| `--alert-email-host` | `OPSMESH_ALERT_EMAIL_HOST` | 空 | SMTP 主机 |
| `--alert-email-port` | `OPSMESH_ALERT_EMAIL_PORT` | 25 | SMTP 端口 |
| `--alert-email-user` | `OPSMESH_ALERT_EMAIL_USER` | 空 | SMTP 用户名 |
| `--alert-email-pass` | `OPSMESH_ALERT_EMAIL_PASS` | 空 | SMTP 密码 |
| `--alert-email-from` | `OPSMESH_ALERT_EMAIL_FROM` | 空 | 发件人 |
| `--alert-email-to` | `OPSMESH_ALERT_EMAIL_TO` | 空 | 收件人列表（逗号分隔） |
| `--notify-channels-config` | `OPSMESH_NOTIFY_CHANNELS_CONFIG` | 空 | 多渠道 JSON 配置文件路径 |
| `--notify-dedup-ttl-min` | `OPSMESH_NOTIFY_DEDUP_TTL_MIN` | 5 | 通知去重 TTL（分钟，0=关闭） |
| `--notify-retry-max-attempts` | `OPSMESH_NOTIFY_RETRY_MAX_ATTEMPTS` | 3 | 通知重试次数 |
| `--notify-retry-interval` | `OPSMESH_NOTIFY_RETRY_INTERVAL` | 5s | 通知重试基础间隔 |
| `--notify-retry-backoff` | `OPSMESH_NOTIFY_RETRY_BACKOFF` | 2.0 | 通知重试退避系数 |
| `--inhibit-rules-file` | `OPSMESH_INHIBIT_RULES_FILE` | 空 | 告警抑制规则 JSON 文件 |
| `--anomaly-detection` | `OPSMESH_ANOMALY_DETECTION` | false | 启用异常检测 |
| `--anomaly-window-size` | `OPSMESH_ANOMALY_WINDOW_SIZE` | 100 | 异常检测基线窗口 |
| `--anomaly-threshold` | `OPSMESH_ANOMALY_THRESHOLD` | 3.0 | 异常检测 Z-Score 阈值 |

### 2.9 日志、追踪与熔断

表：日志、追踪与熔断配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--log-backend` | `OPSMESH_LOG_BACKEND` | memory | 日志后端：memory \| sql \| loki \| es |
| `--log-store` | `OPSMESH_LOG_STORE` | memory | `--log-backend` 别名 |
| `--loki-endpoint` | `OPSMESH_LOKI_ENDPOINT` | 空 | Loki API endpoint |
| `--es-endpoint` | `OPSMESH_ES_ENDPOINT` | 空 | Elasticsearch endpoint |
| `--es-index` | `OPSMESH_ES_INDEX` | `opsmesh-logs` | ES 索引名 |
| `--log-push-enabled` | `OPSMESH_LOG_PUSH_ENABLED` | false | 启用 agent 日志采集推送 |
| `--log-push-files` | `OPSMESH_LOG_PUSH_FILES` | 空 | 日志采集文件列表（逗号分隔） |
| `--log-push-pattern` | `OPSMESH_LOG_PUSH_PATTERN` | 空 | 日志采集正则过滤 |
| `--log-push-endpoint` | `OPSMESH_LOG_PUSH_ENDPOINT` | 空 | 日志推送目标 |
| `--log-push-backend` | `OPSMESH_LOG_PUSH_BACKEND` | loki | 日志推送后端：loki \| es |
| `--otel-endpoint` | `OPSMESH_OTEL_ENDPOINT` | 空 | OTLP gRPC 导出地址 |
| `--otel-service-name` | `OPSMESH_OTEL_SERVICE_NAME` | 空 | OTel 服务名 |
| `--otel-stdout` | `OPSMESH_OTEL_STDOUT` | false | OTel 导出 stderr（调试） |
| `--cb-failure-threshold` | `OPSMESH_CB_FAILURE_THRESHOLD` | 5 | 熔断失败阈值（0=禁用） |
| `--cb-recovery-timeout` | `OPSMESH_CB_RECOVERY_TIMEOUT` | 30s | 熔断恢复等待 |
| `--cb-half-open-max-calls` | `OPSMESH_CB_HALF_OPEN_MAX_CALLS` | 1 | 半开状态最大探测调用数 |
| `--cb-rate-limit-per-sec` | `OPSMESH_CB_RATE_LIMIT_PER_SEC` | 0 | API 限流阈值（0=禁用） |

### 2.10 自动纳管与配额

表：自动纳管与配额配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--discover` | `OPSMESH_DISCOVER` | false | 开启真实网段发现 |
| `--segment-cidr` | `OPSMESH_SEGMENT_CIDR` | 空 | 待扫描网段（如 10.30.0.0/24） |
| `--auto-provision` | `OPSMESH_AUTO_PROVISION` | false | 自动纳管（需 `--provision-ssh-key`） |
| `--provision-ssh-user` | `OPSMESH_PROVISION_SSH_USER` | root | SSH 用户 |
| `--provision-ssh-key` | `OPSMESH_PROVISION_SSH_KEY` | 空 | SSH 私钥路径 |
| `--provision-ssh-key-pass` | `OPSMESH_PROVISION_SSH_KEY_PASS` | 空 | SSH 密钥密码 |
| `--provision-ssh-known-hosts` | `OPSMESH_PROVISION_SSH_KNOWN_HOSTS` | 空 | SSH KnownHosts 文件（生产必填） |
| `--install-token` | `OPSMESH_INSTALL_TOKEN` | 空 | agent bootstrap 一次性 token |
| `--quota-enabled` | `OPSMESH_QUOTA_ENABLED` | false | 启用租户资源配额检查 |
| `--quota-max-devices` | `OPSMESH_QUOTA_MAX_DEVICES` | 0 | 默认最大设备数（0=不限） |
| `--quota-max-tasks` | `OPSMESH_QUOTA_MAX_TASKS` | 0 | 默认最大任务数 |
| `--quota-max-alerts` | `OPSMESH_QUOTA_MAX_ALERTS` | 0 | 默认最大告警数 |

### 2.11 事件总线与密钥外置

表：事件总线与密钥外置配置说明表

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--event-bus` | `OPSMESH_EVENT_BUS` | noop | 事件总线：noop \| log \| kafka |
| `--kafka-brokers` | `OPSMESH_KAFKA_BROKERS` | 空 | Kafka brokers |
| `--kafka-topic` | `OPSMESH_KAFKA_TOPIC` | 空 | Kafka topic |
| `--secret-provider` | `OPSMESH_SECRET_PROVIDER` | 空 | 密钥来源：env \| file \| vault \| chain:env,file |
| `--secret-file` | `OPSMESH_SECRET_FILE` | 空 | JSON 密钥文件路径 |
| `--vault-addr` | `OPSMESH_VAULT_ADDR` | 空 | Vault API 地址 |
| `--vault-token` | `OPSMESH_VAULT_TOKEN` | 空 | Vault 访问令牌 |
| `--vault-mount` | `OPSMESH_VAULT_MOUNT` | secret | Vault KV v2 挂载路径 |

### 2.12 启动校验（Validate）

`config.Validate()` 在启动期对以下情况 fail-fast（拒绝启动）：

- `--mode` 非 `controlplane` / `agent`
- 端口超出 1-65535
- `--store=mysql` 但 `--mysql-dsn` 为空
- `--store=memory` 且 `--replicas>1`（多副本分裂）
- `--discover=true` 但 `--segment-cidr` 缺失或非法
- **生产模式** `--production=true` 但 `--tls-cert` 为空（明文通信不满足等保三级）
- **生产模式** `--jwt-secret` 为空或长度 <32 字节
- **生产模式** `--encryption-key` 为空（kubeconfig 明文存储）
- `--log-backend=loki` 但 `--loki-endpoint` 为空
- `--log-backend=es` 但 `--es-endpoint` / `--es-index` 为空
- `--multi-schema=true` 但 `--store!=mysql`
- `--federation-peers` 非空但 `--federation-secret` 为空
- `--federation-port>0` 但联邦 TLS 证书缺失
- `--session-store` 非 `redis://host:port` 格式
- `--inhibit-rules-file` 文件不存在
- `--log-push-enabled=true` 但 files/endpoint 缺失或 backend 非法

---

## 第3章 日常运维

### 3.1 启停服务

#### 3.1.1 Docker Compose

命令示例：Docker Compose 启停

```bash
docker compose up -d            # 启动
docker compose stop             # 停止（保留容器）
docker compose down             # 停止并删除容器（保留卷）
docker compose down -v          # 停止并删除容器与卷
docker compose restart controlplane   # 重启单个服务
```

#### 3.1.2 Helm / Kubernetes

命令示例：K8s 启停

```bash
# 启动（已 install 则无需重复）
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh

# 优雅缩容到 0（停止控制面，保留数据）
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=0

# 恢复
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=3

# 滚动重启
kubectl rollout restart deploy/opsmesh-controlplane -n opsmesh
kubectl rollout restart daemonset/opsmesh-agent -n opsmesh

# 卸载（保留 PVC 数据）
helm uninstall opsmesh -n opsmesh
```

#### 3.1.3 systemd

命令示例：systemd 启停

```bash
systemctl start opsmesh-controlplane
systemctl stop opsmesh-controlplane
systemctl restart opsmesh-controlplane
systemctl status opsmesh-controlplane

# agent 同理
systemctl start opsmesh-agent
systemctl status opsmesh-agent
```

#### 3.1.4 优雅退出

控制面收到 SIGTERM 后将在 `--shutdown-timeout`（默认 15s）窗口内完成：

1. 停止接收新请求
2. 等待在飞任务租约过期或被回收
3. 释放 leader 身份（若为 leader）
4. 关闭 MySQL/Redis 连接
5. 退出进程

> **注意**：K8s 滚动更新默认 `terminationGracePeriodSeconds=30s`，须保证 > `--shutdown-timeout`。

### 3.2 健康检查

#### 3.2.1 健康端点

| 端点 | 用途 | 期望响应 |
|---|---|---|
| `GET /healthz` | 存活/就绪探针 | `ok`（200） |
| `GET /metrics` | Prometheus 采集 | Prometheus 文本格式 |
| `GET /` | 仪表盘 | HTML |

#### 3.2.2 健康检查命令

命令示例：健康检查

```bash
# 控制面健康
curl -fsS http://<cp>:8080/healthz

# distroless 镜像无 curl，用内置 --health 子命令
docker exec opsmesh-cp /usr/local/bin/opsmesh --health

# K8s 探针状态
kubectl describe pod -n opsmesh -l app.kubernetes.io/component=controlplane | grep -A5 "Liveness\|Readiness"

# Agent 在线数
curl -s http://<cp>:9091/metrics | grep opsmesh_agents_total
```

### 3.3 日志查看

#### 3.3.1 实时日志

命令示例：实时日志查看

```bash
# Docker Compose
docker compose logs -f controlplane
docker compose logs -f agent

# K8s
kubectl logs -f -n opsmesh deploy/opsmesh-controlplane
kubectl logs -f -n opsmesh -l app.kubernetes.io/component=agent --tail=100

# systemd
journalctl -u opsmesh-controlplane -f
journalctl -u opsmesh-agent -f --since "10 min ago"
```

#### 3.3.2 历史日志检索

```bash
# K8s 历史 Pod 日志（需在 Pod 删除前抓取）
kubectl logs -n opsmesh --previous <pod-name>

# Loki / ES 后端检索（需 --log-backend=loki/es）
# Loki LogQL
logcli query --addr=http://loki:3100 '{service="opsmesh-controlplane"} |= "ERROR"'

# Elasticsearch KQL
curl -s http://es:9200/opsmesh-logs/_search?q=level:ERROR | jq .
```

### 3.4 用户与租户管理

#### 3.4.1 用户管理

通过控制面 REST API 或仪表盘「用户管理」页操作。所有请求须携带 `X-Tenant-ID` 头（开启 `--require-auth` 后强制）。

命令示例：用户管理 API

```bash
CP=http://controlplane:8080
TENANT=default
AUTH="Authorization: Bearer <admin-token>"

# 创建用户（管理员）
curl -X POST "$CP/api/v1/users" -H "$AUTH" -H "X-Tenant-ID: $TENANT" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"<strong-pass>","roles":["operator"]}'

# 列出用户
curl "$CP/api/v1/users" -H "$AUTH" -H "X-Tenant-ID: $TENANT"

# 审批 pending 用户（公开注册场景）
curl -X POST "$CP/api/v1/users/alice/approve" -H "$AUTH" -H "X-Tenant-ID: $TENANT"

# 禁用用户
curl -X PATCH "$CP/api/v1/users/alice" -H "$AUTH" -H "X-Tenant-ID: $TENANT" \
  -H "Content-Type: application/json" -d '{"status":"disabled"}'
```

#### 3.4.2 租户管理

- 多租户 schema 隔离：开启 `--multi-schema=true --schema-prefix=opsmesh_tenant_`，每租户路由到独立 MySQL schema `opsmesh_tenant_<tenant-id>`
- 创建租户：通过 `POST /api/v1/tenants` 创建，首次访问自动建表（幂等）
- 跨租户查询：须走联邦或上层聚合，单库不跨 schema

命令示例：租户管理 API

```bash
# 创建租户
curl -X POST "$CP/api/v1/tenants" -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"tenant_id":"t1","name":"业务线A"}'

# 查询租户列表
curl "$CP/api/v1/tenants" -H "$AUTH"
```

#### 3.4.3 配额调整

开启 `--quota-enabled=true` 后，设备/任务/告警创建前会校验是否超额。

命令示例：配额调整

```bash
# 设置租户配额
curl -X PUT "$CP/api/v1/tenants/t1/quota" -H "$AUTH" -H "X-Tenant-ID: t1" \
  -H "Content-Type: application/json" \
  -d '{"max_devices":200,"max_tasks":1000,"max_alerts":100}'

# 查询当前用量
curl "$CP/api/v1/tenants/t1/quota" -H "$AUTH" -H "X-Tenant-ID: t1"
```

### 3.5 设备纳管

#### 3.5.1 自动纳管（推荐）

通过 `--discover --segment-cidr=10.30.0.0/24 --auto-provision --provision-ssh-key=/path/to/key` 开启：

1. 控制面扫描 `--segment-cidr` 网段存活主机
2. 自动登记候选设备
3. 通过 SSH 推送 bootstrap 命令安装 agent
4. agent 注册回控制面，完成纳管闭环

命令示例：手动触发纳管

```bash
# 触发网段扫描
curl -X POST "$CP/api/v1/provision/scan" -H "$AUTH" -H "X-Tenant-ID: $TENANT" \
  -H "Content-Type: application/json" \
  -d '{"cidr":"10.30.0.0/24","segment":"seg-a"}'

# 查询候选设备
curl "$CP/api/v1/devices?status=candidate" -H "$AUTH" -H "X-Tenant-ID: $TENANT"
```

#### 3.5.2 手动纳管（bootstrap）

命令示例：手动 bootstrap 安装 agent

```bash
# 从控制面获取 bootstrap 脚本（含一次性 install token）
curl -fsS http://<cp>:8080/install.sh | bash

# 或直接下载 agent 二进制
curl -fsS http://<cp>:8080/bin/opsmesh-agent -o /usr/local/bin/opsmesh-agent
chmod +x /usr/local/bin/opsmesh-agent

# 启动 agent
opsmesh-agent --mode=agent \
  --control-addr=http://<cp>:8080 \
  --segment=seg-a \
  --install-token=<token>
```

#### 3.5.3 设备退役

```bash
# 手动退役
curl -X DELETE "$CP/api/v1/devices/<device-id>" -H "$AUTH" -H "X-Tenant-ID: $TENANT"

# 自动归档：agent 最后心跳早于 --archive-age-min 的设备自动 retired
# 默认 1440 分钟（24 小时），<=0 关闭自动归档
```

### 3.6 Agent 升级

#### 3.6.1 K8s DaemonSet 滚动升级

```bash
# 修改 agent image tag 后
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set agent.image.tag=<new-tag> \
  -f deploy/helm/opsmesh/values-production.yaml

# 或直接 rollout
kubectl set image daemonset/opsmesh-agent \
  agent=opsmesh/opsmesh-agent:<new-tag> -n opsmesh

# 观察滚动状态
kubectl rollout status daemonset/opsmesh-agent -n opsmesh
```

#### 3.6.2 裸金属 agent 升级

命令示例：裸金属 agent 升级

```bash
# 1) 拉取新二进制
curl -fsS http://<cp>:8080/bin/opsmesh-agent -o /usr/local/bin/opsmesh-agent.new
chmod +x /usr/local/bin/opsmesh-agent.new

# 2) 原子替换
mv /usr/local/bin/opsmesh-agent.new /usr/local/bin/opsmesh-agent

# 3) 重启 agent
systemctl restart opsmesh-agent
```

#### 3.6.3 批量操作

通过控制面下发任务到一组 agent：

命令示例：批量下发任务

```bash
# 批量下发 shell 任务到指定 segment
curl -X POST "$CP/api/v1/tasks" -H "$AUTH" -H "X-Tenant-ID: $TENANT" \
  -H "Content-Type: application/json" \
  -d '{
    "type":"shell",
    "command":"uname -a",
    "target":{"segment":"seg-a"},
    "timeout":60
  }'

# 查询任务结果
curl "$CP/api/v1/tasks/<task-id>/result" -H "$AUTH" -H "X-Tenant-ID: $TENANT"
```

---

## 第4章 监控告警

### 4.1 Prometheus Metrics

控制面在 `--metrics-port`（默认 9091）暴露 `/metrics` 端点，关键指标：

表：关键 Prometheus 指标说明表

| 指标 | 类型 | 说明 |
|---|---|---|
| `opsmesh_agents_total` | gauge | 在线 agent 总数 |
| `opsmesh_tasks_total{status}` | counter | 任务总数（按状态分：pending/running/succeeded/failed） |
| `opsmesh_task_queue_depth` | gauge | 任务队列深度 |
| `opsmesh_devices_total{status}` | gauge | 设备总数（按状态分：online/offline/retired） |
| `opsmesh_alerts_total{severity}` | counter | 告警总数 |
| `opsmesh_http_request_duration_seconds` | histogram | HTTP 请求耗时 |
| `opsmesh_grpc_request_total` | counter | gRPC 请求总数 |
| `opsmesh_leader_elections_total` | counter | leader 选举次数 |

### 4.2 ServiceMonitor 配置

Helm Chart 内置 `templates/servicemonitor.yaml`，开启方式：

```yaml
observability:
  serviceMonitor:
    enabled: true
    interval: 30s
    scrapeTimeout: 10s
```

或通过 `podAnnotations` 兼容非 Prometheus Operator 集群：

```yaml
podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "9091"
```

### 4.3 内置告警规则

Helm Chart 内置 `templates/prometheusrule.yaml`，开启方式：

```yaml
observability:
  prometheusRule:
    enabled: true
```

内置三条规则：

| 告警名 | 表达式 | 触发条件 | 严重级别 |
|---|---|---|---|
| `OpsMeshAgentsOffline` | `opsmesh_agents_total < 1` | 在线 agent 数 <1 持续 5m | critical |
| `OpsMeshTaskFailureRateHigh` | `rate(opsmesh_tasks_total{status="failed"}[5m]) / rate(opsmesh_tasks_total[5m]) > 0.3` | 任务失败率 >30% 持续 5m | warning |
| `OpsMeshTaskQueueBacklog` | `opsmesh_task_queue_depth > 100` | 队列深度 >100 持续 5m | warning |

### 4.4 自定义告警规则

命令示例：自定义 PrometheusRule

```yaml:opsmesh-custom-rules.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: opsmesh-custom
  namespace: opsmesh
spec:
  groups:
    - name: opsmesh.custom
      rules:
        - alert: OpsMeshHighLatency
          expr: histogram_quantile(0.95, rate(opsmesh_http_request_duration_seconds_bucket[5m])) > 1
          for: 10m
          labels:
            severity: warning
            component: controlplane
          annotations:
            summary: "OpsMesh P95 延迟超过 1s"
            description: "控制面 P95 HTTP 延迟 {{ $value }}s 已持续 10m"
        - alert: OpsMeshMySQLBackupStale
          expr: time() - max(kube_job_status_completion_time{job_name=~"opsmesh-mysql-backup.*",condition="Complete"}) > 93600
          for: 5m
          labels:
            severity: p1
          annotations:
            summary: "OpsMesh MySQL 备份已超过 26 小时未成功"
```

```bash
kubectl apply -f opsmesh-custom-rules.yaml -n opsmesh
```

### 4.5 通知渠道配置

#### 4.5.1 单渠道（环境变量）

命令示例：配置飞书告警

```bash
export OPSMESH_ALERT_WEBHOOK_URL="https://open.feishu.cn/open-apis/bot/v2/hook/xxx"
export OPSMESH_ALERT_NOTIFIER_TYPE="feishu"
```

通知类型自动识别：URL 含 `slack.com` 走 Slack Block Kit；含 `qyapi.weixin.qq.com` 走企业微信 markdown。

#### 4.5.2 多渠道（JSON 配置文件）

```json:notify-channels.json
{
  "channels": [
    {"type": "dingtalk", "webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx", "secret": "SECxxx"},
    {"type": "feishu", "webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", "secret": "xxx"},
    {"type": "wechat", "webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"},
    {"type": "slack", "webhook_url": "https://hooks.slack.com/services/xxx", "channel": "#ops"},
    {"type": "email", "smtp_host": "smtp.example.com", "smtp_port": 25, "username": "u", "password": "p", "from": "ops@example.com", "to": ["ops1@example.com", "ops2@example.com"]}
  ]
}
```

```bash
./opsmesh --mode=controlplane --notify-channels-config=/etc/opsmesh/notify-channels.json
```

#### 4.5.3 告警抑制

```json:inhibit-rules.json
[
  {
    "source_match": {"alertname": "OpsMeshAgentsOffline"},
    "target_match": {"component": "agent"},
    "equal": ["tenant"]
  }
]
```

```bash
./opsmesh --mode=controlplane --inhibit-rules-file=/etc/opsmesh/inhibit-rules.json
```

### 4.6 OpenTelemetry 链路追踪

```bash
export OPSMESH_OTEL_ENDPOINT="otel-collector:4317"
export OPSMESH_OTEL_SERVICE_NAME="opsmesh-controlplane"
```

启用后控制面 HTTP + agent gRPC 自动埋点，trace_id 贯穿 agent → 控制面 → store。

---

## 第5章 日志管理

### 5.1 日志级别

OpsMesh 使用 Go `slog` 结构化日志，默认级别 `INFO`。生产建议保持 `INFO`，排查时可临时调到 `DEBUG`：

> **注意**：当前版本未暴露 `--log-level` flag，日志级别由代码控制。如需调试级别，可通过环境变量 `OPSMESH_LOG_LEVEL=debug`（若代码支持）或修改源码重新构建。

### 5.2 日志后端选择

表：日志后端对照表

| 后端 | flag 值 | 适用规模 | 说明 |
|---|---|---|---|
| memory | `--log-backend=memory` | 小规模/demo | 环形缓冲，重启丢失 |
| sql | `--log-backend=sql` | 中规模 | 写入 MySQL，与业务库共用 |
| loki | `--log-backend=loki` | 大规模 | Grafana Loki，需 `--loki-endpoint` |
| es | `--log-backend=es` | 大规模 | Elasticsearch，需 `--es-endpoint` + `--es-index` |

### 5.3 日志轮转

#### 5.3.1 systemd journald

systemd 部署日志走 journald，自动轮转。配置 `/etc/systemd/journald.conf`：

```ini
SystemMaxUse=2G
SystemKeepFree=1G
MaxRetentionSec=30day
```

#### 5.3.2 K8s 容器日志

K8s 容器日志由 kubelet 管理，配置节点 `/etc/kubernetes/kubelet-config.yaml`：

```yaml
logRotation: true
maxLogSize: "100Mi"
```

#### 5.3.3 文件日志轮转（logrotate）

若 agent 通过 `--log-push-files` 采集应用日志，需配合 logrotate：

```text:/etc/logrotate.d/opsmesh
/var/log/opsmesh/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

### 5.4 日志检索

#### 5.4.1 Loki 检索

命令示例：Loki LogQL 检索

```bash
# 查询最近 1 小时 ERROR 日志
logcli query --addr=http://loki:3100 \
  '{service="opsmesh-controlplane"} |= "ERROR" | json | level="error"' \
  --since=1h

# 查询特定租户
logcli query --addr=http://loki:3100 \
  '{service="opsmesh-controlplane",tenant="t1"}' --since=24h
```

#### 5.4.2 Elasticsearch 检索

命令示例：ES KQL 检索

```bash
# 查询 ERROR 日志
curl -s "http://es:9200/opsmesh-logs/_search?q=level:error&size=100" | jq '.hits.hits[]._source'

# 聚合查询：按 level 统计
curl -s "http://es:9200/opsmesh-logs/_search" -H "Content-Type: application/json" -d '{
  "size": 0,
  "aggs": {"by_level": {"terms": {"field": "level.keyword"}}}
}' | jq '.aggregations'
```

### 5.5 日志归档

#### 5.5.1 Agent 日志采集推送

开启 `--log-push-enabled=true`，agent 端尾随日志文件批量推送到 Loki/ES：

```bash
./opsmesh --mode=agent \
  --log-push-enabled \
  --log-push-files=/var/log/syslog,/var/log/app.log \
  --log-push-pattern='^ERROR|^WARN' \
  --log-push-endpoint=http://loki:3100/loki/api/v1/push \
  --log-push-backend=loki
```

#### 5.5.2 长期归档

Loki/ES 后端配置保留策略：

- Loki：`retention_period: 30d`（配置文件）
- ES：ILM 策略，30 天后转 warm，90 天后 delete

---

## 第6章 数据库运维

### 6.1 备份

#### 6.1.1 自动备份（CronJob）

Helm Chart 内置 `templates/mysql-backup-cronjob.yaml`，启用条件：`mysql.enabled=true && controlplane.store=mysql && mysql.backup.enabled=true`。

```yaml:values.yaml
mysql:
  backup:
    enabled: true
    schedule: "0 2 * * *"        # 每天 02:00
    retentionDays: 7             # 本地保留 7 天
    historyLimit: 7
    failedHistoryLimit: 3
    backoffLimit: 2
    activeDeadlineSeconds: 1800  # 单次最多 30 分钟
    storageSize: 20Gi
    storageClass: ""
    resources:
      requests: { cpu: 100m, memory: 128Mi }
      limits:   { cpu: 500m, memory: 512Mi }
```

执行流程：

1. CronJob 触发，Pod 挂载 `opsmesh-mysql-backup` PVC 到 `/backup`
2. `mysqldump --single-transaction`（InnoDB MVCC 一致性快照，不锁表）管道 `gzip` 写 `/backup/opsmesh-<UTC时间戳>.sql.gz`
3. `gzip -t` 完整性自检，失败即删档并 `exit 1`
4. `find /backup -mtime +7 -delete` 清理超龄文件

#### 6.1.2 手动触发备份

命令示例：手动触发备份

```bash
kubectl create job --from=cronjob/opsmesh-mysql-backup \
  -n opsmesh manual-backup-$(date +%s)

# 查看备份文件
kubectl exec -n opsmesh job/manual-backup-<ts> -- ls -lt /backup
```

#### 6.1.3 异地副本

集群内 PVC 与 MySQL 同集群，单集群全损场景下两者一起丢失。生产必须叠加异地副本：

- **方案 A（推荐）**：CronJob 末尾追加 `rclone copy` 推到对象存储（S3/OBS），生命周期 30 天
- **方案 B**：Velero 整集群备份到对象存储，每日 1 次
- **加密**：对象存储启用 SSE-S3 / OBS KMS；客户端加密在 gzip 后叠加 `openssl enc -aes-256-cbc`

### 6.2 恢复

详见 [dr-runbook.md 第3章 恢复步骤](./dr-runbook.md#第3章-恢复步骤)。核心流程：

命令示例：MySQL 全量恢复

```bash
# 1) 取最新备份
LATEST=$(kubectl exec -n opsmesh job/opsmesh-mysql-backup-<ts> -- ls -1t /backup | head -1)

# 2) 缩容控制面，停止写入
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=0

# 3) 等待 MySQL Pod Ready
kubectl wait pod -n opsmesh -l app.kubernetes.io/component=mysql \
  --for=condition=Ready --timeout=120s

# 4) 灌库
kubectl exec -n opsmesh -c mysql opsmesh-mysql-0 -- sh -c '
  gunzip -c /backup/'"${LATEST}"' | mysql -uroot -p"$MYSQL_ROOT_PASSWORD"
'

# 5) 校验关键表
kubectl exec -n opsmesh -c mysql opsmesh-mysql-0 -- mysql -uroot -p"$MYSQL_ROOT_PASSWORD" \
  -e "USE opsmesh; SELECT COUNT(*) AS agents FROM agents; SELECT COUNT(*) AS tasks FROM tasks;"

# 6) 扩容控制面
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=3
```

### 6.3 迁移

#### 6.3.1 Schema 迁移

OpsMesh 启动时自动建表（幂等），无需手动迁移。版本升级时若 schema 变更，由控制面启动期 `Migrate()` 自动执行。

#### 6.3.2 MySQL 实例迁移

命令示例：MySQL 实例迁移

```bash
# 1) 旧实例导出
mysqldump -h old-mysql -uroot -p --single-transaction --all-databases | gzip > dump.sql.gz

# 2) 新实例导入
gunzip -c dump.sql.gz | mysql -h new-mysql -uroot -p

# 3) 更新控制面 DSN
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set mysql.image=mysql:8 \
  -f deploy/helm/opsmesh/values-production.yaml

# 4) 切换后观察控制面日志确认连接正常
kubectl logs -n opsmesh deploy/opsmesh-controlplane --tail=50 | grep -i mysql
```

### 6.4 慢查询排查

#### 6.4.1 开启慢查询日志

```sql
SET GLOBAL slow_query_log = 'ON';
SET GLOBAL long_query_time = 2;
SET GLOBAL slow_query_log_file = '/var/log/mysql/slow.log';
```

#### 6.4.2 排查命令

命令示例：慢查询排查

```bash
# 实时慢查询
kubectl exec -n opsmesh opsmesh-mysql-0 -- tail -f /var/lib/mysql/slow.log

# 用 pt-query-digest 分析
kubectl exec -n opsmesh opsmesh-mysql-0 -- cat /var/lib/mysql/slow.log | pt-query-digest

# 查看当前活跃连接
kubectl exec -n opsmesh opsmesh-mysql-0 -- \
  mysql -uroot -p -e "SHOW PROCESSLIST;"
```

### 6.5 容量监控

表：MySQL 容量监控项

| 监控项 | 方法 | 告警阈值 |
|---|---|---|
| 数据卷已用 | `kubelet_volume_stats_used_bytes / kubelet_volume_stats_capacity_bytes` | >80% warning，>90% critical |
| 表空间 | `SELECT table_schema, SUM(data_length+index_length)/1024/1024 AS mb FROM information_schema.tables GROUP BY table_schema` | 较上周增长 >20% |
| 连接数 | `SHOW GLOBAL STATUS LIKE 'Threads_connected'` | >80% max_connections |
| InnoDB 缓冲池命中率 | `Innodb_buffer_pool_read_requests - Innodb_buffer_pool_reads` / `Innodb_buffer_pool_read_requests` | <95% |
| 慢查询数 | `SHOW GLOBAL STATUS LIKE 'Slow_queries'` | 每分钟 >10 |

### 6.6 分库分表

#### 6.6.1 多租户 schema 隔离

开启 `--multi-schema=true --schema-prefix=opsmesh_tenant_`，每租户独立 schema：

```
opsmesh_tenant_t1  ← 租户 t1 数据
opsmesh_tenant_t2  ← 租户 t2 数据
opsmesh_tenant_t3  ← 租户 t3 数据
```

首次访问某租户 schema 时自动建表（幂等）。跨租户查询须走联邦或上层聚合。

#### 6.6.2 大表归档

对 `tasks` / `task_results` / `audit_logs` 等增长型表，建议按时间分区或定期归档：

```sql
-- 归档 90 天前任务到历史表
CREATE TABLE tasks_archive LIKE tasks;
INSERT INTO tasks_archive
  SELECT * FROM tasks WHERE created_at < DATE_SUB(NOW(), INTERVAL 90 DAY);
DELETE FROM tasks WHERE created_at < DATE_SUB(NOW(), INTERVAL 90 DAY);
```

---

## 第7章 扩缩容

### 7.1 控制面水平扩展

#### 7.1.1 手动扩缩容

命令示例：控制面扩缩容

```bash
# K8s
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=5

# Helm
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set controlplane.replicaCount=5 \
  -f deploy/helm/opsmesh/values-production.yaml
```

> **注意**：`replicas>1` 必须用 `--store=mysql`，否则 memory store 多副本数据分裂（启动期 Validate 拒绝）。

#### 7.1.2 HPA 自动扩缩容

```yaml
controlplane:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    # targetMemoryUtilizationPercentage: 80  # 可选
```

#### 7.1.3 扩容前检查

- [ ] MySQL 连接池上限足够（新副本会增加连接数）
- [ ] Redis 连接数上限足够
- [ ] 节点资源充足（CPU/内存）
- [ ] Pod 反亲和配置正确（避免调度到同节点）
- [ ] `PodDisruptionBudget` 已配置（minAvailable=1）

### 7.2 Agent 扩容

#### 7.2.1 K8s DaemonSet

DaemonSet 自动在新节点调度 agent，无需手动操作。新增节点：

```bash
# 加入节点后自动调度
kubectl get pods -n opsmesh -l app.kubernetes.io/component=agent -o wide
```

#### 7.2.2 裸金属 agent 扩容

命令示例：裸金属 agent 扩容

```bash
# 在新机器上执行 bootstrap
curl -fsS http://<cp>:8080/install.sh | bash

# 或手动安装
curl -fsS http://<cp>:8080/bin/opsmesh-agent -o /usr/local/bin/opsmesh-agent
chmod +x /usr/local/bin/opsmesh-agent
opsmesh-agent --mode=agent --control-addr=http://<cp>:8080 --segment=seg-a
```

#### 7.2.3 调整 worker 并发度

```bash
# K8s
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set agent.workerConcurrency=16 \
  -f deploy/helm/opsmesh/values-production.yaml

# 裸金属：修改环境变量后重启
export OPSMESH_WORKER_CONCURRENCY=16
systemctl restart opsmesh-agent
```

### 7.3 数据库扩容

#### 7.3.1 MySQL 扩容

```bash
# 扩容 PVC（需 StorageClass 支持 ExpandInUsePersistentVolumes）
kubectl patch pvc data-opsmesh-mysql-0 -n opsmesh \
  -p '{"spec":{"resources":{"requests":{"storage":"200Gi"}}}}'

# 观察扩容状态
kubectl get pvc -n opsmesh -l app.kubernetes.io/component=mysql
```

#### 7.3.2 MySQL 资源扩容

```bash
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set mysql.resources.limits.cpu=4000m \
  --set mysql.resources.limits.memory=8Gi \
  -f deploy/helm/opsmesh/values-production.yaml
```

#### 7.3.3 MySQL 主从复制（规划中）

MVP 基线为单副本 StatefulSet。如需更高可用，引入主从复制：

- 主：读写
- 从：只读（控制面读流量分流）
- binlog 流式备份，RPO 从 24h 降至秒级

### 7.4 Redis 扩容

```bash
# 扩容 PVC
kubectl patch pvc data-opsmesh-redis-0 -n opsmesh \
  -p '{"spec":{"resources":{"requests":{"storage":"50Gi"}}}}'

# 资源扩容
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set redis.resources.limits.cpu=2000m \
  --set redis.resources.limits.memory=2Gi \
  -f deploy/helm/opsmesh/values-production.yaml
```

多副本 HA 须配置 `--session-store=redis://host:port`，否则登出/限流/改密令牌不跨副本共享。

---

## 第8章 故障排查

### 8.1 常见故障速查

表：常见故障速查表

| 现象 | 可能原因 | 诊断步骤 | 处置 |
|---|---|---|---|
| 控制面启动失败 `store=memory 不支持多副本` | `replicas>1` 但 `store=memory` | 查启动日志 | 改 `--store=mysql` |
| 控制面启动失败 `生产模式必须配置 TLS` | `--production=true` 但 `--tls-cert` 为空 | 查启动日志 | 配置 TLS 证书或关闭 `--production` |
| 控制面启动失败 `jwt-secret 长度不足` | JWT 密钥 <32 字节 | 查启动日志 | `openssl rand -hex 32` 重新生成 |
| Agent 注册失败 `connection refused` | 控制面不可达 | `nc -zv <cp> 8080` | 检查控制面状态/网络/防火墙 |
| Agent 注册失败 `signature verification failed` | `--grpc-require-signature=true` 但 agent 未配置签名密钥 | 查 agent 日志 | 配置 `--grpc-signature-key` 一致密钥 |
| 任务持续 `pending` | 无 agent 在线或 worker 池满 | 查 `opsmesh_agents_total` / `opsmesh_task_queue_depth` | 扩容 agent 或调大 `--worker-concurrency` |
| 任务 `failed` 重试达上限 | agent 执行失败 | 查任务结果日志 | 修复命令/环境后重投 |
| 用户登录 401 | JWT 密钥不一致（多副本） | 查各副本 `OPSMESH_JWT_SECRET` | 统一密钥后重启 |
| 用户登录后立即掉线 | `--cookie-secure=true` 但走 HTTP | 查请求协议 | 改走 HTTPS 或关闭 `--cookie-secure` |
| MySQL 连接耗尽 | 连接池太小或慢查询堆积 | `SHOW PROCESSLIST` | 调大连接池/优化慢查询 |
| 备份 Job 失败 | MySQL 不可达或 PVC 满 | `kubectl logs job/<name>` | 修 MySQL/扩 PVC |

### 8.2 诊断步骤通用流程

图：故障诊断通用流程图

```
故障现象
   │
   ▼
┌─────────────────────────┐
│ 1. 控制面健康？           │ ── 否 ──→ 查控制面 Pod/进程/日志
│    curl /healthz         │
└─────────────────────────┘
   │ 是
   ▼
┌─────────────────────────┐
│ 2. MySQL 可连？           │ ── 否 ──→ 查 MySQL Pod/DSN/网络
│    控制面日志无 dial 错误  │
└─────────────────────────┘
   │ 是
   ▼
┌─────────────────────────┐
│ 3. Redis 可连？           │ ── 否 ──→ 查 Redis Pod/地址/网络
│    控制面日志无 dial 错误  │
└─────────────────────────┘
   │ 是
   ▼
┌─────────────────────────┐
│ 4. Agent 在线？           │ ── 否 ──→ 查 agent Pod/日志/网络
│    opsmesh_agents_total  │
└─────────────────────────┘
   │ 是
   ▼
┌─────────────────────────┐
│ 5. 任务正常调度？         │ ── 否 ──→ 查队列/worker/租约
│    opsmesh_task_queue_depth │
└─────────────────────────┘
   │ 是
   ▼
┌─────────────────────────┐
│ 6. 业务侧问题             │ ──→ 查具体 API/任务/告警
└─────────────────────────┘
```

### 8.3 控制面故障

#### 8.3.1 主副本故障

控制面多副本 + leader 选举（`leaderTTLSec=15`），主副本故障自动切换：

命令示例：控制面主副本故障处置

```bash
# 1) 确认 leader 已切换
kubectl logs -n opsmesh deploy/opsmesh-controlplane --tail=50 | grep -iE "leader|elect"

# 2) 确认副本数符合预期
kubectl get deploy opsmesh-controlplane -n opsmesh -o jsonpath='{.status.readyReplicas}'

# 3) 若未自动切，强制重启故障 Pod
kubectl delete pod -n opsmesh <faulty-controlplane-pod>
```

预期 RTO ≤ 15s（leader TTL）。详见 [dr-runbook.md 第5.1节](./dr-runbook.md#51-控制面主副本故障)。

#### 8.3.2 全副本故障

控制面无状态，K8s 自动重调度：

```bash
kubectl rollout restart deploy/opsmesh-controlplane -n opsmesh
kubectl rollout status deploy/opsmesh-controlplane -n opsmesh
```

预期 RTO ≤ 2min。详见 [dr-runbook.md 第5.1节](./dr-runbook.md#51-控制面主副本故障)。

### 8.4 MySQL 故障

详见 [dr-runbook.md 第5.2节](./dr-runbook.md#52-mysql-单点故障)。按 PVC 是否存活分流：

```bash
# 1) 诊断 PVC 状态
kubectl get pvc -n opsmesh -l app.kubernetes.io/component=mysql
kubectl get pvc opsmesh-mysql-backup -n opsmesh

# 2a) PVC data 还在：直接重启 StatefulSet
kubectl rollout restart statefulset opsmesh-mysql -n opsmesh

# 2b) PVC data 丢失：走全量恢复流程（见第6.2节）
```

### 8.5 Agent 失联

单台 agent 失联不影响平台和其他 agent。详见 [dr-runbook.md 第5.3节](./dr-runbook.md#53-agent-失联)。

```bash
# 查失联 agent
kubectl get pods -n opsmesh -l app.kubernetes.io/component=agent --field-selector status.phase!=Running

# 节点级故障：K8s 重调度 DaemonSet 自动恢复
# 节点存活但 agent 异常：kubectl delete pod <agent-pod> 触发重建
```

失联期间该节点任务置 `failed`（租约超期回收），可在 agent 恢复后手动重投。

### 8.6 全中心灾难

详见 [dr-runbook.md 第5.4节](./dr-runbook.md#54-全中心灾难切换到灾备中心)。预期 RTO ≤ 4h。

### 8.7 恢复后验证

表：恢复后验证项

| 验证项 | 命令 | 期望 |
|---|---|---|
| 控制面健康 | `curl http://<cp>:8080/healthz` | `ok` |
| gRPC 端口 | `nc -zv <cp> 9090` | succeeded |
| MySQL 连接 | 控制面日志无 `dial tcp ...:3306` 错误 | 无错误 |
| Agent 在线数 | 仪表盘设备页 / `opsmesh_agents_total` | 与故障前一致 |
| 任务可下发 | 下发 `echo ok` 测试任务 | 成功执行 |
| 审计留痕 | 查审计表最近恢复后记录 | 恢复操作有记录 |

---

## 第9章 安全运维

### 9.1 证书续期

#### 9.1.1 查看证书有效期

命令示例：查看证书有效期

```bash
# gRPC TLS 证书
openssl x509 -in /etc/opsmesh/tls/server.crt -noout -dates

# K8s Secret 证书
kubectl get secret opsmesh-tls -n opsmesh -o jsonpath='{.data.tls\.crt}' \
  | base64 -d | openssl x509 -noout -dates
```

#### 9.1.2 自动续期（cert-manager）

```yaml:tls-secret.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: opsmesh-tls
  namespace: opsmesh
spec:
  secretName: opsmesh-tls
  duration: 2160h        # 90 天
  renewBefore: 360h      # 提前 15 天续期
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - opsmesh.example.com
```

#### 9.1.3 手动续期

命令示例：手动续期证书

```bash
# 1) 生成新证书（Let's Encrypt / 企业 PKI）
certbot certonly --standalone -d opsmesh.example.com

# 2) 更新 K8s Secret
kubectl create secret tls opsmesh-tls \
  --cert=/etc/letsencrypt/live/opsmesh.example.com/fullchain.pem \
  --key=/etc/letsencrypt/live/opsmesh.example.com/privkey.pem \
  -n opsmesh --dry-run=client -o yaml | kubectl apply -f -

# 3) 触发控制面重启（若未启用 --tls-watch）
kubectl rollout restart deploy/opsmesh-controlplane -n opsmesh
```

#### 9.1.4 TLS 证书热重载

启用 `--tls-watch=true` 后，fsnotify 监听证书文件变更，自动重载无需重启：

```bash
./opsmesh --mode=controlplane \
  --tls-cert=/etc/opsmesh/tls/server.crt \
  --tls-key=/etc/opsmesh/tls/server.key \
  --tls-watch
```

### 9.2 密钥轮换

#### 9.2.1 JWT 密钥轮换

> **注意**：轮换 JWT 密钥会导致所有已签发 token 失效，用户须重新登录。

命令示例：JWT 密钥轮换

```bash
# K8s：删除旧 Secret 后 helm upgrade 重新生成
kubectl delete secret opsmesh-secret -n opsmesh
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set controlplane.jwtSecret=$(openssl rand -hex 32) \
  -f deploy/helm/opsmesh/values-production.yaml

# systemd：修改环境变量后重启
NEW_SECRET=$(openssl rand -hex 32)
sed -i "s/^OPSMESH_JWT_SECRET=.*/OPSMESH_JWT_SECRET=${NEW_SECRET}/" /etc/opsmesh/opsmesh-controlplane.env
systemctl restart opsmesh-controlplane
```

#### 9.2.2 provision-secret 轮换

```bash
kubectl delete secret opsmesh-secret -n opsmesh
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set controlplane.provisionSecret=$(openssl rand -hex 32) \
  -f deploy/helm/opsmesh/values-production.yaml
```

#### 9.2.3 kubeconfig 加密密钥轮换

> **注意**：轮换 `--encryption-key` 前须重新加密所有 kubeconfig，否则旧数据无法解密。

```bash
NEW_KEY=$(openssl rand 32 | base64)
# 1) 用旧密钥解密所有 kubeconfig
# 2) 用新密钥重新加密
# 3) 更新配置并重启
export OPSMESH_ENCRYPTION_KEY=${NEW_KEY}
systemctl restart opsmesh-controlplane
```

### 9.3 安全审计

#### 9.3.1 审计日志

OpsMesh 控制面记录所有关键操作到审计表（用户/租户/操作/时间/IP）。查询：

命令示例：审计日志查询

```bash
# 查询最近 24 小时审计记录
curl "$CP/api/v1/audit?since=24h" -H "$AUTH" -H "X-Tenant-ID: $TENANT"

# 按用户查询
curl "$CP/api/v1/audit?user=alice&limit=100" -H "$AUTH" -H "X-Tenant-ID: $TENANT"
```

#### 9.3.2 审计项

- 用户登录/登出/失败登录
- 用户创建/修改/禁用/审批
- 租户创建/配额调整
- 设备纳管/退役
- 任务下发/取消
- 配置变更
- API 调用（含 4xx/5xx）

#### 9.3.3 合规审计

定期导出审计日志归档：

```bash
# 导出最近 30 天审计日志
curl "$CP/api/v1/audit/export?since=720h&format=csv" \
  -H "$AUTH" -H "X-Tenant-ID: $TENANT" \
  -o audit-$(date +%Y%m%d).csv
```

### 9.4 渗透测试

#### 9.4.1 测试前检查

- [ ] 测试环境与生产隔离
- [ ] 已获书面授权
- [ ] 备份已完成
- [ ] 监控告警已配置（异常登录/批量请求）

#### 9.4.2 测试重点项

- 鉴权绕过（缺失 `X-Tenant-ID` 头）
- 越权访问（跨租户数据访问）
- SSRF（webhook URL 内网访问）
- 命令注入（agent shell 任务）
- 路径遍历（agent 文件任务）
- JWT 伪造/篡改
- SQL 注入
- XSS（仪表盘输入）

#### 9.4.3 测试后处置

- 修复发现的所有漏洞
- 复测验证修复有效
- 更新安全文档
- 归档测试报告

---

## 第10章 巡检 SOP

### 10.1 日常巡检（每日）

表：日常巡检项

| 巡检项 | 命令 | 期望 | 异常处置 |
|---|---|---|---|
| 控制面健康 | `curl http://<cp>:8080/healthz` | `ok` | 查日志/重启 |
| 控制面副本数 | `kubectl get deploy opsmesh-controlplane -n opsmesh` | readyReplicas=期望 | 查 Pod 状态 |
| Agent 在线数 | `curl http://<cp>:9091/metrics \| grep opsmesh_agents_total` | 与期望一致 | 查 agent Pod/网络 |
| MySQL 健康 | `kubectl exec opsmesh-mysql-0 -- mysqladmin ping` | alive | 查 MySQL 状态 |
| 备份 Job 成功 | `kubectl get jobs -n opsmesh -l app.kubernetes.io/component=mysql-backup` | 最近 25h 内有成功 | 手动触发/排查 |
| 任务队列深度 | `curl http://<cp>:9091/metrics \| grep opsmesh_task_queue_depth` | <100 | 扩容 agent/查阻塞 |
| 任务失败率 | `rate(opsmesh_tasks_total{status="failed"}[5m]) / rate(opsmesh_tasks_total[5m])` | <30% | 查任务日志 |
| 告警通知 | 查告警渠道 | 无未确认 critical 告警 | 处置告警 |

### 10.2 周巡检

表：周巡检项

| 巡检项 | 命令 | 期望 | 异常处置 |
|---|---|---|---|
| 备份 PVC 容量 | `kubectl get pvc opsmesh-mysql-backup -n opsmesh` | 已用 <80% | 扩容/清理 |
| MySQL 数据卷容量 | `kubectl get pvc -n opsmesh -l app.kubernetes.io/component=mysql` | 已用 <80% | 扩容 |
| MySQL 慢查询 | 查 slow.log | 无新增高频慢查询 | 优化索引/SQL |
| Redis 内存 | `redis-cli INFO memory` | used_memory <80% maxmemory | 扩容/清理 |
| 控制面资源使用 | `kubectl top pod -n opsmesh -l app.kubernetes.io/component=controlplane` | CPU/内存 <80% | 扩容/调优 |
| 证书有效期 | `openssl x509 -in server.crt -noout -dates` | 剩余 >30 天 | 续期 |
| 审计日志归档 | 导出本周审计 | 成功 | 排查 |
| 告警规则评估 | Prometheus UI | 无规则异常 | 修复规则 |

### 10.3 月巡检

表：月巡检项

| 巡检项 | 方法 | 期望 | 异常处置 |
|---|---|---|---|
| 备份可恢复性演练 | 见 [dr-runbook.md 第6.2节](./dr-runbook.md#62-演练步骤) | 灌库成功，行数一致 | 排查备份/恢复流程 |
| MySQL 容量趋势 | 对比本月与上月 | 增长 <20% | 归档/扩容 |
| 用户/租户清单 | 导出用户/租户列表 | 无异常账号 | 清理 |
| 配额使用率 | 查各租户配额 | 无超额/接近上限 | 调整配额 |
| 安全配置审计 | 对照生产检查清单 | 全部符合 | 修复 |
| 日志归档 | 归档本月日志 | 成功 | 排查 |
| 漏洞扫描 | CVE 扫描镜像/依赖 | 无高危 | 升级 |
| 性能基线 | 记录本月 P95/P99 延迟 | 与上月持平或下降 | 调优 |

### 10.4 年度审计

表：年度审计项

| 审计项 | 方法 | 期望 |
|---|---|---|
| 全中心切换演练 | 见 [dr-runbook.md 第5.4节](./dr-runbook.md#54-全中心灾难切换到灾备中心) | RTO ≤4h，数据一致 |
| 渗透测试 | 第三方安全团队 | 无高危漏洞 |
| 合规审计 | 等保三级/ISO 27001 | 全项符合 |
| 灾备预案评审 | 评审 dr-runbook | 流程可行 |
| 容量规划评审 | 预测下一年容量 | 资源充足 |
| 安全培训 | 全员安全意识培训 | 完成 |
| 密钥全量轮换 | JWT/provision/encryption 全轮换 | 完成 |
| 证书全量续期 | TLS/联邦证书全续期 | 完成 |

---

## 第11章 升级指南

### 11.1 版本升级步骤

#### 11.1.1 升级前准备

- [ ] 阅读目标版本 `CHANGELOG.md`，确认无 breaking change
- [ ] 在测试环境完整升级一次，验证业务正常
- [ ] 备份 MySQL（见第6.1节）
- [ ] 备份 Helm values：`helm get values opsmesh -n opsmesh > opsmesh-values-backup.yaml`
- [ ] 通知业务方升级窗口
- [ ] 确认回滚方案（见第11.2节）

#### 11.1.2 K8s 升级

命令示例：K8s 升级 OpsMesh

```bash
# 1) 更新镜像 tag
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set controlplane.image.tag=<new-version> \
  --set agent.image.tag=<new-version> \
  -f deploy/helm/opsmesh/values-production.yaml

# 2) 观察滚动状态
kubectl rollout status deploy/opsmesh-controlplane -n opsmesh
kubectl rollout status daemonset/opsmesh-agent -n opsmesh

# 3) 验证
curl http://<cp>:8080/healthz
kubectl get pods -n opsmesh
```

#### 11.1.3 systemd 升级

命令示例：systemd 升级

```bash
# 1) 备份旧二进制
cp /usr/local/bin/opsmesh /usr/local/bin/opsmesh.bak

# 2) 替换新二进制
install -m 0755 opsmesh-new /usr/local/bin/opsmesh

# 3) 重启
systemctl restart opsmesh-controlplane
systemctl status opsmesh-controlplane

# 4) 验证
curl http://localhost:8080/healthz
```

### 11.2 回滚

#### 11.2.1 K8s 回滚

命令示例：K8s 回滚

```bash
# 1) 回滚 Helm release
helm rollback opsmesh <previous-revision> -n opsmesh

# 2) 或回退镜像 tag
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set controlplane.image.tag=<old-version> \
  --set agent.image.tag=<old-version> \
  -f deploy/helm/opsmesh/values-production.yaml

# 3) 若涉及数据库 schema 变更，须从备份恢复（见第6.2节）
```

#### 11.2.2 systemd 回滚

```bash
# 1) 恢复旧二进制
cp /usr/local/bin/opsmesh.bak /usr/local/bin/opsmesh

# 2) 重启
systemctl restart opsmesh-controlplane
```

#### 11.2.3 数据库回滚

若升级涉及 schema 变更且无法向后兼容，须从升级前备份恢复：

```bash
# 见第6.2节 MySQL 全量恢复流程
```

### 11.3 兼容性检查

#### 11.3.1 版本兼容矩阵

升级前查阅 `CHANGELOG.md` 确认：

- 控制面与 agent 版本兼容性（通常控制面 >= agent 版本）
- API 版本兼容性（`/api/v1/` 是否有 breaking change）
- 配置 flag 兼容性（是否有 flag 被移除/重命名）
- 数据库 schema 兼容性（是否需要迁移）

#### 11.3.2 灰度升级

大规模集群建议灰度升级：

```bash
# 1) 先升级一个 agent 副本（修改 DaemonSet 的 nodeSelector 限定到测试节点）
kubectl patch daemonset opsmesh-agent -n opsmesh --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/nodeSelector","value":{"opsmesh-test":"true"}}]'

# 2) 观察测试 agent 工作正常

# 3) 全量升级
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  --set agent.image.tag=<new-version> \
  -f deploy/helm/opsmesh/values-production.yaml
```

---

## 第12章 性能调优

### 12.1 Go runtime 调优

#### 12.1.1 GOMAXPROCS

Go runtime 默认 `GOMAXPROCS=runtime.NumCPU()`，容器环境下由 cgroup CPU quota 决定。一般无需手动调整。

如需显式设置：

```bash
export GOMAXPROCS=4
```

#### 12.1.2 GC 调优

```bash
# 减少 GC 频率（吞吐优先，内存换吞吐）
export GOGC=200    # 默认 100，调大减少 GC 频率

# 或关闭 GC（仅短生命周期进程）
export GOGC=off
```

#### 12.1.3 内存限制

K8s 容器内存 limit 会自动设置 Go runtime memory limit（Go 1.19+）。控制面建议：

```yaml
controlplane:
  resources:
    requests:
      memory: 512Mi
    limits:
      memory: 2Gi    # 建议 limits = 4 * requests
```

### 12.2 数据库调优

#### 12.2.1 连接池

控制面通过 `--mysql-dsn` 配置连接池，DSN 参数：

```text
opsmesh:pass@tcp(mysql:3306)/opsmesh?parseTime=true&maxOpenConns=100&maxIdleConns=20&connMaxLifetime=5m
```

| 参数 | 建议 | 说明 |
|---|---|---|
| `maxOpenConns` | 50-200 | 最大连接数，按副本数 × 副本数核算 |
| `maxIdleConns` | 10-50 | 空闲连接数 |
| `connMaxLifetime` | 5m | 连接最大生命周期 |

#### 12.2.2 InnoDB 调优

```ini
# /etc/mysql/conf.d/opsmesh.cnf
[mysqld]
innodb_buffer_pool_size = 2G        # 物理内存 50-70%
innodb_log_file_size = 256M
innodb_flush_log_at_trx_commit = 1
innodb_flush_method = O_DIRECT
innodb_io_capacity = 2000           # SSD
innodb_io_capacity_max = 4000
query_cache_size = 0                # MySQL 8 已移除 query cache
max_connections = 500
thread_cache_size = 50
table_open_cache = 4000
```

#### 12.2.3 索引优化

定期分析慢查询日志，对高频查询建索引：

```sql
-- 查询缺失索引的表
SELECT * FROM sys.schema_missing_indexes;

-- 查询冗余索引
SELECT * FROM sys.schema_redundant_indexes;
```

### 12.3 Redis 调优

#### 12.3.1 内存配置

```ini
# /etc/redis/redis.conf
maxmemory 1gb
maxmemory-policy allkeys-lru    # LRU 淘汰
```

#### 12.3.2 持久化

```ini
# RDB 快照（默认）
save 900 1
save 300 10
save 60 10000

# AOF（更可靠，性能略降）
appendonly yes
appendfsync everysec
```

#### 12.3.3 连接池

控制面 Redis 连接池通过 `go-redis` 内部管理，默认 10 倍 CPU 数。如需调优：

> **注意**：当前版本未暴露 Redis 连接池参数 flag，由代码默认值控制。如需调优需修改源码。

### 12.4 控制面调优

#### 12.4.1 任务调度参数

表：任务调度调优参数

| 参数 | 默认 | 调优建议 | 说明 |
|---|---|---|---|
| `--task-lease-sec` | 300 | 长任务调大（600+） | 租约过短导致误重调度 |
| `--task-max-retries` | 3 | 按业务容错调整 | 过大浪费资源 |
| `--leader-ttl-sec` | 15 | 网络抖动环境调大（30） | TTL 过短误切主 |
| `--leader-tick-sec` | 5 | ttl 的 1/3 | 续租频率 |
| `--archive-age-min` | 1440 | 按设备管理策略调整 | 自动归档阈值 |

#### 12.4.2 限流与熔断

```bash
# 控制面 API 限流（每秒每 IP/tenant 100 请求）
--cb-rate-limit-per-sec=100

# 熔断阈值
--cb-failure-threshold=5
--cb-recovery-timeout=30s
--cb-half-open-max-calls=1
```

### 12.5 Agent 调优

#### 12.5.1 worker 池

```bash
# 按 CPU 核数调整（建议 = CPU 核数 × 2）
--worker-concurrency=16
```

#### 12.5.2 资源限额

```bash
# 按机器规格调整
--max-procs=512          # 最大进程数
--max-files=8192         # 最大文件描述符
--max-memory-mb=2048     # 最大内存
```

#### 12.5.3 任务超时

```bash
# 按任务类型调整
--task-timeout=300s      # 长任务调大
```

### 12.6 前端调优

#### 12.6.1 Nginx 静态资源缓存

```nginx
server {
    listen 80;
    root /path/to/web/enterprise/dist;

    # 静态资源长缓存（Vite 构建产物含 hash）
    location /assets/ {
        expires 1y;
        add_header Cache-Control "public, max-age=31536000, immutable";
    }

    # index.html 不缓存
    location = /index.html {
        add_header Cache-Control "no-cache";
    }

    # SPA 路由回退
    location / {
        try_files $uri $uri/ /index.html;
    }

    # API 反代
    location /api/v1/ {
        proxy_pass http://controlplane:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 启用 keepalive
        proxy_http_version 1.1;
        proxy_set_header Connection "";

        # 缓冲区
        proxy_buffering on;
        proxy_buffer_size 16k;
        proxy_buffers 8 16k;
    }

    # 启用 gzip
    gzip on;
    gzip_types text/plain text/css application/json application/javascript;
    gzip_min_length 1024;
}
```

#### 12.6.2 HTTP/2

```nginx
listen 443 ssl http2;
ssl_protocols TLSv1.2 TLSv1.3;
ssl_ciphers HIGH:!aNULL:!MD5;
```

#### 12.6.3 HSTS

```nginx
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
```

---

## 附录

### 附录 A 命令速查

表：常用运维命令速查表

| 用途 | 命令 |
|---|---|
| 控制面健康 | `curl http://<cp>:8080/healthz` |
| Agent 在线数 | `curl http://<cp>:9091/metrics \| grep opsmesh_agents_total` |
| 任务队列深度 | `curl http://<cp>:9091/metrics \| grep opsmesh_task_queue_depth` |
| 查最近备份 Job | `kubectl get jobs -n opsmesh -l app.kubernetes.io/component=mysql-backup \| tail -5` |
| 手动触发备份 | `kubectl create job --from=cronjob/opsmesh-mysql-backup -n opsmesh manual-backup-$(date +%s)` |
| 缩容控制面 | `kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=0` |
| 滚动重启控制面 | `kubectl rollout restart deploy/opsmesh-controlplane -n opsmesh` |
| 滚动重启 agent | `kubectl rollout restart daemonset/opsmesh-agent -n opsmesh` |
| 查控制面日志 | `kubectl logs -f -n opsmesh deploy/opsmesh-controlplane` |
| 查 agent 日志 | `kubectl logs -f -n opsmesh -l app.kubernetes.io/component=agent --tail=100` |
| 查证书有效期 | `openssl x509 -in server.crt -noout -dates` |
| 生成 JWT 密钥 | `openssl rand -hex 32` |
| 生成加密密钥 | `openssl rand 32 \| base64` |

### 附录 B 联系人与升级路径

```
P3（备份 Job 单次失败 / 单个 agent 离线）
  └─ SRE 自行处理，记录台账

P2（连续失败 / 备份体积异常 / 任务失败率高）
  └─ SRE → 通知 OpsMesh Owner，1h 内响应

P1（备份超 26h 未成功 / MySQL 不可用 / 控制面全副本故障）
  └─ 立即通知 Owner + DBA，启动恢复流程，30min 内响应

S1（全中心灾难）
  └─ 启动全中心切换，Owner 决策，4h 内恢复
```

> 联系人信息维护在团队通讯录（本文档不内嵌，避免过期）。

### 附录 C 相关文档

| 文档 | 说明 |
|---|---|
| [deployment-guide.md](./deployment-guide.md) | 部署指南（docker/systemd/helm/operator） |
| [dr-runbook.md](./dr-runbook.md) | 灾难恢复 Runbook（备份/恢复/RTO-RPO/故障切换） |
| [flag-matrix.md](./flag-matrix.md) | 配置 flag 矩阵 |
| [security-mechanism.md](./security-mechanism.md) | 安全机制说明 |
| [api-reference.md](./api-reference.md) | API 参考 |
| [architecture.md](./architecture.md) | 架构设计 |
| [database-design.md](./database-design.md) | 数据库设计 |
| [README.md](../README.md) | 项目总览 |

### 附录 D 变更记录

| 版本 | 日期 | 变更 | 作者 |
|---|---|---|---|
| v0.1 | 2026-08-17 | 初版：覆盖部署/配置/运维/监控/日志/数据库/扩缩容/故障排查/安全/巡检/升级/调优 12 章 | OpsMesh Team |