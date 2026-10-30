<template>
  <PageShell
    :title="$t('mwdep.title')"
    :desc="$t('mwdep.desc')"
    testid="mwdep"
    :error="store.error || loadErr"
  >
    <!-- 单卡片 + 顶部视图切换：模板目录 / 已部署实例（分段开关保留原位，不做页面级 .btnbar） -->
    <div class="card">
      <div class="view-head">
        <div class="segmented" role="tablist">
          <Button
            variant="primary" size="md" :class="[{ 'is-active': view === 'templates' }, 'segmented__item']"
            role="tab"
            :aria-selected="view === 'templates'"
            data-testid="mwdep-view-templates"
            @click="setView('templates')"
          >{{ $t('mwdep.viewTemplates') }}</Button>
          <Button
            variant="primary" size="md" :class="[{ 'is-active': view === 'instances' }, 'segmented__item']"
            role="tab"
            :aria-selected="view === 'instances'"
            data-testid="mwdep-view-instances"
            @click="setView('instances')"
          >{{ $t('mwdep.viewInstances') }}</Button>
        </div>
        <div class="view-head__actions">
          <Button
            v-if="view === 'instances'"
            variant="secondary"
            size="sm"
            icon="refresh"
            data-testid="mwdep-instances-refresh"
            @click="store.fetchInstances()"
          >{{ $t('mwdep.refresh') }}</Button>
        </div>
      </div>

      <!-- 分类筛选：仅模板目录视图可见 -->
      <div v-if="view === 'templates'" class="cat-filter">
        <Button
          variant="primary" size="md" v-for="cat in categories"
          :key="cat"
          :class="[{ active: store.category === cat }, 'cat-btn']"
          @click="store.setCategory(cat)"
        >{{ $t('mwdep.category.' + cat) }}</Button>
      </div>

      <!-- 模板目录表 -->
      <template v-if="view === 'templates'">
        <div v-if="store.loading && !store.templates.length" class="muted">{{ $t('mwdep.loading') }}</div>
        <DataTable
          v-else
          :columns="tplColumns"
          :rows="store.templates"
          row-key="id"
          :empty-text="$t('mwdep.noTemplates')"
        >
          <template #cell-tpl-id="{ value }"><code>{{ value }}</code></template>
          <template #cell-tpl-category="{ value }">
            <span class="badge info">{{ $t('mwdep.category.' + (value || 'all')) }}</span>
          </template>
          <template #cell-tpl-version="{ row }">
            <span class="ver-cell">
              <code>{{ defaultVersion(row) || '—' }}</code>
              <span
                v-if="otherVersions(row).length"
                class="ver-more"
                :title="moreVersionsTitle(row)"
                data-testid="mwdep-more-versions"
              >+{{ otherVersions(row).length }}</span>
            </span>
          </template>
          <template #cell-tpl-deployTypes="{ value }">
            <span v-for="dt in (value || [])" :key="dt" class="badge info">{{ $t('mwdep.deployType.' + dt) }}</span>
            <span v-if="!(value && value.length)" class="muted">—</span>
          </template>
          <template #cell-tpl-risk="{ value }">
            <StatusBadge :status="riskStatus(value)" :text="$t('mwdep.risk.' + (value || 'medium'))" />
          </template>
          <template #cell-tpl-actions="{ row }">
            <div class="row-actions" @click.stop>
              <Button variant="ghost" size="sm" @click="onView(row.id)">{{ $t('mwdep.view') }}</Button>
              <Button variant="primary" size="sm" @click="onDeploy(row.id)">{{ $t('mwdep.deploy') }}</Button>
            </div>
          </template>
        </DataTable>
      </template>

      <!-- 已部署实例表 -->
      <template v-else>
        <p class="hint">{{ $t('mwdep.instancesHint') }}</p>
        <div v-if="store.instancesLoading && !store.instances.length" class="muted">{{ $t('mwdep.loading') }}</div>
        <DataTable
          v-else
          :columns="insColumns"
          :rows="store.instances"
          row-key="id"
          :empty-text="$t('mwdep.noInstances')"
        >
          <template #cell-ins-id="{ value }"><code>{{ value }}</code></template>
          <template #cell-ins-deployType="{ value }">
            <span class="badge info">{{ $t('mwdep.deployType.' + (value || 'docker')) }}</span>
          </template>
          <template #cell-ins-status="{ value }">
            <StatusBadge :status="instanceStatus(value)" :text="value || '-'" />
          </template>
          <template #cell-ins-createdAt="{ value }">{{ fmtTime(value) }}</template>
          <template #cell-ins-actions="{ row }">
            <Button
              variant="ghost" size="sm" v-if="canUninstall(row.status)"
              style="color: var(--fail); border-color: var(--fail)"
              @click.stop="onUninstall(row)"
            >{{ $t('mwdep.uninstall') }}</Button>
            <span v-else class="muted">—</span>
          </template>
        </DataTable>
      </template>
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
        <!-- 可用版本 -->
        <div v-if="tplVersions.length" class="versions-section">
          <h4>{{ $t('mwdep.availableVersions') }}</h4>
          <ul class="version-list">
            <li
              v-for="v in tplVersions"
              :key="v.version"
              class="version-item"
              :class="{ 'is-default': v.version === tplDefaultVersion }"
            >
              <code>{{ v.version }}</code>
              <span v-if="v.label" class="muted">{{ v.label }}</span>
              <span v-if="v.version === tplDefaultVersion" class="badge info">{{ $t('mwdep.defaultVersion') }}</span>
            </li>
          </ul>
        </div>
        <div v-if="tpl.params && tpl.params.length" class="params-section">
          <h4>{{ $t('mwdep.detailParams') }}</h4>
          <DataTable :columns="paramCols" :rows="tpl.params" row-key="name" :empty-text="$t('mwdep.noParams')">
            <template #cell-param-name="{ value }"><code>{{ value }}</code></template>
            <template #cell-param-required="{ value }">
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
          <Button variant="primary" size="md" @click="onDeploy(tpl.id)">{{ $t('mwdep.deploy') }}</Button>
        </div>
      </div>
    </DetailDrawer>

    <!-- 部署对话框 -->
    <div v-if="deployOpen" class="modal-mask" @click.self="closeDeploy">
      <div class="modal modal-deploy">
        <header class="modal-head">
          <h3>{{ $t('mwdep.deployTitle') }}</h3>
          <Button variant="ghost" size="sm" @click="closeDeploy">✕</Button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('mwdep.deployHint') }}</p>
          <div class="deploy-fields">
            <div class="field">
              <label>{{ $t('mwdep.selectDeployType') }}</label>
              <select v-model="deployForm.deployType">
                <option v-for="dt in (deployTpl?.deployTypes || ['docker', 'systemd'])" :key="dt" :value="dt">{{ $t('mwdep.deployType.' + dt) }}</option>
              </select>
            </div>
            <div class="field">
              <label>{{ $t('mwdep.selectVersion') }}</label>
              <select v-model="deployForm.version" data-testid="mwdep-version-select" :disabled="!deployVersions.length">
                <option v-for="v in deployVersions" :key="v.version" :value="v.version">{{ versionLabel(v) }}</option>
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
          <div v-if="deployParams.length" class="params-form">
            <h4>{{ $t('mwdep.params') }}</h4>
            <div v-for="p in deployParams" :key="p.name" class="field">
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
            <Button variant="primary" size="md" @click="confirmDeploy" :disabled="deploying">{{ $t('mwdep.confirm') }}</Button>
            <Button variant="ghost" size="md" @click="closeDeploy">{{ $t('mwdep.cancel') }}</Button>
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
          <Button variant="ghost" size="sm" @click="uninstallOpen = false">✕</Button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('mwdep.uninstallHint') }}</p>
          <p>{{ $t('mwdep.instanceIdLabel') }} <code>{{ uninstallTarget?.id }}</code></p>
          <div class="btnbar">
            <Button variant="danger" size="md" @click="confirmUninstall" :disabled="uninstalling">{{ $t('mwdep.uninstallConfirmBtn') }}</Button>
            <Button variant="ghost" size="md" @click="uninstallOpen = false">{{ $t('mwdep.uninstallCancel') }}</Button>
          </div>
          <p v-if="uninstallMsg" :class="['msg', uninstallOk ? 'ok' : 'err']">{{ uninstallMsg }}</p>
          <div v-if="uninstallLog" class="log-block">
            <h4>{{ $t('mwdep.uninstallLog') }}</h4>
            <pre class="code-block">{{ uninstallLog }}</pre>
          </div>
        </div>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

