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

// 兼容旧引用：聚合对象形式
export const k8sApi = {
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
}