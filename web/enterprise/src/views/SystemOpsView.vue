<template>
  <!-- testid="systemops" 沿用既有标题 id（systemops-title） -->
  <PageShell :title="$t('system_ops.title')" :desc="$t('system_ops.subtitle')" testid="systemops" :error="error || loadErr">

    <!-- 设备选择 -->
    <div class="toolbar">
      <div class="field">
        <label class="field__label">{{ $t('system_ops.device') }}</label>
        <Select
          v-model="selectedDevice"
          :placeholder="$t('system_ops.select_device')"
          :options="deviceOptions"
          @update:model-value="loadSystemInfo"
        />
      </div>
      <Button variant="primary" size="sm" @click="loadSystemInfo" :disabled="!selectedDevice">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="!selectedDevice" class="muted">{{ $t('system_ops.please_select') }}</div>
    <div v-else-if="loading" class="muted">{{ $t('common.loading') }}</div>

    <template v-else>
      <!-- Tab 切换 -->
      <Tabs v-model="tab" type="line">
        <TabItem name="users" :label="$t('system_ops.tab_users') + ' (' + users.length + ')' " />
        <TabItem name="groups" :label="$t('system_ops.tab_groups') + ' (' + groups.length + ')' " />
        <TabItem name="firewall" :label="$t('system_ops.tab_firewall') + ' (' + firewall.length + ')' " />
        <TabItem name="services" :label="$t('system_ops.tab_services') + ' (' + services.length + ')' " />
        <TabItem name="journal" :label="$t('system_ops.tab_journal') " />
      </Tabs>

      <!-- 用户 -->
      <div v-show="tab === 'users'" class="card">
        <div class="flowbar">
          <h3 class="card__title">{{ $t('system_ops.users_title') }}</h3>
          <Input v-model="userSearch" :placeholder="$t('system_ops.search_user')" class="search-input" />
          <label class="chk"><input type="checkbox" v-model="showSystemUsers" /> {{ $t('system_ops.show_system') }}</label>
          <Button variant="primary" size="sm" @click="openCreateUser">
            <Icon name="add" :size="13" /> {{ $t('system_ops.add_user') }}
          </Button>
        </div>
        <Table
          :data="filteredUsers"
          :columns="usersColumns"
          row-key="name"
        />
      </div>

      <!-- 用户组 -->
      <div v-show="tab === 'groups'" class="card">
        <div class="flowbar">
          <h3 class="card__title">{{ $t('system_ops.groups_title') }}</h3>
          <Input v-model="groupSearch" :placeholder="$t('system_ops.search_group')" class="search-input" />
          <Button variant="primary" size="sm" @click="openCreateGroup">
            <Icon name="add" :size="13" /> {{ $t('system_ops.add_group') }}
          </Button>
        </div>
        <Table
          :data="filteredGroups"
          :columns="groupsColumns"
          row-key="name"
        />
      </div>

      <!-- 防火墙 -->
      <div v-show="tab === 'firewall'" class="card">
        <div class="flowbar">
          <h3 class="card__title">{{ $t('system_ops.firewall_title') }}</h3>
          <span class="tag">{{ $t('system_ops.backend') }}: {{ firewallBackend || '—' }}</span>
          <Button variant="ghost" size="sm" @click="onQueryFirewallRules">
            {{ $t('system_ops.query_rules') }}
          </Button>
          <Button variant="primary" size="sm" @click="openAddRule">
            <Icon name="add" :size="13" /> {{ $t('system_ops.add_rule') }}
          </Button>
        </div>
        <Table
          :data="firewall"
          :columns="firewallColumns"
        />
        <div v-if="firewallRules" class="firewall-rules-result">
          <h4>{{ $t('system_ops.firewall_rules_result') }}</h4>
          <pre class="code-block">{{ JSON.stringify(firewallRules, null, 2) }}</pre>
        </div>
      </div>

      <!-- 服务管理 -->
      <div v-show="tab === 'services'" class="card">
        <div class="flowbar">
          <h3 class="card__title">{{ $t('system_ops.services_title') }}</h3>
          <span class="muted">{{ $t('system_ops.services_hint') }}</span>
          <Button variant="ghost" size="sm" @click="loadSystemInfo">
            <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
          </Button>
        </div>
        <Table
          :data="services"
          :columns="servicesColumns"
        />
      </div>

      <!-- journal 日志查询 -->
      <div v-show="tab === 'journal'" class="card">
        <div class="flowbar">
          <h3 class="card__title">{{ $t('system_ops.journal_title') }}</h3>
        </div>
        <div class="journal-form">
          <div class="field">
            <label class="field__label">{{ $t('system_ops.journal_unit') }}</label>
            <Input v-model.trim="journalForm.unit" placeholder="nginx.service" />
          </div>
          <div class="field">
            <label class="field__label">{{ $t('system_ops.journal_lines') }}</label>
            <Input v-model.number="journalForm.lines" type="number" min="1" max="2000" placeholder="100" />
          </div>
          <div class="field">
            <label class="field__label">{{ $t('system_ops.journal_level') }}</label>
            <Select
              v-model="journalForm.level"
              :options="journalLevelOptions"
              :placeholder="$t('system_ops.journal_all_levels')"
            />
          </div>
          <div class="field">
            <label class="field__label">{{ $t('system_ops.journal_since') }}</label>
            <Input v-model.trim="journalForm.since" placeholder="1 hour ago" />
          </div>
          <Button
            variant="primary"
            size="sm"
            @click="runJournalQuery"
            :disabled="journalLoading"
            data-testid="journal-query-btn"
          >
            {{ journalLoading ? $t('common.loading') : $t('system_ops.journal_query') }}
          </Button>
        </div>
        <div v-if="journalError" class="poll-err">{{ journalError }}</div>
        <pre v-if="journalResult" class="journal-output" data-testid="journal-output">{{ journalResult }}</pre>
      </div>
    </template>

    <!-- 创建用户弹窗 -->
    <Drawer v-model="showUserForm" :title="$t('system_ops.add_user')" @close="showUserForm = false">
      <Form class="op-form" @submit.prevent="submitCreateUser">
        <FormItem :label="t('system_ops.col_name') + ' *'" required>
          <Input v-model.trim="userForm.username" placeholder="deploy" />
        </FormItem>
        <FormItem :label="t('system_ops.col_group')">
          <Input v-model.trim="userForm.group" placeholder="deploy" />
        </FormItem>
        <FormItem :label="t('system_ops.col_home')">
          <Input v-model.trim="userForm.home" placeholder="/home/deploy" />
        </FormItem>
        <FormItem :label="t('system_ops.col_shell')">
          <Input v-model.trim="userForm.shell" placeholder="/bin/bash" />
        </FormItem>
        <div class="btnbar">
          <Button type="submit" variant="primary" size="sm" :disabled="submitting">{{ $t('system_ops.submit') }}</Button>
          <Button variant="ghost" size="sm" @click="showUserForm = false">{{ $t('common.cancel') }}</Button>
        </div>
      </Form>
    </Drawer>

    <!-- 创建组弹窗 -->
    <Drawer v-model="showGroupForm" :title="$t('system_ops.add_group')" @close="showGroupForm = false">
      <Form class="op-form" @submit.prevent="submitCreateGroup">
        <FormItem :label="t('system_ops.col_group_name') + ' *'" required>
          <Input v-model.trim="groupForm.groupName" placeholder="deploy" />
        </FormItem>
        <div class="btnbar">
          <Button type="submit" variant="primary" size="sm" :disabled="submitting">{{ $t('system_ops.submit') }}</Button>
          <Button variant="ghost" size="sm" @click="showGroupForm = false">{{ $t('common.cancel') }}</Button>
        </div>
      </Form>
    </Drawer>

    <!-- 添加防火墙规则弹窗 -->
    <Drawer v-model="showRuleForm" :title="$t('system_ops.add_rule')" @close="showRuleForm = false">
      <Form class="op-form" @submit.prevent="submitAddRule">
        <FormItem :label="t('system_ops.col_port') + ' *'" required>
          <Input v-model.trim="ruleForm.port" placeholder="8080" />
        </FormItem>
        <FormItem :label="t('system_ops.col_protocol')">
          <Select v-model="ruleForm.protocol" :options="protocolOptions" />
        </FormItem>
        <FormItem :label="t('system_ops.col_source')">
          <Input v-model.trim="ruleForm.source" placeholder="0.0.0.0/0" />
        </FormItem>
        <div class="btnbar">
          <Button type="submit" variant="primary" size="sm" :disabled="submitting">{{ $t('system_ops.submit') }}</Button>
          <Button variant="ghost" size="sm" @click="showRuleForm = false">{{ $t('common.cancel') }}</Button>
        </div>
      </Form>
    </Drawer>

    <!-- 删除确认 -->
    <Modal v-model="deleteConfirm.show" :title="$t('common.delete')" @close="deleteConfirm.show = false">
      <p>{{ deleteConfirm.message }}</p>
      <div class="btnbar" style="justify-content: flex-end">
        <Button variant="ghost" size="sm" @click="deleteConfirm.show = false">{{ $t('common.cancel') }}</Button>
        <Button variant="danger" size="sm" @click="doDelete">{{ $t('common.delete') }}</Button>
      </div>
    </Modal>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import { reactive, ref, computed, onMounted } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import Icon from '@/components/Icon.vue'
