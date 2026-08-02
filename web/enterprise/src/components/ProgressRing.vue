<template>
  <!--
    圆环进度条组件 — 纯 SVG 实现，无第三方图表库依赖
    - 通过 stroke-dasharray + stroke-dashoffset 控制进度
    - 中心展示百分比文字
    - 颜色按阈值自动切换：ok / warn / danger
  -->
  <div class="ring" :style="{ width: size + 'px', height: size + 'px' }">
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`">
      <!-- 背景圆环 -->
      <circle
        :cx="cx" :cy="cy" :r="radius"
        fill="none"
        :stroke="trackColor"
        :stroke-width="strokeWidth"
      />
      <!-- 进度圆环 -->
      <circle
        :cx="cx" :cy="cy" :r="radius"
        fill="none"
        :stroke="ringColor"
        :stroke-width="strokeWidth"
        stroke-linecap="round"
        :stroke-dasharray="circumference"
        :stroke-dashoffset="dashOffset"
        :transform="`rotate(-90 ${cx} ${cy})`"
        style="transition: stroke-dashoffset .35s ease;"
      />
    </svg>
    <div class="ring-text">
      <span class="ring-val">{{ displayValue }}<small>%</small></span>
      <span v-if="label" class="ring-label">{{ label }}</span>
    </div>
  </div>
</template>

<script setup>
// 圆环进度条 — SVG 实现，按阈值切换颜色
import { computed } from 'vue'

const props = defineProps({
  // 进度值（0-100）
  value: { type: Number, default: 0 },
  // 圆环尺寸（像素）
  size: { type: Number, default: 120 },
  // 描边宽度
  strokeWidth: { type: Number, default: 10 },
  // 中心标签
  label: { type: String, default: '' },
  // 阈值：超过 warn 显示黄色，超过 danger 显示红色，否则绿色
  warnAt: { type: Number, default: 60 },
  dangerAt: { type: Number, default: 85 },
  // 自定义颜色（覆盖阈值自动判定）
  color: { type: String, default: '' }
})

// 限定进度在 0-100 之间
const safeValue = computed(() => Math.max(0, Math.min(100, props.value || 0)))
const displayValue = computed(() => safeValue.value.toFixed(safeValue.value >= 100 ? 0 : 1))

// SVG 几何参数
const cx = computed(() => props.size / 2)
const cy = computed(() => props.size / 2)
const radius = computed(() => (props.size - props.strokeWidth) / 2)
const circumference = computed(() => 2 * Math.PI * radius.value)
// 偏移量：100% 时 offset=0，0% 时 offset=周长
const dashOffset = computed(() => circumference.value * (1 - safeValue.value / 100))

// 背景轨道色
const trackColor = 'var(--surface-3)'

// 进度色：自定义优先，否则按阈值
const ringColor = computed(() => {
  if (props.color) return props.color
  if (safeValue.value >= props.dangerAt) return 'var(--fail)'
  if (safeValue.value >= props.warnAt) return 'var(--warn)'
  return 'var(--ok)'
})
</script>

<style scoped>
.ring { position: relative; display: inline-flex; align-items: center; justify-content: center; }
.ring-text {
  position: absolute; inset: 0;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  pointer-events: none;
}
.ring-val {
  font-size: 20px; font-weight: 700; color: var(--text);
  line-height: 1; font-variant-numeric: tabular-nums;
}
.ring-val small { font-size: 12px; font-weight: 600; color: var(--text-3); margin-left: 1px; }
.ring-label { font-size: 11.5px; color: var(--text-3); margin-top: 4px; }
</style>