import { postJSON, getJSON } from '@/api/request'

export const etlApi = {
  listJobs: () => postJSON('/etl/jobs', { action: 'list' }),
  createJob: (body) => postJSON('/etl/jobs', { ...body, action: 'create' }),
  updateJob: (body) => postJSON('/etl/jobs', { ...body, action: 'update' }),
  deleteJob: (id) => postJSON('/etl/jobs', { action: 'delete', job: { id } }),
  getJob: (id) => postJSON('/etl/jobs', { action: 'get', job: { id } }),
  runJob: (id, deviceID) => postJSON('/etl/jobs', { action: 'run', job: { id, params: { deviceID } } }),
  listRuns: (jobID) => getJSON('/etl/runs', jobID ? { jobID } : {}),
  updateRun: (body) => postJSON('/etl/runs', body),
  getRun: (id) => postJSON('/etl/runs', { action: 'get', runID: id })
}
