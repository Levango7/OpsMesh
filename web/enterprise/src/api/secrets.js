// 密钥管理 API（task 267）
// 契约：
//   GET  /api/v1/secrets/status → 200 {provider, enabled, addr, mount, file}
//   POST /api/v1/secrets/test   {addr, token, mount} → 200 {ok, latencyMs, error}
//   GET  /api/v1/secrets/keys   → 200 [{key, provider}]
//
// 安全约束：listSecretKeys 仅返回 key 名称与来源 provider，不返回密钥值；
//           status 不返回 Vault token（避免前端泄露）。
import { getJSON, postJSON } from './request'

// 获取密钥提供者状态：当前 provider 类型（env/file/vault/chain）、是否启用、Vault 地址等。
export function getSecretProviderStatus() {
  return getJSON('/secrets/status')
}

// 测试密钥提供者连接（通常用于 Vault）：传入 addr/token/mount，返回 ok/fail + 延迟。
export function testSecretProvider(config) {
  return postJSON('/secrets/test', config)
}

// 列出可用密钥 key 列表（仅名称 + 来源 provider，不返回值）。
export function listSecretKeys() {
  return getJSON('/secrets/keys')
}

// 兼容旧引用：聚合对象形式
export const secretsApi = {
  getSecretProviderStatus,
  testSecretProvider,
  listSecretKeys
}