<template>
  <div>
    <h2>{{ $t('osopt.title') }}</h2>
    <p class="muted">{{ $t('osopt.desc') }}</p>

    <!-- 分类筛选 -->
    <div class="cat-filter">
      <button
        v-for="cat in categories"
        :key="cat"
        class="cat-btn"
        :class="{ active: store.category === cat }"
        @click="store.setCategory(cat)"
      >{{ $t('osopt.category.' + cat) }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 模板列表 -->
    <div class="card">
      <h3>{{ $t('osopt.title') }}</h3>
      <div v-if="store.loading && !store.templates.length" class="muted">{{ $t('osopt.loading') }}</div>
      <DataTable
        v-else
        :columns="columns"
        :rows="store.templates"
        row-key="id"
        :empty-text="$t('osopt.noTemplates')"
      >
        <template #cell-id="{ value }"><code>{{ value }}</code></template>
        <template #cell-category="{ value }">
          <span class="badge info">{{ $t('osopt.category.' + (value || 'all')) }}</span>
        </template>
        <template #cell-risk="{ value }">
          <StatusBadge :status="riskStatus(value)" :text="$t('osopt.risk.' + (value || 'medium'))" />
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <button class="xs outline" @click="onView(row.id)">{{ $t('osopt.view') }}</button>
            <button class="xs primary" @click="onExecute(row.id)">{{ $t('osopt.execute') }}</button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- 模板详情抽屉 -->
    <DetailDrawer :open="!!store.current" :title="drawerTitle" @close="store.clearCurrent()">
      <div v-if="store.current">
        <div class="detail-meta">
          <div><span class="field-hint">{{ $t('osopt.col.category') }}</span> <span class="badge info">{{ $t('osopt.category.' + (tpl.category || 'all')) }}</span></div>
          <div><span class="field-hint">{{ $t('osopt.col.risk') }}</span> <StatusBadge :status="riskStatus(tpl.risk)" :text="$t('osopt.risk.' + (tpl.risk || 'medium'))" /></div>
          <div><span class="field-hint">{{ $t('osopt.detailOs') }}</span> {{ tpl.os || 'all' }}</div>
          <div><span class="field-hint">{{ $t('osopt.detailTags') }}</span>
            <span v-for="tag in (tpl.tags || [])" :key="tag" class="badge info">{{ tag }}</span>
            <span v-if="!(tpl.tags && tpl.tags.length)" class="muted">—</span>
          </div>
        </div>
        <h4>{{ $t('osopt.detailDesc') }}</h4>
        <p>{{ tpl.description || '—' }}</p>
        <h4>{{ $t('osopt.detailCommands') }}</h4>
        <pre class="code-block">{{ tpl.commands || '' }}</pre>
        <div v-if="tpl.params && tpl.params.length" class="params-section">
          <h4>{{ $t('mwdep.detailParams') }}</h4>
          <DataTable :columns="paramCols" :rows="tpl.params" row-key="name" :empty-text="$t('osopt.noParams')">
            <template #cell-name="{ value }"><code>{{ value }}</code></template>
            <template #cell-required="{ value }">
              <StatusBadge v-if="value" status="failed" :text="$t('mwdep.param.required')" />
              <span v-else class="badge info">{{ $t('mwdep.param.optional') }}</span>
            </template>
          </DataTable>
        </div>
        <div class="btnbar">
          <button class="primary" @click="onExecute(tpl.id)">{{ $t('osopt.execute') }}</button>
        </div>
      </div>
    </DetailDrawer>

    <!-- 执行对话框 -->
    <div v-if="execOpen" class="modal-mask" @click.self="closeExec">
      <div class="modal">
        <header class="modal-head">
          <h3>{{ $t('osopt.execTitle') }}</h3>
          <button class="xs outline" @click="closeExec">✕</button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('osopt.execHint') }}</p>
          <div class="field">
            <label>{{ $t('osopt.selectAgent') }}</label>
            <select v-model="execForm.agentID">
              <option value="">— {{ $t('osopt.selectAgent') }} —</option>
              <option v-for="d in store.devices" :key="d.agentID || d.id" :value="d.agentID || d.id">
                {{ d.agentID || d.id }} ({{ d.hostname || d.ip || d.deviceID || '' }})
              </option>
            </select>
          </div>
          <div v-if="execTpl && execTpl.params && execTpl.params.length" class="params-form">
            <div v-for="p in execTpl.params" :key="p.name" class="field">
              <label>{{ p.name }}<span v-if="p.required" class="req">*</span></label>
              <input
                v-model="execForm.params[p.name]"
                :placeholder="p.default != null ? String(p.default) : ''"
                :type="p.type === 'password' ? 'password' : 'text'"
              />
              <span v-if="p.description" class="hint">{{ p.description }}</span>
            </div>
          </div>
          <div v-else class="field">
            <label>{{ $t('osopt.params') }}</label>
            <textarea v-model="execForm.rawParams" rows="3" placeholder="param1&#10;param2"></textarea>
          </div>
          <div class="btnbar">
            <button class="primary" @click="confirmExec" :disabled="executing">{{ $t('osopt.confirm') }}</button>
            <button class="outline" @click="closeExec">{{ $t('osopt.cancel') }}</button>
          </div>
          <p v-if="execMsg" :class="['msg', execOk ? 'ok' : 'err']">{{ execMsg }}</p>
          <div v-if="execLog" class="log-block">
            <h4>{{ $t('osopt.execLog') }}</h4>
            <pre class="code-block">{{ execLog }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// OS 基础环境优化 — 模板列表 + 分类筛选 + 详情抽屉 + 执行对话框 + 日志轮询
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useOSOptimizeStore } from '@/stores/os-optimize'
import { getOSTemplate } from '@/api/os-optimize'
import { getTaskDetail } from '@/api/task'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'