// 中间件部署 — 单面板视图切换（模板目录 / 已部署实例）+ 分类筛选 + 详情 + 多版本部署 + 卸载
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useMiddlewareStore } from '@/stores/middleware'
import { getMiddlewareTemplate } from '@/api/middleware'
import { getTaskDetail } from '@/api/task'
import { t, currentLang } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import { Button } from '@/components/ui'

const store = useMiddlewareStore()
// 模板详情加载失败此前静默吞错——统一骨架批次补上可见错误（与 store.error 并列展示）
const loadErr = ref('')

// 视图切换：模板目录 / 已部署实例（分段开关）
const view = ref('templates')   // 'templates' | 'instances'

// 分类列表（与后端预置模板的 category 集合对齐：database/cache/message/web/search/storage/service/monitor）
const categories = ['all', 'database', 'cache', 'message', 'web', 'search', 'storage', 'service', 'monitor', 'gpu']

const tplColumns = computed(() => [
  { key: 'id', title: 'ID', slot: 'cell-tpl-id' },
  { key: 'name', title: t('mwdep.col.name') },
  { key: 'category', title: t('mwdep.col.category'), slot: 'cell-tpl-category' },
  { key: 'version', title: t('mwdep.col.version'), slot: 'cell-tpl-version' },
  { key: 'deployTypes', title: t('mwdep.col.deployTypes'), slot: 'cell-tpl-deployTypes' },
  { key: 'risk', title: t('mwdep.col.risk'), slot: 'cell-tpl-risk' },
  { key: 'actions', title: t('mwdep.col.action'), slot: 'cell-tpl-actions', width: '160px' }
])
const insColumns = computed(() => [
  { key: 'id', title: t('mwdep.instance.col.id'), slot: 'cell-ins-id' },
  { key: 'templateID', title: t('mwdep.instance.col.template') },
  { key: 'agentID', title: t('mwdep.instance.col.agent') },
  { key: 'deployType', title: t('mwdep.instance.col.deployType'), slot: 'cell-ins-deployType' },
  { key: 'version', title: t('mwdep.instanceVersion') },
  { key: 'status', title: t('mwdep.instance.col.status'), slot: 'cell-ins-status' },
  { key: 'createdAt', title: t('mwdep.instance.col.createdAt'), slot: 'cell-ins-createdAt' },
  { key: 'actions', title: t('mwdep.instance.col.action'), slot: 'cell-ins-actions', width: '90px' }
])
const paramCols = [
  { key: 'name', title: 'Name', slot: 'cell-param-name' },
  { key: 'type', title: 'Type' },
  { key: 'default', title: 'Default' },
  { key: 'required', title: 'Required', slot: 'cell-param-required' },
  { key: 'description', title: 'Description' }
]

