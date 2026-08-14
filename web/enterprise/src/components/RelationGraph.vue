<template>
  <!--
    RelationGraph — CMDB 关系图谱可视化组件
    纯 SVG 实现，无第三方图表库依赖：
    - 力导向布局（force）：斥力 + 弹簧力 + 中心引力，迭代收敛
    - 网络拓扑布局（topology）：按 CI 类型分层，体现 cluster→machine→os→service→app 层次
    - 交互：节点拖拽、点击选中、hover 高亮、滚轮缩放、空白拖拽平移
    - 不同 CI 类型用不同颜色，不同关系类型用不同线型
    - 图例 + 节点详情面板
  -->
  <div class="rg-wrap" :style="{ width: width + 'px', height: height + 'px' }">
    <!-- 工具栏：布局模式切换 + 缩放 + 重置 -->
    <div class="rg-toolbar">
      <button
        v-for="m in modes"
        :key="m.key"
        :class="['rg-tb-btn', mode === m.key ? 'active' : '']"
        @click="$emit('update:mode', m.key)"
        :title="m.label"
      >{{ m.icon }}</button>
      <span class="rg-tb-sep" />
      <button class="rg-tb-btn" @click="zoomBy(1.2)" title="放大">＋</button>
      <button class="rg-tb-btn" @click="zoomBy(1 / 1.2)" title="缩小">－</button>
      <button class="rg-tb-btn" @click="resetView" title="重置视图">⟲</button>
      <span class="rg-tb-sep" />
      <span class="rg-tb-info">{{ nodes.length }} 节点 · {{ edges.length }} 边</span>
    </div>

    <!-- SVG 画布 -->
    <svg
      ref="svgEl"
      :width="width"
      :height="height"
      class="rg-svg"
      @mousedown="onCanvasMouseDown"
      @wheel.prevent="onWheel"
    >
      <defs>
        <!-- 箭头标记：每种关系类型一个颜色 -->
        <marker
          v-for="rt in relationTypes"
          :id="`rg-arrow-${rt}`"
          :key="rt"
          viewBox="0 0 10 10" refX="9" refY="5"
          markerWidth="7" markerHeight="7" orient="auto-start-reverse"
        >
          <path d="M 0 0 L 10 5 L 0 10 z" :fill="relationColor(rt)" />
        </marker>
      </defs>

      <!-- 变换组：缩放 + 平移 -->
      <g class="rg-canvas" :transform="`translate(${pan.x},${pan.y}) scale(${scale})`">
        <!-- 边 -->
        <g class="rg-edges">
          <g
            v-for="(e, i) in edges"
            :key="'e' + i"
            :class="['rg-edge', hoveredEdge === i ? 'hovered' : '']"
            @mouseenter="hoveredEdge = i"
            @mouseleave="hoveredEdge = -1"
          >
            <line
              :x1="nodePos(e.source).x" :y1="nodePos(e.source).y"
              :x2="nodePos(e.target).x" :y2="nodePos(e.target).y"
              :stroke="relationColor(e.relationType)"
              :stroke-width="edgeWidth(e.relationType)"
              :stroke-dasharray="edgeDash(e.relationType)"
              :marker-end="`url(#rg-arrow-${e.relationType})`"
              class="rg-edge-line"
            />
            <!-- 边标签：关系类型 -->
            <text
              :x="edgeLabelPos(e).x" :y="edgeLabelPos(e).y"
              class="rg-edge-label" :fill="relationColor(e.relationType)"
            >{{ e.relationType }}</text>
          </g>
        </g>

        <!-- 节点 -->
        <g class="rg-nodes">
          <g
            v-for="n in nodes"
            :key="n.id"
            :transform="`translate(${nodePos(n.id).x},${nodePos(n.id).y})`"
            :class="['rg-node', n.isCenter ? 'center' : '', selectedNode === n.id ? 'selected' : '', hoveredNode === n.id ? 'hovered' : '']"
            @mousedown.stop="onNodeMouseDown($event, n)"
            @click.stop="onNodeClick(n)"
            @mouseenter="onNodeHover(n)"
            @mouseleave="onNodeHover(null)"
          >
            <!-- 节点圆形 -->
            <circle
              :r="nodeRadius(n)"
              :fill="typeColor(n.type)"
              :stroke="n.isCenter ? 'var(--accent)' : 'var(--surface-2)'"
              :stroke-width="n.isCenter ? 3 : 1.5"
              class="rg-node-circle"
            />
            <!-- 节点名称 -->
            <text
              :y="nodeRadius(n) + 14"
              class="rg-node-label"
            >{{ n.name || n.id }}</text>
            <!-- 节点类型小标签 -->
            <text
              :y="nodeRadius(n) + 28"
              class="rg-node-type"
            >{{ n.type }}</text>
          </g>
        </g>
      </g>
    </svg>

    <!-- 图例 -->
    <div class="rg-legend">
      <div class="rg-legend-section">
        <span class="rg-legend-title">{{ $t('cmdb.graph_legend_type') }}</span>
        <span v-for="tp in legendTypes" :key="tp" class="rg-legend-item">
          <span class="rg-legend-dot" :style="{ background: typeColor(tp) }" />
          {{ tp }}
        </span>
      </div>
      <div class="rg-legend-section">
        <span class="rg-legend-title">{{ $t('cmdb.graph_legend_rel') }}</span>
        <span v-for="rt in relationTypes" :key="rt" class="rg-legend-item">
          <svg width="22" height="6" class="rg-legend-line">
            <line
              x1="0" y1="3" x2="22" y2="3"
              :stroke="relationColor(rt)"
              :stroke-dasharray="edgeDash(rt)"
            />
          </svg>
          {{ rt }}
        </span>
      </div>
    </div>

    <!-- 节点详情浮层 -->
    <div v-if="focusedNode" class="rg-detail">
      <div class="rg-detail-head">
        <span class="rg-detail-dot" :style="{ background: typeColor(focusedNode.type) }" />
        <b>{{ focusedNode.name || focusedNode.id }}</b>
        <span class="rg-detail-type">{{ focusedNode.type }}</span>
        <button class="rg-detail-close" @click="focusedNode = null">×</button>
      </div>
      <div class="rg-detail-body">
        <div class="rg-detail-row"><span class="muted">ID</span><code>{{ focusedNode.id }}</code></div>
        <div v-if="focusedNode.status" class="rg-detail-row">
          <span class="muted">{{ $t('cmdb.status_label') }}</span>{{ focusedNode.status }}
        </div>
        <div v-if="focusedNode.source" class="rg-detail-row">
          <span class="muted">{{ $t('cmdb.source_label') }}</span>{{ focusedNode.source }}
        </div>
        <div v-if="focusedNode.attrs && Object.keys(focusedNode.attrs).length" class="rg-detail-attrs">
          <span class="muted">{{ $t('cmdb.attrs_label') }}</span>
          <div class="rg-detail-attr" v-for="(v, k) in focusedNode.attrs" :key="k">
            <code>{{ k }}</code>={{ v }}
          </div>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <div v-if="!nodes.length" class="rg-empty">{{ $t('cmdb.graph_empty') }}</div>
  </div>
