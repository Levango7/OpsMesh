// api 层导出使用率守卫（2026-11-02）
//
// 背景：api 层是「后端能力的接线面」——每个导出函数都对应一条真实后端端点。
// 导出存在但 views/stores 无人调用 ⇒ 该能力**后端做了、前端没连**（用户 concern #1
// 的原样形态）。2026-11-02 实测：498 个导出中 59 个无人调用，其中最有价值的一族是：
//   AI 提示词 CRUD（list/create/get/update/deletePrompt）
//   + RAG 摄入/检索（ragIngest/ragSearch）+ embed + getAIHealth = **9 条 AI 能力**
//   后端全有（internal/ai/gateway/handler.go:364-378）、api 层全接、UI 零引用
//   ⇒ 整片 AI 能力面对用户不可见。另有两处真缺口：
//   云资源（getCloudAccount / listCloudResources）无列表界面；K8s 命名空间创建/删除无入口。
//
// 为什么不是删掉这些导出：导出 = 后端契约入口，删了会丢掉「这能力存在」的记录；
// 正确做法是**显式记账**——允许它们存在，但必须逐条写明为什么还没接 UI。
//
// 双向棘轮（与 page-conventions / shared-primitives / router-perms 同族）：
//   ① 新增「导出但无人调用」的函数 ⇒ 红（要么接 UI，要么进 ALLOW 写明理由）；
//   ② ALLOW 条目一旦被接上 UI ⇒ 也必须删条 ⇒ 红（防账本腐化）。
//
// 判定口径：在 src 下、非测试文件里，按标识符词边界出现即视为已用——**排除该导出
// 自己的定义文件**（否则定义行本身就把自己算成"已用"）。api 目录内的互相引用算已用
// （request.js 的 helper 被其它 api 模块 import）。**api 目录内的单测不算消费方**
// （只有测试在用的导出仍需记账，理由写「仅单测断言」）。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

const SRC = join(__dirname, '../..')
const API_DIR = join(SRC, 'api')

/**
 * 已记账的「导出但暂无 UI 消费方」。键为 `<api 文件相对路径>|<导出名>`。
 * 分类：AI 能力未接 UI / 写操作能力未接 / 详情读取未做详情页 /
 *       命名空间聚合对象 / 仅单测断言。
 * **条目只减不增**：接上 UI 后必须删条（否则测试判「陈旧条目」）。
 */
const ALLOW = new Map([
  // ── AI 能力：提示词 CRUD（AIPromptsView）、RAG 检索/入库与网关健康（AIRagView）
  //    均已于 2026-11-02 接上 UI；仅剩 embed（内部能力，暂无面向用户的界面）──
  ['ai.js|getPrompt', 'AI 提示词详情（GET /ai/prompts/{id}）：列表已含全字段、编辑直接用行数据，故无单独详情视图'],
  ['ai.js|embed', '向量化：POST /ai/embed 已有，属内部能力、暂不面向用户'],

  // ── 写操作能力未接（后端有写端点，前端只做了读）──
  ['cloud-account.js|getCloudAccount', '云账号详情：后端有端点，前端列表已含所需字段'],

  // ── 详情读取未做详情页（前端直接用列表数据）──
  ['apikeys.js|getAPIKey', '按 id 取单个 API Key：前端列表已含所需字段'],
  ['approval.js|getApprovalFlow', '按 id 取审批流：前端列表已含所需字段'],
  ['approval.js|getApprovalRequest', '按 id 取审批单：前端列表已含所需字段'],
  ['argocd.js|getApp', '按 id 取 ArgoCD 应用：前端列表已含所需字段'],
  ['automation.js|getAutomationRule', '按 id 取自动化规则：前端列表已含所需字段'],
  ['automation.js|getAutomationExecution', '按 id 取自动化执行：前端列表已含所需字段'],
  ['billing.js|getPlan', '按 id 取套餐：前端列表已含所需字段'],
  ['billing.js|getInvoice', '按 id 取发票：前端列表已含所需字段'],
  ['cmdb.js|getCIRelationGraph', '取单 CI 关系图：RelationGraph 组件走列表数据渲染'],
  ['compliance.js|getRule', '按 id 取合规规则：前端列表已含所需字段'],
  ['flux.js|getFluxApp', '按 id 取 Flux 应用：前端列表已含所需字段'],
  ['gateway.js|getRoute', '按 id 取网关路由：前端列表已含所需字段'],
  ['network.js|getNetworkDevice', '按 id 取网络设备：前端列表已含所需字段'],
  ['pipeline.js|getTemplate', '按 id 取流水线模板：前端列表已含所需字段'],
  ['pipeline.js|getRun', '按 id 取流水线运行：前端列表已含所需字段'],
  ['quota.js|getQuota', '按 id 取配额：前端列表已含所需字段'],
  ['schedule.js|getSchedule', '按 id 取定时任务：前端列表已含所需字段'],
  ['script.js|getScript', '按 id 取脚本：前端列表已含所需字段'],
  ['slo.js|getSLO', '按 id 取 SLO：前端列表已含所需字段'],
  ['tenant.js|getTenant', '按 id 取租户：前端列表已含所需字段'],
  ['ticket.js|getTicket', '按 id 取工单：前端列表已含所需字段'],
  ['traffic.js|getTrafficPolicy', '按 id 取流量策略：前端列表已含所需字段'],
  ['webhook.js|getWebhook', '按 id 取 Webhook：前端列表已含所需字段'],

  // ── 命名空间聚合对象（成员被单独 import，聚合对象本身无消费方）──
  ['admin.js|adminApi', '聚合命名空间对象：成员被单独 import'],
  ['admin.js|getDiagnosticsUrl', '被 downloadDiagnostics 在 admin.js 内部调用（getDiagnosticsUrl → /admin/diagnostics），无需 UI 直接消费'],
  ['admin.js|setLogLevel', '后端端点 POST /admin/loglevel 已有，前端日志级别切换 UI 暂未接线（logLevel select 存在但未接 setLogLevel）'],
  ['agent.js|agentApi', '聚合命名空间对象：成员被单独 import'],
  ['ai.js|aiApi', '聚合命名空间对象：成员被单独 import'],
  ['autoscaler.js|autoscalerApi', '聚合命名空间对象：成员被单独 import'],
  ['bot.js|botApi', '聚合命名空间对象：成员被单独 import'],
  ['cost.js|costApi', '聚合命名空间对象：成员被单独 import'],
  ['device.js|deviceApi', '聚合命名空间对象：成员被单独 import'],
  ['gpu.js|gpuApi', '聚合命名空间对象：成员被单独 import'],
  ['incident.js|incidentApi', '聚合命名空间对象：成员被单独 import'],
  ['knowledge.js|knowledgeApi', '聚合命名空间对象：成员被单独 import'],
  ['middleware.js|middlewareApi', '聚合命名空间对象：成员被单独 import'],
  ['os-optimize.js|osOptimizeApi', '聚合命名空间对象：成员被单独 import'],
  ['plugin.js|pluginApi', '聚合命名空间对象：成员被单独 import'],
  ['portal.js|portalApi', '聚合命名空间对象：成员被单独 import'],
  ['runbook.js|runbookApi', '聚合命名空间对象：成员被单独 import'],
  ['secrets.js|secretsApi', '聚合命名空间对象：成员被单独 import'],

  // ── 仅单测断言 / 模块内部使用（App.vue 只用 createSSEClient）──
  ['sse.js|parseSSEFrame', '仅 sse.test.js 断言：帧解析是内部纯函数'],
  ['sse.js|validateEnvelope', '仅 sse.test.js 断言：信封校验是内部纯函数'],
  ['sse.js|validateEventData', '仅 sse.test.js 断言：事件数据校验是内部纯函数'],
  ['sse.js|EVENT_CONTRACT', '仅 sse.test.js 断言：契约常量给测试做形状对照'],
  ['request.js|loginURL', 'request.js 内部使用（redirectToLogin）+ 单测断言']
])

