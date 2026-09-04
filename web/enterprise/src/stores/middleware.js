// 中间件部署 store — 模板列表 + 详情 + 部署 + 实例列表 + 卸载
import { defineStore } from 'pinia'
import {
  getMiddlewareTemplates,
  getMiddlewareTemplate,
  deployMiddleware,
  getMiddlewareInstances,
  uninstallMiddleware
} from '@/api/middleware'
import { getDevices } from '@/api/device'
import { t } from '@/i18n'

export const useMiddlewareStore = defineStore('middleware', {
  state: () => ({
    templates: [],         // MiddlewareTemplate[]
    instances: [],         // MiddlewareInstance[]
    current: null,         // 当前查看的模板详情
    devices: [],           // 可选目标设备列表（部署对话框用）
    category: '',          // 当前分类筛选
    loading: false,
    instancesLoading: false,
    error: ''
  }),
  actions: {
    async fetchTemplates() {
      this.loading = true; this.error = ''
      try {
        this.templates = await getMiddlewareTemplates(this.category) || []
      } catch (e) {
        this.error = e.j?.error || t('error.mwTemplatesFailed')
      } finally {
        this.loading = false
      }
    },
    async fetchInstances() {
      this.instancesLoading = true
      try {
        this.instances = await getMiddlewareInstances() || []
      } catch {
        // 实例列表失败不覆盖主错误
      } finally {
        this.instancesLoading = false
      }
    },
    async fetchDetail(id) {
      try {
        this.current = await getMiddlewareTemplate(id)
      } catch (e) {
        this.error = e.j?.error || t('error.mwDetailFailed')
      }
    },
    async fetchDevices() {
      try {
        const devs = await getDevices()
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
    async deploy(id, agentID, deployType, params) {
      return await deployMiddleware(id, agentID, deployType, params)
    },
    async uninstall(instanceID, agentID, deployType) {
      return await uninstallMiddleware(instanceID, agentID, deployType)
    },
    setCategory(cat) {
      this.category = cat || ''
      this.fetchTemplates()
    },
    clearCurrent() { this.current = null }
  }
})