</template>

<script setup>
// RelationGraph — CMDB 关系图谱可视化（力导向 / 网络拓扑）
// 纯 SVG + 轻量力模拟，零第三方依赖；布局算法为确定性实现，便于测试。
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import { t } from '@/i18n'

const props = defineProps({
  // CIRelationGraph 数据：{ centerCI, relations: [{ sourceCIID, targetCIID, relationType, sourceName, targetName, targetType, ... }] }
  graph: { type: Object, default: () => null },
  // 布局模式：'force' 力导向 | 'topology' 网络拓扑分层
  mode: { type: String, default: 'force' },
  // 画布尺寸
  width: { type: Number, default: 520 },
  height: { type: Number, default: 420 },
  // 节点是否可点击选中
  selectable: { type: Boolean, default: true }
})
const emit = defineEmits(['node-click', 'node-hover', 'update:mode'])

// === CI 类型 → 颜色 映射 ===
const TYPE_COLORS = {
  cluster: 'var(--info)',
  machine: 'var(--accent)',
  os: 'var(--teal)',
  service: 'var(--ok)',
  app: 'var(--warn)',
  default: 'var(--text-2)'
}
function typeColor(type) {
  return TYPE_COLORS[type] || TYPE_COLORS.default
}

// === 关系类型 → 颜色 / 线宽 / 虚线 映射 ===
const RELATION_COLORS = {
  runs_on: 'var(--accent)',
  depends: 'var(--warn)',
  connects_to: 'var(--teal)',
  contains: 'var(--info)',
  member_of: 'var(--text-3)',
  default: 'var(--text-2)'
}
const RELATION_DASH = {
  runs_on: 'none',
  depends: '6 4',
  connects_to: '2 3',
  contains: 'none',
  member_of: '6 4',
  default: 'none'
}
const RELATION_WIDTH = {
  contains: 2.5,
  default: 1.5
}
function relationColor(rt) {
  return RELATION_COLORS[rt] || RELATION_COLORS.default
}
function edgeDash(rt) {
  return RELATION_DASH[rt] || RELATION_DASH.default
}
function edgeWidth(rt) {
  return RELATION_WIDTH[rt] || RELATION_WIDTH.default
}

