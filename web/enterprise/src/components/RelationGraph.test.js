// RelationGraph 组件单元测试
// 覆盖：组件渲染、节点/边提取、CI 类型颜色映射、关系类型线型映射、
//       布局模式切换（force/topology）、交互（点击/hover emit）、
//       工具栏（缩放/重置/模式）、图例、节点详情面板、空状态。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import RelationGraph from '@/components/RelationGraph.vue'

// === 测试数据：模拟后端 GetCIRelationGraph 返回结构 ===
// centerCI 为 machine，4 条关系涉及 os/service/app/cluster 四种类型。
const sampleGraph = {
  centerCI: {
    id: 'ci-host-1', ciType: 'machine', name: 'web-host-1',
    status: 'active', source: 'agent', version: 3,
    attrs: { env: 'prod', region: 'cn-east' }
  },
  relations: [
    { id: 1, sourceCIID: 'ci-host-1', targetCIID: 'ci-os-1', relationType: 'runs_on', sourceName: 'web-host-1', targetName: 'ubuntu-22.04', targetType: 'os' },
    { id: 2, sourceCIID: 'ci-host-1', targetCIID: 'ci-svc-1', relationType: 'runs_on', sourceName: 'web-host-1', targetName: 'nginx', targetType: 'service' },
    { id: 3, sourceCIID: 'ci-app-1', targetCIID: 'ci-host-1', relationType: 'depends', sourceName: 'web-app', targetName: 'web-host-1', targetType: 'machine' },
    { id: 4, sourceCIID: 'ci-cluster-1', targetCIID: 'ci-host-1', relationType: 'contains', sourceName: 'prod-cluster', targetName: 'web-host-1', targetType: 'machine' }
  ]
}

// 仅中心节点、无关系
const centerOnlyGraph = {
  centerCI: { id: 'ci-1', ciType: 'machine', name: 'host-1', status: 'active', source: 'manual', version: 1, attrs: {} },
  relations: []
}

// 空图谱
const emptyGraph = { centerCI: null, relations: [] }

// mock $t：返回键本身，便于断言（组件使用全局 $t，main.js 注入，测试通过 global.mocks 提供）
const mockT = (key) => key

// 挂载辅助：挂载后等待 onMounted 内 nextTick 布局计算完成
async function mountGraph(props = {}) {
  const wrapper = mount(RelationGraph, {
    props: { graph: sampleGraph, ...props },
    global: { mocks: { $t: mockT } },
  })
  await nextTick()
  return wrapper
}

