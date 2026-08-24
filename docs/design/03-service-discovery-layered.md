# 03 - 服务发现分层：Nacos vs K8s Service

## 冲突描述

Nacos 和 K8s Service 都提供服务注册与发现：
- **K8s Service**：容器内服务发现（ClusterIP、DNS、Headless）
- **Nacos**：服务注册、配置中心、健康检查、跨集群发现

两者同时使用导致：服务注册双写、发现结果不一致、配置分散。

## 解决方案

**分层发现**：
- **K8s Service** → 容器内发现：ClusterIP/DNS 自动注入，零配置
- **Nacos** → 跨集群/混合云发现：非 K8s 服务、配置中心、全局路由

**边界规则**：K8s 内服务不注册到 Nacos，Nacos 仅管理跨集群和非容器服务。

## 数据流图

```
┌─────────────────────────────────────────────────────┐
│                 K8s Cluster A                        │
│                                                      │
│  ┌──────┐    ┌───────────┐    ┌──────┐              │
│  │svc-A │───▶│ K8s DNS   │───▶│svc-B │              │
│  │      │    │ (自动发现) │    │      │              │
│  └──────┘    └───────────┘    └──────┘              │
│                      │                               │
└──────────────────────┼───────────────────────────────┘
                       │
                       ▼
              ┌────────────────┐
              │     Nacos      │
              │  (全局发现)     │
              │                │
              │  Cluster A svc │
              │  Cluster B svc │
              │  Non-K8s svc   │
              └────────┬───────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│                 K8s Cluster B                        │
│                                                      │
│  ┌──────┐    ┌───────────┐    ┌──────┐              │
│  │svc-C │───▶│ K8s DNS   │───▶│svc-D │              │
│  │      │    │ (自动发现) │    │      │              │
│  └──────┘    └───────────┘    └──────┘              │
│                                                      │
└─────────────────────────────────────────────────────┘
```

## 配置示例

### K8s 内服务（零配置自动发现）
```yaml
apiVersion: v1
kind: Service
metadata:
  name: order-service
spec:
  selector:
    app: order
  ports:
    - port: 8080
      targetPort: 8080
```

### Nacos 跨集群服务注册
```yaml
# 非 K8s 服务注册到 Nacos
nacos:
  discovery:
    server-addr: "nacos:8848"
    namespace: "production"
    services:
      - name: "legacy-db"
        ip: "192.168.1.100"
        port: 3306
        metadata:
          cluster: "on-premise"
          type: "mysql"
```

## 迁移路径

1. **Phase 1**：K8s 内服务保持原生 Service 发现（零改动）
2. **Phase 2**：部署 Nacos 做配置中心
3. **Phase 3**：非 K8s 服务注册到 Nacos
4. **Phase 4**：跨集群路由通过 Nacos 发现
5. **Phase 5**：K8s 内服务不注册 Nacos（避免双写）

## 验收标准

- [ ] K8s 内服务发现零配置（原生 DNS）
- [ ] 跨集群发现经 Nacos
- [ ] 非 K8s 服务注册到 Nacos
- [ ] 配置中心统一到 Nacos
- [ ] 无服务注册双写

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Nacos 单点 | 跨集群发现中断 | Nacos 集群部署 3 节点 |
| 发现结果不一致 | 路由错误 | 明确分层边界，禁止双写 |
| 配置迁移风险 | 服务启动失败 | 灰度迁移 + 回滚机制 |