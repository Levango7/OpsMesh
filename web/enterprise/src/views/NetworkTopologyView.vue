<template>
  <div>
    <h2 data-testid="network-topology-title">{{ $t('network.topology_title') }}</h2>
    <p class="muted">{{ $t('network.topology_subtitle') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="btnbar">
      <button class="primary" @click="refresh" data-testid="network-topology-refresh-btn">
        <Icon name="refresh" :size="14" /> {{ $t('network.refresh_topology') }}
      </button>
      <button class="outline" @click="loadCache" data-testid="network-topology-cache-btn">
        <Icon name="success" :size="14" /> {{ $t('network.load_cache') }}
      </button>
    </div>

    <div v-if="store.loading && !store.topology" class="muted">{{ $t('common.loading') }}</div>
    <div v-else-if="store.topology" class="card">
      <div class="kv-grid">
        <div class="kv"><span class="k">{{ $t('network.field_generatedAt') }}</span><span class="v">{{ store.topology.generatedAt || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('network.field_tenantID') }}</span><span class="v">{{ store.topology.tenantID || '—' }}</span></div>
        <div class="kv"><span class="k">{{ $t('network.field_nodes') }}</span><span class="v">{{ (store.topology.nodes || []).length }}</span></div>
        <div class="kv"><span class="k">{{ $t('network.field_edges') }}</span><span class="v">{{ (store.topology.edges || []).length }}</span></div>
      </div>

      <!-- SVG 拓扑图 -->
      <h3>{{ $t('network.topology_graph') }}</h3>
      <div class="topology-svg-wrap">
        <svg
          v-if="(store.topology.nodes || []).length"
          :width="svgWidth"
          :height="svgHeight"
          class="topology-svg"
        >
          <!-- 边 -->
          <g class="edges">
            <line
              v-for="(e, i) in edgeLayout"
              :key="'e' + i"
              :x1="e.x1" :y1="e.y1" :x2="e.x2" :y2="e.y2"
              class="edge"
            />
            <text
              v-for="(e, i) in edgeLayout"
              :key="'et' + i"
              :x="(e.x1 + e.x2) / 2"
              :y="(e.y1 + e.y2) / 2"
              class="edge-label"
            >{{ edgeLabel(e) }}</text>
          </g>
          <!-- 节点 -->
          <g class="nodes">
            <g v-for="(n, i) in nodeLayout" :key="'n' + i" :transform="`translate(${n.x}, ${n.y})`">
              <circle
                :r="nodeRadius"
                :class="['node', n.status === 'online' ? 'online' : 'offline']"
              />
              <text class="node-label" :y="nodeRadius + 14">{{ n.hostname || n.id }}</text>
              <text class="node-ip" :y="nodeRadius + 28">{{ n.ip || '' }}</text>
            </g>
          </g>
        </svg>
        <div v-else class="muted">{{ $t('network.empty_topology') }}</div>
      </div>
    </div>

    <ConfirmModal
      v-model="errorConfirm.show"
      data-testid="network-topology-error-modal"
      :title="$t('common.error')"
      :message="errorConfirm.message"
      info
    />
  </div>
</template>

<script setup>
// 网络拓扑页 — 拓扑图展示（nodes/edges SVG 渲染）+ 刷新按钮 + 缓存拓扑
import { reactive, computed, onMounted } from 'vue'
import { useNetworkStore } from '@/stores/network'
import { t } from '@/i18n'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'

const store = useNetworkStore()
const errorConfirm = reactive({ show: false, message: '' })

// SVG 画布参数
const svgWidth = 800
const svgHeight = 500
const nodeRadius = 18
const padding = 60

// 节点布局：圆形分布
const nodeLayout = computed(() => {
  const nodes = (store.topology && store.topology.nodes) || []
  if (!nodes.length) return []
  const cx = svgWidth / 2
  const cy = svgHeight / 2
  const radius = Math.min(svgWidth, svgHeight) / 2 - padding
  return nodes.map((n, i) => {
    if (nodes.length === 1) return { ...n, x: cx, y: cy }
    const angle = (2 * Math.PI * i) / nodes.length - Math.PI / 2
    return {
      ...n,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle)
    }
  })
})

// 节点 id → 坐标映射
const nodePosMap = computed(() => {
  const m = new Map()
  for (const n of nodeLayout.value) m.set(n.id, n)
  return m
})

// 边布局：根据 source/target 节点坐标连线
const edgeLayout = computed(() => {
  const edges = (store.topology && store.topology.edges) || []
  const layout = []
  for (const e of edges) {
    const s = nodePosMap.value.get(e.source)
    const tg = nodePosMap.value.get(e.target)
    if (s && tg) {
      layout.push({ ...e, x1: s.x, y1: s.y, x2: tg.x, y2: tg.y })
    }
  }
  return layout
})

function edgeLabel(e) {
  const parts = []
  if (e.latencyMs != null) parts.push(`${e.latencyMs}ms`)
  if (e.loss != null) parts.push(`loss:${e.loss}`)
  return parts.join(' · ')
}

async function refresh() {
  try { await store.fetchTopology({ refresh: 'true' }) }
  catch (e) {
    errorConfirm.message = e.j?.error || t('error.networkTopologyFailed')
    errorConfirm.show = true
  }
}

async function loadCache() {
  try { await store.fetchCachedTopology() }
  catch (e) {
    errorConfirm.message = e.j?.error || t('error.networkTopologyFailed')
    errorConfirm.show = true
  }
}

onMounted(() => {
  store.fetchTopology().catch((e) => {
    errorConfirm.message = e.j?.error || t('error.networkTopologyFailed')
    errorConfirm.show = true
  })
})
</script>

<style scoped>
.card {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 16px; margin-top: 14px;
  box-shadow: var(--shadow);
}
.card h3 { margin: 16px 0 8px; font-size: 13px; }
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }

.kv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 10px; }
.kv { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--radius-sm); }
.kv .k { font-size: 11.5px; color: var(--text-3); }
.kv .v { font-size: 13px; color: var(--text); word-break: break-all; }

.topology-svg-wrap {
  overflow: auto; border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--surface-2); padding: 8px;
}
.topology-svg { display: block; max-width: 100%; }

.node { stroke-width: 2; }
.node.online { fill: var(--accent-soft, #e0f2fe); stroke: var(--teal, #14b8a6); }
.node.offline { fill: var(--surface-3); stroke: var(--rose, #f43f5e); }
.node-label { font-size: 11px; fill: var(--text); text-anchor: middle; }
.node-ip { font-size: 10px; fill: var(--text-3); text-anchor: middle; }

.edge { stroke: var(--border); stroke-width: 1.5; }
.edge-label { font-size: 9.5px; fill: var(--text-3); text-anchor: middle; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
</style>