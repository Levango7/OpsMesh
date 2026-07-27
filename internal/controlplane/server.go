// Package controlplane 实现控制面：HTTP(B/S) 仪表盘 + gRPC 注册通道 + metrics。
// U-05: 通过 --mode=controlplane 启动。
//   - gRPC 9090：承载 agent 注册/心跳/拉任务/上报结果（真实 gRPC，JSON codec，见 grpc.go）。
//   - HTTP 8080：B/S 仪表盘 + GET /api/v1/devices（人工查看）+ POST /api/v1/tasks（内部下发入口）。
//   - metrics 9091：极简文本指标（P2-1 观测）。
//
// U-04: 持久化后端由 --store 选择（默认 memory，可选 mysql）。
package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"opsmesh/internal/version"

	"google.golang.org/grpc"

	"opsmesh/internal/authctx"
	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/cron"
	"opsmesh/internal/deploy"
	"opsmesh/internal/domain"
	"opsmesh/internal/events"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/logstore"
	"opsmesh/internal/logx"
	"opsmesh/internal/metrics"
	"opsmesh/internal/notify"
	"opsmesh/internal/orchestration"
	"opsmesh/internal/proto"
	"opsmesh/internal/provision"
	"opsmesh/internal/store"
	"opsmesh/internal/tlsutil"
)

// Server 控制面服务（HTTP + gRPC + metrics）。
type Server struct {
	reg           *Registry
	cfg           *config.Config
	httpPort      int
	grpcPort      int
	metricsPort   int
	requireAuth   bool
	tlsCert       string
	tlsKey        string
	clientCA      string
	shutdownWait  time.Duration
	bus           events.Bus             // 审计/告警事件总线（P1-5，默认 noop）
	metrics       *metrics.M             // 观测指标（P2-1）
	store         store.Store            // 持久化存储（供 CMDB 等子系统复用）
	cmdbHandler   *cmdb.Handler          // CMDB 处理器（Phase 1）
	logHandler    *logstore.Handler      // M6 日志检索处理器
	deployHandler *deploy.Handler        // M3 部署中心处理器
	orchHandler   *orchestration.Handler // M5 作业编排中心处理器
	lastAlertSent time.Time              // M7 告警 Webhook：上次已推送的告警时间戳（notifyLoop 防重复）
}

// NewServer 构造控制面服务。按 cfg.Store 选择持久化后端（默认 memory），并初始化事件总线与指标。
func NewServer(cfg *config.Config) *Server {
	// Kafka brokers/topic 经参数传入事件总线（避免 os.Setenv 并发不安全，P1-5）。
	bus := events.New(cfg.EventBus, cfg.KafkaBrokers, cfg.KafkaTopic)
	st := selectStore(cfg, bus)
	reg := NewRegistryWithStore(st)
	s := &Server{
		reg:           reg,
		cfg:           cfg,
		store:         st,
		httpPort:      cfg.HTTPPort,
		grpcPort:      cfg.GRPCPort,
		metricsPort:   cfg.MetricsPort,
		requireAuth:   cfg.RequireAuth,
		tlsCert:       cfg.TLSCert,
		tlsKey:        cfg.TLSKey,
		clientCA:      cfg.ClientCA,
		shutdownWait:  cfg.ShutdownTimeout,
		bus:           bus,
		metrics:       metrics.New(),
		cmdbHandler:   newCMDBHandler(st),
		logHandler:    newLogHandler(st),
		deployHandler: newDeployHandler(st, reg),
		orchHandler:   newOrchestrationHandler(st),
	}
	if cfg.Demo {
		// 演示模式（P0-5）：主动播种 demo 拓扑，让 6 大模块在无真实 agent 时也能完整演示。
		if ms, ok := st.(*store.MemoryStore); ok {
			ms.SeedDemoTopology()
		}
		// 预置一条示例部署（created），用户点开「部署中心」即可一键「执行」看 fan-out。
		if _, err := s.deployHandler.Store().Create(context.Background(), &deploy.DeployTask{
			Name:      "deploy-nginx-demo",
			Type:      deploy.TypeScript,
			RepoURL:   "https://git.example.com/ops/nginx.git",
			TargetIDs: "dev-demo-01, dev-demo-02",
			TenantID:  "default",
			CreatedBy: "demo",
		}); err != nil {
			logx.Warn(context.Background(), "demo 部署播种失败", err)
		}
	}
	return s
}

// newDeployHandler 构造 M3 部署处理器：按 store 类型选 SQL/Memory 后端，
// 并用 Registry 适配 deploy.Dispatcher（防腐接口，避免 deploy 反向依赖 controlplane）。
func newDeployHandler(st store.Store, reg *Registry) *deploy.Handler {
	var ds deploy.DeployStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := deploy.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 部署后端初始化失败，回退 memory", err)
			ds = deploy.NewMemory()
		} else {
			logx.Info(context.Background(), "M3 部署后端=mysql", "reason", "U-04 数据本地化")
			ds = s
		}
	} else {
		ds = deploy.NewMemory()
	}
	return deploy.NewHandler(ds, &registryDispatcher{reg: reg})
}

