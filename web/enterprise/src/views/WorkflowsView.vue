<template>
  <div data-testid="workflows-view">
    <h2 data-testid="workflows-title">{{ $t('workflows.title') }}</h2>
    <p class="muted">{{ $t('workflows.subtitle') }}</p>

    <div class="flowbar">
      <div class="field">
        <label>{{ $t('workflows.workflow_label') }}</label>
        <select v-model="selectedId" data-testid="workflows-select" @change="onOpen">
          <option value="">{{ $t('workflows.new_blank') }}</option>
          <option v-for="w in store.list" :key="w.id" :value="w.id">
            #{{ w.id }} {{ w.name }} [{{ w.status }}]
          </option>
        </select>
      </div>
      <div class="field">
        <label>{{ $t('workflows.agent_label') }}</label>
        <select v-model="store.current.agentID" data-testid="workflows-agent-select">
          <option v-for="a in agents" :key="a.agentID" :value="a.agentID">
            {{ a.agentID }} ({{ a.hostname }})
          </option>
        </select>
      </div>
      <div class="field">
        <label>{{ $t('workflows.name_label') }}</label>
        <input v-model="store.current.name" :placeholder="$t('workflows.name_placeholder')" data-testid="input-workflow-name" />
      </div>
      <div class="field">
        <label>{{ $t('workflows.cron_label') }}</label>
        <input v-model="store.current.cron" placeholder="*/5 * * * *" data-testid="input-workflow-cron" />
      </div>
    </div>

    <div class="btnbar">
      <button class="primary" data-testid="workflows-save-btn" @click="onSave">{{ $t('workflows.save_btn') }}</button>
      <button class="teal" data-testid="workflows-run-btn" @click="onRun" :disabled="!store.current.id">{{ $t('workflows.run_btn') }}</button>
      <button data-testid="workflows-schedule-btn" @click="onSchedule" :disabled="!store.current.id">{{ $t('workflows.schedule_btn') }}</button>
      <button data-testid="workflows-add-step-btn" @click="store.addNode()">{{ $t('workflows.add_step_btn') }}</button>
      <button data-testid="workflows-auto-layout-btn" @click="store.autoLayout()">{{ $t('workflows.auto_layout_btn') }}</button>
      <button data-testid="workflows-load-demo-btn" @click="loadDemo">{{ $t('workflows.load_demo_btn') }}</button>
      <button data-testid="workflows-clear-btn" @click="store.reset()">{{ $t('workflows.clear_btn') }}</button>
    </div>

    <p v-if="store.msg" :class="['msg', store.error ? 'err' : 'ok']" data-testid="workflows-msg">{{ store.msg }}</p>
    <p v-if="store.error" class="msg err" data-testid="workflows-error-msg">{{ store.error }}</p>

    <!-- DAG 画布 -->
    <svg ref="canvasRef" class="canvas" data-testid="workflows-canvas" @click="onCanvasClick">
      <defs>
        <pattern id="grid" width="26" height="26" patternUnits="userSpaceOnUse">
          <circle class="grid-dot" cx="2" cy="2" r="1" />
        </pattern>
        <marker id="arrow" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto" markerUnits="strokeWidth">
          <path class="arrow-head" d="M0,0 L8,3 L0,6 Z" />
        </marker>
      </defs>
      <rect x="-3000" y="-3000" width="8000" height="8000" fill="url(#grid)" />
      <!-- 边 -->
      <g>
        <template v-for="n in store.current.dag" :key="'e-' + n.id">
          <line
            v-for="d in (n.dependsOn || [])"
            :key="n.id + '-' + d"
            class="edge"
            :x1="edgeStart(d).x" :y1="edgeStart(d).y"
            :x2="edgeEnd(n.id).x" :y2="edgeEnd(n.id).y"
            marker-end="url(#arrow)"
          />
        </template>
      </g>
      <!-- 节点 -->
      <g
        v-for="n in store.current.dag"
        :key="n.id"
        class="node"
        :class="{ sel: store.selectedNode === n.id, run: store.status[n.id] === 'running', fail: store.status[n.id] === 'failed' }"
        :transform="`translate(${(store.nodePos[n.id] || {x:60,y:60}).x},${(store.nodePos[n.id] || {x:60,y:60}).y})`"
        :data-testid="'workflows-node-' + n.id"
        @click.stop="store.selectedNode = n.id"
      >
        <rect class="card" width="170" height="66" rx="10" ry="10" />
        <rect width="4" height="66" rx="2" :fill="typeColor(n.type)" />
        <text x="14" y="24" class="ntitle">{{ n.name || n.id }}</text>
        <text x="14" y="44" class="ncmd">▸ {{ (n.command || noCommandText).slice(0, 20) }}</text>
        <rect :x="170 - 48" y="9" width="40" height="16" rx="8" :fill="typeSoft(n.type)" />
        <text :x="170 - 28" y="21" class="ntype" :fill="typeColor(n.type)">{{ n.type }}</text>
      </g>
    </svg>

    <!-- 节点编辑器 -->
    <div v-if="currentNode" class="node-editor" data-testid="workflows-node-editor">
      <h4>{{ $t('workflows.edit_node_title', { id: currentNode.id }) }}</h4>
      <div class="row">
        <div class="field">
          <label>{{ $t('workflows.name_label') }}</label>
          <input v-model="currentNode.name" data-testid="input-node-name" />
        </div>
        <div class="field">
          <label>{{ $t('workflows.type_label') }}</label>
          <select v-model="currentNode.type" data-testid="input-node-type">
            <option value="shell">shell</option>
            <option value="file">file</option>
            <option value="service">service</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label>{{ $t('workflows.command_label') }}</label>
        <input v-model="currentNode.command" style="width: 70%" data-testid="input-node-command" />
      </div>
      <div class="field">
        <label>{{ $t('workflows.depends_label') }}</label>
        <input :value="(currentNode.dependsOn || []).join(', ')" data-testid="input-node-depends" @change="onDepsChange" />
      </div>
      <button class="danger xs" data-testid="workflows-delete-node-btn" @click="store.deleteNode(currentNode.id)">{{ $t('workflows.delete_node_btn') }}</button>
    </div>
  </div>

  <!-- Cron 定时输入（替代 prompt） -->
  <PromptModal
    v-model="cronModal.show"
    data-testid="workflows-cron-modal"
    :title="$t('workflows.cron_title')"
    :message="$t('workflows.cron_prompt')"
    :default-value="store.current.cron || '*/5 * * * *'"
    placeholder="*/5 * * * *"
    @confirm="onCronConfirm"
  />
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useWorkflowStore } from '@/stores/workflow'
import { getAgents } from '@/api/device'
import { t } from '@/i18n'
import PromptModal from '@/components/PromptModal.vue'

