// RAG store 单元测试（2026-11-02）
// 契约 mock 形状从后端 handler 抄：search → {documents, count}；ingest → {ingested: N}。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/ai', () => ({
  ragSearch: vi.fn(),
  ragIngest: vi.fn(),
  getAIHealth: vi.fn()
}))

import { useAIRagStore } from '@/stores/ai-rag'
import { ragSearch, ragIngest, getAIHealth } from '@/api/ai'

describe('useAIRagStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('检索读 documents 字段', async () => {
    ragSearch.mockResolvedValue({ documents: [{ id: 'd1', content: 'abc', score: 0.9 }], count: 1 })
    const store = useAIRagStore()
    await store.search('abc')
    expect(store.results).toHaveLength(1)
    expect(store.searched).toBe(true)
  })

  it('空 query 不发请求', async () => {
    const store = useAIRagStore()
    await store.search('  ')
    expect(ragSearch).not.toHaveBeenCalled()
  })

  it('检索失败写错误文案', async () => {
    ragSearch.mockRejectedValue({ j: { error: 'denied' } })
    const store = useAIRagStore()
    await store.search('x')
    expect(store.results).toEqual([])
    expect(store.error).toBe('denied')
  })

  it('入库按行拆分并组装 documents', async () => {
    ragIngest.mockResolvedValue({ ingested: 2 })
    const store = useAIRagStore()
    const n = await store.ingest('第一行\n\n  第二行  ')
    expect(n).toBe(2)
    const body = ragIngest.mock.calls[0][0]
    expect(body.documents).toHaveLength(2)
    expect(body.documents[0].content).toBe('第一行')
    // embedding 由后端生成，UI 不应塞这个字段
    expect('embedding' in body.documents[0]).toBe(false)
  })

  it('入库可选 metadata 透传给每条文档，未传则维持空对象', async () => {
    ragIngest.mockResolvedValue({ ingested: 2 })
    const store = useAIRagStore()
    await store.ingest('a\nb', { team: 'a' })
    const withMeta = ragIngest.mock.calls[0][0]
    expect(withMeta.documents[0].metadata).toEqual({ team: 'a' })
    expect(withMeta.documents[1].metadata).toEqual({ team: 'a' })
    ragIngest.mockClear()
    await store.ingest('c')
    const withoutMeta = ragIngest.mock.calls[0][0]
    expect(withoutMeta.documents[0].metadata).toEqual({})
  })

  it('入库返回 0 时写错误文案', async () => {
    ragIngest.mockRejectedValue({ j: { error: 'quota' } })
    const store = useAIRagStore()
    const n = await store.ingest('x')
    expect(n).toBe(0)
    expect(store.error).toBe('quota')
  })

  it('健康读 providers 字段', async () => {
    getAIHealth.mockResolvedValue({ providers: [{ name: 'openai', ok: true }] })
    const store = useAIRagStore()
    await store.fetchHealth()
    expect(store.health).toHaveLength(1)
  })

  it('健康失败不阻塞检索，单独报错', async () => {
    getAIHealth.mockRejectedValue({ j: { error: 'boom' } })
    const store = useAIRagStore()
    await store.fetchHealth()
    expect(store.health).toEqual([])
    expect(store.healthError).toBe('boom')
  })
})
