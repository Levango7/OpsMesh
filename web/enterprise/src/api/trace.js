// Trace 可观测性 API — Jaeger / OTLP collector 查询接口
//
// Jaeger 查询 API 契约（与 OTel collector / Jaeger 查询端点对齐）：
//   - GET /api/services              列出所有服务
//   - GET /api/services/{service}/operations  列出服务的操作
//   - GET /api/traces                查询 traces（service, operation, start, end, limit, lookback, minDuration, maxDuration, tags）
//   - GET /api/traces/{traceID}      获取 trace 详情（spans, duration, timestamps）
//
// 后端 OTel 已通过 --otel-endpoint 导出到 OTLP gRPC（Jaeger 作为 OTLP collector 接收即可）。
// 前端直接调用 Jaeger 查询端点，若未部署 Jaeger 则返回空结果（不破坏现有功能）。
import { getJSON } from './request'

export const getTraceServices = () => getJSON('/api/services')

export const getTraceServiceOperations = (service) => getJSON(`/api/services/${encodeURIComponent(service)}/operations`)

export const getTraces = (params) => getJSON('/api/traces', params)

export const getTrace = (traceID) => getJSON(`/api/traces/${encodeURIComponent(traceID)}`)

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
