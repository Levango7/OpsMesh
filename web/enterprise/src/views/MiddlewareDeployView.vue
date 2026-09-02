<template>
  <div>
    <h2>{{ $t('mwdep.title') }}</h2>
    <p class="muted">{{ $t('mwdep.desc') }}</p>

    <!-- 分类筛选 -->
    <div class="cat-filter">
      <button
        v-for="cat in categories"
        :key="cat"
        class="cat-btn"
        :class="{ active: store.category === cat }"
        @click="store.setCategory(cat)"
      >{{ $t('mwdep.category.' + cat) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="row">
      <!-- 左：模板列表 + 详情 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('mwdep.title') }}</h3>
          <div v-if="store.loading && !store.templates.length" class="muted">{{ $t('mwdep.loading') }}</div>
          <DataTable
            v-else
            :columns="tplColumns"
            :rows="store.templates"
            row-key="id"
            :empty-text="$t('mwdep.noTemplates')"
          >
            <template #cell-id="{ value }"><code>{{ value }}</code></template>
            <template #cell-category="{ value }">
              <span class="badge info">{{ $t('mwdep.category.' + (value || 'all')) }}</span>
            </template>
            <template #cell-deployTypes="{ value }">
              <span v-for="dt in (value || [])" :key="dt" class="badge info">{{ $t('mwdep.deployType.' + dt) }}</span>
              <span v-if="!(value && value.length)" class="muted">—</span>
            </template>
            <template #cell-risk="{ value }">
              <StatusBadge :status="riskStatus(value)" :text="$t('mwdep.risk.' + (value || 'medium'))" />
            </template>
            <template #cell-actions="{ row }">
              <div class="row-actions" @click.stop>
                <button class="xs outline" @click="onView(row.id)">{{ $t('mwdep.view') }}</button>
                <button class="xs primary" @click="onDeploy(row.id)">{{ $t('mwdep.deploy') }}</button>
              </div>
            </template>
          </DataTable>
        </div>
      </div>

      <!-- 右：已部署实例 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('mwdep.instancesTitle') }}</h3>
            <button class="xs outline" @click="store.fetchInstances()">↻ {{ $t('mwdep.refresh') }}</button>
          </div>
          <p class="hint">{{ $t('mwdep.instancesHint') }}</p>
          <div v-if="store.instancesLoading && !store.instances.length" class="muted">{{ $t('mwdep.loading') }}</div>
          <DataTable
            v-else
            :columns="insColumns"
            :rows="store.instances"
            row-key="id"
            :empty-text="$t('mwdep.noInstances')"
          >
            <template #cell-id="{ value }"><code>{{ value }}</code></template>
            <template #cell-deployType="{ value }">
              <span class="badge info">{{ $t('mwdep.deployType.' + (value || 'docker')) }}</span>
            </template>
            <template #cell-status="{ value }">
              <StatusBadge :status="instanceStatus(value)" :text="value || '-'" />
            </template>
            <template #cell-createdAt="{ value }">{{ fmtTime(value) }}</template>
            <template #cell-actions="{ row }">
              <button
                v-if="canUninstall(row.status)"
                class="xs outline"
                style="color: var(--fail); border-color: var(--fail)"
                @click.stop="onUninstall(row)"
              >{{ $t('mwdep.uninstall') }}</button>
              <span v-else class="muted">—</span>
            </template>
          </DataTable>
        </div>
      </div>
    </div>

    <!-- 模板详情抽屉 -->
    <DetailDrawer :open="!!store.current" :title="drawerTitle" @close="store.clearCurrent()">
      <div v-if="store.current">
        <div class="detail-meta">
          <div><span class="field-hint">{{ $t('mwdep.col.category') }}</span> <span class="badge info">{{ $t('mwdep.category.' + (tpl.category || 'all')) }}</span></div>
          <div><span class="field-hint">{{ $t('mwdep.detailVersion') }}</span> {{ tpl.version || '—' }}</div>
          <div><span class="field-hint">{{ $t('mwdep.col.risk') }}</span> <StatusBadge :status="riskStatus(tpl.risk)" :text="$t('mwdep.risk.' + (tpl.risk || 'medium'))" /></div>
          <div><span class="field-hint">{{ $t('mwdep.col.deployTypes') }}</span>
            <span v-for="dt in (tpl.deployTypes || [])" :key="dt" class="badge info">{{ $t('mwdep.deployType.' + dt) }}</span>
          </div>
          <div><span class="field-hint">{{ $t('mwdep.detailTags') }}</span>
            <span v-for="tag in (tpl.tags || [])" :key="tag" class="badge info">{{ tag }}</span>
            <span v-if="!(tpl.tags && tpl.tags.length)" class="muted">—</span>
          </div>
        </div>
        <h4>{{ $t('mwdep.detailDesc') }}</h4>
        <p>{{ tpl.description || '—' }}</p>
        <div v-if="tpl.params && tpl.params.length" class="params-section">
          <h4>{{ $t('mwdep.detailParams') }}</h4>
          <DataTable :columns="paramCols" :rows="tpl.params" row-key="name" :empty-text="$t('mwdep.noParams')">
            <template #cell-name="{ value }"><code>{{ value }}</code></template>
            <template #cell-required="{ value }">
              <StatusBadge v-if="value" status="failed" :text="$t('mwdep.param.required')" />
              <span v-else class="badge info">{{ $t('mwdep.param.optional') }}</span>
            </template>
          </DataTable>
        </div>
        <div v-if="scriptsKeys.length" class="scripts-section">
          <h4>{{ $t('mwdep.detailScripts') }}</h4>
          <div v-for="st in scriptsKeys" :key="st" class="script-group">
            <div class="script-type">{{ $t('mwdep.deployType.' + st) }}</div>
            <div class="script-item"><span class="field-hint">{{ $t('mwdep.detailDeployScript') }}</span><pre class="code-block">{{ tpl.scripts[st].deploy || '' }}</pre></div>
            <div class="script-item"><span class="field-hint">{{ $t('mwdep.detailVerifyScript') }}</span><pre class="code-block">{{ tpl.scripts[st].verify || '' }}</pre></div>
            <div class="script-item"><span class="field-hint">{{ $t('mwdep.detailUninstallScript') }}</span><pre class="code-block">{{ tpl.scripts[st].uninstall || '' }}</pre></div>
          </div>
        </div>
        <div class="btnbar">
          <button class="primary" @click="onDeploy(tpl.id)">{{ $t('mwdep.deploy') }}</button>
        </div>
      </div>
    </DetailDrawer>

    <!-- 部署对话框 -->
    <div v-if="deployOpen" class="modal-mask" @click.self="closeDeploy">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('mwdep.deployTitle') }}</h3>
          <button class="xs outline" @click="closeDeploy">✕</button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('mwdep.deployHint') }}</p>
          <div class="row">
            <div class="field">
              <label>{{ $t('mwdep.selectDeployType') }}</label>
              <select v-model="deployForm.deployType">
                <option v-for="dt in (deployTpl?.deployTypes || ['docker', 'systemd'])" :key="dt" :value="dt">{{ $t('mwdep.deployType.' + dt) }}</option>
              </select>
            </div>
            <div class="field">
              <label>{{ $t('mwdep.selectAgent') }}</label>
              <select v-model="deployForm.agentID">
                <option value="">— {{ $t('mwdep.selectAgent') }} —</option>
                <option v-for="d in store.devices" :key="d.agentID || d.id" :value="d.agentID || d.id">
                  {{ d.agentID || d.id }} ({{ d.hostname || d.ip || d.deviceID || '' }})
                </option>
              </select>
            </div>
          </div>
          <div v-if="deployTpl && deployTpl.params && deployTpl.params.length" class="params-form">
            <h4>{{ $t('mwdep.params') }}</h4>
            <div v-for="p in deployTpl.params" :key="p.name" class="field">
              <label>{{ p.name }}<span v-if="p.required" class="req">*</span></label>
              <input
                v-model="deployForm.params[p.name]"
                :placeholder="p.default != null ? String(p.default) : ''"
                :type="p.type === 'password' ? 'password' : 'text'"
              />
              <span v-if="p.description" class="hint">{{ p.description }}</span>
            </div>
          </div>
          <div v-else class="muted">—</div>
          <div class="btnbar">
            <button class="primary" @click="confirmDeploy" :disabled="deploying">{{ $t('mwdep.confirm') }}</button>
            <button class="outline" @click="closeDeploy">{{ $t('mwdep.cancel') }}</button>
          </div>
          <p v-if="deployMsg" :class="['msg', deployOk ? 'ok' : 'err']">{{ deployMsg }}</p>
          <div v-if="deployLog" class="log-block">
            <h4>{{ $t('mwdep.deployLog') }}</h4>
            <pre class="code-block">{{ deployLog }}</pre>
          </div>
        </div>
      </div>
    </div>

    <!-- 卸载确认对话框 -->
    <div v-if="uninstallOpen" class="modal-mask" @click.self="uninstallOpen = false">
      <div class="modal modal-sm">
        <header class="modal-head">
          <h3>{{ $t('mwdep.uninstallTitle') }}</h3>
          <button class="xs outline" @click="uninstallOpen = false">✕</button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('mwdep.uninstallHint') }}</p>
          <p>{{ $t('mwdep.instanceIdLabel') }} <code>{{ uninstallTarget?.id }}</code></p>
          <div class="btnbar">
            <button class="danger" @click="confirmUninstall" :disabled="uninstalling">{{ $t('mwdep.uninstallConfirmBtn') }}</button>
            <button class="outline" @click="uninstallOpen = false">{{ $t('mwdep.uninstallCancel') }}</button>
          </div>
          <p v-if="uninstallMsg" :class="['msg', uninstallOk ? 'ok' : 'err']">{{ uninstallMsg }}</p>
          <div v-if="uninstallLog" class="log-block">
            <h4>{{ $t('mwdep.uninstallLog') }}</h4>
            <pre class="code-block">{{ uninstallLog }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// 中间件部署 — 模板列表 + 分类筛选 + 详情 + 部署 + 实例列表 + 卸载
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useMiddlewareStore } from '@/stores/middleware'
import { getMiddlewareTemplate } from '@/api/middleware'
import { getTaskDetail } from '@/api/task'
import { t, currentLang } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'

