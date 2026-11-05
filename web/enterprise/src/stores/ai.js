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
        // TD-97 ① 前端闭环：预算超限是后端明确拒绝形态（gateway.go Complete 入口），
        // 识别后给可操作的本地化提示，而不是把英文内部错误甩给用户。
        const raw = e.j?.error || ''
        if (raw.startsWith('budget exceeded for tenant')) {
          this.error = t('aiChat.budget_exceeded')
          this.messages.push({ role: 'assistant', content: t('aiChat.budget_exceeded') })
        } else {
          this.error = raw || t('error.aiChatFailed')
          this.messages.push({ role: 'assistant', content: t('error.aiChatFailed') })
        }
      } finally {
        this.loading = false
      }
    },
    clearMessages() {
      this.messages = []
    },
    async fetchUsage(scope) {
      try {
        this.tokenUsage = await getAIUsage(scope)
      } catch {
        // scope=all 被拒（非平台租户/无权限）时回退本租户口径
        if (scope === 'all') await this.fetchUsage()
      }
    }
  }
})
