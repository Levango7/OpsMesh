// 页面骨架统一门禁（2026-10-27 布局专项）
//
// 规则：src/views 下每个视图必须用 PageShell 组件包裹（引用 '@/components/PageShell' 即视为合规）。
// 本清单 = 历史存量豁免（按迁移批次递减）；棘轮语义：
//   ① 非合规文件必须全在豁免清单里（新增页面不得绕过，新页面请直接用 PageShell）；
//   ② 清单里任何文件一旦合规就必须删条目，否则判红（防豁免变永久）；
//   ③ 每迁一批，条目只减不增。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

const VIEWS_DIR = join(__dirname, '..')

// 历史存量豁免（按迁移批次递减）
const EXEMPT = new Set([
  'AIChatView.vue',
  'APIKeysView.vue',
  'AgentView.vue',
  'AlertsView.vue',
  'AutoProvisionView.vue',
  'BatchView.vue',
  'BotView.vue',
  'CMDBAttrTemplatesView.vue',
  'CMDBChangesView.vue',
  'CMDBView.vue',
  'CloudAccountsView.vue',
  'CostView.vue',
  'DeadLetterView.vue',
  'DeploysView.vue',
  'DeviceDetailView.vue',
  'DeviceDiscoveryView.vue',
  'DevicesView.vue',
  'GPUView.vue',
  'GatewayRoutesView.vue',
  'HelmReposView.vue',
  'IncidentView.vue',
  'InfraConsoleView.vue',
  'K8sClusterDeployView.vue',
  'K8sClustersView.vue',
  'K8sLifecycleView.vue',
  'K8sResourcesView.vue',
  'KnowledgeView.vue',
  'LogsView.vue',
  'MiddlewareDeployView.vue',
  'NetworkDevicesView.vue',
  'NetworkDiagnoseView.vue',
  'NetworkTopologyView.vue',
  'NotifyChannelsView.vue',
  'OSOptimizeView.vue',
  'OverviewView.vue',
  'PermissionsView.vue',
  'PipelineView.vue',
  'PlatformConfigView.vue',
  'PortalView.vue',
  'QuotasView.vue',
  'RolesView.vue',
  'RunbookView.vue',
  'SLOsView.vue',
  'SchedulesView.vue',
  'ScriptsView.vue',
  'SystemOpsView.vue',
  'TasksView.vue',
  'TenantsView.vue',
  'TicketsView.vue',
  'TrafficPoliciesView.vue',
  'UsersView.vue',
  'WebhooksView.vue',
  'WorkflowsView.vue',
  'secrets/SecretsView.vue',
  // 全屏认证页：登录/注册/首登改密为全屏独立布局（app 壳之外），不适用 PageShell——保留豁免。
  'LoginView.vue',
  'RegisterView.vue',
  'ChangePasswordView.vue'
])

function walk(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

describe('页面骨架统一（PageShell）', () => {
  it('非合规视图 ⊆ 豁免清单，且豁免清单无过期条目（棘轮）', () => {
    const files = walk(VIEWS_DIR)
    const nonConforming = []
    for (const f of files) {
      const rel = relative(VIEWS_DIR, f).split(sep).join('/')
      const src = readFileSync(f, 'utf-8')
      if (!/components\/PageShell/.test(src)) nonConforming.push(rel)
    }
    // ① 新页面/未豁免页面必须合规
    const unlisted = nonConforming.filter((v) => !EXEMPT.has(v))
    expect(unlisted, '未用 PageShell 且不在豁免清单（新增页面请用 PageShell 包裹）').toEqual([])
    // ② 已合规的豁免条目必须删除（防豁免变永久）
    const stale = [...EXEMPT].filter((v) => !nonConforming.includes(v))
    expect(stale, '这些页面已合规，请从 EXEMPT 删除对应条目').toEqual([])
  })
})
