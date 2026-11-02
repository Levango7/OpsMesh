// 云资源列表 store 单元测试（2026-11-02）
// 契约 mock 的形状从后端响应抄（server_lifecycle.go:431 → {resources, total, offset, limit}）。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/cloud-account', () => ({
  listCloudResources: vi.fn()
}))

import { useCloudResourcesStore } from '@/stores/cloud-resources'
import { listCloudResources } from '@/api/cloud-account'

describe('useCloudResourcesStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('拉取列表读 resources 字段', async () => {
    listCloudResources.mockResolvedValue({ resources: [{ id: 'r1', resourceName: 'ecs' }], total: 1 })
    const store = useCloudResourcesStore()
    await store.fetchResources()
    expect(store.resources).toHaveLength(1)
    expect(store.error).toBe('')
  })

  it('失败时写错误文案且列表清空', async () => {
    listCloudResources.mockRejectedValue({ j: { error: 'denied' } })
    const store = useCloudResourcesStore()
    await store.fetchResources()
    expect(store.resources).toEqual([])
    expect(store.error).toBe('denied')
  })

  it('setFilter 修改过滤条件', () => {
    const store = useCloudResourcesStore()
    store.setFilter('accountID', 'acc1')
    expect(store.filters.accountID).toBe('acc1')
  })

  it('过滤条件随请求一起下发', async () => {
    listCloudResources.mockResolvedValue({ resources: [] })
    const store = useCloudResourcesStore()
    store.setFilter('resourceType', 'ecs')
    await store.fetchResources()
    expect(listCloudResources).toHaveBeenCalledWith(expect.objectContaining({ resourceType: 'ecs' }))
  })
})
