// K8s 集群 / 资源管理 API
// 契约：
//   GET    /api/v1/k8s/clusters                                  → 200 {clusters: [{id,name,server,status,createdAt,updatedAt}]}
//   POST   /api/v1/k8s/clusters          {name,server,kubeconfig} → 200 Cluster
//   DELETE /api/v1/k8s/clusters/{id}                             → 204
//   POST   /api/v1/k8s/clusters/{id}/test                        → 200 {status,message}
//   GET    /api/v1/k8s/clusters/{id}/namespaces                  → 200 {namespaces: [{name,status,createdAt}]}
//   GET    /api/v1/k8s/clusters/{id}/pods?namespace={ns}         → 200 {pods: [{name,namespace,status,podIP,nodeIP,restarts,age}]}
//   GET    /api/v1/k8s/clusters/{id}/pods/{ns}/{name}/logs       → 200 {logs: "..."}
//   DELETE /api/v1/k8s/clusters/{id}/pods/{ns}/{name}            → 204
//   GET    /api/v1/k8s/clusters/{id}/deployments?namespace={ns}  → 200 {deployments: [{name,namespace,replicas,availableReplicas,image}]}
//   POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/scale  {replicas} → 200 {name,replicas}
//   POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/restart        → 200 {status,restartedAt}
//   GET    /api/v1/k8s/clusters/{id}/services?namespace={ns}     → 200 {services: [{name,namespace,type,clusterIP,externalIP,ports}]}
//   GET    /api/v1/k8s/clusters/{id}/configmaps?namespace={ns}   → 200 {configmaps: [{name,namespace,dataKeys}]}
//   GET    /api/v1/k8s/clusters/{id}/secrets?namespace={ns}      → 200 {secrets: [{name,namespace,type,dataKeys}]}
//   GET    /api/v1/k8s/clusters/{id}/nodes                       → 200 {nodes: [{name,status,roles,version,internalIP,externalIP,cpu,memory}]}
import { getJSON, postJSON, deleteJSON } from './request'

// ---------- K8s 集群管理 ----------

// 列出所有集群（kubeconfig 已脱敏为 ***）
export const getK8sClusters = () => getJSON('/k8s/clusters')

// 添加集群
export const createK8sCluster = (name, server, kubeconfig) =>
  postJSON('/k8s/clusters', { name, server, kubeconfig })

// 删除集群：返回 {s, j}（j 为 null，204）
export const deleteK8sCluster = (id) =>
  deleteJSON(`/k8s/clusters/${encodeURIComponent(id)}`)

// 测试集群连接：返回 {s, j}，j 形如 {status, message}
export const testK8sCluster = (id) =>
  postJSON(`/k8s/clusters/${encodeURIComponent(id)}/test`)

// ---------- K8s 资源管理 ----------

// 列出 namespace
export const getK8sNamespaces = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/namespaces`)

// 列出 pod（按 namespace 过滤）
export const getK8sPods = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/pods`, namespace ? { namespace } : undefined)

// 获取 pod 日志（tailLines 默认 100，container 可选）
export const getK8sPodLogs = (clusterID, ns, name, tailLines, container) => {
  const params = {}
  if (tailLines) params.tailLines = tailLines
  if (container) params.container = container
  return getJSON(
    `/k8s/clusters/${encodeURIComponent(clusterID)}/pods/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/logs`,
    params
  )
}

// 删除 pod：返回 {s, j}（j 为 null，204）
export const deleteK8sPod = (clusterID, ns, name) =>
  deleteJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/pods/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`)

// 列出 deployment（按 namespace 过滤）
export const getK8sDeployments = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/deployments`, namespace ? { namespace } : undefined)

// 扩缩容 deployment：body {replicas}，返回 {s, j}，j 形如 {name, replicas}
export const scaleK8sDeployment = (clusterID, ns, name, replicas) =>
  postJSON(
    `/k8s/clusters/${encodeURIComponent(clusterID)}/deployments/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/scale`,
    { replicas }
  )

