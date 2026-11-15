// 控制面联邦相关 API
//
// Endpoint 契约（后端 internal/controlplane/federation.go / federation_device.go）：
//   - GET  /federation/peers     联邦 Peer 列表 → 裸数组 [PeerStatus]
//       PeerStatus {url, online}（online 为即时 /healthz 探测结果）
//   - POST /federation/forward/task  转发任务到指定 Peer → 201 + 完整 Task（含 taskID/status）
//       body: {peerURL, task: {agentID, type, command, ...}}
//   - POST /federation/forward/device-op  设备域操作转发 → peer 响应 body/status 透传
//       body: {peerURL, op, payload}；op ∈ retire|provision|metrics|system|netsec|cmdb-discover
//   - GET  /federation/devices   跨 Peer 聚合设备视图 →
//       {data: [FederatedDevice], total, page, pageSize, hasMore, local: [DeviceInfo], peers: {url: {devices, error}}, search}
//   - GET  /federation/tasks/{taskID}?peerURL=  peer 上任务执行结果 JSON 透传
import { getJSON, postJSON } from './request'

// 获取联邦 Peer 列表
export const getPeers = () => getJSON('/federation/peers')

// 转发任务到指定 Peer
export const forwardTask = (body) => postJSON('/federation/forward/task', body)

// 跨 Peer 聚合设备视图（支持分页+搜索：?page=&pageSize=&search=）
export const getFederationDevices = (params) => getJSON('/federation/devices', params)

// 查询联邦任务在指定 Peer 的执行状态：?peerURL=
export const getFederationTaskStatus = (taskID, peerURL) => getJSON(`/federation/tasks/${encodeURIComponent(taskID)}`, { peerURL })