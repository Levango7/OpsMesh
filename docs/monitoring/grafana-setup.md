# Grafana 本地监控配置指南

> 由于网络限制无法拉取 Grafana 镜像，提供本地 Windows 版 Grafana 配置方案。

## 方案 A：本地 Grafana Windows 版（推荐）

### 1. 下载 Grafana

```powershell
# 使用 winget 安装
winget install GrafanaLabs.Grafana

# 或手动下载
# https://grafana.com/grafana/download?platform=windows
```

### 2. 启动 Grafana

```powershell
# 默认端口 3000
& "C:\Program Files\GrafanaLabs\grafana\bin\grafana-server.exe"
# 访问 http://localhost:3000 (默认 admin/admin)
```

### 3. 配置数据源

打开 Grafana → Configuration → Data Sources → Add data source:

#### Prometheus
- Name: Prometheus
- URL: http://localhost:9092
- Access: Server (default)
→ Save & Test

#### Loki (可选，用于日志)
- Name: Loki
- URL: http://localhost:3100
→ Save & Test

### 4. 导入面板

Grafana → Dashboards → Import → Upload JSON:

```
deploy/monitoring/grafana/dashboards/opsmesh-overview.json
```

---

## 方案 B：Docker Grafana（网络恢复后）

```powershell
docker run -d --name opsmesh-grafana `
  -p 3000:3000 `
  -v grafana_data:/var/lib/grafana `
  grafana/grafana:latest
```

---

## 手动查看指标（无需 Grafana）

### Prometheus 文本格式

```powershell
Invoke-WebRequest -Uri "http://localhost:8080/metrics" | Select-Object -ExpandProperty Content
```

### 关键指标

| 指标 | 说明 |
|------|------|
| `http_requests_total` | API 请求计数 |
| `http_request_duration_seconds` | 请求延迟 |
| `active_connections` | 活跃连接数 |
| `opsmesh_device_total` | 设备总数 |
| `opsmesh_alert_firing` | 告警触发数 |

### 健康检查

```powershell
Invoke-WebRequest -Uri "http://localhost:8080/healthz" | Select-Object -ExpandProperty Content
```

---

## 监控架构

```
┌──────────────────────────────────────────────────┐
│                  Grafana UI                       │
│              (http://localhost:3000)              │
└──────────┬───────────────────────────┬───────────┘
           │                           │
┌──────────▼──────────┐    ┌───────────▼──────────┐
│     Prometheus       │    │        Loki           │
│   (scrape metrics)   │    │    (aggregate logs)   │
└──────────┬──────────┘    └───────────┬──────────┘
           │                          │
┌──────────▼──────────────────────────▼───────────┐
│              OpsMesh Services                     │
│  /metrics endpoint on each service                │
└─────────────────────────────────────────────────┘
```

---

## 告警规则

告警规则配置在 `deploy/monitoring/prometheus-alerts.yml`：

| 规则 | 条件 | 级别 |
|------|------|------|
| HighErrorRate | 5xx 错误率 > 5% | Critical |
| HighLatency | P99 延迟 > 5s | Warning |
| ServiceDown | 服务不可达 | Critical |
| HighCPU | CPU > 80% | Warning |
| HighMemory | 内存 > 85% | Warning |

---

## 下一步

1. 安装 Grafana Windows 版
2. 配置 Prometheus 数据源
3. 导入面板
4. 验证告警规则
5. 配置通知通道（企微/飞书/Slack）
