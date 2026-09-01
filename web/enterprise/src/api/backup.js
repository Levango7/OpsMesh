// 灾备备份相关 API
//
// 后端契约（internal/controlplane/backup_api.go，server_lifecycle.go 注册）：
//   - POST   /api/v1/backup/create   → BackupRecord（201）
//       请求体：{type: "full"|"config"|"devices"|"tasks"}
//       后端先落库 status=creating，由后台 goroutine 异步归档（完成后置 completed）
//   - GET    /api/v1/backup/list     → {backups: [BackupRecord]}
//   - POST   /api/v1/backup/restore   → {status: "restored", backup, restored, completedAt}
//       请求体：{id: "..."}；高危操作：读归档快照写回 store（前端必须二次确认）
//   - DELETE /api/v1/backup/{id}      → {status: "deleted"}
// BackupRecord 字段：id/type("full"|"config"|"devices"|"tasks")/
//   status("creating"|"completed"|"failed")/size/path/createdAt
import { getJSON, postJSON, deleteJSON } from './request'

export const createBackup = (type) => postJSON('/backup/create', { type })
export const listBackups = () => getJSON('/backup/list')
export const restoreBackup = (id) => postJSON('/backup/restore', { id })
export const deleteBackup = (id) => deleteJSON(`/backup/${encodeURIComponent(id)}`)