// === 从 graph 提取节点与边 ===
// 节点：centerCI + 所有 relation 涉及的 source/target CI（去重）
// 边：每条 relation 为一条有向边 source→target
const nodes = computed(() => {
  const g = props.graph
  if (!g) return []
  const map = new Map()
  // 中心节点
  if (g.centerCI) {
    map.set(g.centerCI.id, {
      id: g.centerCI.id,
      name: g.centerCI.name,
      type: g.centerCI.ciType,
      status: g.centerCI.status,
      source: g.centerCI.source,
      attrs: g.centerCI.attrs,
      isCenter: true
    })
  }
  // 关系两端节点
  const rels = g.relations || []
  for (const r of rels) {
    if (!map.has(r.sourceCIID)) {
      map.set(r.sourceCIID, {
        id: r.sourceCIID,
        name: r.sourceName || r.sourceCIID,
        type: inferType(r, 'source'),
        isCenter: false
      })
    }
    if (!map.has(r.targetCIID)) {
      map.set(r.targetCIID, {
        id: r.targetCIID,
        name: r.targetName || r.targetCIID,
        type: r.targetType || 'unknown',
        isCenter: false
      })
    }
    // 若中心节点存在于关系端点，补全 isCenter
    if (g.centerCI && r.sourceCIID === g.centerCI.id) {
      map.get(r.sourceCIID).isCenter = true
    }
    if (g.centerCI && r.targetCIID === g.centerCI.id) {
      map.get(r.targetCIID).isCenter = true
    }
  }
  return Array.from(map.values())
})

// 推断 source 端 CI 类型：若与 centerCI 相同则用其类型，否则 unknown
function inferType(r, side) {
  const g = props.graph
  if (g && g.centerCI) {
    if (side === 'source' && r.sourceCIID === g.centerCI.id) return g.centerCI.ciType
    if (side === 'target' && r.targetCIID === g.centerCI.id) return g.centerCI.ciType
  }
  if (side === 'source') {
    // source 端类型未在 RelationWithTarget 中直接给出，用 target 反推不可靠，标记 unknown
    return 'unknown'
  }
  return r.targetType || 'unknown'
}

const edges = computed(() => {
  const g = props.graph
  if (!g) return []
  return (g.relations || []).map(r => ({
    source: r.sourceCIID,
    target: r.targetCIID,
    relationType: r.relationType
  }))
})

// 图例中出现的类型与关系类型（去重 + 保序）
const legendTypes = computed(() => {
  const seen = new Set()
  const out = []
  for (const n of nodes.value) {
    if (!seen.has(n.type)) { seen.add(n.type); out.push(n.type) }
  }
  return out
})
const relationTypes = computed(() => {
  const seen = new Set()
  const out = []
  for (const e of edges.value) {
    if (!seen.has(e.relationType)) { seen.add(e.relationType); out.push(e.relationType) }
  }
  return out
})

// === 布局：节点位置 ===
// positions: Map<id, {x, y}>，由布局算法计算
const positions = ref(new Map())

