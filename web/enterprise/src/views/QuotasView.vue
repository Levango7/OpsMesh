<template>
  <div>
    <h2>{{ $t('quotas.title') }}</h2>
    <p class="muted">{{ $t('quotas.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openEdit">
        <Icon name="edit" :size="14" />
        {{ $t('quotas.edit') }}
      </button>
      <button class="outline" @click="fetchQuota">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else-if="!usage" class="muted">{{ $t('quotas.empty') }}</div>
    <div v-else>
      <!-- 配额/用量卡片（用量对比上限，0=不限） -->
      <div class="quota-cards">
        <div v-for="k in quotaKeys" :key="k" class="quota-card">
          <div class="quota-card-label">{{ $t('quotas.' + k) }}</div>
          <div class="quota-card-value">
            {{ usage[k] ?? 0 }}<span class="quota-sep">/</span>
            <span class="quota-limit">{{ limit(k) }}</span>
          </div>
          <div class="quota-bar">
            <div class="quota-bar-fill" :style="{ width: pct(k) }" :class="{ over: overRatio(k) }"></div>
          </div>
        </div>
      </div>
      <p class="muted">{{ $t('quotas.unlimited_hint') }}</p>
    </div>

    <!-- 编辑配额抽屉 -->
    <DetailDrawer :open="!!form" :title="$t('quotas.edit')" @close="form = null">
      <form v-if="form" class="quota-form" @submit.prevent="onSave">
        <div v-for="k in quotaKeys" :key="k" class="field">
          <label>{{ $t('quotas.max_' + k) }}</label>
          <input v-model.number="form[k]" type="number" min="0" step="1" />
        </div>
        <p class="muted">{{ $t('quotas.unlimited_hint') }}</p>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('quotas.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('quotas.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 重置确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="quotas-delete-confirm-modal"
      :title="$t('quotas.reset')"
      :message="$t('quotas.confirm_reset')"
      @confirm="onResetConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="quotas-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 租户配额页 — 当前租户配额/用量视图 + 编辑（PUT）+ 重置（DELETE，回退默认）
// GET /quotas 返回 {enabled, current: QuotaUsage}（MVP：当前租户视图）
import { ref, reactive, computed, onMounted } from 'vue'
import * as quotaApi from '@/api/quota'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const usage = ref(null)
const enabled = ref(false)
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 重置确认弹窗
const deleteConfirm = reactive({ show: false })
// 错误提示弹窗
const errorConfirm = reactive({ show: false, message: '' })

// 三类配额维度：devices/tasks/alerts
const quotaKeys = ['devices', 'tasks', 'alerts']

// 当前租户 ID：取用量数据里后端回传的 tenantID（MVP 后端只认网关注入的 X-Tenant-ID，
// PUT/DELETE /quotas/{tenantID} 的 path 段须与其一致，回退 'default'）
const tenantID = computed(() => usage.value?.tenantID || 'default')

// 上限显示：0=不限
function limit(k) {
  const v = usage.value?.quota?.['max' + k[0].toUpperCase() + k.slice(1)]
  return v > 0 ? v : '∞'
}

// 用量占比（百分比字符串；上限 0=不限时按 0 处理）
function pct(k) {
  const max = usage.value?.quota?.['max' + k[0].toUpperCase() + k.slice(1)] || 0
  const cur = usage.value?.[k] || 0
  if (max <= 0) return cur > 0 ? '100%' : '0%'
  return Math.min(100, Math.round((cur / max) * 100)) + '%'
}

function overRatio(k) {
  const max = usage.value?.quota?.['max' + k[0].toUpperCase() + k.slice(1)] || 0
  const cur = usage.value?.[k] || 0
  return max > 0 && cur > max
}

async function fetchQuota() {
  loading.value = true
  try {
    const r = await quotaApi.listQuotas()
    enabled.value = !!(r && r.enabled)
    usage.value = (r && r.current) || null
  } catch (e) {
    usage.value = null
  } finally {
    loading.value = false
  }
}

function openEdit() {
  formError.value = ''
  const q = usage.value?.quota || {}
  form.value = {
    maxDevices: q.maxDevices || 0,
    maxTasks: q.maxTasks || 0,
    maxAlerts: q.maxAlerts || 0
  }
}

async function onSave() {
  formError.value = ''
  const v = form.value
  if ([v.maxDevices, v.maxTasks, v.maxAlerts].some((n) => n < 0 || !Number.isInteger(n))) {
    formError.value = t('quotas.value_invalid')
    return
  }
  try {
    await quotaApi.setQuota(tenantID.value, {
      maxDevices: v.maxDevices,
      maxTasks: v.maxTasks,
      maxAlerts: v.maxAlerts
    })
    toast.success(t('quotas.saved'))
    form.value = null
    await fetchQuota()
  } catch (e) {
    formError.value = e.j?.error || t('quotas.save_failed')
  }
}

function onReset() {
  deleteConfirm.show = true
}

async function onResetConfirm() {
  try {
    await quotaApi.deleteQuota(tenantID.value)
    toast.success(t('quotas.reset_done'))
    await fetchQuota()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('quotas.reset_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchQuota)
</script>

<style scoped>
.quota-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; margin-top: 6px; }
.quota-card {
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 14px 16px;
}
.quota-card-label { font-size: 12px; color: var(--text-3); text-transform: uppercase; letter-spacing: .04em; }
.quota-card-value { margin-top: 6px; font-size: 17px; font-weight: 600; }
.quota-sep { margin: 0 4px; color: var(--text-3); font-weight: 400; }
.quota-limit { color: var(--text-2); font-size: 14px; font-weight: 500; }
.quota-bar { margin-top: 10px; height: 6px; border-radius: 999px; background: var(--surface-3); overflow: hidden; }
.quota-bar-fill { height: 100%; border-radius: 999px; background: var(--accent); transition: width .3s; }
.quota-bar-fill.over { background: #e74c3c; }
.quota-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.btnbar { margin-top: 8px; }
</style>
