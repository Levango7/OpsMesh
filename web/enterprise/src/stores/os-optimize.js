// OS 优化 store — 模板列表 + 详情 + 执行
import { defineStore } from 'pinia'
import { getOSTemplates, getOSTemplate, executeOSTemplate } from '@/api/os-optimize'
import { getDevices } from '@/api/device'

export const useOSOptimizeStore = defineStore('os-optimize', {
  state: () => ({
    templates: [],         // OSTemplate[]
    current: null,         // 当前查看的模板详情
    devices: [],           // 可选目标设备列表（执行对话框用）
    category: '',          // 当前分类筛选
    loading: false,
    error: ''
  }),
  actions: {
    async fetchTemplates() {
      this.loading = true; this.error = ''
      try {
        this.templates = await getOSTemplates(this.category) || []
      } catch (e) {
        this.error = e.j?.error || 'OS 优化模板列表拉取失败'
      } finally {
        this.loading = false
      }
    },
    async fetchDetail(id) {
      try {
        this.current = await getOSTemplate(id)
      } catch (e) {
        this.error = e.j?.error || 'OS 优化模板详情拉取失败'
      }
    },
    async fetchDevices() {
      try {
        const devs = await getDevices()
        // 兼容数组或按网段分组的对象
        if (Array.isArray(devs)) {
          this.devices = devs
        } else if (devs && typeof devs === 'object') {
          this.devices = Object.values(devs).flat()
        } else {
          this.devices = []
        }
      } catch {
        this.devices = []
      }
    },
    async execute(id, agentID, params) {
      return await executeOSTemplate(id, agentID, params)
    },
    setCategory(cat) {
      this.category = cat || ''
      this.fetchTemplates()
    },
    clearCurrent() { this.current = null }
  }
})