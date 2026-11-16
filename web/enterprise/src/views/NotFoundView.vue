<template>
  <!-- 404 兜底：未知路由统一落到这里（替代白屏）。
       语义：路由未找到 ≠ 服务器错误；文案请与 PageShell 其他页一致。 -->
  <PageShell :title="$t('not_found.title')" :desc="$t('not_found.subtitle')" testid="not-found">
    <div class="card not-found" data-testid="not-found-card">
      <p class="not-found__code" aria-hidden="true">404</p>
      <p class="not-found__text">{{ $t('not_found.path') }}</p>
      <code class="not-found__path">{{ $route.fullPath }}</code>
      <div class="not-found__actions">
        <Button variant="primary" data-testid="not-found-home-btn" @click="goHome">
          {{ $t('not_found.go_home') }}
        </Button>
        <Button variant="ghost" data-testid="not-found-back-btn" @click="goBack">
          {{ $t('not_found.go_back') }}
        </Button>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import { useRouter, useRoute } from 'vue-router'
import PageShell from '@/components/PageShell.vue'
import { Button } from '@/components/ui'

const router = useRouter()
const route = useRoute()
function goHome() {
  router.push('/overview')
}
function goBack() {
  if (window.history.length > 1) router.back()
  else router.push('/overview')
}
</script>

<style scoped>
.not-found {
  display: grid;
  gap: 14px;
  padding: 32px 28px;
  text-align: center;
}
.not-found__code {
  font-size: 64px;
  line-height: 1;
  font-weight: 700;
  color: var(--text-muted, #6b7280);
  letter-spacing: 4px;
}
.not-found__text {
  color: var(--text-muted, #6b7280);
  margin: 0;
}
.not-found__path {
  display: inline-block;
  max-width: 100%;
  overflow: auto;
  font-size: 0.85rem;
  padding: 6px 10px;
  background: var(--surface-2, #f3f4f6);
  border-radius: 6px;
  word-break: break-all;
}
.not-found__actions {
  display: flex;
  gap: 10px;
  justify-content: center;
  flex-wrap: wrap;
}
</style>
