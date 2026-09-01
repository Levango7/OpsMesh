// 高可用（HA）状态相关 API
//
// 后端契约（internal/controlplane/ha.go，server_lifecycle.go 注册）：
//   - GET  /api/v1/ha/status    → {leader, current, instances, replicas, generatedAt}
//   - GET  /api/v1/ha/instances → {instances: [haInstanceInfo], count}
//   - POST /api/v1/ha/failover  → {status, message, current, simulated}
//       高危操作：手动触发 leader 切换（需 ha:write 权限，前端必须二次确认）
//   - GET  /api/v1/ha/health    → {status: "healthy", instance, timestamp}
// haInstanceInfo 字段：instanceID/hostname/httpPort/grpcPort/role("leader"|"follower")/isLeader
import { getJSON, postJSON } from './request'

export const getStatus = () => getJSON('/ha/status')
export const getInstances = () => getJSON('/ha/instances')
export const failover = () => postJSON('/ha/failover')
export const getHealth = () => getJSON('/ha/health')
