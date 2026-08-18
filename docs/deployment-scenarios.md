# OpsMesh 部署场景与方案设计文档

> 版本：v1.0 · 编制日期：2026-08-17 · 适用基线：OpsMesh MVP + M2 演进 · 编制人：部署场景文档工程师
>
> 本文档系统化定义 OpsMesh 自动化运维平台在真实生产环境下的 12 类典型部署场景，给出架构图、组件清单、网络拓扑、数据流、配置示例、容量规划、高可用与适用边界，并提供场景对比、选型决策、自动化部署与附录模板。所有配置示例与现有 `deploy/helm/opsmesh/`、`deploy/systemd/`、`docker-compose.yaml` 保持一致，可直接落地。

---

## 第1章 文档说明

### 1.1 目的与范围

本文档面向平台架构师、SRE、交付工程师与售前解决方案架构师，提供以下内容：

1. **场景全景**：覆盖单机房、异地多机房、多数据中心、电信资源池、混合云、公有云、私有云、边缘计算、国产化环境、容器化、高安全隔离、灾备与连续性共 12 类典型部署场景。
2. **可执行方案**：每个场景给出架构图、组件部署清单、网络拓扑、数据流、配置示例（docker-compose / Helm values / systemd / Terraform / Ansible）、容量规划公式与高可用策略。
3. **选型依据**：通过场景对比矩阵、选型决策树、成本估算表，给出按业务规模与合规要求的推荐配置。
4. **自动化基线**：定义部署工具链、流水线、配置管理与回滚策略，与现有 `deploy/gitops/`、`deploy/helm/`、`operator/` 闭环对齐。

范围限定于 OpsMesh 控制面 + Agent + Store（MySQL/Redis）+ 前端的部署形态；具体业务模块（CMDB、作业流、告警引擎等）的内部设计请参考 `docs/architecture.md` 与 `docs/module-design.md`。

### 1.2 设计原则

| 编号 | 原则 | 含义 |
|---|---|---|
| P1 | 单二进制双模式 | 控制面与 Agent 共用同一二进制，`--mode=controlplane\|agent` 切换，降低交付与升级成本 |
| P2 | 渐进式高可用 | 从单机 memory store 起步，按规模渐进切换 mysql + 多副本 + 联邦，避免过度设计 |
| P3 | 网段亲和 | Agent 仅与本网段控制面通信，跨网段经联邦 mTLS+HMAC 同步，避免长链路心跳拖垮 |
| P4 | 配置即代码 | 所有部署形态经 Helm Chart / Operator / Terraform / Ansible 声明式管理，禁手工 kubectl 改 |
| P5 | 安全默认关 | 公开注册、免审批、明文 Cookie、InsecureIgnoreHostKey 等不安全项默认关闭，生产模式强校验 |
| P6 | 可观测先行 | 每个场景必须配置 Prometheus 采集 + 告警 Webhook，否则视为未完成交付 |
| P7 | 演练驱动 | 灾备与切换方案必须可演练、可审计，参考 `docs/dr-runbook.md` 第 6 章 |
| P8 | 国产化可替换 | CPU/OS/DB/中间件全栈可替换为国产化组件，不绑定 x86 + Linux + MySQL |

### 1.3 术语对照

表：术语对照表

| 术语 | 全称 | 含义 |
|---|---|---|
| ControlPlane | 控制面 | OpsMesh 单二进制的 `--mode=controlplane` 形态，承载 HTTP/gRPC/Metrics 三端口 |
| Agent | 代理 | OpsMesh 单二进制的 `--mode=agent` 形态，部署到每台纳管设备 |
| Store | 存储抽象层 | 控制面与具体存储后端解耦的接口层，实现为 MemoryStore / SQLStore |
| Segment | 网段 | Agent 所属网络分段，用于多网段路由与联邦 |
| Federation | 联邦 | 跨控制面经 mTLS+HMAC 互联，同步设备视图与跨网段任务转发 |
| RTO | Recovery Time Objective | 恢复服务可用所需最大时长 |
| RPO | Recovery Point Objective | 可容忍的最大数据丢失时间窗口 |
| DC | Data Center | 数据中心 |
| DR | Disaster Recovery | 灾备中心 |
| DCN | Data Communication Network | 电信专用数据通信网 |
| HPA | Horizontal Pod Autoscaler | K8s 水平自动扩缩容 |
| PDB | Pod Disruption Budget | K8s Pod 中断预算 |
| mTLS | mutual TLS | 双向 TLS 认证 |
| HMAC | Hash-based Message Authentication Code | 哈希消息认证码，用于联邦与 Agent 签名 |
| 等保 | 网络安全等级保护 | 中国网络安全等级保护制度，分 1-5 级 |
| DCN | 电信 DCN 网 | 电信运营商内部数据通信网，与公网隔离 |

---

## 第2章 部署模式总览

### 2.1 部署模式矩阵

表：OpsMesh 部署模式矩阵表

| 模式 | 规模（纳管设备） | 高可用 | 适用场景 | 复杂度 | 控制面副本 | Store 后端 |
|---|---|:---:|---|:---:|:---:|---|
| 单机 | ≤100 | ❌ | 本地演示、PoC、开发调试 | ★ | 1 | memory |
| 集群 | ≤500 | ✅ | 单机房生产、中小型企业 | ★★ | 3 | mysql+redis |
| 分布式 | ≤10000 | ✅ | 多机房/多 DC、大规模企业 | ★★★ | 每机房 3 | 各机房 mysql+redis |
| 联邦 | ≥50000 | ✅ | 电信资源池、全国级运营商 | ★★★★ | 每网段 3 | 各网段 mysql+redis + 联邦同步 |
| 云原生 | 弹性 | ✅ | 公有云/私有云 K8s、SaaS | ★★★ | HPA 2-10 | mysql+redis (PVC) |
| 边缘 | ≤100/边缘 | 部分 | IoT、CDN 节点、门店 | ★★★ | 中心 3 + 边缘 1 | 中心 mysql + 边缘 sqlite |
| 国产化 | 任意 | ✅ | 党政军、金融、能源 | ★★★★ | 3 | 达梦/人大金仓+redis |
| 灾备 | 任意 | ✅✅ | 关键业务、监管要求 | ★★★★ | 主+备+DR | 主从复制 + 异地备份 |

### 2.2 部署架构总览

图：OpsMesh 部署模式总览架构图

```text
                            ┌─────────────────────────────────────────┐
                            │          运维用户 / SRE / API 调用方       │
                            └────────────────────┬────────────────────┘
                                                 │ HTTPS / OAuth2 / API Token
                                                 ▼
        ┌────────────────────────────────────────────────────────────────────┐
        │                       全局负载均衡 / DNS / CDN                          │
        └──────┬──────────────┬──────────────┬──────────────┬─────────────────┘
               │              │              │              │
               ▼              ▼              ▼              ▼
        ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
        │  DC1 控制面│   │  DC2 控制面│   │  DC3 控制面│   │ 边缘控制面 │
        │  (3 副本) │   │  (3 副本) │   │  (3 副本) │   │  (1 副本) │
        │  HA+PDB  │   │  HA+PDB  │   │  HA+PDB  │   │  轻量级   │
        └────┬─────┘   └────┬─────┘   └────┬─────┘   └────┬─────┘
             │               │               │               │
             │   mTLS+HMAC 联邦同步（gRPC :9090）            │
             └───────────────┴───────────────┴───────────────┘
                             │
                ┌────────────┼────────────┐
                ▼            ▼            ▼
          ┌─────────┐  ┌─────────┐  ┌─────────┐
          │ MySQL HA│  │ Redis HA│  │  对象存储 │
          │ 主从/集群│  │ 哨兵/集群│  │  备份归档 │
          └─────────┘  └─────────┘  └─────────┘
                │
                ▼
        ┌─────────────────────────────────────────────┐
        │   Agent 集群（每台纳管设备一个，DaemonSet/系统服务）  │
        │   ┌──────┐ ┌──────┐ ┌──────┐     ┌──────┐       │
        │   │Agent1│ │Agent2│ │Agent3│ ... │AgentN│       │
        │   └──────┘ └──────┘ └──────┘     └──────┘       │
        └─────────────────────────────────────────────┘
```

### 2.3 模式选择快速指引

| 问题 | 选择 |
|---|---|
| 是否 ≤100 台设备且仅用于演示？ | 单机模式 |
| 是否 ≤500 台且单机房？ | 集群模式（场景一） |
| 是否多机房且需异地容灾？ | 分布式模式（场景二/三） |
| 是否 ≥50000 台且分多级管理？ | 联邦模式（场景四） |
| 是否部署在公有云 K8s？ | 云原生模式（场景六） |
| 是否部署在 OpenStack/VMware？ | 私有云模式（场景七） |
| 是否有边缘节点？ | 边缘模式（场景八） |
| 是否要求全栈国产化？ | 国产化模式（场景九） |
| 是否要求等保三级以上？ | 高安全隔离模式（场景十一） |
| 是否要求 RTO ≤ 4h？ | 灾备模式（场景十二） |

---

## 第3章 场景一：单机房部署

### 3.1 场景描述

单机房部署是 OpsMesh 最基础的生产形态，适用于中小型企业或单一机房内的运维管理。所有控制面、Agent、Store 部署在同一机房内，网络互通，无跨机房链路。规模上限 ≤500 台纳管设备。

### 3.2 架构图

图：单机房部署架构图

```text
        ┌──────────────────────────────────────────────────────────┐
        │                       单机房（IDC-A）                       │
        │                                                          │
        │   ┌──────────────────────────────────────────────────┐   │
        │   │              接入层（LB + Ingress）                  │   │
        │   │   Nginx/HAProxy → 8080 (HTTP) / 9090 (gRPC)        │   │
        │   └───────────────────────┬──────────────────────────┘   │
        │                           │                              │
        │   ┌───────────────────────┴──────────────────────────┐   │
        │   │              控制面（3 副本 + PDB）                  │   │
        │   │   ┌─────────┐  ┌─────────┐  ┌─────────┐           │   │
        │   │   │  CP-1   │  │  CP-2   │  │  CP-3   │           │   │
        │   │   │ Leader? │  │ Standby │  │ Standby │           │   │
        │   │   └────┬────┘  └────┬────┘  └────┬────┘           │   │
        │   └────────┼─────────────┼─────────────┼──────────────┘   │
        │            │             │             │                  │
        │   ┌────────┼─────────────┼─────────────┼──────────────┐   │
        │   │        ▼             ▼             ▼              │   │
        │   │   ┌─────────┐  ┌─────────┐  ┌─────────┐          │   │
        │   │   │ MySQL   │  │ Redis   │  │  备份   │          │   │
        │   │   │ Master  │  │ Master  │  │ CronJob │          │   │
        │   │   │ +Slave  │  │ +Slave  │  │  PVC    │          │   │
        │   │   └─────────┘  └─────────┘  └─────────┘          │   │
        │   └───────────────────────────────────────────────────┘   │
        │                           │                              │
        │   ┌───────────────────────┴──────────────────────────┐   │
        │   │              Agent 集群（≤500 台）                  │   │
        │   │   ┌──────┐ ┌──────┐ ┌──────┐    ┌──────┐         │   │
        │   │   │ Ag-1 │ │ Ag-2 │ │ Ag-3 │... │Ag-500│         │   │
        │   │   └──────┘ └──────┘ └──────┘    └──────┘         │   │
        │   └───────────────────────────────────────────────────┘   │
        └──────────────────────────────────────────────────────────┘
```

### 3.3 组件清单

表：单机房部署组件清单

| 组件 | 副本数 | 资源规格 | 端口 | 备注 |
|---|:---:|---|---|---|
| ControlPlane | 3 | 2C/2Gi | 8080/9090/9091 | Pod 反亲和跨节点分散 |
| MySQL | 1主1从 | 2C/4Gi | 3306 | 主从复制，PVC 100Gi |
| Redis | 1主1从 | 1C/1Gi | 6379 | 哨兵哨兵模式 |
| Agent | ≤500 | 0.5C/512Mi | - | DaemonSet 或 systemd |
| Ingress/LB | 2 | 1C/1Gi | 80/443 | Nginx 或 HAProxy |
| Prometheus | 1 | 1C/2Gi | 9090 | 采集控制面 /metrics |
| 备份 CronJob | - | 0.5C/512Mi | - | 每日 02:00 mysqldump |

### 3.4 网络拓扑

```text
    外部运维用户
         │
         ▼ HTTPS 443
    ┌─────────┐
    │  LB     │  10.0.0.10/24
    └────┬────┘
         │ HTTP 8080 / gRPC 9090
         ▼
    ┌─────────┐ 10.0.0.20-22/24
    │ CP 集群 │
    └────┬────┘
         │
    ┌────┼────────────┐
    │    │            │
    ▼    ▼            ▼
  MySQL  Redis     Agent 集群
 10.0.1.10 10.0.1.20  10.0.2.0/24
```

### 3.5 数据流

1. 运维用户经 LB → 控制面 8080 端口访问仪表盘与 API
2. 控制面经 leader 选举（Redis 租约）确定主副本，仅主副本调度任务
3. Agent 经 gRPC 9090 注册/心跳/拉任务/上报结果
4. 控制面写入 MySQL（业务数据）+ Redis（任务租约/会话）
5. 备份 CronJob 每日 02:00 经 mysqldump 持久化到 PVC，rclone 推异地