const store = useWorkflowStore()
const agents = ref([])
const selectedId = ref('')
const canvasRef = ref(null)
const cronModal = reactive({ show: false })

const NODE_W = 170, NODE_H = 66
// 节点类型配色改用主题 token（--indigo/--teal/--amber 及对应 soft），
// 浅/深主题下自动取对应色值，避免暗色主题下硬编码浅色冲突。
const TYPE_COLOR = { shell: 'var(--indigo)', file: 'var(--teal)', service: 'var(--amber)' }
const TYPE_SOFT = { shell: 'var(--indigo-soft)', file: 'var(--teal-soft)', service: 'var(--amber-soft)' }
function typeColor(t) { return TYPE_COLOR[t] || TYPE_COLOR.shell }
function typeSoft(t) { return TYPE_SOFT[t] || TYPE_SOFT.shell }

const noCommandText = t('workflows.no_command')

const currentNode = computed(() =>
  store.current.dag.find((n) => n.id === store.selectedNode) || null
)

function edgeStart(srcId) {
  const p = store.nodePos[srcId] || { x: 60, y: 60 }
  return { x: p.x + NODE_W / 2, y: p.y + NODE_H }
}
function edgeEnd(dstId) {
  const p = store.nodePos[dstId] || { x: 60, y: 60 }
  return { x: p.x + NODE_W / 2, y: p.y }
}
function onCanvasClick() { store.selectedNode = null }
function onDepsChange(e) {
  if (!currentNode.value) return
  currentNode.value.dependsOn = e.target.value
    .split(/[,\s]+/).map((s) => s.trim()).filter(Boolean)
}
async function onOpen() {
  await store.open(selectedId.value)
}
async function onSave() {
  try { await store.save(); store.msg = t('workflows.saved_msg', { id: store.current.id }); store.error = '' }
  catch (e) { console.error('save workflow failed:', e) }
}
async function onRun() {
  try {
    const r = await store.run()
    store.msg = `[${r.s}] ${JSON.stringify(r.j)}`; store.error = r.s >= 400 ? 'run' : ''
  } catch (e) { console.error('run workflow failed:', e) }
}
async function onSchedule() {
  cronModal.show = true
}
async function onCronConfirm(cron) {
  if (!cron) return
  try {
    const r = await store.schedule(cron)
    store.msg = t('workflows.schedule_set_msg', { cron: r.j.cron || t('workflows.none') }); store.error = ''
  } catch (e) { console.error('schedule workflow failed:', e) }
}
function loadDemo() {
  store.reset()
  store.current.name = t('workflows.demo_name')
  store.current.dag = [
    { id: 'n1', name: t('workflows.demo_step_pull'), type: 'shell', command: 'docker pull nginx:latest', path: '', dependsOn: [] },
    { id: 'n2', name: t('workflows.demo_step_stop'), type: 'shell', command: 'docker stop nginx', path: '', dependsOn: ['n1'] },
    { id: 'n3', name: t('workflows.demo_step_start'), type: 'service', command: 'nginx', path: '', dependsOn: ['n2'] }
  ]
  store.autoLayout()
  store.msg = t('workflows.demo_loaded_msg'); store.error = ''
}

