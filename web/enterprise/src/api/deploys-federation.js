// 多集群联邦部署相关 API
//
// Endpoint 契约（后端 internal/controlplane/deploys_federation.go，mux 已注册）：
//   - GET   /deploys/federation        联邦部署列表 → {deploys: [FederationDeploy]}
//   - POST  /deploys/federation        创建联邦部署 → FederationDeploy（201）
//       body: {name, clusters: [clusterID], template, params}
//   - GET   /deploys/federation/{id}   联邦部署详情 → FederationDeploy
//
// 响应结构：
//   FederationDeploy {id, name, clusters: [{clusterID, status, error}],
//                     template, params, createdAt, createdBy, status}
import { getJSON, postJSON } from './request'

// 联邦部署列表
export const getFederationDeploys = () => getJSON('/deploys/federation')

// 创建联邦部署
export const createFederationDeploy = (body) => postJSON('/deploys/federation', body)

// 联邦部署详情
export const getFederationDeploy = (id) => getJSON(`/deploys/federation/${encodeURIComponent(id)}`)