### 3.6 配置示例

```yaml
# docker-compose.yaml（单机房生产示例）
version: "3.9"
services:
  controlplane:
    image: opsmesh/opsmesh:latest
    command: --mode=controlplane --store=mysql --production
    environment:
      OPSMESH_MYSQL_DSN: opsmesh:opsmesh@tcp(mysql:3306)/opsmesh?parseTime=true
      OPSMESH_REDIS_ADDR: redis:6379
      OPSMESH_JWT_SECRET: ${OPSMESH_JWT_SECRET}
      OPSMESH_PROVISION_SECRET: ${OPSMESH_PROVISION_SECRET}
      OPSMESH_PRODUCTION: "true"
      OPSMESH_REQUIRE_AUTH: "true"
      OPSMESH_COOKIE_SECURE: "true"
      OPSMESH_TLS_CERT: /etc/opsmesh/tls/server.crt
      OPSMESH_TLS_KEY: /etc/opsmesh/tls/server.key
    ports: ["8080:8080", "9090:9090", "9091:9091"]
    volumes:
      - ./tls:/etc/opsmesh/tls:ro
    depends_on: [mysql, redis]
    deploy:
      replicas: 3
      update_config: { parallelism: 1, order: start-first }
      restart_policy: { condition: any }
    healthcheck:
      test: ["CMD", "/usr/local/bin/opsmesh", "--health"]
      interval: 30s
      timeout: 5s
      retries: 3

  mysql:
    image: mysql:8
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD}
      MYSQL_DATABASE: opsmesh
      MYSQL_USER: opsmesh
      MYSQL_PASSWORD: ${MYSQL_PASSWORD}
    volumes:
      - mysql-data:/var/lib/mysql
    deploy:
      placement: { constraints: [node.role == manager] }

  redis:
    image: redis:7
    command: redis-server --appendonly yes --requirepass ${REDIS_PASSWORD}
    volumes:
      - redis-data:/data

  agent:
    image: opsmesh/opsmesh-agent:latest
    command: --mode=agent --control-addr=http://controlplane:8080 --segment=idc-a
    deploy:
      mode: global
    depends_on: [controlplane]

volumes:
  mysql-data:
  redis-data:
```

### 3.7 容量规划

表：单机房容量规划表（≤500 台）

| 指标 | 公式 | 500 台估算 |
|---|---|---|
| 控制面 CPU | 0.5 核 + 设备数 × 0.002 核 | 1.5 核 |
| 控制面内存 | 512Mi + 设备数 × 2Mi | 1.5Gi |
| MySQL 存储 | 10Gi + 设备数 × 50Mi + 任务数 × 10Ki | 35Gi |
| Redis 内存 | 64Mi + 设备数 × 1Mi | 564Mi |
| 任务吞吐 | 100 任务/秒（3 副本） | 满足 |
| 心跳带宽 | 设备数 × 1KB / 10s | 50KB/s |
| 并发 gRPC | 设备数 × 1 长连接 | 500 连接 |

### 3.8 高可用策略

- 控制面 3 副本 + leader 选举（`leaderTTLSec=15`，`leaderTickSec=5`），主副本故障 ≤15s 切换
- MySQL 主从复制 + 半同步，主故障切换到从（MHA/Orchestrator）
- Redis 哨兵模式，主故障自动切换
- Pod 反亲和跨节点分散，PDB minAvailable=1
- 备份 PVC + 异地副本（rclone → S3/OBS），RPO ≤ 24h，RTO ≤ 30min

### 3.9 适用场景

- 中小型企业单机房运维
- ≤500 台 Linux 设备纳管
- 无异地容灾需求或容灾由基础设施层提供
- 团队规模 ≤10 人，运维预算有限

---

## 第4章 场景二：异地多机房

### 4.1 场景描述

异地多机房部署适用于跨城市多机房的企业，主机房承担主要负载，备用房提供灾备能力，DR 站作为最后兜底。每机房部署独立控制面，经联邦同步设备视图与跨机房任务。规模上限 ≤2000 台/机房。

### 4.2 架构图

图：异地多机房部署架构图

```text
        ┌────────────────────────────────────────────────────────────────┐
        │                       全局 DNS / GSLB                            │
        └──────┬────────────────────┬────────────────────┬────────────────┘
               │                    │                    │
               ▼                    ▼                    ▼
    ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
    │   主机房 IDC-A    │  │   备用房 IDC-B    │  │    DR 站 IDC-C   │
    │   (Active)        │  │   (Warm Standby) │  │   (Cold Standby) │
    │                  │  │                  │  │                  │
    │ ┌──────────────┐ │  │ ┌──────────────┐ │  │ ┌──────────────┐ │
    │ │  控制面 ×3   │ │  │ │  控制面 ×3   │ │  │ │  控制面 ×3   │ │
    │ │  + Agent     │ │  │ │  + Agent     │ │  │ │  (+Agent)    │ │
    │ └──────┬───────┘ │  │ └──────┬───────┘ │  │ └──────┬───────┘ │
    │        │         │  │        │         │  │        │         │
    │ ┌──────┴───────┐ │  │ ┌──────┴───────┐ │  │ ┌──────┴───────┐ │
    │ │ MySQL 主从   │ │  │ │ MySQL 主从   │ │  │ │ MySQL 单副本 │ │
    │ │ Redis 哨兵   │ │  │ │ Redis 哨兵   │ │  │ │ Redis 单副本 │ │
    │ └──────────────┘ │  │ └──────────────┘ │  │ └──────────────┘ │
    └──────────────────┘  └──────────────────┘  └──────────────────┘
               │                    │                    │
               └───────┬────────────┴────────────────────┘
                       │ 专线/VPN/SD-WAN（联邦同步）
                       ▼
              ┌────────────────────┐
              │   联邦控制平面      │
              │   mTLS + HMAC      │
              │   设备视图同步      │
              │   跨机房任务转发    │
              └────────────────────┘
                       │
                       ▼
              ┌────────────────────┐
              │   异地备份归档      │
              │   A → B → C → 对象存储 │
              └────────────────────┘
```

### 4.3 组件部署

表：异地多机房组件部署清单

| 机房 | 控制面 | Agent | MySQL | Redis | 联邦 | 角色 |
|---|:---:|:---:|---|---|:---:|---|
| IDC-A（主机房） | 3 | ≤2000 | 主从 + binlog | 哨兵 3 节点 | ✅ | Active，承载主要负载 |
| IDC-B（备用房） | 3 | ≤2000 | 主从（A 的异步从） | 哨兵 3 节点 | ✅ | Warm Standby，可秒级切换 |
| IDC-C（DR 站） | 3 | 0 | 单副本（异地备份恢复） | 单副本 | ✅ | Cold Standby，RTO ≤ 4h |

### 4.4 网络拓扑

```text
    IDC-A (10.1.0.0/16)        IDC-B (10.2.0.0/16)        IDC-C (10.3.0.0/16)
    ┌──────────────┐           ┌──────────────┐           ┌──────────────┐
    │  控制面       │           │  控制面       │           │  控制面       │
    │ 10.1.10.10-12│◄═════════►│ 10.2.10.10-12│◄═════════►│ 10.3.10.10-12│
    │              │  专线/VPN  │              │  专线/VPN  │              │
    │  MySQL       │           │  MySQL       │           │  MySQL       │
    │ 10.1.20.10   │══════════►│ 10.2.20.10   │           │ 10.3.20.10   │
    │  (主)        │ binlog复制 │  (从)        │           │  (DR)        │
    └──────────────┘           └──────────────┘           └──────────────┘
            │                          │                          │
            └──────────────────────────┴──────────────────────────┘
                                       │
                                       ▼
                            ┌────────────────────┐
                            │  对象存储（异地）   │
                            │  S3/OBS 跨区域复制  │
                            └────────────────────┘
```

链路要求：
- 主-备专线带宽 ≥ 100Mbps，延迟 ≤ 30ms
- 主-DR 链路带宽 ≥ 50Mbps，延迟 ≤ 100ms
- 联邦 mTLS 心跳间隔 30s，超时 90s

### 4.5 数据同步

1. **业务数据**：MySQL 主从异步复制（binlog），主机房 → 备用房，RPO ≤ 1s
2. **联邦元数据**：控制面之间经 gRPC mTLS+HMAC 同步设备视图、任务状态，每 30s 一次
3. **备份归档**：每日 02:00 mysqldump → 对象存储，A → B → C 跨区域复制
4. **Redis 缓存**：各机房独立，不跨机房同步（缓存可重建）

### 4.6 故障切换

表：异地多机房故障切换流程

| 步骤 | 操作 | 责任人 | 耗时 |
|---|---|---|---|
| 1 | 监控告警确认主机房故障 | SRE 值班 | ≤2min |
| 2 | DNS 切换到备用房控制面 | SRE 值班 | ≤1min |
| 3 | 备用房 MySQL 提升为主 | MHA/Orchestrator | ≤30s |
| 4 | Agent 重新指向备用房控制面 | 自动（多控制面 failover） | ≤60s |
| 5 | 验证业务可用 | SRE + 业务方 | ≤10min |
| 6 | 故障机房修复后回切 | OpsMesh Owner | 计划性 |

### 4.7 脑裂防护

- **fencing**：切换前先隔离主机房控制面（kubectl scale deploy opsmesh-controlplane --replicas=0）
- **租约仲裁**：联邦切换需 ≥2/3 机房投票同意（Raft 协议）
- **数据版本号**：每条记录带机房 ID + 版本号，回切时按版本号合并冲突
- **DNS TTL**：设为 60s，避免客户端缓存旧地址

### 4.8 配置示例

```yaml
# IDC-A 控制面 Helm values（主机房）
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  requireAuth: true
  tls:
    enabled: true
    secretName: opsmesh-tls
  extraEnv:
    - name: OPSMESH_FEDERATION_PEERS
      value: "https://idc-b.opsmesh.example.com:9090,https://idc-c.opsmesh.example.com:9090"
    - name: OPSMESH_FEDERATION_SECRET
      valueFrom:
        secretKeyRef:
          name: federation-secret
          key: secret
    - name: OPSMESH_FEDERATION_ROLE
      value: "active"
    - name: OPSMESH_METRICS_ALLOW_CIDR
      value: "10.0.0.0/8"

mysql:
  enabled: true
  persistence:
    size: 200Gi
  # 主从复制配置
  replication:
    enabled: true
    role: master
    slaveOf: ""  # 主机房为主
```

```yaml
# IDC-B 控制面 Helm values（备用房）
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  extraEnv:
    - name: OPSMESH_FEDERATION_PEERS
      value: "https://idc-a.opsmesh.example.com:9090,https://idc-c.opsmesh.example.com:9090"
    - name: OPSMESH_FEDERATION_SECRET
      valueFrom:
        secretKeyRef:
          name: federation-secret
          key: secret
    - name: OPSMESH_FEDERATION_ROLE
      value: "warm-standby"

mysql:
  replication:
    enabled: true
    role: slave
    slaveOf: "idc-a.mysql.opsmesh.example.com:3306"
```

### 4.9 容量规划

表：异地多机房容量规划（≤2000 台/机房）

| 指标 | 公式 | 2000 台估算 |
|---|---|---|
| 控制面 CPU | 1 核 + 设备数 × 0.003 核 | 7 核 |
| 控制面内存 | 1Gi + 设备数 × 3Mi | 7Gi |
| MySQL 存储 | 50Gi + 设备数 × 80Mi | 210Gi |
| 联邦同步带宽 | 设备数 × 5KB / 30s | 333KB/s |
| binlog 复制带宽 | 写入 QPS × 1KB | 5Mbps |
| 跨机房延迟 | 专线 RTT | ≤30ms |

### 4.10 适用场景

- 跨城市多机房企业（金融、电商、制造业）
- ≤2000 台/机房，总规模 ≤6000 台
- 要求 RTO ≤ 30min，RPO ≤ 1s
- 有专线或 SD-WAN 跨机房链路
- 需要异地容灾但不需要双活

---

## 第5章 场景三：多数据中心

### 5.1 场景描述

多数据中心部署适用于大型企业或运营商，3 个或更多 DC 对等部署，全局控制中心统一调度。每个 DC 独立控制面 + Store，经联邦同步。规模上限 ≤10000 台。

### 5.2 架构图

