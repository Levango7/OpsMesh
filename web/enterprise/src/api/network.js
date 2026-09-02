// 网络拓扑诊断相关 API
//
// Endpoint 契约（后端 internal/controlplane/network.go，mux 已注册）：
//   - GET  /network/topology              拓扑图 → NetworkTopology（?refresh=true）
//   - GET  /network/topology/cache        缓存拓扑 → NetworkTopology
//   - POST /network/diagnose              发起诊断 → {taskID, status}
//       body: {agentID, command: "ping|traceroute|tcping|nslookup|curl",
//              target, count, timeout}
//   - GET  /network/diagnose/{taskID}     诊断结果 → {taskID, status, output, finishedAt}
//   - POST /network/connectivity          批量连通性检测 → {results: [{source, target, reachable, latencyMs, loss}]}
//       body: {targets: [{source, target}], timeout}
//   - GET  /network/devices               网络设备列表 → {devices: [NetworkDevice]}
//   - POST /network/devices               创建网络设备 → NetworkDevice（201）
//   - GET  /network/devices/{id}          网络设备详情 → NetworkDevice
//   - DELETE /network/devices/{id}        删除网络设备 → {status: "deleted"}
//   - GET  /network/devices/{id}/metrics  设备指标 → {metrics: [{timestamp, bandwidth, throughput, errors}]}
//   - POST /network/devices/{id}/config   配置下发 → {taskID}
//       body: {config, format}
//   - POST /network/discover              网络发现 → {discovered: [NetworkDevice]}
//       body: {segment, agentID}
//
// 响应结构：
//   NetworkTopology {nodes: [NetworkNode], edges: [NetworkEdge], generatedAt, tenantID}
//   NetworkNode     {id, hostname, ip, status: "online|offline", os, segment}
//   NetworkEdge     {source, target, latencyMs, loss}
//   NetworkDevice   {id, name, type, ip, segment, vendor, model, status, createdAt}
import { getJSON, postJSON, deleteJSON } from './request'

// ---------- 拓扑 ----------

// 拓扑图（refresh=true 时强制刷新）
export const getNetworkTopology = (params) => getJSON('/network/topology', params)

// 缓存拓扑
export const getCachedTopology = () => getJSON('/network/topology/cache')

// ---------- 诊断 ----------

// 发起诊断任务
export const startDiagnose = (body) => postJSON('/network/diagnose', body)

// 查询诊断结果
export const getDiagnoseResult = (taskID) => getJSON(`/network/diagnose/${encodeURIComponent(taskID)}`)

// 批量连通性检测
export const checkConnectivity = (body) => postJSON('/network/connectivity', body)

// ---------- 网络设备 CRUD ----------

export const getNetworkDevices = () => getJSON('/network/devices')
export const createNetworkDevice = (body) => postJSON('/network/devices', body)
export const getNetworkDevice = (id) => getJSON(`/network/devices/${encodeURIComponent(id)}`)
export const deleteNetworkDevice = (id) => deleteJSON(`/network/devices/${encodeURIComponent(id)}`)

// 设备指标
export const getDeviceMetrics = (id) => getJSON(`/network/devices/${encodeURIComponent(id)}/metrics`)

// 配置下发
export const pushDeviceConfig = (id, body) => postJSON(`/network/devices/${encodeURIComponent(id)}/config`, body)

// ---------- 网络发现 ----------

export const discoverNetwork = (body) => postJSON('/network/discover', body)