const store = useMiddlewareStore()

// 分类列表（与个人版对齐）
const categories = ['all', 'database', 'cache', 'message', 'web', 'search', 'storage', 'service', 'monitor']

const tplColumns = computed(() => [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'name', title: t('mwdep.col.name') },
  { key: 'category', title: t('mwdep.col.category'), slot: 'cell-category' },
  { key: 'version', title: t('mwdep.col.version') },
  { key: 'deployTypes', title: t('mwdep.col.deployTypes'), slot: 'cell-deployTypes' },
  { key: 'risk', title: t('mwdep.col.risk'), slot: 'cell-risk' },
  { key: 'actions', title: t('mwdep.col.action'), slot: 'cell-actions', width: '160px' }
])
const insColumns = computed(() => [
  { key: 'id', title: t('mwdep.instance.col.id'), slot: 'cell-id' },
  { key: 'templateID', title: t('mwdep.instance.col.template') },
  { key: 'agentID', title: t('mwdep.instance.col.agent') },
  { key: 'deployType', title: t('mwdep.instance.col.deployType'), slot: 'cell-deployType' },
  { key: 'status', title: t('mwdep.instance.col.status'), slot: 'cell-status' },
  { key: 'createdAt', title: t('mwdep.instance.col.createdAt'), slot: 'cell-createdAt' },
  { key: 'actions', title: t('mwdep.instance.col.action'), slot: 'cell-actions', width: '90px' }
])
const paramCols = [
  { key: 'name', title: 'Name', slot: 'cell-name' },
  { key: 'type', title: 'Type' },
  { key: 'default', title: 'Default' },
  { key: 'required', title: 'Required', slot: 'cell-required' },
  { key: 'description', title: 'Description' }
]