const tpl = computed(() => store.current || {})
const drawerTitle = computed(() => tpl.value.name || tpl.value.id || '')
const scriptsKeys = computed(() => (tpl.value.scripts ? Object.keys(tpl.value.scripts) : []))

// ---- 版本处理 ----
// 模板版本列表：后端未下发 versions 时回退到顶层 version（单版本）
function templateVersions(tplObj) {
  if (!tplObj) return []
  const list = Array.isArray(tplObj.versions) ? tplObj.versions.filter((v) => v && v.version) : []
  if (list.length) return list
  if (tplObj.version) return [{ version: tplObj.version, label: tplObj.version, default: true }]
  return []
}

// 默认版本：default:true 优先，否则取第一个
function defaultVersion(tplObj) {
  const list = templateVersions(tplObj)
  if (!list.length) return ''
  const d = list.find((v) => v.default)
  return (d || list[0]).version
}

// 除默认版本外的其它版本
function otherVersions(tplObj) {
  const dv = defaultVersion(tplObj)
  return templateVersions(tplObj).filter((v) => v.version !== dv)
}

// 版本展示名：version（带 label 时附加 label）
function versionLabel(v) {
  if (!v) return ''
  return v.label ? `${v.version} · ${v.label}` : v.version
}

// 合并模板参数与版本参数：同名以版本参数为准（版本未声明的字段沿用模板定义）
function mergeParams(tplObj, version) {
  const base = tplObj && Array.isArray(tplObj.params) ? tplObj.params : []
  const v = templateVersions(tplObj).find((x) => x.version === version)
  const vParams = v && Array.isArray(v.params) ? v.params : []
  const map = new Map()
  base.forEach((p) => { if (p && p.name) map.set(p.name, { ...p }) })
  vParams.forEach((p) => {
    if (!p || !p.name) return
    map.set(p.name, { ...(map.get(p.name) || {}), ...p })
  })
  return Array.from(map.values())
}

