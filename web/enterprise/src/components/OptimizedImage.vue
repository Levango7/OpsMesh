<template>
  <div class="optimized-image" :style="containerStyle">
    <div v-if="blurhash && !loaded" class="blurhash-placeholder" :style="blurhashStyle">
      <canvas ref="canvas" :width="width" :height="height" />
    </div>
    <picture v-if="srcset && srcset.length">
      <source
        v-for="source in webpSources"
        :key="source.src"
        :srcset="source.src"
        :media="source.media"
        type="image/webp"
      />
      <img
        ref="imgEl"
        :src="src"
        :alt="alt"
        :loading="lazy ? 'lazy' : 'eager'"
        :decoding="async ? 'async' : 'sync'"
        :class="['image', { loaded: loaded, 'is-loading': !loaded && !blurhash }]"
        @load="onLoad"
        @error="onError"
      />
    </picture>
    <img
      v-else
      ref="imgEl"
      :src="src"
      :alt="alt"
      :loading="lazy ? 'lazy' : 'eager'"
      :decoding="async ? 'async' : 'sync'"
      :class="['image', { loaded: loaded, 'is-loading': !loaded && !blurhash }]"
      @load="onLoad"
      @error="onError"
    />
    <div v-if="!loaded && !blurhash" class="skeleton" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'

const props = defineProps({
  src: { type: String, required: true },
  alt: { type: String, default: '' },
  width: { type: Number, default: 320 },
  height: { type: Number, default: 240 },
  lazy: { type: Boolean, default: true },
  async: { type: Boolean, default: true },
  blurhash: { type: String, default: '' },
  srcset: { type: Array, default: () => [] },
  sizes: { type: String, default: '100vw' },
  objectFit: { type: String, default: 'cover' },
  rootMargin: { type: String, default: '50px' }
})

const loaded = ref(false)
const inView = ref(false)
const imgEl = ref(null)
const canvas = ref(null)
let observer = null

const containerStyle = computed(() => ({
  position: 'relative',
  width: '100%',
  aspectRatio: `${props.width} / ${props.height}`,
  overflow: 'hidden'
}))

const blurhashStyle = computed(() => ({
  position: 'absolute',
  inset: 0,
  width: '100%',
  height: '100%'
}))

const webpSources = computed(() =>
  props.srcset.map((s) => ({
    src: s.src.replace(/\.(jpe?g|png|gif)$/i, '.webp'),
    media: s.media
  }))
)

function onLoad() {
  loaded.value = true
}

function onError() {
  loaded.value = true
}

function decodeBlurhash() {
  if (!props.blurhash || !canvas.value) return
  const ctx = canvas.value.getContext('2d')
  const pixels = decodeBlurhashData(props.blurhash, props.width, props.height)
  if (!pixels) return
  const imageData = ctx.createImageData(props.width, props.height)
  imageData.data.set(pixels)
  ctx.putImageData(imageData, 0, 0)
}

function decodeBlurhashData(hash, width, height) {
  if (!hash || hash.length < 6) return null

  const sizeFlag = decode83(hash[0])
  const numY = Math.floor(sizeFlag / 9) + 1
  const numX = (sizeFlag % 9) + 1
  const quantMaxValue = decode83(hash[1])
  const maxValue = (quantMaxValue + 1) / 166
  if (hash.length !== 4 + 2 * numX * numY) return null

  const colors = new Array(numX * numY)
  for (let i = 0; i < numX * numY; i++) {
    if (i === 0) {
      const colorValue = decode83(hash.substring(2, 6))
      colors[i] = decodeDC(colorValue)
    } else {
      const colorValue = decode83(hash.substring(4 + i * 2, 6 + i * 2))
      colors[i] = decodeAC(colorValue, maxValue)
    }
  }

  const pixels = new Uint8ClampedArray(width * height * 4)
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      let r = 0, g = 0, b = 0
      for (let j = 0; j < numY; j++) {
        for (let i = 0; i < numX; i++) {
          const basis = Math.cos((Math.PI * x * i) / width) * Math.cos((Math.PI * y * j) / height)
          const color = colors[i + j * numX]
          r += color[0] * basis
          g += color[1] * basis
          b += color[2] * basis
        }
      }
      const idx = (y * width + x) * 4
      pixels[idx] = srgbToLinear(r)
      pixels[idx + 1] = srgbToLinear(g)
      pixels[idx + 2] = srgbToLinear(b)
      pixels[idx + 3] = 255
    }
  }
  return pixels
}

function decode83(str) {
  const chars = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~'
  let value = 0
  for (const c of str) {
    value = value * 83 + chars.indexOf(c)
  }
  return value
}

function decodeDC(value) {
  const r = value >> 16
  const g = (value >> 8) & 255
  const b = value & 255
  return [r, g, b]
}

function decodeAC(value, maxValue) {
  const quantR = Math.floor(value / (19 * 19))
  const quantG = Math.floor(value / 19) % 19
  const quantB = value % 19
  return [
    Math.sign(quantR) * (quantR * quantR) / (19 * 19) * maxValue,
    Math.sign(quantG) * (quantG * quantG) / (19 * 19) * maxValue,
    Math.sign(quantB) * (quantB * quantB) / (19 * 19) * maxValue
  ]
}

function srgbToLinear(value) {
  const v = Math.max(0, Math.min(1, value / 255))
  return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4) * 255
}

onMounted(() => {
  decodeBlurhash()

  if (!props.lazy) {
    inView.value = true
    return
  }

  if ('IntersectionObserver' in window) {
    observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            inView.value = true
            observer.disconnect()
            break
          }
        }
      },
      { rootMargin: props.rootMargin }
    )
    if (imgEl.value) observer.observe(imgEl.value)
  } else {
    inView.value = true
  }
})

onUnmounted(() => {
  if (observer) observer.disconnect()
})

watch(() => props.src, () => {
  loaded.value = false
})
</script>

<style scoped>
.optimized-image {
  display: block;
  background: var(--surface-3, #1e293b);
}

.image {
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0;
  transition: opacity 0.3s ease;
}

.image.loaded {
  opacity: 1;
}

.blurhash-placeholder {
  filter: blur(20px);
  transform: scale(1.1);
}

.blurhash-placeholder canvas {
  width: 100%;
  height: 100%;
}

.skeleton {
  position: absolute;
  inset: 0;
  background: linear-gradient(90deg, var(--surface-3, #1e293b) 25%, var(--surface, #334155) 50%, var(--surface-3, #1e293b) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
}

@keyframes shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
</style>
