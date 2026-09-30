// GPU 资源管理 API
// 契约：
//   GET    /api/v1/gpu/nodes                                → 200 {nodes: [{id,name,model,memory,status,health,temperature,utilization,createdAt}]}
//   GET    /api/v1/gpu/workloads                            → 200 {workloads: [{id,name,type,model,status,gpuCount,nodeId,createdAt}]}
//   POST   /api/v1/gpu/workloads     {name,type,model,gpuCount,nodeId} → 200 Workload
//   DELETE /api/v1/gpu/workloads/{id}                       → 204
//   GET    /api/v1/gpu/models                               → 200 {models: [{name,size,status,nodeId}]}
//   POST   /api/v1/gpu/models        {name,nodeId}         → 200 {status,message}
//   DELETE /api/v1/gpu/models/{name}                        → 204
//   GET    /api/v1/gpu/quotas                               → 200 {quotas: [{tenantId,totalGpu,usedGpu,limit}]}
//   GET    /api/v1/gpu/metrics?nodeId={id}&range={range}    → 200 {metrics: [{node_id,timestamp,source,gpus:[{index,utilization_pct,memory_used_mb,temperature_c,...}],avg_utilization,total_memory_used_mb,avg_temperature_c}]}
//         字段名为 snake_case（后端 models.GPUMetrics 的 json tag）；source=nvidia-smi 为真实采集，
//         simulated 为无 GPU/无该工具时的兜底——GpuView 据此显示来源提示，消费方不得把 simulated 当容量依据。
import { getJSON, postJSON, deleteJSON } from './request'

// ---------- GPU 节点 ----------

export const getGpuNodes = () => getJSON('/gpu/nodes')

// ---------- AI 工作负载 ----------

export const getGpuWorkloads = () => getJSON('/gpu/workloads')

export const createGpuWorkload = (name, type, model, gpuCount, nodeId) =>
  postJSON('/gpu/workloads', { name, type, model, gpuCount, nodeId })

export const deleteGpuWorkload = (id) =>
  deleteJSON(`/gpu/workloads/${encodeURIComponent(id)}`)

// ---------- Ollama 模型 ----------

export const getGpuModels = () => getJSON('/gpu/models')

export const pullGpuModel = (name, nodeId) =>
  postJSON('/gpu/models', { name, nodeId })

export const deleteGpuModel = (name) =>
  deleteJSON(`/gpu/models/${encodeURIComponent(name)}`)

// ---------- 配额 ----------

export const getGpuQuotas = () => getJSON('/gpu/quotas')

// ---------- 指标 ----------

export const getGpuMetrics = (nodeId, range) =>
  getJSON('/gpu/metrics', { nodeId, range })

// 兼容旧引用：聚合对象形式
export const gpuApi = {
  getGpuNodes,
  getGpuWorkloads,
  createGpuWorkload,
  deleteGpuWorkload,
  getGpuModels,
  pullGpuModel,
  deleteGpuModel,
  getGpuQuotas,
  getGpuMetrics
}
