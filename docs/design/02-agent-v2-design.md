# 02 - Agent v2.0 设计：多 Agent 合并

## 冲突描述

运维平台上通常需要部署多个 Agent：
- **Prometheus Node Exporter**：系统指标采集（CPU/内存/磁盘/网络）
- **Fluent Bit**：日志采集与转发
- **OpsMesh Agent**：任务执行、配置下发、心跳上报

三者在同一主机上运行导致：资源浪费（3 个进程）、配置分散、版本管理复杂。

## 解决方案

**合并到 OpsMesh Agent v2.0**：
- **指标采集**：内置 gopsutil 采集系统指标（替代 Node Exporter）
- **日志采集**：内置 LogCollector（Fluent Bit 嵌入模式/原生采集）
- **任务执行**：保留原有任务执行引擎
- **统一上报**：通过 gRPC 单通道上报控制面

**设计原则**：单进程、单配置、单上报通道、资源占用 < 100MB。

## 数据流图

```
┌──────────────────────────────────────────────────┐
│                OpsMesh Agent v2.0                 │
│                                                   │
│  ┌─────────┐  ┌──────────┐  ┌─────────────────┐ │
│  │ Metrics │  │   Logs   │  │     Tasks       │ │
│  │ Collect │  │ Collect  │  │    Execute      │ │
│  │ (gopsutil)│ │ (LogColl)│  │  (os/exec)      │ │
│  └────┬────┘  └────┬─────┘  └───────┬─────────┘ │
│       │            │                 │           │
│       ▼            ▼                 ▼           │
│  ┌──────────────────────────────────────────┐   │
│  │          gRPC Channel (9090)              │   │
│  │    指标上报 | 日志推送 | 任务结果上报     │   │
│  └──────────────────┬───────────────────────┘   │
│                     │                            │
└─────────────────────┼────────────────────────────┘
                      ▼
              ┌──────────────┐
              │  ControlPlane │
              └──────────────┘
```

## 配置示例

### Agent v2.0 配置（opsmesh-agent.yaml）
```yaml
agent:
  id: "agent-prod-01"
  server: "grpc://controlplane:9090"
  
  # 指标采集（替代 Node Exporter）
  metrics:
    enabled: true
    interval: 30s
    collect:
      - cpu
      - memory
      - disk
      - network
      - process
  
  # 日志采集（替代 Fluent Bit）
  logs:
    enabled: true
    interval: 10s
    paths:
      - "/var/log/*.log"
      - "/var/log/syslog"
    multiline: "^\d{4}-\d{2}-\d{2}"  # ISO 日期开头的行
    include: ["ERROR", "WARN", "FATAL"]
    exclude: ["DEBUG", "TRACE"]
    rate_limit: 1000  # 行/秒
  
  # 任务执行
  tasks:
    workers: 4
    timeout: 300s
    shell_whitelist:
      - "/bin/bash"
      - "/usr/bin/ansible"
```

## 迁移路径

1. **Phase 1**：Agent v2.0 实现指标采集（已有 metrics_collect.go）
2. **Phase 2**：Agent v2.0 实现日志采集（新增 log_collect.go）
3. **Phase 3**：配置文件支持 v2 格式（向后兼容 v1）
4. **Phase 4**：逐步替换 Node Exporter 和 Fluent Bit
5. **Phase 5**：验证单 Agent 资源占用 < 100MB

## 验收标准

- [ ] 单 Agent 覆盖指标 + 日志 + 任务三通道
- [ ] 内存占用 < 100MB
- [ ] CPU 占用 < 5%（空闲时 < 1%）
- [ ] v1 配置文件向后兼容
- [ ] Node Exporter 和 Fluent Bit 可移除
- [ ] gRPC 单通道上报正常

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 单进程故障导致全断 | 指标+日志+任务同时不可用 | Agent 自动重启 + 健康检查 |
| 日志采集风暴 | 内存/CPU 飙升 | 速率限制 + 背压机制 |
| 指标精度降低 | 不如 Node Exporter 专业 | 对标 Node Exporter 指标定义 |