# 06 - 监控统一：Prometheus + Zabbix 桥接

## 冲突描述

Prometheus 和 Zabbix 都想做监控告警：
- **Prometheus**：云原生监控，Pull 模式，PromQL，适合容器/微服务
- **Zabbix**：传统监控，Push/SNMP，适合网络设备/物理机/OS

两者各自告警导致：告警重复、通知通道不一致、值班混乱。

## 解决方案

**分工 + 统一告警**：
- **Prometheus** → 云原生监控：K8s、容器、微服务、自定义指标
- **Zabbix** → 传统监控：网络设备、物理机、SNMP、IPMI
- **Alertmanager** → 统一告警：两者告警都路由到 Alertmanager
- **Zabbix Alert Adapter** → 桥接：Zabbix 告警转 Prometheus Alertmanager 格式

## 数据流图

```
┌────────────┐         ┌──────────────┐
│ K8s/容器   │         │ 网络设备/物理机│
│ /metrics   │         │ SNMP/IPMI    │
└──────┬─────┘         └──────┬───────┘
       │                      │
       ▼ (Pull)               ▼ (Push)
┌──────────────┐    ┌──────────────┐
│ Prometheus   │    │   Zabbix     │
│ (云原生指标) │    │ (传统指标)   │
│ PromQL       │    │ Zabbix Query │
└──────┬───────┘    └──────┬───────┘
       │                    │
       ▼                    ▼
┌──────────────┐    ┌──────────────────────┐
│ Alertmanager │◀───│ Zabbix Alert Adapter │
│ (统一告警)   │    │ (格式转换)           │
└──────┬───────┘    └──────────────────────┘
       │
       ▼
┌──────────────┐
│ Notification │
│ 邮件/Slack/  │
│ 企业微信     │
└──────────────┘
```

## 配置示例

### Prometheus 告警规则
```yaml
groups:
  - name: k8s-alerts
    rules:
      - alert: PodCrashLooping
        expr: rate(kube_pod_container_status_restarts_total[5m]) > 0
        for: 5m
        labels:
          severity: warning
          source: prometheus
        annotations:
          summary: "Pod {{ $labels.pod }} crash looping"
```

### Zabbix Alert Adapter
```yaml
adapter:
  zabbix_api: "http://zabbix:8080/api_jsonrpc.php"
  zabbix_user: "alert-adapter"
  zabbix_password: "<from-vault>"
  alertmanager: "http://alertmanager:9093/api/v2/alerts"
  poll_interval: 30s
  mapping:
    - zabbix_severity: 4  # High
      prometheus_severity: "critical"
    - zabbix_severity: 3  # Average
      prometheus_severity: "warning"
```

## 迁移路径

1. **Phase 1**：部署 Prometheus + Alertmanager（已有）
2. **Phase 2**：部署 Zabbix Alert Adapter
3. **Phase 3**：Zabbix 告警路由到 Adapter → Alertmanager
4. **Phase 4**：统一通知渠道（邮件/Slack/企业微信）
5. **Phase 5**：统一告警面板（Grafana Alert Panel）

## 验收标准

- [ ] 告警 100% 经 Alertmanager
- [ ] 云原生指标由 Prometheus 采集
- [ ] 传统指标由 Zabbix 采集
- [ ] 通知渠道统一
- [ ] 无重复告警

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Adapter 单点 | Zabbix 告警丢失 | Adapter 多副本 + 去重 |
| 告警风暴 | 通知轰炸 | Alertmanager 分组/抑制/静默 |
| 双栈运维 | 复杂度高 | 统一仪表盘 + 文档 |