// 抽屉里的可用版本
const tplVersions = computed(() => templateVersions(tpl.value))
const tplDefaultVersion = computed(() => defaultVersion(tpl.value))

// 模板表「+N」徽标的 tooltip：列出其它版本
function moreVersionsTitle(tplObj) {
  const others = otherVersions(tplObj)
  if (!others.length) return ''
  return t('mwdep.moreVersions', { n: others.length }) + ' · ' + others.map((v) => versionLabel(v)).join(' / ')
}

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

// 视图切换：进入实例视图时刷新实例列表（运维场景要求实时性）
function setView(v) {
  if (view.value === v) return
  view.value = v
  if (v === 'instances') store.fetchInstances()
}

// ---- 部署对话框 ----
const deployOpen = ref(false)
const deployTpl = ref(null)
const deployForm = ref({ deployType: '', agentID: '', params: {}, version: '' })
const deployMsg = ref('')
const deployOk = ref(false)
const deploying = ref(false)
const deployLog = ref('')
let deployTimer = null

// 部署对话框的可选版本 + 当前版本的参数定义
const deployVersions = computed(() => templateVersions(deployTpl.value))
const deployParams = computed(() => mergeParams(deployTpl.value, deployForm.value.version))

// 切换版本时按新版本的参数定义重建参数表单
watch(() => deployForm.value.version, () => {
  const vals = {}
  deployParams.value.forEach((p) => {
    vals[p.name] = p.default != null ? String(p.default) : ''
  })
  deployForm.value.params = vals
})