// 力导向布局：确定性初始位置（圆周均匀分布）+ 迭代力模拟
function computeForceLayout() {
  const ns = nodes.value
  const es = edges.value
  if (!ns.length) { positions.value = new Map(); return }

  const cx = props.width / 2
  const cy = (props.height - 40) / 2 // 减去工具栏高度
  const pos = new Map()

  // 初始：圆周均匀分布（中心节点放圆心）
  const nonCenter = ns.filter(n => !n.isCenter)
  const radius = Math.min(props.width, props.height) * 0.32
  nonCenter.forEach((n, i) => {
    const ang = (2 * Math.PI * i) / Math.max(1, nonCenter.length) - Math.PI / 2
    pos.set(n.id, { x: cx + radius * Math.cos(ang), y: cy + radius * Math.sin(ang), vx: 0, vy: 0 })
  })
  for (const n of ns) {
    if (n.isCenter && !pos.has(n.id)) pos.set(n.id, { x: cx, y: cy, vx: 0, vy: 0 })
  }

  // 力模拟参数
  const REPULSION = 8000      // 斥力强度
  const SPRING_K = 0.08       // 弹簧刚度
  const SPRING_L = 120        // 弹簧自然长度
  const CENTER_K = 0.012      // 中心引力
  const DAMPING = 0.85        // 阻尼
  const ITERATIONS = 120      // 迭代次数

  for (let iter = 0; iter < ITERATIONS; iter++) {
    // 重置速度
    for (const p of pos.values()) { p.vx = 0; p.vy = 0 }
    // 斥力（每对节点）
    const ids = Array.from(pos.keys())
    for (let i = 0; i < ids.length; i++) {
      for (let j = i + 1; j < ids.length; j++) {
        const a = pos.get(ids[i]), b = pos.get(ids[j])
        let dx = a.x - b.x, dy = a.y - b.y
        let d2 = dx * dx + dy * dy
        if (d2 < 1) d2 = 1
        const d = Math.sqrt(d2)
        const f = REPULSION / d2
        const fx = (f * dx) / d, fy = (f * dy) / d
        a.vx += fx; a.vy += fy
        b.vx -= fx; b.vy -= fy
      }
    }
    // 弹簧力（相连节点）
    for (const e of es) {
      const a = pos.get(e.source), b = pos.get(e.target)
      if (!a || !b) continue
      let dx = b.x - a.x, dy = b.y - a.y
      const d = Math.sqrt(dx * dx + dy * dy) || 1
      const f = SPRING_K * (d - SPRING_L)
      const fx = (f * dx) / d, fy = (f * dy) / d
      a.vx += fx; a.vy += fy
      b.vx -= fx; b.vy -= fy
    }
    // 中心引力 + 阻尼 + 积分
    for (const p of pos.values()) {
      p.vx += -CENTER_K * (p.x - cx)
      p.vy += -CENTER_K * (p.y - cy)
      p.vx *= DAMPING
      p.vy *= DAMPING
      p.x += p.vx
      p.y += p.vy
    }
  }
  // 清理速度字段
  for (const p of pos.values()) { delete p.vx; delete p.vy }
  positions.value = pos
}

// 网络拓扑布局：按 CI 类型分层，每层水平排列
// 层次顺序：cluster → machine → os → service → app → unknown → 其他
const LAYER_ORDER = ['cluster', 'machine', 'os', 'service', 'app', 'unknown']
function computeTopologyLayout() {
  const ns = nodes.value
  if (!ns.length) { positions.value = new Map(); return }

  // 按类型分组
  const layers = new Map()
  for (const n of ns) {
    const layer = LAYER_ORDER.includes(n.type) ? n.type : 'unknown'
    if (!layers.has(layer)) layers.set(layer, [])
    layers.get(layer).push(n)
  }
  // 按 LAYER_ORDER 排序层，未知类型放最后
  const orderedLayers = Array.from(layers.keys()).sort((a, b) => {
    const ia = LAYER_ORDER.indexOf(a), ib = LAYER_ORDER.indexOf(b)
    return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib)
  })

  const pos = new Map()
  const layerCount = orderedLayers.length
  const layerGap = (props.height - 60) / Math.max(1, layerCount)
  const startY = 50
  orderedLayers.forEach((layer, li) => {
    const items = layers.get(layer)
    const y = startY + li * layerGap + layerGap / 2
    const count = items.length
    const slotW = props.width / Math.max(1, count)
    items.forEach((n, i) => {
      const x = slotW * (i + 0.5)
      pos.set(n.id, { x, y })
    })
  })
  positions.value = pos
}

