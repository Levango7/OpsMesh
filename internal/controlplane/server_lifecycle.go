// server_lifecycle.go — 控制面进程生命周期：Start 启动 HTTP/gRPC/metrics 三监听并优雅退出。
package controlplane

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Levango7/OpsMesh/internal/ai/agent"
	aigateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/controlplane/svcproxy"
	"github.com/Levango7/OpsMesh/internal/cost"
	"github.com/Levango7/OpsMesh/internal/k8slifecycle"
	"github.com/Levango7/OpsMesh/internal/knowledge"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/otelx"
)

// Start 启动 HTTP(B/S)、gRPC(9090)、metrics(9091) 三个监听，并在收到 SIGTERM/SIGINT 时优雅退出。
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/assets/", s.handleAsset) // 前端静态资源（独立化：web/assets/*）
	// 企业版前端（P0-3）：/enterprise/ 静态外壳 + SPA 回退；/enterprise/assets/ 带哈希资源。
	// 未内置构建产物的二进制会返回「未内置」说明页（不是 404），且个人版入口自动隐藏。
	mux.HandleFunc("/enterprise/", s.handleEnterpriseUI)
	mux.HandleFunc("/enterprise/assets/", s.handleEnterpriseAsset)
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/me", s.handleMe)
	// 商业授权状态（Open-Core）：社区版与授权版都能查，但需登录——
	// 授权态含客户名与设备上限，属商务信息。端点本身不受 requireEnterpriseGate 约束，
	// 否则未授权用户恰好无法查"为什么没授权"。
	mux.HandleFunc("/api/v1/license", s.handleLicenseStatus)
	s.registerTaskRoutes(mux)
	mux.HandleFunc("/healthz", s.handleHealthz) // K8s liveness 探针（+ 深度检查）
	mux.HandleFunc("/readyz", s.handleReadyz)   // K8s readiness 探针（新增）
	// P1-6 可支撑性：构建信息（无鉴权，仅版本/构建/运行时，供现场排障核对版本）。
	mux.HandleFunc("/version", s.handleVersion)
	// P1-6 可支撑性：配置转储与诊断包（需 diagnostics:dump 权限，RBAC 派生规则下仅 admin）。
	mux.HandleFunc("/api/v1/admin/config", s.handleAdminConfig)
	// 运行期日志级别开关（diagnostics:execute，operator 亦可）：避免"为开 debug 而重启"
	// 把要观察的状态本身打乱。
	mux.HandleFunc("/api/v1/admin/loglevel", s.handleAdminLogLevel)
	mux.HandleFunc("/api/v1/admin/diagnostics", s.handleAdminDiagnostics)
	// 存储层被吞掉的写/读错误（Store 接口不返回 error，失败此前不可见；见 internal/store/failures.go）。
	mux.HandleFunc("/api/v1/admin/store-failures", s.handleAdminStoreFailures)
	// TD-60 切流可见性：域 → 实际后端 → 启用状态 → 自检结论。
	mux.HandleFunc("/api/v1/admin/service-routing", s.handleServiceRouting)
	// TD-60 逐域裁决取数：域 → 自启动以来真实请求数（按归一化路径前缀聚合）。
	mux.HandleFunc("/api/v1/admin/service-traffic", s.handleAdminServiceTraffic)
	// P1-6 可支撑性：pprof（默认关闭；开启后仍受 --metrics-allow-cidr 白名单约束）。
	s.registerPprof(mux)
	mux.HandleFunc("/api/v1/audits", s.handleAudits) // GET 审计检索
	// /api/v1/tasks/batch：按方法拆分注册（2026-10-21）。
	// 此前是**无方法限定**的裸字面量注册，与 registerTaskRoutes 的 "GET /api/v1/tasks/{id}"
	// 冲突——Go 1.22 ServeMux 判定「字面量路径更具体，但方法集更宽」时无法裁定优先级，
	// Start() 第二次注册即 panic（进程起不来）。按方法显式拆分后：
	//   - GET 仍进 handler 并由其返回 405（observable 行为与拆分前逐字一致）；
	//   - POST 走原创建逻辑；
	//   - 与 "GET /api/v1/tasks/{id}" 同段数字面量+同方法 ⇒ 更具体者胜，不冲突。
	mux.HandleFunc("GET /api/v1/tasks/batch", s.handleBatchCreateTasks)  // GET：handler 内 405（保持既有行为）
	mux.HandleFunc("POST /api/v1/tasks/batch", s.handleBatchCreateTasks) // POST 批量下发
	mux.HandleFunc("/api/v1/devices/", s.handleDeviceRouting)            // 子路径：{id} DELETE 退役、{id}/provision
	mux.HandleFunc("/api/v1/alerts", s.handleAlerts)                     // GET 活跃告警（M7）
	mux.HandleFunc("/api/v1/alerts/", s.handleAlertRouting)              // 子路径：{id}/ack、{id}/silence
	mux.HandleFunc("/api/v1/events/stream", s.handleEventsStream)        // SSE 实时推送（替代 5s 轮询）
	// 自动纳管：install.sh 分发脚本 + agent 二进制分发 + 自动纳管触发
	mux.HandleFunc("/install.sh", s.handleInstallSh)
	mux.HandleFunc("/bin/opsmesh-agent", s.handleServeAgent)
	mux.HandleFunc("/api/v1/provision/auto", s.handleAutoProvision)
	// CMDB（Phase 1）：按持久化后端选择 SQL 或 Memory 实现。
	s.cmdbHandler.RegisterRoutes(mux)
	// M6 日志检索：GET/POST /api/v1/logs（租户隔离由 authctx 注入）。
	s.logHandler.RegisterRoutes(mux)
	// M3 部署中心：POST/GET /api/v1/deploys（租户隔离由 authctx 注入）。
	// 修复 3：用 paginate.PaginateJSONHandler 包装 GET 列表做分页（向后兼容）。
	deployMux := http.NewServeMux()
	s.deployHandler.RegisterRoutes(deployMux)
	mux.Handle("/api/v1/deploys", paginate.PaginateJSONHandler(deployMux))
	mux.Handle("/api/v1/deploys/", deployMux)
	// M5 作业编排中心：POST/GET /api/v1/workflows（租户隔离由 authctx 注入）。
	// 修复 3：同上分页包装。
	orchMux := http.NewServeMux()
	s.orchHandler.RegisterRoutes(orchMux)
	mux.Handle("/api/v1/workflows", paginate.PaginateJSONHandler(orchMux))
	mux.Handle("/api/v1/workflows/", orchMux)
	// AIOps：POST /api/v1/aiops/analyze|remediate|remediate-nl|execute，GET /api/v1/aiops/status|executions。
	s.aiopsHandler.RegisterRoutes(mux)
	if s.aiGatewayHandler != nil {
		aigateway.RegisterAIGatewayRoutes(mux, s.aiGatewayHandler)
	}
	if s.agentHandler != nil {
		agent.RegisterAgentRoutes(mux, s.agentHandler)
	}
	if s.knowledgeHandler != nil {
		knowledge.RegisterKnowledgeRoutes(mux, s.knowledgeHandler)
	}
	if s.costHandler != nil {
		cost.RegisterCostRoutes(mux, s.costHandler)
	}
	if s.k8sLifecycleHandler != nil {
		k8slifecycle.RegisterK8sLifecycleRoutes(mux, s.k8sLifecycleHandler)
	}
	// M13 微服务聚合代理：gpu/runbook/incident/autoscaler/portal 五域转发到
	// services/* 独立进程（规则表见 service_proxy.go；bot 域由 bot_bridge.go 提供
	// 聚合 handler，契约与前端 src/api/bot.js 对齐）。鉴权在代理层统一完成。
	//
	// 启动期两道闸：
	//  1. 自环自检：转发后端指向控制面自身时在**请求侧**已硬拦为 503（见
	//     handleServiceProxy），这里只负责把问题在启动日志里说清楚。
	//     不选择「拒绝启动」：控制面单体还承载鉴权/任务/设备等核心流量，
	//     为一个域的地址配置错误拖停全站，可用性代价远大于收益。
	//  2. 路由开关 OPSMESH_SERVICE_PROXY：被停用的域**不注册**代理路由，
	//     请求自然回落到单体同名 handler——这才是可逆的切流。
	disabled, switchErr := svcproxy.DisabledDomains()
	if switchErr != nil {
		// 开关写错不等于该切换：按全部启用继续启动，但必须留下醒目痕迹。
		logx.Warn(context.Background(), "微服务转发开关取值非法，已忽略并按「全部转发」处理",
			"env", svcproxy.EnvKey, "err", switchErr)
		disabled = map[string]bool{}
	} else if len(disabled) > 0 {
		logx.Info(context.Background(), "微服务转发已按开关停用部分域（回落到单体本地实现）",
			"env", svcproxy.EnvKey, "disabled", fmt.Sprint(disabled))
	}
	if problems := svcproxy.ValidateTargets(s.httpPort, s.grpcPort, s.metricsPort); len(problems) > 0 {
		for _, p := range problems {
			logx.Error(context.Background(), "微服务转发配置有误，该域将被 503 隔离", nil, "detail", p)
		}
	}
	for i := range svcproxy.Rules {
		if svcproxy.Rules[i].PublicPrefix == "" {
			continue
		}
		if disabled[svcproxy.Rules[i].Domain] {
			continue
		}
		mux.HandleFunc(svcproxy.Rules[i].PublicPrefix, s.handleServiceProxy)
		mux.HandleFunc(svcproxy.Rules[i].PublicPrefix+"/", s.handleServiceProxy)
	}
	// device 域（TD-60 D1/D3 后 device-svc REST 网关接线）：/api/v1/device-svc/{devices,
	// agents,cmdb,discovery} 转发（规则见 service_proxy.go svcproxy.DeviceExtras；代理层
	// 鉴权后注入 X-Tenant-ID 头——device-svc 网关消费租户上下文，与五域不同）。
	// 前缀带 device-svc 域名：controlplane 本地已有 /api/v1/devices 等同名 handler
	//（server_lifecycle.go:25-34），同 mux 重复注册会 panic；双轨期新旧并存。
	for i := range svcproxy.DeviceExtras {
		if disabled[svcproxy.DeviceExtras[i].Domain] {
			continue
		}
		mux.HandleFunc(svcproxy.DeviceExtras[i].PublicPrefix, s.handleServiceProxy)
		mux.HandleFunc(svcproxy.DeviceExtras[i].PublicPrefix+"/", s.handleServiceProxy)
	}
	// task 域（TD-60 阶段 2「三域接通」：task-svc REST 网关接线）：/api/v1/task-svc/*
	// 转发（规则见 service_proxy.go svcproxy.TaskExtras；权限按方法+路径分级对齐单体
	// 本地要求，代理层鉴权后注入 X-Tenant-ID 头——task-svc 网关消费租户上下文）。
	// 前缀带 task-svc 域名：controlplane 本地已有 /api/v1/tasks、/api/v1/schedules、
	// /api/v1/approval/* 等同名 handler，同 mux 重复注册会 panic；双轨期新旧并存。
	for i := range svcproxy.TaskExtras {
		if disabled[svcproxy.TaskExtras[i].Domain] {
			continue
		}
		mux.HandleFunc(svcproxy.TaskExtras[i].PublicPrefix, s.handleServiceProxy)
		mux.HandleFunc(svcproxy.TaskExtras[i].PublicPrefix+"/", s.handleServiceProxy)
	}
	// ChatOps Web 命令台（bot_bridge.go）：/opsmesh status|devices|alerts|ack|metrics|help，
	// 历史进程级内存。bot-svc（IM webhook 入口）已于 2026-09-29 删除，本命令台为 bot 域唯一实现。
	mux.HandleFunc("/api/v1/bot/command", s.handleBotCommand)
	mux.HandleFunc("/api/v1/bot/history", s.handleBotHistory)
	mux.HandleFunc("/api/v1/bot/platforms", s.handleBotPlatforms)
	mux.HandleFunc("/api/v1/bot/quick-commands", s.handleBotQuickCommands)
	// 控制面联邦：仅当配置了 --federation-peers 时注册联邦 API。
	// 未启用时这些端点返回 404（mux 未注册），保证向后兼容。
	if s.fed != nil {
		mux.HandleFunc("/api/v1/federation/peers", s.handleFederationPeers)
		mux.HandleFunc("/api/v1/federation/forward/task", s.handleFederationForwardTask)
		mux.HandleFunc("/api/v1/federation/devices", s.handleFederationDevices)
		mux.HandleFunc("/api/v1/federation/tasks/", s.handleFederationTaskRouting) // 子路径：{taskID} GET 查询任务状态
	}
	// 用户中心（RBAC + JWT）：注册/登录/查询当前用户 + 用户/角色/权限 CRUD。
	// 与网关注入身份模式并存：用户中心用于 B/S 仪表盘登录，网关注入用于 agent gRPC 通道。
	mux.HandleFunc("/api/v1/auth/register", s.handleAuthRegister)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/me", s.handleAuthMe)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)                  // 登出清 HttpOnly Cookie
	mux.HandleFunc("/api/v1/auth/refresh", s.handleAuthRefresh)                // 双 Cookie：rt 静默换新 at+rt（旋转）
	mux.HandleFunc("/api/v1/auth/change-password", s.handleAuthChangePassword) // 安全债：预置弱口令强制改密
	mux.HandleFunc("/api/v1/users", s.handleUsers)
	mux.HandleFunc("/api/v1/users/", s.handleUserRouting)
	mux.HandleFunc("/api/v1/roles", s.handleRoles)
	mux.HandleFunc("/api/v1/roles/", s.handleRoleRouting)
	mux.HandleFunc("/api/v1/permissions", s.handlePermissions)
	// OS 基础环境优化：预置模板列表 + 详情 + 在指定 agent 上执行。
	mux.HandleFunc("/api/v1/os-templates", s.handleListOSTemplates)
	mux.HandleFunc("/api/v1/os-templates/", s.handleOSTemplateRouting) // 子路径：{id} 和 {id}/execute
	// 系统运维操作：用户/组/防火墙的 shell 模板执行（查询走 /api/v1/devices/{id}/system）。
	mux.HandleFunc("/api/v1/system/users", s.handleSystemUsers)                    // POST 创建 / DELETE 删除用户
	mux.HandleFunc("/api/v1/system/groups", s.handleSystemGroups)                  // POST 创建 / DELETE 删除组
	mux.HandleFunc("/api/v1/system/firewall", s.handleSystemFirewall)              // POST 添加 / DELETE 删除规则
	mux.HandleFunc("GET /api/v1/system/firewall/rules", s.handleListFirewallRules) // GET 防火墙规则结构化查询
	// systemctl 服务管理 + journal 日志查询。
	mux.HandleFunc("/api/v1/system/services", s.handleSystemServices) // POST 服务操作（start/stop/restart/...）
	mux.HandleFunc("/api/v1/system/journal", s.handleSystemJournal)   // POST journalctl 查询下发
	// 包管理（yum/apt）：POST 安装/升级，DELETE 卸载。
	mux.HandleFunc("/api/v1/system/packages", s.handleSystemPackages) // POST install/upgrade / DELETE remove
	// 中间件运维：Kafka Topic 等固定路径优先注册（ServeMux 精确匹配优先于前缀匹配）。
	mux.HandleFunc("/api/v1/middleware/kafka/topic", s.handleKafkaTopic) // POST Kafka Topic 管理
	// 中间件运维泛路由：GET /api/v1/middleware/{deviceID} / POST /api/v1/middleware/{type}/{action}。
	s.registerMiddlewareRoutes(mux)
	s.registerKafkaRoutes(mux)
	s.registerKafkaExtendedRoutes(mux)
	s.registerKafkaAdvancedRoutes(mux)
	s.registerKafkaScaleRoutes(mux)
	s.registerKafkaFinalRoutes(mux)
	s.registerRabbitMQExtendedRoutes(mux)
	s.registerRabbitMQFinalRoutes(mux)
	s.registerRocketMQExtendedRoutes(mux)
	s.registerRocketMQFinalRoutes(mux)
	s.registerActiveMQExtendedRoutes(mux)
	s.registerActiveMQFinalRoutes(mux)
	s.registerPulsarRoutes(mux)
	s.registerPulsarExtendedRoutes(mux)
	s.registerDataRoutes(mux)
	// 中间件部署：预置模板列表 + 详情 + 在指定 agent 上部署 + 已部署实例查询 + 卸载。
	mux.HandleFunc("/api/v1/middleware-templates", s.handleMiddlewareTemplates)
	mux.HandleFunc("/api/v1/middleware-templates/", s.handleMiddlewareTemplateDetail) // 子路径：{id} 和 {id}/deploy
	mux.HandleFunc("/api/v1/middleware-instances", s.handleMiddlewareInstances)
	mux.HandleFunc("/api/v1/middleware-instances/", s.handleMiddlewareInstanceRouting) // 子路径：{id}/uninstall
	// 数据库运维：PostgreSQL/Oracle/TiDB/OceanBase（固定路径优先，再泛路由 /api/v1/db/）。
	s.registerDBRoutes(mux)
	// 负载均衡运维：HaProxy/LVS/keepalived（固定路径优先，再泛路由 /api/v1/lb/）。
	s.registerLBRoutes(mux)
	// 对象存储运维：Amazon S3 / Aliyun OSS / MinIO（固定路径优先，再泛路由 /api/v1/storage/）。
	s.registerStorageRoutes(mux)
	s.RegisterFastDFSRoutes(mux)
	s.registerDistFSRoutes(mux)
	s.registerCacheRoutes(mux)
	// 镜像仓库运维：Harbor（固定路径优先，再泛路由 /api/v1/harbor/）。
	s.registerHarborRoutes(mux)
	// 环境依赖优化：Java/node.js/Python/Go + fd-limit/IPVS（固定路径优先，再泛路由 /api/v1/env/）。
	s.registerEnvRoutes(mux)
	// 监控系统兼容：Zabbix/夜莺/SkyWalking（固定路径优先，再泛路由 /api/v1/monitor/）。
	s.registerMonitorRoutes(mux)
	s.registerGPUExtendedRoutes(mux)
	s.registerAscendRoutes(mux)
	s.registerELKRoutes(mux)
	s.registerELKExtendedRoutes(mux)
	// 时序/流数据存储：InfluxDB/TDengine/VictoriaMetrics（固定路径优先，再泛路由 /api/v1/tsdb/）。
	s.registerTSDBRoutes(mux)
	// 向量数据库：Milvus/Qdrant/pgvector（固定路径优先，再泛路由 /api/v1/vector/）。
	s.registerVectorRoutes(mux)
	s.registerVectorExtendedRoutes(mux)
	// 图数据库：Neo4j/NebulaGraph（固定路径优先，再泛路由 /api/v1/graph/）。
	s.registerGraphRoutes(mux)
	s.registerGraphExtendedRoutes2(mux)
	// MongoDB 运维：固定路径优先，再泛路由 /api/v1/mongo/{deviceID}。
	s.registerMongoRoutes(mux)
	// OLAP 分析引擎：ClickHouse/Doris/Trino（固定路径优先，再泛路由 /api/v1/olap/）。
	s.registerOLAPRoutes(mux)
	s.registerOLAPExtendedRoutes(mux)
	// 内核运维：参数/模块/升级/GRUB（固定路径优先，再泛路由 /api/v1/kernel/）。
	s.registerKernelRoutes(mux)
	s.registerOSExtRoutes(mux)
	s.registerDomesticRoutes(mux)
	s.registerApacheRoutes(mux)
	s.registerOSTuningRoutes(mux)
	s.registerGitUnixRoutes(mux) // 安全运维：漏洞扫描/基线修复/组件更新/回滚（固定路径优先，再泛路由 /api/v1/security/）。
	s.registerSecurityRoutes(mux)
	s.registerSecurityExtRoutes(mux)
	s.registerCICDRoutes(mux)
	s.registerContainerRoutes(mux)
	s.registerHardwareRoutes(mux)
	s.registerDockerRoutes(mux)
	s.registerDBBackupRoutes(mux)
	s.registerNetworkExtRoutes(mux)
	s.registerIdentityRoutes(mux)
	s.registerAutomationRoutes(mux)
	s.registerCapacityRoutes(mux)
	s.registerServiceMeshRoutes(mux)
	s.registerHARoutes(mux)
	s.registerObservabilityRoutes(mux)
	s.registerIaCRoutes(mux)
	s.registerSyntheticRoutes(mux)
	s.registerStatusPageRoutes(mux)
	s.registerOnCallRoutes(mux)
	s.registerDBReplicationRoutes(mux)
	s.registerServiceDiscoveryRoutes(mux)
	s.registerK8sGatewayAPIRoutes(mux)
	s.registerMicroserviceRoutes(mux)
	s.registerRedisCacheScenarioRoutes(mux)
	s.registerNetDiagRoutes(mux)
	s.registerABACRoutes(mux)
	s.registerRAIDExtRoutes(mux)
	s.registerDBPerfRoutes(mux)
	s.registerGatewayMgmtRoutes(mux)
	// 虚拟化衔接：KVM/vSphere/OpenStack/Proxmox（固定路径优先，再泛路由 /api/v1/virt/）。
	s.registerVirtRoutes(mux)
	// Ansible 自动化运维：inventory/playbook/ad-hoc（固定路径优先，再泛路由 /api/v1/ansible/）。
	s.registerAnsibleRoutes(mux)
	s.registerScriptCompileRoutes(mux)
	// SSH 互信：keygen/authorize/revoke/list/test。
	s.registerSSHTrustRoutes(mux)
	// NTP 时间同步：status/sync/config/timezone/deploy/service。
	s.registerNTPRoutes(mux)
	// DNS 服务：status/deploy/record/query/service（bind/dnsmasq）。
	s.registerDNSRoutes(mux)
	// 文件共享：FTP/Samba/NFS（status/deploy/share/mount/service）。
	s.registerFSShareRoutes(mux)
	// Phase 3 K8s 集群管理：GET/POST 集群列表 + DELETE 单集群 + POST 测试连接。
	mux.HandleFunc("/api/v1/k8s/clusters", s.handleK8sClusters)
	mux.HandleFunc("/api/v1/k8s/clusters/", s.handleK8sClusterRouting) // 子路径：{id} 和 {id}/test
	s.registerK8sProvisionRoutes(mux)
	s.RegisterK8sClusterDeployRoutes(mux)
	// 修复 9：告警规则 CRUD API。
	mux.HandleFunc("/api/v1/alert-rules", s.handleAlertRules)
	mux.HandleFunc("/api/v1/alert-rules/", s.handleAlertRuleRouting) // 子路径：{id} DELETE 删除
	// M2 集成：告警规则引擎（多条件）+ 静默规则 + 通知渠道 + 通知模板 API。
	// 走独立路由 /api/v1/alert-rules-engine 避免与旧版 alert-rules 冲突（向后兼容）。
	mux.HandleFunc("/api/v1/alert-rules-engine", s.handleAlertRulesEngine)
	mux.HandleFunc("/api/v1/alert-rules-engine/", s.handleAlertRuleEngineRouting) // 子路径：{id} GET/PUT/DELETE
	mux.HandleFunc("/api/v1/alert-silences", s.handleAlertSilences)
	mux.HandleFunc("/api/v1/alert-silences/", s.handleAlertSilenceRouting) // 子路径：{id} DELETE
	mux.HandleFunc("/api/v1/notify-channels", s.handleNotifyChannels)
	mux.HandleFunc("/api/v1/notify-channels/", s.handleNotifyChannelRouting) // 子路径：{id} PUT/DELETE、{id}/test POST
	mux.HandleFunc("/api/v1/notify-templates", s.handleNotifyTemplates)
	mux.HandleFunc("/api/v1/notify-templates/", s.handleNotifyTemplateRouting) // 子路径：{id} PUT/DELETE

	// M3 集成：Helm 应用商店 API（仓库/Chart/Release/Catalog）。
	// helm CLI 不存在时各 API 返回 503，不阻断控制面启动。
	mux.HandleFunc("/api/v1/helm/repos", s.handleHelmRepos)
	mux.HandleFunc("/api/v1/helm/repos/", s.handleHelmRepoRouting)        // 子路径：{name} DELETE、{name}/charts GET
	mux.HandleFunc("/api/v1/helm/charts/search", s.handleHelmChartSearch) // ?q=xxx 搜索 chart
	mux.HandleFunc("/api/v1/helm/releases", s.handleHelmReleases)
	mux.HandleFunc("/api/v1/helm/releases/", s.handleHelmReleaseRouting) // 子路径：{name} PUT/DELETE、{name}/rollback、{name}/history
	mux.HandleFunc("/api/v1/helm/catalog", s.handleHelmCatalog)          // 预置应用目录

	// M5 集成：批量运维/灰度发布 + 定时任务管理 + 审批 API。
	// 批量执行走 /api/v1/tasks/batch-exec（避免与既有 /api/v1/tasks/batch 冲突）；
	// 批量状态查询走 /api/v1/tasks/batch/{id}；灰度发布走 /api/v1/tasks/canary[/{id}|/{id}/advance]。
	// 定时任务管理走 /api/v1/schedules[/{id}|/{id}/pause|/{id}/resume]。
	// 审批 API 走 /api/v1/approval/{flows,requests,pending}。
	s.registerBatchCanaryRoutes(mux)
	mux.HandleFunc("/api/v1/schedules", s.handleSchedules)                       // GET 列表 / POST 创建定时任务
	mux.HandleFunc("/api/v1/schedules/", s.handleScheduleRouting)                // 子路径：{id} GET/PUT/DELETE、{id}/pause、{id}/resume
	mux.HandleFunc("/api/v1/approval/flows", s.handleApprovalFlows)              // GET 列表 / POST 创建审批流
	mux.HandleFunc("/api/v1/approval/flows/", s.handleApprovalFlowRouting)       // 子路径：{id} GET/PUT/DELETE
	mux.HandleFunc("/api/v1/approval/requests", s.handleApprovalRequests)        // GET 列表 / POST 提交审批请求
	mux.HandleFunc("/api/v1/approval/requests/", s.handleApprovalRequestRouting) // 子路径：{id} GET、{id}/approve|reject|cancel|history
	mux.HandleFunc("/api/v1/approval/pending", s.handleApprovalPending)          // GET 待我审批列表

	// M6 集成：网络拓扑发现 + 网络诊断工具 + 连通性检测 API。
	// 拓扑探测结果缓存 5 分钟（networkTopologyCache），?refresh=true 强制刷新；
	// 诊断命令通过下发 shell task 到指定 agent 执行（复用现有任务机制）；
	// 连通性检测对每个 target 发起 tcping/ping 检测，同步等待最多 5 秒收集结果。
	mux.HandleFunc("/api/v1/network/topology", s.handleNetworkTopology)            // GET 拓扑图（?refresh=true 强制刷新）
	mux.HandleFunc("/api/v1/network/topology/cache", s.handleNetworkTopologyCache) // GET 缓存拓扑（不触发探测）
	mux.HandleFunc("/api/v1/network/diagnose", s.handleNetworkDiagnose)            // POST 发起诊断任务
	mux.HandleFunc("/api/v1/network/diagnose/", s.handleNetworkDiagnoseResult)     // GET 子路径：{taskId}
	mux.HandleFunc("/api/v1/network/connectivity", s.handleNetworkConnectivity)    // POST 批量连通性检测
	// Phase 4 网络设备管理：设备 CRUD + 指标 + 配置下发 + 网络发现。
	// 注意：/api/v1/network/topology 已由上方 server_network.go 注册，此处不重复。
	mux.HandleFunc("/api/v1/network/devices", s.handleNetworkDevices)
	mux.HandleFunc("/api/v1/network/devices/", s.handleNetworkDeviceRouting) // 子路径：{id} GET/DELETE、{id}/metrics GET、{id}/config POST
	mux.HandleFunc("/api/v1/network/discover", s.handleNetworkDiscover)      // POST 网络发现
	// Phase 4 自动化闭环：规则 CRUD + 启停 + 测试 + 执行历史。
	mux.HandleFunc("/api/v1/automation/rules", s.handleAutomationRules)
	mux.HandleFunc("/api/v1/automation/rules/", s.handleAutomationRuleRouting) // 子路径：{id} GET/PUT/DELETE、{id}/enable|disable|test POST
	mux.HandleFunc("/api/v1/automation/executions", s.handleAutomationExecutions)
	mux.HandleFunc("/api/v1/automation/executions/", s.handleAutomationExecutionRouting) // 子路径：{id} GET

	// Phase 5 扩展能力：API 网关 + Webhook + 自定义脚本。
	mux.HandleFunc("/api/v1/gateway/routes", s.handleGatewayRoutes)
	mux.HandleFunc("/api/v1/gateway/routes/", s.handleGatewayRouteRouting) // 子路径：{id} GET/PUT/DELETE、{id}/enable|disable POST
	mux.HandleFunc("/api/v1/gateway/stats", s.handleGatewayStats)
	mux.HandleFunc("/gw/", s.handleGatewayProxy) // API 网关最小数据面：按 PathPrefix 匹配路由反向代理转发
	mux.HandleFunc("/api/v1/webhooks", s.handleWebhooks)
	mux.HandleFunc("/api/v1/webhooks/", s.handleWebhookRouting) // 子路径：{id} GET/PUT/DELETE、{id}/test|deliveries
	mux.HandleFunc("/api/v1/scripts", s.handleScripts)
	mux.HandleFunc("/api/v1/scripts/", s.handleScriptRouting) // 子路径：{id} GET/PUT/DELETE、{id}/execute|executions

	// Phase 6 平台化：租户管理 + API Key + 插件市场 + 计费 + 平台配置/健康/指标。
	mux.HandleFunc("/api/v1/tenants", s.handleTenants)
	mux.HandleFunc("/api/v1/tenants/", s.handleTenantRouting) // 子路径：{id} GET/PUT/DELETE、{id}/suspend|activate
	mux.HandleFunc("/api/v1/apikeys", s.handleAPIKeys)
	mux.HandleFunc("/api/v1/apikeys/", s.handleAPIKeyRouting) // 子路径：{id} GET/PUT/DELETE、{id}/enable|disable
	mux.HandleFunc("/api/v1/marketplace/plugins", s.handleMarketplacePlugins)
	mux.HandleFunc("/api/v1/marketplace/plugins/", s.handleMarketplacePluginRouting) // 子路径：{id} GET/DELETE、{id}/install|uninstall|enable|disable
	mux.HandleFunc("/api/v1/billing/plans", s.handleBillingPlans)
	mux.HandleFunc("/api/v1/billing/plans/", s.handleBillingPlanRouting)                 // 子路径：{id} GET/PUT/DELETE
	mux.HandleFunc("/api/v1/billing/subscriptions", s.handleBillingSubscriptions)        // GET 列表 / POST 创建
	mux.HandleFunc("/api/v1/billing/subscriptions/", s.handleBillingSubscriptionRouting) // 子路径：{id} GET/PUT/DELETE
	mux.HandleFunc("/api/v1/billing/invoices", s.handleBillingInvoices)                  // GET 列表
	mux.HandleFunc("/api/v1/billing/invoices/", s.handleBillingInvoiceRouting)           // 子路径：{id} GET
	mux.HandleFunc("/api/v1/billing/usage", s.handleBillingUsage)                        // GET 资源用量统计
	mux.HandleFunc("/api/v1/platform/config", s.handlePlatformConfig)                    // GET/PUT 平台配置
	mux.HandleFunc("/api/v1/platform/health", s.handlePlatformHealth)                    // GET 平台健康检查
	mux.HandleFunc("/api/v1/platform/metrics", s.handlePlatformMetrics)                  // GET 平台指标汇总

	// 密钥管理 API：查看 provider 状态 + 测试连接 + 列出密钥 key。
	// status/keys 不返回 Vault token 与密钥值（安全考虑）；test 端点做 SSRF 校验。
	mux.HandleFunc("/api/v1/secrets/status", s.handleSecretsStatus)           // GET 当前 provider 配置概览
	mux.HandleFunc("/api/v1/secrets/test", s.handleSecretsTest)               // POST 测试 Vault 连接
	mux.HandleFunc("/api/v1/secrets/keys", s.handleSecretsKeys)               // GET 密钥 key 列表（仅名称 + provider）
	mux.HandleFunc("POST /api/v1/secrets/{key}/rotate", s.handleRotateSecret) // POST 密钥轮换 + 访问审计

	// Wave 2-B 云账号与云资源（租户隔离）。
	// GET/POST /api/v1/cloud-accounts 列表/创建；子路径 /{id}、/{id}/sync、/{id}/test、/{id}/regions 走 routing。
	// GET /api/v1/cloud-resources 云资源列表（可按 accountID/resourceType 过滤）。
	// POST /api/v1/cloud-resources 创建云资源。
	// DELETE /api/v1/cloud-resources/{id} 删除云资源。
	// PUT /api/v1/cloud-resources/{id}/tags 同步云资源标签。
	mux.HandleFunc("GET /api/v1/cloud-accounts", s.handleCloudAccounts)
	mux.HandleFunc("POST /api/v1/cloud-accounts", s.handleCloudAccounts)
	mux.HandleFunc("/api/v1/cloud-accounts/", s.handleCloudAccountRouting)
	mux.HandleFunc("GET /api/v1/cloud-resources", s.handleListCloudResources)
	mux.HandleFunc("POST /api/v1/cloud-resources", s.handleCreateCloudResource)
	mux.HandleFunc("DELETE /api/v1/cloud-resources/", s.handleDeleteCloudResource)
	mux.HandleFunc("PUT /api/v1/cloud-resources/", s.handleSyncCloudResourceTags)

	// CMDB 采集自动化：POST 手动触发全量采集（返回 {collected, failed}）。
	mux.HandleFunc("/api/v1/cmdb/collect", s.handleCMDBCollect)
	// CMDB 变更审批流：CI 创建/修改/删除走审批，审批通过后才执行实际变更。
	mux.HandleFunc("/api/v1/cmdb/changes", s.handleCMDBChanges)        // GET 列表 / POST 提交变更申请
	mux.HandleFunc("/api/v1/cmdb/changes/", s.handleCMDBChangeRouting) // 子路径：{id} GET、{id}/approve|reject POST

	// 多租户资源配额：GET/PUT/DELETE /api/v1/quotas[/{tenantID}]。
	// 始终注册（即使未启用配额检查，也可查询用量统计）；handler 内部按 s.quotaMgr.Enabled() 决定行为。
	mux.HandleFunc("/api/v1/quotas", s.handleQuotas)
	mux.HandleFunc("/api/v1/quotas/", s.handleQuotaRouting) // 子路径：{tenantID} GET/PUT/DELETE

	// Phase 1 服务台与工单管理 + SLO 管理 + Prometheus metrics 端点。
	mux.HandleFunc("/api/v1/tickets", s.handleTickets)
	mux.HandleFunc("/api/v1/tickets/", s.handleTicketRouting) // 子路径：{id} GET/PUT、{id}/close POST
	mux.HandleFunc("/api/v1/slos", s.handleSLOs)
	mux.HandleFunc("/api/v1/slos/", s.handleSLORouting) // 子路径：{id} GET/PUT/DELETE、{id}/status GET
	// Phase 2 路由：流量治理 / CI/CD 流水线 / ArgoCD / 灰度增强 / 配置热推送
	mux.HandleFunc("/api/v1/traffic/policies", s.handleTrafficPolicies)
	mux.HandleFunc("/api/v1/traffic/policies/", s.handleTrafficPolicyRouting) // 子路径：{id} GET/PUT/DELETE、{id}/enable|disable
	mux.HandleFunc("/api/v1/pipeline/templates", s.handlePipelineTemplates)
	mux.HandleFunc("/api/v1/pipeline/templates/", s.handlePipelineTemplate) // 子路径：{id} GET/PUT/DELETE、{id}/run
	mux.HandleFunc("/api/v1/pipeline/runs", s.handlePipelineRuns)
	mux.HandleFunc("/api/v1/pipeline/runs/", s.handlePipelineRun) // 子路径：{id} GET
	mux.HandleFunc("/api/v1/argocd/apps", s.handleArgoCDApps)
	mux.HandleFunc("/api/v1/argocd/apps/", s.handleArgoCDApp) // 子路径：{id} GET/PUT/DELETE、{id}/sync
	// Phase 2 Flux 应用管理（GitOps 原生第二大支柱）：GET/POST /api/v1/flux/apps，子路径 {id} GET/PUT/DELETE、{id}/sync。
	mux.HandleFunc("/api/v1/flux/apps", s.handleFluxApps)
	mux.HandleFunc("/api/v1/flux/apps/", s.handleFluxApp)
	mux.HandleFunc("/api/v1/canary/", s.handleCanaryEnhance) // 子路径：{id}/traffic-split、{id}/metrics
	mux.HandleFunc("/api/v1/config/hotpush", s.handleConfigHotpush)
	mux.HandleFunc("/api/v1/config/canary", s.handleConfigCanary)
	mux.HandleFunc("/api/v1/config/versions", s.handleConfigVersions)
	// Phase 3 路由：安全合规 / 审计查询 / HA 管理 / 灾备恢复
	mux.HandleFunc("/api/v1/compliance/rules", s.handleComplianceRules)
	mux.HandleFunc("/api/v1/compliance/rules/", s.handleComplianceRuleRouting) // 子路径：{id} GET
	mux.HandleFunc("/api/v1/compliance/scan", s.handleComplianceScan)          // POST 扫描设备合规状态
	mux.HandleFunc("/api/v1/compliance/reports", s.handleComplianceReports)
	mux.HandleFunc("/api/v1/compliance/reports/", s.handleComplianceReportRouting) // 子路径：{id} GET
	mux.HandleFunc("/api/v1/audit/events", s.handleAuditEvents)                    // GET 查询审计事件
	mux.HandleFunc("GET /api/v1/audit/high-risk", s.handleHighRiskAudits)          // GET 高风险审计专项查询（Wave 5-C）
	mux.HandleFunc("/api/v1/audit/verify", s.handleAuditVerify)                    // GET 校验审计哈希链（P1-3 防篡改）
	mux.HandleFunc("/api/v1/audit/export", s.handleAuditExport)                    // GET 导出审计日志
	mux.HandleFunc("/api/v1/ha/status", s.handleHAStatus)                          // GET HA 状态
	mux.HandleFunc("/api/v1/ha/instances", s.handleHAInstances)                    // GET 实例列表
	mux.HandleFunc("/api/v1/ha/failover", s.handleHAFailover)                      // POST 手动切换 leader
	mux.HandleFunc("/api/v1/ha/health", s.handleHAHealth)                          // GET 健康检查
	mux.HandleFunc("/api/v1/backup/create", s.handleBackupCreate)                  // POST 创建备份
	mux.HandleFunc("/api/v1/backup/list", s.handleBackupList)                      // GET 列出备份
	mux.HandleFunc("/api/v1/backup/restore", s.handleBackupRestore)                // POST 恢复备份
	mux.HandleFunc("/api/v1/backup/", s.handleBackupDeleteRouting)                 // 子路径：{id} DELETE
	mux.HandleFunc("/metrics", s.handlePrometheusMetrics)

	// 可观测性链路追踪：Jaeger 原生查询路径，不带 /api/v1 前缀。
	// 前端 trace.js 覆盖 baseURL 为 ''，直接请求 /api/services、/api/traces。
	// 当前为 graceful 降级实现，返回空结果，避免前端 404。
	mux.HandleFunc("GET /api/services", s.handleTraceServices)
	mux.HandleFunc("GET /api/services/", s.handleTraceRouting) // {service}/operations
	mux.HandleFunc("GET /api/traces", s.handleListTraces)
	mux.HandleFunc("GET /api/traces/", s.handleTraceRouting) // {traceID}

	// 修复 4：用 jsonErrorMux 包装 mux，将 404 统一为 JSON 格式。
	// ：httpMetricsMiddleware 包在最外层，记录所有请求（含 panic 转的 500）的计数与延迟。
	// ：otelx.HTTPMiddleware 为每个请求创建 span 并从请求头提取 W3C Trace Context，
	// 置于 recoveryMiddleware 之内使 panic 被捕获后 span 仍能正常 End()，置于 securityHeaders 之外
	// 使 span 覆盖完整业务逻辑（安全头注入不影响 span 边界）。
	httpSrv := &http.Server{
		Addr: fmt.Sprintf(":%d", s.httpPort),
		Handler: s.corsMiddleware( // CORS 白名单（--allowed-origins；空=同源策略不输出 CORS 头）
			s.httpMetricsMiddleware( // HTTP 指标（计数 + 延迟直方图）
				recoveryMiddleware( // 兜底盘
					otelx.HTTPMiddleware("opsmesh-controlplane", // OTel HTTP 自动埋点
						s.rateLimitMiddleware( // API 限流（429 Too Many Requests）
							s.securityHeadersMiddleware( // 安全头 + B1 CSP nonce
								s.csrfOriginCheck( // CSRF Origin 校验（状态变更方法）
									&paginate.JSONErrorMux{Inner: mux}))))))), // B1 404 JSON
		ReadHeaderTimeout: 10 * time.Second,
	}

	grpcSrv, grpcLis, err := s.buildGRPC()
	if err != nil {
		return fmt.Errorf("gRPC 建造失败: %w", err)
	}
	metricsSrv, metricsLis, err := s.buildMetrics()
	if err != nil {
		return fmt.Errorf("metrics 建造失败: %w", err)
	}
	// 联邦独立 mTLS 监听（端口 >0 且已启用联邦时生效；否则返回 nil）。
	fedSrv, fedLis, fedErr := s.buildFederationServer()
	if fedErr != nil {
		return fmt.Errorf("联邦 mTLS 监听构建失败: %w", fedErr)
	}

	// Web/REST 监听协议（--http-tls）：auto 时证书齐备即 HTTPS。
	// 显式 net.Listen 而非 ListenAndServe：端口占用等监听错误在启动期 fail-fast，
	// 而非推迟到后台 goroutine 里（原实现只在 goroutine 中返回错误）。
	httpTLS, err := s.buildHTTPTLS()
	if err != nil {
		return fmt.Errorf("Web/REST TLS 配置失败: %w", err)
	}
	if httpTLS != nil {
		httpSrv.TLSConfig = httpTLS
	}
	httpLis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.httpPort))
	if err != nil {
		return fmt.Errorf("HTTP(B/S) 监听失败 %d: %w", s.httpPort, err)
	}
	httpScheme := "http"
	if httpTLS != nil {
		httpScheme = "https"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logx.WithTrace(ctx, "controlplane")
	// 生产 + 明文 Web/REST：显式告警。--http-tls=off 是合法架构选择（TLS 由上游
	// Ingress/反代终止，本端口仅对内网开放），但端口一旦可达即为明文传输口令与会话。
	if s.cfg.Production && httpScheme == "http" {
		logx.Warn(ctx, "生产模式 Web/REST 为明文 HTTP（--http-tls=off）",
			"port", s.httpPort,
			"hint", "确保该端口仅对内网/反向代理开放、TLS 由上游终止；Cookie 已强制 Secure，浏览器直连明文端口会丢弃会话")
	}
	go s.leaderLoop(ctx)                // 选主：周期续租，仅 leader 执行周期协调任务
	go s.reclaimLoop(ctx)               // 任务租约回收：周期复位失联 agent 的 running 任务（仅 leader）
	go s.scheduleLoop(ctx)              // F4 定时/周期调度：周期派生到点模板任务的 pending 实例（仅 leader）
	go s.monitorTaskTimeouts(ctx)       // Wave 4-D：任务超时监控，超时 running 任务自动取消（仅 leader）
	go s.archiveLoop(ctx)               // F5 离线超龄自动归档（仅 leader）
	go s.auditMaintenanceLoop(ctx)      // P1-3 审计归档保留 + 哈希链自检（仅 leader）
	go s.notifyLoop(ctx)                // M7 告警 Webhook 推送：周期检查新 critical 告警并推送到 webhook URL
	go s.alertEngineLoop(ctx)           // M2 告警评估循环：alertengine.Engine + Silencer + Aggregator + Notifier
	go s.autoProvisionLoop(ctx)         // 自动纳管：--discover + --auto-provision 时周期扫描网段并推送 agent
	go s.deployReconcileLoop(ctx)       // M3 部署对账：周期把 running 部署按底层任务结果翻终态（仅 leader）
	go s.workflowScheduleLoop(ctx)      // M5 作业编排：周期按 cron 触发 active 工作流并 reconcile 运行态（仅 leader）
	s.startRefreshSweep(ctx, time.Hour) // 周期清理过期刷新令牌 + blacklist，ctx 取消时优雅退出
	go s.cutoverRosterLoop(ctx)         // 切流名册热重载：摘除条目即单用户回退、清空即回纯本地（无需重启；仅文件名册可热更）
	if s.cmdbCollector != nil {
		go s.cmdbCollector.Run(ctx) // CMDB 定时采集：周期从设备指标更新 CMDB CI（仅 leader）
	}
	s.startPipelineExecutor(ctx, 10*time.Second)                // pipeline 执行器：推进 pending→running→succeeded
	go s.startAutomationEvalLoop(ctx, s.automationEvalInterval) // 自动化引擎评估循环：周期评估 enabled 规则并执行命中动作

	errCh := make(chan error, 3)
	go func() {
		if err := grpcSrv.Serve(grpcLis); err != nil {
			errCh <- fmt.Errorf("grpc: %w", err)
		}
	}()
	go func() {
		if err := metricsSrv.Serve(metricsLis); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("metrics: %w", err)
		}
	}()
	go func() {
		logx.Info(ctx, "HTTP(B/S) 监听", "port", s.httpPort, "scheme", httpScheme)
		var serveErr error
		if httpTLS != nil {
			// TLSConfig 已就位（含 --tls-watch 热重载时的 GetCertificate），
			// 故此处的 cert/key 文件参数留空。
			serveErr = httpSrv.ServeTLS(httpLis, "", "")
		} else {
			serveErr = httpSrv.Serve(httpLis)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", serveErr)
		}
	}()
	if fedSrv != nil {
		go func() {
			logx.Info(ctx, "联邦 mTLS 监听", "port", s.cfg.FederationPort)
			// TLSConfig 已设置 RequireAndVerifyClientCert，ServeTLS 启用 mTLS。
			if err := fedSrv.ServeTLS(fedLis, "", ""); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("federation: %w", err)
			}
		}()
	}

	if s.cfg.FederationPort > 0 && s.fed != nil {
		logx.Info(ctx, "控制面已启动", "http", s.httpPort, "http_scheme", httpScheme, "grpc", s.grpcPort, "metrics", s.metricsPort, "federation_mtls", s.cfg.FederationPort)
	} else {
		logx.Info(ctx, "控制面已启动", "http", s.httpPort, "http_scheme", httpScheme, "grpc", s.grpcPort, "metrics", s.metricsPort)
	}
	// 优雅退出清理：无论正常收信号还是 server 异常返回，都停止 loginGuard 的 sweep goroutine，
	// 避免 goroutine 泄漏。startRefreshSweep 的 goroutine 由 ctx 取消自动退出（defer stop() 取消 ctx）。
	defer s.loginGuard.StopSweep()
	// OTel 优雅关闭：flush 残留 span 到导出器（OTLP gRPC batch / stdout）。
	// 用独立超时（5s）避免退出窗口耗尽在 OTel flush 上；未启用时为 no-op。
	defer s.shutdownOTel()
	// TLS 证书热重载器优雅关闭：关闭 fsnotify watcher 与退出 watchLoop goroutine。
	// 未启用热重载时 tlsReloader 为 nil，shutdownTLSReloader 为 no-op。
	defer s.shutdownTLSReloader()
	select {
	case <-ctx.Done():
		logx.Info(ctx, "收到终止信号，优雅退出", "window", s.shutdownWait.String())
		grpcSrv.GracefulStop()
		shutCtx, cancel := context.WithTimeout(context.Background(), s.shutdownWait)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			log.Printf("controlplane: HTTP 服务优雅退出失败: %v", err)
		}
		if err := metricsSrv.Shutdown(shutCtx); err != nil {
			log.Printf("controlplane: metrics 服务优雅退出失败: %v", err)
		}
		if fedSrv != nil {
			if err := fedSrv.Shutdown(shutCtx); err != nil {
				log.Printf("controlplane: federation 服务优雅退出失败: %v", err)
			}
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// registerTaskRoutes 注册任务域路由。
//
// 抽出独立方法的理由：Start() 内联注册无法在测试里复用（会真的起监听），
// 而"路由是否可达"正是本域最容易被改坏的东西（精确匹配 vs 通配匹配）。
// 测试直接构造 mux 调用本方法，断言的是**真实注册的模式**，不是复制一份。
//
// 模式语义（Go 1.22 ServeMux）：
//   - "/api/v1/tasks/"：子树模式，兜住 {id}/cancel|result|approve|reject|retry|dead-letter 等子路径。
//   - "GET /api/v1/tasks/{id}"：裸任务详情（前端 OS 优化/中间件部署的日志轮询依赖它）。
//   - "GET /api/v1/tasks/dead-letter"：字面量优先于 {id} 通配（更具体者胜），互不冲突。
func (s *Server) registerTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("/api/v1/tasks/", s.handleTaskRouting)                        // 子路径：{id}/cancel、{id}/result、{id}/retry、{id}/dead-letter
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleTaskRouting)                // GET 任务详情（{id} 详情 + 详情外的子树兜底）
	mux.HandleFunc("GET /api/v1/tasks/dead-letter", s.handleListDeadLetterTasks) // Wave 4-D DLQ 列表
	mux.HandleFunc("POST /api/v1/tasks/{id}/retry", s.handleTaskRouting)         // Wave 4-D DLQ 重试
	mux.HandleFunc("DELETE /api/v1/tasks/{id}/dead-letter", s.handleTaskRouting) // Wave 4-D DLQ 清除
}