const store = useOSOptimizeStore()

// 分类列表（与个人版对齐：内核/网络/安全/时间/SSH/磁盘/系统/用户/存储/服务/监控）
const categories = ['all', 'kernel', 'network', 'security', 'time', 'ssh', 'disk', 'system', 'user', 'storage', 'service', 'monitor']

const columns = computed(() => [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'name', title: t('osopt.col.name') },
  { key: 'category', title: t('osopt.col.category'), slot: 'cell-category' },
  { key: 'risk', title: t('osopt.col.risk'), slot: 'cell-risk' },
  { key: 'actions', title: t('osopt.col.action'), slot: 'cell-actions', width: '160px' }
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

// 风险等级 → StatusBadge 状态
function riskStatus(risk) {
  if (risk === 'low') return 'success'
  if (risk === 'high') return 'failed'
  return 'warn'
}

// 查看详情
function onView(id) { store.fetchDetail(id) }

// ---- 执行对话框 ----
const execOpen = ref(false)
const execTpl = ref(null)
const execForm = ref({ agentID: '', params: {}, rawParams: '' })
const execMsg = ref('')
const execOk = ref(false)
const executing = ref(false)
const execLog = ref('')
let execTimer = null

async function onExecute(id) {
  execOpen.value = true
  execMsg.value = ''
  execLog.value = ''
  execForm.value = { agentID: '', params: {}, rawParams: '' }
  execTpl.value = null
  if (execTimer) { clearInterval(execTimer); execTimer = null }
  // 加载模板详情（用于动态生成参数表单）
  try {
    execTpl.value = await getOSTemplate(id)
    // 用 default 预填参数
    const params = {}
    ;(execTpl.value.params || []).forEach((p) => {
      params[p.name] = p.default != null ? String(p.default) : ''
    })
    execForm.value.params = params
  } catch (e) {
    execTpl.value = null
  }
  // 加载设备列表
  if (!store.devices.length) store.fetchDevices()
}

function closeExec() {
  execOpen.value = false
  if (execTimer) { clearInterval(execTimer); execTimer = null }
}

async function confirmExec() {
  if (!execTpl.value || !execTpl.value.id) {
    execMsg.value = t('osopt.execFailNoTemplate'); execOk.value = false; return
  }
  if (!execForm.value.agentID) {
    execMsg.value = t('osopt.noAgent'); execOk.value = false; return
  }
  // 收集参数
  let params = []
  if (execTpl.value.params && execTpl.value.params.length > 0) {
    // 校验必填
    for (const p of execTpl.value.params) {
      const v = execForm.value.params[p.name]
      if (p.required && (v == null || v === '')) {
        execMsg.value = t('osopt.paramRequired', { name: p.name }); execOk.value = false; return
      }
      // 端口范围校验
      if (v != null && v !== '' && /port/i.test(p.name)) {
        const n = Number(v)
        if (!Number.isInteger(n) || n < 1 || n > 65535) {
          execMsg.value = t('osopt.paramPortInvalid', { name: p.name }); execOk.value = false; return
        }
      }
      params.push(v != null ? String(v) : '')
    }
  } else {
    params = execForm.value.rawParams.split('\n').map((s) => s.trim()).filter((s) => s.length > 0)
  }

  executing.value = true
  execMsg.value = t('osopt.submitting'); execOk.value = true
  execLog.value = ''
  try {
    const r = await store.execute(execTpl.value.id, execForm.value.agentID, params)
    if (r.s < 400 && r.j) {
      const taskId = r.j.taskID || r.j.id || r.j.taskId || ''
      execMsg.value = t('osopt.execTaskCreated', { id: (taskId || JSON.stringify(r.j)) })
      execOk.value = true
      if (taskId) startPoll(taskId)
    } else {
      execMsg.value = t('osopt.execFailHttp', { code: (r.s || '?'), msg: (r.j ? JSON.stringify(r.j) : '') })
      execOk.value = false
    }
  } catch (e) {
    execMsg.value = t('osopt.execFailError', { msg: (e.j?.error || e.message || e) })
    execOk.value = false
  } finally {
    executing.value = false
  }
}

// 轮询任务状态 + 日志
function startPoll(taskId) {
  if (execTimer) clearInterval(execTimer)
  let count = 0
  const max = 40
  execLog.value = t('osopt.pollingStart')
  execTimer = setInterval(async () => {
    count++
    try {
      const task = await getTaskDetail(taskId)
      const st = task?.status || ''
      const output = String(task?.output || '')
      const label = t('osopt.taskStatus.' + st) || st
      execLog.value = '⏳ [' + label + ']\n' + output
      if (st === 'completed' || st === 'failed' || count >= max) {
        clearInterval(execTimer); execTimer = null
        if (st === 'completed') {
          execLog.value += '\n' + t('osopt.execSuccessLog')
          execMsg.value = t('osopt.execSuccessTask', { id: taskId }); execOk.value = true
        } else if (st === 'failed') {
          execLog.value += '\n' + t('osopt.execFailLog')
          execMsg.value = t('osopt.execFailTask', { id: taskId }); execOk.value = false
        } else {
          execLog.value += '\n' + t('osopt.pollTimeout')
        }
      }
    } catch (e) {
      if (count >= max) {
        clearInterval(execTimer); execTimer = null
        execLog.value += '\n' + t('osopt.pollTimeoutShort')
      }
    }
  }, 3000)
}

onMounted(() => {
  store.fetchTemplates()
  store.fetchDevices()
})

// 组件卸载时清理执行轮询定时器，防止切路由后幽灵轮询（同 MiddlewareDeployView）。
onUnmounted(() => {
  if (execTimer) { clearInterval(execTimer); execTimer = null }
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
.detail-meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; margin-bottom: 12px; }
.field-hint { font-size: 11.5px; color: var(--text-3); margin-right: 6px; }
.badge { display: inline-flex; align-items: center; height: 20px; padding: 0 9px; border-radius: 999px; font-size: 11.5px; font-weight: 600; margin-right: 4px; }
.badge.info { background: var(--info-bg); color: var(--info); }
.code-block {
  background: var(--surface-3); color: var(--text); padding: 12px;
  border-radius: var(--radius-sm); overflow: auto; max-height: 320px;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono);
}
.params-section { margin-top: 12px; }

/* 模态对话框 */
.modal-mask {
  position: fixed; inset: 0; z-index: 50;
  background: rgba(31,37,64,.42); display: flex; align-items: center; justify-content: center;
}
.modal {
  width: 540px; max-width: 94vw; max-height: 88vh; overflow: auto;
  background: var(--surface); border-radius: var(--radius); box-shadow: var(--shadow);
  padding: 20px 22px;
}
.modal-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.modal-body .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.modal-body .field label { margin: 0; }
.modal-body .req { color: var(--fail); margin-left: 2px; }
.params-form { margin: 8px 0; }
.log-block { margin-top: 12px; }
</style>