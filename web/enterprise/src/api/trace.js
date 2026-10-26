// Trace 可观测性 API — Jaeger / OTLP collector 查询接口
//
// 保留本模块/本路由（不删）的理由：AlertsView.vue:134 在 AIOps 根因分析推送 traceID 后
// 会 router.push({ name: 'trace', query: { traceID } }) —— 侧栏不放该入口，但告警详情页的
// 「链路追踪」按钮是**按路由名**的既有依赖。删路由/删页面会直接打断这条跳转，故只修形状不删表面。
//
// Jaeger 查询 API 契约（与 OTel collector / Jaeger 查询端点对齐）：
//   - GET /api/services              列出所有服务
//   - GET /api/services/{service}/operations  列出服务的操作
//   - GET /api/traces                查询 traces（service, operation, start, end, limit, lookback, minDuration, maxDuration, tags）
//   - GET /api/traces/{traceID}      获取 trace 详情（spans, duration, timestamps）
//
// 路径写法：这些是 Jaeger 查询端的原生路径，**不带控制面的 /api/v1 前缀**。
// axios 实例的 baseURL 是 /api/v1（见 src/api/request.js），若直接用 getJSON('/api/services')
// 会拼成 /api/v1/api/services —— 多一层前缀必然 404。故这里显式把 baseURL 覆盖为空串，
// 请求最终落在同源的 /api/services、/api/traces 上。
//
// 现状（部署前置条件，未满足前该页取不到数据）：
//   - 后端控制面没有 trace/Jaeger 查询路由（internal/controlplane 无对应 mux 注册，
//     svcproxy 规则表也无 trace 域）；
//   - 前端 dev/preview 代理只有 '/api' → 控制面 一条规则（vite.config.js）。
// 因此要真正跑通，边缘代理（nginx/ingress）需把同源路径转到 Jaeger 查询端：
//   location /api/services → http://<jaeger>:16686/api/services
//   location /api/traces   → http://<jaeger>:16686/api/traces
// （或由控制面新增一个转发到 Jaeger 的 /api/v1/trace/* 代理端点，二者取一。）
//
// 后端 OTel 已通过 --otel-endpoint 导出到 OTLP gRPC（Jaeger 作为 OTLP collector 接收即可）。
import { http } from './request'

// 走 Jaeger 原生路径：覆盖实例 baseURL（'/api/v1'），避免二次拼前缀。
async function jaegerGet(url, params) {
  const { j } = await http.get(url, { baseURL: '', params })
  return j
}

export const getTraceServices = () => jaegerGet('/api/services')

export const getTraceServiceOperations = (service) => jaegerGet(`/api/services/${encodeURIComponent(service)}/operations`)

export const getTraces = (params) => jaegerGet('/api/traces', params)

export const getTrace = (traceID) => jaegerGet(`/api/traces/${encodeURIComponent(traceID)}`)

// 将 Jaeger trace 数据转为前端友好的树形结构
export function normalizeTrace(trace) {
  if (!trace || !trace.data || !trace.data[0]) return null

  const rawTrace = trace.data[0]
  const spans = rawTrace.spans || []

  // 构建 span 树：按 spanID 索引，parentSpanID 建立父子关系
  const spanMap = new Map()
  const rootSpans = []

  spans.forEach((s) => {
    spanMap.set(s.spanID, {
      ...s,
      children: [],
      durationMs: s.duration ? Math.round(s.duration / 1000000) : 0, // 纳秒 -> 毫秒
      startTimeMs: s.startTime ? Math.round(s.startTime / 1000000) : 0,
    })
  })

  spans.forEach((s) => {
    const span = spanMap.get(s.spanID)
    if (s.parentSpanID && spanMap.has(s.parentSpanID)) {
      spanMap.get(s.parentSpanID).children.push(span)
    } else {
      rootSpans.push(span)
    }
  })

  // 按开始时间排序
  rootSpans.sort((a, b) => a.startTimeMs - b.startTimeMs)
  spans.forEach((s) => {
    const span = spanMap.get(s.spanID)
    if (span.children.length > 0) {
      span.children.sort((a, b) => a.startTimeMs - b.startTimeMs)
    }
  })

  const totalDuration = spans.reduce((max, s) => {
    const end = s.startTime + s.duration
    return Math.max(max, end)
  }, 0) - spans.reduce((min, s) => Math.min(min, s.startTime), spans[0]?.startTime || 0)

  return {
    traceID: rawTrace.traceID,
    spanCount: spans.length,
    totalDurationMs: totalDuration > 0 ? Math.round(totalDuration / 1000000) : 0,
    spans: rootSpans,
    allSpans: spans.map((s) => spanMap.get(s.spanID)),
  }
}
