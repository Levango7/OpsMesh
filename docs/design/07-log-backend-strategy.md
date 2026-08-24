# 07 - 日志后端分层：Loki vs Elasticsearch

## 冲突描述

Loki 和 Elasticsearch 都想做日志存储与查询：
- **Loki**：轻量、低成本、Label 索引、适合小规模
- **Elasticsearch**：全文检索、聚合分析、适合大规模

选择困难：小规模用 ES 太重，大规模用 Loki 查询慢。

## 解决方案

**按规模分层 + 适配层统一**：
- **小规模 (<1TB/天)** → Loki：低成本、简单运维
- **大规模 (>1TB/天)** → Elasticsearch：全文检索、聚合分析
- **OpsMesh logstore** → 适配层：统一查询接口，后端可切换

**边界规则**：logstore 是唯一查询入口，上层不直接查 Loki/ES。

## 数据流图

```
┌──────────┐    ┌───────────┐    ┌──────────────────────┐
│  Logs    │───▶│ Fluent Bit│───▶│  路由决策             │
│ (采集)   │    │ (转发)    │    │  按规模/租户路由      │
└──────────┘    └───────────┘    └──────────┬───────────┘
                                           │
                          ┌────────────────┴────────────────┐
                          ▼                                 ▼
                 ┌──────────────┐                 ┌──────────────────┐
                 │    Loki      │                 │ Elasticsearch    │
                 │  (小规模)    │                 │  (大规模)        │
                 │  Label 索引  │                 │  全文检索        │
                 └──────┬───────┘                 └────────┬─────────┘
                        │                                  │
                        └──────────┬───────────────────────┘
                                   ▼
                          ┌──────────────────┐
                          │ OpsMesh logstore │
                          │  (适配层)        │
                          │                  │
                          │  Query()        │
                          │  Search()       │
                          │  Tail()         │
                          └────────┬─────────┘
                                   │
                                   ▼
                          ┌──────────────────┐
                          │   Grafana / UI   │
                          └──────────────────┘
```

## 配置示例

### Fluent Bit 路由配置
```yaml
[OUTPUT]
    Name  loki
    Match small.*
    Host  loki.observability
    Port  3100
    Labels tenant=small, env=prod

[OUTPUT]
    Name  es
    Match large.*
    Host  elasticsearch.observability
    Port  9200
    Index large-logs
```

### OpsMesh logstore 适配层
```yaml
logstore:
  backend: "auto"  # auto|loki|es
  loki:
    url: "http://loki:3100"
  elasticsearch:
    url: "http://elasticsearch:9200"
    index: "logs-%{tenant}-%{date}"
  routing:
    - tenant: "small-corp"
      backend: "loki"
    - tenant: "large-corp"
      backend: "es"
```

## 迁移路径

1. **Phase 1**：默认部署 Loki（已有 logstore 适配）
2. **Phase 2**：logstore 实现统一查询接口
3. **Phase 3**：大规模租户切换到 ES
4. **Phase 4**：Fluent Bit 按租户路由
5. **Phase 5**：Grafana 统一查询面板

## 验收标准

- [ ] logstore 统一查询接口
- [ ] 后端可切换（loki/es）
- [ ] 小规模用 Loki，大规模用 ES
- [ ] Fluent Bit 路由正确
- [ ] Grafana 统一面板

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 后端切换数据迁移 | 历史日志丢失 | 双写过渡期 + 按需迁移 |
| ES 成本高 | 资源浪费 | 按需启用 + ILM 策略 |
| 查询语义差异 | 结果不一致 | logstore 适配层抹平差异 |