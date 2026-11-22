import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PageShell from '@/components/PageShell.vue'

let wrappers = []
afterEach(() => {
  wrappers.forEach(w => w.unmount())
  wrappers = []
})

function mountShell(error) {
  const wrapper = mount(PageShell, {
    props: { title: 'Helm', testid: 'helm', error },
    slots: { default: '<button class="business-action">安装</button>' },
    global: { mocks: { $t: key => key } },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('PageShell 可选能力门控', () => {
  it('明确能力缺失时显示空态并隐藏不可用操作', () => {
    const wrapper = mountShell('helm CLI not available')
    expect(wrapper.find('[data-testid="helm-capability"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('common.capability_unavailable_title')
    expect(wrapper.find('.poll-err').exists()).toBe(false)
    expect(wrapper.find('.business-action').exists()).toBe(false)
  })

  it('真实临时故障仍显示错误且保留业务区', () => {
    const wrapper = mountShell('authentication temporarily unavailable, retry shortly')
    expect(wrapper.find('[data-testid="helm-capability"]').exists()).toBe(false)
    expect(wrapper.find('.poll-err').exists()).toBe(true)
    expect(wrapper.find('.business-action').exists()).toBe(true)
  })
})
