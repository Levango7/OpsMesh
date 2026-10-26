// 密钥管理 API
import { getJSON, postJSON } from './request'

export const getSecretProviderStatus = () => getJSON('/secrets/status')
export const testSecretProvider = (body) => postJSON('/secrets/test', body)
export const listSecretKeys = () => getJSON('/secrets/keys')

// 轮转密钥（P1-5B：版本递增 + 访问审计）
// 契约：POST /api/v1/secrets/{key}/rotate  body {keyType, newValue, rotatedBy}
//   → 200 {meta: {key, keyType, version, tenantID, createdAt, updatedAt}}
export const rotateSecret = (key, body) => postJSON(`/secrets/${encodeURIComponent(key)}/rotate`, body)

export const secretsApi = {
  getSecretProviderStatus,
  testSecretProvider,
  listSecretKeys,
  rotateSecret
}
