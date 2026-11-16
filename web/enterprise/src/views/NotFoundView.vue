<template>
  <PageShell title="404" testid="not-found">
    <div class="not-found">
      <h1 class="not-found__code">404</h1>
      <p class="not-found__msg">{{ $t('common.not_found') }}</p>
      <Button variant="primary" @click="goHome">
        {{ $t('nav.home') }}
      </Button>
    </div>
  </PageShell>
</template>

<script setup>
import { useRouter } from 'vue-router'
import PageShell from '@/components/PageShell.vue'
import Button from '@/components/ui/Button.vue'

const router = useRouter()
function goHome() {
  router.push('/')
}
</script>

<style scoped>
.not-found {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  padding: 48px 24px;
  text-align: center;
}
.not-found__code {
  font-size: 64px;
  font-weight: 700;
  line-height: 1;
  color: var(--color-primary, #5e6ad2);
  margin: 0;
}
.not-found__msg {
  font-size: 16px;
  color: var(--color-text-secondary, #666);
  margin: 0;
}
</style>
