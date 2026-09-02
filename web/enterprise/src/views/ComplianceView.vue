<template>
  <div>
    <h2>{{ $t('compliance.title') }}</h2>
    <p class="muted">{{ $t('compliance.subtitle') }}</p>

    <div class="btnbar">
      <button class="outline" @click="fetchRules">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <!-- 规则目录 -->
      <DataTable :columns="ruleColumns" :rows="rules" row-key="id" :empty-text="$t('compliance.empty')">
        <template #cell-id="{ value }"><code>{{ value }}</code></template>
        <template #cell-category="{ value }">
          <span class="tag">{{ value }}</span>
        </template>
        <template #cell-severity="{ value }">
          <span class="tag" :class="'sev-' + value">{{ $t('compliance.severity_' + value) }}</span>
        </template>
      </DataTable>

      <!-- 触发扫描 -->
      <h3 class="section-title">{{ $t('compliance.scan') }}</h3>
      <form class="scan-form" @submit.prevent="onScan">
        <div class="field">
          <label>{{ $t('compliance.device_label') }}</label>
          <input v-model.trim="scanDeviceID" type="text" :placeholder="$t('compliance.device_placeholder')" required />
        </div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('compliance.run_scan') }}</button>
        </div>
      </form>

      <!-- 扫描报告 -->
      <h3 class="section-title">{{ $t('compliance.reports') }}</h3>
      <DataTable :columns="reportColumns" :rows="reports" row-key="id" :empty-text="$t('compliance.no_reports')">
        <template #cell-deviceID="{ value }"><code>{{ value }}</code></template>
        <template #cell-score="{ value }">
          <span class="tag" :class="value >= 80 ? 'score-high' : value >= 60 ? 'score-mid' : 'score-low'">{{ value }}</span>
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openDetail(row)"><Icon name="edit" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 报告详情抽屉 -->
    <DetailDrawer :open="!!detail" :title="$t('compliance.report_detail')" @close="detail = null">
      <div v-if="detail" class="report-detail">
        <div class="meta">
          <span>{{ $t('compliance.device_label') }}: <code>{{ detail.deviceID }}</code></span>
          <span>{{ $t('compliance.score_label') }}: <b>{{ detail.score }}</b></span>
          <span>{{ fmtTime(detail.createdAt) }}</span>
        </div>
        <DataTable :columns="detailColumns" :rows="detail.results || []" :empty-text="$t('compliance.no_results')">
          <template #cell-ruleId="{ value }"><code>{{ value }}</code></template>
          <template #cell-passed="{ value }">
            <span class="tag" :class="value ? 'pass' : 'nopass'">{{ value ? $t('compliance.passed') : $t('compliance.failed') }}</span>
          </template>
          <template #cell-output="{ value }"><code class="output">{{ value }}</code></template>
        </DataTable>
      </div>
    </DetailDrawer>

    <!-- 删除确认（保留位置：本域无删除，报告由扫描生成） -->
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="compliance-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 合规管理页 — 规则目录 + 触发设备扫描 + 报告列表与详情
// 规则为引擎预置（只读）；扫描需 deviceID，POST /compliance/scan 后刷新报告列表
import { ref, reactive, onMounted } from 'vue'
import * as complianceApi from '@/api/compliance'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const rules = ref([])
const reports = ref([])
const loading = ref(false)
const scanDeviceID = ref('')
const detail = ref(null)

// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

const ruleColumns = [
  { key: 'id', title: 'ID', slot: 'cell-id', width: '120px' },
  { key: 'name', title: t('compliance.rule_name') },
  { key: 'category', title: t('compliance.category'), slot: 'cell-category', width: '90px' },
  { key: 'severity', title: t('compliance.severity'), slot: 'cell-severity', width: '80px' },
  { key: 'description', title: t('compliance.description') },
  { key: 'remediation', title: t('compliance.remediation') }
]

const reportColumns = [
  { key: 'id', title: 'ID' },
  { key: 'deviceID', title: t('compliance.device'), slot: 'cell-deviceID' },
  { key: 'score', title: t('compliance.score'), slot: 'cell-score', width: '80px' },
  { key: 'createdAt', title: t('compliance.created_at'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('compliance.actions'), slot: 'cell-actions', width: '60px' }
]

const detailColumns = [
  { key: 'ruleId', title: t('compliance.rule'), slot: 'cell-ruleId' },
  { key: 'passed', title: t('compliance.result'), slot: 'cell-passed', width: '80px' },
  { key: 'output', title: t('compliance.output'), slot: 'cell-output' }
]

async function fetchAll() {
  loading.value = true
  try {
    const [rr, pr] = await Promise.all([complianceApi.listRules(), complianceApi.listReports()])
    rules.value = (rr && rr.rules) || []
    reports.value = (pr && pr.reports) || []
  } catch {
    rules.value = []
    reports.value = []
  } finally {
    loading.value = false
  }
}

function fetchRules() {
  fetchAll()
}

async function onScan() {
  if (!scanDeviceID.value) {
    errorConfirm.message = t('compliance.device_required')
    errorConfirm.show = true
    return
  }
  try {
    // results 留空：后端用引擎规则生成占位结果（实际执行由 agent 任务下发完成）
    await complianceApi.scanDevice({ deviceID: scanDeviceID.value })
    toast.success(t('compliance.scan_started'))
    await fetchAll()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('compliance.scan_failed')
    errorConfirm.show = true
  }
}

async function openDetail(row) {
  try {
    const r = await complianceApi.getReport(row.id)
    detail.value = r || row
  } catch {
    // 详情拉取失败时降级用列表行数据（结果可能缺失）
    detail.value = row
  }
}

onMounted(fetchAll)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.section-title { margin: 22px 0 4px; font-size: 15px; }
.tag {
  display: inline-block; padding: 2px 8px; border-radius: 999px;
  font-size: 12px; background: var(--accent-soft); color: var(--accent);
}
.tag.sev-high { background: var(--fail-bg); color: var(--fail); }
.tag.sev-medium { background: var(--warn-bg); color: var(--warn); }
.tag.score-high { background: var(--ok-bg); color: var(--ok); }
.tag.score-mid { background: var(--warn-bg); color: var(--warn); }
.tag.score-low { background: var(--fail-bg); color: var(--fail); }
.tag.pass { background: var(--ok-bg); color: var(--ok); }
.tag.nopass { background: var(--fail-bg); color: var(--fail); }
.scan-form { display: flex; flex-direction: column; gap: 8px; max-width: 380px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.report-detail { display: flex; flex-direction: column; gap: 10px; }
.meta { display: flex; gap: 16px; flex-wrap: wrap; font-size: 13px; color: var(--text-2); }
.output { font-size: 12px; }
</style>
