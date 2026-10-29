<template>
  <PageShell
    :title="$t('autoscaler.title')"
    :desc="$t('autoscaler.desc')"
    testid="autoscaler"
    :error="store.error"
  >
    <div class="row">
      <!-- 左：扩缩容规则 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('autoscaler.rules') }}</h3>
            <Button variant="primary" size="sm" @click="openAddRule" data-testid="autoscaler-add-btn">{{ $t('autoscaler.addRule') }}</Button>
            <Button variant="ghost" size="sm" @click="store.fetchRules()">↻ {{ $t('common.refresh') }}</Button>
          </div>
          <div v-if="store.loading && !store.rules.length" class="muted">{{ $t('common.loading') }}</div>
          <DataTable v-else :columns="ruleCols" :rows="store.rules" row-key="id" :empty-text="$t('autoscaler.noRules')">
            <template #cell-name="{ row }">
              <b>{{ row.name }}</b><br><code>{{ row.id }}</code>
            </template>
            <template #cell-metric="{ value }"><code>{{ value || '-' }}</code></template>
            <template #cell-enabled="{ value }">
              <StatusBadge :status="value ? 'success' : 'info'" :text="value ? $t('autoscaler.enabled') : $t('autoscaler.disabled')" />
            </template>
            <template #cell-threshold="{ value }">{{ value || 0 }}%</template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <Button variant="ghost" size="sm" @click="onToggleRule(row)" data-testid="autoscaler-toggle-btn">{{ row.enabled ? $t('autoscaler.disable') : $t('autoscaler.enable') }}</Button>
                <Button variant="ghost" size="sm" style="color: var(--fail); border-color: var(--fail)" @click="onDeleteRule(row.id)" data-testid="autoscaler-delete-btn">{{ $t('common.delete') }}</Button>
              </div>
            </template>
          </DataTable>
        </div>

        <!-- 冷却状态 -->
        <div class="card">
          <h3>{{ $t('autoscaler.cooldowns') }}</h3>
          <div v-if="!store.cooldowns.length" class="muted">{{ $t('autoscaler.noCooldowns') }}</div>
          <DataTable v-else :columns="cooldownCols" :rows="store.cooldowns" row-key="ruleId" :empty-text="$t('autoscaler.noCooldowns')">
            <template #cell-remaining="{ value }">
              <span class="cooldown-badge">{{ value }}s</span>
            </template>
          </DataTable>
        </div>
      </div>

      <!-- 右：决策历史 + 手动触发 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('autoscaler.decisions') }}</h3>
            <Button variant="ghost" size="sm" @click="store.fetchDecisions()">↻ {{ $t('common.refresh') }}</Button>
          </div>
          <div v-if="store.decisionsLoading && !store.decisions.length" class="muted">{{ $t('common.loading') }}</div>
          <div v-else-if="!store.decisions.length" class="muted">{{ $t('autoscaler.noDecisions') }}</div>
          <div v-else class="chart-area">
            <div class="mock-chart">
              <div
                v-for="(d, idx) in store.decisions.slice(0, 20)"
                :key="idx"
                class="chart-bar"
                :class="d.action === 'scale-up' ? 'bar-up' : 'bar-down'"
                :style="{ height: barHeight(d) + '%' }"
                :title="d.action + ': ' + d.fromReplicas + ' → ' + d.toReplicas"
              />
            </div>
          </div>
          <DataTable v-if="store.decisions.length" :columns="decisionCols" :rows="store.decisions.slice(0, 10)" row-key="id" :empty-text="$t('autoscaler.noDecisions')">
            <template #cell-action="{ value }">
              <StatusBadge :status="value === 'scale-up' ? 'success' : 'info'" :text="value || '-'" />
            </template>
          </DataTable>
        </div>

        <!-- 手动触发 -->
        <div class="card">
          <h3>{{ $t('autoscaler.manualScale') }}</h3>
          <div class="field">
            <label>{{ $t('autoscaler.target') }}</label>
            <input v-model="scaleForm.target" placeholder="deployment/my-app" data-testid="autoscaler-scale-target" />
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.replicas') }}</label>
            <input v-model.number="scaleForm.replicas" type="number" min="0" data-testid="autoscaler-scale-replicas" />
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.reason') }}</label>
            <input v-model="scaleForm.reason" :placeholder="$t('autoscaler.reasonPlaceholder')" data-testid="autoscaler-scale-reason" />
          </div>
          <div class="btnbar">
            <Button variant="primary" size="md" @click="onManualScale" :disabled="scaling" data-testid="autoscaler-scale-btn">{{ $t('autoscaler.trigger') }}</Button>
          </div>
          <p v-if="scaleMsg" :class="['msg', scaleOk ? 'ok' : 'err']">{{ scaleMsg }}</p>
        </div>
      </div>
    </div>

    <!-- 添加规则对话框 -->
    <div v-if="addOpen" class="modal-mask" data-testid="autoscaler-add-modal" @click.self="addOpen = false">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('autoscaler.addRule') }}</h3>
          <Button variant="ghost" size="sm" @click="addOpen = false">✕</Button>
        </header>
        <div class="modal-body">
          <div class="field">
            <label>{{ $t('autoscaler.ruleName') }}</label>
            <input v-model="addForm.name" required data-testid="autoscaler-add-name" />
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.metric') }}</label>
            <select v-model="addForm.metric" data-testid="autoscaler-add-metric">
              <option value="cpu">CPU</option>
              <option value="memory">Memory</option>
              <option value="requests">Requests</option>
              <option value="custom">Custom</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.threshold') }} (%)</label>
            <input v-model.number="addForm.threshold" type="number" min="1" max="100" data-testid="autoscaler-add-threshold" />
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.minReplicas') }}</label>
            <input v-model.number="addForm.minReplicas" type="number" min="0" data-testid="autoscaler-add-min" />
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.maxReplicas') }}</label>
            <input v-model.number="addForm.maxReplicas" type="number" min="1" data-testid="autoscaler-add-max" />
          </div>
          <div class="field">
            <label>{{ $t('autoscaler.cooldown') }} (s)</label>
            <input v-model.number="addForm.cooldown" type="number" min="0" data-testid="autoscaler-add-cooldown" />
          </div>
          <div class="btnbar">
            <Button variant="primary" size="md" @click="confirmAddRule" :disabled="adding" data-testid="autoscaler-add-confirm">{{ $t('common.confirm') }}</Button>
            <Button variant="ghost" size="md" @click="addOpen = false">{{ $t('common.cancel') }}</Button>
          </div>
          <p v-if="addMsg" :class="['msg', addOk ? 'ok' : 'err']">{{ addMsg }}</p>
        </div>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import { computed, onMounted, ref } from 'vue'
