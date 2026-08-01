<template>
  <div>
    <h2>作业编排</h2>
    <p class="muted">DAG 作业流编辑器：节点表示步骤，边表示依赖。支持自动布局、保存、运行、定时调度。</p>

    <div class="flowbar">
      <div class="field">
        <label>作业流</label>
        <select v-model="selectedId" @change="onOpen">
          <option value="">（新建空白作业流）</option>
          <option v-for="w in store.list" :key="w.id" :value="w.id">
            #{{ w.id }} {{ w.name }} [{{ w.status }}]
          </option>
        </select>
      </div>
      <div class="field">
        <label>采集端</label>
        <select v-model="store.current.agentID">
          <option v-for="a in agents" :key="a.agentID" :value="a.agentID">
            {{ a.agentID }} ({{ a.hostname }})
          </option>
        </select>
      </div>
      <div class="field">
        <label>名称</label>
        <input v-model="store.current.name" placeholder="作业流名称" />
      </div>
      <div class="field">
        <label>Cron</label>
        <input v-model="store.current.cron" placeholder="*/5 * * * *" />
      </div>
    </div>

    <div class="btnbar">
      <button class="primary" @click="onSave">💾 保存</button>
      <button class="teal" @click="onRun" :disabled="!store.current.id">▶ 运行</button>
      <button @click="onSchedule" :disabled="!store.current.id">⏰ 定时</button>
      <button @click="store.addNode()">＋ 添加步骤</button>
      <button @click="store.autoLayout()">⊞ 自动布局</button>
      <button @click="loadDemo">📋 载入示例</button>
      <button @click="store.reset()">✕ 清空</button>
    </div>

    <p v-if="store.msg" :class="['msg', store.error ? 'err' : 'ok']">{{ store.msg }}</p>
    <p v-if="store.error" class="msg err">{{ store.error }}</p>

    <!-- DAG 画布 -->
    <svg ref="canvasRef" class="canvas" @click="onCanvasClick">
      <defs>
        <pattern id="grid" width="26" height="26" patternUnits="userSpaceOnUse">
          <circle cx="2" cy="2" r="1" fill="#dbe2f1" />
        </pattern>
        <marker id="arrow" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto" markerUnits="strokeWidth">
          <path d="M0,0 L8,3 L0,6 Z" fill="#94a3b8" />
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
        @click.stop="store.selectedNode = n.id"
      >
        <rect class="card" width="170" height="66" rx="10" ry="10" />
        <rect width="4" height="66" rx="2" :fill="typeColor(n.type)" />
        <text x="14" y="24" class="ntitle">{{ n.name || n.id }}</text>
        <text x="14" y="44" class="ncmd">▸ {{ (n.command || '(无命令)').slice(0, 20) }}</text>
        <rect :x="170 - 48" y="9" width="40" height="16" rx="8" :fill="typeSoft(n.type)" />
        <text :x="170 - 28" y="21" class="ntype" :fill="typeColor(n.type)">{{ n.type }}</text>
      </g>
    </svg>

    <!-- 节点编辑器 -->
    <div v-if="currentNode" class="node-editor">
      <h4>编辑节点 {{ currentNode.id }}</h4>
      <div class="row">
        <div class="field">
          <label>名称</label>
          <input v-model="currentNode.name" />
        </div>
        <div class="field">
          <label>类型</label>
          <select v-model="currentNode.type">
            <option value="shell">shell</option>
            <option value="file">file</option>
            <option value="service">service</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label>命令</label>
        <input v-model="currentNode.command" style="width: 70%" />
      </div>
      <div class="field">
        <label>依赖（逗号分隔的节点 id）</label>
        <input :value="(currentNode.dependsOn || []).join(', ')" @change="onDepsChange" />
      </div>
      <button class="danger xs" @click="store.deleteNode(currentNode.id)">删除节点</button>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useWorkflowStore } from '@/stores/workflow'
import { getAgents } from '@/api/device'

const store = useWorkflowStore()
const agents = ref([])
const selectedId = ref('')
const canvasRef = ref(null)

const NODE_W = 170, NODE_H = 66
const TYPE_COLOR = { shell: '#6366f1', file: '#0d9488', service: '#d97706' }
const TYPE_SOFT = { shell: '#eceaff', file: '#d8f3ef', service: '#fef3e2' }
function typeColor(t) { return TYPE_COLOR[t] || '#6366f1' }
function typeSoft(t) { return TYPE_SOFT[t] || '#eceaff' }

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
  try { await store.save(); store.msg = '已保存 #' + store.current.id; store.error = '' }
  catch (_) { /* 错误已记录 */ }
}
async function onRun() {
  try {
    const r = await store.run()
    store.msg = `[${r.s}] ${JSON.stringify(r.j)}`; store.error = r.s >= 400 ? 'run' : ''
  } catch (_) { /* */ }
}
async function onSchedule() {
  const cron = prompt('Cron 表达式：', store.current.cron || '*/5 * * * *')
  if (!cron) return
  try {
    const r = await store.schedule(cron)
    store.msg = `已设置定时: ${r.j.cron || '(无)'}`; store.error = ''
  } catch (_) { /* */ }
}
function loadDemo() {
  store.reset()
  store.current.name = '示例-nginx发布'
  store.current.dag = [
    { id: 'n1', name: '拉取镜像', type: 'shell', command: 'docker pull nginx:latest', path: '', dependsOn: [] },
    { id: 'n2', name: '停旧容器', type: 'shell', command: 'docker stop nginx', path: '', dependsOn: ['n1'] },
    { id: 'n3', name: '起新容器', type: 'service', command: 'nginx', path: '', dependsOn: ['n2'] }
  ]
  store.autoLayout()
  store.msg = '已载入示例作业流（尚未保存）'; store.error = ''
}

onMounted(async () => {
  store.fetchList()
  try { agents.value = await getAgents() || [] } catch (_) { /* */ }
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