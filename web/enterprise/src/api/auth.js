// 认证与用户管理 API — 对接后端 /api/v1/auth/* /users /roles /permissions
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

export const authApi = {
  // —— 认证 ——
  login: (username, password) => postJSON('/auth/login', { username, password }),
  register: (username, password, email) =>
    postJSON('/auth/register', { username, password, email: email || undefined }),
  me: () => getJSON('/auth/me'),
  logout: () => postJSON('/auth/logout'),

  // —— 用户管理 ——
  listUsers: () => getJSON('/users'),
  createUser: (data) => postJSON('/users', data),
  updateUser: (id, data) => putJSON(`/users/${encodeURIComponent(id)}`, data),
  deleteUser: (id) => deleteJSON(`/users/${encodeURIComponent(id)}`),

  // —— 角色管理 ——
  listRoles: () => getJSON('/roles'),
  createRole: (data) => postJSON('/roles', data),
  updateRole: (id, data) => putJSON(`/roles/${encodeURIComponent(id)}`, data),
  deleteRole: (id) => deleteJSON(`/roles/${encodeURIComponent(id)}`),

  // —— 权限 ——
  listPermissions: () => getJSON('/permissions')
}