import { useAutoscalerStore } from '@/stores/autoscaler'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Button from "@/components/ui/Button.vue"

const store = useAutoscalerStore()

const ruleCols = computed(() => [
  { key: 'name', title: t('autoscaler.col.rule'), slot: 'cell-name' },
  { key: 'metric', title: t('autoscaler.col.metric'), slot: 'cell-metric' },
  { key: 'threshold', title: t('autoscaler.col.threshold'), slot: 'cell-threshold' },
  { key: 'enabled', title: t('autoscaler.col.enabled'), slot: 'cell-enabled' },
  { key: 'actions', title: t('autoscaler.col.action'), slot: 'cell-actions', width: '180px' }
])

const cooldownCols = computed(() => [
  { key: 'ruleName', title: t('autoscaler.col.rule') },
  { key: 'remaining', title: t('autoscaler.col.remaining'), slot: 'cell-remaining' },
  { key: 'expiresAt', title: t('autoscaler.col.expiresAt') }
])

const decisionCols = computed(() => [
  { key: 'id', title: t('autoscaler.col.decisionId') },
  { key: 'action', title: t('autoscaler.col.action'), slot: 'cell-action' },
  { key: 'fromReplicas', title: t('autoscaler.col.from') },
  { key: 'toReplicas', title: t('autoscaler.col.to') },
  { key: 'timestamp', title: t('autoscaler.col.timestamp') }
])

function barHeight(d) {
  const delta = Math.abs((d.toReplicas || 0) - (d.fromReplicas || 0))
  return Math.min(100, Math.max(20, delta * 25))
}

