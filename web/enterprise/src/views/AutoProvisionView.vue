<template>
  <div>
    <h2 data-testid="auto-provision-title">{{ $t('provision.title') }}</h2>
    <p class="muted">{{ $t('provision.subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="card">
      <h3>{{ $t('provision.form_title') }}</h3>
      <form @submit.prevent="onSubmit">
        <div class="row-2">
          <div class="field">
            <label>{{ $t('provision.field_segment') }}</label>
            <input v-model.trim="form.segment" type="text" :placeholder="$t('provision.segment_placeholder')" required />
          </div>
          <div class="field">
            <label>{{ $t('provision.field_agentVersion') }}</label>
            <input v-model.trim="form.agentVersion" type="text" :placeholder="$t('provision.agentVersion_placeholder')" />
          </div>
        </div>
        <div class="btnbar">
          <button type="submit" class="primary" :disabled="store.loading" data-testid="auto-provision-btn">
            <Icon name="success" :size="14" />
            {{ store.loading ? $t('common.loading') : $t('provision.submit') }}
          </button>
        </div>
        <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
      </form>
    </div>

    <!-- 结果展示 -->
    <div class="card" v-if="store.result">
      <h3>{{ $t('provision.result_title') }}</h3>
      <div class="stats">
        <div class="stat indigo">
          <div class="stat-v">{{ store.result.discovered ?? 0 }}</div>
          <div class="stat-l">{{ $t('provision.stat_discovered') }}</div>
        </div>
        <div class="stat teal">
          <div class="stat-v">{{ store.result.provisioned ?? 0 }}</div>
          <div class="stat-l">{{ $t('provision.stat_provisioned') }}</div>
        </div>
        <div class="stat rose">
          <div class="stat-v">{{ store.result.failed ?? 0 }}</div>
          <div class="stat-l">{{ $t('provision.stat_failed') }}</div>
        </div>
      </div>

      <h4>{{ $t('provision.devices_title') }}</h4>
      <DataTable
        :columns="deviceColumns"
        :rows="store.result.devices || []"
        row-key="ip"
        :empty-text="$t('provision.empty_devices')"
      >
        <template #cell-status="{ value }">
          <span class="status-pill" :class="statusClass(value)">{{ value || '—' }}</span>
        </template>
      </DataTable>
    </div>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="auto-provision-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 自动纳管页 — 触发表单（网段 + agent 版本）+ 结果展示（发现/纳管/失败数量 + 设备列表）
import { ref, reactive, computed, onMounted } from 'vue'
import { useProvisionStore } from '@/stores/provision'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useProvisionStore()
const msg = ref('')
const msgOk = ref(false)
const errorConfirm = reactive({ show: false, message: '' })

const form = ref({ segment: '', agentVersion: '' })

const deviceColumns = computed(() => [
  { key: 'ip', title: t('provision.col_ip') },
  { key: 'hostname', title: t('provision.col_hostname') },
  { key: 'status', title: t('provision.col_status'), slot: 'cell-status' }
])

function statusClass(s) {
  if (s === 'provisioned' || s === 'success') return 'ok'
  if (s === 'failed' || s === 'error') return 'off'
  if (s === 'discovered' || s === 'pending') return 'warn'
  return 'off'
}

async function onSubmit() {
  msg.value = ''
  try {
    await store.autoProvision({
      segment: form.value.segment,
      agentVersion: form.value.agentVersion
    })
    msg.value = t('provision.success')
    msgOk.value = true
  } catch (e) {
    msg.value = t('provision.failed') + (e.j?.error || e.message || '')
    msgOk.value = false
  }
}

onMounted(() => {})
</script>

<style scoped>
.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h3 { margin: 0 0 12px; font-size: 14px; }
.card h4 { margin: 16px 0 8px; font-size: 13px; }
.btnbar { display: flex; gap: 8px; margin-top: 8px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.field label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-top: 4px; }
.stat {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 12px 14px;
  border-top: 3px solid var(--accent);
}
.stat.rose { border-top-color: var(--rose); }
.stat.indigo { border-top-color: var(--indigo); }
.stat.teal { border-top-color: var(--teal); }
.stat-v { font-size: 22px; font-weight: 700; color: var(--text); line-height: 1.1; font-variant-numeric: tabular-nums; }
.stat-l { font-size: 11.5px; color: var(--text-3); margin-top: 4px; }

.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.warn { background: var(--warn-soft, #fef3c7); color: var(--warn, #b45309); }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

@media (max-width: 768px) {
  .row-2 { grid-template-columns: 1fr; }
  .stats { grid-template-columns: 1fr; }
}
</style>