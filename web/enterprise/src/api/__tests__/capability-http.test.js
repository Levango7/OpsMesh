import { afterEach, describe, expect, it, vi } from 'vitest'

const { errorToast } = vi.hoisted(() => ({ errorToast: vi.fn() }))
vi.mock('@/utils/toast', () => ({ toast: { error: errorToast } }))

import { http } from '@/api/request'

const originalAdapter = http.defaults.adapter
afterEach(() => {
  http.defaults.adapter = originalAdapter
  errorToast.mockClear()
})

function rejectWith(status, message) {
  return async config => {
    const response = { status, statusText: '', headers: {}, config, data: { error: message } }
    throw { config, response }
  }
}

describe('503 能力错误的 toast 门控', () => {
  it('明确能力缺失由 PageShell 展示，不再重复红 toast', async () => {
    http.defaults.adapter = rejectWith(503, 'helm CLI not available')
    await expect(http.get('/helm/releases')).rejects.toMatchObject({ s: 503 })
    expect(errorToast).not.toHaveBeenCalled()
  })

  it('真实临时故障仍提示', async () => {
    http.defaults.adapter = rejectWith(503, 'authentication temporarily unavailable, retry shortly')
    await expect(http.get('/auth/login')).rejects.toMatchObject({ s: 503 })
    expect(errorToast).toHaveBeenCalledTimes(1)
  })

  it('相同文案但非 503 不误判为能力缺失', async () => {
    http.defaults.adapter = rejectWith(500, 'helm CLI not available')
    await expect(http.get('/helm/releases')).rejects.toMatchObject({ s: 500 })
    expect(errorToast).toHaveBeenCalledTimes(1)
  })
})