const tpl = computed(() => store.current || {})
const drawerTitle = computed(() => tpl.value.name || tpl.value.id || '')
const scriptsKeys = computed(() => (tpl.value.scripts ? Object.keys(tpl.value.scripts) : []))

function riskStatus(risk) {
  if (risk === 'low') return 'success'
  if (risk === 'high') return 'failed'
  return 'warn'
}
function instanceStatus(s) {
  if (s === 'running' || s === 'ok' || s === 'success' || s === 'deployed' || s === 'installed') return 'success'
  if (s === 'failed' || s === 'error') return 'failed'
  return 'info'
}
function canUninstall(s) {
  return s === 'running' || s === 'ok' || s === 'success' || s === 'deployed' || s === 'installed'
}
function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString(currentLang.value === 'en' ? 'en-US' : 'zh-CN', { hour12: false })
}

function onView(id) { store.fetchDetail(id) }

// ---- 部署对话框 ----
const deployOpen = ref(false)
const deployTpl = ref(null)
const deployForm = ref({ deployType: '', agentID: '', params: {} })
const deployMsg = ref('')
const deployOk = ref(false)
const deploying = ref(false)
const deployLog = ref('')
let deployTimer = null

async function onDeploy(id) {
  deployOpen.value = true
  deployMsg.value = ''
  deployLog.value = ''
  deployForm.value = { deployType: '', agentID: '', params: {} }
  deployTpl.value = null
  if (deployTimer) { clearInterval(deployTimer); deployTimer = null }
  try {
    deployTpl.value = await getMiddlewareTemplate(id)
    const types = deployTpl.value.deployTypes || ['docker', 'systemd']
    deployForm.value.deployType = types[0] || 'docker'
    const params = {}
    ;(deployTpl.value.params || []).forEach((p) => {
      params[p.name] = p.default != null ? String(p.default) : ''
    })
    deployForm.value.params = params
  } catch {
    deployTpl.value = null
  }
  if (!store.devices.length) store.fetchDevices()
}

