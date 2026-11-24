<template>
  <PageShell :title="$t('tuning.title')" :desc="$t('tuning.subtitle')" testid="tuning">
    <div class="toolbar">
      <Button variant="ghost" size="md" @click="refresh" :disabled="loading">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>
    <div v-if="errorMsg" class="poll-err" data-testid="tuning-error">{{ errorMsg }}</div>
    <div class="tabbar" data-testid="tuning-tabs">
      <Button
        v-for="t in tabs" :key="t.key" variant="ghost" size="md"
        :class="['tab', { active: activeTab === t.key }]" @click="activeTab = t.key"
        :data-testid="'tuning-tab-' + t.key"
      >{{ $t('tuning.tab_' + t.key) }}</Button>
    </div>
    <div class="result" v-if="result" data-testid="tuning-result">
      <pre>{{ resultText }}</pre>
    </div>
  </PageShell>
</template>
<script setup>
import PageShell from '@/components/PageShell.vue'
import Button from '@/components/ui/Button.vue'
import Icon from '@/components/Icon.vue'
import { computed, ref } from 'vue'
import { postJSON, getJSON } from '@/api/request'
import { toast } from '@/utils/toast'

const loading = ref(false)
const errorMsg = ref('')
const activeTab = ref('profiles')
const result = ref(null)
const resultText = computed(() => (result.value ? JSON.stringify(result.value, null, 2) : ''))

const tabs = [{ key: 'profiles' }, { key: 'recommend' }]

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    if (activeTab.value === 'profiles') result.value = await getJSON('/data/tuning/profiles')
    else result.value = await postJSON('/data/tuning/recommend', { count: 3, cores: 16, ramGB: 64, diskGB: 2000, workload: 'batch', dataScaleTB: 2, replication: 3 })
  } catch (e) {
    errorMsg.value = e?.message || String(e)
    toast.error(errorMsg.value)
  } finally {
    loading.value = false
  }
}
</script>
<style scoped>
.toolbar { display: flex; gap: 8px; margin-bottom: 12px; }
.tabbar { display: flex; gap: 4px; margin-bottom: 12px; flex-wrap: wrap; }
.tab.active { background: var(--color-primary, #3b82f6); color: #fff; }
.result { margin-top: 12px; }
.result pre { background: var(--color-surface, #f8f9fa); padding: 12px; border-radius: 6px; overflow: auto; max-height: 400px; font-size: 12px; }
</style>
