// SSE 实时推送客户端 — 基于 fetch 流式读取（非 EventSource）。
//
// 为什么不用原生 EventSource：EventSource 不支持自定义请求头，而本系统在
// require-auth 模式下要求 X-Tenant-ID（网关注入）或 Authorization Bearer；
// fetch + ReadableStream 可携带任意头，且能拿到 HTTP 状态码做错误分级。
//
// 协议契约（与 docs/sse-protocol.md 逐字对齐，字段名为 Go tag 直出 camelCase）：
//   - 信封：{ type, tenantID?, data, traceID? }
//   - 事件类型 10 种：hello / task_status / alert_new / device_online /
//     device_offline / approval_status / schedule_status / os_template_changed /
//     mw_template_changed / agent_logs
//
// 行为：
//   - 自动重连（指数退避 1s→30s 上限，页面可见时）
//   - 心跳注释帧（: ping）不触发事件
//   - 契约校验：信封字段缺失或事件 data 关键字段缺失 → console.warn + 丢弃（不崩）
//   - 断线/异常时 onDisconnect 回调（调用方可降级到轮询）
//   - 401 时尝试刷新 token 后重连；刷新失败则停止（由 request.js 拦截器跳登录）

import { refreshToken } from './request'

// EVENT_CONTRACT：事件 → data 必含字段（与 sse-protocol.md 表对齐）。
// 用于运行时契约校验（DoD：前端对 SSE 字段名做静态校验/契约测试）。
export const EVENT_CONTRACT = {
  hello: [],
  task_status: ['taskID', 'status'],
  alert_new: ['alertID', 'severity'],
  device_online: ['deviceID', 'segment'],
  device_offline: ['deviceID'],
  approval_status: ['requestID', 'action'],
  schedule_status: ['scheduleID', 'action'],
  os_template_changed: ['templateID', 'action'],
  mw_template_changed: ['templateID', 'action'],
  agent_logs: ['agentID', 'logName', 'lines']
}

const ALLOWED_TYPES = Object.keys(EVENT_CONTRACT)

// validateEnvelope 校验 SSE 信封契约；不合法返回 false（调用方丢弃）。
export function validateEnvelope(ev) {
  if (!ev || typeof ev !== 'object') return false
  if (!ALLOWED_TYPES.includes(ev.type)) return false
  if (ev.data === undefined || ev.data === null) return false
  // data 应为对象（hello 为 {}，其余为 JSON 对象）
  if (typeof ev.data !== 'object') return false
  return true
}

// validateEventData 校验事件 data 的关键字段（与 EVENT_CONTRACT 对齐）。
export function validateEventData(type, data) {
  const required = EVENT_CONTRACT[type] || []
  return required.every((k) => data[k] !== undefined && data[k] !== null)
}

// parseSSEFrame 解析一段 SSE 原始文本，返回事件帧数组。
// 支持规范：event: / data: / id: / retry: / 注释帧（: xx）。
// 返回 [{ event, data, id }]，data 为原始字符串（可能多行拼接）。
export function parseSSEFrame(raw) {
  const frames = []
  let event = 'message'
  let id = null
  let dataLines = []
  const lines = String(raw).split(/\r?\n/)
  for (const line of lines) {
    if (line === '') {
      // 空行 = 帧结束
      if (dataLines.length > 0) {
        frames.push({ event, data: dataLines.join('\n'), id })
      }
      event = 'message'
      id = null
      dataLines = []
      continue
    }
    if (line.startsWith(':')) continue // 注释帧（心跳 : ping）
    const colonIdx = line.indexOf(':')
    const field = colonIdx === -1 ? line : line.slice(0, colonIdx)
    const value = colonIdx === -1 ? '' : line.slice(colonIdx + 1).replace(/^ /, '')
    if (field === 'event') event = value
    else if (field === 'data') dataLines.push(value)
    else if (field === 'id') id = value
    // retry 字段忽略（我们自管重连退避）
  }
  // 末尾无空行但已有数据：flush
  if (dataLines.length > 0) {
    frames.push({ event, data: dataLines.join('\n'), id })
  }
  return frames
}

// SSE 客户端类：连接 / 事件分发 / 自动重连 / 契约校验 / 断开回调。
export class SSEClient {
  /**
   * @param {Object} opts
   * @param {string} opts.url         SSE 端点（相对路径自动拼接 baseURL）
   * @param {Object} [opts.headers]   额外请求头（如 Authorization Bearer）
   * @param {(ev:{type:string,data:Object,tenantID?:string,traceID?:string})=>void} opts.onEvent
   * @param {()=>void} [opts.onDisconnect]   断线（需重连）回调
   * @param {()=>void} [opts.onConnect]      首次连接成功回调
   * @param {number} [opts.maxRetry=30]      最大退避秒数
   */
  constructor({ url, headers, onEvent, onDisconnect, onConnect, maxRetry = 30 }) {
    this.url = url
    this.headers = headers || {}
    this.onEvent = onEvent
    this.onDisconnect = onDisconnect
    this.onConnect = onConnect
    this.maxRetry = maxRetry
    this.abort = null
    this.retryDelay = 1000
    this.stopped = false
    this.connected = false
    this._validateCount = 0 // 契约违规计数（测试/观测用）
    this._refreshAttempted = false // ：401 时是否已尝试过 token 刷新（防死循环）
  }

