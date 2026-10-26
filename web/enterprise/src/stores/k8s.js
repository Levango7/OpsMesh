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
  getK8sNodes,
  getK8sStatefulSets,
  scaleK8sStatefulSet,
  restartK8sStatefulSet,
  getK8sDaemonSets,
  restartK8sDaemonSet,
  getK8sReplicaSets,
  getK8sJobs,
  deleteK8sJob,
  getK8sCronJobs,
  deleteK8sCronJob,
  getK8sIngresses,
  getK8sPVCs,
  getK8sPVs,
  getK8sHPAs,
  getK8sRoles,
  getK8sRoleBindings,
  getK8sClusterRoles,
  getK8sClusterRoleBindings,
  getK8sCRDs,
  getK8sNetworkPolicies,
  getK8sIngressClasses,
  getK8sStorageClasses,
  getK8sPDBs,
  getK8sServiceAccounts,
  getK8sEndpointSlices,
  getK8sResourceQuotas,
  getK8sLimitRanges,
  getK8sPriorityClasses,
  getK8sRuntimeClasses,
  getK8sLeases,
  getK8sEvents,
  getK8sMutatingWebhooks,
  getK8sValidatingWebhooks,
  getK8sCSRs
} from '@/api/k8s'
import { t } from '@/i18n'

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
    error: '',
    _fetchSeq: 0
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
        this.error = e.j?.error || t('error.k8sClustersFailed')
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
      } catch {
        this.namespaces = []
      }
    },
    // 加载指定集群的指定资源类型
    async fetchResources() {
      if (!this.currentClusterID) {
        this.resources = []
        return
      }
      const seq = ++this._fetchSeq
      this.resourcesLoading = true; this.error = ''
      const cid = this.currentClusterID
      const ns = this.namespace
      try {
        let data
        switch (this.resourceType) {
         case 'pods':
            data = await getK8sPods(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.pods) || data || []; break
          case 'deployments':
            data = await getK8sDeployments(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.deployments) || data || []; break
          case 'statefulsets':
            data = await getK8sStatefulSets(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.statefulsets) || data || []; break
          case 'daemonsets':
            data = await getK8sDaemonSets(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.daemonsets) || data || []; break
          case 'replicasets':
            data = await getK8sReplicaSets(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.replicasets) || data || []; break
          case 'jobs':
            data = await getK8sJobs(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.jobs) || data || []; break
          case 'cronjobs':
            data = await getK8sCronJobs(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.cronjobs) || data || []; break
          case 'services':
            data = await getK8sServices(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.services) || data || []; break
          case 'configmaps':
            data = await getK8sConfigMaps(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.configmaps) || data || []; break
          case 'secrets':
            data = await getK8sSecrets(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.secrets) || data || []; break
          case 'ingresses':
            data = await getK8sIngresses(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.ingresses) || data || []; break
          case 'pvcs':
            data = await getK8sPVCs(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.pvcs) || data || []; break
          case 'pvs':
            data = await getK8sPVs(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.pvs) || data || []; break
          case 'hpas':
            data = await getK8sHPAs(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.hpas) || data || []; break
          case 'roles':
            data = await getK8sRoles(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.roles) || data || []; break
          case 'rolebindings':
            data = await getK8sRoleBindings(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.rolebindings) || data || []; break
          case 'clusterroles':
            data = await getK8sClusterRoles(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.clusterroles) || data || []; break
          case 'clusterrolebindings':
            data = await getK8sClusterRoleBindings(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.clusterrolebindings) || data || []; break
          case 'crds':
            data = await getK8sCRDs(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.crds) || data || []; break
          case 'nodes':
            data = await getK8sNodes(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.nodes) || data || []; break
          case 'networkpolicies':
            data = await getK8sNetworkPolicies(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.networkpolicies) || data || []; break
          case 'ingressclasses':
            data = await getK8sIngressClasses(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.ingressclasses) || data || []; break
          case 'storageclasses':
            data = await getK8sStorageClasses(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.storageclasses) || data || []; break
          case 'pdbs':
            data = await getK8sPDBs(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.pdbs) || data || []; break
          case 'serviceaccounts':
            data = await getK8sServiceAccounts(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.serviceaccounts) || data || []; break
          case 'endpointslices':
            data = await getK8sEndpointSlices(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.endpointslices) || data || []; break
          case 'resourcequotas':
            data = await getK8sResourceQuotas(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.resourcequotas) || data || []; break
          case 'limitranges':
            data = await getK8sLimitRanges(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.limitranges) || data || []; break
          case 'priorityclasses':
            data = await getK8sPriorityClasses(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.priorityclasses) || data || []; break
          case 'runtimeclasses':
            data = await getK8sRuntimeClasses(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.runtimeclasses) || data || []; break
          case 'leases':
            data = await getK8sLeases(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.leases) || data || []; break
          case 'events':
            data = await getK8sEvents(cid, ns); if (seq !== this._fetchSeq) return; this.resources = (data && data.events) || data || []; break
          case 'mutatingwebhooks':
            data = await getK8sMutatingWebhooks(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.mutatingwebhooks) || data || []; break
          case 'validatingwebhooks':
            data = await getK8sValidatingWebhooks(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.validatingwebhooks) || data || []; break
          case 'csrs':
            data = await getK8sCSRs(cid); if (seq !== this._fetchSeq) return; this.resources = (data && data.csrs) || data || []; break
          default:
            this.resources = []
        }
      } catch (e) {
        if (seq !== this._fetchSeq) return
        this.error = e.j?.error || t('error.k8sResourcesFailed')
        this.resources = []
      } finally {
        if (seq === this._fetchSeq) {
          this.resourcesLoading = false
        }
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
    async scaleStatefulSet(clusterID, ns, name, replicas) {
      return await scaleK8sStatefulSet(clusterID, ns, name, replicas)
    },
    async restartStatefulSet(clusterID, ns, name) {
      return await restartK8sStatefulSet(clusterID, ns, name)
    },
    async restartDaemonSet(clusterID, ns, name) {
      return await restartK8sDaemonSet(clusterID, ns, name)
    },
    async deleteJob(clusterID, ns, name) {
      return await deleteK8sJob(clusterID, ns, name)
    },
    async deleteCronJob(clusterID, ns, name) {
      return await deleteK8sCronJob(clusterID, ns, name)
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