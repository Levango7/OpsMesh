// OptimizedImage 组件单元测试
// 覆盖：
//   - 组件挂载与基本结构
//   - src/alt/width/height 属性传递
//   - loading="lazy" 属性（lazy prop 控制）
//   - decoding 属性（async prop 控制）
//   - load/error 事件回调
//   - srcset 渲染 picture + source
//   - blurhash 占位
//   - 容器 aspect-ratio 样式
//
// 环境说明：jsdom 不支持 canvas.getContext（需 canvas npm 包），
// 因此对 blurhash 用例 mock HTMLCanvasElement.prototype.getContext 返回 stub ctx。
import { describe, it, expect, beforeAll } from 'vitest'
import { mount } from '@vue/test-utils'
import OptimizedImage from '@/components/OptimizedImage.vue'

// jsdom 下 canvas.getContext 返回 null，blurhash 解码会抛错。
// 这里 stub 一个最小 2d context，使 decodeBlurhash 不致抛出。
beforeAll(() => {
  if (!HTMLCanvasElement.prototype.getContext) {
    HTMLCanvasElement.prototype.getContext = () => ({
      createImageData: (w, h) => ({ data: new Uint8ClampedArray(w * h * 4) }),
      putImageData: () => {},
    })
  } else {
    const orig = HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.getContext = function (type, ...args) {
      try {
        const ctx = orig.call(this, type, ...args)
        if (ctx) return ctx
      } catch {
        // fallthrough to stub
      }
      return {
        createImageData: (w, h) => ({ data: new Uint8ClampedArray(w * h * 4) }),
        putImageData: () => {},
      }
    }
  }
})

describe('OptimizedImage 组件', () => {
  describe('组件渲染', () => {
    it('挂载成功，包含 .optimized-image 容器', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('.optimized-image').exists()).toBe(true)
    })

    it('默认渲染 img 元素（无 srcset 时不渲染 picture）', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('img').exists()).toBe(true)
      expect(wrapper.find('picture').exists()).toBe(false)
    })
  })

  describe('属性传递', () => {
    it('src 属性传递到 img', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/img/photo.png' } })
      expect(wrapper.find('img').attributes('src')).toBe('/img/photo.png')
    })

    it('alt 属性传递到 img', () => {
      const wrapper = mount(OptimizedImage, {
        props: { src: '/a.jpg', alt: '描述文字' },
      })
      expect(wrapper.find('img').attributes('alt')).toBe('描述文字')
    })

    it('alt 默认为空字符串', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('img').attributes('alt')).toBe('')
    })
  })

  describe('loading 属性（lazy 控制）', () => {
    it('默认 lazy=true 时 img loading="lazy"', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('img').attributes('loading')).toBe('lazy')
    })

    it('lazy=false 时 img loading="eager"', () => {
      const wrapper = mount(OptimizedImage, {
        props: { src: '/a.jpg', lazy: false },
      })
      expect(wrapper.find('img').attributes('loading')).toBe('eager')
    })
  })

  describe('decoding 属性（async 控制）', () => {
    it('默认 async=true 时 img decoding="async"', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('img').attributes('decoding')).toBe('async')
    })

    it('async=false 时 img decoding="sync"', () => {
      const wrapper = mount(OptimizedImage, {
        props: { src: '/a.jpg', async: false },
      })
      expect(wrapper.find('img').attributes('decoding')).toBe('sync')
    })
  })

  describe('容器样式（aspect-ratio）', () => {
    it('默认 width/height 320/240 → aspect-ratio: 320 / 240', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      const style = wrapper.find('.optimized-image').attributes('style') || ''
      expect(style).toContain('aspect-ratio: 320 / 240')
    })

    it('自定义 width/height → aspect-ratio 相应变化', () => {
      const wrapper = mount(OptimizedImage, {
        props: { src: '/a.jpg', width: 800, height: 600 },
      })
      const style = wrapper.find('.optimized-image').attributes('style') || ''
      expect(style).toContain('aspect-ratio: 800 / 600')
    })
  })

  describe('srcset 渲染 picture', () => {
    it('提供 srcset 时渲染 picture 与 source 元素', () => {
      const wrapper = mount(OptimizedImage, {
        props: {
          src: '/a.jpg',
          srcset: [
            { src: '/a-1x.jpg', media: '(max-width: 600px)' },
            { src: '/a-2x.jpg', media: '(min-width: 601px)' },
          ],
        },
      })
      expect(wrapper.find('picture').exists()).toBe(true)
      const sources = wrapper.findAll('source')
      expect(sources).toHaveLength(2)
      // source type 应为 image/webp
      expect(sources[0].attributes('type')).toBe('image/webp')
    })

    it('srcset 中 jpg/png/gif 后缀应替换为 .webp', () => {
      const wrapper = mount(OptimizedImage, {
        props: {
          src: '/a.jpg',
          srcset: [{ src: '/photo.png', media: '(max-width: 600px)' }],
        },
      })
      const source = wrapper.find('source')
      expect(source.attributes('srcset')).toBe('/photo.webp')
    })

    it('srcset media 属性传递', () => {
      const wrapper = mount(OptimizedImage, {
        props: {
          src: '/a.jpg',
          srcset: [{ src: '/a.jpg', media: '(min-width: 1000px)' }],
        },
      })
      expect(wrapper.find('source').attributes('media')).toBe('(min-width: 1000px)')
    })
  })

  describe('skeleton 占位', () => {
    it('无 blurhash 且未 loaded 时显示 skeleton', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('.skeleton').exists()).toBe(true)
    })

    it('提供 blurhash 时不显示 skeleton（改用 blurhash-placeholder）', () => {
      const wrapper = mount(OptimizedImage, {
        props: { src: '/a.jpg', blurhash: 'LFE@DvWV00NG}^M{R*oe~V?bxV?b' },
      })
      expect(wrapper.find('.skeleton').exists()).toBe(false)
      expect(wrapper.find('.blurhash-placeholder').exists()).toBe(true)
    })
  })

  describe('img class 状态', () => {
    it('初始未 loaded 时 img 包含 is-loading class（无 blurhash）', () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      expect(wrapper.find('img').classes()).toContain('is-loading')
      expect(wrapper.find('img').classes()).not.toContain('loaded')
    })
  })

  describe('load 事件', () => {
    it('img 触发 load 事件后添加 loaded class', async () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/a.jpg' } })
      await wrapper.find('img').trigger('load')
      expect(wrapper.find('img').classes()).toContain('loaded')
    })
  })

  describe('error 事件', () => {
    it('img 触发 error 事件后也标记 loaded（停止 loading 态）', async () => {
      const wrapper = mount(OptimizedImage, { props: { src: '/missing.jpg' } })
      await wrapper.find('img').trigger('error')
      expect(wrapper.find('img').classes()).toContain('loaded')
    })
  })
})
