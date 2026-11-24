<template>
  <PageShell :title="$t('lakehouse.title')" :desc="$t('lakehouse.subtitle')" testid="lakehouse">
    <div class="toolbar">
      <Button variant="ghost" size="md" @click="refresh" :disabled="loading">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>
    <div v-if="errorMsg" class="poll-err" data-testid="lakehouse-error">{{ errorMsg }}</div>
    <div class="tabbar" data-testid="lakehouse-tabs">
      <Button
        v-for="t in tabs" :key="t.key" variant="ghost" size="md"
        :class="['tab', { active: activeTab === t.key }]" @click="activeTab = t.key"
        :data-testid="'lakehouse-tab-' + t.key"
      >{{ $t('lakehouse.tab_' + t.key) }}</Button>
    </div>
    <div class="result" v-if="result" data-testid="lakehouse-result">
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
const activeTab = ref('tables')
const result = ref(null)
const resultText = computed(() => (result.value ? JSON.stringify(result.value, null, 2) : ''))

const tabs = [{ key: 'tables' }, { key: 'maintenance' }, { key: 'jobs' }, { key: 'runs' }]

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    if (activeTab.value === 'tables') result.value = await postJSON('/lakehouse/tables', { action: 'list' })
    else if (activeTab.value === 'maintenance') result.value = await postJSON('/lakehouse/maintenance', { action: 'list' })
    else if (activeTab.value === 'jobs') result.value = await postJSON('/lakehouse/jobs', { action: 'list' })
    else result.value = await getJSON('/lakehouse/runs')
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