// newOrchestrationHandler 构造 M5 作业编排处理器：按 store 类型选 SQL/Memory 后端，
// 并以 store（具备 CreateTask + TasksByParent）直接适配 orchestration.TaskEngine（防腐）。
func newOrchestrationHandler(st store.Store) *orchestration.Handler {
	var ws orchestration.WorkflowStore
	if ss, ok := st.(*store.SQLStore); ok {
		s, err := orchestration.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 工作流后端初始化失败，回退 memory", err)
			ws = orchestration.NewMemory()
		} else {
			logx.Info(context.Background(), "M5 工作流后端=mysql", "reason", "U-04 数据本地化")
			ws = s
		}
	} else {
		ws = orchestration.NewMemory()
	}
	return orchestration.NewHandler(ws, st)
}

// registryDispatcher 以 Registry 适配 deploy.Dispatcher（M3 -> M4 任务引擎派发）。
type registryDispatcher struct {
	reg *Registry
}

func (d *registryDispatcher) CreateTask(t *proto.Task) *proto.Task {
	return d.reg.CreateTask(t)
}

func (d *registryDispatcher) Device(id string) *proto.DeviceInfo {
	return d.reg.Device(id)
}

func (d *registryDispatcher) TaskStates(ids []string, tenantID string) map[string]string {
	out := make(map[string]string)
	if len(ids) == 0 {
		return out
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, t := range d.reg.AllTasks(tenantID) {
		if set[t.TaskID] {
			out[t.TaskID] = t.Status
		}
	}
	return out
}

// selectStore 按配置选择后端：--store=mysql 且 DSN 非空时启用 SQLStore，否则 MemoryStore。
// 同时注入事件总线（P1-5），使 store 层状态变更可经 Kafka 等真实消费。
func selectStore(cfg *config.Config, bus events.Bus) store.Store {
	if cfg.Store == "mysql" && cfg.MySQLDSN != "" {
		ss, err := store.NewSQLStore(cfg.MySQLDSN, cfg.RedisAddr)
		if err != nil {
			logx.Error(context.Background(), "MySQL store 初始化失败，回退 memory", err)
			return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo)
		}
		logx.Info(context.Background(), "持久化后端=mysql", "reason", "U-04 数据本地化")
		return ss.WithBus(bus).WithSecret(cfg.ProvisionSecret).WithDemo(cfg.Demo)
	}
	logx.Info(context.Background(), "持久化后端=memory", "reason", "默认，无外部依赖")
	return store.NewMemoryStore().WithSecret(cfg.ProvisionSecret).WithBus(bus).WithDemo(cfg.Demo)
}

// newCMDBHandler 按 store 类型创建 CMDB 处理器：MySQL 时使用 SQLCiStore，否则 MemoryCiStore。
func newCMDBHandler(st store.Store) *cmdb.Handler {
	if ss, ok := st.(*store.SQLStore); ok {
		return cmdb.NewHandler(cmdb.NewSQLCiStore(ss.DB()))
	}
	return cmdb.NewHandler(cmdb.NewMemoryCiStore())
}

// newLogHandler 按 store 类型创建 M6 日志检索处理器：MySQL 时使用 SQLLogStore，否则 MemoryLogStore。
func newLogHandler(st store.Store) *logstore.Handler {
	if ss, ok := st.(*store.SQLStore); ok {
		ls, err := logstore.NewSQL(ss.DB())
		if err != nil {
			logx.Error(context.Background(), "MySQL 日志后端初始化失败，回退 memory", err)
			return logstore.NewHandler(logstore.NewMemory(0))
		}
		logx.Info(context.Background(), "M6 日志后端=mysql", "reason", "U-04 数据本地化")
		return logstore.NewHandler(ls)
	}
	logx.Info(context.Background(), "M6 日志后端=memory", "reason", "默认，无外部依赖")
	return logstore.NewHandler(logstore.NewMemory(0))
}