function closeDeploy() {
  deployOpen.value = false
  if (deployTimer) { clearInterval(deployTimer); deployTimer = null }
}

async function confirmDeploy() {
  if (!deployTpl.value || !deployTpl.value.id) {
    deployMsg.value = t('mwdep.deployFailNoTemplate'); deployOk.value = false; return
  }
  if (!deployForm.value.deployType) {
    deployMsg.value = t('mwdep.noDeployType'); deployOk.value = false; return
  }
  if (!deployForm.value.agentID) {
    deployMsg.value = t('mwdep.noAgent'); deployOk.value = false; return
  }
  // 校验参数
  const params = {}
  for (const p of (deployTpl.value.params || [])) {
    const v = deployForm.value.params[p.name]
    if (p.required && (v == null || v === '')) {
      deployMsg.value = t('mwdep.paramRequired', { name: p.name }); deployOk.value = false; return
    }
    if (v != null && v !== '' && /port/i.test(p.name)) {
      const n = Number(v)
      if (!Number.isInteger(n) || n < 1 || n > 65535) {
        deployMsg.value = t('mwdep.paramPortInvalid', { name: p.name }); deployOk.value = false; return
      }
    }
    params[p.name] = v != null ? v : ''
  }

  deploying.value = true
  deployMsg.value = t('mwdep.submitting'); deployOk.value = true
  deployLog.value = ''
  try {
    const r = await store.deploy(deployTpl.value.id, deployForm.value.agentID, deployForm.value.deployType, params)
    if (r.s < 400 && r.j) {
      const taskId = r.j.taskID || r.j.id || r.j.taskId || ''
      deployMsg.value = t('mwdep.deployTaskCreated', { id: (taskId || JSON.stringify(r.j)) })
      deployOk.value = true
      if (taskId) startDeployPoll(taskId)
    } else {
      deployMsg.value = t('mwdep.deployFailHttp', { code: (r.s || '?'), msg: (r.j ? JSON.stringify(r.j) : '') })
      deployOk.value = false
    }
  } catch (e) {
    deployMsg.value = t('mwdep.deployFailError', { msg: (e.j?.error || e.message || e) })
    deployOk.value = false
  } finally {
    deploying.value = false
  }
}