import { Button, Input, Select, Drawer, Modal, Table, Form, FormItem, Tabs, TabItem } from '@/components/ui'
import {
  getDeviceSystemInfo,
  createSystemUser,
  deleteSystemUser,
  createSystemGroup,
  deleteSystemGroup,
  addFirewallRule,
  queryJournal,
  getTaskResult,
  listFirewallRules,
} from '@/api/system-ops'

const deviceStore = useDeviceStore()

// device store 的设备列表 getter 是 flat（segments 扁平化）；此前误用不存在的 .list 导致下拉恒空。
const devices = computed(() => deviceStore.flat || [])
const deviceOptions = computed(() => devices.value.map(d => ({
  value: d.deviceID,
  label: (d.hostname || d.deviceID) + ' (' + (d.ip || d.deviceID) + ')'
})))
const selectedDevice = ref('')
const loading = ref(false)
const submitting = ref(false)
const error = ref('')
// journal 轮询中途失败的错误（骨架横幅）：此前 catch 静默吞错
const loadErr = ref('')
const tab = ref('users')

const users = ref([])
const groups = ref([])
const firewall = ref([])
const firewallBackend = ref('')
const firewallRules = ref(null)
const services = ref([])

// journal 查询状态
const journalForm = reactive({ unit: '', lines: 100, level: '', since: '' })
const journalLoading = ref(false)
const journalResult = ref('')
const journalError = ref('')

