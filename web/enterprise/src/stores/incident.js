// Incident 管理 store — Incident CRUD + 时间线 + 复盘
import { defineStore } from 'pinia'
import {
  getIncidents,
  createIncident,
  getIncident,
  updateIncident,
  deleteIncident,
  getIncidentTimeline,
  addTimelineEvent,
  generatePostmortem,
  getIncidentMetrics
} from '@/api/incident'
import { t } from '@/i18n'

export const useIncidentStore = defineStore('incident', {
  state: () => ({
    incidents: [],
    currentIncident: null,
    timeline: [],
    metrics: null,
    postmortemContent: '',
    loading: false,
    timelineLoading: false,
    error: ''
  }),
  getters: {
    openIncidents: (s) => s.incidents.filter((i) => i.status === 'open'),
    criticalIncidents: (s) => s.incidents.filter((i) => i.severity === 'critical')
  },
  actions: {
    async fetchIncidents() {
      this.loading = true; this.error = ''
      try {
        const data = await getIncidents()
        this.incidents = (data && data.incidents) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.incidentsFailed')
      } finally {
        this.loading = false
      }
    },
    async addIncident(title, severity, description, assignee) {
      return await createIncident(title, severity, description, assignee)
    },
    async fetchIncident(id) {
      this.loading = true; this.error = ''
      try {
        const data = await getIncident(id)
        this.currentIncident = data
      } catch (e) {
        this.error = e.j?.error || t('error.incidentDetailFailed')
      } finally {
        this.loading = false
      }
    },
    async editIncident(id, status, severity, assignee) {
      return await updateIncident(id, status, severity, assignee)
    },
    async removeIncident(id) {
      return await deleteIncident(id)
    },
    async fetchTimeline(id) {
      this.timelineLoading = true; this.error = ''
      try {
        const data = await getIncidentTimeline(id)
        this.timeline = (data && data.events) || data || []
      } catch (e) {
        this.error = e.j?.error || t('error.timelineFailed')
      } finally {
        this.timelineLoading = false
      }
    },
    async addEvent(id, type, content) {
      return await addTimelineEvent(id, type, content)
    },
    async fetchPostmortem(id) {
      try {
        const data = await generatePostmortem(id)
        this.postmortemContent = (data && data.content) || ''
      } catch (e) {
        this.error = e.j?.error || t('error.postmortemFailed')
      }
    },
    async fetchMetrics() {
      try {
        const data = await getIncidentMetrics()
        this.metrics = data
      } catch (e) {
        this.metrics = null
      }
    }
  }
})
