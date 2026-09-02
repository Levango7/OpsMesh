// 控制面联邦相关 API
//
// Endpoint 契约（后端 internal/controlplane/federation.go，mux 已注册）：
//   - GET  /federation/peers     联邦 Peer 列表 → {peers: [PeerStatus]}
//   - POST /federation/forward/task  转发任务到指定 Peer → {taskID, peerURL, status}
//       body: {peerURL, taskType, command, deviceID, timeoutSec}
//   - GET  /federation/devices   跨 Peer 聚合设备视图 → {devices: [Device], peers: [{url, online, deviceCount}]}
//
// 响应结构：
//   PeerStatus {url, online, lastCheckAt, latencyMs}
//   转发响应   {taskID, peerURL, status: "forwarded|failed", error}
import { getJSON, postJSON } from './request'

// 获取联邦 Peer 列表
export const getPeers = () => getJSON('/federation/peers')

// 转发任务到指定 Peer
export const forwardTask = (body) => postJSON('/federation/forward/task', body)

// 跨 Peer 聚合设备视图
export const getFederationDevices = () => getJSON('/federation/devices')