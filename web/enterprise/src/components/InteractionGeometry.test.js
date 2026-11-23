import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { mount } from '@vue/test-utils'
import Button from '@/components/ui/Button.vue'

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')

describe('交互几何契约', () => {
  it('sm 是 Button 支持的 32px 尺寸，而不是无样式的 small', () => {
    const wrapper = mount(Button, { props: { size: 'sm' }, slots: { default: '操作' } })
    expect(wrapper.classes()).toContain('btn--sm')
    expect(wrapper.classes()).not.toContain('btn--small')
    expect(read('../components/ui/Button.vue')).toMatch(/\.btn--sm\s*\{[^}]*height:\s*32px/s)
  })

  it('Batch/Canary 不再传入无样式的 small', () => {
    expect(read('../views/BatchView.vue')).not.toContain('size="small"')
    expect(read('../views/CanaryView.vue')).not.toContain('size="small"')
  })

  it('Toast 关闭热区至少 32×32', () => {
    const source = read('../components/ToastHost.vue')
    expect(source).toMatch(/\.toast-close\s*\{[^}]*min-width:\s*32px/s)
    expect(source).toMatch(/\.toast-close\s*\{[^}]*min-height:\s*32px/s)
  })
})
