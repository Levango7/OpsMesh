<template>
  <div>
    <h2 data-testid="alerts-title">{{ $t('alerts.title') }}</h2>
    <p class="muted">{{ $t('alerts.subtitle') }}</p>

    <div class="stats">
      <div class="stat rose">
        <div class="stat-v">{{ store.critical.length }}</div>
        <div class="stat-l">{{ $t('alerts.stat_critical') }}</div>
      </div>
      <div class="stat amber">
        <div class="stat-v">{{ store.warning.length }}</div>
        <div class="stat-l">{{ $t('alerts.stat_warning') }}</div>
      </div>
      <div class="stat indigo">
        <div class="stat-v">{{ store.list.length }}</div>
        <div class="stat-l">{{ $t('alerts.stat_active') }}</div>
      </div>
      <div class="stat teal">
        <div class="stat-v">{{ ackedCount }}</div>
        <div class="stat-l">{{ $t('alerts.stat_handled') }}</div>
      </div>
    </div>

    <div class="flowbar">
      <button @click="store.fetchAlerts()" data-testid="alerts-refresh-btn">↻ {{ $t('common.refresh') }}</button>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>
    <div v-else-if="store.loading && !store.list.length" class="muted" data-testid="alerts-loading">{{ $t('common.loading') }}</div>
    <div v-else-if="!store.list.length" class="muted" data-testid="alerts-empty"><Icon name="success" :size="14" /> {{ $t('alerts.empty') }}</div>

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
      {{ $t('alerts.device_label') }} {{ a.deviceID }} ｜ {{ $t('alerts.agent_label') }} {{ a.agentID }}
      <small v-if="a.comment" class="muted"><br />{{ $t('alerts.comment_label') }}{{ a.comment }}</small>
      <br />{{ a.message }}
      <br /><small class="muted">{{ fmtTime(a.createdAt) }}</small>
      <div class="alert-actions">
        <template v-if="(a.status || 'firing') === 'firing'">
          <button class="xs" @click="onAck(a.alertID)" data-testid="alert-ack-btn">{{ $t('alerts.ack') }}</button>
          <button class="xs outline" @click="onSilence(a.alertID)" data-testid="alert-silence-btn">{{ $t('alerts.silence') }}</button>
        </template>
        <span v-else class="muted" style="font-size:12px">
          {{ $t('alerts.handler') }}{{ a.acknowledgedBy || '—' }}<template v-if="a.status === 'silenced' && a.silencedUntil">{{ $t('alerts.silenced_until') }}{{ a.silencedUntil }}</template>
        </span>
      </div>
    </div>
  </div>

  <!-- 静默时长输入（替代 prompt） -->
  <PromptModal
    v-model="durationModal.show"
    test-id="silence-duration-modal"
    :title="$t('alerts.silence_duration_title')"
    :message="$t('alerts.silence_duration_prompt')"
    :default-value="'1440'"
    placeholder="1440"
    @confirm="onDurationConfirm"
  />
  <!-- 静默备注输入（替代 prompt） -->
  <PromptModal
    v-model="commentModal.show"
    test-id="silence-comment-modal"
    :title="$t('alerts.silence_comment_title')"
    :message="$t('alerts.silence_comment_prompt')"
    @confirm="onCommentConfirm"
  />
  <!-- 错误提示（替代 alert） -->
  <ConfirmModal
    v-model="confirmState.show"
    :title="confirmState.title"
    :message="confirmState.message"
    info
  />
</template>

<script setup>
import { computed, reactive, onMounted, defineAsyncComponent } from 'vue'
import { useAlertStore } from '@/stores/alert'
import { t } from '@/i18n'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { fmtTime } from '@/composables/useFormatTime'
// PromptModal 仅少数页面使用，改为异步加载减小首屏 components chunk
const PromptModal = defineAsyncComponent(() => import('@/components/PromptModal.vue'))

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
  return s === 'acknowledged' ? t('alerts.status_acknowledged') : s === 'silenced' ? t('alerts.status_silenced') : t('alerts.status_firing')
}

// 错误提示弹窗（替代 alert）
const confirmState = reactive({ show: false, title: '', message: '' })
function showConfirm(title, message) {
  confirmState.title = title
  confirmState.message = message
  confirmState.show = true
}

// 静默流程：先输入时长，再输入备注，最后调用 API
const durationModal = reactive({ show: false, id: null })
const commentModal = reactive({ show: false, id: null, duration: 1440 })

async function onAck(id) {
  try { await store.ack(id) }
  catch (e) { showConfirm(t('common.error'), t('alerts.ack_failed') + (e.j?.error || e.s)) }
}

function onSilence(id) {
  durationModal.id = id
  durationModal.show = true
}

function onDurationConfirm(value) {
  let minutes = parseInt(value, 10)
  if (isNaN(minutes) || minutes <= 0) minutes = 1440
  commentModal.id = durationModal.id
  commentModal.duration = minutes
  commentModal.show = true
}

async function onCommentConfirm(value) {
  const comment = value || ''
  const id = commentModal.id
  const duration = commentModal.duration
  try { await store.silence(id, { durationMinutes: duration, comment }) }
  catch (e) { showConfirm(t('common.error'), t('alerts.silence_failed') + (e.j?.error || e.s)) }
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