function startDeployPoll(taskId) {
  if (deployTimer) clearInterval(deployTimer)
  let count = 0
  const max = 40
  deployLog.value = t('mwdep.pollingStart')
  deployTimer = setInterval(async () => {
    count++
    try {
      const task = await getTaskDetail(taskId)
      const st = task?.status || ''
      const output = String(task?.output || '')
      const label = t('mwdep.taskStatus.' + st) || st
      deployLog.value = '⏳ [' + label + ']\n' + output
      if (st === 'completed' || st === 'failed' || count >= max) {
        clearInterval(deployTimer); deployTimer = null
        if (st === 'completed') {
          deployLog.value += '\n' + t('mwdep.deploySuccessLog')
          deployMsg.value = t('mwdep.deploySuccessTask', { id: taskId }); deployOk.value = true
          store.fetchInstances()
        } else if (st === 'failed') {
          deployLog.value += '\n' + t('mwdep.deployFailLog')
          deployMsg.value = t('mwdep.deployFailTask', { id: taskId }); deployOk.value = false
        } else {
          deployLog.value += '\n' + t('mwdep.pollTimeout')
        }
      }
    } catch {
      if (count >= max) {
        clearInterval(deployTimer); deployTimer = null
        deployLog.value += '\n' + t('mwdep.pollTimeout')
      }
    }
  }, 3000)
}

// ---- 卸载 ----
const uninstallOpen = ref(false)
const uninstallTarget = ref(null)
const uninstallMsg = ref('')
const uninstallOk = ref(false)
const uninstalling = ref(false)
const uninstallLog = ref('')
let uninstallTimer = null

function onUninstall(row) {
  uninstallTarget.value = row
  uninstallOpen.value = true
  uninstallMsg.value = ''
  uninstallLog.value = ''
  if (uninstallTimer) { clearInterval(uninstallTimer); uninstallTimer = null }
}

async function confirmUninstall() {
  const ins = uninstallTarget.value
  if (!ins || !ins.id) return
  uninstalling.value = true
  uninstallMsg.value = t('mwdep.uninstalling'); uninstallOk.value = true
  uninstallLog.value = ''
  try {
    const r = await store.uninstall(ins.id, ins.agentID || ins.agentId, ins.deployType || ins.deploy_type)
    if (r.s < 400 && r.j) {
      const taskId = r.j.taskID || r.j.id || r.j.taskId || ''
      uninstallMsg.value = t('mwdep.uninstallTaskCreated', { id: (taskId || JSON.stringify(r.j)) })
      uninstallOk.value = true
      if (taskId) startUninstallPoll(taskId)
    } else {
      uninstallMsg.value = t('mwdep.uninstallFailHttp', { code: (r.s || '?'), msg: (r.j ? JSON.stringify(r.j) : '') })
      uninstallOk.value = false
    }
  } catch (e) {
    uninstallMsg.value = t('mwdep.uninstallFailError', { msg: (e.j?.error || e.message || e) })
    uninstallOk.value = false
  } finally {
    uninstalling.value = false
  }
}