function recompute() {
  if (props.mode === 'topology') computeTopologyLayout()
  else computeForceLayout()
}

// 节点位置查询（拖拽时优先用 dragPositions）
const dragPositions = ref(new Map())
function nodePos(id) {
  return dragPositions.value.get(id) || positions.value.get(id) || { x: 0, y: 0 }
}

// === 交互状态 ===
const scale = ref(1)
const pan = ref({ x: 0, y: 0 })
const hoveredNode = ref('')
const hoveredEdge = ref(-1)
const selectedNode = ref('')
const focusedNode = ref(null)
const svgEl = ref(null)

// 节点拖拽
let draggingNode = null
function onNodeMouseDown(evt, n) {
  draggingNode = { id: n.id }
  const start = clientToSvg(evt)
  draggingNode.startX = start.x
  draggingNode.startY = start.y
  const cur = nodePos(n.id)
  draggingNode.origX = cur.x
  draggingNode.origY = cur.y
  window.addEventListener('mousemove', onNodeMouseMove)
  window.addEventListener('mouseup', onNodeMouseUp)
}
function onNodeMouseMove(evt) {
  if (!draggingNode) return
  const cur = clientToSvg(evt)
  const dx = cur.x - draggingNode.startX
  const dy = cur.y - draggingNode.startY
  const next = new Map(dragPositions.value)
  next.set(draggingNode.id, { x: draggingNode.origX + dx, y: draggingNode.origY + dy })
  dragPositions.value = next
}
function onNodeMouseUp() {
  draggingNode = null
  window.removeEventListener('mousemove', onNodeMouseMove)
  window.removeEventListener('mouseup', onNodeMouseUp)
}

// 将鼠标客户端坐标转换为 SVG 内部坐标（考虑缩放与平移）
function clientToSvg(evt) {
  const rect = svgEl.value.getBoundingClientRect()
  const x = (evt.clientX - rect.left - pan.value.x) / scale.value
  const y = (evt.clientY - rect.top - pan.value.y) / scale.value
  return { x, y }
}

// 画布平移（空白拖拽）
let panning = null
function onCanvasMouseDown(evt) {
  if (evt.target === svgEl.value || evt.target.classList.contains('rg-svg')) {
    panning = { startX: evt.clientX, startY: evt.clientY, origX: pan.value.x, origY: pan.value.y }
    window.addEventListener('mousemove', onPanMove)
    window.addEventListener('mouseup', onPanUp)
  }
}
function onPanMove(evt) {
  if (!panning) return
  pan.value = {
    x: panning.origX + (evt.clientX - panning.startX),
    y: panning.origY + (evt.clientY - panning.startY)
  }
}
function onPanUp() {
  panning = null
  window.removeEventListener('mousemove', onPanMove)
  window.removeEventListener('mouseup', onPanUp)
}

// 滚轮缩放
function onWheel(evt) {
  const factor = evt.deltaY < 0 ? 1.1 : 1 / 1.1
  zoomBy(factor)
}
function zoomBy(factor) {
  scale.value = Math.max(0.3, Math.min(3, scale.value * factor))
}
function resetView() {
  scale.value = 1
  pan.value = { x: 0, y: 0 }
  dragPositions.value = new Map()
  recompute()
}

// 节点点击 / hover
function onNodeClick(n) {
  if (!props.selectable) return
  selectedNode.value = n.id
  focusedNode.value = n
  emit('node-click', n)
}
function onNodeHover(n) {
  hoveredNode.value = n ? n.id : ''
  emit('node-hover', n)
}

// 节点半径：中心节点更大
function nodeRadius(n) {
  return n.isCenter ? 22 : 16
}

// 边标签位置：中点偏上
function edgeLabelPos(e) {
  const a = nodePos(e.source), b = nodePos(e.target)
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 - 6 }
}

// 工具栏模式按钮
const modes = computed(() => [
  { key: 'force', icon: '◈', label: t('cmdb.graph_mode_force') },
  { key: 'topology', icon: '☰', label: t('cmdb.graph_mode_topology') }
])

