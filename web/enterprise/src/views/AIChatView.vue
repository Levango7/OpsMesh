<template>
  <!-- 特殊页：全高对话布局——PageShell 只接管页头/错误横幅，对话区的滚动与高度样式保持原样 -->
  <PageShell :title="$t('aiChat.title')" :desc="$t('aiChat.desc')" testid="ai-chat" :error="store.error">
    <!-- 聊天区域 -->
    <div class="chat-container">
      <div class="chat-messages" ref="messagesRef">
        <div v-for="(msg, idx) in store.messages" :key="idx" :class="['chat-msg', msg.role]">
          <div class="msg-role">{{ msg.role === 'user' ? $t('aiChat.you') : $t('aiChat.ai') }}</div>
          <div class="msg-content">{{ msg.content }}</div>
          <div v-if="msg.usage" class="msg-usage">
            {{ $t('aiChat.token_usage') }}: {{ msg.usage.totalTokens }}
          </div>
        </div>
        <div v-if="store.loading" class="chat-msg assistant">
          <div class="msg-role">{{ $t('aiChat.ai') }}</div>
          <div class="msg-content typing">{{ $t('aiChat.thinking') }}</div>
        </div>
      </div>

      <!-- 输入区域 -->
      <div class="chat-input-bar">
        <textarea
          v-model="input"
          :placeholder="$t('aiChat.placeholder')"
          @keyup.enter.exact.prevent="send"
          rows="2"
          class="chat-input"
        ></textarea>
        <div class="chat-actions">
          <label class="chk"><input type="checkbox" v-model="useRAG" /> {{ $t('aiChat.use_rag') }}</label>
          <select v-model="model" class="model-select">
            <option value="gpt-4o-mini">GPT-4o Mini</option>
            <option value="gpt-4o">GPT-4o</option>
            <option value="claude-3-5-sonnet">Claude 3.5 Sonnet</option>
            <option value="qwen-turbo">Qwen Turbo</option>
            <option value="llama3.1">Llama 3.1 (Local)</option>
          </select>
          <Button variant="primary" size="sm" @click="send" :disabled="store.loading || !input.trim()">
            {{ $t('aiChat.send') }}
          </Button>
          <Button variant="ghost" size="sm" @click="store.clearMessages()">{{ $t('aiChat.clear') }}</Button>
        </div>
      </div>
    </div>

    <!-- Token 用量 -->
    <div class="card" v-if="store.tokenUsage">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('aiChat.token_usage') }}</h3>
      </div>
      <div class="usage-grid">
        <div class="usage-item">
          <span class="usage-label">{{ $t('aiChat.input_tokens') }}</span>
          <span class="usage-value">{{ store.tokenUsage.totalInputTokens }}</span>
        </div>
        <div class="usage-item">
          <span class="usage-label">{{ $t('aiChat.output_tokens') }}</span>
          <span class="usage-value">{{ store.tokenUsage.totalOutputTokens }}</span>
        </div>
        <div class="usage-item">
          <span class="usage-label">{{ $t('aiChat.total_tokens') }}</span>
          <span class="usage-value">{{ store.tokenUsage.totalTokens }}</span>
        </div>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { ref, nextTick } from 'vue'
import { useAIStore } from '@/stores/ai'
import { Button } from '@/components/ui'

const store = useAIStore()
const input = ref('')
const useRAG = ref(false)
const model = ref('gpt-4o-mini')
const messagesRef = ref(null)

async function send() {
  const text = input.value.trim()
  if (!text || store.loading) return
  input.value = ''
  try {
    await store.chat(text, model.value, useRAG.value)
    await nextTick()
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  } catch {
    // error already in store
  }
}
</script>

<style scoped>
.chat-container { display: flex; flex-direction: column; gap: 12px; margin-bottom: 16px; }
.chat-messages { max-height: 500px; overflow-y: auto; padding: 12px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-md, 8px); background: var(--color-surface-1, #ffffff); }
.chat-msg { margin-bottom: 12px; }
.chat-msg.user .msg-role { color: var(--color-primary, #3b82f6); }
.chat-msg.assistant .msg-role { color: var(--color-success, #059669); }
.msg-role { font-size: 11px; font-weight: 600; margin-bottom: 4px; text-transform: uppercase; }
.msg-content { font-size: 13px; line-height: 1.6; white-space: pre-wrap; word-break: break-word; }
.msg-usage { font-size: 10px; color: var(--color-ink-muted, #6b7280); margin-top: 4px; }
.typing { opacity: 0.6; font-style: italic; }
.chat-input-bar { display: flex; flex-direction: column; gap: 8px; }
.chat-input { width: 100%; padding: 8px 10px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 13px; resize: vertical; min-height: 60px; background: var(--color-surface-1, #ffffff); }
.chat-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.model-select { padding: 6px 8px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 12px; background: var(--color-surface-1, #ffffff); }
.chk { display: flex; align-items: center; gap: 4px; font-size: 12px; }
.usage-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.usage-item { display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); }
.usage-label { font-size: 11px; color: var(--color-ink-muted, #6b7280); }
.usage-value { font-size: 16px; font-weight: 600; }
</style>