// AI 提示词模板 store 单元测试（2026-11-02）
// 契约 mock 的形状必须从后端 handler 的真实响应抄（gateway/handler.go:368-378）：
// GET /ai/prompts → {templates: [...]}，不要手编 {items} 之类。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/ai', () => ({
  listPrompts: vi.fn(),
  createPrompt: vi.fn(),
  getPrompt: vi.fn(),
  updatePrompt: vi.fn(),
  deletePrompt: vi.fn()
}))

import { useAIPromptsStore } from '@/stores/ai-prompts'
import { listPrompts, createPrompt, updatePrompt, deletePrompt } from '@/api/ai'

describe('useAIPromptsStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('拉取列表读 templates 字段（不是 items）', async () => {
    listPrompts.mockResolvedValue({ templates: [{ id: 'p1', name: '默认' }] })
    const store = useAIPromptsStore()
    await store.fetchTemplates()
    expect(store.templates).toHaveLength(1)
    expect(store.error).toBe('')
  })

  it('拉取失败时写入错误文案，列表清空', async () => {
    listPrompts.mockRejectedValue({ j: { error: 'boom' } })
    const store = useAIPromptsStore()
    await store.fetchTemplates()
    expect(store.templates).toEqual([])
    expect(store.error).toBe('boom')
  })

  it('无 id 时走 create、有 id 走 update', async () => {
    createPrompt.mockResolvedValue({ id: 'p2' })
    updatePrompt.mockResolvedValue({ id: 'p1' })
    const store = useAIPromptsStore()
    await store.saveTemplate({ id: '', name: 'n', category: 'c', content: 'x' })
    expect(createPrompt).toHaveBeenCalledWith({ name: 'n', category: 'c', content: 'x' })
    await store.saveTemplate({ id: 'p1', name: 'n', category: 'c', content: 'x' })
    expect(updatePrompt).toHaveBeenCalledWith('p1', { name: 'n', category: 'c', content: 'x' })
  })

  it('保存后自动刷新列表', async () => {
    createPrompt.mockResolvedValue({ id: 'p2' })
    listPrompts.mockResolvedValue({ templates: [] })
    const store = useAIPromptsStore()
    await store.saveTemplate({ name: 'n', category: '', content: 'x' })
    expect(listPrompts).toHaveBeenCalled()
  })

  it('删除后自动刷新列表', async () => {
    deletePrompt.mockResolvedValue(undefined)
    listPrompts.mockResolvedValue({ templates: [] })
    const store = useAIPromptsStore()
    await store.removeTemplate('p1')
    expect(deletePrompt).toHaveBeenCalledWith('p1')
    expect(listPrompts).toHaveBeenCalled()
  })
})