const userSearch = ref('')
const groupSearch = ref('')
const showSystemUsers = ref(false)

const showUserForm = ref(false)
const showGroupForm = ref(false)
const showRuleForm = ref(false)
const userForm = reactive({ username: '', group: '', home: '', shell: '' })
const groupForm = reactive({ groupName: '' })
const ruleForm = reactive({ port: '', protocol: 'tcp', source: '' })
const deleteConfirm = reactive({ show: false, kind: '', name: '', message: '' })

const protocolOptions = [
  { value: 'tcp', label: 'tcp' },
  { value: 'udp', label: 'udp' }
]

const journalLevelOptions = [
  { value: '', label: t('system_ops.journal_all_levels') },
  { value: 'err', label: 'err' },
  { value: 'warning', label: 'warning' },
  { value: 'info', label: 'info' },
  { value: 'debug', label: 'debug' }
]

const filteredUsers = computed(() => {
  let list = users.value
  if (!showSystemUsers.value) list = list.filter(u => !u.system)
  const q = userSearch.value.toLowerCase()
  if (q) list = list.filter(u => u.name.toLowerCase().includes(q))
  return list
})

const filteredGroups = computed(() => {
  const q = groupSearch.value.toLowerCase()
  if (!q) return groups.value
  return groups.value.filter(g => g.name.toLowerCase().includes(q))
})

const usersColumns = [
  { prop: 'name', label: t('system_ops.col_name'), width: 150 },
  { prop: 'uid', label: 'UID', width: 60 },
  { prop: 'gid', label: 'GID', width: 60 },
  { prop: 'home', label: t('system_ops.col_home'), width: 200 },
  { prop: 'shell', label: t('system_ops.col_shell'), width: 120 },
  { prop: 'system', label: t('system_ops.col_type'), width: 100, formatter: (row) => row.system ? t('system_ops.type_system') : t('system_ops.type_normal') },
  { prop: 'actions', label: t('system_ops.col_actions'), width: 100, slot: 'cell-actions' }
]

