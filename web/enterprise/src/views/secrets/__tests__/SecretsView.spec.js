// SecretsView 组件单元测试
// 覆盖：
//   - 组件能正确挂载
//   - 调用 API 获取状态（getSecretProviderStatus + listSecretKeys）
//   - 测试连接按钮点击触发 testSecretProvider API 调用
//   - 密钥列表正确渲染
//
// mock 策略：
//   - @/api/secrets：vi.mock 替换为 vi.fn，避免真实网络请求
//   - @/i18n：返回键本身，便于断言
//   - 全局 $t：通过 global.mocks 注入
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

// mock @/api/secrets：所有 API 用 vi.fn 控制返回值
vi.mock('@/api/secrets', () => ({
  getSecretProviderStatus: vi.fn(),
  testSecretProvider: vi.fn(),
  listSecretKeys: vi.fn(),
}))

// mock @/i18n：返回键本身（与 global.mocks.$t 行为一致），便于断言。
// 有 params 时附加 JSON.stringify(params) 便于断言插值参数被正确传递。
vi.mock('@/i18n', () => ({
  t: (key, params) => {
    if (!params) return key
    return key + JSON.stringify(params)
  },
  currentLang: { value: 'zh' },
  setLang: vi.fn(),
  initLang: vi.fn(),
  default: {},
}))

import SecretsView from '@/views/secrets/SecretsView.vue'
import {
  getSecretProviderStatus,
  testSecretProvider,
  listSecretKeys,
} from '@/api/secrets'

// mock $t：返回键本身，便于断言
const mockT = (key, params) => {
  if (!params) return key
  // 简单插值，与 i18n/index.js 行为一致
  return key.replace(/\{(\w+)\}/g, (_, k) => (params[k] != null ? params[k] : `{${k}}`))
}

describe('SecretsView 组件', () => {
  beforeEach(() => {
    // 每个测试前重置 mock 调用记录与返回值
    vi.clearAllMocks()
    // 默认 mock：状态空、密钥空
    getSecretProviderStatus.mockResolvedValue({ provider: 'env', enabled: true, addr: '', mount: '', file: '' })
    listSecretKeys.mockResolvedValue([])
    testSecretProvider.mockResolvedValue({ s: 200, j: { ok: true, latencyMs: 5 } })
  })

  describe('组件挂载', () => {
    it('挂载成功，包含标题', async () => {
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      // 等待 onMounted 中的异步 fetch 完成
      await nextTick()
      await nextTick()
      expect(wrapper.exists()).toBe(true)
      expect(wrapper.find('h2').text()).toBe('secrets.title')
    })

    it('挂载后调用 getSecretProviderStatus 获取状态', async () => {
      mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      expect(getSecretProviderStatus).toHaveBeenCalledTimes(1)
    })

    it('挂载后调用 listSecretKeys 获取密钥列表', async () => {
      mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      expect(listSecretKeys).toHaveBeenCalledTimes(1)
    })
  })

  describe('提供者状态展示', () => {
    it('状态数据正确渲染到 DOM', async () => {
      getSecretProviderStatus.mockResolvedValue({
        provider: 'vault',
        enabled: true,
        addr: 'https://vault:8200',
        mount: 'secret',
        file: '',
      })
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      // provider 类型展示在 badge 中
      const badges = wrapper.findAll('.badge.info')
      expect(badges[0].text()).toBe('vault')
      // Vault 地址展示在 code 中
      const codes = wrapper.findAll('code')
      const addrText = codes.map((c) => c.text()).find((t) => t === 'https://vault:8200')
      expect(addrText).toBe('https://vault:8200')
    })

    it('加载失败时显示错误信息', async () => {
      getSecretProviderStatus.mockRejectedValue({ s: 500, j: { error: 'server down' } })
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      expect(wrapper.find('.poll-err').exists()).toBe(true)
      expect(wrapper.find('.poll-err').text()).toContain('server down')
    })
  })

  describe('密钥列表渲染', () => {
    it('密钥列表正确渲染多行', async () => {
      listSecretKeys.mockResolvedValue([
        { key: 'db/password', provider: 'vault' },
        { key: 'api/token', provider: 'env' },
        { key: 'notify/dingtalk#webhook_url', provider: 'file' },
      ])
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      const trs = wrapper.findAll('tbody tr')
      expect(trs).toHaveLength(3)
    })

    it('密钥列表为空时显示空提示行', async () => {
      listSecretKeys.mockResolvedValue([])
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      const trs = wrapper.findAll('tbody tr')
      expect(trs).toHaveLength(1) // 空提示行
      expect(trs[0].text()).toContain('secrets.no_keys')
    })

    it('listSecretKeys 返回非数组时回退为空', async () => {
      listSecretKeys.mockResolvedValue(null)
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      const trs = wrapper.findAll('tbody tr')
      expect(trs).toHaveLength(1) // 空提示行
    })
  })

  describe('测试连接按钮', () => {
    it('点击测试连接按钮触发 testSecretProvider API 调用', async () => {
      getSecretProviderStatus.mockResolvedValue({
        provider: 'vault',
        enabled: true,
        addr: 'https://vault:8200',
        mount: 'secret',
        file: '',
      })
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()

      // 找到提交按钮（type=submit）
      const submitBtn = wrapper.find('button[type="submit"]')
      expect(submitBtn.exists()).toBe(true)
      await submitBtn.trigger('submit')
      await nextTick()
      await nextTick()
      expect(testSecretProvider).toHaveBeenCalledTimes(1)
      const callArg = testSecretProvider.mock.calls[0][0]
      expect(callArg).toHaveProperty('addr')
      expect(callArg).toHaveProperty('token')
      expect(callArg).toHaveProperty('mount')
    })

    it('测试成功时显示成功消息', async () => {
      getSecretProviderStatus.mockResolvedValue({
        provider: 'vault',
        enabled: true,
        addr: 'https://vault:8200',
        mount: 'secret',
        file: '',
      })
      testSecretProvider.mockResolvedValue({ s: 200, j: { ok: true, latencyMs: 12 } })
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      const submitBtn = wrapper.find('button[type="submit"]')
      await submitBtn.trigger('submit')
      await nextTick()
      await nextTick()
      const msg = wrapper.find('.msg.ok')
      expect(msg.exists()).toBe(true)
      expect(msg.text()).toContain('12')
    })

    it('测试失败时显示错误消息', async () => {
      getSecretProviderStatus.mockResolvedValue({
        provider: 'vault',
        enabled: true,
        addr: 'https://vault:8200',
        mount: 'secret',
        file: '',
      })
      testSecretProvider.mockResolvedValue({ s: 200, j: { ok: false, error: 'auth failed' } })
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      const submitBtn = wrapper.find('button[type="submit"]')
      await submitBtn.trigger('submit')
      await nextTick()
      await nextTick()
      const msg = wrapper.find('.msg.err')
      expect(msg.exists()).toBe(true)
      expect(msg.text()).toContain('auth failed')
    })

    it('addr 为空时不调用 API 并提示', async () => {
      getSecretProviderStatus.mockResolvedValue({ provider: 'env', enabled: true, addr: '', mount: '', file: '' })
      const wrapper = mount(SecretsView, {
        global: { mocks: { $t: mockT } },
      })
      await nextTick()
      await nextTick()
      const submitBtn = wrapper.find('button[type="submit"]')
      await submitBtn.trigger('submit')
      await nextTick()
      await nextTick()
      expect(testSecretProvider).not.toHaveBeenCalled()
      const msg = wrapper.find('.msg.err')
      expect(msg.exists()).toBe(true)
      expect(msg.text()).toContain('need_addr')
    })
  })
})