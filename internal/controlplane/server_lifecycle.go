// server_lifecycle.go — 控制面进程生命周期：Start 启动 HTTP/gRPC/metrics 三监听并优雅退出。
package controlplane

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opsmesh/internal/logx"
	"opsmesh/internal/otelx"
)

// Start 启动 HTTP(B/S)、gRPC(9090)、metrics(9091) 三个监听，并在收到 SIGTERM/SIGINT 时优雅退出。
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/assets/", s.handleAsset) // 前端静态资源（独立化：web/assets/*）
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/me", s.handleMe)
	mux.HandleFunc("/api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("/api/v1/tasks/", s.handleTaskRouting)           // 子路径：{id}/cancel、{id}/result
	mux.HandleFunc("/healthz", s.handleHealthz)                     // K8s liveness 探针（+ 深度检查）
	mux.HandleFunc("/readyz", s.handleReadyz)                       // K8s readiness 探针（新增）
	mux.HandleFunc("/api/v1/audits", s.handleAudits)                // GET 审计检索
	mux.HandleFunc("/api/v1/tasks/batch", s.handleBatchCreateTasks) // POST 批量下发
	mux.HandleFunc("/api/v1/devices/", s.handleDeviceRouting)       // 子路径：{id} DELETE 退役、{id}/provision
	mux.HandleFunc("/api/v1/alerts", s.handleAlerts)                // GET 活跃告警（M7）
	mux.HandleFunc("/api/v1/alerts/", s.handleAlertRouting)         // 子路径：{id}/ack、{id}/silence
	mux.HandleFunc("/api/v1/events/stream", s.handleEventsStream)   // SSE 实时推送（替代 5s 轮询）
	// 自动纳管：install.sh 分发脚本 + agent 二进制分发 + 自动纳管触发
	mux.HandleFunc("/install.sh", s.handleInstallSh)
	mux.HandleFunc("/bin/opsmesh-agent", s.handleServeAgent)
	mux.HandleFunc("/api/v1/provision/auto", s.handleAutoProvision)
	// CMDB（Phase 1）：按持久化后端选择 SQL 或 Memory 实现。
	s.cmdbHandler.RegisterRoutes(mux)
	// M6 日志检索：GET/POST /api/v1/logs（租户隔离由 authctx 注入）。
	s.logHandler.RegisterRoutes(mux)
	// M3 部署中心：POST/GET /api/v1/deploys（租户隔离由 authctx 注入）。
	// 修复 3：用 paginateJSONHandler 包装 GET 列表做分页（向后兼容）。
	deployMux := http.NewServeMux()
	s.deployHandler.RegisterRoutes(deployMux)
	mux.Handle("/api/v1/deploys", paginateJSONHandler(deployMux))
	mux.Handle("/api/v1/deploys/", deployMux)
	// M5 作业编排中心：POST/GET /api/v1/workflows（租户隔离由 authctx 注入）。
	// 修复 3：同上分页包装。
	orchMux := http.NewServeMux()
	s.orchHandler.RegisterRoutes(orchMux)
	mux.Handle("/api/v1/workflows", paginateJSONHandler(orchMux))
	mux.Handle("/api/v1/workflows/", orchMux)
	// 控制面联邦：仅当配置了 --federation-peers 时注册联邦 API。
	// 未启用时这些端点返回 404（mux 未注册），保证向后兼容。
	if s.fed != nil {
		mux.HandleFunc("/api/v1/federation/peers", s.handleFederationPeers)
		mux.HandleFunc("/api/v1/federation/forward/task", s.handleFederationForwardTask)
		mux.HandleFunc("/api/v1/federation/devices", s.handleFederationDevices)
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
	// 中间件部署：预置模板列表 + 详情 + 在指定 agent 上部署 + 已部署实例查询 + 卸载。
	mux.HandleFunc("/api/v1/middleware-templates", s.handleMiddlewareTemplates)
	mux.HandleFunc("/api/v1/middleware-templates/", s.handleMiddlewareTemplateDetail) // 子路径：{id} 和 {id}/deploy
	mux.HandleFunc("/api/v1/middleware-instances", s.handleMiddlewareInstances)
	mux.HandleFunc("/api/v1/middleware-instances/", s.handleMiddlewareInstanceRouting) // 子路径：{id}/uninstall
	// Phase 3 K8s 集群管理：GET/POST 集群列表 + DELETE 单集群 + POST 测试连接。
	mux.HandleFunc("/api/v1/k8s/clusters", s.handleK8sClusters)
	mux.HandleFunc("/api/v1/k8s/clusters/", s.handleK8sClusterRouting) // 子路径：{id} 和 {id}/test
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
	mux.HandleFunc("/api/v1/tasks/batch-exec", s.handleBatchExec)                // POST 批量执行（M5 增强）
	mux.HandleFunc("/api/v1/tasks/batch/", s.handleBatchRouting)                 // GET 批量状态查询
	mux.HandleFunc("/api/v1/tasks/canary", s.handleCanaryCreate)                 // POST 灰度发布
	mux.HandleFunc("/api/v1/tasks/canary/", s.handleCanaryRouting)               // GET 灰度状态 / POST advance
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

	// 密钥管理 API：查看 provider 状态 + 测试连接 + 列出密钥 key。
	// status/keys 不返回 Vault token 与密钥值（安全考虑）；test 端点做 SSRF 校验。
	mux.HandleFunc("/api/v1/secrets/status", s.handleSecretsStatus) // GET 当前 provider 配置概览
	mux.HandleFunc("/api/v1/secrets/test", s.handleSecretsTest)     // POST 测试 Vault 连接
	mux.HandleFunc("/api/v1/secrets/keys", s.handleSecretsKeys)     // GET 密钥 key 列表（仅名称 + provider）

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
	mux.HandleFunc("/metrics", s.handlePrometheusMetrics)

	// 修复 4：用 jsonErrorMux 包装 mux，将 404 统一为 JSON 格式。
	// ：httpMetricsMiddleware 包在最外层，记录所有请求（含 panic 转的 500）的计数与延迟。
	// ：otelx.HTTPMiddleware 为每个请求创建 span 并从请求头提取 W3C Trace Context，
	// 置于 recoveryMiddleware 之内使 panic 被捕获后 span 仍能正常 End()，置于 securityHeaders 之外
	// 使 span 覆盖完整业务逻辑（安全头注入不影响 span 边界）。
	httpSrv := &http.Server{
		Addr: fmt.Sprintf(":%d", s.httpPort),
		Handler: s.httpMetricsMiddleware( // HTTP 指标（计数 + 延迟直方图）
			recoveryMiddleware( // 兜底盘
				otelx.HTTPMiddleware("opsmesh-controlplane", // OTel HTTP 自动埋点
					s.rateLimitMiddleware( // API 限流（429 Too Many Requests）
						s.securityHeadersMiddleware( // 安全头 + B1 CSP nonce
							s.csrfOriginCheck( // CSRF Origin 校验（状态变更方法）
								&jsonErrorMux{inner: mux})))))), // B1 404 JSON
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logx.WithTrace(ctx, "controlplane")
	go s.leaderLoop(ctx)                // 选主：周期续租，仅 leader 执行周期协调任务
	go s.reclaimLoop(ctx)               // 任务租约回收：周期复位失联 agent 的 running 任务（仅 leader）
	go s.scheduleLoop(ctx)              // F4 定时/周期调度：周期派生到点模板任务的 pending 实例（仅 leader）
	go s.archiveLoop(ctx)               // F5 ��线超龄自动归档（仅 leader）
	go s.notifyLoop(ctx)                // M7 告警 Webhook 推送：周期检查新 critical 告警并推送到 webhook URL
	go s.alertEngineLoop(ctx)           // M2 告警评估循环：alertengine.Engine + Silencer + Aggregator + Notifier
	go s.autoProvisionLoop(ctx)         // 自动纳管：--discover + --auto-provision 时周期扫描网段并推送 agent
	go s.deployReconcileLoop(ctx)       // M3 部署对账：周期把 running 部署按底层任务结果翻终态（仅 leader）
	go s.workflowScheduleLoop(ctx)      // M5 作业编排：周期按 cron 触发 active 工作流并 reconcile 运行态（仅 leader）
	s.startRefreshSweep(ctx, time.Hour) // 周期清理过期刷新令牌 + blacklist，ctx 取消时优雅退出
	if s.cmdbCollector != nil {
		go s.cmdbCollector.Run(ctx) // CMDB 定时采集：周期从设备指标更新 CMDB CI（仅 leader）
	}

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
		logx.Info(ctx, "HTTP(B/S) 监听", "port", s.httpPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", err)
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
		logx.Info(ctx, "控制面已启动", "http", s.httpPort, "grpc", s.grpcPort, "metrics", s.metricsPort, "federation_mtls", s.cfg.FederationPort)
	} else {
		logx.Info(ctx, "控制面已启动", "http", s.httpPort, "grpc", s.grpcPort, "metrics", s.metricsPort)
	}
	// 优雅退出清理：无论正常收信号还是 server 异常返回，都停止 loginGuard 的 sweep goroutine，
	// 避免 goroutine 泄漏。startRefreshSweep 的 goroutine 由 ctx 取消自动退出（defer stop() 取消 ctx）。
	defer s.loginGuard.stopSweep()
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
