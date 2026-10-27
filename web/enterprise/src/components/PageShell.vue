<template>
  <!-- 页面骨架：全站统一的内容层包裹（规则见 styles/pages.css 与 docs/ui-design.md） -->
  <section class="page" :data-testid="testid || undefined">
    <header class="page__head">
      <div>
        <h2 class="page__title" :data-testid="testid ? testid + '-title' : undefined">{{ title }}</h2>
        <p v-if="desc" class="page__desc">{{ desc }}</p>
      </div>
      <div v-if="$slots.actions" class="page__actions">
        <slot name="actions" />
      </div>
    </header>
    <div v-if="error" class="poll-err" :data-testid="testid ? testid + '-error' : undefined">{{ error }}</div>
    <slot />
  </section>
</template>

<script setup>
// 统一页面骨架（2026-10-27 布局专项）：标题/描述/操作区/错误横幅的唯一实现。
// 用法：
//   <PageShell :title="$t('x.title')" :desc="$t('x.subtitle')" testid="x" :error="store.error">
//     <div class="btnbar">…</div>
//     <DataTable … />
//   </PageShell>
defineProps({
  title: { type: String, required: true },
  desc: { type: String, default: '' },
  // data-testid 前缀：外层 section 用 testid 本身，标题用 testid-title，错误横幅 testid-error
  testid: { type: String, default: '' },
  // 错误横幅文本（由调用方传入 store.error 等；为空则不渲染）
  error: { type: String, default: '' }
})
</script>
