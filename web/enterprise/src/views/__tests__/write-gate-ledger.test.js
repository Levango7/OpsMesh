// 写按钮权限门台账（棘轮） — 2026-11-06 五维排查批
//
// 背景：后端所有写路由都已过 RBAC（internal/gates/ops_route_ratchet_gate_test.go：408/408），
// 但「前端按钮不拦」时用户点下去必吃 403——这是体验缺口，不是安全缺口。本台账把
// 全量比对结果（scripts/qa-matrix.py 的三级矩阵：路由 × 端点 × 视图 × 权限目录）固化成
// **只许减不许增**的欠账表：已挂门的不许掉，未挂门的必须先改台账再动视图。
//
// 判据（与 api-usage 双向棘轮同思路）：
//   1. GATED 里的视图：必须含每个声明的 hasPerm('<perm>') 与常驻提示 data-testid="write-denied-hint"；
//      GATED 条目形如 [view, perm1, perm2, ...]——同一视图多个动作权限点不同（如用户页建号/删号/审批）
//      必须逐一声明，少一个即判红；
//   2. PENDING 里的视图：必须**仍**未挂门（含 hasPerm 即判红）——写成"已挂门就要挪出 PENDING"，
//      避免"悄悄挂了一半、台账没人更新"；
//   3. 台账里的文件必须存在（改名/删除要同步台账）；
//   4. 共享双语键 common.write_denied 必须 zh/en 双份存在（按钮 title 与提示都指向它）。
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'

const VIEWS = path.resolve(__dirname, '..')
const LOCALES = path.resolve(__dirname, '../../i18n/locales')

// —— 已挂门（本批 2026-11-06 落地，范式：auth.hasPerm(perm) → canWrite → 写按钮 :disabled + title + 常驻提示）
// 2026-11-16 高风险管理页批：用户/角色/设备纳管/CMDB/AIOps 按后端路由权限点逐动作挂门。
const GATED = [
  ['APIKeysView.vue', 'apikey:write'],
  ['AutomationView.vue', 'automation:write'],
  ['BillingView.vue', 'billing:write'],
  ['GatewayRoutesView.vue', 'gateway:write'],
  ['SLOsView.vue', 'slo:write'],
  ['SchedulesView.vue', 'schedule:write'],
  ['ScriptsView.vue', 'script:write'],
  ['TicketsView.vue', 'ticket:write'],
  ['TrafficPoliciesView.vue', 'traffic:write'],
  ['WebhooksView.vue', 'webhook:write'],
  ['K8sClusterDeployView.vue', 'k8s:write'],
  ['K8sLifecycleView.vue', 'k8s:write'],
  ['K8sResourcesView.vue', 'k8s:write'],
  ['SystemOpsView.vue', 'system:write'],
  ['secrets/SecretsView.vue', 'secrets:write'],
  // 批 2：死信处置门（task:write）——同批补了 purge 的 ConfirmModal
  ['DeadLetterView.vue', 'task:write'],
  // 先行批（留档，勿删）
  ['FluxView.vue', 'flux:write'],
  ['InfraConsoleView.vue', 'task:write'],
  // 批 3：高风险管理页（权限点对齐后端路由）
  //   UsersView：POST/PUT user:write、DELETE user:delete、approve/reject user:approve
  //   （controlplane/auth_users.go:110/401/491/293/326）
  ['UsersView.vue', 'user:write', 'user:delete', 'user:approve'],
  //   RolesView：POST/PUT role:write、DELETE role:delete（controlplane/auth_roles.go:50/110）
  ['RolesView.vue', 'role:write', 'role:delete'],
  //   DevicesView：下发任务/纳管门为 device:delete 或 provision:execute（hasPerm 复合串 OR 语义），
  //   复合串必须整体登记——拆成两条会要求视图同时含两次独立 hasPerm 调用（server_devices.go:292-297）
  ['DevicesView.vue', 'device:delete/provision:execute'],
  //   CMDB 四页：CI CRUD/审批/采集/导入/属性模板统一 cmdb:write
  //   （internal/cmdb/handler.go:66-78、cmdb_approval.go:432、cmdb_collector.go:34）
  ['CMDBView.vue', 'cmdb:write'],
  ['CMDBChangesView.vue', 'cmdb:write'],
  ['CMDBCollectView.vue', 'cmdb:write'],
  ['CMDBAttrTemplatesView.vue', 'cmdb:write'],
  //   AIOpsView：POST analyze/remediate/remediate-nl/execute 门为 aiops:write（internal/aiops/handler.go:38-42）
  ['AIOpsView.vue', 'aiops:write'],
  // 批 2 已挂门（不许降级回 PENDING）：视图内已含 hasPerm + write-denied-hint
  ['AlertsView.vue', 'alert:ack/alert:silence'],
  ['K8sClustersView.vue', 'k8s:write/delete'],
  ['KnowledgeView.vue', 'knowledge:write'],
  ['MiddlewareDeployView.vue', 'middleware:write/execute'],
  ['NetworkDevicesView.vue', 'network:write'],
  ['WorkflowsView.vue', 'workflow:write']
]