  start() {
    this.stopped = false
    this._connect()
  }

  stop() {
    this.stopped = true
    if (this.abort) this.abort.abort()
    this.abort = null
  }

  // 是否已建立连接（供调用方判断轮询降级）
  isConnected() {
    return this.connected
  }

  _connect() {
    if (this.stopped) return
    const ctrl = new AbortController()
    this.abort = ctrl
    fetch(this.url, { headers: this.headers, signal: ctrl.signal })
      .then(async (resp) => {
        if (!resp.ok) {
          this.connected = false
          // ：401 时尝试刷新 token 后重连一次（at 过期场景）；
          // 刷新失败或已尝试过则停止重连，交 request.js 拦截器跳登录。
          if (resp.status === 401 && !this._refreshAttempted) {
            this._refreshAttempted = true
            try {
              await refreshToken()
              this._connect()
              return
            } catch (_) {
              // 刷新失败：request.js 拦截器已触发 unauthorizedHandler 跳登录
              if (this.onDisconnect) this.onDisconnect()
              return
            }
          }
          // 403 或已尝试过刷新：不重试（避免死循环）——交调用方降级轮询。
          if (this.onDisconnect) this.onDisconnect()
          return
        }
        if (!resp.body) throw new Error('SSE: 无响应体')
        this.connected = true
        this.retryDelay = 1000 // 连接成功重置退避
        this._refreshAttempted = false // 连接成功重置刷新标记，后续 401 可再次刷新
        if (this.onConnect) this.onConnect()
        const reader = resp.body.getReader()
        const decoder = new TextDecoder()
        let buf = ''
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true })
          // 按帧边界切分（空行分隔；保留尾部未完成数据到下一块）
          let idx
          let match
          while ((match = buf.match(/\r?\n\r?\n/)) !== null) {
            const idx = match.index
            const frameEnd = buf.slice(0, idx)
            buf = buf.slice(idx + match[0].length)
            this._dispatchFrame(frameEnd)
          }
        }
        this.connected = false
        this._scheduleRetry('连接已关闭')
      })
      .catch((err) => {
        this.connected = false
        if (this.stopped) return // 主动 stop 不算故障
        if (err.name === 'AbortError') return
        this._scheduleRetry(String(err.message || err))
      })
  }

  _dispatchFrame(frameText) {
    const frames = parseSSEFrame(frameText)
    for (const f of frames) {
      if (f.event === 'message' && !f.data) continue
      let parsed = null
      try {
        parsed = f.data ? JSON.parse(f.data) : {}
      } catch {
        this._validateCount++
        console.warn('[sse] 事件 data 非 JSON，丢弃:', f.event)
        continue
      }
      // 类型以帧的 event 行为准（后端事件帧 event 行与信封 type 一致；
      // 握手帧 event: hello 的 data 为空对象无 type——这是协议的两种形态，见 sse-protocol.md）。
      // 信封结构：{ type, tenantID?, data, traceID? }
      const type = parsed.type || f.event
      const ev = {
        type,
        tenantID: parsed.tenantID,
        traceID: parsed.traceID,
        data: parsed.data !== undefined ? parsed.data : {}
      }
      if (!validateEnvelope(ev)) {
        this._validateCount++
        console.warn('[sse] 信封契约校验失败，丢弃:', ev)
        continue
      }
      if (!validateEventData(ev.type, ev.data)) {
        this._validateCount++
        console.warn(`[sse] ${ev.type} 事件 data 缺关键字段，丢弃:`, ev.data)
        continue
      }
      if (this.onEvent) this.onEvent(ev)
    }
  }

  _scheduleRetry(reason) {
    if (this.stopped) return
    const delay = Math.min(this.retryDelay, this.maxRetry * 1000)
    if (this.onDisconnect) this.onDisconnect(reason)
    this._retryTimer = setTimeout(() => {
      this.retryDelay = Math.min(this.retryDelay * 2, this.maxRetry * 1000)
      this._connect()
    }, delay)
  }
}

// 便捷工厂：创建已绑定事件的 SSE 客户端（供 App.vue 等调用）。
export function createSSEClient(opts) {
  return new SSEClient(opts)
}