// 重启 deployment：返回 {s, j}，j 形如 {status, restartedAt}
export const restartK8sDeployment = (clusterID, ns, name) =>
  postJSON(
    `/k8s/clusters/${encodeURIComponent(clusterID)}/deployments/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/restart`,
    {}
  )

// 列出 service（按 namespace 过滤）
export const getK8sServices = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/services`, namespace ? { namespace } : undefined)

// 列出 configmap（按 namespace 过滤）
export const getK8sConfigMaps = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/configmaps`, namespace ? { namespace } : undefined)

// 列出 secret（按 namespace 过滤）
export const getK8sSecrets = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/secrets`, namespace ? { namespace } : undefined)

// 列出 node
export const getK8sNodes = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/nodes`)

// 列出 statefulset（按 namespace 过滤）
export const getK8sStatefulSets = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/statefulsets`, namespace ? { namespace } : undefined)

// 扩缩容 statefulset
export const scaleK8sStatefulSet = (clusterID, ns, name, replicas) =>
  postJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/statefulsets/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/scale`, { replicas })

// 重启 statefulset
export const restartK8sStatefulSet = (clusterID, ns, name) =>
  postJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/statefulsets/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/restart`, {})

// 列出 daemonset（按 namespace 过滤）
export const getK8sDaemonSets = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/daemonsets`, namespace ? { namespace } : undefined)

// 重启 daemonset
export const restartK8sDaemonSet = (clusterID, ns, name) =>
  postJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/daemonsets/${encodeURIComponent(ns)}/${encodeURIComponent(name)}/restart`, {})

// 列出 replicaset（按 namespace 过滤）
export const getK8sReplicaSets = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/replicasets`, namespace ? { namespace } : undefined)

// 列出 job（按 namespace 过滤）
export const getK8sJobs = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/jobs`, namespace ? { namespace } : undefined)

// 删除 job
export const deleteK8sJob = (clusterID, ns, name) =>
  deleteJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/jobs/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`)

// 列出 cronjob（按 namespace 过滤）
export const getK8sCronJobs = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/cronjobs`, namespace ? { namespace } : undefined)

// 删除 cronjob
export const deleteK8sCronJob = (clusterID, ns, name) =>
  deleteJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/cronjobs/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`)

// 列出 ingress（按 namespace 过滤）
export const getK8sIngresses = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/ingresses`, namespace ? { namespace } : undefined)

// 列出 pvc（按 namespace 过滤）
export const getK8sPVCs = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/pvcs`, namespace ? { namespace } : undefined)

// 列出 pv
export const getK8sPVs = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/pvs`)

// 列出 hpa（按 namespace 过滤）
export const getK8sHPAs = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/hpas`, namespace ? { namespace } : undefined)

// 列出 role（按 namespace 过滤）
export const getK8sRoles = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/roles`, namespace ? { namespace } : undefined)

// 列出 rolebinding（按 namespace 过滤）
export const getK8sRoleBindings = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/rolebindings`, namespace ? { namespace } : undefined)

// 列出 clusterrole
export const getK8sClusterRoles = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/clusterroles`)

// 列出 clusterrolebinding
export const getK8sClusterRoleBindings = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/clusterrolebindings`)

// 列出 crd
export const getK8sCRDs = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/crds`)

// ---------- K8s 资源管理（扩展资源类型） ----------

// 列出 networkpolicy（按 namespace 过滤）
export const getK8sNetworkPolicies = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/networkpolicies`, namespace ? { namespace } : undefined)

// 列出 ingressclass（集群级）
export const getK8sIngressClasses = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/ingressclasses`)

// 列出 storageclass（集群级）
export const getK8sStorageClasses = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/storageclasses`)

// 列出 poddisruptionbudget（按 namespace 过滤）
export const getK8sPDBs = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/pdbs`, namespace ? { namespace } : undefined)

// 列出 serviceaccount（按 namespace 过滤）
export const getK8sServiceAccounts = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/serviceaccounts`, namespace ? { namespace } : undefined)