// —— 欠账（matrix 实测：调用写端点但视图内无对应 hasPerm）。每挂完一个视图，把它从这里挪到 GATED。
// 顺序按 matrix 输出；括号内是该视图缺的权限点（供下一批直接用）。
// 注：PermissionsView 与 DeviceDetailView 经核对为纯只读页（仅 GET listPermissions / fetchMetrics），
// 无任何可变控件，故保留在 PENDING，不为了台账好看而添加无意义的 hasPerm。
const PENDING = [
  ['AIPromptsView.vue', 'ai:write'],
  ['AIRagView.vue', 'ai:write'],
  ['AgentView.vue', 'ai:write'],
  ['AutoProvisionView.vue', 'provision:execute'],
  ['AutoscalerView.vue', 'autoscaler:write'],
  ['BatchView.vue', 'task:write'],
  ['BotView.vue', 'bot:write'],
  ['CanaryView.vue', 'task:write'],
  ['ConfigHotpushView.vue', 'cmdb:write'],
  ['CostView.vue', 'cost:write'],
  ['DeploysView.vue', 'deploy:write'],
  ['DeviceDetailView.vue', 'device:delete/provision:execute'],
  ['DeviceDiscoveryView.vue', 'device:delete/provision:execute'],
  ['FederationDeploysView.vue', 'deploy:write'],
  ['FederationView.vue', 'federation:write'],
  ['GitOpsDashboardView.vue', 'argocd:write/flux:write'],
  ['NetworkDiagnoseView.vue', 'network:write'],
  ['NetworkTopologyView.vue', 'network:write'],
  ['OSOptimizeView.vue', 'os:write/execute'],
  ['OverviewView.vue', 'alert:ack/workflow:write'],
  ['PermissionsView.vue', 'role:write/user:write'],
  ['PlatformConfigView.vue', 'platform:write'],
  ['PluginView.vue', 'plugin:write'],
  ['PortalView.vue', 'portal:write']
]

// 先行两批用的是自有 testid（历史形态，保留）
const HINT_TESTID = {
  'FluxView.vue': 'flux-write-denied',
  'InfraConsoleView.vue': 'dispatch-denied-hint'
}

const read = (p) => fs.readFileSync(path.join(VIEWS, p), 'utf8')

describe('写按钮权限门台账（棘轮）', () => {
  it('GATED：已挂门的视图仍带 hasPerm + 常驻提示', () => {
    const bad = []
    for (const [view, ...perms] of GATED) {
      const src = read(view)
      for (const perm of perms) {
        if (!src.includes(`hasPerm('${perm}')`)) bad.push(`${view} 丢了 hasPerm('${perm}')`)
      }
      const hint = HINT_TESTID[view] || 'write-denied-hint'
      if (!src.includes(`data-testid="${hint}"`)) bad.push(`${view} 丢了常驻提示（${hint}）`)
    }
    expect(bad).toEqual([])
  })

  it('GATED：每个视图只登记一条，且至少声明一个权限点', () => {
    const seen = new Set()
    const bad = []
    for (const [view, ...perms] of GATED) {
      if (seen.has(view)) bad.push(`${view} 在 GATED 中重复登记`)
      seen.add(view)
      if (perms.length === 0) bad.push(`${view} 未声明任何权限点`)
    }
    expect(bad).toEqual([])
  })

  it('PENDING：欠账视图仍未被挂门（挂完必须挪出 PENDING）', () => {
    const bad = []
    for (const [view] of PENDING) {
      if (!fs.existsSync(path.join(VIEWS, view))) {
        bad.push(`${view} 已不存在——台账要同步`)
        continue
      }
      if (read(view).includes('hasPerm(')) {
        bad.push(`${view} 已挂门——请把它从 PENDING 挪到 GATED`)
      }
    }
    expect(bad).toEqual([])
  })

  it('共享双语键 common.write_denied 双语齐备', () => {
    for (const lang of ['zh', 'en']) {
      const d = JSON.parse(fs.readFileSync(path.join(LOCALES, lang, 'common.json'), 'utf8'))
      expect(d.common && d.common.write_denied, `${lang} 缺 common.write_denied`).toBeTruthy()
    }
  })

  it('欠账清单不得新增（新视图若调写端点必须先入台账）', () => {
    // 台账数量上限：只许减。挂完一个视图后，GATED+1、PENDING-1 ⇒ 总数不变。
    expect(GATED.length + PENDING.length).toBeLessThanOrEqual(56)
  })
})