图：多数据中心部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │            全局控制中心（GCC）                   │
                │   ┌──────────────────────────────────────────┐ │
                │   │   全局 DNS / GSLB / API 网关              │ │
                │   │   统一认证 / 跨 DC 任务编排 / 全局视图     │ │
                │   └──────────────────────────────────────────┘ │
                └──────┬──────────────┬──────────────┬────────────┘
                       │              │              │
                       ▼              ▼              ▼
        ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
        │      DC1         │ │      DC2         │ │      DC3         │
        │   (北京)         │ │   (上海)         │ │   (深圳)         │
        │                  │ │                  │ │                  │
        │ ┌──────────────┐ │ │ ┌──────────────┐ │ │ ┌──────────────┐ │
        │ │ 控制面 ×3     │ │ │ │ 控制面 ×3     │ │ │ │ 控制面 ×3     │ │
        │ │ + Agent 集群  │ │ │ │ + Agent 集群  │ │ │ │ + Agent 集群  │ │
        │ └──────┬───────┘ │ │ └──────┬───────┘ │ │ └──────┬───────┘ │
        │        │         │ │        │         │ │        │         │
        │ ┌──────┴───────┐ │ │ ┌──────┴───────┐ │ │ ┌──────┴───────┐ │
        │ │ MySQL 集群   │ │ │ │ MySQL 集群   │ │ │ │ MySQL 集群   │ │
        │ │ (3 节点)     │ │ │ │ (3 节点)     │ │ │ │ (3 节点)     │ │
        │ │ Redis 集群   │ │ │ │ Redis 集群   │ │ │ │ Redis 集群   │ │
        │ └──────────────┘ │ │ └──────────────┘ │ │ └──────────────┘ │
        └──────────────────┘ └──────────────────┘ └──────────────────┘
                │                  │                  │
                └──────────────────┴──────────────────┘
                                │
                                ▼
                ┌──────────────────────────────────┐
                │   联邦同步网络（mTLS + HMAC）      │
                │   设备视图 / 任务状态 / 告警聚合    │
                │   跨 DC 任务转发 + 全局审计        │
                └──────────────────────────────────┘
```

### 5.3 组件部署

表：多数据中心组件部署清单

| 组件 | DC1 | DC2 | DC3 | GCC | 备注 |
|---|:---:|:---:|:---:|:---:|---|
| 控制面 | 3 | 3 | 3 | 3 | GCC 控制面为全局协调者 |
| Agent | ≤3500 | ≤3500 | ≤3500 | 0 | 各 DC 独立 Agent 集群 |
| MySQL | 3 节点 | 3 节点 | 3 节点 | 3 节点 | 各 DC 独立集群，binlog 不跨 DC |
| Redis | 3 节点 | 3 节点 | 3 节点 | 3 节点 | 各 DC 独立集群 |
| 联邦代理 | 1 | 1 | 1 | 1 | gRPC mTLS+HMAC 互联 |

### 5.4 网络拓扑

```text
                    Internet
                       │
                       ▼
            ┌────────────────────┐
            │   GCC 入口         │
            │   Anycast IP       │
            └────────┬───────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
        ▼            ▼            ▼
     ┌─────┐      ┌─────┐      ┌─────┐
     │ DC1 │◄═══►│ DC2 │◄═══►│ DC3 │
     │ BJ  │      │ SH  │      │ SZ  │
     └─────┘      └─────┘      └─────┘
        │            │            │
        ▼            ▼            ▼
     Agent         Agent         Agent
     集群          集群          集群
```

- DC 之间专线带宽 ≥ 1Gbps，延迟 ≤ 50ms
- GCC 与各 DC 专线带宽 ≥ 500Mbps
- Anycast DNS 解析到最近 DC

### 5.5 管理模式

- **统一认证**：GCC 部署全局用户中心 + RBAC，各 DC 同步用户/角色
- **分权分域**：DC 管理员仅可管理本 DC 资源，全局管理员可跨 DC 操作
- **任务编排**：跨 DC 任务由 GCC 协调，本 DC 任务由 DC 控制面直接调度
- **告警聚合**：各 DC 告警上报 GCC，GCC 聚合后通知（避免告警风暴）

### 5.6 数据分区

| 数据类型 | 分区策略 | 跨 DC 同步 |
|---|---|---|
| 设备元数据 | 按 DC 分区，本 DC 写入 | 联邦同步只读视图 |
| 任务记录 | 按 DC 分区，本 DC 写入 | 跨 DC 任务转发经联邦 |
| 告警事件 | 按 DC 分区，本 DC 写入 | 上报 GCC 聚合 |
| 审计日志 | 按 DC 分区，本 DC 写入 | 异步同步到 GCC 全局审计 |
| 用户/角色 | GCC 主写，各 DC 只读 | 全量同步 |
| CMDB 模型 | GCC 主写，各 DC 只读 | 全量同步 |

### 5.7 跨 DC 操作

```bash
# 跨 DC 任务下发（经 GCC 协调）
opsmesh task create \
  --name "patch-all-dcs" \
  --target "tag:linux" \
  --scope "all-dcs" \
  --command "yum update -y security" \
  --approval required

# 跨 DC 设备查询
opsmesh device list --scope dc1,dc2 --filter "os=centos7"

# 跨 DC 告警聚合
opsmesh alert list --scope global --severity critical
```

### 5.8 配置示例

```yaml
# DC1 控制面 Helm values
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  extraEnv:
    - name: OPSMESH_FEDERATION_PEERS
      value: "https://dc2.opsmesh.example.com:9090,https://dc3.opsmesh.example.com:9090,https://gcc.opsmesh.example.com:9090"
    - name: OPSMESH_FEDERATION_SECRET
      valueFrom:
        secretKeyRef:
          name: federation-secret
          key: secret
    - name: OPSMESH_FEDERATION_ROLE
      value: "dc"
    - name: OPSMESH_FEDERATION_DC_ID
      value: "dc1-bj"
    - name: OPSMESH_METRICS_ALLOW_CIDR
      value: "10.0.0.0/8,172.16.0.0/12"

mysql:
  enabled: true
  persistence:
    size: 500Gi
  replication:
    enabled: true
    role: master
    # 各 DC 独立 MySQL 集群，不跨 DC 复制
```

```yaml
# GCC 控制面 Helm values（全局协调者）
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "global-coordinator"
    - name: OPSMESH_FEDERATION_DC_ID
      value: "gcc"
    - name: OPSMESH_GLOBAL_AUDIT
      value: "true"
    - name: OPSMESH_ALERT_AGGREGATION
      value: "true"
```

### 5.9 容量规划

表：多数据中心容量规划（≤10000 台）

| 指标 | 公式 | 10000 台估算（3 DC） |
|---|---|---|
| 单 DC 控制面 CPU | 2 核 + (设备数/3) × 0.003 核 | 13 核 |
| 单 DC 控制面内存 | 2Gi + (设备数/3) × 3Mi | 12Gi |
| 单 DC MySQL 存储 | 100Gi + (设备数/3) × 100Mi | 433Gi |
| 联邦同步带宽 | 设备数 × 5KB / 30s | 1.6MB/s |
| GCC 聚合带宽 | 告警 QPS × 2KB | 2Mbps |
| 跨 DC 任务延迟 | 联邦一跳 + 网络延迟 | ≤100ms |

### 5.10 适用场景

- 大型企业多 DC（银行、互联网、能源）
- ≤10000 台总规模，3-5 个 DC
- 要求统一管理 + 分权分域
- 跨 DC 操作需求（统一补丁、配置下发）
- 有专线或 SD-WAN 跨 DC 链路

---

## 第6章 场景四：电信资源池

### 6.1 场景描述

电信资源池部署是 OpsMesh 最大规模形态，适用于电信运营商省级资源池管理。分层控制（省→市→机房→设备），支持万级到十万级设备纳管。需满足等保 2.0 三级合规与国产化要求。

### 6.2 架构图

图：电信资源池部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │           省级控制中心（PCC）                    │
                │   ┌──────────────────────────────────────────┐ │
                │   │   全省视图 / 全省告警聚合 / 全省审计       │ │
                │   │   全省 KPI / 资源池管理 / 批量作业编排    │ │
                │   │   控制面 ×5 + MySQL 集群 + Redis 集群     │ │
                │   └──────────────────────────────────────────┘ │
                └──────┬───────────────────────────────────────────┘
                       │ DCN 专网（电信数据通信网）
                       │
        ┌──────────────┼──────────────┬─────────────┐
        │              │              │             │
        ▼              ▼              ▼             ▼
    ┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐
    │ 市级A  │    │ 市级B  │    │ 市级C  │... │ 市级N  │
    │ (MCC)  │    │ (MCC)  │    │ (MCC)  │    │ (MCC)  │
    │ CP ×3  │    │ CP ×3  │    │ CP ×3  │    │ CP ×3  │
    └───┬────┘    └───┬────┘    └───┬────┘    └───┬────┘
        │             │             │             │
        ▼             ▼             ▼             ▼
    ┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐
    │ 机房1  │    │ 机房2  │    │ 机房3  │    │ 机房N  │
    │ (CRC)  │    │ (CRC)  │    │ (CRC)  │    │ (CRC)  │
    │ CP ×1  │    │ CP ×1  │    │ CP ×1  │    │ CP ×1  │
    └───┬────┘    └───┬────┘    └───┬────┘    └───┬────┘
        │             │             │             │
        ▼             ▼             ▼             ▼
    ┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐
    │ 设备群 │    │ 设备群 │    │ 设备群 │    │ 设备群 │
    │ ≤5000  │    │ ≤5000  │    │ ≤5000  │    │ ≤5000  │
    └────────┘    └────────┘    └────────┘    └────────┘
```

### 6.3 分层控制

表：电信资源池分层控制对照表

| 层级 | 名称 | 控制面副本 | 管理范围 | 职责 |
|---|---|:---:|---|---|
| L1 | 省级控制中心 PCC | 5 | 全省 | 全省视图、告警聚合、批量作业、KPI、资源池管理 |
| L2 | 市级控制中心 MCC | 3 | 单市 | 市内设备纳管、任务调度、告警初筛 |
| L3 | 机房控制中心 CRC | 1 | 单机房 | 机房内设备纳管、本地任务执行 |
| L4 | 设备 Agent | - | 单设备 | 任务执行、状态上报 |

### 6.4 网络拓扑

```text
    公网（不可达）
         │
         ▼ (无)
    ┌─────────┐
    │  DCN   │  电信专网（与公网物理隔离）
    │  专网  │  带宽 ≥ 10Gbps，延迟 ≤ 20ms
    └────┬───┘
         │
    ┌────┼────┐
    │    │    │
    ▼    ▼    ▼
   PCC  MCC  MCC
    │    │    │
    ▼    ▼    ▼
   CRC  CRC  CRC
    │    │    │
    ▼    ▼    ▼
  Agent Agent Agent
```

- DCN 专网与公网物理隔离，仅电信内部可达
- PCC ↔ MCC 带宽 ≥ 10Gbps，延迟 ≤ 20ms
- MCC ↔ CRC 带宽 ≥ 1Gbps，延迟 ≤ 10ms
- CRC ↔ Agent 带宽 ≥ 100Mbps，延迟 ≤ 5ms

### 6.5 多级权限与分权分域

表：电信资源池分权分域对照表

| 角色 | 权限范围 | 可操作 |
|---|---|---|
| 省级管理员 | 全省 | 全省视图、批量作业、资源池管理、用户管理 |
| 市级管理员 | 单市 | 市内设备纳管、任务下发、告警确认 |
| 机房管理员 | 单机房 | 机房内设备操作、本地任务 |
| 只读审计员 | 全省只读 | 全省视图、审计日志查询 |
| 操作员 | 指定设备组 | 指定设备的任务下发 |

### 6.6 资源池管理

- **资源池划分**：按业务系统（BSS/OSS/计费/CRM）划分资源池
- **资源标签**：每设备打标签（省/市/机房/业务/重要等级）
- **容量看板**：PCC 提供全省容量看板（CPU/内存/存储利用率）
- **资源调度**：跨机房资源调度（虚拟机迁移、容器扩缩容）

### 6.7 批量操作（万级设备）

```bash
# 全省批量补丁（分批执行，避免同时打满网络）
opsmesh batch create \
  --name "province-patch-2026Q3" \
  --target "tag:province=GD" \
  --filter "os=centos7,state=online" \
  --command "yum update -y security" \
  --batch-size 500 \
  --batch-interval 60s \
  --approval required \
  --rollback-on-failure 10%

# 全省配置下发
opsmesh batch create \
  --name "config-sync" \
  --target "tag:province=GD" \
  --template "nginx-config" \
  --variables "env=prod" \
  --batch-size 1000 \
  --dry-run false
```

### 6.8 告警聚合

- **三级聚合**：CRC 聚合本机房 → MCC 聚合本市 → PCC 聚合全省
- **告警抑制**：根因告警抑制衍生告警（如交换机故障抑制下联设备告警）
- **告警分级**：P1（全省业务影响）/ P2（市级业务影响）/ P3（机房级）/ P4（设备级）
- **告警通知**：P1 短信+电话+邮件，P2 短信+邮件，P3 邮件，P4 仅记录

### 6.9 配置示例

```yaml
# 省级控制中心 PCC Helm values
controlplane:
  replicaCount: 5
  store: mysql
  production: true
  resources:
    requests: { cpu: 2000m, memory: 4Gi }
    limits: { cpu: 8000m, memory: 16Gi }
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "province-control-center"
    - name: OPSMESH_FEDERATION_LEVEL
      value: "L1"
    - name: OPSMESH_ALERT_AGGREGATION_LEVEL
      value: "province"
    - name: OPSMESH_BATCH_MAX_SIZE
      value: "10000"
    - name: OPSMESH_BATCH_DEFAULT_SIZE
      value: "500"
    - name: OPSMESH_RBAC_DOMAIN_ENABLED
      value: "true"
    - name: OPSMESH_AUDIT_RETENTION_DAYS
      value: "365"

mysql:
  persistence:
    size: 2Ti
  resources:
    requests: { cpu: 4000m, memory: 16Gi }
    limits: { cpu: 16000m, memory: 64Gi }
```