function walk(dir, filter) {
  const out = []
  for (const n of readdirSync(dir)) {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) out.push(...walk(p, filter))
    else if (filter(p)) out.push(p)
  }
  return out
}

/** api 层导出 */
function apiExports() {
  const out = []
  for (const f of walk(API_DIR, (p) => p.endsWith('.js') && !p.includes('__tests__'))) {
    const src = readFileSync(f, 'utf-8')
    const file = relative(API_DIR, f).split(sep).join('/')
    for (const m of src.matchAll(/export\s+const\s+([A-Za-z_]\w*)\s*=/g)) out.push({ file, name: m[1], path: f })
    for (const m of src.matchAll(/export\s+(?:async\s+)?function\s+([A-Za-z_]\w*)/g)) out.push({ file, name: m[1], path: f })
  }
  return out
}

/** 消费方语料：src 下全部非测试文件（含 api 内部互相引用），按文件索引以便排除定义自身 */
function corpus() {
  const files = walk(SRC, (p) => p.endsWith('.js') || p.endsWith('.vue')).filter((p) => {
    const rel = relative(SRC, p).split(sep).join('/')
    return !rel.includes('__tests__') && !rel.endsWith('.test.js')
  })
  return new Map(files.map((f) => [f, readFileSync(f, 'utf-8')]))
}

describe('api 层导出使用率（死 API 面）', () => {
  const exports = apiExports()
  const texts = corpus()

  const used = (name, ownPath) => {
    const re = new RegExp(`\\b${name}\\b`)
    for (const [p, t] of texts) if (p !== ownPath && re.test(t)) return true
    return false
  }

  it('解析规模合理（防解析失效后空集通过）', () => {
    expect(exports.length).toBeGreaterThan(450)
    expect(texts.size).toBeGreaterThan(100)
  })

  it('检测口径自证：已知在用/已知未用各一例', () => {
    const getAIOps = exports.find((e) => e.name === 'getAIOpsStatus')
    expect(used('getAIOpsStatus', getAIOps.path)).toBe(true)
    expect(used('purgeNothingCallsThis', join(API_DIR, 'none.js'))).toBe(false)
  })

  it('未接线导出 ⊆ ALLOW，且 ALLOW 无陈旧条目（双向棘轮）', () => {
    const unused = exports.filter((e) => !used(e.name, e.path)).map((e) => `${e.file}|${e.name}`)
    const unusedSet = new Set(unused)
    const allowSet = new Set(ALLOW.keys())

    expect(
      unused.filter((k) => !allowSet.has(k)),
      '新增了「导出但无人调用」的函数：要么在 views/stores 里接上，要么加进 ALLOW 并写明为什么还没接 UI'
    ).toEqual([])
    expect(
      [...allowSet].filter((k) => !unusedSet.has(k)),
      'ALLOW 条目已被接上 UI（或导出已删）：请删掉陈旧条目，账本只减不增'
    ).toEqual([])
  })
})
