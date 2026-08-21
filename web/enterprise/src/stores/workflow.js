// 作业编排 store — DAG 节点编辑
import { defineStore } from 'pinia'
import {
  getWorkflows, getWorkflow, createWorkflow, updateWorkflow,
  runWorkflow, getWorkflowStatus, scheduleWorkflow
} from '@/api/workflow'
import { t } from '@/i18n'

export const useWorkflowStore = defineStore('workflow', {
  state: () => ({
    list: [],
    current: { id: 0, name: '', agentID: '', cron: '', dag: [], status: 'draft' },
    nodePos: {},          // { nodeId: {x, y} }
    selectedNode: null,
    selectedEdge: null,   // { src, dst }
    status: {},           // 运行态 { nodeId: 'running'|'done'|'failed' }
    loading: false,
    error: '',
    msg: ''
  }),
  actions: {
    async fetchList() {
      try { this.list = await getWorkflows() || [] }
      catch (e) { this.error = e.j?.error || t('error.workflowListFailed') }
    },
    async open(id) {
      if (!id) { this.reset(); return }
      try {
        const w = await getWorkflow(id)
        this.current = {
          id: w.id, name: w.name, agentID: w.agentID, cron: w.cron || '',
          dag: w.dag ? JSON.parse(w.dag) : [], status: w.status
        }
        this.nodePos = {}
        this.autoLayout()
      } catch (e) { this.error = e.j?.error || t('error.workflowDetailFailed') }
    },
    reset() {
      this.current = { id: 0, name: '', agentID: '', cron: '', dag: [], status: 'draft' }
      this.nodePos = {}; this.selectedNode = null; this.selectedEdge = null; this.status = {}
    },
    addNode() {
      let id = 'n' + (this.current.dag.length + 1)
      while (this.current.dag.some((n) => n.id === id)) id = 'n' + Math.floor(Math.random() * 1000)
      this.current.dag.push({ id, name: '步骤' + id, type: 'shell', command: '', path: '', dependsOn: [] })
      this.nodePos[id] = { x: 60, y: 60 + this.current.dag.length * 70 }
      this.selectedNode = id
    },
    deleteNode(id) {
      this.current.dag = this.current.dag.filter((n) => n.id !== id)
      this.current.dag.forEach((n) => { n.dependsOn = (n.dependsOn || []).filter((d) => d !== id) })
      delete this.nodePos[id]
      if (this.selectedNode === id) this.selectedNode = null
    },
    addEdge(src, dst) {
      const node = this.current.dag.find((n) => n.id === dst)
      if (!node) return
      if (!node.dependsOn.includes(src)) node.dependsOn.push(src)
    },
    autoLayout() {
      const indeg = {}, adj = {}
      this.current.dag.forEach((n) => { indeg[n.id] = 0; adj[n.id] = [] })
      this.current.dag.forEach((n) => {
        (n.dependsOn || []).forEach((d) => { if (indeg[d] !== undefined) { indeg[d]++; adj[n.id].push(d) } })
      })
      const level = {}, q = []
      this.current.dag.forEach((n) => { if (indeg[n.id] === 0) { level[n.id] = 0; q.push(n.id) } })
      while (q.length) {
        const cur = q.shift()
        ;(adj[cur] || []).forEach((p) => {
          if (level[p] === undefined || level[cur] + 1 > level[p]) level[p] = level[cur] + 1
          indeg[p]--; if (indeg[p] === 0) q.push(p)
        })
      }
      this.current.dag.forEach((n) => { if (level[n.id] === undefined) level[n.id] = 0 })
      const per = {}
      this.nodePos = {}
      this.current.dag.forEach((n) => {
        const L = level[n.id]; per[L] = per[L] || 0
        const idx = per[L]++
        this.nodePos[n.id] = { x: 60 + L * 230, y: 50 + idx * 100 }
      })
    },
    async save() {
      const body = {
        name: this.current.name, agentID: this.current.agentID, cron: this.current.cron,
        dag: JSON.stringify(this.current.dag)
      }
      try {
        const r = this.current.id ? await updateWorkflow(this.current.id, body) : await createWorkflow(body)
        if (r.s < 400 && r.j.id) { this.current.id = r.j.id; this.current.status = r.j.status || this.current.status }
        this.msg = `[${r.s}] ${JSON.stringify(r.j)}`
        await this.fetchList()
        return r
      } catch (e) { this.error = e.j?.error || t('error.workflowSaveFailed'); throw e }
    },
    async run() {
      try { return await runWorkflow(this.current.id) }
      catch (e) { this.error = e.j?.error || t('error.workflowRunFailed'); throw e }
    },
    async fetchStatus() {
      try { this.status = await getWorkflowStatus(this.current.id) || {} }
      catch (e) { this.error = e.j?.error || t('error.workflowStatusFailed') }
    },
    async schedule(cron) {
      try { return await scheduleWorkflow(this.current.id, cron) }
      catch (e) { this.error = e.j?.error || t('error.workflowScheduleFailed'); throw e }
    }
  }
})