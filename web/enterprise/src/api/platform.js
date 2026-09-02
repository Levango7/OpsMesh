// 平台配置相关 API
//
// Endpoint 契约（后端 internal/controlplane/platform.go，mux 已注册）：
//   - GET  /platform/config   读取平台配置 → PlatformConfig
//   - PUT  /platform/config   更新平台配置 → PlatformConfig
//       body: {defaultTenant, maxTenants, enableMarketplace, enableBilling}
//   - GET  /platform/health   平台健康检查 → PlatformHealth
//   - GET  /platform/metrics  平台指标汇总 → PlatformMetrics
//
// 响应结构：
//   PlatformConfig  {version, buildTime, goVersion, defaultTenant, maxTenants,
//                    enableMarketplace, enableBilling, updatedAt}
//   PlatformHealth  {status: "ok|degraded|down", components: {name: status}, timestamp}
//   PlatformMetrics {tenants, devices, tasks, alerts, apiKeys, plugins,
//                    subscriptions, invoices}
import { getJSON, putJSON } from './request'

// 读取平台配置
export const getPlatformConfig = () => getJSON('/platform/config')

// 更新平台配置
export const updatePlatformConfig = (body) => putJSON('/platform/config', body)

// 平台健康检查
export const getPlatformHealth = () => getJSON('/platform/health')

// 平台指标汇总
export const getPlatformMetrics = () => getJSON('/platform/metrics')