<template>
  <div>
    <h2>{{ $t('webhooks.title') }}</h2>
    <p class="muted">{{ $t('webhooks.subtitle') }}</p>

    <div class="btnbar">
      <button class="primary" @click="openCreate">
        <Icon name="add" :size="14" />
        {{ $t('webhooks.add') }}
      </button>
      <button class="outline" @click="fetchWebhooks">
        <Icon name="refresh" :size="14" />
        {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
    <div v-else>
      <DataTable :columns="columns" :rows="webhooks" row-key="id" :empty-text="$t('webhooks.empty')">
        <template #cell-url="{ value }"><code class="url-cell">{{ value }}</code></template>
        <template #cell-events="{ value }">
          <span class="muted">{{ (value || []).join(', ') || '—' }}</span>
        </template>
        <template #cell-enabled="{ row }">
          <span class="status-pill" :class="row.enabled ? 'ok' : 'off'">{{ row.enabled ? $t('webhooks.enabled') : $t('webhooks.disabled') }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" :title="$t('webhooks.test')" @click="onTest(row)"><Icon name="check" :size="13" /></button>
            <button class="xs outline" :title="$t('webhooks.deliveries')" @click="openDeliveries(row)"><Icon name="clipboard" :size="13" /></button>
            <button class="xs outline" @click="openEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDelete(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 新增/编辑抽屉 -->
    <DetailDrawer :open="!!form" :title="form && form.id ? $t('webhooks.edit') : $t('webhooks.add')" @close="form = null">
      <form v-if="form" class="webhook-form" @submit.prevent="onSave">
        <div class="field">
          <label>{{ $t('webhooks.new_name') }}</label>
          <input v-model.trim="form.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('webhooks.new_url') }}</label>
          <input v-model.trim="form.url" type="url" placeholder="https://example.com/hook" required />
        </div>
        <div class="field">
          <label>{{ $t('webhooks.new_events') }}</label>
          <input v-model.trim="form.eventsText" type="text" :placeholder="$t('webhooks.events_placeholder')" />
        </div>
        <div class="field">
          <label>{{ $t('webhooks.new_secret') }}</label>
          <input v-model.trim="form.secret" type="text" :placeholder="$t('webhooks.secret_placeholder')" />
        </div>
        <div class="field">
          <label>{{ $t('webhooks.new_headers') }}</label>
          <textarea v-model="form.headersText" rows="2" :placeholder="$t('webhooks.headers_placeholder')"></textarea>
        </div>
        <div class="checkbox-item">
          <label class="inline">
            <input type="checkbox" v-model="form.enabled" />
            <span>{{ $t('webhooks.enabled') }}</span>
          </label>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('webhooks.save') }}</button>
          <button type="button" class="outline" @click="form = null">{{ $t('webhooks.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 投递记录抽屉 -->
    <DetailDrawer :open="!!deliveryRow" :title="$t('webhooks.deliveries_title', { name: deliveryRow?.name || '' })" @close="deliveryRow = null">
      <div v-if="deliveriesLoading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="deliveryColumns" :rows="deliveries" row-key="id" :empty-text="$t('webhooks.no_deliveries')">
        <template #cell-event="{ value }"><code>{{ value }}</code></template>
        <template #cell-statusCode="{ value }">
          <span class="status-pill" :class="value >= 200 && value < 300 ? 'ok' : 'bad'">{{ value }}</span>
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
      </DataTable>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="webhooks-delete-confirm-modal"
      :title="$t('webhooks.delete')"
      :message="$t('webhooks.confirm_delete')"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="webhooks-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// Webhook 管理页 — CRUD + 测试投递 + 投递记录查看
import { ref, computed, reactive, onMounted } from 'vue'
import {
  getWebhooks, createWebhook, updateWebhook, deleteWebhook,
  testWebhook, getWebhookDeliveries
} from '@/api/webhook'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'

const webhooks = ref([])
const loading = ref(false)
const form = ref(null)
const formError = ref('')

// 投递记录抽屉状态
const deliveryRow = ref(null)
const deliveries = ref([])
const deliveriesLoading = ref(false)

// 删除确认弹窗（替代 confirm）
const deleteConfirm = reactive({ show: false, row: null })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const columns = computed(() => [
  { key: 'name', title: t('webhooks.name') },
  { key: 'url', title: t('webhooks.url'), slot: 'cell-url' },
  { key: 'events', title: t('webhooks.events'), slot: 'cell-events' },
  { key: 'enabled', title: t('webhooks.status'), slot: 'cell-enabled' },
  { key: 'actions', title: t('webhooks.actions'), slot: 'cell-actions', width: '120px' }
])

const deliveryColumns = computed(() => [
  { key: 'event', title: t('webhooks.dl_event'), slot: 'cell-event' },
  { key: 'statusCode', title: t('webhooks.dl_status'), slot: 'cell-statusCode' },
  { key: 'response', title: t('webhooks.dl_response') },
  { key: 'createdAt', title: t('webhooks.dl_time'), slot: 'cell-createdAt' }
])

async function fetchWebhooks() {
  loading.value = true
  try {
    const r = await getWebhooks()
    webhooks.value = (r && r.webhooks) || []
  } catch {
    webhooks.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formError.value = ''
  form.value = { id: null, name: '', url: '', eventsText: '', secret: '', headersText: '', enabled: true }
}

function openEdit(row) {
  formError.value = ''
  form.value = {
    id: row.id,
    name: row.name || '',
    url: row.url || '',
    eventsText: (row.events || []).join(', '),
    secret: row.secret || '',
    headersText: JSON.stringify(row.headers || {}, null, 2),
    enabled: row.enabled !== false
  }
}

async function onSave() {
  formError.value = ''
  try {
    // 事件列表：逗号分隔文本 → 数组
    const events = form.value.eventsText.split(',').map((s) => s.trim()).filter(Boolean)
    // 自定义 header：JSON 文本 → 对象
    let headers = {}
    if (form.value.headersText && form.value.headersText.trim()) {
      headers = JSON.parse(form.value.headersText) // 解析失败抛错走 catch
    }
    const body = {
      name: form.value.name,
      url: form.value.url,
      events,
      headers,
      enabled: form.value.enabled
    }
    if (form.value.id) await updateWebhook(form.value.id, body)
    else await createWebhook(body)
    form.value = null
    toast.success(t('webhooks.saved'))
    await fetchWebhooks()
  } catch (e) {
    formError.value = e.j?.error || (e instanceof SyntaxError ? t('webhooks.headers_invalid') : '') || t('webhooks.save_failed')
  }
}

async function onTest(row) {
  try {
    const r = await testWebhook(row.id)
    const j = r?.j || r
    toast.success(t('webhooks.test_done') + (j?.statusCode != null ? ` (HTTP ${j.statusCode})` : ''))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('webhooks.test_failed')
    errorConfirm.show = true
  }
}

async function openDeliveries(row) {
  deliveryRow.value = row
  deliveriesLoading.value = true
  deliveries.value = []
  try {
    const r = await getWebhookDeliveries(row.id)
    deliveries.value = (r && r.deliveries) || []
  } catch {
    deliveries.value = []
  } finally {
    deliveriesLoading.value = false
  }
}

function onDelete(row) {
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const row = deleteConfirm.row
  if (!row) return
  try {
    await deleteWebhook(row.id)
    toast.success(t('webhooks.deleted'))
    await fetchWebhooks()
  } catch (e) {
    errorConfirm.message = e.j?.error || t('webhooks.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(fetchWebhooks)
</script>

<style scoped>
.row-actions { display: flex; gap: 4px; }
.url-cell { font-size: 12px; }
.status-pill {
  display: inline-flex; align-items: center;
  padding: 1px 9px; border-radius: 999px; font-size: 12px; font-weight: 600;
}
.status-pill.ok { background: var(--accent-soft); color: var(--accent); }
.status-pill.off { background: var(--surface-3); color: var(--text-3); }
.status-pill.bad { background: var(--fail-bg); color: var(--fail); }
.webhook-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.field textarea { resize: vertical; }
.checkbox-item {
  display: inline-flex; padding: 4px 10px;
  border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); width: fit-content;
}
.inline { display: inline-flex; align-items: center; gap: 5px; margin: 0; cursor: pointer; }
.btnbar { margin-top: 8px; }
</style>
