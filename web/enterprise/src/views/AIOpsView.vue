<template>
  <div>
    <h2 data-testid="aiops-title">{{ $t('aiops.title') }}</h2>
    <p class="muted">{{ $t('aiops.desc') }}</p>

    <!-- 引擎状态卡片 -->
    <div class="metrics-row">
      <MetricsCard :title="$t('aiops.status_enabled')" icon="info" accent="--indigo">
        <div class="metric-value">{{ status.enabled ? $t('common.yes') : $t('common.no') }}</div>
        <div class="metric-sub">{{ status.provider }} · {{ status.model }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('aiops.analyzes')" icon="alerts">
        <div class="metric-value">{{ status.stats.analyzesTotal }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('aiops.remediations')" icon="task">
        <div class="metric-value">{{ status.stats.remediations }}</div>
      </MetricsCard>
      <MetricsCard :title="$t('aiops.executed')" icon="play">
        <div class="metric-value">{{ status.stats.executedRemediations }}</div>
      </MetricsCard>
    </div>

    <div v-if="status.error" class="poll-err"><Icon name="warning" :size="14" /> {{ status.error }}</div>

    <div class="tabs">
      <button :class="{ active: activeTab === 'analyze' }" @click="activeTab = 'analyze'">{{ $t('aiops.tab_analyze') }}</button>
      <button :class="{ active: activeTab === 'remediate' }" @click="activeTab = 'remediate'">{{ $t('aiops.tab_remediate') }}</button>
      <button :class="{ active: activeTab === 'nl' }" @click="activeTab = 'nl'">{{ $t('aiops.tab_nl') }}</button>
      <button :class="{ active: activeTab === 'execute' }" @click="activeTab = 'execute'">{{ $t('aiops.tab_execute') }}</button>
      <button :class="{ active: activeTab === 'history' }" @click="activeTab = 'history'">{{ $t('aiops.tab_history') }}</button>
    </div>

    <!-- Tab 1: 故障根因分析 -->
    <div v-if="activeTab === 'analyze'" class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('aiops.analyze_title') }}</h3>
        <button class="xs primary" @click="runAnalyze" data-testid="aiops-analyze-btn">{{ $t('aiops.analyze_run') }}</button>
        <button class="xs outline" @click="loadStatus">↻ {{ $t('common.refresh') }}</button>
      </div>

      <div class="form-grid">
        <div class="form-field">
          <label>{{ $t('aiops.alert_id') }}</label>
          <input v-model="analyzeReq.alert.alertID" placeholder="al-001" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.device_id') }}</label>
          <input v-model="analyzeReq.alert.deviceID" placeholder="dev-web-01" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.severity') }}</label>
          <select v-model="analyzeReq.alert.severity">
            <option>critical</option>
            <option>warning</option>
            <option>info</option>
          </select>
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.metric') }}</label>
          <input v-model="analyzeReq.alert.metric" placeholder="cpu_usage" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.message') }}</label>
          <textarea v-model="analyzeReq.alert.message" placeholder="CPU 使用率持续超过阈值"></textarea>
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.logs') }}</label>
          <textarea v-model="analyzeLogText" placeholder="每行一条日志：[时间] [级别] [来源] 消息"></textarea>
        </div>
      </div>

      <div v-if="analyzeLoading" class="muted">{{ $t('common.analyzing') }}</div>

      <div v-if="rootCause" class="result-box">
        <h4>{{ $t('aiops.root_cause') }}</h4>
        <div class="result-row"><span>{{ $t('aiops.category') }}:</span> {{ rootCause.category }}</div>
        <div class="result-row"><span>{{ $t('aiops.component') }}:</span> {{ rootCause.component }}</div>
        <div class="result-row"><span>{{ $t('aiops.description') }}:</span> {{ rootCause.description }}</div>
        <div class="result-row"><span>{{ $t('aiops.confidence') }}:</span> {{ (rootCause.confidence * 100).toFixed(0) }}%</div>
        <div class="result-row"><span>{{ $t('aiops.evidence') }}:</span> <ul><li v-for="(e, i) in rootCause.evidence" :key="i">{{ e }}</li></ul></div>
        <button class="xs primary" @click="useRootCause(rootCause)" data-testid="aiops-use-root-cause">{{ $t('aiops.use_for_remediation') }}</button>
      </div>
    </div>

    <!-- Tab 2: 智能修复方案 -->
    <div v-if="activeTab === 'remediate'" class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('aiops.remediate_title') }}</h3>
        <button class="xs primary" @click="runRemediate" data-testid="aiops-remediate-btn">{{ $t('aiops.remediate_run') }}</button>
        <button class="xs outline" @click="loadStatus">↻ {{ $t('common.refresh') }}</button>
      </div>

      <div class="form-grid">
        <div class="form-field">
          <label>{{ $t('aiops.root_cause_desc') }}</label>
          <textarea v-model="remediateReq.rootCause.description" placeholder="nginx 进程异常退出"></textarea>
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.category') }}</label>
          <input v-model="remediateReq.rootCause.category" placeholder="软件" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.component') }}</label>
          <input v-model="remediateReq.rootCause.component" placeholder="nginx" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.confidence') }}</label>
          <input v-model.number="remediateReq.rootCause.confidence" type="number" min="0" max="1" step="0.1" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.risk_tolerance') }}</label>
          <select v-model="remediateReq.constraints.riskTolerance">
            <option>low</option>
            <option>medium</option>
            <option>high</option>
          </select>
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.require_approval') }}</label>
          <input type="checkbox" v-model="remediateReq.constraints.requireApproval" />
        </div>
        <div class="form-field">
          <label>{{ $t('aiops.auto_execute') }}</label>
          <input type="checkbox" v-model="remediateReq.constraints.autoExecute" />
        </div>
      </div>

      <div v-if="remediateLoading" class="muted">{{ $t('common.generating') }}</div>

      <div v-if="remediationPlan" class="result-box">
        <h4>{{ $t('aiops.plan_title') }}: {{ remediationPlan.title }}</h4>
        <div class="result-row"><span>{{ $t('aiops.estimated_time') }}:</span> {{ remediationPlan.estimatedTime }}</div>
        <div class="result-row"><span>{{ $t('aiops.risk_level') }}:</span> {{ remediationPlan.safety.riskLevel }}</div>
        <div class="result-row"><span>{{ $t('aiops.requires_confirm') }}:</span> {{ remediationPlan.safety.requiresConfirm ? $t('common.yes') : $t('common.no') }}</div>

        <h5>{{ $t('aiops.steps') }}</h5>
        <ul>
          <li v-for="(s, i) in remediationPlan.steps" :key="i">
            <b>{{ s.order }}.</b> {{ s.title }} — {{ s.action }} <span class="muted">[{{ s.target }}]</span>
            <div class="muted" v-if="s.command">{{ s.command }}</div>
          </li>
        </ul>

        <h5>{{ $t('aiops.commands') }}</h5>
        <ul>
          <li v-for="(c, i) in remediationPlan.commands" :key="i">
            <code>{{ c.command }}</code> <span class="muted">[agent={{ c.agentID }} timeout={{ c.timeout }}s]</span>
          </li>
        </ul>

        <div v-if="remediationPlan.safety.notes.length" class="muted">
          <b>{{ $t('aiops.safety_notes') }}:</b>
          <ul>
            <li v-for="(n, i) in remediationPlan.safety.notes" :key="i">{{ n }}</li>
          </ul>
        </div>

        <button class="xs primary" @click="executePlan(remediationPlan)" :disabled="!remediationPlan.safety.requiresConfirm && !remediateReq.constraints.autoExecute" data-testid="aiops-execute-plan-btn">
          {{ remediationPlan.safety.requiresConfirm ? $t('aiops.execute_with_confirm') : $t('aiops.execute_plan') }}
        </button>
      </div>
    </div>

    <!-- Tab 3: 自然语言任务 -->
    <div v-if="activeTab === 'nl'" class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('aiops.nl_title') }}</h3>
        <button class="xs primary" @click="runNL" data-testid="aiops-nl-btn">{{ $t('aiops.nl_run') }}</button>
      </div>

      <div class="form-field">
        <label>{{ $t('aiops.nl_input') }}</label>
        <textarea v-model="nlInput" placeholder="例如：重启设备 dev-web-01 的 nginx 服务"></textarea>
      </div>

      <div v-if="nlLoading" class="muted">{{ $t('common.generating') }}</div>

      <div v-if="nlPlan" class="result-box">
        <h4>{{ $t('aiops.nl_plan_title') }}</h4>
        <ul>
          <li v-for="(c, i) in nlPlan.commands" :key="i">
            <code>{{ c.command }}</code> <span class="muted">[agent={{ c.agentID }}]</span>
          </li>
        </ul>
        <button class="xs primary" @click="executePlan(nlPlan)" data-testid="aiops-execute-nl-btn">{{ $t('aiops.execute_plan') }}</button>
      </div>
    </div>

    <!-- Tab 4: 自动执行 -->
    <div v-if="activeTab === 'execute'" class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('aiops.execute_title') }}</h3>
        <p class="hint">{{ $t('aiops.execute_hint') }}</p>
      </div>

      <div v-if="executeResults.length" class="result-box">
        <h4>{{ $t('aiops.execute_results') }}</h4>
        <div class="result-row"><span>{{ $t('aiops.total') }}:</span> {{ executeResults.length }}</div>
        <div class="result-row"><span>{{ $t('aiops.dispatched') }}:</span> {{ executeStats.dispatched }}</div>
        <ul>
          <li v-for="(r, i) in executeResults" :key="i">
            <b>{{ r.CommandID }}</b>: <span :class="{ ok: r.Status === 'dispatched', warn: r.Status !== 'dispatched' }">{{ r.Status }}</span>
            <span v-if="r.TaskID">taskID={{ r.TaskID }}</span>
            <span v-if="r.AgentID">agent={{ r.AgentID }}</span>
            <span v-if="r.Reason" class="muted">— {{ r.Reason }}</span>
          </li>
        </ul>
      </div>
      <div v-else class="muted">{{ $t('aiops.execute_no_results') }}</div>
    </div>

    <!-- Tab 5: 执行历史 -->
    <div v-if="activeTab === 'history'" class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('aiops.history_title') }}</h3>
        <button class="xs outline" @click="loadExecutions">↻ {{ $t('common.refresh') }}</button>
      </div>

      <div v-if="executionsLoading" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="executions.length === 0" class="muted">{{ $t('aiops.history_no_data') }}</div>
      <div v-else>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ $t('aiops.history_id') }}</th>
              <th>{{ $t('aiops.history_title') }}</th>
              <th>{{ $t('aiops.history_total') }}</th>
              <th>{{ $t('aiops.history_dispatched') }}</th>
              <th>{{ $t('aiops.history_time') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(rec, i) in executions" :key="i">
              <td>{{ rec.ID }}</td>
              <td>{{ rec.PlanTitle }}</td>
              <td>{{ rec.Total }}</td>
              <td>{{ rec.Dispatched }}</td>
              <td>{{ rec.CreatedAt }}</td>
            </tr>
          </tbody>
        </table>
        <div class="result-box" v-if="selectedExec">
          <h4>{{ $t('aiops.history_detail') }}: {{ selectedExec.PlanTitle }}</h4>
          <ul>
            <li v-for="(r, i) in selectedExec.Results" :key="i">
              <b>{{ r.CommandID }}</b>: <span :class="{ ok: r.Status === 'dispatched', warn: r.Status !== 'dispatched' }">{{ r.Status }}</span>
              <span v-if="r.TaskID">taskID={{ r.TaskID }}</span>
              <span v-if="r.AgentID">agent={{ r.AgentID }}</span>
              <span v-if="r.Reason" class="muted">— {{ r.Reason }}</span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { getAIOpsStatus, analyzeAIOps, generateRemediation, generateRemediationNL, executeRemediation, getAIOpsExecutions } from '@/api/aiops'
import { t } from '@/i18n'
import Icon from '@/components/Icon.vue'
import MetricsCard from '@/components/MetricsCard.vue'

const status = reactive({
  enabled: false,
  provider: '',
  model: '',
  stats: { analyzesTotal: 0, remediations: 0, nlParses: 0, executedRemediations: 0 },
  error: ''
})

const activeTab = ref('analyze')

// 分析状态
const analyzeLoading = ref(false)
const analyzeReq = reactive({
  alert: { alertID: '', deviceID: '', severity: 'critical', metric: '', message: '' },
  metrics: {},
  logs: []
})
const analyzeLogText = ref('')
const rootCause = ref(null)

// 修复状态
const remediateLoading = ref(false)
const remediateReq = reactive({
  rootCause: { category: '', component: '', description: '', confidence: 0.9, evidence: [] },
  deviceContext: {},
  constraints: { maxSteps: 5, riskTolerance: 'low', requireApproval: false, timeoutSec: 30, autoExecute: false }
})
const remediationPlan = ref(null)

// 自然语言状态
const nlLoading = ref(false)
const nlInput = ref('')
const nlPlan = ref(null)

// 执行状态
const executeResults = reactive([])
const executeStats = reactive({ dispatched: 0, total: 0 })

// 执行历史
const executionsLoading = ref(false)
const executions = reactive([])
const selectedExec = ref(null)

async function loadStatus() {
  try {
    Object.assign(status, await getAIOpsStatus())
  } catch (e) {
    status.error = e.j?.error || e.s
  }
}

async function loadExecutions() {
  executionsLoading.value = true
  try {
    const resp = await getAIOpsExecutions()
    executions.splice(0, executions.length, ...resp.executions)
  } catch (e) {
    status.error = e.j?.error || e.s
  } finally {
    executionsLoading.value = false
  }
}

async function runAnalyze() {
  analyzeLoading.value = true
  rootCause.value = null
  try {
    analyzeReq.logs = analyzeLogText.value.split('\n').filter(l => l.trim()).map(l => {
      const parts = l.split(/\s+/, 3)
      return { timestamp: new Date().toISOString(), level: parts[0] || 'INFO', source: parts[1] || 'system', message: parts[2] || l }
    })
    const rc = await analyzeAIOps(analyzeReq)
    rootCause.value = rc
  } catch (e) {
    alert(t('common.error') + ': ' + (e.j?.error || e.s))
  } finally {
    analyzeLoading.value = false
  }
}

function useRootCause(rc) {
  remediateReq.rootCause.category = rc.category
  remediateReq.rootCause.component = rc.component
  remediateReq.rootCause.description = rc.description
  remediateReq.rootCause.confidence = rc.confidence
  remediateReq.rootCause.evidence = rc.evidence
  activeTab.value = 'remediate'
}

async function runRemediate() {
  remediateLoading.value = true
  remediationPlan.value = null
  try {
    const plan = await generateRemediation(remediateReq)
    remediationPlan.value = plan
  } catch (e) {
    alert(t('common.error') + ': ' + (e.j?.error || e.s))
  } finally {
    remediateLoading.value = false
  }
}

async function runNL() {
  nlLoading.value = true
  nlPlan.value = null
  try {
    const plan = await generateRemediationNL({ input: nlInput.value, devices: [] })
    nlPlan.value = plan
  } catch (e) {
    alert(t('common.error') + ': ' + (e.j?.error || e.s))
  } finally {
    nlLoading.value = false
  }
}

async function executePlan(plan) {
  const req = { plan, autoExecute: true }
  try {
    const resp = await executeRemediation(req)
    executeResults.splice(0, executeResults.length, ...resp.results)
    executeStats.dispatched = resp.dispatched
    executeStats.total = resp.total
    if (activeTab.value !== 'execute') activeTab.value = 'execute'
  } catch (e) {
    alert(t('common.error') + ': ' + (e.j?.error || e.s))
  }
}

onMounted(() => {
  loadStatus()
  loadExecutions()
})
</script>

<style scoped>
.metrics-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; margin-bottom: 18px; }
.tabs { display: flex; gap: 8px; margin-bottom: 14px; border-bottom: 1px solid var(--border); padding-bottom: 8px; }
.tabs button {
  background: none; border: none; border-bottom: 2px solid transparent;
  padding: 6px 12px; cursor: pointer; font-size: 13px; color: var(--text-2);
}
.tabs button.active { border-bottom-color: var(--accent); color: var(--accent); font-weight: 600; }
.card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 16px; margin-bottom: 14px; }
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }
.form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; margin-bottom: 14px; }
.form-field { display: flex; flex-direction: column; gap: 4px; }
.form-field label { display: block; font-size: 12px; color: var(--text-2); }
.form-field input, .form-field select, .form-field textarea {
  padding: 6px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); font-size: 13px;
}
.form-field textarea { resize: vertical; min-height: 80px; }
.result-box { margin-top: 12px; padding: 12px; background: var(--muted-soft); border-radius: var(--radius-sm); }
.result-row { display: flex; justify-content: space-between; margin: 4px 0; font-size: 13px; }
.result-row span:first-child { color: var(--text-2); }
.metric-sub { font-size: 11px; color: var(--text-2); margin-top: 2px; }
.ok { color: var(--ok); }
.warn { color: var(--fail); }
ul { margin: 6px 0; padding-left: 20px; }
code { background: var(--muted); padding: 2px 6px; border-radius: var(--radius-sm); font-size: 12px; }
.hint { font-size: 12px; color: var(--text-2); margin-top: 4px; }
.poll-err { padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm); background: var(--fail-soft); border: 1px solid var(--fail-bg); color: var(--fail); font-size: 12.5px; font-weight: 500; }
.data-table { width: 100%; border-collapse: collapse; margin-top: 10px; font-size: 13px; }
.data-table th, .data-table td { border: 1px solid var(--border); padding: 6px 8px; text-align: left; }
.data-table th { background: var(--muted); color: var(--text-2); font-size: 12px; }
.data-table tr:hover { background: var(--muted-soft); }
@media (max-width: 768px) { .metrics-row { grid-template-columns: repeat(2, 1fr); } }
</style>