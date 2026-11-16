// Pagination 服务端分页边界测试（ADR-2026-11-15 §ADR-3）
//
// 覆盖：
//   - total 判定：page * limit == total 时无下一页；< total 时有下一页
//   - total 为 0 / 负数时回退 pageSize >= limit 满页启发式
//   - 字符串 total / page 的防御性数字收敛
//   - 非法 page（负数/非数字）收敛为 1，上一页按钮禁用
//
// mock 策略与 src/components/Pagination.test.js 保持一致：
//   - @/i18n 的 t 用 vi.fn（info computed 直接调用）
//   - 模板里的 $t 通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const { mockT } = vi.hoisted(() => {
  const mockT = vi.fn((key, params) => {
    if (key === 'common.page') return `第 ${params.n} 页（本页 ${params.m} 条）`
    if (key === 'common.prev') return '‹ 上一页'
    if (key === 'common.next') return '下一页 ›'
    return key
  })
  return { mockT }
})

vi.mock('@/i18n', () => ({
  t: mockT,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: { t: mockT }
}))

import Pagination from '@/components/Pagination.vue'

function mountP(props = {}) {
  return mount(Pagination, { props, global: { mocks: { $t: mockT } } })
}

function nextDisabled(wrapper) {
  return wrapper.findAll('button')[1].attributes('disabled') !== undefined
}

describe('Pagination 服务端 total 边界', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockT.mockImplementation((key, params) => {
      if (key === 'common.page') return `第 ${params.n} 页（本页 ${params.m} 条）`
      if (key === 'common.prev') return '‹ 上一页'
      if (key === 'common.next') return '下一页 ›'
      return key
    })
  })

  it('page * limit == total 时下一页禁用（末页恰好满页）', () => {
    const wrapper = mountP({ page: 2, pageSize: 20, limit: 20, total: 40 })
    expect(nextDisabled(wrapper)).toBe(true)
  })

  it('page * limit < total 时下一页可用', () => {
    const wrapper = mountP({ page: 2, pageSize: 20, limit: 20, total: 41 })
    expect(nextDisabled(wrapper)).toBe(false)
  })

  it('page * limit > total（越界页）时下一页禁用', () => {
    const wrapper = mountP({ page: 3, pageSize: 5, limit: 20, total: 45 })
    expect(nextDisabled(wrapper)).toBe(true)
  })

  it('total 为负数时回退 pageSize 启发式：满页可翻页', () => {
    const wrapper = mountP({ page: 1, pageSize: 20, limit: 20, total: -5 })
    expect(nextDisabled(wrapper)).toBe(false)
  })

  it('total 为负数时回退 pageSize 启发式：不满页禁用', () => {
    const wrapper = mountP({ page: 1, pageSize: 19, limit: 20, total: -1 })
    expect(nextDisabled(wrapper)).toBe(true)
  })

  it('total 为 0 时回退 pageSize 启发式', () => {
    expect(nextDisabled(mountP({ page: 1, pageSize: 20, limit: 20, total: 0 }))).toBe(false)
    expect(nextDisabled(mountP({ page: 1, pageSize: 3, limit: 20, total: 0 }))).toBe(true)
  })

  it('字符串 total/page 被数字收敛后参与判定', () => {
    const wrapper = mountP({ page: '2', pageSize: 20, limit: 20, total: '40' })
    expect(nextDisabled(wrapper)).toBe(true)
    expect(wrapper.find('.info').text()).toBe('第 2 页（本页 20 条）')
  })

  it('非法 page（负数/非数字）收敛为 1，上一页禁用', () => {
    for (const page of [-3, 'abc', null]) {
      const wrapper = mountP({ page, pageSize: 20, limit: 20, total: 40 })
      const prevBtn = wrapper.findAll('button')[0]
      expect(prevBtn.attributes('disabled')).toBeDefined()
      expect(wrapper.find('.info').text()).toContain('第 1 页')
    }
  })
})