```yaml
# 市级控制中心 MCC Helm values
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "city-control-center"
    - name: OPSMESH_FEDERATION_LEVEL
      value: "L2"
    - name: OPSMESH_FEDERATION_PARENT
      value: "https://pcc.opsmesh.telecom.cn:9090"
    - name: OPSMESH_BATCH_MAX_SIZE
      value: "5000"
```

### 6.10 容量规划

表：电信资源池容量规划（≥50000 台）

| 指标 | 公式 | 50000 台估算 |
|---|---|---|
| PCC 控制面 CPU | 4 核 + 设备数 × 0.0005 核 | 29 核 |
| PCC 控制面内存 | 4Gi + 设备数 × 1Mi | 54Gi |
| PCC MySQL 存储 | 100Gi + 设备数 × 50Mi | 2.6Ti |
| 联邦同步带宽 | 设备数 × 5KB / 30s | 8.3MB/s |
| 批量操作并发 | 500 设备/批 × 60s 间隔 | 500 并发 |
| 告警聚合 QPS | 设备数 × 0.001 告警/秒 | 50 告警/秒 |

### 6.11 等保合规与国产化

- **等保 2.0 三级**：身份鉴别、访问控制、安全审计、入侵防范、恶意代码防范、数据完整性、数据保密性
- **国产化 CPU**：鲲鹏 920 / 飞腾 FT2000+ / 海光 / 兆芯 / 龙芯
- **国产化 OS**：中标麒麟 / 统信 UOS / openEuler
- **国产化 DB**：达梦 DM8 / 人大金仓 KingbaseES / OceanBase / GaussDB
- **国产化中间件**：东方通 TongWeb / 中创 InforSuite / 宝兰德 BES
- **密码合规**：商密 SM2/SM3/SM4 算法，密码产品认证

### 6.12 适用场景

- 电信运营商省级资源池
- ≥50000 台设备纳管
- 分层管理（省→市→机房→设备）
- 等保 2.0 三级合规
- 全栈国产化要求
- DCN 专网环境

---

## 第7章 场景五：混合云

### 7.1 场景描述

混合云部署适用于私有云 + 公有云 + 边缘的混合架构。私有云承载核心业务，公有云承载弹性业务，边缘承载就近业务。OpsMesh 统一纳管三种环境。

### 7.2 架构图

图：混合云部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │            OpsMesh 全局控制面                    │
                │   ┌──────────────────────────────────────────┐ │
                │   │   统一纳管 / 统一认证 / 统一审计          │ │
                │   │   跨云任务编排 / 跨云告警聚合             │ │
                │   └──────────────────────────────────────────┘ │
                └──────┬──────────────┬──────────────┬────────────┘
                       │              │              │
                       ▼              ▼              ▼
        ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
        │    私有云         │ │    公有云         │ │    边缘          │
        │  (On-Premise)    │ │  (Public Cloud)  │ │  (Edge)          │
        │                  │ │                  │ │                  │
        │ ┌──────────────┐ │ │ ┌──────────────┐ │ │ ┌──────────────┐ │
        │ │ 控制面 ×3     │ │ │ │ 控制面 ×3     │ │ │ │ 控制面 ×1     │ │
        │ │ + Agent       │ │ │ │ + Agent       │ │ │ │ + Agent       │ │
        │ └──────┬───────┘ │ │ └──────┬───────┘ │ │ └──────┬───────┘ │
        │        │         │ │        │         │ │        │         │
        │ ┌──────┴───────┐ │ │ ┌──────┴───────┐ │ │ ┌──────┴───────┐ │
        │ │ OpenStack/   │ │ │ │ AWS/阿里云/   │ │ │ │ K3s/设备     │ │
        │ │ KubeSphere   │ │ │ │ 华为云CCE     │ │ │ │              │ │
        │ │ VMware       │ │ │ │ Azure AKS    │ │ │ │              │ │
        │ └──────────────┘ │ │ └──────────────┘ │ │ └──────────────┘ │
        └──────────────────┘ └──────────────────┘ └──────────────────┘
                │                  │                  │
                └───────专线──────┴──────VPN/公网────┘
                                │
                                ▼
                    ┌────────────────────┐
                    │   联邦同步网络      │
                    │   mTLS + HMAC      │
                    └────────────────────┘
```

### 7.3 组件部署

表：混合云组件部署清单

| 环境 | 控制面 | Agent | Store | 网络接入 |
|---|:---:|:---:|---|---|
| 私有云 | 3 | OpenStack VM / KubeSphere Pod | MySQL+Redis | 内网 |
| 公有云 | 3 | 云主机 / K8s Pod | 云 RDS + 云 Redis | VPC + 专线 |
| 边缘 | 1 | 边缘设备 | SQLite + 内存 | VPN / 公网 |
| 全局 | 3 | 0 | MySQL+Redis | 联邦中心 |

### 7.4 网络拓扑

```text
    私有云 (10.0.0.0/8)          公有云 (VPC)              边缘
    ┌──────────────┐           ┌──────────────┐           ┌──────────┐
    │  控制面       │◄══专线══►│  控制面       │◄══VPN═══►│  控制面   │
    │ 10.0.10.10   │  100Mbps  │ 10.1.10.10   │  50Mbps  │ 10.2.10  │
    │              │           │              │           │          │
    │  Agent       │           │  Agent       │           │  Agent   │
    │ 10.0.20.0/24 │           │ 10.1.20.0/24 │           │ 10.2.20  │
    └──────────────┘           └──────────────┘           └──────────┘
            │                          │                       │
            └──────────────────────────┴───────────────────────┘
                                       │
                                       ▼
                            ┌────────────────────┐
                            │   全局控制面         │
                            │   10.0.100.10       │
                            └────────────────────┘
```

### 7.5 资源管理

- **统一纳管**：私有云 VM、公有云云主机、边缘设备统一在 OpsMesh 设备列表
- **资源标签**：`env=private/public/edge`，`provider=openstack/aws/aliyun/huawei`
- **跨云操作**：经全局控制面下发跨云任务，各环境控制面执行
- **资源同步**：定期同步各云资源清单到 CMDB

### 7.6 安全策略

- **专线加密**：私有云 ↔ 公有云专线启用 IPsec 加密
- **VPN 加密**：公有云 ↔ 边缘 VPN 使用 IPSec/L2TP
- **mTLS 联邦**：所有控制面之间 mTLS 双向认证
- **API 网关**：公有云控制面经云 API 网关暴露，WAF 防护
- **数据脱敏**：跨云同步的敏感数据经 SM4/AES-256 加密

### 7.7 网络策略

```yaml
# 网络策略示例（K8s NetworkPolicy）
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: opsmesh-controlplane
  namespace: opsmesh
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: controlplane
  policyTypes: [Ingress, Egress]
  ingress:
    - from:
        - podSelector: {}  # 同 namespace
        - ipBlock:
            cidr: 10.0.0.0/8  # 私有云内网
        - ipBlock:
            cidr: 10.1.0.0/16  # 公有云 VPC
      ports:
        - { protocol: TCP, port: 8080 }
        - { protocol: TCP, port: 9090 }
        - { protocol: TCP, port: 9091 }
  egress:
    - to:
        - ipBlock: { cidr: 0.0.0.0/0 }
      ports:
        - { protocol: TCP, port: 3306 }
        - { protocol: TCP, port: 6379 }
        - { protocol: TCP, port: 443 }
```

### 7.8 配置示例

```yaml
# 私有云控制面 values
controlplane:
  replicaCount: 3
  store: mysql
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "private-cloud"
    - name: OPSMESH_FEDERATION_PEERS
      value: "https://public.opsmesh.example.com:9090,https://edge.opsmesh.example.com:9090,https://global.opsmesh.example.com:9090"
    - name: OPSMESH_CLOUD_PROVIDER
      value: "on-premise"
    - name: OPSMESH_CMDB_SYNC_INTERVAL
      value: "300s"

# 公有云控制面 values（以华为云 CCE 为例）
controlplane:
  replicaCount: 3
  store: mysql
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "public-cloud"
    - name: OPSMESH_CLOUD_PROVIDER
      value: "huawei-cloud"
    - name: OPSMESH_CLOUD_CCE_CLUSTER_ID
      value: "cce-xxxxx"
    - name: OPSMESH_CLOUD_RDS_ENDPOINT
      valueFrom:
        secretKeyRef:
          name: cloud-rds-secret
          key: endpoint
```

### 7.9 适用场景

- 企业混合云架构（私有云 + 公有云）
- 跨云统一运维管理
- 边缘 + 中心混合架构
- 业务跨云迁移过渡期
- 灾备云 + 主云混合

---

## 第8章 场景六：公有云

### 8.1 场景描述

公有云部署将 OpsMesh 完全部署在公有云 K8s 服务上，利用云原生能力（弹性伸缩、托管数据库、对象存储、云监控）降低运维成本。

### 8.2 架构图

图：公有云部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │              公有云（AWS/Azure/GCP/              │
                │              华为云/阿里云/腾讯云）              │
                │                                                │
                │   ┌──────────────────────────────────────────┐ │
                │   │   云负载均衡（ALB/SLB/CLB）               │ │
                │   │   WAF + DDoS 防护 + TLS 终止              │ │
                │   └────────────────────┬─────────────────────┘ │
                │                        │                        │
                │   ┌────────────────────┴─────────────────────┐ │
                │   │   K8s 集群（EKS/AKS/GKE/CCE/ACK/TKE）     │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   OpsMesh 控制面（HPA 2-10）       │   │ │
                │   │   │   ┌─────┐ ┌─────┐ ┌─────┐         │   │ │
                │   │   │   │ CP1 │ │ CP2 │ │ CPN │         │   │ │
                │   │   │   └─────┘ └─────┘ └─────┘         │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   Agent DaemonSet（每节点一个）    │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                │                        │                        │
                │   ┌────────────────────┴─────────────────────┐ │
                │   │   云托管服务                              │ │
                │   │   ┌─────────┐ ┌─────────┐ ┌─────────┐   │ │
                │   │   │ 云 RDS  │ │云 Redis │ │ 对象存储│   │ │
                │   │   │ (MySQL) │ │         │ │ (S3/OBS)│   │ │
                │   │   └─────────┘ └─────────┘ └─────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                │                        │                        │
                │   ┌────────────────────┴─────────────────────┐ │
                │   │   云监控（CloudWatch/Azure Monitor/        │ │
                │   │   云监控/华为云 CES）                      │ │
                │   └──────────────────────────────────────────┘ │
                └────────────────────────────────────────────────┘
```

### 8.3 K8s 部署与云服务集成

表：公有云 K8s 服务对照表

| 云平台 | K8s 服务 | 托管 MySQL | 托管 Redis | 对象存储 | 监控 |
|---|---|---|---|---|---|
| AWS | EKS | RDS for MySQL | ElastiCache | S3 | CloudWatch |
| Azure | AKS | Azure Database for MySQL | Azure Cache | Blob Storage | Azure Monitor |
| GCP | GKE | Cloud SQL | Memorystore | Cloud Storage | Cloud Monitoring |
| 华为云 | CCE | RDS for MySQL | DCS | OBS | CES |
| 阿里云 | ACK | RDS MySQL | Tair/Redis | OSS | 云监控 |
| 腾讯云 | TKE | TDSQL-C | TencentDB for Redis | COS | 云监控 |

### 8.4 云服务集成

- **托管数据库**：使用云 RDS 替代自建 MySQL，自动备份、主从切换、只读实例
- **托管缓存**：使用云 Redis 替代自建 Redis，自动故障切换
- **对象存储**：备份归档到 S3/OBS，生命周期规则自动清理
- **云监控**：控制面 /metrics 经云监控采集，告警经云通知服务
- **IAM 集成**：与云 IAM 集成（如华为云 IAM），实现云账号 SSO

### 8.5 自动伸缩

```yaml
# HPA 配置
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: opsmesh-controlplane
  namespace: opsmesh
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: opsmesh-controlplane
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Percent
          value: 100
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Percent
          value: 50
          periodSeconds: 60
```

### 8.6 多租户

- **租户隔离**：每租户独立 namespace 或独立集群
- **资源配额**：每租户 CPU/内存/存储配额（QuotaStore）
- **数据隔离**：每租户独立数据库 schema 或独立数据库实例
- **网络隔离**：每租户独立 VPC 或 NetworkPolicy

### 8.7 配置示例（Helm Chart）

```yaml
# values.yaml（公有云生产）
global:
  imageRegistry: "registry.cn-north-4.huaweicloud.com/opsmesh/"
  imagePullSecrets:
    - name: huawei-cloud-registry-secret

controlplane:
  replicaCount: 3
  image:
    repository: opsmesh/opsmesh
    tag: "v1.0.0"
  store: mysql
  production: true
  requireAuth: true
  tls:
    enabled: true
    secretName: opsmesh-tls
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 20
    targetCPUUtilizationPercentage: 70
  resources:
    requests: { cpu: 500m, memory: 512Mi }
    limits: { cpu: 4000m, memory: 4Gi }
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchLabels:
              app.kubernetes.io/component: controlplane
          topologyKey: kubernetes.io/hostname
  extraEnv:
    - name: OPSMESH_CLOUD_PROVIDER
      value: "huawei-cloud"
    - name: OPSMESH_MYSQL_DSN
      valueFrom:
        secretKeyRef:
          name: cloud-rds-secret
          key: dsn
    - name: OPSMESH_REDIS_ADDR
      valueFrom:
        secretKeyRef:
          name: cloud-redis-secret
          key: addr

# 使用云托管 MySQL，禁用自建
mysql:
  enabled: false  # 使用云 RDS

redis:
  enabled: false  # 使用云 DCS

ingress:
  enabled: true
  className: nginx
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
  hosts:
    - host: opsmesh.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: opsmesh-tls
      hosts: [opsmesh.example.com]

observability:
  serviceMonitor:
    enabled: true
  prometheusRule:
    enabled: true
```

