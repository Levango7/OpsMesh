import { postJSON, getJSON } from '@/api/request'

export const governanceApi = {
  listRules: (body) => postJSON('/governance/rules', body),
  createRule: (body) => postJSON('/governance/rules', { ...body, action: 'create' }),
  updateRule: (body) => postJSON('/governance/rules', { ...body, action: 'update' }),
  deleteRule: (id) => postJSON('/governance/rules', { action: 'delete', rule: { id } }),
  evaluateRule: (id, rows) => postJSON('/governance/rules', { action: 'evaluate', rule: { id }, rows }),
  listPII: (body) => postJSON('/governance/pii', { ...body, action: 'list' }),
  createPII: (body) => postJSON('/governance/pii', { ...body, action: 'create' }),
  updatePII: (body) => postJSON('/governance/pii', { ...body, action: 'update' }),
  deletePII: (id) => postJSON('/governance/pii', { action: 'delete', policy: { id } }),
  scanPII: (body) => postJSON('/governance/pii', { ...body, action: 'scan' }),
  listRetention: (body) => postJSON('/governance/retention', { ...body, action: 'list' }),
  createRetention: (body) => postJSON('/governance/retention', { ...body, action: 'create' }),
  updateRetention: (body) => postJSON('/governance/retention', { ...body, action: 'update' }),
  deleteRetention: (id) => postJSON('/governance/retention', { action: 'delete', policy: { id } }),
  applyRetention: (body) => postJSON('/governance/retention', { ...body, action: 'apply' }),
  catalog: (params) => getJSON('/governance/catalog', params),
  lineage: (query) => postJSON('/governance/lineage', { query })
}
