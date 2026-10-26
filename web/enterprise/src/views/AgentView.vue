<template>
  <div class="agent-page">
    <div class="page-head">
      <h2 class="page-title">{{ $t('agent.title') }}</h2>
      <p class="page-desc">{{ $t('agent.desc') }}</p>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 意图分类 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('agent.classify_title') }}</h3>
      </div>
      <div class="row">
        <div class="field" style="flex: 1;">
          <label>{{ $t('agent.input_label') }}</label>
          <textarea v-model="input" rows="3" :placeholder="$t('agent.input_placeholder')" class="agent-input"></textarea>
        </div>
      </div>
      <div class="btnbar">
        <Button variant="primary" size="sm" @click="classify" :disabled="classifying">{{ $t('agent.classify_btn') }}</Button>
        <Button variant="ghost" size="sm" @click="decompose" :disabled="decomposing">{{ $t('agent.decompose_btn') }}</Button>
        <Button variant="primary" size="sm" @click="execute" :disabled="store.loading || !input.trim()">{{ $t('agent.execute_btn') }}</Button>
        <label class="chk"><input type="checkbox" v-model="useDAG" /> {{ $t('agent.use_dag') }}</label>
      </div>
      <div v-if="store.intentType" class="result-box">
        <div class="result-row"><span>{{ $t('agent.intent') }}:</span> {{ store.intentType }}</div>
        <div class="result-row"><span>{{ $t('agent.confidence') }}:</span> {{ (store.intentConfidence * 100).toFixed(0) }}%</div>
      </div>
    </div>

    <!-- 任务拆解结果 -->
    <div class="card" v-if="store.decomposedTasks.length">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('agent.decompose_title') }}</h3>
      </div>
      <DataTable
        :columns="taskColumns"
        :rows="store.decomposedTasks"
        row-key="id"
        :empty-text="$t('agent.noTasks')"
      />
    </div>

    <!-- 执行结果 -->
    <div class="card" v-if="store.currentResult">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('agent.result_title') }}</h3>
      </div>
      <div class="result-box">
        <div class="result-row"><span>{{ $t('agent.success') }}:</span> {{ store.currentResult.success ? $t('common.yes') : $t('common.no') }}</div>
        <div class="result-row"><span>{{ $t('agent.agent_type') }}:</span> {{ store.currentResult.agentType }}</div>
        <div class="result-row"><span>{{ $t('agent.duration') }}:</span> {{ (store.currentResult.duration / 1000000).toFixed(0) }}ms</div>
        <div class="result-row"><span>{{ $t('agent.tokens') }}:</span> {{ store.currentResult.tokenUsage }}</div>
      </div>
      <div class="result-content">{{ store.currentResult.output }}</div>
      <div v-if="store.currentResult.steps?.length" class="steps-section">
        <h4>{{ $t('agent.steps') }}</h4>
        <div v-for="step in store.currentResult.steps" :key="step.step" class="step-item">
          <div class="step-header">Step {{ step.step }}</div>
          <div class="step-thought">{{ step.thought }}</div>
          <div v-if="step.action" class="step-action">Action: {{ step.action }}({{ step.actionInput }})</div>
          <div v-if="step.observation" class="step-obs">Observation: {{ step.observation }}</div>
        </div>
      </div>
      <div v-if="store.currentResult.toolCalls?.length" class="tools-section">
        <h4>{{ $t('agent.tool_calls') }}</h4>
        <div v-for="tc in store.currentResult.toolCalls" :key="tc.toolName + tc.timestamp" class="tool-item">
          <span class="tool-name">{{ tc.toolName }}</span>
          <span class="tool-duration">{{ (tc.duration / 1000000).toFixed(0) }}ms</span>
        </div>
      </div>
    </div>

    <!-- 可用工具 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('agent.tools_title') }}</h3>
        <Button variant="ghost" size="sm" @click="store.fetchTools()">{{ $t('common.refresh') }}</Button>
      </div>
      <div class="tools-grid">
        <div v-for="tool in store.tools" :key="tool.name" class="tool-card">
          <div class="tool-name">{{ tool.name }}</div>
          <div class="tool-desc">{{ tool.description }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useAgentStore } from '@/stores/agent'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'

