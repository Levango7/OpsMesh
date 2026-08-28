<template>
  <div>
    <h2 data-testid="bot-title">{{ $t('bot.title') }}</h2>
    <p class="muted">{{ $t('bot.desc') }}</p>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <div class="row">
      <!-- 左：命令面板 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('bot.commandPanel') }}</h3>
          <div class="flowbar">
            <div class="field">
              <label>{{ $t('bot.platform') }}</label>
              <select v-model="store.selectedPlatform" @change="onPlatformChange" data-testid="bot-platform-select">
                <option value="">— {{ $t('bot.allPlatforms') }} —</option>
                <option v-for="p in store.platforms" :key="p.id" :value="p.id" :disabled="!p.enabled">
                  {{ p.name }}{{ p.enabled ? '' : ' (' + $t('bot.disabled') + ')' }}
                </option>
              </select>
            </div>
          </div>

          <!-- 快捷命令 -->
          <div v-if="store.quickCommands.length" class="quick-commands">
            <label class="quick-label">{{ $t('bot.quickCommands') }}</label>
            <div class="quick-btns">
              <button
                v-for="qc in store.quickCommands"
                :key="qc.command"
                class="xs outline quick-btn"
                :data-testid="'bot-quick-' + qc.label"
                @click="runQuickCommand(qc.command)"
              >{{ qc.label }}</button>
            </div>
          </div>

          <!-- 命令输入 -->
          <div class="cmd-input-row">
            <input
              v-model="commandInput"
              class="cmd-input"
              :placeholder="$t('bot.commandPlaceholder')"
              data-testid="bot-command-input"
              @keyup.enter="runCommand"
            />
            <button class="primary" @click="runCommand" :disabled="store.executing || !commandInput.trim()" data-testid="bot-run-btn">
              {{ store.executing ? $t('bot.executing') : $t('bot.run') }}
            </button>
          </div>

          <!-- 响应显示 -->
          <div v-if="lastResponse" class="response-area" data-testid="bot-response">
            <div class="response-header">
              <StatusBadge :status="lastResponse.status === 'success' ? 'success' : 'failed'" :text="lastResponse.status" />
              <span class="response-time">{{ lastResponse.executedAt || '' }}</span>
              <button class="xs outline" @click="copyResponse">{{ $t('bot.copy') }}</button>
            </div>
            <pre class="code-block response-content">{{ formatResponse(lastResponse.response) }}</pre>
          </div>
        </div>
      </div>

      <!-- 右：命令历史 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0">{{ $t('bot.history') }}</h3>
            <button class="xs outline" @click="store.fetchHistory(store.selectedPlatform)">↻ {{ $t('common.refresh') }}</button>
          </div>
          <div v-if="store.loading && !store.history.length" class="muted">{{ $t('common.loading') }}</div>
          <div v-else-if="!store.history.length" class="muted">{{ $t('bot.noHistory') }}</div>
          <div v-else class="history-list">
            <div
              v-for="item in store.history"
              :key="item.id"
              class="history-item"
              :data-testid="'bot-history-' + item.id"
              @click="lastResponse = item"
            >
              <div class="history-meta">
                <code class="history-cmd">{{ item.command }}</code>
                <StatusBadge :status="item.status === 'success' ? 'success' : 'failed'" :text="item.status" />
              </div>
              <div class="history-platform">{{ item.platform || '-' }}</div>
              <div class="history-time">{{ item.executedAt || '' }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useBotStore } from '@/stores/bot'
import { t } from '@/i18n'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'

const store = useBotStore()
const commandInput = ref('')
const lastResponse = ref(null)

function onPlatformChange() {
  store.selectPlatform(store.selectedPlatform)
}

function runQuickCommand(cmd) {
  commandInput.value = cmd
  runCommand()
}

async function runCommand() {
  const cmd = commandInput.value.trim()
  if (!cmd || store.executing) return
  const result = await store.runCommand(cmd, store.selectedPlatform)
  if (result) {
    lastResponse.value = result
    commandInput.value = ''
  }
}

function formatResponse(resp) {
  if (!resp) return '—'
  if (typeof resp === 'object') return JSON.stringify(resp, null, 2)
  return String(resp)
}

function copyResponse() {
  if (!lastResponse.value) return
  const text = formatResponse(lastResponse.value.response)
  navigator.clipboard.writeText(text).catch(() => {})
}

onMounted(() => {
  store.fetchPlatforms()
  store.fetchQuickCommands()
  store.fetchHistory()
})
</script>

<style scoped>
.row .col:nth-child(1) { flex: 55; }
.row .col:nth-child(2) { flex: 45; }

.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.flowbar .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 0; min-width: 200px; }
.flowbar .field label { margin: 0; }

.quick-commands { margin-bottom: 14px; }
.quick-label { font-size: 12px; color: var(--text-2); margin-bottom: 6px; display: block; }
.quick-btns { display: flex; flex-wrap: wrap; gap: 6px; }
.quick-btn { font-size: 12px; }

.cmd-input-row { display: flex; gap: 8px; margin-bottom: 14px; }
.cmd-input { flex: 1; font-family: var(--font-mono); }

.response-area { border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
.response-header {
  display: flex; align-items: center; gap: 10px; padding: 8px 12px;
  background: var(--surface-3); border-bottom: 1px solid var(--border);
}
.response-time { font-size: 11.5px; color: var(--text-3); flex: 1; }

.code-block {
  background: var(--surface); color: var(--text); padding: 12px;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono); margin: 0; max-height: 320px; overflow: auto;
}

.history-list { max-height: 480px; overflow: auto; }
.history-item {
  padding: 10px 12px; border-bottom: 1px solid var(--border); cursor: pointer; transition: .12s;
}
.history-item:last-child { border-bottom: none; }
.history-item:hover { background: var(--bg-soft); }
.history-meta { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.history-cmd { font-size: 12px; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.history-platform { font-size: 11.5px; color: var(--text-3); }
.history-time { font-size: 11.5px; color: var(--text-3); }
</style>
