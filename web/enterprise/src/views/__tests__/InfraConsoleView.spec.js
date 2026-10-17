// InfraConsoleView 视图单元测试
// 覆盖：
//   - 组件挂载与基本结构（标题 / domain 导航 / 设备选择）
//   - 未选设备时提示、选中设备后渲染动作面板
//   - domain/动作切换、动态字段渲染
//   - 提交动作调用 submitInfraAction，query 类型展示结果
//   - task 类型展示任务卡片并可拉取结果
//   - INFRA_DOMAINS 目录结构完整性（每个 domain 有动作、动作有字段与 path 函数）
//
// mock 策略：
//   - @/i18n：t 返回键，currentLang 为 zh
//   - @/stores/device：可控 flat 列表 + fetchDevices
//   - @/api/infra-ops：submitInfraAction / getInfraTaskResult 用 vi.fn；
//     INFRA_DOMAINS 透传真实 catalog（同时验证目录结构）
//   - @/utils/toast：stub
//   - Icon.vue：stub
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const { mockSubmit, mockTaskResult } = vi.hoisted(() => ({
  mockSubmit: vi.fn(),
  mockTaskResult: vi.fn(),
}))

vi.mock('@/i18n', () => ({
  t: (key) => key,
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

vi.mock('@/stores/device', () => ({
  useDeviceStore: () => ({
    flat: [
      { deviceID: 'dev-1', hostname: 'host-1', ip: '10.0.0.1' },
      { deviceID: 'dev-2', hostname: 'host-2', ip: '10.0.0.2' },
    ],
    fetchDevices: vi.fn().mockResolvedValue(),
  }),
}))

vi.mock('@/api/infra-ops', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    submitInfraAction: mockSubmit,
    getInfraTaskResult: mockTaskResult,
  }
})

vi.mock('@/utils/toast', () => ({
  toast: { error: vi.fn(), warn: vi.fn(), success: vi.fn() },
  default: {},
}))

vi.mock('@/components/Icon.vue', () => ({
  default: { name: 'Icon', props: ['name', 'size'], template: '<span class="icon-mock" />' },
}))

import InfraConsoleView from '@/views/InfraConsoleView.vue'
import { INFRA_DOMAINS } from '@/api/infra-ops'

function mountView() {
  return mount(InfraConsoleView, {
    global: { mocks: { $t: (k) => k } },
  })
}

describe('InfraConsoleView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染标题与 domain 导航', () => {
    const w = mountView()
    expect(w.find('[data-testid="infra-console-title"]').exists()).toBe(true)
    expect(w.find('[data-testid="domain-nav"]').exists()).toBe(true)
    // 目录中的每个 domain 都有导航按钮
    for (const d of INFRA_DOMAINS) {
      expect(w.find(`[data-testid="domain-${d.id}"]`).exists()).toBe(true)
    }
  })

  it('未选设备时提示选择设备', async () => {
    const w = mountView()
    expect(w.find('[data-testid="please-select"]').exists()).toBe(true)
  })

  it('选中设备后渲染动作面板与动态表单', async () => {
    const w = mountView()
    await w.find('[data-testid="device-select"]').setValue('dev-1')
    expect(w.find('[data-testid="action-tabs"]').exists()).toBe(true)
    expect(w.find('[data-testid="action-form"]').exists()).toBe(true)
  })

  it('切换 domain 显示对应动作集合', async () => {
    const w = mountView()
    await w.find('[data-testid="device-select"]').setValue('dev-1')
    // 切到中间件 domain（有 kafka-topic 动作）
    await w.find('[data-testid="domain-middleware"]').trigger('click')
    expect(w.find('[data-testid="action-kafka-topic"]').exists()).toBe(true)
    // 切到安全 domain（有 scan 动作）
    await w.find('[data-testid="domain-security"]').trigger('click')
    expect(w.find('[data-testid="action-scan"]').exists()).toBe(true)
  })

  it('提交动作调用 submitInfraAction 并展示任务卡片', async () => {
    mockSubmit.mockResolvedValueOnce({ kind: 'task', data: { id: 'task-42', state: 'pending' } })
    const w = mountView()
    await w.find('[data-testid="device-select"]').setValue('dev-1')
    // 选一个必填字段少的动作：中间件 lifecycle（type/action 必填）
    await w.find('[data-testid="domain-middleware"]').trigger('click')
    await w.find('[data-testid="action-lifecycle"]').trigger('click')
    await w.find('[data-testid="submit-btn"]').trigger('submit')
    await w.vm.$nextTick()
    expect(mockSubmit).toHaveBeenCalledTimes(1)
    const box = w.find('[data-testid="result-box"]')
    expect(box.exists()).toBe(true)
    expect(box.text()).toContain('task-42')
  })

  it('query 类型动作直接展示查询结果', async () => {
    mockSubmit.mockResolvedValueOnce({ kind: 'query', data: { deviceID: 'dev-1', middleware: [] } })
    const w = mountView()
    await w.find('[data-testid="device-select"]').setValue('dev-1')
    // 默认 domain 的第一个动作即状态查询（GET）
    await w.find('[data-testid="submit-btn"]').trigger('submit')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="result-box"]').text()).toContain('deviceID')
  })

  it('task 类型可拉取任务结果', async () => {
    mockSubmit.mockResolvedValueOnce({ kind: 'task', data: { id: 'task-7', state: 'done' } })
    mockTaskResult.mockResolvedValueOnce({ output: 'hello from agent' })
    const w = mountView()
    await w.find('[data-testid="device-select"]').setValue('dev-1')
    await w.find('[data-testid="submit-btn"]').trigger('submit')
    await w.vm.$nextTick()
    // 点击"查询结果"
    const buttons = w.findAll('button')
    const fetchBtn = buttons.find((b) => b.text().includes('查询结果'))
    expect(fetchBtn).toBeTruthy()
    await fetchBtn.trigger('click')
    await w.vm.$nextTick()
    expect(mockTaskResult).toHaveBeenCalledWith('task-7')
    expect(w.find('[data-testid="result-box"]').text()).toContain('hello from agent')
  })
})