### 8.8 适用场景

- 公有云原生部署
- SaaS 多租户运维服务
- 弹性伸缩需求强烈
- 不愿自建数据库/缓存
- 云监控/云告警集成

---

## 第9章 场景七：私有云

### 9.1 场景描述

私有云部署将 OpsMesh 部署在企业私有云平台上，与 OpenStack/KubeSphere/VMware/Proxmox 集成，纳管私有云资源。

### 9.2 架构图

图：私有云部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │              企业私有云平台                      │
                │                                                │
                │   ┌──────────────────────────────────────────┐ │
                │   │   私有云管理平台                          │ │
                │   │   ┌────────────┐  ┌────────────┐         │ │
                │   │   │ OpenStack  │  │ KubeSphere │         │ │
                │   │   │ Horizon    │  │ Console    │         │ │
                │   │   └────────────┘  └────────────┘         │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   OpsMesh 控制面（K8s Deployment）         │ │
                │   │   ┌─────┐ ┌─────┐ ┌─────┐                │ │
                │   │   │ CP1 │ │ CP2 │ │ CP3 │                │ │
                │   │   └─────┘ └─────┘ └─────┘                │ │
                │   │   + Agent DaemonSet                      │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   私有云资源                              │ │
                │   │   ┌─────────┐ ┌─────────┐ ┌─────────┐   │ │
                │   │   │  VM     │ │  K8s    │ │ 裸金属  │   │ │
                │   │   │ (VMware)│ │ (K8s)   │ │         │   │ │
                │   │   └─────────┘ └─────────┘ └─────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   存储                                  │ │
                │   │   ┌─────────┐ ┌─────────┐ ┌─────────┐   │ │
                │   │   │ Ceph    │ │ NFS      │ │ iSCSI   │   │ │
                │   │   └─────────┘ └─────────┘ └─────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                └────────────────────────────────────────────────┘
```

### 9.3 组件部署

表：私有云组件部署清单

| 平台 | OpsMesh 部署方式 | Agent 部署方式 | 集成方式 |
|---|---|---|---|
| OpenStack | K8s Deployment（Magnum 集群） | VM 内 systemd | OpenStack API 纳管 VM |
| KubeSphere | K8s Deployment（KubeSphere 集群） | K8s DaemonSet | KubeSphere API 纳管 Pod |
| VMware | VM 部署 | VM 内 systemd | vSphere API 纳管 VM |
| Proxmox | VM/CT 部署 | VM/CT 内 systemd | Proxmox API 纳管 |

### 9.4 私有云平台集成

#### 9.4.1 OpenStack 集成

```yaml
# OpenStack 集成配置
controlplane:
  extraEnv:
    - name: OPSMESH_OPENSTACK_ENABLED
      value: "true"
    - name: OPSMESH_OPENSTACK_AUTH_URL
      value: "https://keystone.example.com:5000/v3"
    - name: OPSMESH_OPENSTACK_USERNAME
      valueFrom:
        secretKeyRef:
          name: openstack-secret
          key: username
    - name: OPSMESH_OPENSTACK_PASSWORD
      valueFrom:
        secretKeyRef:
          name: openstack-secret
          key: password
    - name: OPSMESH_OPENSTACK_PROJECT
      value: "opsmesh"
    - name: OPSMESH_OPENSTACK_DOMAIN
      value: "default"
```

#### 9.4.2 KubeSphere 集成

```yaml
# KubeSphere 集成配置
controlplane:
  extraEnv:
    - name: OPSMESH_KUBESPHERE_ENABLED
      value: "true"
    - name: OPSMESH_KUBESPHERE_API_SERVER
      value: "https://ks.example.com:6443"
    - name: OPSMESH_KUBESPHERE_TOKEN
      valueFrom:
        secretKeyRef:
          name: kubesphere-secret
          key: token
```

### 9.5 资源管理

- **VM 纳管**：经 OpenStack/vSphere API 创建/销毁/迁移 VM
- **K8s 纳管**：经 K8s API 管理 Pod/Deployment/Service
- **裸金属**：经 IPMI/Redfish 管理裸金属服务器
- **资源同步**：定期同步私有云资源到 CMDB

### 9.6 网络管理

- **VPC/子网**：管理私有云 VPC 与子网
- **安全组**：管理安全组规则
- **负载均衡**：管理私有云 LB
- **DNS**：管理私有云 DNS 解析

### 9.7 存储管理

- **Ceph**：管理 Ceph 集群、Pool、RBD、CephFS
- **NFS**：管理 NFS 共享
- **iSCSI**：管理 iSCSI Target/LUN
- **快照**：定期快照 + 备份到对象存储

### 9.8 配置示例

```yaml
# 私有云 Helm values
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  extraEnv:
    - name: OPSMESH_CLOUD_PROVIDER
      value: "private-cloud"
    - name: OPSMESH_OPENSTACK_ENABLED
      value: "true"
    - name: OPSMESH_KUBESPHERE_ENABLED
      value: "true"
    - name: OPSMESH_CMDB_SYNC_INTERVAL
      value: "600s"

mysql:
  enabled: true
  persistence:
    size: 200Gi
    storageClass: "ceph-rbd"  # 使用 Ceph RBD

redis:
  enabled: true
  persistence:
    size: 20Gi
    storageClass: "ceph-rbd"

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: opsmesh.internal.example.com
      paths:
        - path: /
          pathType: Prefix
```

### 9.9 适用场景

- 企业私有云运维
- OpenStack/KubeSphere/VMware 环境
- 与私有云平台深度集成
- 数据不出企业内网
- 自建存储（Ceph/NFS）

---

## 第10章 场景八：边缘计算

### 10.1 场景描述

边缘计算部署适用于 IoT、CDN、门店等边缘场景。中心控制面统一管理，边缘节点部署轻量 Agent 或边缘集群，支持离线运维与资源受限环境。

### 10.2 架构图

图：边缘计算部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │            中心控制面（云端/DC）                 │
                │   ┌──────────────────────────────────────────┐ │
                │   │   控制面 ×3 + MySQL + Redis              │ │
                │   │   全局视图 / 边缘节点管理 / 配置同步       │ │
                │   └──────────────────────────────────────────┘ │
                └────────────────────────────────────────────────┘
                                │
                                │ 互联网 / 专线 / 5G
                                │
        ┌───────────────────────┼───────────────────────┐
        │                       │                       │
        ▼                       ▼                       ▼
    ┌──────────┐           ┌──────────┐           ┌──────────┐
    │ 边缘集群1│           │ 边缘集群2│           │ 边缘集群N│
    │ (门店)   │           │ (工厂)   │           │ (基站)   │
    │          │           │          │           │          │
    │ ┌──────┐ │           │ ┌──────┐ │           │ ┌──────┐ │
    │ │ K3s  │ │           │ │MicroK8│ │           │ │ k0s  │ │
    │ │      │ │              │ │      │ │           │ │      │ │
    │ │ ┌──┐ │ │           │ │ ┌──┐ │ │           │ │ ┌──┐ │ │
    │ │ │CP│ │ │           │ │ │CP│ │ │           │ │ │CP│ │ │
    │ │ └──┘ │ │           │ │ └──┘ │ │           │ │ └──┘ │ │
    │ └──────┘ │           │ └──────┘ │           │ └──────┘ │
    │          │           │          │           │          │
    │ ┌──────┐ │           │ ┌──────┐ │           │ ┌──────┐ │
    │ │Agent │ │           │ │Agent │ │           │ │Agent │ │
    │ │集群  │ │           │ │集群  │ │           │ │集群  │ │
    │ └──────┘ │           │ └──────┘ │           │ └──────┘ │
    └──────────┘           └──────────┘           └──────────┘
```

### 10.3 轻量 Agent 与边缘集群

表：边缘节点形态对照表

| 形态 | 资源规格 | 控制面 | Agent | 适用 |
|---|---|:---:|:---:|---|
| 轻量 Agent | ≤0.5C/512Mi | ❌ | ✅ | 单设备纳管 |
| K3s 边缘集群 | 1C/1Gi | ✅ 1 副本 | ✅ | 边缘小集群 |
| MicroK8s 边缘集群 | 1C/1Gi | ✅ 1 副本 | ✅ | 边缘小集群 |
| k0s 边缘集群 | 1C/1Gi | ✅ 1 副本 | ✅ | 边缘小集群 |
| 多节点边缘集群 | 2C/2Gi | ✅ 3 副本 | ✅ | 边缘重要节点 |

### 10.4 边缘节点

- **K3s**：轻量 K8s，单二进制，适合资源受限边缘
- **MicroK8s**：Ubuntu 系轻量 K8s，snap 安装
- **k0s**：零依赖 K8s，单二进制
- **KubeEdge**：专门为边缘设计的 K8s 发行版

### 10.5 离线运维

- **本地缓存**：边缘控制面缓存任务与配置，断网时本地执行
- **断网续传**：网络恢复后批量上报执行结果
- **本地审批**：边缘管理员可本地审批紧急操作
- **配置预下发**：常用配置预下发到边缘，断网可用

### 10.6 资源受限优化

```yaml
# 边缘控制面 values（资源受限）
controlplane:
  replicaCount: 1  # 单副本节省资源
  store: memory  # 内存存储，无需 MySQL
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits: { cpu: 500m, memory: 512Mi }
  extraEnv:
    - name: OPSMESH_EDGE_MODE
      value: "true"
    - name: OPSMESH_OFFLINE_CACHE
      value: "true"
    - name: OPSMESH_LOCAL_APPROVAL
      value: "true"
    - name: OPSMESH_REPORT_BATCH_SIZE
      value: "100"

agent:
  resources:
    requests: { cpu: 10m, memory: 32Mi }
    limits: { cpu: 100m, memory: 128Mi }
  workerConcurrency: 2  # 降低并发
```

### 10.7 配置同步

- **中心 → 边缘**：配置经联邦同步到边缘控制面
- **边缘 → 中心**：边缘状态/告警上报中心
- **冲突解决**：以中心为权威，边缘本地修改仅临时生效
- **同步频率**：在线时 30s 一次，离线时缓存

### 10.8 适用场景

- IoT 边缘设备运维
- CDN 边缘节点管理
- 门店/分支机构的本地运维
- 5G MEC 边缘计算
- 网络不稳定或离线环境

---

## 第11章 场景九：国产化环境

### 11.1 场景描述

国产化环境部署满足党政军、金融、能源等行业的全栈国产化要求，CPU/OS/DB/中间件全部使用国产化组件，并通过等保 2.0 合规。

### 11.2 架构图

图：国产化环境部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │            全栈国产化环境                       │
                │                                                │
                │   ┌──────────────────────────────────────────┐ │
                │   │   应用层                                  │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   OpsMesh 控制面（Go 二进制）     │   │ │
                │   │   │   + Agent（Go 二进制）            │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   中间件层                                │ │
                │   │   ┌─────────┐ ┌─────────┐ ┌─────────┐   │ │
                │   │   │东方通   │ │中创     │ │宝兰德   │   │ │
                │   │   │TongWeb  │ │InforSuite│ │BES      │   │ │
                │   │   └─────────┘ └─────────┘ └─────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   数据库层                                │ │
                │   │   ┌─────────┐ ┌─────────┐ ┌─────────┐   │ │
                │   │   │达梦 DM8 │ │人大金仓 │ │OceanBase│   │ │
                │   │   │         │ │KingbaseES│ │         │   │ │
                │   │   └─────────┘ └─────────┘ └─────────┘   │ │
                │   │   ┌─────────┐                            │ │
                │   │   │GaussDB  │                            │ │
                │   │   └─────────┘                            │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   操作系统层                              │ │
                │   │   ┌─────────┐ ┌─────────┐ ┌─────────┐   │ │
                │   │   │中标麒麟 │ │统信 UOS │ │openEuler│   │ │
                │   │   └─────────┘ └─────────┘ └─────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                │                      │                          │
                │   ┌──────────────────┴───────────────────────┐ │
                │   │   CPU 层                                  │ │
                │   │   ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐│ │
                │   │   │鲲鹏 │ │飞腾 │ │海光 │ │兆芯 │ │龙芯 ││ │
                │   │   │920  │ │FT2K │ │     │ │     │ │     ││ │
                │   │   └─────┘ └─────┘ └─────┘ └─────┘ └─────┘│ │
                │   └──────────────────────────────────────────┘ │
                └────────────────────────────────────────────────┘
