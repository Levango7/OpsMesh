// 可选能力未部署/未启用的明确错误，与真实临时故障分开呈现。
const capabilityMarkers = [
  'helm cli not available',
  'helm not initialized',
  'service unreachable',
  'not configured',
  'not deployed',
  'not enabled'
]

export function isCapabilityMessage(message) {
  const text = String(message || '').toLowerCase()
  return capabilityMarkers.some(marker => text.includes(marker))
}

export function isCapabilityError(status, payload) {
  return status === 503 && isCapabilityMessage(payload?.error)
}
