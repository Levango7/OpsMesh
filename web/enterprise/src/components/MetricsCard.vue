<template>
  <!--
    通用指标卡片 — 统一标题 + 内容插槽
    - title：卡片标题
    - icon：可选图标名（Icon 组件 name）
    - accent：可选标题强调色（CSS 变量名，如 --indigo）
    - 默认插槽：卡片主体内容
    - actions 插槽：右上角操作区
  -->
  <section class="metrics-card">
    <header class="mc-head">
      <div class="mc-title">
        <span v-if="icon" class="mc-icon" :style="accent ? { color: `var(${accent})` } : null">
          <Icon :name="icon" :size="16" />
        </span>
        <h3>{{ title }}</h3>
      </div>
      <div v-if="$slots.actions" class="mc-actions">
        <slot name="actions" />
      </div>
    </header>
    <div class="mc-body">
      <slot />
    </div>
  </section>
</template>

<script setup>
// 通用指标卡片：标题 + 图标 + 内容插槽 + 操作插槽
import Icon from '@/components/Icon.vue'

defineProps({
  title: { type: String, required: true },
  icon: { type: String, default: '' },
  accent: { type: String, default: '' }
})
</script>

<style scoped>
.metrics-card {
  /* 统一层级：一层 L1（主卡片）+ 清晰的边框 */
  background: var(--color-surface-1, #ffffff);
  border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: var(--radius-md, 8px);
  padding: 16px 18px;
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.05));
  margin-bottom: 16px;
}
.mc-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 12px;
}
.mc-title { display: flex; align-items: center; gap: 8px; }
.mc-icon {
  width: 28px; height: 28px; border-radius: 8px;
  /* 三层 L3（图标底衬），与 L1 卡片形成层级差 */
  background: var(--color-surface-3, #eef1f7);
  display: inline-flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.mc-title h3 { margin: 0; font-size: 14px; }
.mc-actions { display: inline-flex; align-items: center; gap: 6px; }
.mc-body { font-size: 13px; color: var(--color-ink-muted, #6b7280); }
</style>