// Start 启动 HTTP(B/S)、gRPC(9090)、metrics(9091) 三个监听，并在收到 SIGTERM/SIGINT 时优雅退出。
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/assets/", s.handleAsset) // 前端静态资源（E2 独立化：web/assets/*）
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/me", s.handleMe)
	mux.HandleFunc("/api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("/api/v1/tasks/", s.handleTaskRouting)           // 子路径：{id}/cancel、{id}/result
	mux.HandleFunc("/healthz", s.handleHealthz)                     // K8s 探针（P2-12）
	mux.HandleFunc("/api/v1/audits", s.handleAudits)                // GET 审计检索（P0-4）
	mux.HandleFunc("/api/v1/tasks/batch", s.handleBatchCreateTasks) // POST 批量下发（P0-3）
	mux.HandleFunc("/api/v1/devices/", s.handleDeviceRouting)       // 子路径：{id} DELETE 退役、{id}/provision
	mux.HandleFunc("/api/v1/alerts", s.handleAlerts)                // GET 活跃告警（M7）
	mux.HandleFunc("/api/v1/alerts/", s.handleAlertRouting)         // 子路径：{id}/ack、{id}/silence
	// B1 自动纳管：install.sh 分发脚本 + agent 二进制分发 + 自动纳管触发
	mux.HandleFunc("/install.sh", s.handleInstallSh)
	mux.HandleFunc("/bin/opsmesh-agent", s.handleServeAgent)
	mux.HandleFunc("/api/v1/provision/auto", s.handleAutoProvision)
	// CMDB（Phase 1）：按持久化后端选择 SQL 或 Memory 实现。
	s.cmdbHandler.RegisterRoutes(mux)
	// M6 日志检索：GET/POST /api/v1/logs（租户隔离由 authctx 注入）。
	s.logHandler.RegisterRoutes(mux)
	// M3 部署中心：POST/GET /api/v1/deploys（租户隔离由 authctx 注入）。
	s.deployHandler.RegisterRoutes(mux)
	// M5 作业编排中心：POST/GET /api/v1/workflows（租户隔离由 authctx 注入）。
	s.orchHandler.RegisterRoutes(mux)

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.httpPort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	grpcSrv, grpcLis := s.buildGRPC()
	metricsSrv, metricsLis := s.buildMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logx.WithTrace(ctx, "controlplane")
	go s.leaderLoop(ctx)           // A3 选主：周期续租，仅 leader 执行周期协调任务
	go s.reclaimLoop(ctx)          // P0-1 任务租约回收：周期复位失联 agent 的 running 任务（仅 leader）
	go s.scheduleLoop(ctx)         // F4 定时/周期调度：周期派生到点模板任务的 pending 实例（仅 leader）
	go s.archiveLoop(ctx)          // F5 ��线超龄自动归档（仅 leader）
	go s.notifyLoop(ctx)           // M7 告警 Webhook 推送：周期检查新 critical 告警并推送到 webhook URL
	go s.autoProvisionLoop(ctx)    // B1 自动纳管：--discover + --auto-provision 时周期扫描网段并推送 agent
	go s.deployReconcileLoop(ctx)  // M3 部署对账：周期把 running 部署按底层任务结果翻终态（仅 leader）
	go s.workflowScheduleLoop(ctx) // M5 作业编排：周期按 cron 触发 active 工作流并 reconcile 运行态（仅 leader）

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

	logx.Info(ctx, "控制面已启动", "http", s.httpPort, "grpc", s.grpcPort, "metrics", s.metricsPort)
	select {
	case <-ctx.Done():
		logx.Info(ctx, "收到终止信号，优雅退出", "window", s.shutdownWait.String())
		grpcSrv.GracefulStop()
		shutCtx, cancel := context.WithTimeout(context.Background(), s.shutdownWait)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		_ = metricsSrv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// buildGRPC 构造 gRPC server 与监听（支持可选 TLS/mTLS）。
func (s *Server) buildGRPC() (*grpc.Server, net.Listener) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.grpcPort))
	if err != nil {
		log.Fatalf("[controlplane] gRPC 监听失败 %d: %v", s.grpcPort, err)
	}
	var opts []grpc.ServerOption
	if s.tlsCert != "" && s.tlsKey != "" {
		creds, err := tlsutil.ServerCreds(s.tlsCert, s.tlsKey, s.clientCA)
		if err != nil {
			log.Fatalf("[controlplane] gRPC TLS 加载失败: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
		logx.Info(context.Background(), "gRPC 已启用 TLS", "mtls", s.clientCA != "")
	}
	gs := grpc.NewServer(opts...)
	gs.RegisterService(&grpcx.Registration_ServiceDesc, &grpcServerImpl{
		reg:         s.reg,
		requireAuth: s.requireAuth,
		cfg:         s.cfg,
		bus:         s.bus,
		metrics:     s.metrics,
		cmdb:        s.cmdbHandler,
		logs:        s.logHandler,
	})
	return gs, lis
}

// buildMetrics 构造 metrics HTTP server 与监听，渲染零依赖 Prometheus 文本指标（P2-1）。
func (s *Server) buildMetrics() (*http.Server, net.Listener) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.metricsPort))
	if err != nil {
		log.Fatalf("[controlplane] metrics 监听失败 %d: %v", s.metricsPort, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		s.metrics.SetAgents(len(s.reg.Agents("")))
		fmt.Fprint(w, s.metrics.Render())
	})
	return &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}, lis
}

// handleDevices 处理 GET /api/v1/devices，按网关注入租户返回 segment -> 设备 列表。
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	writeJSON(w, http.StatusOK, s.reg.Snapshot(actx.TenantID))
}

