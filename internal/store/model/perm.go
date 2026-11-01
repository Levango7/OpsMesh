// perm.go 预置 RBAC 权限目录与角色派生（TD-61，原 internal/store/sql_rbac.go 搬迁）。
//
// 为什么在中性层：权限目录被 memory 与 sql 两个后端的 seedRBAC 共用（「单一来源，
// 杜绝两份定义漂移」），控制面 RBAC 闸经 store.RolePermissions() 消费，联邦/网关注入
// 也用它。留在任一后端的包内都会造成跨后端反向依赖（memory → sql 定义）。
//
// 字段导出：目录消费方分布在 memory / sqlstore 两个子包，匿名结构的私有字段
// 无法跨包读取，故具名为 PermSpec 且字段导出。
package model

import "strings"

// PermSpec 一个权限点定义（资源组 + 权限名 + 中文说明）。
type PermSpec struct {
	Group string // 资源组（device/task/alert/...）
	Name  string // 权限点（device:read）
	Desc  string // 中文说明（管理端展示）
}

// PermSpecs 默认权限定义（与两后端的 seedRBAC 共用；新增权限点时在此追加，
// 并同步 sql_rbac_catalog_test.go 的 handlerRequiredPerms 覆盖守护）。
var PermSpecs = []PermSpec{
	{"device", "device:read", "查看设备"},
	{"device", "device:write", "操作设备"},
	{"device", "device:delete", "退役设备"},
	{"task", "task:read", "查看任务"},
	{"task", "task:write", "下发任务"},
	{"task", "task:cancel", "取消任务"},
	{"alert", "alert:read", "查看告警"},
	{"alert", "alert:ack", "确认告警"},
	{"alert", "alert:silence", "静默告警"},
	{"cmdb", "cmdb:read", "查看配置项"},
	{"cmdb", "cmdb:write", "编辑配置项"},
	// cmdb:approve CI 变更审批（G1/SEC-5）：cmdb_approval.go 的 approve/reject 端点经
	// requireProd 校验该权限点，但此前未在权限目录定义——已建库的 SQLStore 走 INSERT
	// IGNORE 幂等补种；operator 角色按 RolePermissions 派生规则（仅 read/write/execute）
	// 不会获得审批权，审批仅 admin 可用（最小权限：CI 变更审批属敏感操作）。
	{"cmdb", "cmdb:approve", "审批配置项变更"},
	{"deploy", "deploy:read", "查看部署"},
	{"deploy", "deploy:write", "执行部署"},
	{"workflow", "workflow:read", "查看工作流"},
	{"workflow", "workflow:write", "编辑工作流"},
	{"log", "log:read", "查看日志"},
	{"audit", "audit:read", "查看审计"},
	{"user", "user:read", "查看用户"},
	{"user", "user:write", "编辑用户"},
	{"user", "user:delete", "删除用户"},
	{"user", "user:approve", "审批用户注册"},
	{"role", "role:read", "查看角色"},
	{"role", "role:write", "编辑角色"},
	{"role", "role:delete", "删除角色"},
	{"federation", "federation:read", "查看联邦"},
	{"federation", "federation:write", "编辑联邦"},
	{"os", "os:read", "查看OS优化模板"},
	{"os", "os:execute", "执行OS优化"},
	{"middleware", "middleware:read", "查看中间件模板"},
	{"middleware", "middleware:execute", "部署/卸载中间件"},
	{"provision", "provision:execute", "自动纳管/纳管设备"},
	{"k8s", "k8s:read", "查看K8s集群"},
	{"k8s", "k8s:write", "管理K8s集群"},
	{"k8s", "k8s:delete", "删除K8s集群"},
	{"ticket", "ticket:read", "查看工单"},
	{"ticket", "ticket:write", "编辑工单"},
	{"slo", "slo:read", "查看SLO"},
	{"slo", "slo:write", "编辑SLO"},
	{"slo", "slo:delete", "删除SLO"},
	{"traffic", "traffic:read", "查看流量策略"},
	{"traffic", "traffic:write", "编辑流量策略"},
	{"pipeline", "pipeline:read", "查看流水线"},
	{"pipeline", "pipeline:write", "编辑流水线"},
	{"argocd", "argocd:read", "查看ArgoCD应用"},
	{"argocd", "argocd:write", "编辑ArgoCD应用"},
	{"compliance", "compliance:read", "查看合规报告"},
	{"compliance", "compliance:write", "执行合规扫描"},
	{"ha", "ha:read", "查看HA状态"},
	{"ha", "ha:write", "手动切换leader"},
	{"backup", "backup:read", "查看备份记录"},
	{"backup", "backup:write", "创建/恢复/删除备份"},
	{"network", "network:read", "查看网络设备"},
	{"network", "network:write", "管理网络设备"},
	{"automation", "automation:read", "查看自动化规则"},
	{"automation", "automation:write", "管理自动化规则"},
	{"webhook", "webhook:read", "查看 Webhook"},
	{"webhook", "webhook:write", "管理 Webhook"},
	{"script", "script:read", "查看自定义脚本"},
	{"script", "script:write", "管理自定义脚本"},
	{"gateway", "gateway:read", "查看 API 网关"},
	{"gateway", "gateway:write", "管理 API 网关"},
	{"tenant", "tenant:read", "查看租户"},
	{"tenant", "tenant:write", "管理租户"},
	{"apikey", "apikey:read", "查看 API Key"},
	{"apikey", "apikey:write", "管理 API Key"},
	{"plugin", "plugin:read", "查看插件"},
	{"plugin", "plugin:write", "管理插件"},
	{"billing", "billing:read", "查看计费"},
	{"billing", "billing:write", "管理计费"},
	{"platform", "platform:read", "查看平台配置"},
	{"platform", "platform:write", "管理平台配置"},
	// M13 六域接线新增（聚合代理/ChatOps 命令台的权限点）：
	// 前端 router requirePerm 已引用（nav.gpu/bot/runbooks/incidents/autoscaler/portal），
	// 此前缺目录——requirePermission 对 admin 不受影响（admin=全量），但角色无法被
	// 显式授予、权限管理页不可见。补种后 SQLStore INSERT IGNORE 幂等/MemoryStore
	// 构造期全量重建；viewer/operator 派生规则自动生效（viewer=全部 *:read）。
	{"gpu", "gpu:read", "查看GPU资源"},
	{"gpu", "gpu:write", "管理GPU工作负载/模型"},
	{"bot", "bot:read", "查看ChatOps命令台"},
	{"bot", "bot:write", "执行ChatOps命令"},
	{"runbook", "runbook:read", "查看Runbook"},
	{"runbook", "runbook:write", "编辑/执行Runbook"},
	{"incident", "incident:read", "查看事件"},
	{"incident", "incident:write", "编辑事件"},
	{"autoscaler", "autoscaler:read", "查看扩缩容规则"},
	{"autoscaler", "autoscaler:write", "编辑扩缩容规则"},
	{"portal", "portal:read", "查看服务门户"},
	{"portal", "portal:write", "审批门户请求"},
	// P1-6 可支撑性：配置转储（/api/v1/admin/config）与诊断包（/api/v1/admin/diagnostics）。
	// 刻意**不以 `:read` 结尾**——派生规则会把所有 `*:read` 自动授予 viewer，而配置转储
	// 与诊断包含内部拓扑与配置细节，只应给 admin（admin 自动获得全部权限点）。
	{"diagnostics", "diagnostics:dump", "导出脱敏配置转储与诊断包（仅 admin）"},
	// 运行期改日志级别：动作名刻意用 :execute 而非 :read/:dump，配合 operatorGroups 里的
	// "diagnostics" 让 operator 也能提级别（现场排障的人不该为此找管理员要 token），
	// 同时 diagnostics:dump（含内部拓扑）仍严格 admin-only。
	{"diagnostics", "diagnostics:execute", "调整进程日志级别"},
	// 2026-09-29 目录补齐（15 项）：以下权限点被 controlplane handler 的 requireProd
	// 校验引用，但从未进入权限目录——RolePermissions 派生集不含它们，任何内置角色
	//（含 admin）都无法持有，对应端点对全部角色恒 403。sim 实测证据：admin 携合法
	// 会话 token 调 GET /api/v1/schedules 与 GET /api/v1/approval/flows 均 403
	//（permission denied: schedule:read / approval:read）；本地单体与 task-svc
	// 平行代理路径行为一致（双轨等价，代理映射如实镜像本地语义）。
	// 与 cmdb:approve（见上）同类缺陷的批量修复；机制同 M13 六域补种（INSERT IGNORE
	// 幂等补种 + 角色快照并集回填，升级部署下次启动自动生效）。
	// 派生效应（如实记录，防误判）：viewer（全部 *:read）自动获得
	// approval/helm/quota/schedule/secrets 的 read；operator（operatorGroups ×
	// read/write/execute）自动获得 alert:write、middleware:write、os:write。
	// approval:approve 与 task:approve 不属派生动作集（approve ∉ {read,write,execute}），
	// 与 cmdb:approve 同策：仅 admin 可用（最小权限）。
	{"alert", "alert:write", "编辑告警规则"},
	{"approval", "approval:read", "查看审批流程与请求"},
	{"approval", "approval:write", "编辑审批流程/请求"},
	{"approval", "approval:approve", "审批通过/驳回"},
	{"helm", "helm:read", "查看Helm应用商店"},
	{"helm", "helm:write", "管理Helm应用"},
	{"middleware", "middleware:write", "编辑中间件模板"},
	{"os", "os:write", "编辑OS优化模板"},
	{"quota", "quota:read", "查看租户配额"},
	{"quota", "quota:write", "管理租户配额"},
	{"schedule", "schedule:read", "查看定时任务"},
	{"schedule", "schedule:write", "管理定时任务"},
	{"secrets", "secrets:read", "查看密钥服务配置"},
	{"secrets", "secrets:write", "测试/管理密钥服务"},
	{"task", "task:approve", "审批高风险任务"},
	// 2026-10-22 云账号权限族补齐：前端路由 /cloud-accounts 以 requirePerm: 'cloud:read' 把关，
	// 但目录里此前**没有 cloud 族**——admin 的权限集由 PermSpecs 派生（RolePermissions），
	// admin 拿不到 cloud:read ⇒ 该路由对全部角色（含 admin）恒不可达（死路由）。
	// 机制与 M13 六域补种 / cmdb:approve 同策：目录补齐后 SQLStore INSERT IGNORE 幂等补种，
	// 预置角色权限快照按并集回填（只加不减），升级部署下次启动自动生效。
	// 派生效应（如实记录，防误判）：viewer 随「全部 *:read」自动获得 cloud:read；
	// operator **不获得**（cloud 组不在 operatorGroups / operatorReadOnlyGroups，
	// 云账号写权限是否下放 operator 属产品语义，未决前按最小权限处理）。
	{"cloud", "cloud:read", "查看云账号"},
	{"cloud", "cloud:write", "管理云账号"},
	// 系统级写权限：中间件部署/数据库操作等高危运维端点的 requireProd 校验引用此权限点，
	// 此前未在目录定义导致 admin 等所有角色恒 403。补种后 RolePermissions 派生生效，
	// admin 角色自动获得（由 RolePermissions 派生集合），升级部署下次启动自动生效。
	{"system", "system:read", "系统级读权限（系统状态/配置查询等）"},
	{"system", "system:write", "系统级写权限（中间件部署/数据库操作等高危运维）"},
	// AI 域补种（2026-11-01，用户指派「修 A：ai/agent 路由接鉴权」）：
	// /api/v1/ai/agent/* 六条路由此前由 RegisterAgentRoutes 裸注册，包内既无
	// requireProd 类校验、也不在控制面的 G1 注入清单里 ⇒ 未登录即可驱动 Agent；
	// 且 HandleExecute 把请求体里的 userId/tenantId 原样塞进 AgentContext。
	// ai:read 进 operatorReadOnlyGroups（否则 operator 连读都没有，低于 viewer——
	// 与 gpu/runbook/incident/k8s 同一类层级倒挂）；ai:write **不**下放 operator：
	// Agent 编排可驱动 execute_command（远程 shell），其工具侧权限校验（CheckPermission
	// 恒 true 的空壳）尚未收口，写权限先按最小权限只给 admin。
	{"ai", "ai:read", "查看 AI Agent、意图分类与会话记忆"},
	{"ai", "ai:write", "执行 AI Agent 编排/清理会话记忆"},
	// AI 域同族补齐（2026-11-02，用户指派「同族裸路由全部收口」）。三族都是同一形状：
	// handler 侧引用了权限点，目录里没有 ⇒ 任何内置角色（含 admin）都拿不到，端点对
	// 全部角色恒 403（与 2026-09-29 的 15 项缺口同类；守护测试的手工清单当时看不见它们，
	// 因为那三包用的是自己的 authorize 包装而不是 requireProd 字面量）。
	//
	// aiops 族另有第二层缺口：SetAuthorize 在生产代码里从未被调用（authz==nil 时
	// authorize() 原样放行），所以 /api/v1/aiops/* 六条此前是**匿名可达**——
	// 含 POST /aiops/execute（把修复方案直接下发成 agent 任务）。接线与补目录必须同批，
	// 只做其一要么仍是匿名（不接线），要么变成对所有人 403（不补目录）。
	{"aiops", "aiops:read", "查看 AIOps 分析结果、引擎状态与执行历史"},
	{"aiops", "aiops:write", "触发 AIOps 根因分析/修复方案生成/自动执行"},
	{"knowledge", "knowledge:read", "查看知识库文档、检索结果与同步配置"},
	{"knowledge", "knowledge:write", "同步/导入/删除知识库文档与同步配置"},
	{"cost", "cost:read", "查看成本报表、预算与模型路由"},
	{"cost", "cost:write", "设置预算、改模型路由、生成成本报表"},
	// flux 族（2026-11-02，由新加的源码守护 TestRolePermissions_CoversAuthorizeCallSites
	// 扫出，非人工盘点）：internal/controlplane/flux.go 的 5 个 handler 全部
	// requirePermission(w, r, "flux:read"/"flux:write")，但目录里没有 flux 族 ⇒
	// /api/v1/flux/apps* 对含 admin 在内的全部角色恒 403，且前端路由
	// web/enterprise/src/router/index.js:164 的 requirePerm: 'flux:read' 让 /flux
	// 页面谁都进不去（与 2026-10-22 的 cloud 族同一形态的死路由）。
	// flux:write（含 flux reconcile 真实下发）是否下放 operator 待 owner 定夺——
	// deploy 组已在 operatorGroups 里，GitOps 同步与之同域，此处先按最小权限只授 read。
	{"flux", "flux:read", "查看 Flux GitOps 应用与同步状态"},
	{"flux", "flux:write", "创建/更新/删除 Flux 应用并触发同步"},
}