onMounted(async () => {
  store.fetchList()
  try { agents.value = await getAgents() || [] } catch (e) { console.error('fetch agents failed:', e) }
  if (agents.value.length) store.current.agentID = agents.value[0].agentID
})
</script>

<style scoped>
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.flowbar .field { display: flex; align-items: center; gap: 6px; }
.flowbar label { margin: 0; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
.field label { margin: 0; }

.canvas {
  display: block; width: 100%; height: 560px;
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); box-shadow: var(--shadow);
}
.canvas .grid-dot { fill: var(--border-2); }
.canvas .arrow-head { fill: var(--text-3); }
.canvas .card { fill: var(--surface); stroke: var(--border-2); stroke-width: 1.5px; }
.canvas .node { cursor: pointer; }
.canvas .node.sel .card { stroke: var(--accent); stroke-width: 2.5px; filter: drop-shadow(0 4px 10px rgba(99,102,241,.2)); }
.canvas .ntitle { fill: var(--text); font-size: 13px; font-weight: 600; pointer-events: none; }
.canvas .ncmd { fill: var(--text-3); font-size: 11px; pointer-events: none; }
.canvas .ntype { font-size: 10px; font-weight: 600; text-anchor: middle; pointer-events: none; }
.canvas line.edge { stroke: var(--text-3); stroke-width: 1.5px; }
.canvas .node.run .card { animation: runPulse 1.1s ease-in-out infinite; }
@keyframes runPulse { 0%,100% { filter: drop-shadow(0 0 0 rgba(99,102,241,0)); } 50% { filter: drop-shadow(0 0 11px rgba(99,102,241,.65)); } }
.canvas .node.fail .card { animation: failPulse 1.1s ease-in-out infinite; }
@keyframes failPulse { 0%,100% { filter: drop-shadow(0 0 0 rgba(225,29,72,0)); } 50% { filter: drop-shadow(0 0 13px rgba(225,29,72,.75)); } }

.node-editor {
  margin-top: 12px; padding: 14px;
  background: var(--bg-soft); border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
</style>