```

### 11.3 国产化组件

表：国产化组件对照表

| 层级 | 组件 | 厂商 | 备注 |
|---|---|---|---|
| CPU | 鲲鹏 920 | 华为 | ARM64 架构 |
| CPU | 飞腾 FT2000+/D2000 | 飞腾信息 | ARM64 架构 |
| CPU | 海光 7000 系列 | 海光信息 | x86_64 架构 |
| CPU | 兆芯 KX-7000 | 兆芯 | x86_64 架构 |
| CPU | 龙芯 3A5000/3C5000 | 龙芯中科 | LoongArch 架构 |
| OS | 中标麒麟 V7/V10 | 中标软件 | 基于 CentOS/UOS |
| OS | 统信 UOS V20 | 统信软件 | 基于 Debian |
| OS | openEuler 22.03 LTS | 开源社区 | 华为主导 |
| DB | 达梦 DM8 | 达梦数据 | 关系型数据库 |
| DB | 人大金仓 KingbaseES V8 | 人大金仓 | 基于 PostgreSQL |
| DB | OceanBase V4 | 蚂蚁集团 | 分布式数据库 |
| DB | GaussDB | 华为 | 关系型数据库 |
| 中间件 | 东方通 TongWeb V7 | 东方通 | 应用服务器 |
| 中间件 | 中创 InforSuite AS | 中创软件 | 应用服务器 |
| 中间件 | 宝兰德 BES AppServer | 宝兰德 | 应用服务器 |

### 11.4 兼容性验证矩阵

表：国产化兼容性验证矩阵

| CPU | OS | DB | 中间件 | Go 编译 | OpsMesh 验证 |
|---|---|---|---|:---:|:---:|
| 鲲鹏 920 | openEuler 22.03 | GaussDB | - | ✅ | ✅ 已验证 |
| 鲲鹏 920 | 统信 UOS V20 | 达梦 DM8 | 东方通 | ✅ | ✅ 已验证 |
| 飞腾 FT2000+ | 中标麒麟 V10 | 人大金仓 | 中创 | ✅ | ✅ 已验证 |
| 飞腾 D2000 | openEuler 22.03 | OceanBase | - | ✅ | ⚠ 待验证 |
| 海光 7000 | 中标麒麟 V10 | 达梦 DM8 | 宝兰德 | ✅ | ✅ 已验证 |
| 兆芯 KX-7000 | 统信 UOS V20 | 人大金仓 | 东方通 | ✅ | ⚠ 待验证 |
| 龙芯 3A5000 | 统信 UOS V20 | 达梦 DM8 | - | ✅ | ⚠ 待验证 |

### 11.5 等保 2.0 合规

表：等保 2.0 三级合规对照表

| 控制域 | 控制点 | OpsMesh 实现 |
|---|---|---|
| 安全物理环境 | 物理访问控制 | 由机房提供 |
| 安全通信网络 | 网络架构 | 分区隔离（DMZ/内网/核心区） |
| 安全通信网络 | 通信加密 | TLS/mTLS + 商密 SM2/SM4 |
| 安全区域边界 | 边界防护 | NetworkPolicy + WAF |
| 安全区域边界 | 访问控制 | RBAC + ABAC |
| 安全计算环境 | 身份鉴别 | JWT + 多因素 + 密码复杂度 |
| 安全计算环境 | 访问控制 | RBAC + 分权分域 |
| 安全计算环境 | 安全审计 | 全操作审计 + 365 天留存 |
| 安全计算环境 | 入侵防范 | 命令白名单 + 文件白名单 |
| 安全计算环境 | 恶意代码防范 | Agent 命令白名单 |
| 安全管理中心 | 系统管理 | 集中管理 + 分权 |
| 安全管理中心 | 集中管控 | 统一控制面 + 联邦 |

### 11.6 配置示例

```yaml
# 国产化环境 Helm values（鲲鹏 + openEuler + GaussDB）
global:
  imageRegistry: "registry.domestic.example.com/opsmesh/"
  imagePullSecrets:
    - name: domestic-registry-secret

controlplane:
  image:
    repository: opsmesh/opsmesh
    tag: "v1.0.0-linux-arm64"  # 鲲鹏 ARM64
  replicaCount: 3
  store: mysql
  production: true
  nodeSelector:
    kubernetes.io/arch: arm64
    kubernetes.io/os: linux
  extraEnv:
    - name: OPSMESH_DOMESTIC_MODE
      value: "true"
    - name: OPSMESH_DB_DRIVER
      value: "gaussdb"  # 使用 GaussDB
    - name: OPSMESH_MYSQL_DSN
      valueFrom:
        secretKeyRef:
          name: gaussdb-secret
          key: dsn
    - name: OPSMESH_CRYPTO_ALGORITHM
      value: "sm4"  # 商密 SM4 加密
    - name: OPSMESH_TLS_CIPHER
      value: "ECDHE-SM2-SM4-SM3"  # 商密 TLS 套件
    - name: OPSMESH_AUDIT_RETENTION_DAYS
      value: "365"  # 等保要求 365 天
    - name: OPSMESH_RBAC_DOMAIN_ENABLED
      value: "true"  # 分权分域

mysql:
  enabled: false  # 使用外部 GaussDB

# 国产化中间件（如使用东方通 TongWeb 替代 Tomcat）
# 注：OpsMesh 控制面是 Go 二进制，不依赖 Java 中间件
# 中间件仅用于被纳管的业务系统
```

### 11.7 适用场景

- 党政军信息系统
- 金融行业关键系统
- 能源/电力行业
- 国企央企
- 等保 2.0 三级及以上要求
- 国产化替代项目

---

## 第12章 场景十：容器化部署

### 12.1 场景描述

容器化部署将 OpsMesh 完全容器化，经 Helm Chart 或 Operator 部署到 K8s 集群，利用 K8s 的弹性、自愈、滚动升级能力。

### 12.2 架构图

图：容器化部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │              K8s 集群                          │
                │                                                │
                │   ┌──────────────────────────────────────────┐ │
                │   │   Ingress Controller                     │ │
                │   │   Nginx / Traefik / HAProxy              │ │
                │   └────────────────────┬─────────────────────┘ │
                │                        │                        │
                │   ┌────────────────────┴─────────────────────┐ │
                │   │   opsmesh namespace                      │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   控制面 Deployment (3 副本)      │   │ │
                │   │   │   + PDB (minAvailable=1)          │   │ │
                │   │   │   + HPA (2-10)                    │   │ │
                │   │   │   + ServiceAccount                │   │ │
                │   │   │   + ConfigMap                     │   │ │
                │   │   │   + Secret                        │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   Agent DaemonSet (每节点一个)    │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   MySQL StatefulSet (主从)        │   │ │
                │   │   │   + PVC (100Gi)                   │   │ │
                │   │   │   + Backup CronJob                │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   Redis StatefulSet (哨兵)        │   │ │
                │   │   │   + PVC (20Gi)                    │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   ServiceMonitor + PrometheusRule│   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                └────────────────────────────────────────────────┘
```

### 12.3 Helm Chart 与 Operator

表：容器化部署方式对照表

| 方式 | 路径 | 适用 | 特点 |
|---|---|---|---|
| Helm Chart | `deploy/helm/opsmesh/` | 通用 K8s 部署 | 简单直接，14 个模板 |
| K8s Operator | `operator/` | 声明式管理 | CRD + Reconcile，自动化更高 |
| GitOps | `deploy/gitops/` | 持续部署 | ArgoCD ApplicationSet，多集群 |

### 12.4 组件容器化

- **控制面镜像**：`opsmesh/opsmesh:latest`，基于 distroless，nonroot 运行
- **Agent 镜像**：`opsmesh/opsmesh-agent:latest`，基于 debian，含 sh
- **MySQL 镜像**：`mysql:8`，官方镜像
- **Redis 镜像**：`redis:7`，官方镜像

### 12.5 存储配置

```yaml
# 存储配置
mysql:
  persistence:
    size: 100Gi
    storageClass: "fast-ssd"  # 推荐 SSD
  # 备份 PVC
  backup:
    enabled: true
    storageSize: 50Gi
    storageClass: "standard"

redis:
  persistence:
    size: 20Gi
    storageClass: "fast-ssd"
```

### 12.6 网络配置

```yaml
# Service 配置
apiVersion: v1
kind: Service
metadata:
  name: opsmesh-controlplane
  namespace: opsmesh
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/component: controlplane
  ports:
    - name: http
      port: 8080
      targetPort: 8080
    - name: grpc
      port: 9090
      targetPort: 9090
    - name: metrics
      port: 9091
      targetPort: 9091
```

### 12.7 配置管理

- **ConfigMap**：非敏感配置（如 advertiseAddr）
- **Secret**：敏感配置（jwtSecret、provisionSecret、mysql 密码）
- **Helm values**：环境差异（dev/staging/prod）
- **GitOps**：配置版本控制，PR 审核

### 12.8 弹性伸缩

```yaml
# HPA + PDB
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: opsmesh-controlplane-pdb
  namespace: opsmesh
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/component: controlplane
```

### 12.9 配置示例（values.yaml）

```yaml
# 完整生产 values.yaml
global:
  imageRegistry: "registry.example.com/"
  storageClass: "fast-ssd"

controlplane:
  image:
    repository: opsmesh/opsmesh
    tag: "v1.0.0"
  replicaCount: 3
  store: mysql
  production: true
  requireAuth: true
  tls:
    enabled: true
    secretName: opsmesh-tls
  taskLeaseSec: 300
  taskMaxRetries: 3
  leaderTTLSec: 15
  leaderTickSec: 5
  archiveAgeMin: 1440
  cookieSecure: true
  alertWebhookURL: "https://feishu.example.com/webhook/xxx"
  alertNotifierType: feishu
  resources:
    requests: { cpu: 500m, memory: 512Mi }
    limits: { cpu: 2000m, memory: 2Gi }
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/name: opsmesh
                app.kubernetes.io/component: controlplane
            topologyKey: kubernetes.io/hostname

agent:
  enabled: true
  image:
    repository: opsmesh/opsmesh-agent
    tag: "v1.0.0"
  segment: default
  workerConcurrency: 8
  taskTimeout: 300s
  demo: false
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits: { cpu: 500m, memory: 512Mi }

mysql:
  enabled: true
  image: mysql:8
  database: opsmesh
  persistence:
    size: 100Gi
    storageClass: "fast-ssd"
  backup:
    enabled: true
    schedule: "0 2 * * *"
    retentionDays: 7
    storageSize: 50Gi
  resources:
    requests: { cpu: 500m, memory: 1Gi }
    limits: { cpu: 2000m, memory: 4Gi }

redis:
  enabled: true
  image: redis:7
  persistence:
    size: 20Gi
  resources:
    requests: { cpu: 200m, memory: 256Mi }
    limits: { cpu: 1000m, memory: 1Gi }

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: opsmesh.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: opsmesh-tls
      hosts: [opsmesh.example.com]

observability:
  serviceMonitor:
    enabled: true
    interval: 30s
  prometheusRule:
    enabled: true

podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "9091"
```

### 12.10 适用场景

- K8s 集群环境
- 云原生部署
- 需要弹性伸缩
- GitOps 持续部署
- 多集群管理

---

## 第13章 场景十一：高安全隔离环境

### 13.1 场景描述

高安全隔离环境适用于涉密系统、金融核心、军工等场景，要求物理或逻辑隔离，满足等保三级或分保四级要求。

### 13.2 架构图

图：高安全隔离环境架构图

```text
                ┌────────────────────────────────────────────────┐
                │              物理隔离 / 逻辑隔离                │
                │                                                │
                │   ┌──────────────────────────────────────────┐ │
                │   │   DMZ 区（非军事区）                      │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   堡垒机 / 跳板机                  │   │ │
                │   │   │   + 审计代理                       │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   └──────────────────────┬───────────────────┘ │
                │                          │ 单向网闸             │
                │   ┌──────────────────────┴───────────────────┐ │
                │   │   内网区                                  │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   OpsMesh 控制面 ×3               │   │ │
                │   │   │   + Agent                          │   │ │
                │   │   │   + MySQL + Redis                  │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   └──────────────────────┬───────────────────┘ │
                │                          │ 防火墙               │
                │   ┌──────────────────────┴───────────────────┐ │
                │   │   核心区                                  │ │
                │   │   ┌──────────────────────────────────┐   │ │
                │   │   │   核心业务系统                     │   │ │
                │   │   │   + 核心数据                       │   │ │
                │   │   └──────────────────────────────────┘   │ │
                │   └──────────────────────────────────────────┘ │
                └────────────────────────────────────────────────┘
```

### 13.3 网络隔离

表：网络分区对照表

| 区域 | 网段 | 可访问 | 被访问 | 安全控制 |
|---|---|---|---|---|
| DMZ | 10.1.0.0/16 | 外部（经堡垒机） | 内网（单向） | WAF + IDS + IPS |
| 内网 | 10.2.0.0/16 | DMZ（被访问） | 核心区（经防火墙） | 防火墙 + 审计 |
| 核心区 | 10.3.0.0/16 | 内网（被访问） | 无 | 严格访问控制 |

### 13.4 安全要求

表：安全等级要求对照表

| 等级 | 要求 | OpsMesh 实现 |
|---|---|---|
| 等保三级 | 身份鉴别、访问控制、安全审计、数据完整性、数据保密性 | RBAC + 审计 + TLS + 加密 |
| 分保四级 | 物理隔离、密码保护、严格审计 | 物理隔离 + 商密 + 全审计 |
| 涉密 | 物理隔离、专人管理、定期审查 | 物理隔离 + 三员分立 |

