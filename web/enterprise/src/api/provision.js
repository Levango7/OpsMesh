// 自动纳管相关 API
//
// Endpoint 契约（后端 internal/controlplane/provision.go，mux 已注册）：
//   - POST /provision/auto  自动纳管 → {discovered, provisioned, failed, devices: [{ip, hostname, status}]}（201）
//       body: {segment, agentVersion}
import { postJSON } from './request'

// 触发自动纳管
export const autoProvision = (body) => postJSON('/provision/auto', body)