function startUninstallPoll(taskId) {
  if (uninstallTimer) clearInterval(uninstallTimer)
  let count = 0
  const max = 40
  uninstallLog.value = t('mwdep.pollingStart')
  uninstallTimer = setInterval(async () => {
    count++
    try {
      const task = await getTaskDetail(taskId)
      const st = task?.status || ''
      const output = String(task?.output || '')
      const label = t('mwdep.taskStatus.' + st) || st
      uninstallLog.value = '⏳ [' + label + ']\n' + output
      if (st === 'completed' || st === 'failed' || count >= max) {
        clearInterval(uninstallTimer); uninstallTimer = null
        if (st === 'completed') {
          uninstallLog.value += '\n' + t('mwdep.uninstallSuccessLog')
          uninstallMsg.value = t('mwdep.uninstallSuccessTask', { id: taskId }); uninstallOk.value = true
          store.fetchInstances()
        } else if (st === 'failed') {
          uninstallLog.value += '\n' + t('mwdep.uninstallFailLog')
          uninstallMsg.value = t('mwdep.uninstallFailTask', { id: taskId }); uninstallOk.value = false
        } else {
          uninstallLog.value += '\n' + t('mwdep.pollTimeout')
        }
      }
    } catch {
      if (count >= max) {
        clearInterval(uninstallTimer); uninstallTimer = null
        uninstallLog.value += '\n' + t('mwdep.pollTimeout')
      }
    }
  }, 3000)
}

onMounted(() => {
  store.fetchTemplates()
  store.fetchInstances()
  store.fetchDevices()
})

// 组件卸载时清理轮询定时器：弹窗开着切路由会销毁组件，
// 不清理则定时器继续打 GET /tasks/{id} 直到上限（幽灵轮询）。
onUnmounted(() => {
  if (deployTimer) { clearInterval(deployTimer); deployTimer = null }
  if (uninstallTimer) { clearInterval(uninstallTimer); uninstallTimer = null }
})
</script>

<style scoped>
.cat-filter { display: flex; flex-wrap: wrap; gap: 6px; margin: 10px 0 14px; }
.cat-btn {
  padding: 5px 12px; font-size: 12.5px; border-radius: 999px;
  background: var(--surface-3); border: 1px solid var(--border);
  color: var(--text-2); cursor: pointer; transition: .15s;
}
.cat-btn:hover { background: var(--bg-soft); color: var(--text); }
.cat-btn.active { background: var(--accent); color: #fff; border-color: var(--accent); }
.row-actions { display: inline-flex; gap: 6px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 6px; }
.detail-meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; margin-bottom: 12px; }
.field-hint { font-size: 11.5px; color: var(--text-3); margin-right: 6px; }
.badge { display: inline-flex; align-items: center; height: 20px; padding: 0 9px; border-radius: 999px; font-size: 11.5px; font-weight: 600; margin-right: 4px; }
.badge.info { background: var(--info-bg); color: var(--info); }
.code-block {
  background: var(--surface-3); color: var(--text); padding: 12px;
  border-radius: var(--radius-sm); overflow: auto; max-height: 200px;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono);
}
.params-section, .scripts-section { margin-top: 12px; }
.script-group { margin-bottom: 14px; }
.script-type { font-weight: 600; margin-bottom: 6px; }
.script-item { margin-bottom: 8px; }

/* 模态对话框 */
.modal-mask {
  position: fixed; inset: 0; z-index: 50;
  background: var(--modal-mask); display: flex; align-items: center; justify-content: center;
}
.modal {
  width: 580px; max-width: 94vw; max-height: 88vh; overflow: auto;
  background: var(--surface); border-radius: var(--radius); box-shadow: var(--shadow);
  padding: 20px 22px;
}
.modal-sm { width: 420px; }
.modal-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.modal-body .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; min-width: 220px; }
.modal-body .field label { margin: 0; }
.modal-body .req { color: var(--fail); margin-left: 2px; }
.params-form { margin: 8px 0; }
.log-block { margin-top: 12px; }
</style>