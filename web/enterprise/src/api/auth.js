// 认证与用户管理 API — 对接后端 /api/v1/auth/* /users /roles /permissions
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const authApi = {
  // —— 认证 ——
  login: (username, password) => postJSON('/auth/login', { username, password }),
  register: (username, password, email) =>
    postJSON('/auth/register', { username, password, email: email || undefined }),
  me: () => getJSON('/auth/me'),
  logout: () => postJSON('/auth/logout'),
  // changePasswordToken 为首登强制改密场景的一次性短时效 token（登录响应带回），
  // 传给后端换取身份并签发正式会话；已登录主动改密场景可不传。
  changePassword: (oldPassword, newPassword, changePasswordToken) =>
    postJSON('/auth/change-password', {
      oldPassword,
      newPassword,
      changePasswordToken: changePasswordToken || undefined
    }),

  // —— 用户管理 ——
  listUsers: () => getJSON('/users'),
  createUser: (data) => postJSON('/users', data),
  updateUser: (id, data) => putJSON(`/users/${encodeURIComponent(id)}`, data),
  deleteUser: (id) => deleteJSON(`/users/${encodeURIComponent(id)}`),
  // 注册审批（需 user:approve 权限）：仅 status=pending 用户可操作，其余状态后端返回 409。
  // reject 请求体可选 {reason}，记录到审计日志。
  approveUser: (id) => postJSON(`/users/${encodeURIComponent(id)}/approve`),
  rejectUser: (id, reason) =>
    postJSON(`/users/${encodeURIComponent(id)}/reject`, reason ? { reason } : {}),

  // —— 角色管理 ——
  listRoles: () => getJSON('/roles'),
  createRole: (data) => postJSON('/roles', data),
  updateRole: (id, data) => putJSON(`/roles/${encodeURIComponent(id)}`, data),
  deleteRole: (id) => deleteJSON(`/roles/${encodeURIComponent(id)}`),

  // —— 权限 ——
  listPermissions: () => getJSON('/permissions')
}