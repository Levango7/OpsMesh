// Pagination 组件单元测试
// 覆盖：组件渲染、页码显示（info）、翻页事件（prev/next）、按钮禁用状态。
// 组件模板用全局 $t（按钮文本），info computed 用 import { t } from '@/i18n'；
// 测试中 mock @/i18n 的 t，并通过 global.mocks.$t 注入模板用的 $t。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// vi.mock 是 hoisted 的，工厂函数内不能引用外部变量。
// 用 vi.hoisted 把 mockT 提升到与 vi.mock 同一作用域，避免 "Cannot access before initialization"。
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
  default: { t: mockT },
}))

import Pagination from '@/components/Pagination.vue'

// mount helper：统一注入 $t mock，避免每个测试重复
function mountP(props = {}) {
  return mount(Pagination, { props, global: { mocks: { $t: mockT } } })
}

describe('Pagination 组件', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 重新设置 mockT 默认实现（clearAllMocks 只清调用记录，不清实现）
    mockT.mockImplementation((key, params) => {
      if (key === 'common.page') return `第 ${params.n} 页（本页 ${params.m} 条）`
      if (key === 'common.prev') return '‹ 上一页'
      if (key === 'common.next') return '下一页 ›'
      return key
    })
  })

  describe('组件渲染', () => {
    it('挂载成功，包含 .pager 容器', () => {
      const wrapper = mountP({ page: 1, pageSize: 10, limit: 200 })
      expect(wrapper.find('.pager').exists()).toBe(true)
    })

    it('包含 info 区域和按钮组', () => {
      const wrapper = mountP({ page: 1, pageSize: 10, limit: 200 })
      expect(wrapper.find('.info').exists()).toBe(true)
      expect(wrapper.find('.btns').exists()).toBe(true)
      expect(wrapper.findAll('button')).toHaveLength(2)
    })

    it('渲染上一页/下一页按钮文本', () => {
      const wrapper = mountP({ page: 1, pageSize: 10, limit: 200 })
      const btns = wrapper.findAll('button')
      expect(btns[0].text()).toBe('‹ 上一页')
      expect(btns[1].text()).toBe('下一页 ›')
    })
  })

  describe('页码显示（info）', () => {
    it('显示当前页码和条数', () => {
      const wrapper = mountP({ page: 3, pageSize: 20, limit: 200 })
      expect(wrapper.find('.info').text()).toBe('第 3 页（本页 20 条）')
    })

    it('page=1, pageSize=0 时显示初始状态', () => {
      const wrapper = mountP({ page: 1, pageSize: 0, limit: 200 })
      expect(wrapper.find('.info').text()).toBe('第 1 页（本页 0 条）')
    })

    it('切换 page prop 后 info 更新', async () => {
      const wrapper = mountP({ page: 1, pageSize: 10, limit: 200 })
      expect(wrapper.find('.info').text()).toBe('第 1 页（本页 10 条）')

      await wrapper.setProps({ page: 5, pageSize: 15 })
      expect(wrapper.find('.info').text()).toBe('第 5 页（本页 15 条）')
    })
  })

  describe('翻页事件', () => {
    it('点击上一页按钮触发 prev 事件', async () => {
      const wrapper = mountP({ page: 2, pageSize: 10, limit: 200 })
      const prevBtn = wrapper.findAll('button')[0]
      await prevBtn.trigger('click')
      expect(wrapper.emitted('prev')).toBeTruthy()
      expect(wrapper.emitted('prev')).toHaveLength(1)
    })

    it('点击下一页按钮触发 next 事件', async () => {
      const wrapper = mountP({ page: 1, pageSize: 200, limit: 200 })
      const nextBtn = wrapper.findAll('button')[1]
      await nextBtn.trigger('click')
      expect(wrapper.emitted('next')).toBeTruthy()
      expect(wrapper.emitted('next')).toHaveLength(1)
    })

    it('多次点击累计触发事件', async () => {
      const wrapper = mountP({ page: 2, pageSize: 10, limit: 200 })
      const prevBtn = wrapper.findAll('button')[0]
      await prevBtn.trigger('click')
      await prevBtn.trigger('click')
      expect(wrapper.emitted('prev')).toHaveLength(2)
    })
  })

  describe('按钮禁用状态', () => {
    it('page=1 时上一页按钮禁用', () => {
      const wrapper = mountP({ page: 1, pageSize: 10, limit: 200 })
      const prevBtn = wrapper.findAll('button')[0]
      expect(prevBtn.attributes('disabled')).toBeDefined()
    })

    it('page>1 时上一页按钮可用', () => {
      const wrapper = mountP({ page: 2, pageSize: 10, limit: 200 })
      const prevBtn = wrapper.findAll('button')[0]
      expect(prevBtn.attributes('disabled')).toBeUndefined()
    })

    it('pageSize < limit 时下一页按钮禁用', () => {
      const wrapper = mountP({ page: 1, pageSize: 10, limit: 200 })
      const nextBtn = wrapper.findAll('button')[1]
      expect(nextBtn.attributes('disabled')).toBeDefined()
    })

    it('pageSize >= limit 时下一页按钮可用', () => {
      const wrapper = mountP({ page: 1, pageSize: 200, limit: 200 })
      const nextBtn = wrapper.findAll('button')[1]
      expect(nextBtn.attributes('disabled')).toBeUndefined()
    })

    it('pageSize > limit 时下一页按钮也可用', () => {
      const wrapper = mountP({ page: 1, pageSize: 250, limit: 200 })
      const nextBtn = wrapper.findAll('button')[1]
      expect(nextBtn.attributes('disabled')).toBeUndefined()
    })

    it('默认 props（page=1, pageSize=0, limit=200）两个按钮均禁用', () => {
      const wrapper = mountP({})
      const [prevBtn, nextBtn] = wrapper.findAll('button')
      expect(prevBtn.attributes('disabled')).toBeDefined()
      expect(nextBtn.attributes('disabled')).toBeDefined()
    })
  })

  describe('props 默认值', () => {
    it('page 默认为 1', () => {
      const wrapper = mountP({ pageSize: 10 })
      expect(wrapper.find('.info').text()).toContain('第 1 页')
    })

    it('pageSize 默认为 0', () => {
      const wrapper = mountP({ page: 1 })
      expect(wrapper.find('.info').text()).toContain('本页 0 条')
    })

    it('limit 默认为 200', () => {
      const wrapper = mountP({ page: 1, pageSize: 200 })
      // pageSize=200 >= limit=200 → next 可用
      const nextBtn = wrapper.findAll('button')[1]
      expect(nextBtn.attributes('disabled')).toBeUndefined()
    })
  })
})
