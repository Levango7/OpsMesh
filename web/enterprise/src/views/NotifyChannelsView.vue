<template>
  <div>
    <h2>{{ $t('notify.title') }}</h2>
    <p class="muted">{{ $t('notify.subtitle') }}</p>

    <!-- tab 分区：通知渠道 / 通知模板 -->
    <div class="btnbar tabs">
      <button :class="['tab-btn', { active: tab === 'channels' }]" @click="tab = 'channels'">
        {{ $t('notify.tab_channels') }}
      </button>
      <button :class="['tab-btn', { active: tab === 'templates' }]" @click="tab = 'templates'">
        {{ $t('notify.tab_templates') }}
      </button>
    </div>

    <!-- ============ 通知渠道 ============ -->
    <template v-if="tab === 'channels'">
      <div class="btnbar">
        <button class="primary" data-testid="notify-add-channel-btn" @click="openChannelCreate">
          <Icon name="add" :size="14" />
          {{ $t('notify.add_channel') }}
        </button>
        <button class="outline" @click="fetchChannels">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </button>
      </div>
      <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="channelCols" :rows="channels" row-key="id" :empty-text="$t('notify.empty_channels')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-type="{ value }">
          <span class="type-tag">{{ fmtChannelType(value) }}</span>
        </template>
        <template #cell-enabled="{ value }">
          <StatusBadge :status="value ? 'ok' : 'fail'" :text="value ? $t('notify.enabled') : $t('notify.disabled')" />
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openChannelEdit(row)"><Icon name="edit" :size="13" /></button>
            <!-- 测试发送：验证渠道连通性 -->
            <button class="xs outline" data-testid="notify-test-btn" @click="onTest(row)">{{ $t('notify.test') }}</button>
            <button class="xs danger" @click="onDeleteChannel(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </template>

    <!-- ============ 通知模板 ============ -->
    <template v-else>
      <div class="btnbar">
        <button class="primary" data-testid="notify-add-template-btn" @click="openTemplateCreate">
          <Icon name="add" :size="14" />
          {{ $t('notify.add_template') }}
        </button>
        <button class="outline" @click="fetchTemplates">
          <Icon name="refresh" :size="14" />
          {{ $t('common.refresh') }}
        </button>
      </div>
      <div v-if="loading" class="muted">{{ $t('common.loading') }}</div>
      <DataTable v-else :columns="templateCols" :rows="templates" row-key="id" :empty-text="$t('notify.empty_templates')">
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-type="{ value }"><span class="type-tag">{{ fmtChannelType(value) }}</span></template>
        <template #cell-format="{ value }"><span class="type-tag">{{ value || '—' }}</span></template>
        <template #cell-title="{ value }">
          <span class="ellipsis" :title="value">{{ value || '—' }}</span>
        </template>
        <template #cell-body="{ value }">
          <span class="ellipsis" :title="value">{{ value || '—' }}</span>
        </template>
        <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="row-actions">
            <button class="xs outline" @click="openTemplateEdit(row)"><Icon name="edit" :size="13" /></button>
            <button class="xs danger" @click="onDeleteTemplate(row)"><Icon name="delete" :size="13" /></button>
          </div>
        </template>
      </DataTable>
    </template>

    <!-- 渠道 新增/编辑抽屉 -->
    <DetailDrawer :open="!!channelForm" :title="channelForm && channelForm.id ? $t('notify.edit_channel') : $t('notify.add_channel')" @close="channelForm = null">
      <form v-if="channelForm" class="entity-form" @submit.prevent="onSaveChannel">
        <div class="field">
          <label>{{ $t('notify.channel_name') }}</label>
          <input v-model.trim="channelForm.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('notify.channel_type') }}</label>
          <select v-model="channelForm.type">
            <option value="webhook">Webhook</option>
            <option value="email">Email</option>
            <option value="sms">SMS</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('notify.channel_config') }}</label>
          <textarea v-model.trim="channelForm.config" rows="5" :placeholder="$t('notify.config_ph')" />
          <span class="hint">{{ $t('notify.config_hint') }}</span>
        </div>
        <div class="field">
          <label class="checkbox-item">
            <input type="checkbox" v-model="channelForm.enabled" />
            <span>{{ $t('notify.new_enabled') }}</span>
          </label>
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('notify.save') }}</button>
          <button type="button" class="outline" @click="channelForm = null">{{ $t('notify.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 模板 新增/编辑抽屉 -->
    <DetailDrawer :open="!!templateForm" :title="templateForm && templateForm.id ? $t('notify.edit_template') : $t('notify.add_template')" @close="templateForm = null">
      <form v-if="templateForm" class="entity-form" @submit.prevent="onSaveTemplate">
        <div class="field">
          <label>{{ $t('notify.template_name') }}</label>
          <input v-model.trim="templateForm.name" type="text" required />
        </div>
        <div class="field">
          <label>{{ $t('notify.channel_type') }}</label>
          <select v-model="templateForm.type">
            <option value="webhook">Webhook</option>
            <option value="email">Email</option>
            <option value="sms">SMS</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('notify.template_format') }}</label>
          <select v-model="templateForm.format">
            <option value="markdown">Markdown</option>
            <option value="text">Text</option>
            <option value="html">HTML</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('notify.template_title') }}</label>
          <input v-model.trim="templateForm.title" type="text" :placeholder="$t('notify.title_ph')" />
        </div>
        <div class="field">
          <label>{{ $t('notify.template_body') }}</label>
          <textarea v-model="templateForm.body" rows="6" :placeholder="$t('notify.body_ph')" />
        </div>
        <div v-if="formError" class="msg err">{{ formError }}</div>
        <div class="btnbar">
          <button type="submit" class="primary"><Icon name="success" :size="14" /> {{ $t('notify.save') }}</button>
          <button type="button" class="outline" @click="templateForm = null">{{ $t('notify.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认（替代 confirm） -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="notify-delete-confirm-modal"
      :title="$t('common.delete')"
      :message="deleteConfirm.msg"
      @confirm="onDeleteConfirm"
    />
    <!-- 错误提示（替代 alert） -->
    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="notify-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 通知渠道管理页 — tab 两区：渠道列表（CRUD + 测试发送）/ 模板列表（CRUD）
import { ref, reactive, watch, onMounted } from 'vue'
import {
  listChannels, createChannel, updateChannel, deleteChannel, testChannel,
  listTemplates, createTemplate, updateTemplate, deleteTemplate
} from '@/api/notify'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import DataTable from '@/components/DataTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { fmtTime } from '@/composables/useFormatTime'

const tab = ref('channels')
const loading = ref(false)
const formError = ref('')

const channels = ref([])
const templates = ref([])

const channelForm = ref(null)
const templateForm = ref(null)

// 删除确认弹窗（替代 confirm）：domain 标记当前删除的实体类型
const deleteConfirm = reactive({ show: false, row: null, domain: '', msg: '' })
// 错误提示弹窗（替代 alert）
const errorConfirm = reactive({ show: false, message: '' })

const channelCols = [
  { key: 'name', title: t('notify.channel_name'), slot: 'cell-name' },
  { key: 'type', title: t('notify.channel_type'), slot: 'cell-type', width: '110px' },
  { key: 'enabled', title: t('notify.status'), slot: 'cell-enabled', width: '80px' },
  { key: 'createdAt', title: t('notify.created_at'), slot: 'cell-createdAt', width: '150px' },
  { key: 'actions', title: t('notify.actions'), slot: 'cell-actions', width: '150px' }
]

const templateCols = [
  { key: 'name', title: t('notify.template_name'), slot: 'cell-name' },
  { key: 'type', title: t('notify.channel_type'), slot: 'cell-type', width: '100px' },
  { key: 'format', title: t('notify.template_format'), slot: 'cell-format', width: '100px' },
  { key: 'title', title: t('notify.template_title'), slot: 'cell-title' },
  { key: 'body', title: t('notify.template_body'), slot: 'cell-body' },
  { key: 'createdAt', title: t('notify.created_at'), slot: 'cell-createdAt', width: '150px' },
  { key: 'actions', title: t('notify.actions'), slot: 'cell-actions', width: '90px' }
]

function fmtChannelType(v) {
  const m = { webhook: 'notify.type_webhook', email: 'notify.type_email', sms: 'notify.type_sms' }
  return m[v] ? t(m[v]) : (v || '—')
}

// ============ 数据拉取 ============
async function fetchChannels() {
  loading.value = true
  try {
    // 后端返回数组（Config 已脱敏）
    const r = await listChannels()
    channels.value = Array.isArray(r) ? r : (r && r.channels) || []
  } catch {
    channels.value = []
  } finally {
    loading.value = false
  }
}

async function fetchTemplates() {
  loading.value = true
  try {
    const r = await listTemplates()
    templates.value = Array.isArray(r) ? r : (r && r.templates) || []
  } catch {
    templates.value = []
  } finally {
    loading.value = false
  }
}

// 切 tab 时按需拉取
watch(tab, (key) => {
  if (key === 'channels') fetchChannels()
  else fetchTemplates()
})

// ============ 渠道：新增/编辑/删除/测试 ============
function openChannelCreate() {
  formError.value = ''
  channelForm.value = { id: null, name: '', type: 'webhook', config: '', enabled: true }
}

function openChannelEdit(row) {
  formError.value = ''
  // 列表返回的 config 已脱敏（***），编辑时原样回显，保存时若未改动则后端保持原值
  channelForm.value = { id: row.id, name: row.name || '', type: row.type || 'webhook', config: row.config || '', enabled: !!row.enabled }
}

async function onSaveChannel() {
  formError.value = ''
  const body = {
    name: channelForm.value.name,
    type: channelForm.value.type,
    config: channelForm.value.config,
    enabled: channelForm.value.enabled
  }
  try {
    if (channelForm.value.id) {
      await updateChannel(channelForm.value.id, body)
      toast.success(t('notify.update_ok'))
    } else {
      await createChannel(body)
      toast.success(t('notify.create_ok'))
    }
    channelForm.value = null
    await fetchChannels()
  } catch (e) {
    formError.value = e.j?.error || t('notify.save_failed')
  }
}

// 测试发送：POST /notify-channels/{id}/test（body 缺省用后端内置测试消息）
async function onTest(row) {
  try {
    await testChannel(row.id)
    toast.success(t('notify.test_ok'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('notify.test_failed')
    errorConfirm.show = true
  }
}

function onDeleteChannel(row) {
  deleteConfirm.row = row
  deleteConfirm.domain = 'channel'
  deleteConfirm.msg = t('notify.confirm_delete_channel', { name: row.name || row.id })
  deleteConfirm.show = true
}

// ============ 模板：新增/编辑/删除 ============
function openTemplateCreate() {
  formError.value = ''
  templateForm.value = { id: null, name: '', type: 'webhook', format: 'markdown', title: '', body: '' }
}

function openTemplateEdit(row) {
  formError.value = ''
  templateForm.value = {
    id: row.id,
    name: row.name || '',
    type: row.type || 'webhook',
    format: row.format || 'markdown',
    title: row.title || '',
    body: row.body || ''
  }
}

async function onSaveTemplate() {
  formError.value = ''
  const body = {
    name: templateForm.value.name,
    type: templateForm.value.type,
    format: templateForm.value.format,
    title: templateForm.value.title,
    body: templateForm.value.body
  }
  try {
    if (templateForm.value.id) {
      await updateTemplate(templateForm.value.id, body)
      toast.success(t('notify.update_ok'))
    } else {
      await createTemplate(body)
      toast.success(t('notify.create_ok'))
    }
    templateForm.value = null
    await fetchTemplates()
  } catch (e) {
    formError.value = e.j?.error || t('notify.save_failed')
  }
}

function onDeleteTemplate(row) {
  deleteConfirm.row = row
  deleteConfirm.domain = 'template'
  deleteConfirm.msg = t('notify.confirm_delete_template', { name: row.name || row.id })
  deleteConfirm.show = true
}

// ============ 删除确认统一回调 ============
async function onDeleteConfirm() {
  const { row, domain } = deleteConfirm
  if (!row) return
  try {
    if (domain === 'channel') {
      await deleteChannel(row.id)
      await fetchChannels()
    } else {
      await deleteTemplate(row.id)
      await fetchTemplates()
    }
    toast.success(t('notify.delete_ok'))
  } catch (e) {
    errorConfirm.message = e.j?.error || t('notify.delete_failed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  fetchChannels()
  fetchTemplates()
})
</script>

<style scoped>
.tabs { margin: 14px 0 4px; }
.tab-btn {
  border: 1px solid var(--border);
  background: var(--surface-2);
  color: var(--text-2);
  border-radius: var(--radius-sm);
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
  transition: .12s;
}
.tab-btn.active {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent);
  font-weight: 600;
}
.row-actions { display: flex; gap: 4px; }
.type-tag {
  display: inline-flex; align-items: center; height: 19px;
  padding: 0 8px; border-radius: 999px;
  background: var(--accent-soft); color: var(--accent);
  font-size: 11.5px; font-weight: 600;
}
.ellipsis {
  display: inline-block; max-width: 220px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  vertical-align: bottom; font-size: 12.5px;
}
.entity-form { display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field > label { margin: 0; font-size: 12.5px; color: var(--text-2); }
.checkbox-item {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 13px; color: var(--text); margin: 0;
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); cursor: pointer; width: fit-content;
}
.hint { font-size: 12px; color: var(--text-3); }
.btnbar { margin-top: 8px; }
</style>
