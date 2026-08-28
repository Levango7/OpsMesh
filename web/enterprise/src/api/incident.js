// Incident 管理 API
// 契约：
//   GET    /api/v1/incidents                                 → 200 {incidents: [{id,title,severity,status,assignee,createdAt,updatedAt,mttr,mttd}]}
//   POST   /api/v1/incidents          {title,severity,description,assignee} → 200 Incident
//   GET    /api/v1/incidents/{id}                            → 200 Incident
//   PUT    /api/v1/incidents/{id}     {status,severity,assignee} → 200 Incident
//   DELETE /api/v1/incidents/{id}                            → 204
//   GET    /api/v1/incidents/{id}/timeline                   → 200 {events: [{id,type,timestamp,content,author}]}
//   POST   /api/v1/incidents/{id}/timeline {type,content}    → 200 Event
//   POST   /api/v1/incidents/{id}/postmortem                 → 200 {content,generatedAt}
//   GET    /api/v1/incidents/metrics                         → 200 {mttd,mttr,total,open,resolved}
import { getJSON, postJSON, putJSON, deleteJSON } from './request'

// ---------- Incident CRUD ----------

export const getIncidents = () => getJSON('/incidents')

export const createIncident = (title, severity, description, assignee) =>
  postJSON('/incidents', { title, severity, description, assignee })

export const getIncident = (id) =>
  getJSON(`/incidents/${encodeURIComponent(id)}`)

export const updateIncident = (id, status, severity, assignee) =>
  putJSON(`/incidents/${encodeURIComponent(id)}`, { status, severity, assignee })

export const deleteIncident = (id) =>
  deleteJSON(`/incidents/${encodeURIComponent(id)}`)

// ---------- 时间线 ----------

export const getIncidentTimeline = (id) =>
  getJSON(`/incidents/${encodeURIComponent(id)}/timeline`)

export const addTimelineEvent = (id, type, content) =>
  postJSON(`/incidents/${encodeURIComponent(id)}/timeline`, { type, content })

// ---------- 复盘报告 ----------

export const generatePostmortem = (id) =>
  postJSON(`/incidents/${encodeURIComponent(id)}/postmortem`)

// ---------- 指标 ----------

export const getIncidentMetrics = () => getJSON('/incidents/metrics')

// 兼容旧引用：聚合对象形式
export const incidentApi = {
  getIncidents,
  createIncident,
  getIncident,
  updateIncident,
  deleteIncident,
  getIncidentTimeline,
  addTimelineEvent,
  generatePostmortem,
  getIncidentMetrics
}
