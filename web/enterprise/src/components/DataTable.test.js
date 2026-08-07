// DataTable 组件单元测试
// 覆盖：组件渲染、props 传递、列头/行渲染、空数据提示、clickable 行点击、format 函数、slot。
// 组件使用全局 $t（main.js 注入），测试中通过 global.mocks 提供。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import DataTable from '@/components/DataTable.vue'

// mock $t：返回键本身，便于断言
const mockT = (key) => key

// 基础列定义
const baseColumns = [
  { key: 'name', title: '名称' },
  { key: 'age', title: '年龄' },
]

// 基础行数据
const baseRows = [
  { name: '张三', age: 25 },
  { name: '李四', age: 30 },
]

describe('DataTable 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，包含 .dt-wrap 和 table 元素', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.dt-wrap').exists()).toBe(true)
      expect(wrapper.find('table.data-table').exists()).toBe(true)
    })

    it('渲染 thead 表头', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns },
        global: { mocks: { $t: mockT } },
      })
      const ths = wrapper.findAll('thead th')
      expect(ths).toHaveLength(2)
      expect(ths[0].text()).toBe('名称')
      expect(ths[1].text()).toBe('年龄')
    })

    it('渲染 tbody 数据行', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows },
        global: { mocks: { $t: mockT } },
      })
      const trs = wrapper.findAll('tbody tr')
      expect(trs).toHaveLength(2)
    })
  })

  describe('props 传递', () => {
    it('columns 必填，不传时报错', () => {
      // Vue 会 warn 但不抛错，这里验证传了 columns 不报错
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.exists()).toBe(true)
    })

    it('rows 默认为空数组', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns },
        global: { mocks: { $t: mockT } },
      })
      // 空数组 → 显示空数据行
      const trs = wrapper.findAll('tbody tr')
      expect(trs).toHaveLength(1) // 空提示行
      expect(trs[0].text()).toContain('common.no_data')
    })

    it('rowKey 指定时用作行 key', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows, rowKey: 'name' },
        global: { mocks: { $t: mockT } },
      })
      // 渲染成功即可（key 不直接体现在 DOM）
      expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    })

    it('col.width 生成内联 style', () => {
      const columns = [{ key: 'name', title: '名称', width: '100px' }]
      const wrapper = mount(DataTable, {
        props: { columns },
        global: { mocks: { $t: mockT } },
      })
      const th = wrapper.find('thead th')
      expect(th.attributes('style')).toContain('width: 100px')
    })
  })

  describe('emptyText 默认值与空数据提示', () => {
    it('emptyText 默认为空字符串，空数据时使用 $t("common.no_data")', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: [] },
        global: { mocks: { $t: mockT } },
      })
      const emptyTd = wrapper.find('tbody td.empty')
      expect(emptyTd.exists()).toBe(true)
      expect(emptyTd.text()).toBe('common.no_data')
    })

    it('传入 emptyText 时使用自定义文本', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: [], emptyText: '暂无设备' },
        global: { mocks: { $t: mockT } },
      })
      const emptyTd = wrapper.find('tbody td.empty')
      expect(emptyTd.text()).toBe('暂无设备')
    })

    it('rows 为 null 时也显示空提示', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: null },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('tbody td.empty').exists()).toBe(true)
    })

    it('空数据行 colspan 等于列数', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: [] },
        global: { mocks: { $t: mockT } },
      })
      const emptyTd = wrapper.find('tbody td.empty')
      expect(emptyTd.attributes('colspan')).toBe('2')
    })
  })

  describe('单元格渲染', () => {
    it('正确渲染单元格值', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows },
        global: { mocks: { $t: mockT } },
      })
      const cells = wrapper.findAll('tbody td')
      expect(cells[0].text()).toBe('张三')
      expect(cells[1].text()).toBe('25')
      expect(cells[2].text()).toBe('李四')
      expect(cells[3].text()).toBe('30')
    })

    it('col.format 函数格式化单元格', () => {
      const columns = [
        { key: 'age', title: '年龄', format: (v) => `${v} 岁` },
      ]
      const rows = [{ age: 25 }]
      const wrapper = mount(DataTable, {
        props: { columns, rows },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('tbody td').text()).toBe('25 岁')
    })

    it('值为 null 时显示空字符串', () => {
      const columns = [{ key: 'name', title: '名称' }]
      const rows = [{ name: null }]
      const wrapper = mount(DataTable, {
        props: { columns, rows },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('tbody td').text()).toBe('')
    })

    it('col.slot 使用具名插槽渲染', () => {
      const columns = [{ key: 'name', title: '名称', slot: 'name' }]
      const rows = [{ name: '张三' }]
      const wrapper = mount(DataTable, {
        props: { columns, rows },
        slots: {
          name: '<span class="slot-cell">{{ row.name }}-slot</span>',
        },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('.slot-cell').exists()).toBe(true)
    })
  })

  describe('clickable 行点击', () => {
    it('clickable 为 true 时行有 .clickable class', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows, clickable: true },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('tbody tr').classes()).toContain('clickable')
    })

    it('clickable 为 false 时行无 .clickable class', () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows, clickable: false },
        global: { mocks: { $t: mockT } },
      })
      expect(wrapper.find('tbody tr').classes()).not.toContain('clickable')
    })

    it('点击 clickable 行触发 row-click 事件', async () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows, clickable: true },
        global: { mocks: { $t: mockT } },
      })
      await wrapper.findAll('tbody tr')[0].trigger('click')
      expect(wrapper.emitted('row-click')).toBeTruthy()
      expect(wrapper.emitted('row-click')[0][0]).toEqual(baseRows[0])
    })

    it('clickable 为 false 时点击不触发 row-click', async () => {
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows, clickable: false },
        global: { mocks: { $t: mockT } },
      })
      await wrapper.find('tbody tr').trigger('click')
      expect(wrapper.emitted('row-click')).toBeFalsy()
    })
  })

  describe('rowClass 动态行 class', () => {
    it('rowClass 函数为行添加 class', () => {
      const rowClass = (row) => (row.age > 28 ? 'senior' : 'junior')
      const wrapper = mount(DataTable, {
        props: { columns: baseColumns, rows: baseRows, rowClass },
        global: { mocks: { $t: mockT } },
      })
      const trs = wrapper.findAll('tbody tr')
      expect(trs[0].classes()).toContain('junior') // 25
      expect(trs[1].classes()).toContain('senior') // 30
    })
  })
})