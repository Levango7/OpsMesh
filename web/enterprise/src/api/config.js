// 配置热推送相关 API
//
// Endpoint 契约（后端 internal/controlplane/config_hotpush.go，mux 已注册）：
//   - POST /config/hotpush    配置热推送 → {taskID, configVersion}（201）
//       body: {agentID, key, value, path, format, description}
//   - POST /config/canary     配置灰度发布 → {canaryID, versions}
//       body: {agentIDs, key, value, path, format, strategy, percentage}
//   - GET  /config/versions   配置版本历史 → {versions: [ConfigVersion]}
//       query: key, agentID, limit
//
// 响应结构：
//   ConfigVersion {key, version, value, agentID, updatedAt, updatedBy}
//   热推送响应   {taskID, configVersion: ConfigVersion}
import { getJSON, postJSON } from './request'

// 配置热推送
export const hotpushConfig = (body) => postJSON('/config/hotpush', body)

// 配置灰度发布
export const canaryConfig = (body) => postJSON('/config/canary', body)

// 配置版本历史（支持按 key / agentID 过滤与 limit 限制）
export const getConfigVersions = (params) => getJSON('/config/versions', params)