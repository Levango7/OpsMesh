# 00 - 网关分工：Istio vs APISIX

## 冲突描述

Istio 的 IngressGateway 和 APISIX 都定位为南北向 API 网关，存在职责重叠：
- **Istio IngressGateway**：基于 Envoy，提供外部流量入口、TLS 终止、路由
- **APISIX**：基于 Nginx/OpenResty，提供 API 管理、限流、认证、插件生态

两者同时部署会导致：流量路径混乱、配置冲突、运维复杂度倍增。

## 解决方案

**职责分工**：
- **APISIX** → 南北向网关（外部流量入口）：API 路由、认证鉴权、限流熔断、WAF、灰度发布
- **Istio** → 东西向网格（服务间通信）：mTLS、重试、超时、熔断、流量拆分、可观测

**边界规则**：APISIX 是唯一的外部入口，Istio 不暴露 IngressGateway。

## 数据流图

```
                    ┌─────────────────────────────────────────────────┐
                    │                  Kubernetes Cluster               │
                    │                                                   │
  External          │  ┌──────────┐    ┌──────────────────────────┐   │
  Traffic ─────────┼─▶│  APISIX   │───▶│  Istio Sidecar (mTLS)    │   │
                    │  │ (北向)    │    │  ┌─────┐    ┌─────┐      │   │
                    │  │ 认证/限流  │    │  │svc-A│───▶│svc-B│      │   │
                    │  │ 路由/WAF  │    │  └─────┘    └─────┘      │   │
                    │  └──────────┘    └──────────────────────────┘   │
                    │                                                   │
                    └─────────────────────────────────────────────────┘
```

## 配置示例

### APISIX 路由配置（南北向）
```yaml
routes:
  - uri: /api/v1/*
    upstream:
      type: roundrobin
      nodes:
        "opsmesh-controlplane:8080": 1
    plugins:
      limit-req:
        rate: 100
        burst: 50
      jwt-auth: {}
      proxy-rewrite:
        regex_uri: ["^/api/v1/(.*)", "/$1"]
```

### Istio PeerAuthentication（东西向 mTLS）
```yaml
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: STRICT
```

## 迁移路径

1. **Phase 1**：部署 APISIX 作为唯一入口，移除 Istio IngressGateway
2. **Phase 2**：在服务间启用 Istio Sidecar 注入，开启 mTLS
3. **Phase 3**：逐步启用 Istio 流量管理能力（重试、超时、熔断）
4. **Phase 4**：验证南北向 100% 经 APISIX，东西向 mTLS 全覆盖

## 验收标准

- [ ] 南北向流量 100% 经过 APISIX
- [ ] Istio IngressGateway 未启用或已移除
- [ ] 东西向 mTLS 覆盖率 > 90%
- [ ] APISIX 插件（限流、认证）正常工作
- [ ] Istio 流量拆分（灰度）功能正常

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| APISIX 单点故障 | 外部流量全部中断 | APISIX 多副本部署 + 前置 LB |
| Istio Sidecar 资源开销 | 每 Pod +100MB 内存 | 对核心服务启用，边缘服务用 Ambient |
| 两套配置维护 | 运维复杂度增加 | 统一 GitOps 管理，Values 模板化 |