async function onDeploy(id) {
  deployOpen.value = true
  deployMsg.value = ''
  deployLog.value = ''
  deployForm.value = { deployType: '', agentID: '', params: {}, version: '' }
  deployTpl.value = null
  if (deployTimer) { clearInterval(deployTimer); deployTimer = null }
  try {
    deployTpl.value = await getMiddlewareTemplate(id)
    const types = deployTpl.value.deployTypes || ['docker', 'systemd']
    deployForm.value.deployType = types[0] || 'docker'
    // 默认版本：default:true 优先，否则第一个
    deployForm.value.version = defaultVersion(deployTpl.value)
    // 参数按「模板参数 + 选中版本参数」合并后的定义渲染
    const params = {}
    mergeParams(deployTpl.value, deployForm.value.version).forEach((p) => {
      params[p.name] = p.default != null ? String(p.default) : ''
    })
    deployForm.value.params = params
  } catch (e) {
    deployTpl.value = null
    loadErr.value = e.j?.error || t('common.load_failed')
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
  // 校验参数（按当前版本合并后的参数定义）
  const params = {}
  for (const p of deployParams.value) {
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
    const r = await store.deploy(
      deployTpl.value.id,
      deployForm.value.agentID,
      deployForm.value.deployType,
      params,
      deployForm.value.version
    )
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
/* 视图头：分段开关（左）+ 工具条（右） */
.view-head {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; flex-wrap: wrap; margin-bottom: 12px;
}
.view-head__actions { display: inline-flex; align-items: center; gap: 8px; }

/* 分段开关（对齐 .k8s-subnav 模式：容器内嵌，选中项浮起） */
.segmented {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px; background: var(--color-surface-2, #f7f9fc);
  border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: var(--radius-lg, 8px);
}
.segmented__item {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 7px 14px; border: 1px solid transparent;
  border-radius: var(--radius-md, 6px);
  background: transparent; color: var(--color-ink-muted, #6b7280);
  font-size: 12.5px; font-weight: 500; white-space: nowrap;
  cursor: pointer; transition: background-color 150ms ease, color 150ms ease, box-shadow 150ms ease;
}
.segmented__item:hover { background: var(--color-surface-3, #eef1f7); color: var(--color-ink, #f7f8f8); }
.segmented__item.is-active {
  background: var(--color-surface-1, #ffffff);
  color: var(--color-primary, #5e6ad2);
  border-color: var(--color-hairline, #d3dbe8);
  box-shadow: var(--shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.08));
}
.segmented__item:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #5e6ad2) 40%, transparent);
}

.cat-filter { display: flex; flex-wrap: wrap; gap: 6px; margin: 10px 0 14px; }
.cat-btn {
  padding: 5px 12px; font-size: 12.5px; border-radius: 999px;
  background: var(--surface-3); border: 1px solid var(--border);
  color: var(--text-2); cursor: pointer; transition: .15s;
}
.cat-btn:hover { background: var(--bg-soft); color: var(--text); }
.cat-btn.active { background: var(--accent); color: #fff; border-color: var(--accent); }
.detail-meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; margin-bottom: 12px; }
.field-hint { font-size: 11.5px; color: var(--text-3); margin-right: 6px; }
.badge { display: inline-flex; align-items: center; height: 20px; padding: 0 9px; border-radius: 999px; font-size: 11.5px; font-weight: 600; margin-right: 4px; }

.code-block {
  background: var(--surface-3); color: var(--text); padding: 12px;
  border-radius: var(--radius-sm); overflow: auto; max-height: 200px;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono);
}
.params-section, .scripts-section, .versions-section { margin-top: 12px; }
.script-group { margin-bottom: 14px; }
.script-type { font-weight: 600; margin-bottom: 6px; }
.script-item { margin-bottom: 8px; }

/* 版本 */
.ver-cell { display: inline-flex; align-items: center; gap: 6px; }
.ver-more {
  display: inline-flex; align-items: center; height: 18px; padding: 0 7px;
  border-radius: 999px; font-size: 11px; font-weight: 600;
  background: var(--info-bg); color: var(--info); cursor: help;
}
.version-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.version-item {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 10px; border-radius: var(--radius-sm);
  background: var(--surface-3); border: 1px solid transparent;
}
.version-item.is-default { border-color: var(--accent); background: var(--accent-soft); }

/* .modal-mask / .modal / .modal-sm / .modal-head / .modal-body .field( label) /
   .modal-body .req 已改用 styles/pages.css 全局对话框家族 A（2026-10-30 收编）。
   .modal-deploy 是本页专属宽度（700px），故保留。 */
.modal-deploy { width: 700px; }
/* 部署对话框：部署方式 / 版本 / 目标设备 三列并排 */
.deploy-fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 12px; margin-bottom: 12px; }
.deploy-fields .field { min-width: 0; margin-bottom: 0; }
.params-form { margin: 8px 0; }
.log-block { margin-top: 12px; }
</style>