// handleAgents 处理 GET /api/v1/agents，按网关注入租户返回已注册 agent 列表（供前端下拉框，P1-4）。
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	out := make([]map[string]string, 0)
	for _, a := range s.reg.Agents(actx.TenantID) {
		out = append(out, map[string]string{
			"agentID":  a.AgentID,
			"hostname": a.Hostname,
			"segment":  a.Segment,
			"status":   a.Status,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMe 返回网关注入的当前身份上下文（供 B/S 仪表盘渲染身份、租户、角色）。
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tenantID": actx.TenantID,
		"userID":   actx.UserID,
		"roles":    actx.Roles,
		"mode":     "gateway-injected", // 内核不自鉴权，身份由前置网关注入
	})
}

// handleCreateTask 处理 POST /api/v1/tasks：内部下发入口（P0-2）。
// 请求体：{ "agentID": "...", "type": "shell", "command": "...", "tenantID": "可选" }
// 租户隔离：任务只能下发给本租户（网关注入）的 agent；缺失则按 body.tenantID 兜底。
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	var body struct {
		AgentID  string `json:"agentID"`
		Type     string `json:"type"`
		Command  string `json:"command"`
		TenantID string `json:"tenantID"`
		Schedule string `json:"schedule"` // F4 可选：5 字段 cron，设定则成为模板任务（周期派生实例）
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" || body.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID and command are required"})
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	targetTenant := body.TenantID
	if targetTenant == "" {
		targetTenant = actx.TenantID
	}
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	task := s.reg.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       body.Type,
		Command:    body.Command,
		Schedule:   body.Schedule,        // F4 模板任务（cron）随创建下传
		MaxRetries: s.cfg.TaskMaxRetries, // F2 失败重试上限随任务下发（store 层按策略重入队/死信）
	})
	s.reg.Audit(&proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "create_task",
		Target:   task.TaskID,
		Detail:   body.Command,
	})
	// 事件总线（P1-5）+ 队列深度观测（P2-1）。
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "create_task", Target: task.TaskID, Detail: body.Command, Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.reg.PendingDepth())
	}
	writeJSON(w, http.StatusCreated, task)
}

// handleListTasks 统一处理 /api/v1/tasks：
//   - GET：列出任务（租户隔离 + 可选 ?status= 过滤），经 domain 防腐层对外暴露领域模型。
//   - POST：下发给指定 agent（逻辑复用 handleCreateTask，P0-2 内部下发入口）。
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateTask(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	status := r.URL.Query().Get("status")
	tasks := s.reg.AllTasks(actx.TenantID)
	out := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if status != "" && t.Status != status {
			continue
		}
		out = append(out, domain.TaskFromProto(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeviceDetail 处理 GET /api/v1/devices/{id}：返回设备详情 + 其任务与最近执行结果（租户隔离）。
func (s *Server) handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	if id == "" {
		http.Error(w, "device id required", http.StatusBadRequest)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	dev := s.reg.Device(id)
	if dev == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	type deviceDetail struct {
		Device  *domain.Device       `json:"device"`
		Tasks   []*domain.Task       `json:"tasks"`
		Results []*domain.TaskResult `json:"results"`
	}
	dd := deviceDetail{Device: domain.DeviceFromProto(dev)}
	for _, t := range s.reg.AllTasks(actx.TenantID) {
		if t.AgentID == dev.AgentID {
			dd.Tasks = append(dd.Tasks, domain.TaskFromProto(t))
		}
	}
	for _, res := range s.reg.Results(dev.AgentID) {
		dd.Results = append(dd.Results, domain.TaskResultFromProto(res))
	}
	writeJSON(w, http.StatusOK, dd)
}

// lookupAgent 按 agentID 直接查（O(1) 直查，P2-17 修复线性扫描）。
func (s *Server) lookupAgent(id string) *proto.AgentInfo {
	return s.reg.Agent(id)
}

// scheduleLoop 周期性评估模板任务（F4 定时/周期调度）：每 30s 调一次
// reg.FireDueSchedules(now)，对到点（cron 匹配且本分钟未触发）的模板派生 pending 实例。
// ctx 取消即退出。
func (s *Server) scheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.reg.IsLeader() {
				continue // A3：非 leader 不派生，避免多副本重复派生定时实例
			}
			n := s.reg.FireDueSchedules(time.Now())
			if n > 0 {
				logx.Info(ctx, "定时任务派生", "fired", n)
			}
		}
	}
}

// archiveLoop F5 离线超龄自动归档：每 60s 由 leader 扫描最后心跳早于
// ArchiveAgeMin 的 agent 对应设备（或孤儿设备），批量标记 retired。
// 仅 leader 执行（归档属协调任务，避免多副本重复归档）。
func (s *Server) archiveLoop(ctx context.Context) {
	if s.cfg.ArchiveAgeMin <= 0 {
		return // 关闭自动归档（仅手动 DELETE 退役）
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.reg.IsLeader() {
				continue
			}
			n := s.reg.RetireStaleDevices(time.Duration(s.cfg.ArchiveAgeMin) * time.Minute)
			if n > 0 {
				logx.Info(ctx, "离线设备自动归档", "archived", n)
			}
			if tc := s.reg.CleanupTokens(1000); tc > 0 {
				logx.Info(ctx, "过期 install token 清理", "cleaned", tc)
			}
		}
	}
}

