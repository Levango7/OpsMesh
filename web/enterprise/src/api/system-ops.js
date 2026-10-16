// 系统运维操作 API — 用户/组/防火墙的结构化查询 + shell 模板执行。
//
// 设计：查询走结构化 API（返回 JSON 供表格渲染），执行走 shell 模板下发。
// 契约（internal/controlplane/system_ops.go）：
//   - GET    /api/v1/devices/{id}/system   → SystemInventory {users, groups, firewall, firewallBackend}
//   - POST   /api/v1/system/users          {agentID, username, group, home, shell} → Task
//   - DELETE /api/v1/system/users          {agentID, username} → Task
//   - POST   /api/v1/system/groups         {agentID, groupName} → Task
//   - DELETE /api/v1/system/groups         {agentID, groupName} → Task
//   - POST   /api/v1/system/firewall       {agentID, port, protocol, source} → Task
//   - DELETE /api/v1/system/firewall       {agentID, port, protocol, source} → Task
import { getJSON, postJSON, deleteJSONBody } from './request'

// 结构化查询设备系统清单（用户/组/防火墙）
export const getDeviceSystemInfo = (deviceID) => getJSON(`/devices/${encodeURIComponent(deviceID)}/system`)

// 用户管理
export const createSystemUser = (body) => postJSON('/system/users', body)
export const deleteSystemUser = (body) => deleteJSONBody('/system/users', body)

// 用户组管理
export const createSystemGroup = (body) => postJSON('/system/groups', body)
export const deleteSystemGroup = (body) => deleteJSONBody('/system/groups', body)

// 防火墙管理
export const addFirewallRule = (body) => postJSON('/system/firewall', body)
export const deleteFirewallRule = (body) => deleteJSONBody('/system/firewall', body)