const store = useAgentStore()

const input = ref('')
const classifying = ref(false)
const decomposing = ref(false)
const useDAG = ref(false)

const taskColumns = [
  { key: 'id', title: 'ID' },
  { key: 'description', title: t('agent.task_desc') },
  { key: 'agentType', title: t('agent.agent_type') },
  { key: 'priority', title: t('agent.priority') },
  { key: 'dependencies', title: t('agent.dependencies') }
]

async function classify() {
  classifying.value = true
  try { await store.classify(input.value) }
  catch { /* ignore */ }
  finally { classifying.value = false }
}

async function decompose() {
  decomposing.value = true
  try { await store.decompose(input.value) }
  catch { /* ignore */ }
  finally { decomposing.value = false }
}

async function execute() {
  try {
    await store.execute(input.value, 'admin', 'default', 'session-1', useDAG.value)
  } catch { /* ignore */ }
}
</script>

<style scoped>
.agent-page { max-width: 900px; margin: 0 auto; }
.page-head { margin-bottom: 20px; }
.page-title { margin: 0 0 4px 0; font-size: 20px; font-weight: 600; }
.page-desc { margin: 0; color: var(--color-ink-muted, #6b7280); font-size: 13px; }
.card { background: var(--color-surface-1, #ffffff); border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-md, 8px); padding: 16px 18px; margin-bottom: 16px; }
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }
.row { display: flex; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 200px; }
.field label { font-size: 11px; font-weight: 500; color: var(--color-ink-muted, #6b7280); }
.agent-input { width: 100%; padding: 8px 10px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 13px; resize: vertical; min-height: 80px; background: var(--color-surface-1, #ffffff); }
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; align-items: center; }
.chk { display: flex; align-items: center; gap: 4px; font-size: 12px; }
.result-box { margin-top: 12px; padding: 12px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); }
.result-row { display: flex; justify-content: space-between; margin: 4px 0; font-size: 13px; }
.result-row span:first-child { color: var(--color-ink-muted, #6b7280); }
.result-content { margin-top: 12px; padding: 12px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); font-size: 13px; line-height: 1.6; white-space: pre-wrap; }
.steps-section, .tools-section { margin-top: 12px; }
.steps-section h4, .tools-section h4 { margin: 0 0 8px 0; font-size: 13px; font-weight: 600; }
.step-item { padding: 8px 10px; margin-bottom: 6px; border-left: 3px solid var(--color-primary, #3b82f6); background: var(--color-surface-2, #f7f9fc); border-radius: 0 6px 6px 0; font-size: 12px; }
.step-header { font-weight: 600; margin-bottom: 4px; }
.step-thought { margin-bottom: 4px; }
.step-action { color: var(--color-primary, #3b82f6); }
.step-obs { color: var(--color-ink-muted, #6b7280); }
.tool-item { display: flex; justify-content: space-between; padding: 6px 8px; margin-bottom: 4px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); font-size: 12px; }
.tool-name { font-weight: 600; }
.tool-duration { color: var(--color-ink-muted, #6b7280); }
.tools-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
.tool-card { padding: 10px 12px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); }
.tool-card .tool-name { font-size: 13px; font-weight: 600; margin-bottom: 4px; }
.tool-card .tool-desc { font-size: 11px; color: var(--color-ink-muted, #6b7280); }
.muted { color: var(--color-ink-muted, #6b7280); font-size: 12.5px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0 12px; border-radius: var(--radius-sm, 6px);
  background: var(--color-error-soft, #fef2f2); border: 1px solid var(--color-error-light, #fecaca);
  color: var(--color-error, #ef4444); font-size: 12.5px; font-weight: 500;
}
</style>