// notifyLoop M7 告警 Webhook 推送：每 10s 检查是否有新的 critical 告警，
// 有则通过 HTTP POST 推送到 cfg.AlertWebhookURL。cfg.AlertWebhookURL 为空时不启动。
// 防重复：通过 lastAlertSent 时间戳追踪；只推送 CreatedAt 晚于该时间戳的告警。
func (s *Server) notifyLoop(ctx context.Context) {
	if s.cfg.AlertWebhookURL == "" {
		return // Webhook 未配置，不启动 notifyLoop
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			alerts := s.reg.Alerts("")
			for _, a := range alerts {
				if a.Severity != "critical" {
					continue
				}
				if !a.CreatedAt.After(s.lastAlertSent) {
					continue // 已推送过
				}
				if err := notify.PostByType(s.cfg.AlertNotifierType, s.cfg.AlertWebhookURL, a); err != nil {
					logx.Error(ctx, "告警 Webhook 推送失败", err, "alertID", a.AlertID)
				} else {
					logx.Info(ctx, "告警 Webhook 推送成功", "alertID", a.AlertID)
				}
				if a.CreatedAt.After(s.lastAlertSent) {
					s.lastAlertSent = a.CreatedAt
				}
			}
		}
	}
}

// handleHealthz 健康检查端点（K8s liveness/readiness 探针，P2-12）。
// reclaimLoop 周期性复位超期 running 任务（P0-1 任务必达）：agent 领取后超过租约租期仍未
// 上报结果，视为失联，复位 pending 重新进入调度队列。ctx 取消即退出。
func (s *Server) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.reg.IsLeader() {
				continue // A3：非 leader 不回收，避免多副本重复复位 running 任务
			}
			n := s.reg.ReclaimStaleTasks(time.Duration(s.cfg.TaskLeaseSec) * time.Second)
			if n > 0 {
				logx.Info(ctx, "任务租约回收", "reclaimed", n)
			}
		}
	}
}

// leaderLoop A3 选主循环：每 LeaderTickSec 秒续租一次 leader 租约，
// 并通过日志在晋升/失去 leader 身份时打印一次（避免每 tick 刷屏）。
// 仅 leader 才会由 reclaimLoop/scheduleLoop（及后续 provision/离线归档循环）真正执行周期协调任务。
func (s *Server) leaderLoop(ctx context.Context) {
	tick := time.Duration(s.cfg.LeaderTickSec) * time.Second
	if tick <= 0 {
		tick = 5 * time.Second
	}
	ttl := time.Duration(s.cfg.LeaderTTLSec) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	var wasLeader bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			isLeader := s.reg.RenewLeadership(ttl)
			if isLeader != wasLeader {
				if isLeader {
					logx.Info(ctx, "晋升为 leader，开始执行周期协调任务", "ttl", ttl.String())
				} else {
					logx.Info(ctx, "失去 leader 身份，暂停周期协调任务（其他副本接管）")
				}
				wasLeader = isLeader
			}
		}
	}
}

// handleBatchCreateTasks 处理 POST /api/v1/tasks/batch：向多台 agent 批量下发同一任务模板（P0-3 卖点闭环）。
// 请求体：{ "targets":["a1","a2"], "type","command","content","path","tenantID" }
// 逐台 CreateTask（复用租户隔离校验与审计）；返回已创建任务 ID 与逐台失败条目。
func (s *Server) handleBatchCreateTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	var body struct {
		Targets  []string `json:"targets"`
		Type     string   `json:"type"`
		Command  string   `json:"command"`
		Content  string   `json:"content"`
		Path     string   `json:"path"`
		TenantID string   `json:"tenantID"`
		Schedule string   `json:"schedule"` // F4 可选：批量下发的任务模板也支持 cron
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(body.Targets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targets is required (non-empty)"})
		return
	}
	if body.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if body.Type == "" {
		body.Type = "shell"
	}
	targetTenant := body.TenantID
	if targetTenant == "" {
		targetTenant = actx.TenantID
	}
	created := make([]string, 0, len(body.Targets))
	type fail struct {
		Target string `json:"target"`
		Error  string `json:"error"`
	}
	fails := make([]fail, 0)
	for _, tid := range body.Targets {
		agent := s.lookupAgent(tid)
		if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
			fails = append(fails, fail{Target: tid, Error: "agent not found or tenant mismatch"})
			continue
		}
		task := s.reg.CreateTask(&proto.Task{
			AgentID:    tid,
			TenantID:   targetTenant,
			Type:       body.Type,
			Command:    body.Command,
			Content:    body.Content,
			Path:       body.Path,
			Schedule:   body.Schedule,        // F4 批量模板也支持 cron
			MaxRetries: s.cfg.TaskMaxRetries, // F2 批量下发同样带重试上限
		})
		s.reg.Audit(&proto.AuditEvent{
			TenantID: targetTenant,
			UserID:   actx.UserID,
			Action:   "create_task",
			Target:   task.TaskID,
			Detail:   "batch:" + body.Command,
		})
		if s.bus != nil {
			s.bus.Publish(r.Context(), events.Event{
				TenantID: targetTenant, UserID: actx.UserID,
				Action: "create_task", Target: task.TaskID, Detail: "batch:" + body.Command, Level: events.LevelInfo,
			})
		}
		created = append(created, task.TaskID)
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.reg.PendingDepth())
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"count":   len(created),
		"created": created,
		"errors":  fails,
	})
}