// RolePermissions 返回预置角色名→权限集合映射，与 seedRBAC 的角色定义保持一致。
// 供控制面网关注入/联邦转发身份（携带角色名而非权限字符串）做产品级 RBAC 校验。
// 这是角色权限划分的单一来源：seedRBAC 与 RBAC 闸都从此派生，杜绝定义漂移。
func RolePermissions() map[string][]string {
	allPerms := make([]string, 0, len(PermSpecs))
	for _, ps := range PermSpecs {
		allPerms = append(allPerms, ps.Name)
	}
	viewerPerms := make([]string, 0)
	operatorPerms := make([]string, 0)
	// operatorGroups：运维角色可操作的资源组（read + write/execute）。
	// 注意：k8s 不在其中 —— K8s 集群管理仅 admin 可写/删，operator/viewer 仅读（严谨最小权限）。
	operatorGroups := map[string]bool{
		"device": true, "task": true, "alert": true, "cmdb": true,
		"deploy": true, "workflow": true, "log": true, "audit": true,
		"os": true, "middleware": true, "provision": true,
		// diagnostics 组在此 = operator 可执行"调级别"这类动作型权限点；
		// 但 diagnostics:dump 的动作名是 dump（不属 read/write/execute），故 operator 拿不到。
		"diagnostics": true,
	}
	// operatorReadOnlyGroups：只授予 read 的资源组（operator 持有其 *:read，但无 write/execute）。
	// 背景（TD-60 §5.9 缺陷 ④，2026-09-30 复核）：gpu/runbook/incident 三域此前不在
	// operatorGroups 里，而 operator 派生**不等于**全部 *:read（只有 viewer 才派生全部 read），
	// 于是 operator 连 read 都没有 —— 角色层级倒挂（operator 反而低于 viewer）。
	// 前端路由门正是 requirePerm: 'gpu:read'/'runbook:read'/'incident:read'
	// （web/enterprise/src/router/index.js:103/109/112），缺 read 即 403。
	// 刻意**只补 read、不补 write**：gpu:write（管理工作负载/模型）、runbook:write（编辑/执行 Runbook）、
	// incident:write（编辑事件）是否下放 operator 属产品语义，需另行确认，先按最小权限处理。
	// k8s 同理（见上："K8s 集群管理仅 admin 可写/删，operator/viewer 仅读"）——派生循环此前
	// 也只把 k8s:read 给了 viewer，与本注释自述矛盾，一并按只读补齐。
	operatorReadOnlyGroups := map[string]bool{
		"gpu": true, "runbook": true, "incident": true, "k8s": true,
		// ai 只给 read：Agent 编排能驱动 execute_command（远程 shell），
		// write 先按最小权限只给 admin（工具侧 CheckPermission 已于 2026-11-01
		// 改 fail-closed，权限集来自已验签角色；下放 operator 属产品语义，另行裁决）。
		"ai": true,
		// 2026-11-02 同族三组同样只补 read：viewer 派生全部 *:read，若这三族的 read
		// 不给 operator 就会重现「operator 低于 viewer」的层级倒挂；而
		// aiops:write（自动执行修复）、knowledge:write（改文档库）、cost:write（改预算/路由）
		// 是否下放属产品语义，未决前按最小权限留给 admin。
		"aiops": true, "knowledge": true, "cost": true, "flux": true,
	}
	// operatorGroupActions 是 operator 在 operatorGroups 各组内拿到的动作集。
	// 2026-10-14 §3.12（用户裁定：RBAC 矩阵细化，不上基于归属的场景授权）：
	// 在 read/write/execute 之外追加 **cancel/ack/silence 三个作业动词**。
	// 背景（演练 §3.12，三个人设独立复现）：task:cancel 与 alert:ack/alert:silence
	// 所在的组（task/alert）本就在 operatorGroups 里，但动作名不属于
	// read/write/execute ⇒ 派生循环静默丢弃 ⇒ operator「建得了任务却取消不了、
	// 告警确认与静默全 403」——值班角色能开不能关。
	// 为什么不是按场景（归属）授权：本平面的授权模型只有角色矩阵一份
	// （RolePermissions 是 seedRBAC 与 RBAC 闸的共同单一来源），不存在任何
	// subject-object-relation 概念；再造一套"只能取消自己建的任务"等于引入第二套
	// 授权机制与第二套语义（代发任务归属谁？交接班时上一班的失控任务谁停？），
	// 而交接场景恰恰要求下一班能停上一班的任务。归属维度授权若要建设，另立设计项。
	// 刻意**不含** approve（cmdb:approve / approval:approve / task:approve 仍
	// admin-only——审批是管理动作不是作业动词）与 delete（device:delete /
	// role:delete / user:delete / k8s:delete 等毁灭性动作仍 admin-only）。
	operatorGroupActions := map[string]bool{
		"read": true, "write": true, "execute": true,
		"cancel": true, "ack": true, "silence": true,
	}
	for _, p := range allPerms {
		idx := strings.Index(p, ":")
		if idx <= 0 {
			continue
		}
		group, action := p[:idx], p[idx+1:]
		if strings.HasSuffix(p, ":read") {
			viewerPerms = append(viewerPerms, p)
		}
		if (operatorGroups[group] && operatorGroupActions[action]) ||
			(operatorReadOnlyGroups[group] && action == "read") {
			operatorPerms = append(operatorPerms, p)
		}
	}
	return map[string][]string{
		"admin":    allPerms,
		"operator": operatorPerms,
		"viewer":   viewerPerms,
	}
}