// 列出 endpointslice（按 namespace 过滤）
export const getK8sEndpointSlices = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/endpointslices`, namespace ? { namespace } : undefined)

// 列出 resourcequota（按 namespace 过滤）
export const getK8sResourceQuotas = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/resourcequotas`, namespace ? { namespace } : undefined)

// 列出 limitrange（按 namespace 过滤）
export const getK8sLimitRanges = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/limitranges`, namespace ? { namespace } : undefined)

// 列出 priorityclass（集群级）
export const getK8sPriorityClasses = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/priorityclasses`)

// 列出 runtimeclass（集群级）
export const getK8sRuntimeClasses = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/runtimeclasses`)

// 列出 lease（按 namespace 过滤）
export const getK8sLeases = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/leases`, namespace ? { namespace } : undefined)

// 列出 event（按 namespace 过滤）
export const getK8sEvents = (clusterID, namespace) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/events`, namespace ? { namespace } : undefined)

// 列出 mutatingwebhookconfiguration（集群级）
export const getK8sMutatingWebhooks = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/mutatingwebhooks`)

// 列出 validatingwebhookconfiguration（集群级）
export const getK8sValidatingWebhooks = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/validatingwebhooks`)

// 列出 certificatesigningrequest（集群级）
export const getK8sCSRs = (clusterID) =>
  getJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/csrs`)

// 创建 namespace
export const createK8sNamespace = (clusterID, name) =>
  postJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/namespaces`, { name })

// 删除 namespace
export const deleteK8sNamespace = (clusterID, name) =>
  deleteJSON(`/k8s/clusters/${encodeURIComponent(clusterID)}/namespaces/${encodeURIComponent(name)}`)

// 部署 K8s 集群（裸机/VM → 完整集群）
export const deployK8sCluster = (spec) => postJSON('/k8s/clusters/deploy', spec)

// 查询部署状态
export const getK8sClusterDeployStatus = (deployID) =>
  getJSON('/k8s/clusters/deploy/status', { deployID })

// ---------- K8s 生命周期（契约：internal/k8slifecycle/handler.go） ----------
//   POST /api/v1/k8s/lifecycle/upgrade/plan  {clusterId,targetVersion,host,user,sshKey} → 200 {currentVersion,targetVersion,planOutput,available}
//   POST /api/v1/k8s/lifecycle/upgrade       {clusterId,targetVersion}                  → 501 {error,code:"ERR_NOT_IMPLEMENTED"}
//   POST /api/v1/k8s/lifecycle/rollback      {clusterId,targetVersion,reason}           → 501 同上
//   POST /api/v1/k8s/lifecycle/cert-rotate   {clusterId}                                → 501 同上
//   POST /api/v1/k8s/lifecycle/node-scale    {clusterId,action(add|remove),nodes,joinCmd?,cpNode?} → 200 {added:[…]|removed:[…]}
//   POST /api/v1/k8s/lifecycle/etcd-backup   {clusterId,cpNode,backupDir}               → 200 {backupPath,size,timestamp}
//   GET  /api/v1/k8s/lifecycle/etcd-backups  ?cpNode&backupDir                          → 200 {backups:[…]}
// 升级/回滚/证书轮换的执行链路尚未实现（A 项裁决为显式 501，C 项立项中），
// 因此这里不提供封装：UI 已禁用入口，避免"点了就说升级完成"的假成功。
// silent=true：调用方（K8sLifecycleView）自行经页面错误横幅展示，避免全局 toast 双提示。
export const planK8sUpgrade = (spec) => postJSON('/k8s/lifecycle/upgrade/plan', spec, true)
export const scaleK8sNodes = (spec) => postJSON('/k8s/lifecycle/node-scale', spec, true)
export const backupK8sEtcd = (spec) => postJSON('/k8s/lifecycle/etcd-backup', spec, true)
export const listK8sEtcdBackups = (params) => getJSON('/k8s/lifecycle/etcd-backups', params, true)