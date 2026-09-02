// Bot / ChatOps store — 命令历史、平台、快捷命令
import { defineStore } from 'pinia'
import {
  executeCommand,
  getCommandHistory,
  getBotPlatforms,
  getQuickCommands
} from '@/api/bot'
import { t } from '@/i18n'

export const useBotStore = defineStore('bot', {
  state: () => ({
    history: [],
    platforms: [],
    quickCommands: [],
    selectedPlatform: '',
    executing: false,
    loading: false,
    error: ''
  }),
  getters: {
    enabledPlatforms: (s) => s.platforms.filter((p) => p.enabled),
    recentHistory: (s) => s.history.slice(0, 20)
  },
  actions: {
    async fetchPlatforms() {
      try {
        const data = await getBotPlatforms()
        this.platforms = (data && data.platforms) || data || []
      } catch {
        this.platforms = []
      }
    },
    async fetchQuickCommands() {
      try {
        const data = await getQuickCommands()
        this.quickCommands = (data && data.commands) || data || []
      } catch {
        this.quickCommands = []
      }
    },
    async fetchHistory(platform, limit) {
      this.loading = true; this.error = ''
      try {
        const data = await getCommandHistory(platform, limit)
        this.history = (data && data.history) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.botHistoryFailed')
      } finally {
        this.loading = false
      }
    },
    async runCommand(command, platform) {
      this.executing = true; this.error = ''
      try {
        const r = await executeCommand(command, platform)
        if (r.s >= 200 && r.s < 300 && r.j) {
          this.history.unshift(r.j)
          return r.j
        }
        return null
      } catch (e) {
        this.error = e.j?.error || t('error.botCommandFailed')
        return null
      } finally {
        this.executing = false
      }
    },
    selectPlatform(platform) {
      this.selectedPlatform = platform || ''
      this.fetchHistory(platform)
    }
  }
})