// handleAudits 处理 GET /api/v1/audits：按租户/动作/时间窗检索审计事件（P0-4 审计可查；U-04 等保三级留痕必须可检索）。
// 查询参数：tenant（requireAuth 时强制取自身租户）、action、from/to（RFC3339）、limit（默认 100，上限 1000）。
func (s *Server) handleAudits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	q := r.URL.Query()
	tenant := q.Get("tenant")
	if s.requireAuth {
		tenant = actx.TenantID // 强制租户隔离，忽略客户端伪造
	}
	action := q.Get("action")
	var since, until time.Time
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			until = t
		}
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	evs := s.reg.QueryAudits(tenant, action, since, until, limit)
	writeJSON(w, http.StatusOK, evs)
}

// handleTaskRouting 统一分派 /api/v1/tasks/{id}/... 子路径：
//   - POST /api/v1/tasks/{id}/cancel：取消任务（F3）
//   - GET  /api/v1/tasks/{id}/result：查询单条执行结果（A5/F7）
func (s *Server) handleTaskRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		s.handleCancelTask(w, r, id)
	case len(parts) == 2 && parts[1] == "result" && r.Method == http.MethodGet:
		s.handleTaskResult(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// handleCancelTask 处理 POST /api/v1/tasks/{id}/cancel：取消 pending/running 任务（F3）。
// 租户隔离：requireAuth 时强制用网关注入租户，禁止越权取消他租户任务。
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request, id string) {
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	tenant := actx.TenantID
	ok := s.reg.CancelTask(id, tenant)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not cancellable (not found / not pending|running / tenant mismatch)"})
		return
	}
	s.reg.Audit(&proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "cancel_task", Target: id, Detail: "cancelled via HTTP",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: tenant, UserID: actx.UserID, Action: "cancel_task", Target: id, Level: events.LevelInfo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "taskID": id})
}

// handleTaskResult 处理 GET /api/v1/tasks/{id}/result：返回单条执行结果（A5/F7）。
// 租户隔离：requireAuth 时仅返回本租户任务的结果（通过任务的租户归属判定）。
func (s *Server) handleTaskResult(w http.ResponseWriter, r *http.Request, id string) {
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	res := s.reg.TaskResult(id)
	if res == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "result not found"})
		return
	}
	// 租户隔离：结果对应的任务须属于当前租户（requireAuth 时强制）。
	if actx.TenantID != "" {
		found := false
		for _, t := range s.reg.AllTasks(actx.TenantID) {
			if t.TaskID == id {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
	}
	writeJSON(w, http.StatusOK, domain.TaskResultFromProto(res))
}

// handleDeviceRouting 统一分派 /api/v1/devices/{id}... 子路径：
//   - GET    /api/v1/devices/{id}：设备详情（设备+任务+结果）
//   - DELETE /api/v1/devices/{id}：退役/下线设备（F5）
//   - POST   /api/v1/devices/{id}/provision：触发自动纳管推送（B1）
func (s *Server) handleDeviceRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		http.Error(w, "device id required", http.StatusBadRequest)
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleDeviceDetail(w, r)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.handleRetireDevice(w, r, id)
	case len(parts) == 2 && parts[1] == "provision" && r.Method == http.MethodPost:
		s.handleProvision(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// handleRetireDevice 处理 DELETE /api/v1/devices/{id}：退役/下线设备（F5）。
// 标记 retired，退出活跃清单但仍可查归档；租户隔离。
func (s *Server) handleRetireDevice(w http.ResponseWriter, r *http.Request, id string) {
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	tenant := actx.TenantID
	ok := s.reg.RetireDevice(id, tenant)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found or tenant mismatch"})
		return
	}
	s.reg.Audit(&proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "retire_device", Target: id, Detail: "retired via HTTP",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "retired", "deviceID": id})
}