const groupsColumns = [
  { prop: 'name', label: t('system_ops.col_group_name'), width: 150 },
  { prop: 'gid', label: 'GID', width: 60 },
  { prop: 'members', label: t('system_ops.col_members'), width: 200, formatter: (row) => (row.members || []).join(', ') || '—' },
  { prop: 'actions', label: t('system_ops.col_actions'), width: 100, slot: 'cell-actions' }
]

const firewallColumns = [
  { prop: 'chain', label: t('system_ops.col_chain'), width: 100 },
  { prop: 'target', label: t('system_ops.col_target'), width: 100 },
  { prop: 'protocol', label: t('system_ops.col_protocol'), width: 100 },
  { prop: 'port', label: t('system_ops.col_port'), width: 80 },
  { prop: 'source', label: t('system_ops.col_source'), width: 150 },
  { prop: 'rule', label: t('system_ops.col_rule'), width: 200 },
  { prop: 'actions', label: t('system_ops.col_actions'), width: 80, slot: 'cell-actions' }
]

const servicesColumns = [
  { prop: 'name', label: t('system_ops.col_service'), width: 150 },
  { prop: 'status', label: t('system_ops.col_status'), width: 100 },
  { prop: 'enabled', label: t('system_ops.col_enabled'), width: 100 },
  { prop: 'actions', label: t('system_ops.col_actions'), width: 160, slot: 'cell-actions' }
]

async function loadSystemInfo() {
  if (!selectedDevice.value) return
  loading.value = true
  error.value = ''
  try {
    const resp = await getDeviceSystemInfo(selectedDevice.value)
    const inv = resp
    users.value = inv.users || []
    groups.value = inv.groups || []
    firewall.value = inv.firewall || []
    firewallBackend.value = inv.firewallBackend || ''
    services.value = inv.services || []
  } catch (e) {
    error.value = e.j?.error || t('system_ops.load_failed')
  } finally {
    loading.value = false
  }
}

function currentAgentID() {
  const d = devices.value.find(x => x.deviceID === selectedDevice.value)
  return d?.agentID || selectedDevice.value
}

function openCreateUser() {
  Object.assign(userForm, { username: '', group: '', home: '', shell: '' })
  showUserForm.value = true
}

function openCreateGroup() {
  groupForm.groupName = ''
  showGroupForm.value = true
}

function openAddRule() {
  Object.assign(ruleForm, { port: '', protocol: 'tcp', source: '' })
  showRuleForm.value = true
}

async function submitCreateUser() {
  submitting.value = true
  try {
    await createSystemUser({ agentID: currentAgentID(), ...userForm })
    toast.success(t('system_ops.task_dispatched'))
    showUserForm.value = false
    setTimeout(loadSystemInfo, 3000)
  } catch (e) {
    error.value = e.j?.error || t('system_ops.op_failed')
  } finally {
    submitting.value = false
  }
}

async function submitCreateGroup() {
  submitting.value = true
  try {
    await createSystemGroup({ agentID: currentAgentID(), groupName: groupForm.groupName })
    toast.success(t('system_ops.task_dispatched'))
    showGroupForm.value = false
    setTimeout(loadSystemInfo, 3000)
  } catch (e) {
    error.value = e.j?.error || t('system_ops.op_failed')
  } finally {
    submitting.value = false
  }
}

async function submitAddRule() {
  submitting.value = true
  try {
    await addFirewallRule({ agentID: currentAgentID(), ...ruleForm })
    toast.success(t('system_ops.task_dispatched'))
    showRuleForm.value = false
    setTimeout(loadSystemInfo, 3000)
  } catch (e) {
    error.value = e.j?.error || t('system_ops.op_failed')
  } finally {
    submitting.value = false
  }
}

async function doDelete() {
  const { kind, name } = deleteConfirm
  try {
    if (kind === 'user') {
      await deleteSystemUser({ agentID: currentAgentID(), username: name })
    } else if (kind === 'group') {
      await deleteSystemGroup({ agentID: currentAgentID(), groupName: name })
    }
    toast.success(t('system_ops.task_dispatched'))
    setTimeout(loadSystemInfo, 3000)
  } catch (e) {
    error.value = e.j?.error || t('system_ops.op_failed')
  } finally {
    deleteConfirm.show = false
  }
}

