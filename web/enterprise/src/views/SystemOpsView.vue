<template>
  <div>
    <h2 data-testid="systemops-title">{{ $t('system_ops.title') }}</h2>
    <p class="muted">{{ $t('system_ops.subtitle') }}</p>

    <!-- 设备选择 -->
    <div class="toolbar">
      <div class="field">
        <label>{{ $t('system_ops.device') }}</label>
        <select v-model="selectedDevice" @change="loadSystemInfo">
          <option value="">{{ $t('system_ops.select_device') }}</option>
          <option v-for="d in devices" :key="d.deviceID" :value="d.deviceID">
            {{ d.hostname || d.deviceID }} ({{ d.ip || d.deviceID }})
          </option>
        </select>
      </div>
      <button class="outline" @click="loadSystemInfo" :disabled="!selectedDevice">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </button>
    </div>

    <div v-if="error" class="poll-err"><Icon name="warning" :size="14" /> {{ error }}</div>
    <div v-if="!selectedDevice" class="muted">{{ $t('system_ops.please_select') }}</div>
    <div v-else-if="loading" class="muted">{{ $t('common.loading') }}</div>

    <template v-else>
      <!-- Tab 切换 -->
      <div class="tabbar">
        <button :class="['tab', { active: tab === 'users' }]" @click="tab = 'users'" data-testid="tab-users">
          {{ $t('system_ops.tab_users') }} ({{ users.length }})
        </button>
        <button :class="['tab', { active: tab === 'groups' }]" @click="tab = 'groups'" data-testid="tab-groups">
          {{ $t('system_ops.tab_groups') }} ({{ groups.length }})
        </button>
        <button :class="['tab', { active: tab === 'firewall' }]" @click="tab = 'firewall'" data-testid="tab-firewall">
          {{ $t('system_ops.tab_firewall') }} ({{ firewall.length }})
        </button>
      </div>

      <!-- 用户 -->
      <div v-show="tab === 'users'" class="card">
        <div class="flowbar">
          <h3 style="margin: 0">{{ $t('system_ops.users_title') }}</h3>
          <input v-model="userSearch" :placeholder="$t('system_ops.search_user')" class="search-input" />
          <label class="chk"><input type="checkbox" v-model="showSystemUsers" /> {{ $t('system_ops.show_system') }}</label>
          <button class="xs primary" @click="openCreateUser"><Icon name="add" :size="13" /> {{ $t('system_ops.add_user') }}</button>
        </div>
        <table class="data-table" data-testid="users-table">
          <thead>
            <tr>
              <th>{{ $t('system_ops.col_name') }}</th>
              <th>UID</th>
              <th>GID</th>
              <th>{{ $t('system_ops.col_home') }}</th>
              <th>{{ $t('system_ops.col_shell') }}</th>
              <th>{{ $t('system_ops.col_type') }}</th>
              <th>{{ $t('system_ops.col_actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in filteredUsers" :key="u.name">
              <td><code>{{ u.name }}</code></td>
              <td>{{ u.uid }}</td>
              <td>{{ u.gid }}</td>
              <td class="muted">{{ u.home }}</td>
              <td class="muted">{{ u.shell }}</td>
              <td><span class="tag" :class="u.system ? 'off' : 'ok'">{{ u.system ? $t('system_ops.type_system') : $t('system_ops.type_normal') }}</span></td>
              <td>
                <button class="xs danger" @click="confirmDelete('user', u.name)" :disabled="u.system">{{ $t('common.delete') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 用户组 -->
      <div v-show="tab === 'groups'" class="card">
        <div class="flowbar">
          <h3 style="margin: 0">{{ $t('system_ops.groups_title') }}</h3>
          <input v-model="groupSearch" :placeholder="$t('system_ops.search_group')" class="search-input" />
          <button class="xs primary" @click="openCreateGroup"><Icon name="add" :size="13" /> {{ $t('system_ops.add_group') }}</button>
        </div>
        <table class="data-table" data-testid="groups-table">
          <thead>
            <tr>
              <th>{{ $t('system_ops.col_group_name') }}</th>
              <th>GID</th>
              <th>{{ $t('system_ops.col_members') }}</th>
              <th>{{ $t('system_ops.col_actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="g in filteredGroups" :key="g.name">
              <td><code>{{ g.name }}</code></td>
              <td>{{ g.gid }}</td>
              <td class="muted">{{ (g.members || []).join(', ') || '—' }}</td>
              <td>
                <button class="xs danger" @click="confirmDelete('group', g.name)">{{ $t('common.delete') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 防火墙 -->
      <div v-show="tab === 'firewall'" class="card">
        <div class="flowbar">
          <h3 style="margin: 0">{{ $t('system_ops.firewall_title') }}</h3>
          <span class="tag">{{ $t('system_ops.backend') }}: {{ firewallBackend || '—' }}</span>
          <button class="xs primary" @click="openAddRule"><Icon name="add" :size="13" /> {{ $t('system_ops.add_rule') }}</button>
        </div>
        <table class="data-table" data-testid="firewall-table">
          <thead>
            <tr>
              <th>{{ $t('system_ops.col_chain') }}</th>
              <th>{{ $t('system_ops.col_target') }}</th>
              <th>{{ $t('system_ops.col_protocol') }}</th>
              <th>{{ $t('system_ops.col_port') }}</th>
              <th>{{ $t('system_ops.col_source') }}</th>
              <th>{{ $t('system_ops.col_rule') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in firewall" :key="i">
              <td>{{ r.chain || '—' }}</td>
              <td>{{ r.target || '—' }}</td>
              <td>{{ r.protocol || '—' }}</td>
              <td>{{ r.port || '—' }}</td>
              <td class="muted">{{ r.source || '—' }}</td>
              <td class="muted mono">{{ r.rule }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- 创建用户弹窗 -->
    <DetailDrawer :open="showUserForm" :title="$t('system_ops.add_user')" @close="showUserForm = false">
      <form class="op-form" @submit.prevent="submitCreateUser">
        <div class="field">
          <label>{{ $t('system_ops.col_name') }} *</label>
          <input v-model.trim="userForm.username" placeholder="deploy" required />
        </div>
        <div class="field">
          <label>{{ $t('system_ops.col_group') }}</label>
          <input v-model.trim="userForm.group" placeholder="deploy" />
        </div>
        <div class="field">
          <label>{{ $t('system_ops.col_home') }}</label>
          <input v-model.trim="userForm.home" placeholder="/home/deploy" />
        </div>
        <div class="field">
          <label>{{ $t('system_ops.col_shell') }}</label>
          <input v-model.trim="userForm.shell" placeholder="/bin/bash" />
        </div>
        <div class="btnbar">
          <button type="submit" class="primary" :disabled="submitting">{{ $t('system_ops.submit') }}</button>
          <button type="button" class="outline" @click="showUserForm = false">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 创建组弹窗 -->
    <DetailDrawer :open="showGroupForm" :title="$t('system_ops.add_group')" @close="showGroupForm = false">
      <form class="op-form" @submit.prevent="submitCreateGroup">
        <div class="field">
          <label>{{ $t('system_ops.col_group_name') }} *</label>
          <input v-model.trim="groupForm.groupName" placeholder="deploy" required />
        </div>
        <div class="btnbar">
          <button type="submit" class="primary" :disabled="submitting">{{ $t('system_ops.submit') }}</button>
          <button type="button" class="outline" @click="showGroupForm = false">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 添加防火墙规则弹窗 -->
    <DetailDrawer :open="showRuleForm" :title="$t('system_ops.add_rule')" @close="showRuleForm = false">
      <form class="op-form" @submit.prevent="submitAddRule">
        <div class="field">
          <label>{{ $t('system_ops.col_port') }} *</label>
          <input v-model.trim="ruleForm.port" placeholder="8080" required />
        </div>
        <div class="field">
          <label>{{ $t('system_ops.col_protocol') }}</label>
          <select v-model="ruleForm.protocol">
            <option value="tcp">tcp</option>
            <option value="udp">udp</option>
          </select>
        </div>
        <div class="field">
          <label>{{ $t('system_ops.col_source') }}</label>
          <input v-model.trim="ruleForm.source" placeholder="0.0.0.0/0" />
        </div>
        <div class="btnbar">
          <button type="submit" class="primary" :disabled="submitting">{{ $t('system_ops.submit') }}</button>
          <button type="button" class="outline" @click="showRuleForm = false">{{ $t('common.cancel') }}</button>
        </div>
      </form>
    </DetailDrawer>

    <!-- 删除确认 -->
    <ConfirmModal
      v-model="deleteConfirm.show"
      :title="$t('common.delete')"
      :message="deleteConfirm.message"
      @confirm="doDelete"
    />
  </div>
</template>

<script setup>
import { reactive, ref, computed, onMounted } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { t } from '@/i18n'
import { toast } from '@/utils/toast'
import Icon from '@/components/Icon.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import {
  getDeviceSystemInfo,
  createSystemUser,
  deleteSystemUser,
  createSystemGroup,
  deleteSystemGroup,
  addFirewallRule,
  deleteFirewallRule,
} from '@/api/system-ops'

const deviceStore = useDeviceStore()

const devices = computed(() => deviceStore.list || [])
const selectedDevice = ref('')
const loading = ref(false)
const submitting = ref(false)
const error = ref('')
const tab = ref('users')

const users = ref([])
const groups = ref([])
const firewall = ref([])
const firewallBackend = ref('')

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

function confirmDelete(kind, name) {
  deleteConfirm.kind = kind
  deleteConfirm.name = name
  deleteConfirm.message = t('system_ops.confirm_delete', { name })
  deleteConfirm.show = true
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

onMounted(() => {
  deviceStore.fetchDevices?.()
})
</script>

<style scoped>
.toolbar { display: flex; gap: 12px; align-items: flex-end; margin-bottom: 14px; }
.field { display: flex; flex-direction: column; gap: 4px; }
.field label { font-size: 12px; color: var(--text-2); }
.field input, .field select { padding: 6px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); font-size: 13px; }
.tabbar { display: flex; gap: 8px; margin-bottom: 14px; border-bottom: 1px solid var(--border); }
.tab { background: none; border: none; padding: 8px 14px; cursor: pointer; font-size: 13px; color: var(--text-2); border-bottom: 2px solid transparent; }
.tab.active { border-bottom-color: var(--accent); color: var(--accent); font-weight: 600; }
.card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 16px; }
.flowbar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; flex-wrap: wrap; }
.search-input { padding: 6px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); font-size: 13px; flex: 1; min-width: 160px; }
.chk { font-size: 12px; color: var(--text-2); display: flex; align-items: center; gap: 4px; }
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.data-table th, .data-table td { border: 1px solid var(--border); padding: 6px 8px; text-align: left; }
.data-table th { background: var(--muted); color: var(--text-2); font-size: 12px; }
.data-table tr:hover { background: var(--muted-soft); }
.mono { font-family: monospace; font-size: 11.5px; }
.op-form { display: flex; flex-direction: column; gap: 10px; }
.btnbar { display: flex; gap: 8px; justify-content: flex-end; margin-top: 8px; }
.poll-err { padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm); background: var(--fail-soft); border: 1px solid var(--fail-bg); color: var(--fail); font-size: 12.5px; }
</style>
