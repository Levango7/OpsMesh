// AI 聊天 store — 对接 /api/v1/ai/chat
import { defineStore } from 'pinia'
import { chat, getAIUsage } from '@/api/ai'
import { t } from '@/i18n'

export const useAIStore = defineStore('ai', {
  state: () => ({
    messages: [],
    loading: false,
    error: '',
    tokenUsage: null
  }),
  actions: {
    async chat(text, model, useRAG) {
      this.loading = true
      this.error = ''
      // 添加用户消息
      this.messages.push({ role: 'user', content: text })
      try {
        const resp = await chat({
          model,
          messages: this.messages.map(m => ({ role: m.role, content: m.content })),
          useRag: useRAG,
          maxTokens: 2048
        })
        const choice = resp?.choices?.[0]
        const content = choice?.message?.content || ''
        this.messages.push({
          role: 'assistant',
          content,
          usage: resp?.usage
        })
        // 刷新 token 用量
        try {
          this.tokenUsage = await getAIUsage()
        } catch { /* ignore */ }
      } catch (e) {
        this.error = e.j?.error || t('error.aiChatFailed')
        this.messages.push({ role: 'assistant', content: t('error.aiChatFailed') })
      } finally {
        this.loading = false
      }
    },
    clearMessages() {
      this.messages = []
    },
    async fetchUsage() {
      try {
        this.tokenUsage = await getAIUsage()
      } catch { /* ignore */ }
    }
  }
})