async function runJournalQuery() {
  journalLoading.value = true
  journalResult.value = ''
  journalError.value = ''
  loadErr.value = ''
  try {
    const resp = await queryJournal({
      agentID: currentAgentID(),
      unit: journalForm.unit,
      lines: journalForm.lines,
      level: journalForm.level,
      since: journalForm.since,
    })
    const taskID = resp.taskID || resp.j?.taskID
    if (!taskID) throw new Error('no taskID returned')
    // 轮询任务结果。
    for (let i = 0; i < 10; i++) {
      await new Promise(r => setTimeout(r, 1500))
      try {
        const res = await getTaskResult(taskID)
        const out = res.stdout || res.j?.stdout || ''
        const stderr = res.stderr || res.j?.stderr || ''
        if (out || stderr) {
          journalResult.value = out || stderr
          break
        }
      } catch (e) {
        // 轮询中途失败此前静默吞错——统一骨架批次补上可见错误（未取到结果时另有 journal 卡片内提示）
        loadErr.value = e.j?.error || t('common.load_failed')
      }
      if (!journalResult.value) {
        journalError.value = t('system_ops.journal_no_result')
      }
    }
  } catch (e) {
    journalError.value = e.j?.error || t('system_ops.journal_failed')
  } finally {
    journalLoading.value = false
  }
}

async function onQueryFirewallRules() {
  try {
    const resp = await listFirewallRules({ agentID: currentAgentID() })
    firewallRules.value = resp
  } catch (e) {
    error.value = e.j?.error || t('system_ops.query_failed')
  }
}

onMounted(() => {
  deviceStore.fetchDevices?.()
})
</script>

<style scoped>
.toolbar {
  display: flex;
  gap: var(--space-3, 12px);
  align-items: flex-end;
  margin-bottom: var(--space-4, 16px);
}

.field {
  display: flex;
  flex-direction: column;
  gap: var(--space-1, 4px);
}

.field__label {
  font-size: var(--font-size-xs, 11px);
  font-weight: 500;
  color: var(--color-ink-muted, #d0d6e0);
}

.flowbar {
  display: flex;
  gap: var(--space-3, 12px);
  align-items: center;
  margin-bottom: var(--space-3, 12px);
  flex-wrap: wrap;
}

.card {
  background: var(--color-surface-1, #0f1011);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-md, 8px);
  padding: var(--space-4, 16px);
}
.card__title,
.card h3 { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }

.poll-err {
  padding: var(--space-2, 8px) var(--space-3, 12px);
  margin: var(--space-1, 6px) 0;
  border-radius: var(--radius-sm, 6px);
  background: var(--color-error-soft, #fef2f2);
  border: 1px solid var(--color-error-light, #fecaca);
  color: var(--color-error, #ef4444);
  font-size: var(--font-size-sm, 12px);
  font-weight: 500;
}

.muted {
  color: var(--color-ink-muted, #d0d6e0);
}

.tag {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 4px);
  padding: 2px 8px;
  border-radius: var(--radius-pill-sm, 64px);
  font-size: var(--font-size-xs, 11px);
  font-weight: 500;
}

.tag--ok {
  background: color-mix(in srgb, var(--color-success, #27a644) 15%, transparent);
  color: var(--color-success, #27a644);
}

.tag--off {
  background: var(--color-surface-2, #141516);
  color: var(--color-ink-tertiary, #62666d);
}

.op-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 12px);
}

.btnbar {
  display: flex;
  gap: var(--space-2, 8px);
  justify-content: flex-end;
  margin-top: var(--space-2, 8px);
}

.journal-form {
  display: flex;
  gap: var(--space-3, 12px);
  align-items: flex-end;
  flex-wrap: wrap;
  margin-bottom: var(--space-3, 12px);
}

.journal-output {
  background: var(--color-surface-2, #141516);
  border: 1px solid var(--color-hairline, #23252a);
  border-radius: var(--radius-sm, 6px);
  padding: var(--space-3, 12px);
  font-family: var(--font-mono, monospace);
  font-size: var(--font-size-xs, 11px);
  max-height: 500px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.svc-actions {
  display: flex;
  gap: var(--space-1, 4px);
  flex-wrap: wrap;
}

/* Light mode */
[data-theme="light"] .poll-err {
  background: var(--color-error-soft, #fef2f2);
  border-color: var(--color-error-light, #fecaca);
  color: var(--color-error, #ef4444);
}
</style>