// 云资源列表 store（2026-11-02 补「后端有、前端无界面」缺口）
// 后端契约：internal/controlplane/server_lifecycle.go:431-434
//   GET /api/v1/cloud-resources → {resources: [CloudResource], total, offset, limit}
//   （POST/DELETE/PUT 的 wrapper 尚缺，见 src/api/cloud-account.js 备注与
//     src/api/__tests__/api-usage.test.js 的 ALLOW 记账）
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { t } from '@/i18n'
import { listCloudResources } from '@/api/cloud-account'

export const useCloudResourcesStore = defineStore('cloud-resources', () => {
  const resources = ref([])
  const loading = ref(false)
  const error = ref('')
  const filters = ref({ accountID: '', resourceType: '' })

  async function fetchResources(params) {
    loading.value = true
    error.value = ''
    try {
      const merged = { ...filters.value, ...(params || {}) }
      const resp = await listCloudResources(merged)
      resources.value = resp.resources || []
    } catch (e) {
      error.value = e.j?.error || t('cloudResources.listFailed')
    } finally {
      loading.value = false
    }
  }

  function setFilter(key, value) {
    filters.value[key] = value
  }

  return { resources, loading, error, filters, fetchResources, setFilter }
})
