<template>
  <PageShell :title="$t('governance.title')" :desc="$t('governance.subtitle')" testid="governance">
    <div class="toolbar">
      <Button variant="ghost" size="md" @click="refresh" :disabled="loading">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>
    <div v-if="errorMsg" class="poll-err" data-testid="governance-error">{{ errorMsg }}</div>
    <div class="tabbar" data-testid="governance-tabs">
      <Button
        v-for="t in tabs" :key="t.key" variant="ghost" size="md"
        :class="['tab', { active: activeTab === t.key }]" @click="activeTab = t.key"
        :data-testid="'governance-tab-' + t.key"
      >{{ $t('governance.tab_' + t.key) }}</Button>
    </div>
    <div class="result" v-if="result" data-testid="governance-result">
      <pre>{{ resultText }}</pre>
    </div>
  </PageShell>
</template>
<script setup>
import PageShell from '@/components/PageShell.vue'
import Button from '@/components/ui/Button.vue'
import Icon from '@/components/Icon.vue'
import { computed, ref } from 'vue'
import { governanceApi } from '@/api/governance'
import { toast } from '@/utils/toast'

const loading = ref(false)
const errorMsg = ref('')
const activeTab = ref('rules')
const result = ref(null)
const resultText = computed(() => (result.value ? JSON.stringify(result.value, null, 2) : ''))

const tabs = [
  { key: 'rules' }, { key: 'pii' }, { key: 'retention' }, { key: 'catalog' }, { key: 'lineage' }
]

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    if (activeTab.value === 'rules') result.value = await governanceApi.listRules({})
    else if (activeTab.value === 'pii') result.value = await governanceApi.listPII({})
    else if (activeTab.value === 'retention') result.value = await governanceApi.listRetention({})
    else if (activeTab.value === 'catalog') result.value = await governanceApi.catalog({})
    else result.value = { info: 'use lineage action' }
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