describe('RelationGraph 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，包含 .rg-wrap 容器', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.find('.rg-wrap').exists()).toBe(true)
    })

    it('渲染 svg 画布', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.find('svg.rg-svg').exists()).toBe(true)
    })

    it('渲染工具栏 .rg-toolbar', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.find('.rg-toolbar').exists()).toBe(true)
    })

    it('渲染图例 .rg-legend', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.find('.rg-legend').exists()).toBe(true)
    })

    it('width/height prop 传递到容器与 svg', async () => {
      const wrapper = await mountGraph({ width: 600, height: 500 })
      const wrap = wrapper.find('.rg-wrap')
      expect(wrap.attributes('style')).toContain('width: 600px')
      expect(wrap.attributes('style')).toContain('height: 500px')
      expect(wrapper.find('svg.rg-svg').attributes('width')).toBe('600')
      expect(wrapper.find('svg.rg-svg').attributes('height')).toBe('500')
    })

    it('默认 width=520 height=420', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.find('svg.rg-svg').attributes('width')).toBe('520')
      expect(wrapper.find('svg.rg-svg').attributes('height')).toBe('420')
    })
  })

  describe('节点提取', () => {
    it('从 centerCI + relations 提取所有唯一节点（5 个）', async () => {
      const wrapper = await mountGraph()
      const nodeLabels = wrapper.findAll('.rg-node-label').map(el => el.text())
      // ci-host-1, ci-os-1(ubuntu), ci-svc-1(nginx), ci-app-1(web-app), ci-cluster-1(prod-cluster)
      expect(nodeLabels).toHaveLength(5)
      expect(nodeLabels).toContain('web-host-1')
      expect(nodeLabels).toContain('ubuntu-22.04')
      expect(nodeLabels).toContain('nginx')
      expect(nodeLabels).toContain('web-app')
      expect(nodeLabels).toContain('prod-cluster')
    })

    it('中心节点标记为 .center 且半径更大', async () => {
      const wrapper = await mountGraph()
      const centerNode = wrapper.find('.rg-node.center')
      expect(centerNode.exists()).toBe(true)
      const circle = centerNode.find('circle')
      expect(circle.attributes('r')).toBe('22')
    })

    it('非中心节点半径 16', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      const nonCenter = nodes.filter(n => !n.classes().includes('center'))
      expect(nonCenter.length).toBeGreaterThan(0)
      for (const n of nonCenter) {
        expect(n.find('circle').attributes('r')).toBe('16')
      }
    })

    it('节点类型标签正确显示', async () => {
      const wrapper = await mountGraph()
      const typeTexts = wrapper.findAll('.rg-node-type').map(el => el.text())
      expect(typeTexts).toContain('machine')
      expect(typeTexts).toContain('os')
      expect(typeTexts).toContain('service')
    })

    it('仅中心节点无关系时只渲染 1 个节点', async () => {
      const wrapper = await mountGraph({ graph: centerOnlyGraph })
      await nextTick()
      expect(wrapper.findAll('.rg-node')).toHaveLength(1)
      expect(wrapper.findAll('.rg-edge')).toHaveLength(0)
    })
  })

  describe('边提取', () => {
    it('从 relations 提取所有边（4 条）', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.findAll('.rg-edge')).toHaveLength(4)
    })

    it('边标签显示关系类型', async () => {
      const wrapper = await mountGraph()
      const labels = wrapper.findAll('.rg-edge-label').map(el => el.text())
      expect(labels).toContain('runs_on')
      expect(labels).toContain('depends')
      expect(labels).toContain('contains')
    })

    it('边带箭头 marker', async () => {
      const wrapper = await mountGraph()
      const lines = wrapper.findAll('.rg-edge-line')
      for (const line of lines) {
        const marker = line.attributes('marker-end')
        expect(marker).toMatch(/^url\(#rg-arrow-/)
      }
    })

    it('defs 中为每种关系类型生成 arrow marker', async () => {
      const wrapper = await mountGraph()
      const markers = wrapper.findAll('marker')
      const ids = markers.map(m => m.attributes('id'))
      expect(ids).toContain('rg-arrow-runs_on')
      expect(ids).toContain('rg-arrow-depends')
      expect(ids).toContain('rg-arrow-contains')
    })
  })

  describe('CI 类型颜色映射', () => {
    it('machine → var(--accent)', async () => {
      const wrapper = await mountGraph()
      const centerCircle = wrapper.find('.rg-node.center circle')
      expect(centerCircle.attributes('fill')).toBe('var(--accent)')
    })

    it('os → var(--teal)', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      const osNode = nodes.find(n => n.find('.rg-node-type').text() === 'os')
      expect(osNode.find('circle').attributes('fill')).toBe('var(--teal)')
    })

    it('service → var(--ok)', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      const svcNode = nodes.find(n => n.find('.rg-node-type').text() === 'service')
      expect(svcNode.find('circle').attributes('fill')).toBe('var(--ok)')
    })

    it('未知类型 → var(--text-2)', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      const unknownNode = nodes.find(n => n.find('.rg-node-type').text() === 'unknown')
      expect(unknownNode).toBeTruthy()
      expect(unknownNode.find('circle').attributes('fill')).toBe('var(--text-2)')
    })
  })

  describe('关系类型线型映射', () => {
    it('runs_on → var(--accent) 实线', async () => {
      const wrapper = await mountGraph()
      const edges = wrapper.findAll('.rg-edge')
      const runsOnEdge = edges.find(e => e.find('.rg-edge-label').text() === 'runs_on')
      const line = runsOnEdge.find('.rg-edge-line')
      expect(line.attributes('stroke')).toBe('var(--accent)')
      expect(line.attributes('stroke-dasharray')).toBe('none')
    })

    it('depends → var(--warn) 虚线 6 4', async () => {
      const wrapper = await mountGraph()
      const edges = wrapper.findAll('.rg-edge')
      const dependsEdge = edges.find(e => e.find('.rg-edge-label').text() === 'depends')
      const line = dependsEdge.find('.rg-edge-line')
      expect(line.attributes('stroke')).toBe('var(--warn)')
      expect(line.attributes('stroke-dasharray')).toBe('6 4')
    })

    it('contains → var(--info) 实线加粗', async () => {
      const wrapper = await mountGraph()
      const edges = wrapper.findAll('.rg-edge')
      const containsEdge = edges.find(e => e.find('.rg-edge-label').text() === 'contains')
      const line = containsEdge.find('.rg-edge-line')
      expect(line.attributes('stroke')).toBe('var(--info)')
      expect(line.attributes('stroke-dasharray')).toBe('none')
      expect(line.attributes('stroke-width')).toBe('2.5')
    })

    it('connects_to → var(--teal) 点线 2 3', async () => {
      const graph = {
        centerCI: { id: 'c1', ciType: 'service', name: 'svc-a', status: 'active', source: 'manual', version: 1, attrs: {} },
        relations: [{ id: 1, sourceCIID: 'c1', targetCIID: 'c2', relationType: 'connects_to', sourceName: 'svc-a', targetName: 'svc-b', targetType: 'service' }]
      }
      const wrapper = await mountGraph({ graph })
      await nextTick()
      const line = wrapper.find('.rg-edge-line')
      expect(line.attributes('stroke')).toBe('var(--teal)')
      expect(line.attributes('stroke-dasharray')).toBe('2 3')
    })
  })

  describe('布局模式', () => {
    it('默认 mode=force', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.props('mode')).toBe('force')
    })

    it('mode=topology 时仍正确渲染节点与边', async () => {
      const wrapper = await mountGraph({ mode: 'topology' })
      await nextTick()
      expect(wrapper.findAll('.rg-node')).toHaveLength(5)
      expect(wrapper.findAll('.rg-edge')).toHaveLength(4)
    })

    it('工具栏模式按钮切换触发 update:mode', async () => {
      const wrapper = await mountGraph()
      const buttons = wrapper.findAll('.rg-tb-btn')
      // 前两个按钮是模式切换（force / topology）
      await buttons[1].trigger('click')
      const emitted = wrapper.emitted('update:mode')
      expect(emitted).toBeTruthy()
      expect(emitted[0][0]).toBe('topology')
    })

    it('工具栏第一个按钮为 force 模式', async () => {
      const wrapper = await mountGraph()
      const buttons = wrapper.findAll('.rg-tb-btn')
      await buttons[0].trigger('click')
      const emitted = wrapper.emitted('update:mode')
      expect(emitted).toBeTruthy()
      expect(emitted[0][0]).toBe('force')
    })
  })

  describe('交互：节点点击', () => {
    it('点击节点触发 node-click 事件并携带节点数据', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      // 点击中心节点
      await nodes[0].trigger('click')
      const emitted = wrapper.emitted('node-click')
      expect(emitted).toBeTruthy()
      expect(emitted[0][0]).toHaveProperty('id')
      expect(emitted[0][0]).toHaveProperty('name')
      expect(emitted[0][0]).toHaveProperty('type')
    })

    it('点击节点后显示详情面板 .rg-detail', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.find('.rg-detail').exists()).toBe(false)
      const nodes = wrapper.findAll('.rg-node')
      await nodes[0].trigger('click')
      expect(wrapper.find('.rg-detail').exists()).toBe(true)
    })

    it('详情面板关闭按钮 × 清除面板', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      await nodes[0].trigger('click')
      expect(wrapper.find('.rg-detail').exists()).toBe(true)
      await wrapper.find('.rg-detail-close').trigger('click')
      expect(wrapper.find('.rg-detail').exists()).toBe(false)
    })

    it('selectable=false 时不触发 node-click', async () => {
      const wrapper = await mountGraph({ selectable: false })
      const nodes = wrapper.findAll('.rg-node')
      await nodes[0].trigger('click')
      expect(wrapper.emitted('node-click')).toBeFalsy()
    })

    it('点击中心节点不切换图谱（CMDBView 中 onGraphNodeClick 处理）', async () => {
      const wrapper = await mountGraph()
      const centerNode = wrapper.find('.rg-node.center')
      await centerNode.trigger('click')
      const emitted = wrapper.emitted('node-click')
      expect(emitted[0][0].isCenter).toBe(true)
    })
  })

  describe('交互：节点 hover', () => {
    it('hover 节点触发 node-hover 事件', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      await nodes[0].trigger('mouseenter')
      const emitted = wrapper.emitted('node-hover')
      expect(emitted).toBeTruthy()
      expect(emitted[0][0]).toHaveProperty('id')
    })

    it('离开节点触发 node-hover null', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      await nodes[0].trigger('mouseenter')
      await nodes[0].trigger('mouseleave')
      const emitted = wrapper.emitted('node-hover')
      expect(emitted).toBeTruthy()
      expect(emitted[1][0]).toBeNull()
    })

    it('hover 节点添加 .hovered 类', async () => {
      const wrapper = await mountGraph()
      const nodes = wrapper.findAll('.rg-node')
      await nodes[0].trigger('mouseenter')
      // hoveredNode 状态更新后节点应有 hovered 类
      expect(nodes[0].classes()).toContain('hovered')
    })
  })

  describe('工具栏：缩放与重置', () => {
    it('放大按钮增加 scale', async () => {
      const wrapper = await mountGraph()
      const buttons = wrapper.findAll('.rg-tb-btn')
      // 按钮顺序：force, topology, sep, 放大(＋), 缩小(－), 重置(⟲)
      const zoomInBtn = buttons[2]
      await zoomInBtn.trigger('click')
      // scale 通过 transform 体现，验证 transform 包含 scale > 1
      const g = wrapper.find('.rg-canvas')
      expect(g.exists()).toBe(true)
      const transform = g.attributes('transform')
      expect(transform).toMatch(/scale\([0-9.]+\)/)
      // scale 应大于 1
      const match = transform.match(/scale\(([0-9.]+)\)/)
      expect(parseFloat(match[1])).toBeGreaterThan(1)
    })

    it('缩小按钮减少 scale', async () => {
      const wrapper = await mountGraph()
      const buttons = wrapper.findAll('.rg-tb-btn')
      const zoomOutBtn = buttons[3]
      await zoomOutBtn.trigger('click')
      const g = wrapper.find('.rg-canvas')
      const transform = g.attributes('transform')
      const match = transform.match(/scale\(([0-9.]+)\)/)
      expect(parseFloat(match[1])).toBeLessThan(1)
    })

    it('重置按钮恢复 scale=1', async () => {
      const wrapper = await mountGraph()
      const buttons = wrapper.findAll('.rg-tb-btn')
      // 先放大
      await buttons[2].trigger('click')
      // 再重置
      const resetBtn = buttons[4]
      await resetBtn.trigger('click')
      await nextTick()
      const g = wrapper.find('.rg-canvas')
      const transform = g.attributes('transform')
      const match = transform.match(/scale\(([0-9.]+)\)/)
      expect(parseFloat(match[1])).toBeCloseTo(1, 5)
    })
  })

  describe('图例', () => {
    it('图例显示所有出现的 CI 类型', async () => {
      const wrapper = await mountGraph()
      const legendText = wrapper.find('.rg-legend').text()
      expect(legendText).toContain('machine')
      expect(legendText).toContain('os')
      expect(legendText).toContain('service')
    })

    it('图例显示所有出现的关系类型', async () => {
      const wrapper = await mountGraph()
      const legendText = wrapper.find('.rg-legend').text()
      expect(legendText).toContain('runs_on')
      expect(legendText).toContain('depends')
      expect(legendText).toContain('contains')
    })

    it('图例类型项带颜色圆点', async () => {
      const wrapper = await mountGraph()
      const dots = wrapper.findAll('.rg-legend-dot')
      expect(dots.length).toBeGreaterThan(0)
      for (const d of dots) {
        expect(d.attributes('style')).toContain('background')
      }
    })
  })

  describe('节点详情面板', () => {
    it('点击节点后详情面板显示节点 ID', async () => {
      const wrapper = await mountGraph()
      const centerNode = wrapper.find('.rg-node.center')
      await centerNode.trigger('click')
      const detail = wrapper.find('.rg-detail')
      expect(detail.text()).toContain('ci-host-1')
    })

    it('详情面板显示节点名称与类型', async () => {
      const wrapper = await mountGraph()
      const centerNode = wrapper.find('.rg-node.center')
      await centerNode.trigger('click')
      const detail = wrapper.find('.rg-detail')
      expect(detail.text()).toContain('web-host-1')
      expect(detail.text()).toContain('machine')
    })

    it('详情面板显示状态与来源', async () => {
      const wrapper = await mountGraph()
      const centerNode = wrapper.find('.rg-node.center')
      await centerNode.trigger('click')
      const detail = wrapper.find('.rg-detail')
      expect(detail.text()).toContain('active')
      expect(detail.text()).toContain('agent')
    })

    it('详情面板显示属性键值对', async () => {
      const wrapper = await mountGraph()
      const centerNode = wrapper.find('.rg-node.center')
      await centerNode.trigger('click')
      const detail = wrapper.find('.rg-detail')
      expect(detail.text()).toContain('env')
      expect(detail.text()).toContain('prod')
      expect(detail.text()).toContain('region')
      expect(detail.text()).toContain('cn-east')
    })
  })

  describe('空状态', () => {
    it('graph=null 时不渲染节点与边，显示空状态', async () => {
      const wrapper = await mountGraph({ graph: null })
      expect(wrapper.findAll('.rg-node')).toHaveLength(0)
      expect(wrapper.findAll('.rg-edge')).toHaveLength(0)
      expect(wrapper.find('.rg-empty').exists()).toBe(true)
    })

    it('空图谱（centerCI=null, relations=[]）显示空状态', async () => {
      const wrapper = await mountGraph({ graph: emptyGraph })
      expect(wrapper.findAll('.rg-node')).toHaveLength(0)
      expect(wrapper.find('.rg-empty').exists()).toBe(true)
    })

    it('空状态时工具栏信息显示 0 节点 0 边', async () => {
      const wrapper = await mountGraph({ graph: null })
      const info = wrapper.find('.rg-tb-info')
      expect(info.text()).toContain('0 节点')
      expect(info.text()).toContain('0 边')
    })
  })

  describe('工具栏信息', () => {
    it('显示正确的节点与边数量', async () => {
      const wrapper = await mountGraph()
      const info = wrapper.find('.rg-tb-info')
      expect(info.text()).toContain('5 节点')
      expect(info.text()).toContain('4 边')
    })
  })

  describe('graph 变化时重新布局', () => {
    it('切换 graph 后节点数量更新', async () => {
      const wrapper = await mountGraph()
      expect(wrapper.findAll('.rg-node')).toHaveLength(5)
      await wrapper.setProps({ graph: centerOnlyGraph })
      await nextTick()
      expect(wrapper.findAll('.rg-node')).toHaveLength(1)
    })

    it('切换 mode 后重新布局（节点数不变）', async () => {
      const wrapper = await mountGraph({ mode: 'force' })
      expect(wrapper.findAll('.rg-node')).toHaveLength(5)
      await wrapper.setProps({ mode: 'topology' })
      await nextTick()
      expect(wrapper.findAll('.rg-node')).toHaveLength(5)
    })
  })
})