// ---- 添加规则 ----
const addOpen = ref(false)
const addForm = ref({ name: '', metric: 'cpu', threshold: 80, minReplicas: 1, maxReplicas: 10, cooldown: 300 })
const addMsg = ref('')
const addOk = ref(false)
const adding = ref(false)

function openAddRule() {
  addOpen.value = true
  addForm.value = { name: '', metric: 'cpu', threshold: 80, minReplicas: 1, maxReplicas: 10, cooldown: 300 }
  addMsg.value = ''
}

async function confirmAddRule() {
  if (!addForm.value.name) { addMsg.value = t('autoscaler.nameRequired'); addOk.value = false; return }
  adding.value = true
  try {
    const r = await store.addRule(addForm.value.name, addForm.value.metric, addForm.value.threshold, addForm.value.minReplicas, addForm.value.maxReplicas, addForm.value.cooldown)
    if (r.s >= 200 && r.s < 300) {
      addMsg.value = t('autoscaler.addSuccess'); addOk.value = true
      await store.fetchRules()
      setTimeout(() => { addOpen.value = false }, 1200)
    } else {
      addMsg.value = r.j?.error || t('autoscaler.addFail'); addOk.value = false
    }
  } catch (e) {
    addMsg.value = e.j?.error || t('autoscaler.addFail'); addOk.value = false
  } finally {
    adding.value = false
  }
}

async function onToggleRule(row) {
  try {
    await store.editRule(row.id, { enabled: !row.enabled })
    await store.fetchRules()
  } catch (e) {
    alert(e.j?.error || t('autoscaler.toggleFail'))
  }
}

async function onDeleteRule(id) {
  if (!confirm(t('autoscaler.deleteConfirm'))) return
  try {
    const r = await store.removeRule(id)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      await store.fetchRules()
    }
  } catch (e) {
    alert(e.j?.error || t('autoscaler.deleteFail'))
  }
}

// ---- 手动触发 ----
const scaleForm = ref({ target: '', replicas: 1, reason: '' })
const scaleMsg = ref('')
const scaleOk = ref(false)
const scaling = ref(false)

async function onManualScale() {
  if (!scaleForm.value.target) { scaleMsg.value = t('autoscaler.targetRequired'); scaleOk.value = false; return }
  scaling.value = true
  try {
    const r = await store.triggerScale(scaleForm.value.target, scaleForm.value.replicas, scaleForm.value.reason)
    if (r.s >= 200 && r.s < 300) {
      scaleMsg.value = t('autoscaler.scaleSuccess'); scaleOk.value = true
      await store.fetchDecisions()
      await store.fetchCooldowns()
    } else {
      scaleMsg.value = r.j?.error || t('autoscaler.scaleFail'); scaleOk.value = false
    }
  } catch (e) {
    scaleMsg.value = e.j?.error || t('autoscaler.scaleFail'); scaleOk.value = false
  } finally {
    scaling.value = false
  }
}

onMounted(() => {
  store.fetchRules()
  store.fetchDecisions()
  store.fetchCooldowns()
})
</script>

<style scoped>
.row .col:nth-child(1) { flex: 45; }
.row .col:nth-child(2) { flex: 55; }

.cooldown-badge {
  display: inline-flex; align-items: center; height: 20px;
  padding: 0 9px; border-radius: 999px;
  font-size: 11.5px; font-weight: 600;
  background: var(--warn-bg); color: var(--warn);
}

.chart-area { padding: 12px 0; }
.mock-chart {
  display: flex; align-items: flex-end; gap: 3px; height: 100px; padding: 0 4px; margin-bottom: 12px;
}
.chart-bar {
  flex: 1; min-width: 6px; border-radius: 2px 2px 0 0;
  transition: height .3s; opacity: .8;
}
.chart-bar:hover { opacity: 1; }
.bar-up { background: var(--ok); }
.bar-down { background: var(--info); }

/* .modal-mask / .modal / .modal-head / .modal-body .field( label) 已改用
   styles/pages.css 全局对话框家族 A（2026-10-30 收编） */
</style>