### 13.5 访问控制

- **三员分立**：系统管理员、安全管理员、安全审计员三权分立
- **堡垒机**：所有运维操作经堡垒机，禁止直连
- **多因素认证**：密码 + 证书 + 动态口令
- **命令白名单**：Agent 仅可执行白名单内命令
- **文件白名单**：文件操作仅限白名单目录

### 13.6 数据安全

- **传输加密**：TLS 1.3 + 商密 SM2/SM4
- **存储加密**：MySQL 透明加密（TDE）+ Redis 加密
- **备份加密**：备份文件经 SM4/AES-256 加密后存储
- **数据脱敏**：日志/审计中敏感数据脱敏
- **数据销毁**：退役设备数据安全销毁（多次覆写）

### 13.7 配置示例

```yaml
# 高安全隔离环境 Helm values
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  requireAuth: true
  tls:
    enabled: true
    secretName: opsmesh-tls
  extraEnv:
    # 安全加固
    - name: OPSMESH_PUBLIC_REGISTER
      value: "false"
    - name: OPSMESH_ALLOW_PUBLIC_REGISTER
      value: "false"
    - name: OPSMESH_GRPC_REQUIRE_SIGNATURE
      value: "true"
    - name: OPSMESH_COOKIE_SECURE
      value: "true"
    - name: OPSMESH_TRUST_PROXY
      value: "false"
    - name: OPSMESH_METRICS_ALLOW_CIDR
      value: "10.2.0.0/16"  # 仅内网可访问 metrics
    # Agent 限制
    - name: OPSMESH_AGENT_SHELL_WHITELIST
      value: "/usr/bin/yum,/usr/bin/systemctl,/usr/bin/cat,/usr/bin/ls"
    - name: OPSMESH_AGENT_FILE_ROOT_WHITELIST
      value: "/etc/opsmesh,/var/lib/opsmesh"
    - name: OPSMESH_PROVISION_SSH_KNOWN_HOSTS
      value: "/etc/opsmesh/known_hosts"
    # 审计
    - name: OPSMESH_AUDIT_RETENTION_DAYS
      value: "2555"  # 7 年留存
    - name: OPSMESH_AUDIT_DESENSITIZE
      value: "true"  # 审计脱敏
    # 商密
    - name: OPSMESH_CRYPTO_ALGORITHM
      value: "sm4"
    - name: OPSMESH_TLS_CIPHER
      value: "ECDHE-SM2-SM4-SM3"
    # 三员分立
    - name: OPSMESH_RBAC_THREE_ADMIN
      value: "true"

# 网络策略
networkPolicy:
  enabled: true
  ingress:
    - from:
        - ipBlock: { cidr: 10.2.0.0/16 }  # 仅内网
      ports:
        - { port: 8080 }
        - { port: 9090 }
  egress:
    - to:
        - ipBlock: { cidr: 10.2.0.0/16 }  # 仅内网
      ports:
        - { port: 3306 }
        - { port: 6379 }
```

### 13.8 适用场景

- 涉密信息系统
- 金融核心系统
- 军工/国防系统
- 等保三级及以上
- 分保四级
- 物理隔离环境

---

## 第14章 场景十二：灾备与连续性

### 14.1 场景描述

灾备与连续性部署适用于关键业务系统，要求主站故障时备站可快速接管，DR 站作为最后兜底。参考 `docs/dr-runbook.md`。

### 14.2 架构图

图：灾备与连续性部署架构图

```text
                ┌────────────────────────────────────────────────┐
                │            全局 DNS / GSLB                     │
                │   健康检查 + 自动切换                          │
                └──────┬────────────────────┬────────────────────┘
                       │                    │
                       ▼                    ▼
        ┌──────────────────┐      ┌──────────────────┐
        │   主站 Primary    │      │   备站 Secondary  │
        │   (Active)        │◄═══►│   (Active)        │
        │                  │ 双活  │                  │
        │ ┌──────────────┐ │      │ ┌──────────────┐ │
        │ │ 控制面 ×3     │ │      │ │ 控制面 ×3     │ │
        │ │ + Agent       │ │      │ │ + Agent       │ │
        │ └──────────────┘ │      │ └──────────────┘ │
        │ ┌──────────────┐ │      │ ┌──────────────┐ │
        │ │ MySQL 主      │══════►│ │ MySQL 主      │ │
        │ └──────────────┘ │ 双向  │ └──────────────┘ │
        │ ┌──────────────┐ │ 复制  │ ┌──────────────┐ │
        │ │ 备份 PVC      │ │      │ │ 备份 PVC      │ │
        │ └──────────────┘ │      │ └──────────────┘ │
        └──────────────────┘      └──────────────────┘
                       │                    │
                       └─────────┬──────────┘
                                 │ 异地复制
                                 ▼
                        ┌──────────────────┐
                        │   DR 站           │
                        │   (Cold Standby)  │
                        │                  │
                        │ ┌──────────────┐ │
                        │ │ 控制面 ×3     │ │
                        │ │ (休眠)        │ │
                        │ └──────────────┘ │
                        │ ┌──────────────┐ │
                        │ │ MySQL         │ │
                        │ │ (异地备份)    │ │
                        │ └──────────────┘ │
                        └──────────────────┘
```

### 14.3 灾备模式

表：灾备模式对照表

| 模式 | 主站 | 备站 | DR 站 | RTO | RPO | 适用 |
|---|---|---|---|:---:|:---:|---|
| 主备 | Active | Standby | - | ≤30min | ≤1s | 一般关键业务 |
| 双活 | Active | Active | - | ≤0 | ≤0 | 强一致要求 |
| 多活 | Active | Active | Active | ≤0 | ≤0 | 极高可用 |
| 主备+DR | Active | Standby | Cold | ≤4h | ≤24h | 监管要求 |
| 双活+DR | Active | Active | Cold | ≤30min | ≤1s | 关键金融 |

### 14.4 RTO/RPO 目标

表：RTO/RPO 目标对照表

| 场景 | RPO 目标 | RTO 目标 | 达成条件 |
|---|:---:|:---:|---|
| MySQL 误操作 | ≤24h | ≤30min | 备份可挂载，参考 dr-runbook 3.2 |
| MySQL Pod 故障 | 0 | ≤5min | StatefulSet 重调度 |
| MySQL 节点故障 | ≤24h | ≤45min | 异地副本 + 灌库 |
| 控制面全副本故障 | 0 | ≤2min | K8s 重调度 |
| 单站全损 | ≤24h | ≤4h | 异地备份 + 新集群 |
| 双活单站故障 | 0 | ≤0 | 自动切换 |
| 全部站点故障 | ≤24h | ≤8h | DR 站恢复 |

### 14.5 数据复制

- **同步复制**：主 ↔ 备 MySQL 双向同步复制（Galera Cluster / Group Replication）
- **异步复制**：主 → DR MySQL 异步 binlog 复制
- **备份归档**：每日 mysqldump → 对象存储，跨区域复制
- **配置同步**：Helm values 经 Git 版本控制，三站同步

### 14.6 故障切换

参考 `docs/dr-runbook.md` 第 5 章：

1. **控制面主副本故障**：leader 选举自动切换，RTO ≤15s
2. **MySQL 单点故障**：MHA/Orchestrator 自动切换到从，RTO ≤30s
3. **单站全损**：DNS 切换 + 备站提升为主，RTO ≤4h
4. **全部主备故障**：DR 站恢复，RTO ≤8h

### 14.7 切换演练

参考 `docs/dr-runbook.md` 第 6 章：

| 演练项 | 频率 | 负责人 | 通过标准 |
|---|---|---|---|
| 备份可恢复性 | 每月 | SRE | 取最近备份灌入临时库，校验关键表行数 |
| MySQL 恢复 | 每季度 | SRE + Owner | 完整执行 dr-runbook 3.2，RTO 达标 |
| 单站切换 | 每半年 | SRE + Owner | 主备切换，业务可用且数据一致 |
| 全中心切换 | 每年 | Owner | 执行 dr-runbook 5.4，业务可用 |

### 14.8 配置示例

```yaml
# 主站 Helm values
controlplane:
  replicaCount: 3
  store: mysql
  production: true
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "primary"
    - name: OPSMESH_FEDERATION_PEERS
      value: "https://secondary.opsmesh.example.com:9090,https://dr.opsmesh.example.com:9090"
    - name: OPSMESH_FEDERATION_SECRET
      valueFrom:
        secretKeyRef:
          name: federation-secret
          key: secret
    - name: OPSMESH_DR_MODE
      value: "active-active"

mysql:
  replication:
    enabled: true
    role: master
    # 双向同步复制（Galera）
    mode: "galera"
    clusterNodes: 3
  backup:
    enabled: true
    schedule: "0 2 * * *"
    retentionDays: 30
    # 异地副本
    remoteCopy:
      enabled: true
      target: "s3://opsmesh-backup-dr/dr/"
      encryption: "aes-256-cbc"
```

```yaml
# DR 站 Helm values（休眠状态）
controlplane:
  replicaCount: 0  # 休眠，故障时扩容
  store: mysql
  extraEnv:
    - name: OPSMESH_FEDERATION_ROLE
      value: "dr"
    - name: OPSMESH_DR_MODE
      value: "cold-standby"

mysql:
  enabled: true
  replication:
    enabled: false  # 独立，从异地备份恢复
  persistence:
    size: 500Gi
```

### 14.9 参考

- 详细灾备操作流程：`docs/dr-runbook.md`
- 备份 CronJob 配置：`deploy/helm/opsmesh/templates/mysql-backup-cronjob.yaml`
- 生产环境检查清单：`docs/deployment-guide.md` 第 8 章

---

## 第15章 场景对比与选型

### 15.1 场景对比矩阵

表：场景对比矩阵表

| 场景 | 规模 | 高可用 | 复杂度 | RTO | RPO | 成本 | 适用 |
|---|---|:---:|:---:|:---:|:---:|:---:|---|
| 单机房 | ≤500 | ✅ | ★★ | ≤30min | ≤24h | 低 | 中小企业 |
| 异地多机房 | ≤6000 | ✅ | ★★★ | ≤30min | ≤1s | 中 | 跨城市企业 |
| 多数据中心 | ≤10000 | ✅ | ★★★ | ≤30min | ≤0 | 中高 | 大型企业 |
| 电信资源池 | ≥50000 | ✅ | ★★★★ | ≤15min | ≤0 | 高 | 运营商 |
| 混合云 | 弹性 | ✅ | ★★★ | ≤30min | ≤1s | 中 | 混合架构 |
| 公有云 | 弹性 | ✅ | ★★ | ≤5min | ≤0 | 低 | 云原生 |
| 私有云 | ≤5000 | ✅ | ★★★ | ≤30min | ≤24h | 中高 | 私有云 |
| 边缘 | ≤100/边 | 部分 | ★★★ | ≤60min | ≤1h | 中 | IoT/边缘 |
| 国产化 | 任意 | ✅ | ★★★★ | ≤30min | ≤24h | 高 | 党政军 |
| 容器化 | 弹性 | ✅ | ★★ | ≤5min | ≤0 | 低 | K8s |
| 高安全隔离 | ≤2000 | ✅ | ★★★★ | ≤30min | ≤0 | 高 | 涉密 |
| 灾备 | 任意 | ✅✅ | ★★★★ | ≤4h | ≤24h | 高 | 关键业务 |

### 15.2 选型决策树

图：选型决策树

```text
                        ┌──────────────────┐
                        │  设备规模？       │
                        └────────┬─────────┘
                                 │
                ┌────────────────┼────────────────┐
                │                │                │
                ▼                ▼                ▼
           ≤500 台          500-10000         >10000
                │                │                │
                ▼                ▼                ▼
        ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
        │ 是否 K8s？    │ │ 是否多机房？  │ │ 是否电信？    │
        └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
               │                │                │
        ┌──────┴──────┐ ┌──────┴──────┐ ┌──────┴──────┐
        │             │ │             │ │             │
        ▼             ▼ ▼             ▼ ▼             ▼
       是 否          是 否          是 否
        │ │            │ │            │ │
        ▼ ▼            ▼ ▼            ▼ ▼
   容器化 单机房   多机房/多DC  集群   电信资源池  分布式
```

```text
                        ┌──────────────────┐
                        │  特殊要求？       │
                        └────────┬─────────┘
                                 │
        ┌────────────┬───────────┼───────────┬────────────┐
        │            │           │           │            │
        ▼            ▼           ▼           ▼            ▼
     国产化       高安全       灾备        边缘        混合云
        │            │           │           │            │
        ▼            ▼           ▼           ▼            ▼
    国产化环境   高安全隔离   灾备模式    边缘计算     混合云
```

### 15.3 成本估算

表：成本估算表（年成本，单位万元）