// graph 或 mode 变化时重算布局
watch(() => [props.graph, props.mode], () => {
  dragPositions.value = new Map()
  recompute()
}, { deep: true })

onMounted(() => {
  nextTick(() => recompute())
})
</script>

<style scoped>
.rg-wrap {
  position: relative; border: 1px solid var(--border);
  border-radius: var(--radius-sm); background: var(--surface);
  overflow: hidden; user-select: none;
}
.rg-toolbar {
  display: flex; align-items: center; gap: 4px;
  padding: 6px 10px; border-bottom: 1px solid var(--border);
  background: var(--surface-3);
}
.rg-tb-btn {
  width: 28px; height: 26px; border: 1px solid var(--border);
  border-radius: 5px; background: var(--surface-2); cursor: pointer;
  font-size: 14px; color: var(--text-2); transition: .12s;
  display: inline-flex; align-items: center; justify-content: center;
}
.rg-tb-btn:hover { background: var(--bg-soft); color: var(--text); }
.rg-tb-btn.active { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }
.rg-tb-sep { width: 1px; height: 18px; background: var(--border); margin: 0 4px; }
.rg-tb-info { font-size: 12px; color: var(--text-3); margin-left: auto; }
.rg-svg { display: block; cursor: grab; background: var(--surface); }
.rg-svg:active { cursor: grabbing; }

.rg-node-circle { transition: r .15s, stroke-width .15s; cursor: pointer; }
.rg-node.hovered .rg-node-circle { filter: brightness(1.15); }
.rg-node.selected .rg-node-circle { stroke: var(--accent) !important; stroke-width: 4 !important; }
.rg-node-label {
  text-anchor: middle; font-size: 12px; font-weight: 600;
  fill: var(--text); pointer-events: none;
}
.rg-node-type {
  text-anchor: middle; font-size: 10px; fill: var(--text-3);
  pointer-events: none;
}
.rg-edge-line { transition: stroke-width .12s; }
.rg-edge.hovered .rg-edge-line { stroke-width: 3 !important; }
.rg-edge-label {
  font-size: 10px; font-weight: 600; pointer-events: none;
  text-anchor: middle; paint-order: stroke;
  stroke: var(--surface); stroke-width: 3;
}

.rg-legend {
  position: absolute; left: 8px; bottom: 8px;
  display: flex; flex-direction: column; gap: 4px;
  padding: 6px 10px; background: var(--surface-2);
  border: 1px solid var(--border); border-radius: 6px;
  font-size: 11px; color: var(--text-2); max-width: 60%;
}
.rg-legend-section { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.rg-legend-title { font-weight: 600; color: var(--text-3); margin-right: 2px; }
.rg-legend-item { display: inline-flex; align-items: center; gap: 3px; }
.rg-legend-dot { width: 9px; height: 9px; border-radius: 50%; display: inline-block; }
.rg-legend-line { display: inline-block; }

.rg-detail {
  position: absolute; right: 8px; top: 42px;
  width: 220px; padding: 10px 12px;
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: 0 4px 14px rgba(0,0,0,.08);
  font-size: 12px; color: var(--text);
}
.rg-detail-head { display: flex; align-items: center; gap: 6px; margin-bottom: 8px; }
.rg-detail-dot { width: 10px; height: 10px; border-radius: 50%; }
.rg-detail-type { font-size: 11px; color: var(--text-3); margin-left: auto; }
.rg-detail-close {
  border: none; background: none; cursor: pointer;
  font-size: 16px; color: var(--text-3); line-height: 1; padding: 0 2px;
}
.rg-detail-close:hover { color: var(--fail); }
.rg-detail-body { display: flex; flex-direction: column; gap: 4px; }
.rg-detail-row { display: flex; gap: 6px; align-items: center; }
.rg-detail-row .muted { color: var(--text-3); min-width: 32px; }
.rg-detail-row code { font-size: 11px; }
.rg-detail-attrs { margin-top: 4px; }
.rg-detail-attr { font-size: 11px; margin-top: 2px; }
.rg-detail-attr code { font-size: 10.5px; color: var(--teal); }

.rg-empty {
  position: absolute; inset: 40px 0 0 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--text-3); font-size: 13px;
}
</style>