// registerBatchCanaryRoutes 注册批量运维/灰度发布路由（M5）。
//
// 修复（2026-10-21）：此前注册的是**精确路径** "GET /api/v1/tasks/batch" 与
// "GET /api/v1/tasks/canary"，而两个 handler 解析的是 "/api/v1/tasks/batch/{id}"
// 与 "/api/v1/tasks/canary/{id}"（前者 TrimPrefix 掉带斜杠的前缀后为空 → 恒 404）。
// 于是真实路径 /api/v1/tasks/batch/{id} 落到 "/api/v1/tasks/" 子树 → handleTaskRouting
// → 404，handleBatchStatus / handleCanaryStatus / handleCanaryAdvance 全是死代码。
// 改为 Go 1.22 通配模式后真实路径直达对应 handler。
//
// 模式冲突说明（ServeMux 冲突检测）：
//   - "GET /api/v1/tasks/batch/{id}" 与 "GET /api/v1/tasks/{id}" 段数不同 → 不重叠，不冲突；
//   - "GET /api/v1/tasks/canary/{id}" 与 "GET /api/v1/tasks/dead-letter" 段数不同 → 同上；
//   - "GET /api/v1/tasks/{id}" 与 "GET /api/v1/tasks/dead-letter" 同段数但后者是字面量
//     （更具体）→ 允许共存，命中 dead-letter 时字面量胜出。
//
// 注册方式（2026-10-21 补）：batch-exec / canary 两个**字面量**路径必须按方法显式注册。
// 若沿用无方法限定的裸注册（如 "/api/v1/tasks/batch-exec"），它与 "GET /api/v1/tasks/{id}"
// 构成 ServeMux 冲突：batch-exec 路径更具体但方法集更宽（覆盖 GET），而 {id} 是 GET 限定，
// Go 无法裁定谁胜 ⇒ Start() 在 registerBatchCanaryRoutes 第一行即 panic（控制面起不来）。
// 改为 GET+POST 双注册后两者都是「同段数字面量+具体方法 ⇒ 更具体者胜」，不冲突；
// 且 GET 仍进各自 handler 由内部方法检查返回 405（observable 行为不变）。
func (s *Server) registerBatchCanaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tasks/batch-exec", s.handleBatchExec)                     // GET：handler 内 405（保持既有行为）
	mux.HandleFunc("POST /api/v1/tasks/batch-exec", s.handleBatchExec)                    // POST 批量执行（M5 增强）
	mux.HandleFunc("GET /api/v1/tasks/batch/{id}", s.handleBatchRouting)                  // GET 批量状态查询 {id}
	mux.HandleFunc("GET /api/v1/tasks/canary", s.handleCanaryCreate)                      // GET：handler 内 405（保持既有行为）
	mux.HandleFunc("POST /api/v1/tasks/canary", s.handleCanaryCreate)                     // POST 灰度发布
	mux.HandleFunc("GET /api/v1/tasks/canary/{id}", s.handleCanaryRouting)                // GET 灰度状态 {id}
	mux.HandleFunc("POST /api/v1/tasks/canary/{id}/advance", s.handleCanaryRouting)       // POST 灰度推进
	mux.HandleFunc("POST /api/v1/tasks/canary/{id}/traffic-split", s.handleCanaryEnhance) // POST 流量分割
	mux.HandleFunc("GET /api/v1/tasks/canary/{id}/metrics", s.handleCanaryEnhance)        // GET 灰度指标
}

