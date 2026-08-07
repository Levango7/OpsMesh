// K8s 管理 store — 集群 CRUD + 资源 list/scale/restart/logs/delete
import { defineStore } from 'pinia'
import {
  getK8sClusters,
  createK8sCluster,
  deleteK8sCluster,
  testK8sCluster,
  getK8sNamespaces,
  getK8sPods,
  getK8sPodLogs,
  deleteK8sPod,
  getK8sDeployments,
  scaleK8sDeployment,
  restartK8sDeployment,
  getK8sServices,
  getK8sConfigMaps,
  getK8sSecrets,
  getK8sNodes
} from '@/api/k8s'

export const useK8sStore = defineStore('k8s', {
  state: () => ({
    clusters: [],          // Cluster[]
    currentClusterID: '',  // 当前选中的集群 ID（资源管理用）
    namespaces: [],        // 当前集群的 namespace 列表
    resources: [],         // 当前资源列表（pods/deployments/...）
    resourceType: 'pods',  // 当前资源类型
    namespace: '',         // 当前命名空间过滤
    loading: false,
    resourcesLoading: false,
    error: ''
  }),
  getters: {
    currentCluster: (s) => s.clusters.find((c) => c.id === s.currentClusterID) || null
  },
  actions: {
    async fetchClusters() {
      this.loading = true; this.error = ''
      try {
        const data = await getK8sClusters()
        // 兼容 {clusters: []} 或 [] 两种返回形态
        if (Array.isArray(data)) {
          this.clusters = data
        } else if (data && Array.isArray(data.clusters)) {
          this.clusters = data.clusters
        } else {
          this.clusters = []
        }
      } catch (e) {
        this.error = e.j?.error || 'K8s 集群列表拉取失败'
      } finally {
        this.loading = false
      }
    },
    async createCluster(name, server, kubeconfig) {
      return await createK8sCluster(name, server, kubeconfig)
    },
    async removeCluster(id) {
      return await deleteK8sCluster(id)
    },
    async testCluster(id) {
      return await testK8sCluster(id)
    },
    async fetchNamespaces(clusterID) {
      try {
        const data = await getK8sNamespaces(clusterID)
        this.namespaces = (data && data.namespaces) || data || []
      } catch (e) {
        this.namespaces = []
      }
    },
    // 加载指定集群的指定资源类型
    async fetchResources() {
      if (!this.currentClusterID) {
        this.resources = []
        return
      }
      this.resourcesLoading = true; this.error = ''
      const cid = this.currentClusterID
      const ns = this.namespace
      try {
        let data
        switch (this.resourceType) {
          case 'pods':
            data = await getK8sPods(cid, ns); this.resources = (data && data.pods) || data || []; break
          case 'deployments':
            data = await getK8sDeployments(cid, ns); this.resources = (data && data.deployments) || data || []; break
          case 'services':
            data = await getK8sServices(cid, ns); this.resources = (data && data.services) || data || []; break
          case 'configmaps':
            data = await getK8sConfigMaps(cid, ns); this.resources = (data && data.configmaps) || data || []; break
          case 'secrets':
            data = await getK8sSecrets(cid, ns); this.resources = (data && data.secrets) || data || []; break
          case 'nodes':
            data = await getK8sNodes(cid); this.resources = (data && data.nodes) || data || []; break
          default:
            this.resources = []
        }
      } catch (e) {
        this.error = e.j?.error || 'K8s 资源列表拉取失败'
        this.resources = []
      } finally {
        this.resourcesLoading = false
      }
    },
    async fetchPodLogs(clusterID, ns, name, tailLines, container) {
      return await getK8sPodLogs(clusterID, ns, name, tailLines, container)
    },
    async removePod(clusterID, ns, name) {
      return await deleteK8sPod(clusterID, ns, name)
    },
    async scaleDeployment(clusterID, ns, name, replicas) {
      return await scaleK8sDeployment(clusterID, ns, name, replicas)
    },
    async restartDeployment(clusterID, ns, name) {
      return await restartK8sDeployment(clusterID, ns, name)
    },
    // 切换当前集群：同步拉取 namespace + 默认资源
    async selectCluster(id) {
      this.currentClusterID = id || ''
      this.resourceType = 'pods'
      this.namespace = ''
      this.resources = []
      if (id) {
        await this.fetchNamespaces(id)
        await this.fetchResources()
      }
    },
    setResourceType(type) {
      this.resourceType = type || 'pods'
      this.fetchResources()
    },
    setNamespace(ns) {
      this.namespace = ns || ''
      this.fetchResources()
    }
  }
})