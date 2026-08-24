# 08 - Istio 数据面：Sidecar vs Ambient Mesh

## 冲突描述

Istio 1.22+ 支持两种数据面模式：
- **Sidecar 模式**：每 Pod 注入 Envoy Sidecar，功能全（重试/熔断/流量拆分）
- **Ambient 模式**：节点级 ztunnel，轻量（mTLS/基本路由），无 Sidecar

两者选择困难：Sidecar 资源开销大，Ambient 功能受限。

## 解决方案

**混合模式**：
- **默认 Ambient**：核心服务用 Ambient（轻量 mTLS，无 Sidecar 开销）
- **按需 Sidecar**：需要高级流量管控的服务启用 Sidecar（重试/熔断/流量拆分）

**边界规则**：新服务默认 Ambient，显式启用 Sidecar 用于高级场景。

## 数据流图

```
┌──────────────────────────────────────────────────────────┐
│                    K8s Node                               │
│                                                           │
│  ┌─────────────────┐     ┌─────────────────────────┐    │
│  │  Ambient 模式   │     │  Sidecar 模式           │    │
│  │                 │     │                         │    │
│  │  ┌────┐         │     │  ┌────┐  ┌──────┐      │    │
│  │  │svc │─────────┼─────│  │svc │──│Envoy │      │    │
│  │  │ A  │         │     │  │ B  │  │Sidecar│     │    │
│  │  └────┘         │     │  └────┘  └──────┘      │    │
│  │    │            │     │    │        │           │    │
│  │    ▼            │     │    ▼        ▼           │    │
│  │  ┌────┐         │     │  重试/熔断/流量拆分     │    │
│  │  │ztun│         │     │                         │    │
│  │  │nel │         │     │                         │    │
│  │  └────┘         │     │                         │    │
│  │  (mTLS only)    │     │  (full features)        │    │
│  └─────────────────┘     └─────────────────────────┘    │
│                                                           │
└──────────────────────────────────────────────────────────┘
```

## 配置示例

### Ambient 模式启用（默认）
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: default
  labels:
    istio.io/dataplane-mode: ambient  # 启用 Ambient
```

### 按需启用 Sidecar（高级流量管控）
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: critical-services
  labels:
    istio.io/dataplane-mode: ambient  # 保持 Ambient 基础
    istio.io/use-waypoint: "true"     # 启用 Waypoint（L7）
---
# 需要完整 Sidecar 的服务
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-service
spec:
  template:
    metadata:
      annotations:
        sidecar.istio.io/inject: "true"  # 显式注入 Sidecar
```

## 迁移路径

1. **Phase 1**：新集群启用 Ambient 模式
2. **Phase 2**：核心服务 mTLS via ztunnel
3. **Phase 3**：需要 L7 流量管控的服务启用 Waypoint
4. **Phase 4**：少数服务显式注入 Sidecar（重试/熔断）
5. **Phase 5**：验证混合模式正常工作

## 验收标准

- [ ] 新服务默认 Ambient
- [ ] mTLS 全覆盖（ztunnel）
- [ ] Sidecar 按需启用
- [ ] 资源开销降低（vs 全 Sidecar）
- [ ] 高级流量管控可用

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Ambient 功能受限 | 缺少 L7 能力 | Waypoint proxy 补充 L7 |
| 混合模式复杂 | 运维难度 | 明确启用标准 + 文档 |
| Ambient 成熟度 | 生产风险 | 先在非核心服务试点 |