// corsMiddleware 按白名单放行跨域请求（安全加固：替代反射任意 Origin）。
//
// 为什么不能反射 Origin：原实现把请求 Origin 原样回显到 Access-Control-Allow-Origin
// 并固定输出 Allow-Credentials: true，意味着【任何】恶意网站都能携带用户的 HttpOnly
// Cookie（at/rt）跨域调用控制面 API（CSRF 之上叠加数据读取能力，Critical 级）。
// 浏览器只校验"响应里的 Allow-Origin 是否与页面同源"，对反射值照单全收。
//
// 白名单语义（cfg.AllowedOrigins，--allowed-origins / OPSMESH_ALLOWED_ORIGINS）：
//   - 空（默认）=同源策略：不输出任何 CORS 头。跨域浏览器请求被同源策略拦截，
//     同源部署（前端与控制面同域名/经反代同源）完全不受影响。服务端无法区分
//     "跨域被浏览器拦"与"未配置白名单"，非浏览器客户端（curl/agent）不受 CORS 影响。
//   - 非空=仅当请求 Origin 与白名单中某项【精确匹配】时才输出
//     Allow-Origin（回显该 Origin）+ Allow-Credentials: true 等头；
//     白名单已禁止 "*"（Validate fail-fast），凭证模式下通配符无意义且危险。
//   - Origin 不在白名单：不输出任何 CORS 头，透传 next（浏览器侧等同跨域被拒）。
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	// 启动期一次性解析白名单为 map（O(1) 匹配），空 map 表示未配置（同源策略）。
	// corsMiddleware 仅在 Start 时构造一次，白名单运行期不可变，无并发问题。
	allowed := make(map[string]struct{})
	for _, item := range strings.Split(s.cfg.AllowedOrigins, ",") {
		if item = strings.TrimSpace(item); item != "" {
			allowed[item] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// 未配置白名单（同源策略）或 Origin 不在白名单：不输出任何 CORS 头，直接透传。
		// 注意不能对不匹配的预检 OPTIONS 返回 204（那会让浏览器认为放行），
		// 透传后由 mux 处理（OPTIONS 无路由注册则 404/405，浏览器侧判为跨域被拒）。
		if len(allowed) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := allowed[origin]; !ok {
			next.ServeHTTP(w, r)
			return
		}
		// Origin 精确匹配白名单：输出凭证模式 CORS 头。
		w.Header().Set("Access-Control-Allow-Origin", origin)
		// 回显而非 *：凭证模式下浏览器要求 Allow-Origin 为具体来源。
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// 处理预检请求：白名单放行的 OPTIONS 直接 204。
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
