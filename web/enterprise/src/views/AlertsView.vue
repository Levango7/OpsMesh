<template>
  <div>
    <h2>监控告警</h2>
    <p class="muted">实时告警列表，支持确认（ack）与静默（silence）操作。</p>

    <div class="stats">
      <div class="stat rose">
        <div class="stat-v">{{ store.critical.length }}</div>
        <div class="stat-l">严重 Critical</div>
      </div>
      <div class="stat amber">
        <div class="stat-v">{{ store.warning.length }}</div>
        <div class="stat-l">警告 Warning</div>
      </div>
      <div class="stat indigo">
        <div class="stat-v">{{ store.list.length }}</div>
        <div class="stat-l">活跃总数</div>
      </div>
      <div class="stat teal">
        <div class="stat-v">{{ ackedCount }}</div>
        <div class="stat-l">已处理</div>
      </div>
    </div>

    <div class="flowbar">
      <button @click="store.fetchAlerts()">↻ 刷新</button>
    </div>

    <div v-if="store.error" class="poll-err">⚠️ {{ store.error }}</div>
    <div v-else-if="!store.list.length" class="muted">暂无告警，一切正常 ✅</div>

    <div
      v-for="a in store.list"
      :key="a.alertID"
      class="alert"
      :class="{ warn: a.severity !== 'critical' }"
    >
      <div class="alert-head">
        <b>[{{ a.severity }}]</b>
        <StatusBadge :status="alertStatus(a)" :text="alertStatusText(a)" />
      </div>
      设备 {{ a.deviceID }} ｜ Agent {{ a.agentID }}
      <small v-if="a.comment" class="muted"><br />备注：{{ a.comment }}</small>
      <br />{{ a.message }}
      <br /><small class="muted">{{ fmtTime(a.createdAt) }}</small>
      <div class="alert-actions">
        <template v-if="(a.status || 'firing') === 'firing'">
          <button class="xs" @click="onAck(a.alertID)">✓ 确认</button>
          <button class="xs outline" @click="onSilence(a.alertID)">🔕 静默</button>
        </template>
        <span v-else class="muted" style="font-size:12px">
          处理人：{{ a.acknowledgedBy || '—' }}<template v-if="a.status === 'silenced' && a.silencedUntil"> · 至 {{ a.silencedUntil }}</template>
        </span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { useAlertStore } from '@/stores/alert'
import StatusBadge from '@/components/StatusBadge.vue'

const store = useAlertStore()

const ackedCount = computed(() =>
  store.list.filter((a) => (a.status || 'firing') !== 'firing').length
)

function alertStatus(a) {
  const s = a.status || 'firing'
  return s === 'acknowledged' ? 'acknowledged' : s === 'silenced' ? 'silenced' : 'firing'
}
function alertStatusText(a) {
  const s = a.status || 'firing'
  return s === 'acknowledged' ? '已确认' : s === 'silenced' ? '已静默' : '待处理'
}
function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}
async function onAck(id) {
  try { await store.ack(id) } catch (e) { alert('确认失败：' + (e.j?.error || e.s)) }
}
async function onSilence(id) {
  const dur = prompt('静默时长（分钟，留空=24 小时）：', '1440')
  if (dur === null) return
  let minutes = parseInt(dur, 10); if (isNaN(minutes) || minutes <= 0) minutes = 1440
  const comment = prompt('处理备注（可选）：', '') || ''
  try { await store.silence(id, { durationMinutes: minutes, comment }) }
  catch (e) { alert('静默失败：' + (e.j?.error || e.s)) }
}

onMounted(() => { if (!store.list.length) store.fetchAlerts() })
</script>

<style scoped>
.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; margin-bottom: 18px; }
.stat {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 14px 16px;
  border-top: 3px solid var(--accent); box-shadow: var(--shadow);
}
.stat.rose { border-top-color: var(--rose); }
.stat.amber { border-top-color: var(--amber); }
.stat.indigo { border-top-color: var(--indigo); }
.stat.teal { border-top-color: var(--teal); }
.stat-v { font-size: 25px; font-weight: 700; color: var(--text); line-height: 1.1; font-variant-numeric: tabular-nums; }
.stat-l { font-size: 12px; color: var(--text-3); margin-top: 4px; }

.flowbar { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }

.alert {
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  padding: 10px 13px; border-radius: var(--radius-sm);
  margin-top: 8px; font-size: 13px;
}
.alert.warn { background: var(--warn-soft); border-color: var(--warn-bg); }
.alert-head { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
.alert-actions { margin-top: 8px; display: flex; gap: 8px; align-items: center; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
@media (max-width: 768px) { .stats { grid-template-columns: repeat(2, 1fr); } }
</style>