// handleProvision 处理 POST /api/v1/devices/{id}/provision：触发自动纳管（B1）。
// 签发一次性 install token + 构造可直接复制粘贴的 bootstrap curl|sh 命令，
// 经此命令在候选设备上安装 agent 后，agent 携带 token 回注册完成闭环。
func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request, id string) {
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	tenant := actx.TenantID
	dev := s.reg.Device(id)
	if dev == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if tenant != "" && dev.TenantID != tenant {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	token, _, err := s.reg.Provision(id, dev.IP, tenant)
	if err != nil {
		// TOCTOU 窗口补偿：store 层可能返回"device not found"（设备在本 handler 前置校验
		// 与 Provision 之间被删除）。安全（P2-F12）：映射为 404 而非 500。
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": errMsg})
		}
		return
	}
	// 安全（P1-F4）：bootstrap 地址用运维显式配置的 advertise-addr，绝不能用请求方可控的 r.Host
	// （Host 头注入可让 bootstrap 指向攻击者服务器→供应链 RCE）。空则回退本机（仅开发）。
	advertise := strings.TrimRight(s.cfg.AdvertiseAddr, "/")
	if advertise == "" {
		advertise = fmt.Sprintf("http://127.0.0.1:%d", s.httpPort)
		logx.Warn(r.Context(), "advertise-addr 未配置，bootstrap 回退本机地址（仅开发，生产务必配置 --advertise-addr）")
	}
	bootstrap := fmt.Sprintf("curl -sSL %s/install.sh | sh -s -- --token=%s", advertise, token)
	// B1 SSH 自动推送：若配置了 SSH 私钥，自动通过 SSH 在候选设备上执行 bootstrap。
	if s.cfg.ProvisionSSHKey != "" {
		sshAddr := fmt.Sprintf("%s:22", dev.IP)
		go func(addr, cmd, device string) {
			ctx := context.Background()
			logx.Info(ctx, "SSH 自动推送 agent", "device", device, "sshAddr", addr)
			out, err := provision.PushAndExec(ctx, addr, s.cfg.ProvisionSSHUser, s.cfg.ProvisionSSHKey, s.cfg.ProvisionSSHKP, s.cfg.ProvisionSSHKnownHosts, cmd)
			if err != nil {
				logx.Error(ctx, "SSH 推送失败", err, "device", device, "sshAddr", addr, "output", out)
			} else {
				logx.Info(ctx, "SSH 推送成功", "device", device, "output", out)
			}
		}(sshAddr, bootstrap, id)
	}
	s.reg.Audit(&proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "provision_agent", Target: id, Detail: "token issued via HTTP",
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"status":       "provisioning",
		"deviceID":     id,
		"installToken": token,
		"bootstrap":    bootstrap,
	})
}

// handleAlerts 处理 GET /api/v1/alerts：返回活跃告警（M7 监控告警最小数据源）。
// 租户隔离：requireAuth 时仅返回本租户告警。
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	writeJSON(w, http.StatusOK, s.reg.Alerts(actx.TenantID))
}

// handleAlertRouting 统一分派 /api/v1/alerts/{id}/... 子路径：
//   - POST /api/v1/alerts/{id}/ack：确认告警（M7）
//   - POST /api/v1/alerts/{id}/silence：静默告警（M7）
func (s *Server) handleAlertRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		http.Error(w, "alert id required", http.StatusBadRequest)
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "ack" && r.Method == http.MethodPost:
		s.handleAckAlert(w, r, id)
	case len(parts) == 2 && parts[1] == "silence" && r.Method == http.MethodPost:
		s.handleSilenceAlert(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// handleAckAlert 处理 POST /api/v1/alerts/{id}/ack：确认告警（M7）。
// 租户隔离：requireAuth 时强制网关注入租户，禁止越权确认他租户告警。
func (s *Server) handleAckAlert(w http.ResponseWriter, r *http.Request, id string) {
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	if s.reg.Alert(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
		return
	}
	if !s.reg.AckAlert(id, actx.TenantID, actx.UserID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "alert not found or tenant mismatch"})
		return
	}
	s.reg.Audit(&proto.AuditEvent{TenantID: actx.TenantID, UserID: actx.UserID, Action: "ack_alert", Target: id, Detail: "acknowledged via HTTP"})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{TenantID: actx.TenantID, UserID: actx.UserID, Action: "ack_alert", Target: id, Level: events.LevelInfo})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged", "alertID": id})
}

// handleSilenceAlert 处理 POST /api/v1/alerts/{id}/silence：静默告警（M7）。
// 请求体（可选）：{"durationMinutes":1440,"comment":"..."}；缺省静默 24h。
func (s *Server) handleSilenceAlert(w http.ResponseWriter, r *http.Request, id string) {
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	var body struct {
		DurationMinutes int    `json:"durationMinutes"`
		Comment         string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if s.reg.Alert(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
		return
	}
	until := time.Now()
	if body.DurationMinutes > 0 {
		until = until.Add(time.Duration(body.DurationMinutes) * time.Minute)
	}
	if !s.reg.SilenceAlert(id, actx.TenantID, actx.UserID, until, body.Comment) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "alert not found or tenant mismatch"})
		return
	}
	s.reg.Audit(&proto.AuditEvent{TenantID: actx.TenantID, UserID: actx.UserID, Action: "silence_alert", Target: id, Detail: fmt.Sprintf("silenced %dm: %s", body.DurationMinutes, body.Comment)})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{TenantID: actx.TenantID, UserID: actx.UserID, Action: "silence_alert", Target: id, Level: events.LevelInfo})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "silenced", "alertID": id})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleInstallSh 处理 GET /install.sh：下发 agent 自举安装脚本（B1 bootstrap）。