describe('INFRA_DOMAINS catalog 结构完整性', () => {
  it('每个 domain 具备 id/name/icon/actions，且 id 唯一', () => {
    const seen = new Set()
    for (const d of INFRA_DOMAINS) {
      expect(d.id).toBeTruthy()
      expect(seen.has(d.id)).toBe(false)
      seen.add(d.id)
      expect(d.name?.zh).toBeTruthy()
      expect(d.name?.en).toBeTruthy()
      expect(Array.isArray(d.actions)).toBe(true)
      expect(d.actions.length).toBeGreaterThan(0)
    }
    expect(seen.size).toBeGreaterThanOrEqual(15)
  })

  it('每个动作具备 method/path 函数/fields，字段有 key 与 label', () => {
    for (const d of INFRA_DOMAINS) {
      for (const a of d.actions) {
        expect(['GET', 'POST', 'DELETE']).toContain(a.method)
        expect(typeof a.path).toBe('function')
        expect(Array.isArray(a.fields)).toBe(true)
        for (const f of a.fields) {
          expect(f.key).toBeTruthy()
          expect(f.label?.zh).toBeTruthy()
          expect(f.label?.en).toBeTruthy()
        }
        // path 函数可执行（传入样例字段值不抛错）
        const sample = {}
        for (const f of a.fields) sample[f.key] = 'x'
        expect(() => a.path(sample)).not.toThrow()
      }
    }
  })

  it('覆盖 P4/P5/P6/P7 核心 domain', () => {
    const ids = INFRA_DOMAINS.map((d) => d.id)
    for (const expected of [
      'middleware', 'db', 'olap', 'tsdb', 'vector', 'graph', 'lb',
      'storage', 'harbor', 'monitor', 'kernel', 'security', 'virt',
      'ansible', 'packages', 'env', 'k8s-provision',
      'ssh-trust', 'ntp', 'dns', 'fileshare'
    ]) {
      expect(ids).toContain(expected)
    }
  })
})