| 场景 | 服务器 | 网络 | 存储 | 软件 | 运维 | 合计 |
|---|---|---|---|---|---|---|
| 单机房 | 5 | 1 | 2 | 0 | 5 | 13 |
| 异地多机房 | 20 | 10 | 8 | 0 | 15 | 53 |
| 多数据中心 | 50 | 30 | 20 | 0 | 30 | 130 |
| 电信资源池 | 200 | 100 | 80 | 0 | 100 | 480 |
| 混合云 | 15 | 8 | 5 | 5 | 10 | 43 |
| 公有云 | 0 | 0 | 0 | 20 | 5 | 25 |
| 私有云 | 30 | 5 | 10 | 0 | 15 | 60 |
| 边缘 | 10 | 5 | 2 | 0 | 10 | 27 |
| 国产化 | 50 | 10 | 20 | 30 | 20 | 130 |
| 容器化 | 10 | 2 | 5 | 0 | 8 | 25 |
| 高安全隔离 | 40 | 15 | 15 | 10 | 20 | 100 |
| 灾备 | 60 | 20 | 30 | 0 | 30 | 140 |

### 15.4 推荐配置

表：按规模推荐配置表

| 规模 | 推荐场景 | 控制面 | MySQL | Redis | 备注 |
|---|---|---|---|---|---|
| ≤100 | 单机 | 1 副本 memory | - | - | 演示/PoC |
| ≤500 | 单机房 | 3 副本 | 100Gi | 20Gi | 中小企业 |
| ≤2000 | 集群/私有云 | 3 副本 | 200Gi | 40Gi | 中企业 |
| ≤10000 | 多数据中心 | 每DC 3 副本 | 每DC 500Gi | 每DC 80Gi | 大企业 |
| ≤50000 | 联邦 | 每网段 3 副本 | 每网段 1Ti | 每网段 100Gi | 超大企业 |
| >50000 | 电信资源池 | 分层 5/3/1 | 分层 2Ti/500Gi/100Gi | 分层 | 运营商 |

---

## 第16章 部署自动化

### 16.1 部署工具链

表：部署工具链对照表

| 工具 | 用途 | 适用场景 | 文件路径 |
|---|---|---|---|
| Helm | K8s 应用部署 | 容器化、公有云、私有云 | `deploy/helm/opsmesh/` |
| K8s Operator | 声明式管理 | 高级 K8s 部署 | `operator/` |
| ArgoCD GitOps | 持续部署 | 多集群、GitOps | `deploy/gitops/` |
| Terraform | 基础设施即代码 | 公有云、私有云 | - |
| Ansible | 配置管理 | 物理机、VM | - |
| systemd | 系统服务 | 物理机、VM | `deploy/systemd/` |
| docker-compose | 容器编排 | 单机、开发 | `docker-compose.yaml` |

### 16.2 部署流水线

图：部署流水线

```text
    代码提交 → CI 构建 → 镜像推送 → 部署测试 → 部署生产
        │         │         │           │           │
        ▼         ▼         ▼           ▼           ▼
    Git Push  Go Build  Docker Push  Helm Test   ArgoCD Sync
        │         │         │           │           │
        ▼         ▼         ▼           ▼           ▼
    .goreleaser  Make    Registry    E2E Test    GitOps PR
```

```yaml
# CI/CD 流水线示例（GitHub Actions）
name: deploy
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: make build
      - run: make test
      - name: Build Docker image
        run: docker build -t opsmesh/opsmesh:${{ github.sha }} .
      - name: Push to registry
        run: |
          docker login -u ${{ secrets.REGISTRY_USER }} -p ${{ secrets.REGISTRY_PASS }}
          docker push opsmesh/opsmesh:${{ github.sha }}

  deploy-staging:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Deploy to staging
        run: |
          helm upgrade opsmesh ./deploy/helm/opsmesh \
            -f deploy/helm/opsmesh/values-production.yaml \
            --set controlplane.image.tag=${{ github.sha }} \
            -n opsmesh-staging

  deploy-production:
    needs: deploy-staging
    runs-on: ubuntu-latest
    environment: production
    steps:
      - name: Trigger GitOps PR
        run: |
          # 更新 GitOps 仓库的 image tag，创建 PR
          # 经审核后 ArgoCD 自动同步到生产
```

### 16.3 配置管理

- **Helm values 分层**：`values.yaml`（基础）→ `values-production.yaml`（生产）→ `--set`（实例特定）
- **Secret 管理**：Sealed Secrets / External Secrets / Vault
- **GitOps**：配置经 Git 版本控制，PR 审核，ArgoCD 同步
- **环境隔离**：dev / staging / prod 独立 namespace + 独立 values

### 16.4 回滚策略

```bash
# Helm 回滚
helm rollback opsmesh -n opsmesh  # 回滚到上一个 release

# 查看历史
helm history opsmesh -n opsmesh

# 回滚到指定版本
helm rollback opsmesh -n opsmesh 5

# ArgoCD 回滚
argocd app rollback opsmesh-prod  # 回滚到上一个 sync

# K8s Deployment 回滚
kubectl rollout undo deploy/opsmesh-controlplane -n opsmesh
kubectl rollout status deploy/opsmesh-controlplane -n opsmesh

# 数据库回滚（参考 dr-runbook 3.2）
# 1. 缩容控制面
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=0
# 2. 恢复 MySQL 到指定时间点
mysqlbinlog --stop-datetime="2026-08-17 10:00:00" binlog.000123 | mysql -uroot -p
# 3. 扩容控制面
kubectl scale deploy opsmesh-controlplane -n opsmesh --replicas=3
```

---

## 附录

### 附录 A：各场景配置模板

#### A.1 单机房 docker-compose.yaml

参见第 3.6 节。

#### A.2 异地多机房 Helm values

参见第 4.8 节。

#### A.3 多数据中心 Helm values

参见第 5.8 节。

#### A.4 电信资源池 Helm values

参见第 6.9 节。

#### A.5 混合云 Helm values

参见第 7.8 节。

#### A.6 公有云 Helm values

参见第 8.7 节。

#### A.7 私有云 Helm values

参见第 9.8 节。

#### A.8 边缘计算 Helm values

参见第 10.6 节。

#### A.9 国产化环境 Helm values

参见第 11.6 节。

#### A.10 容器化 Helm values

参见第 12.9 节。

#### A.11 高安全隔离 Helm values

参见第 13.7 节。

#### A.12 灾备 Helm values

参见第 14.8 节。

### 附录 B：各场景容量规划公式

表：容量规划公式表

| 资源 | 公式 | 备注 |
|---|---|---|
| 控制面 CPU | `基础CPU + 设备数 × 单设备CPU系数` | 单设备 CPU 系数：0.002-0.005 |
| 控制面内存 | `基础内存 + 设备数 × 单设备内存系数` | 单设备内存系数：2-5Mi |
| MySQL 存储 | `基础存储 + 设备数 × 单设备存储 + 任务数 × 单任务存储` | 单设备 50-100Mi，单任务 10-50Ki |
| Redis 内存 | `基础内存 + 设备数 × 单设备内存` | 单设备 1-3Mi |
| 任务吞吐 | `副本数 × 单副本QPS` | 单副本 30-50 任务/秒 |
| 心跳带宽 | `设备数 × 心跳包大小 / 心跳间隔` | 心跳包 1KB，间隔 10s |
| 联邦同步带宽 | `设备数 × 同步包大小 / 同步间隔` | 同步包 5KB，间隔 30s |
| 并发 gRPC | `设备数 × 1 长连接` | 每设备一个长连接 |

### 附录 C：各场景网络要求

表：网络要求对照表

| 场景 | 带宽 | 延迟 | 协议 | 端口 |
|---|---|---|---|---|
| 单机房 | ≥1Gbps | ≤1ms | TCP | 8080/9090/9091/3306/6379 |
| 异地多机房 | ≥100Mbps | ≤30ms | TCP/IPsec | 同上 + 443 |
| 多数据中心 | ≥1Gbps | ≤50ms | TCP/IPsec | 同上 + 443 |
| 电信资源池 | ≥10Gbps | ≤20ms | TCP | 同上 |
| 混合云 | ≥100Mbps | ≤50ms | TCP/IPsec/VPN | 同上 + 443 |
| 公有云 | 云内网 | ≤10ms | TCP | 同上 + 443 |
| 私有云 | ≥1Gbps | ≤5ms | TCP | 同上 |
| 边缘 | ≥10Mbps | ≤100ms | TCP/VPN | 同上 + 443 |
| 国产化 | ≥1Gbps | ≤5ms | TCP | 同上 |
| 容器化 | ≥1Gbps | ≤1ms | TCP | 同上 |
| 高安全隔离 | ≥1Gbps | ≤5ms | TCP/单向网闸 | 同上 |
| 灾备 | ≥100Mbps | ≤30ms | TCP/IPsec | 同上 + 443 |

### 附录 D：各场景安全检查清单

#### D.1 通用安全检查

- [ ] `--production=true` 生产模式开启
- [ ] `--require-auth=true` 强制鉴权
- [ ] `--store=mysql` 多副本使用 MySQL
- [ ] `--jwt-secret` 已注入 ≥32 字节强随机密钥
- [ ] `--provision-secret` 已注入强随机密钥
- [ ] `--tls-cert` / `--tls-key` TLS 证书已配置
- [ ] `--client-ca` mTLS 客户端 CA 已配置
- [ ] `--cookie-secure=true` Cookie Secure 开启
- [ ] `--public-register=false` 公开注册关闭
- [ ] `--grpc-require-signature=true` gRPC 签名强制
- [ ] `--metrics-allow-cidr` 内网监控网段已配置
- [ ] `--agent-shell-whitelist` 命令白名单已配置
- [ ] `--agent-file-root-whitelist` 文件白名单已配置
- [ ] `--provision-ssh-known-hosts` SSH known_hosts 已配置
- [ ] `--demo` 演示模式已关闭

#### D.2 国产化额外检查

- [ ] CPU 国产化（鲲鹏/飞腾/海光/兆芯/龙芯）
- [ ] OS 国产化（中标麒麟/统信 UOS/openEuler）
- [ ] DB 国产化（达梦/人大金仓/OceanBase/GaussDB）
- [ ] 商密算法（SM2/SM3/SM4）
- [ ] 等保 2.0 三级合规
- [ ] 审计日志留存 ≥180 天

#### D.3 高安全隔离额外检查

- [ ] 物理隔离或逻辑隔离已实施
- [ ] 三员分立（系统/安全/审计管理员）
- [ ] 堡垒机接入
- [ ] 多因素认证
- [ ] 审计日志留存 ≥2555 天（7 年）
- [ ] 数据加密（传输 + 存储 + 备份）
- [ ] 数据脱敏
- [ ] 网络分区（DMZ/内网/核心区）

#### D.4 灾备额外检查

- [ ] 备份 CronJob 正常运行
- [ ] 异地副本已配置
- [ ] 备份加密已启用
- [ ] 恢复演练已执行（参考 dr-runbook 第 6 章）
- [ ] RTO/RPO 达标
- [ ] 故障切换流程已文档化
- [ ] 联邦 mTLS 已配置

### 附录 E：相关文档索引

表：相关文档索引表

| 文档 | 路径 | 说明 |
|---|---|---|
| 架构设计 | `docs/architecture.md` | 整体架构、分层、模块依赖、数据流 |
| 部署指南 | `docs/deployment-guide.md` | 具体部署操作（docker/systemd/helm/operator） |
| 灾备 Runbook | `docs/dr-runbook.md` | 备份恢复、故障切换、演练 |
| 模块设计 | `docs/module-design.md` | 各功能模块详细设计 |
| 功能设计 | `docs/feature-design.md` | 功能特性设计 |
| 数据库设计 | `docs/database-design.md` | 数据库 schema 设计 |
| API 规范 | `docs/api-specification.md` | REST/gRPC API 规范 |
| 安全机制 | `docs/security-mechanism.md` | 安全设计与实现 |
| 运维手册 | `docs/operations.md` | 日常运维操作 |
| 产品路线图 | `docs/product-roadmap.md` | 产品演进规划 |
| 技术选型 | `docs/tech-selection.md` | 技术栈选型说明 |
| UI 设计 | `docs/ui-design.md` | 前端 UI 设计 |
| 测试规范 | `docs/test-specification.md` | 测试用例与规范 |
| Flag 矩阵 | `docs/flag-matrix.md` | 116 个 flag 对照 |
| SSE 协议 | `docs/sse-protocol.md` | SSE 事件协议 |
| 镜像固定 | `docs/image-pinning.md` | 镜像版本固定策略 |
| 技术债务 | `docs/tech-debt.md` | 技术债务跟踪 |
| 安全问题 | `docs/security-issues.md` | 安全问题跟踪 |

表：部署相关代码路径索引表

| 路径 | 说明 |
|---|---|
| `Dockerfile` | 控制面镜像（distroless） |
| `Dockerfile.agent` | Agent 镜像（debian） |
| `docker-compose.yaml` | 开发/演示一键启动 |
| `docker-compose.e2e-sec.yaml` | E2E 安全测试 |
| `deploy/helm/opsmesh/` | Helm Chart（14 个模板） |
| `deploy/systemd/` | systemd 服务文件 |
| `deploy/gitops/` | ArgoCD GitOps 配置 |
| `operator/` | K8s Operator（CRD + Reconcile） |

---

> 本文档随 OpsMesh 版本演进持续更新。新增场景或配置变更请同步更新本文档与 `docs/deployment-guide.md`。如需新增场景，参考第 2 章部署模式总览，按"架构图 + 组件清单 + 网络拓扑 + 数据流 + 配置示例 + 容量规划 + 高可用 + 适用场景"八段式编写。