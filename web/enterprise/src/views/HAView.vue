<template>
  <div>
    <h2>{{ $t('ha.title') }}</h2>
    <p class="muted">{{ $t('ha.subtitle') }}</p>

    <div class="btnbar">
      <button class="outline" @click="fetchAll">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
      <button class="danger" @click="onFailover">
        <Icon name="warning" :size="14" />
        {{ $t('ha.failover') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <!-- 状态卡片 -->
      <div class="ha-cards">
        <div class="ha-card">
          <div class="ha-card-label">{{ $t('ha.leader') }}</div>
          <div class="ha-card-value"><code>{{ status?.leader?.instanceID || '—' }}</code></div>
          <div class="ha-card-sub">{{ $t('ha.role_' + (status?.leader?.role || 'leader')) }}</div>
        </div>
        <div class="ha-card">
          <div class="ha-card-label">{{ $t('ha.current') }}</div>
          <div class="ha-card-value"><code>{{ status?.current?.instanceID || '—' }}</code></div>
          <div class="ha-card-sub">{{ $t('ha.role_' + (status?.current?.role || 'follower')) }}</div>
        </div>
        <div class="ha-card">
          <div class="ha-card-label">{{ $t('ha.replicas') }}</div>
          <div class="ha-card-value">{{ status?.replicas ?? '—' }}</div>
          <div class="ha-card-sub">{{ $t('ha.replicas_hint') }}</div>
        </div>
        <div class="ha-card">
          <div class="ha-card-label">{{ $t('ha.health') }}</div>
          <div class="ha-card-value">
            <span class="tag" :class="health?.status === 'healthy' ? 'ok' : 'bad'">
              {{ health?.status || '—' }}
            </span>
          </div>
          <div class="ha-card-sub">{{ fmtTime(health?.timestamp) }}</div>
        </div>
      </div>

      <!-- 实例列表 -->
      <h3 class="section-title">{{ $t('ha.instances') }}</h3>
      <DataTable :columns="columns" :rows="instances" row-key="instanceID" :empty-text="$t('ha.empty')">
        <template #cell-instanceID="{ value }"><code>{{ value }}</code></template>
        <template #cell-role="{ value }">
          <span class="tag" :class="value === 'leader' ? 'leader' : 'follower'">{{ value }}</span>
        </template>
        <template #cell-ports="{ row }">{{ row.httpPort }} / {{ row.grpcPort }}</template>
        <template #cell-isLeader="{ value }">
          <span class="tag" :class="value ? 'ok' : 'dim'">{{ value ? $t('common.yes') : $t('common.no') }}</span>
        </template>
      </DataTable>
    </div>

    <!-- 手动切换 leader 二次确认（高危操作，替代 confirm） -->
    <ConfirmModal
      v-model="failoverConfirm.show"
      data-testid="ha-failover-confirm-modal"
      :title="$t('ha.failover')"
      :message="$t('ha.confirm_failover')"
      :confirm-text="$t('common.confirm')"
      @confirm="onFailoverConfirm"
    />
    <!-- 错误/结果提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="ha-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 高可用状态页 — 无 CRUD：status 卡片 + instances 实例表 + failover 高危按钮（二次确认）
import { ref, reactive, onMounted } from 'vue'
import * as haApi from '@/api/ha'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const status = ref(null)
const instances = ref([])
const health = ref(null)
const loading = ref(false)

// failover 二次确认弹窗（高危操作）
const failoverConfirm = reactive({ show: false })
// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

const columns = [
  { key: 'instanceID', title: t('ha.instance_id'), slot: 'cell-instanceID' },
  { key: 'hostname', title: t('ha.hostname') },
  { key: 'role', title: t('ha.role'), slot: 'cell-role', width: '100px' },
  { key: 'ports', title: t('ha.ports'), slot: 'cell-ports', width: '110px' },
  { key: 'isLeader', title: t('ha.is_leader'), slot: 'cell-isLeader', width: '90px' }
]

async function fetchAll() {
  loading.value = true
  try {
    const [st, ins, hl] = await Promise.all([haApi.getStatus(), haApi.getInstances(), haApi.getHealth()])
    status.value = st || null
    // instances 接口返回 {instances, count}；status 里也带 instances，优先用专口数据
    instances.value = (ins && ins.instances) || (st && st.instances) || []
    health.value = hl || null
  } catch {
    status.value = null
    instances.value = []
    health.value = null
  } finally {
    loading.value = false
  }
}

function onFailover() {
  // 高危操作必须二次确认，不直接执行
  failoverConfirm.show = true
}

async function onFailoverConfirm() {
  try {
    const { j } = await haApi.failover()
    // 返回 message 透传展示（failover 已受理，实际选主由 leader_lease 续租驱动）
    errorConfirm.message = (j && j.message) || t('ha.failover_done')
    errorConfirm.show = true
    await fetchAll()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('ha.failover_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchAll)
</script>

<style scoped>
.ha-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; margin-top: 6px; }
.ha-card {
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 14px 16px;
}
.ha-card-label { font-size: 12px; color: var(--text-3); text-transform: uppercase; letter-spacing: .04em; }
.ha-card-value { margin-top: 6px; font-size: 15px; font-weight: 600; word-break: break-all; }
.ha-card-sub { margin-top: 4px; font-size: 12.5px; color: var(--text-3); }
.section-title { margin: 22px 0 4px; font-size: 15px; }
.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.ok { background: var(--ok-bg); color: var(--ok); }
.tag.bad { background: var(--fail-bg); color: var(--fail); }
.tag.dim { background: var(--surface-3); color: var(--text-3); }
.tag.leader { background: var(--ok-bg); color: var(--ok); }
.tag.follower { background: var(--surface-3); color: var(--text-2); }
</style>