// 脚本由 provision.InstallScript 按 --advertise-addr 动态生成（内嵌下载地址），
// 配合 bootstrap 命令 `curl -sSL <addr>/install.sh | sh -s -- --token=<tok>` 完成 agent 安装与注册。
func (s *Server) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(provision.InstallScript(s.cfg.AdvertiseAddr, version.Version)))
}

// handleServeAgent 处理 GET /bin/opsmesh-agent：分发 agent 二进制本体（双模式同体），
// 供 install.sh 脚本下载安装。
func (s *Server) handleServeAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, err := os.Executable()
	if err != nil {
		http.Error(w, "cannot resolve agent binary", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cannot open agent binary", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=opsmesh-agent")
	if info != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// handleAutoProvision 处理 POST /api/v1/provision/auto：手动触发 B1 自动纳管编排。
// body: {"cidrs":["10.30.0.0/24"], "tenantID":"t1"}；cidrs 缺省时回退 --segment-cidr。
func (s *Server) handleAutoProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	var body struct {
		CIDRs    []string `json:"cidrs"`
		TenantID string   `json:"tenantID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && r.ContentLength != 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}
	cidrs := body.CIDRs
	if len(cidrs) == 0 && s.cfg.SegmentCIDR != "" {
		cidrs = []string{s.cfg.SegmentCIDR}
	}
	tenant := body.TenantID
	if tenant == "" {
		tenant = actx.TenantID
	}
	if len(cidrs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no cidrs provided (body.cidrs or --segment-cidr)"})
		return
	}
	sum, err := provision.AutoProvision(r.Context(), provision.Deps{
		UpsertDevice: s.reg.UpsertDevice,
		Provision:    s.reg.Provision,
	}, s.cfg, cidrs, tenant)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.reg.Audit(&proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "auto_provision", Target: strings.Join(cidrs, ","),
		Detail: fmt.Sprintf("scanned=%d registered=%d provisioned=%d sshPushed=%d", sum.Scanned, sum.Registered, sum.Provisioned, sum.SSHPushed),
	})
	writeJSON(w, http.StatusOK, sum)
}

// autoProvisionLoop 后台周期执行 B1 自动纳管：仅当 --discover && --auto-provision 开启时，
// 每隔 discoverInterval 对 --segment-cidr 做存活扫描→登记候选设备→（配置 SSH key 时）推送 agent。
// 仅 leader 执行（避免多副本重复推送）。
func (s *Server) autoProvisionLoop(ctx context.Context) {
	if !s.cfg.Discover || !s.cfg.AutoProvision {
		return
	}
	const interval = 5 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if s.reg.IsLeader() && s.cfg.SegmentCIDR != "" {
			_, _ = provision.AutoProvision(ctx, provision.Deps{
				UpsertDevice: s.reg.UpsertDevice,
				Provision:    s.reg.Provision,
			}, s.cfg, []string{s.cfg.SegmentCIDR}, "")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// deployReconcileLoop 后台周期对账 M3 部署：把 running 部署按底层任务结果翻成功/失败。
// 仅 leader 执行（避免多副本重复对账写库）。
func (s *Server) deployReconcileLoop(ctx context.Context) {
	const interval = 15 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if s.reg.IsLeader() {
			s.deployHandler.ReconcileAll(ctx, "")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// workflowScheduleLoop 后台周期按 cron 触发 active 工作流并 reconcile 运行态。
// 仅 leader 执行（避免多副本重复派发底层任务）。
func (s *Server) workflowScheduleLoop(ctx context.Context) {
	const interval = 30 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !s.reg.IsLeader() {
			continue
		}
		list, err := s.orchHandler.ListActive(ctx)
		if err != nil {
			continue
		}
		now := time.Now()
		nowMin := now.Truncate(time.Minute)
		for _, wf := range list {
			if wf.Cron == "" {
				continue
			}
			ok, err := cron.Match(wf.Cron, now)
			if err != nil || !ok {
				continue
			}
			// 防同分钟重复触发：与上次运行落在本分钟内则跳过。
			if !wf.LastRunAt.IsZero() && wf.LastRunAt.Truncate(time.Minute).Equal(nowMin) {
				continue
			}
			if _, err := s.orchHandler.Trigger(ctx, wf.ID, wf.TenantID); err != nil {
				logx.Error(ctx, "工作流 cron 触发失败", err, "workflowID", wf.ID)
				continue
			}
			_ = s.orchHandler.Reconcile(ctx, wf.ID, wf.TenantID)
		}
	}
}

// writeJSON 统一写出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
