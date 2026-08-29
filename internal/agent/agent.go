// Package agent 实现网段 agent：经真实 gRPC(9090) 注册、周期心跳、拉取任务、本地执行(os/exec)、上报结果。
// 通过 --mode=agent 启动；与控制面共用同一份二进制/镜像。
// 四条通道（注册/心跳/拉任务/上报结果）全部走 gRPC 9090（JSON codec，见 grpcclient.go），
// 不依赖 protobuf 代码生成。
package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"go.opentelemetry.io/otel/attribute"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opsmesh/internal/circuitbreaker"
	"opsmesh/internal/config"
	"opsmesh/internal/discovery"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/logx"
	"opsmesh/internal/otelx"
	"opsmesh/internal/proto"
)

// runState 记录一个正在执行中的任务的控制句柄（F3 取消信号用）。
type runState struct {
	cancel    context.CancelFunc // 中止该任务执行的取消函数
	cancelled bool               // 控制面已下发取消（cancelLoop 置位）
}

// Agent 网段 agent 运行时。
type Agent struct {
	cfg            *config.Config
	grpc           *GRPCClient // 到控制面 9090 的 gRPC 通道
	agentID        string
	hostname       string
	dataDir        string // agent.id 落盘目录（身份持久化）
	taskTimeout    time.Duration
	workers        int
	taskCh         chan proto.Task      // 待执行任务队列（worker 池消费）
	runMu          sync.Mutex           // 保护 running 的并发读写
	running        map[string]*runState // 当前正在执行的任务 ID -> 控制句柄（F3）
	cmdbSeq        int64                // CMDB 上报序列号（agent 侧递增）
	cmdbLastCol    time.Time            // 上次 CMDB 采集时间（每 60s 采集一次）
	metricsLastCol time.Time            // 上次监控指标采集时间（每 30s 采集一次）
	metricsHistory *MetricsHistory      // 监控指标历史环形缓冲（默认 2h/240 条，本地保留供扩展查询）
	// 日志采集 agent 推送：定时读取指定日志文件的新增内容（基于文件 offset）并上报控制面。
	// logCollectPaths 为需采集的日志文件路径列表（来自 OPSMESH_LOG_COLLECT_PATHS 环境变量，逗号分隔）；
	// logCollectInterval 为采集间隔（来自 OPSMESH_LOG_COLLECT_INTERVAL，默认 30s）；
	// logOffsets 记录每个文件上次读取到的 offset，下次仅读取增量；logMu 保护 logOffsets 并发读写。
	logCollectPaths    []string
	logCollectInterval time.Duration
	logOffsets         map[string]int64
	logMu              sync.Mutex
	// OTel 链路追踪：TracerProvider 优雅关闭函数。
	// 由 Run 调用 otelx.Init 构造（初始化失败时返回 error 而非 fatal）；
	// Run 优雅退出时调用以 flush 残留 span。
	// nil=未启用 OTel（endpoint 空且 stdout=false）或 Run 尚未执行 OTel 初始化，退出时跳过。
	otelShutdown otelx.ShutdownFunc
	// 熔断器：按 deviceID（即 agentID）隔离的熔断器集合。
	// 由 New 构造；worker 执行任务前经熔断器放行，连续失败 N 次后熔断该设备，
	// 跳过任务执行并返回 "circuit breaker open" 错误，熔断恢复后自动重新执行。
	// cbSet 为 nil 表示禁用熔断器（CBFailureThreshold<=0），worker 直接执行。
	cbSet *circuitbreaker.BreakerSet
	// 日志采集推送：agent 端尾随日志文件批量推送到 Loki/ES。
	// 由 Run 在 cfg.LogPushEnabled=true 时构造并启动 goroutine；nil=未启用。
	// 退出时调用 Stop 触发剩余缓冲 flush。
	logPusher *LogPusher
	// 增强日志采集器（agent v2.0）：多路径/多行合并/过滤/限速/热更新。
	// 由 Run 在配置了日志采集路径（logCollectPaths 非空）时构造并启动 goroutine；nil=未启用。
	// 退出时调用 Stop 等待采集 goroutine 退出。与 logPusher 互补：
	//   - logPusher：尾随文件批量推送到 Loki/ES（外部存储）。
	//   - logCollector：增量采集经 gRPC 上报到控制面（控制面落库）。
	logCollector *LogCollector
}

// New 构造 agent（读取 hostname，作为注册信息）。
func New(cfg *config.Config) *Agent {
	h, err := os.Hostname()
	if err != nil {
		h = "unknown-host"
	}
	w := cfg.WorkerConcurrency
	if w <= 0 {
		w = 4
	}
	a := &Agent{
		cfg:            cfg,
		hostname:       h,
		dataDir:        cfg.DataDir,
		agentID:        loadOrCreateAgentID(cfg.DataDir, h), // 身份持久化：重启沿用稳定 ID
		taskTimeout:    cfg.TaskTimeout,
		workers:        w,
		taskCh:         make(chan proto.Task, w*2),
		running:        make(map[string]*runState),
		metricsHistory: NewMetricsHistory(MetricsHistoryDefaultCap), // 环形缓冲：默认 2h/240 条
	}
	a.initLogCollect()
	// OTel 链路追踪初始化已移至 Run() 方法：New() 返回 *Agent 无法返回 error，
	// 而 OTel 初始化失败属于可向调用方透传的错误，故在 Run()（返回 error）中执行，
	// 失败时由 main.go 的 `if err := ag.Run(); err != nil` 统一处理。
	// 熔断器初始化：CBFailureThreshold>0 时启用，按 deviceID（即 agentID）隔离。
	// 禁用时 cbSet 为 nil，worker 直接执行任务（零开销，向后兼容）。
	if cfg.CBFailureThreshold > 0 {
		a.cbSet = circuitbreaker.NewBreakerSet(circuitbreaker.Config{
			FailureThreshold: cfg.CBFailureThreshold,
			RecoveryTimeout:  cfg.CBRecoveryTimeout,
			HalfOpenMaxCalls: cfg.CBHalfOpenMaxCalls,
			OnStateChange: func(name, from, to string) {
				logx.Info(context.Background(), "熔断器状态变更", "device", name, "from", from, "to", to)
			},
		})
		logx.Info(context.Background(), "熔断器已启用",
			"failureThreshold", cfg.CBFailureThreshold,
			"recoveryTimeout", cfg.CBRecoveryTimeout,
			"halfOpenMaxCalls", cfg.CBHalfOpenMaxCalls)
	}
	// 安全加固：shell 白名单状态启动日志（运维可见性）。
	// 白名单非空时打印生效的白名单（含默认填充的 defaultAgentShellWhitelist）；
	// 白名单为空时打印警告（agent 放行所有命令，仅 demo/受信内网推荐）。
	if cfg.AgentShellWhitelist != "" {
		logx.Info(context.Background(), "agent shell 白名单已启用",
			"whitelist", cfg.AgentShellWhitelist)
	} else {
		logx.Info(context.Background(), "agent shell 白名单未启用（放行所有命令，仅 demo/受信内网推荐；生产建议 --agent-shell-whitelist-default=true 或显式配置）")
	}
	return a
}

// firstNonEmptyAgent 返回第一个非空字符串，全空返回空串。用于服务名默认值回退。
func firstNonEmptyAgent(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// initLogCollect 初始化日志采集配置。
// config.go 不在本次修改范围，故通过环境变量读取配置（与 config.go 的 env 兜底模式一致）：
//   - OPSMESH_LOG_COLLECT_PATHS：逗号分隔的日志文件路径（如 /var/log/messages,/var/log/syslog）。
//   - OPSMESH_LOG_COLLECT_INTERVAL：采集间隔（如 30s），默认 30s。
//
// 任一未配置或非法不阻塞启动，仅不启用日志采集。
func (a *Agent) initLogCollect() {
	a.logOffsets = make(map[string]int64)
	a.logCollectInterval = 30 * time.Second // 默认 30s
	if v, ok := os.LookupEnv("OPSMESH_LOG_COLLECT_INTERVAL"); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			a.logCollectInterval = d
		}
	}
	if v, ok := os.LookupEnv("OPSMESH_LOG_COLLECT_PATHS"); ok && v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				a.logCollectPaths = append(a.logCollectPaths, p)
			}
		}
	}
}

// shutdownOTel OTel 优雅关闭：flush 残留 span 到导出器。
// 未启用 OTel（otelShutdown 为 nil 或 no-op）时直接返回，零开销。
// 用 5s 超时避免退出窗口耗尽在 OTel flush 上。
func (a *Agent) shutdownOTel() {
	if a.otelShutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.otelShutdown(ctx); err != nil {
		log.Printf("agent: OTel shutdown 失败: %v", err)
	}
}

// loadOrCreateAgentID 持久化 agent 身份：优先从 <dir>/agent.id 读取稳定 ID，
// 不存在则生成 <host>-<unixnano> 并落盘。任一环节失败均回退内存生成（重启即新 ID），不阻塞启动。
func loadOrCreateAgentID(dir, host string) string {
	fallback := func() string { return fmt.Sprintf("%s-%d", host, time.Now().UnixNano()) }
	if dir == "" {
		return fallback()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fallback()
	}
	p := filepath.Join(dir, "agent.id")
	if b, err := os.ReadFile(p); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}
	id := fallback()
	if err := os.WriteFile(p, []byte(id), 0o600); err != nil {
		return id // 写入失败也接受：本次运行用此 ID，重启回退
	}
	return id
}

// installToken 读取 install token（自动纳管闭环）。
// 安全（F16）：优先读 <dataDir>/install.token 文件（bootstrap 脚本写入），
// 避 --install-token 命令行参数被 ps 可见。文件不存在或读取失败时回退 cfg.InstallToken。
func (a *Agent) installToken() string {
	if a.dataDir != "" {
		p := filepath.Join(a.dataDir, "install.token")
		if b, err := os.ReadFile(p); err == nil {
			if tok := strings.TrimSpace(string(b)); tok != "" {
				return tok
			}
		}
	}
	return a.cfg.InstallToken
}

// collectCmdbReport 采集主机基础属性并返回 CMDB 增量上报（每 60 秒一次）。
// 扩展采集内容：除原有 hostname/os/kernel 外，增加
//   - 服务列表（复用 monitoredServices + queryService，与 metrics_collect.go 共享逻辑）
//   - 中间件探测（检测常见中间件进程：mysql/redis/nginx/kafka 等）
//   - 网络拓扑（网卡列表 + IP + MAC，复用 gopsutil/net）
//
// Heartbeat 每 10 秒一次，但 CMDB 属性变化慢，降低采集频率减少系统开销。
// 采集结果仍通过心跳携带 CmdbReport 上报（现有机制）；未来可改为独立 gRPC ReportCMDB 方法。
func (a *Agent) collectCmdbReport() *proto.CmdbReport {
	if time.Since(a.cmdbLastCol) < 60*time.Second {
		return nil // 距上次采集不足 60s，跳过
	}
	a.cmdbLastCol = time.Now()
	a.cmdbSeq++

	var osRelease, kernel string
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				osRelease = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/ostype"); err == nil {
		kernel = strings.TrimSpace(string(b))
	}

	attrs := []proto.CmdbAttr{
		{Key: "hostname", Value: a.hostname, Type: "string"},
		{Key: "os.version", Value: osRelease, Type: "string"},
		{Key: "kernel.version", Value: kernel, Type: "string"},
	}
	// 扩展采集：服务列表 + 中间件探测 + 网络拓扑。
	attrs = append(attrs, collectCmdbServices()...)
	attrs = append(attrs, collectCmdbMiddleware()...)
	attrs = append(attrs, collectCmdbNetwork()...)

	return &proto.CmdbReport{
		CiType: "machine",
		Seq:    a.cmdbSeq,
		Attrs:  attrs,
	}
}

// collectCmdbServices 采集关注的服务列表作为 CMDB 属性。
// 复用 metrics_collect.go 中的 monitoredServices 白名单与 queryService 跨平台查询逻辑，
// 避免重复实现。每个服务产出两条属性：service.<name>.status（running/stopped）与
// service.<name>.enabled（true/false）。服务不存在或查询失败时跳过（降级而非报错）。
func collectCmdbServices() []proto.CmdbAttr {
	var attrs []proto.CmdbAttr
	for _, name := range monitoredServices {
		status, enabled := queryService(name)
		if status == "" {
			continue // 服务不存在或查询失败，跳过
		}
		attrs = append(attrs,
			proto.CmdbAttr{Key: "service." + name + ".status", Value: status, Type: "string"},
			proto.CmdbAttr{Key: "service." + name + ".enabled", Value: strconv.FormatBool(enabled), Type: "bool"},
		)
	}
	return attrs
}

// collectCmdbMiddleware 探测常见中间件进程作为 CMDB 属性。
// 跨平台：Linux/Darwin 用 ps aux，Windows 用 tasklist；任一失败返回 nil（降级）。
// 检测关键词：mysql/redis/nginx/kafka/zookeeper/etcd/consul/elasticsearch/rabbitmq/mongodb/postgres/grafana/prometheus。
// 命中即产出 middleware.<name>.installed=true 属性，供控制面 CMDB 自动登记中间件 CI。
func collectCmdbMiddleware() []proto.CmdbAttr {
	keywords := []string{
		"mysql", "redis", "nginx", "kafka", "zookeeper", "etcd", "consul",
		"elasticsearch", "rabbitmq", "mongodb", "postgres", "grafana", "prometheus",
	}
	var psOutput string
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist").Output()
		if err != nil {
			return nil // tasklist 不可用或失败，降级跳过
		}
		psOutput = string(out)
	} else {
		out, err := exec.Command("ps", "aux").Output()
		if err != nil {
			return nil // ps 不可用或失败，降级跳过
		}
		psOutput = string(out)
	}
	psLower := strings.ToLower(psOutput)
	// 按行匹配进程名而非全文子串，避免 /home/mysql_backup/script.sh 等路径误报 mysql。
	// - Linux/Darwin ps aux: 前 10 列为 USER PID %CPU %MEM VSZ RSS TTY STAT START TIME，
	//   第 11 列起为 COMMAND；取 COMMAND 首个 token 的 basename 作为进程名。
	//   另取末字段 basename 兼容 Java 中间件（进程名 java，类名/jar 在命令行末尾）。
	// - Windows tasklist: 第一列为映像名（如 mysql.exe）。
	// 权衡：仍用 Contains 匹配进程名，not_mysql_process 等罕见名仍可能误报，
	// 但已消除路径/参数/其他列的误报，显著优于全文 Contains。
	found := make(map[string]bool)
	for _, line := range strings.Split(psLower, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		var candidates []string
		if runtime.GOOS == "windows" {
			candidates = []string{fields[0]}
		} else {
			candidates = make([]string, 0, 2)
			if len(fields) >= 11 {
				candidates = append(candidates, filepath.Base(fields[10]))
			}
			candidates = append(candidates, filepath.Base(fields[len(fields)-1]))
		}
		for _, procName := range candidates {
			for _, kw := range keywords {
				if strings.Contains(procName, kw) {
					found[kw] = true
					break
				}
			}
		}
	}
	var attrs []proto.CmdbAttr
	for _, kw := range keywords {
		if found[kw] {
			attrs = append(attrs, proto.CmdbAttr{
				Key:   "middleware." + kw + ".installed",
				Value: "true",
				Type:  "bool",
			})
		}
	}
	return attrs
}

// collectCmdbNetwork 采集网络拓扑作为 CMDB 属性。
// 复用 gopsutil/net 获取网卡列表，跳过 loopback，产出 net.<iface>.ip 与 net.<iface>.mac 属性。
// isLoopback/firstIPv4 与 metrics_collect.go 共享（同包），避免重复实现。
func collectCmdbNetwork() []proto.CmdbAttr {
	ifaces, err := gnet.Interfaces()
	if err != nil {
		return nil // 采集失败，降级跳过
	}
	var attrs []proto.CmdbAttr
	for _, iface := range ifaces {
		if isLoopback(iface) {
			continue
		}
		ip := firstIPv4(iface)
		mac := iface.HardwareAddr
		attrs = append(attrs,
			proto.CmdbAttr{Key: "net." + iface.Name + ".ip", Value: ip, Type: "string"},
			proto.CmdbAttr{Key: "net." + iface.Name + ".mac", Value: mac, Type: "string"},
		)
	}
	return attrs
}

// Run 启动 agent：应用资源限额 -> 建 gRPC 通道 -> 注册 -> 启动 worker 池 + 心跳 + 调度循环，
// 直到收到终止信号优雅退出。
func (a *Agent) Run() error {
	a.applyRlimits() // 进程级资源限额（fork 炸弹 / 内存爆炸防护）

	// 显式打印目标机能力矩阵，避免“能注册但半数任务类型必挂”的错觉。
	// 冻结 Linux-only：非 Linux 能跑 shell，但 service(systemctl)/rlimit 不可用。
	logx.Info(context.Background(), "agent 能力矩阵", "os", runtime.GOOS, "note", capabilityNote(runtime.GOOS))

	// OTel 链路追踪初始化：endpoint 为空且 stdout=false 时 no-op（零开销）。
	// 服务名默认 "opsmesh-agent"（未配置时由 otelx 回退 "opsmesh"）。
	// 启用后 agent gRPC 调用 + 心跳 + 任务执行自动埋点，trace_id 贯穿 agent→控制面→store。
	// 初始化失败时返回 error（由 main.go 统一处理），而非 log.Fatalf 强制退出。
	otelShutdown, otelErr := otelx.Init(otelx.Config{
		Endpoint:    a.cfg.OTELEndpoint,
		ServiceName: firstNonEmptyAgent(a.cfg.OTELServiceName, "opsmesh-agent"),
		Stdout:      a.cfg.OTELStdout,
	})
	if otelErr != nil {
		return fmt.Errorf("OTel 初始化失败: %w", otelErr)
	}
	a.otelShutdown = otelShutdown
	if a.cfg.OTELEndpoint != "" || a.cfg.OTELStdout {
		logx.Info(context.Background(), "OTel 链路追踪已启用", "endpoint", a.cfg.OTELEndpoint, "stdout", a.cfg.OTELStdout, "service", a.cfg.OTELServiceName)
	}

	// 多控制面 failover：优先用逗号分隔的 --control-addrs（后也接受 --controlplane-endpoints，
	// config.Load 已合并到 ControlAddrs），为空则回退单地址 --control-addr（向后兼容）。
	addrs := strings.Split(a.cfg.ControlAddrs, ",")
	trimmed := make([]string, 0, len(addrs))
	for _, ad := range addrs {
		ad = strings.TrimSpace(ad)
		if ad != "" {
			trimmed = append(trimmed, ad)
		}
	}
	if len(trimmed) == 0 {
		trimmed = []string{a.cfg.ControlAddr}
	}
	cli, err := NewGRPCClient(trimmed, a.cfg.TLSCert, a.cfg.TLSKey, a.cfg.ClientCA, a.cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("连接控制面 gRPC 失败: %w", err)
	}
	a.grpc = cli
	defer cli.Close()
	// 服务发现：用 StaticDiscovery 加载多控制面地址，按 --lb-strategy 构造 balancer，
	// 注入 gRPC 客户端。单地址时 Failover 退化为始终用该地址（不破坏现有行为）。
	a.setupDiscoveryBalancer(trimmed)
	// OTel 优雅关闭：flush 残留 span 到导出器。defer 在 cli.Close() 之后注册，
	// 故执行顺序为先 shutdownOTel（flush span）再 cli.Close（关 gRPC），确保 span 上报完成。
	defer a.shutdownOTel()

	if err := a.register(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logx.WithTrace(ctx, a.agentID)

	// 启动 worker 池：并发执行被领取的任务。
	// 全部循环经 safeGo 包装（CB-9）：单循环 panic 不再拖垮整个 agent 进程，
	// 捕获后记日志并重启（详见 safego.go）。
	for i := 0; i < a.workers; i++ {
		safeGo(ctx, "worker", a.worker)
	}

	safeGo(ctx, "heartbeatLoop", a.heartbeatLoop)
	safeGo(ctx, "dispatchLoop", a.dispatchLoop)
	safeGo(ctx, "cancelLoop", a.cancelLoop)         // F3 取消信号：轮询控制面，中止已下发取消的正在执行任务
	safeGo(ctx, "logCollectLoop", a.logCollectLoop) // 日志采集：定时读取指定日志文件增量并上报控制面

	// 日志采集推送：构造 LogPusher 并启动（cfg.LogPushEnabled=true 时）。
	// 失败仅告警不阻塞启动（向后兼容，运维可后续修复配置后重启）。
	if a.cfg.LogPushEnabled {
		pusher, err := NewLogPusher(a.cfg.LogPushFiles, a.cfg.LogPushPattern,
			a.cfg.LogPushEndpoint, a.cfg.LogPushBackend, a.cfg.Segment, a.hostname)
		if err != nil {
			logx.Warn(ctx, "LogPusher 构造失败，日志推送未启用", "error", err.Error())
		} else {
			a.logPusher = pusher
			safeGo(ctx, "logPusher", func(ctx context.Context) {
				if err := pusher.Run(ctx); err != nil {
					logx.Warn(ctx, "LogPusher 退出异常", "error", err.Error())
				}
			})
		}
	}

	// 增强日志采集器（agent v2.0）：多路径/多行合并/过滤/限速/热更新。
	// 配置了日志采集路径（logCollectPaths 非空）时构造并启动 LogCollector，
	// 采集到的记录经 gRPC ReportLogs 上报到控制面。与 logCollectLoop（基于环境变量的简单增量上报）
	// 互补：logCollector 提供更完整的采集能力（过滤/多行/限速/热更新）。
	// 失败仅告警不阻塞启动（向后兼容）。
	if len(a.logCollectPaths) > 0 && a.grpc != nil {
		lcCfg := LogCollectConfig{
			Paths:    a.logCollectPaths,
			Interval: a.logCollectInterval,
		}
		lc, err := NewLogCollector(lcCfg, a.makeLogCollectPushFn())
		if err != nil {
			logx.Warn(ctx, "LogCollector 构造失败，增强日志采集未启用", "error", err.Error())
		} else {
			a.logCollector = lc
			safeGo(ctx, "logCollector", func(ctx context.Context) {
				if err := lc.Start(ctx); err != nil {
					logx.Warn(ctx, "LogCollector 退出异常", "error", err.Error())
				}
			})
		}
	}

	logx.Info(ctx, "agent 运行中", "agentID", a.agentID, "workers", a.workers, "grpc", a.cfg.ControlAddr)
	<-ctx.Done()
	logx.Info(ctx, "agent 收到终止信号，退出", "agentID", a.agentID)
	// 退出前 Stop LogPusher，触发剩余缓冲 flush（尽力而为，不阻塞退出）。
	if a.logPusher != nil {
		if err := a.logPusher.Stop(); err != nil {
			logx.Warn(ctx, "LogPusher Stop 失败", "error", err.Error())
		}
	}
	// 退出前 Stop LogCollector，等待采集 goroutine 退出（尽力而为，不阻塞退出）。
	if a.logCollector != nil {
		if err := a.logCollector.Stop(); err != nil {
			logx.Warn(ctx, "LogCollector Stop 失败", "error", err.Error())
		}
	}
	return nil
}

// applyRlimits 应用进程级资源限额。所有选项为 0 时不限制。
// 平台相关实现见 rlimit_unix.go（linux/darwin）与 rlimit_other.go（windows 等无 POSIX rlimit 的平台）。
func (a *Agent) applyRlimits() {
	setRlimits(a)
}

// setupDiscoveryBalancer 服务发现：用 StaticDiscovery 加载多控制面地址，
// 按 --lb-strategy 构造 balancer 并注入 gRPC 客户端。
//
// 逻辑：
//  1. 用 StaticDiscovery 从 addrs 构造控制面实例列表（服务名 "opsmesh-controlplane"）。
//  2. 用 List 获取实例列表，按 cfg.LBStrategy 构造 balancer（round-robin/failover）。
//  3. 通过 SetBalancer 注入 GRPCClient，invoke 后续优先走 balancer 路径。
//
// 单地址时 Failover 退化为始终用该地址（不破坏现有行为）。
// addrs 为空时不注入 balancer（回退到 addrs failover，向后兼容）。
func (a *Agent) setupDiscoveryBalancer(addrs []string) {
	if len(addrs) == 0 || a.grpc == nil {
		return
	}
	// 用逗号拼接的地址列表构造 StaticDiscovery（与 NewStaticDiscoveryFromAddrs 签名匹配）。
	joined := strings.Join(addrs, ",")
	disc := discovery.NewStaticDiscoveryFromAddrs("opsmesh-controlplane", joined)
	instances, err := disc.List(context.Background(), "opsmesh-controlplane")
	if err != nil || len(instances) == 0 {
		// 静态实现不应出错，但防御性处理：不注入 balancer，回退到 addrs failover。
		return
	}
	balancer := discovery.NewBalancer(a.cfg.LBStrategy, instances)
	a.grpc.SetBalancer(balancer)
	logx.Info(context.Background(), "服务发现已启用",
		"instances", len(instances), "lb-strategy", a.cfg.LBStrategy)
}

// capabilityNote 返回目标机能力矩阵提示（显式能力边界，避免“能注册但半数任务类型必挂”错觉）。
// 冻结 Linux-only：非 Linux 能跑 shell，但 service(systemctl)/rlimit 不可用，需在文档与启动日志明示。
func capabilityNote(goos string) string {
	if goos == "linux" {
		return "target=linux: 全能力（shell/service/systemctl + rlimit）"
	}
	return fmt.Sprintf("target=%s: 仅 shell 可用；service(systemctl)/rlimit 不可用（冻结 Linux-only，详见产品文档）", goos)
}

// register 经 gRPC Register 注册自身，拿到 agentID。带有限重试，避免控制面未就绪即退出。
func (a *Agent) register() error {
	info := proto.AgentInfo{
		AgentID:      a.agentID, // 上传持久化身份，服务端幂等复用（不重新分配，避免孤儿）
		Hostname:     a.hostname,
		Segment:      a.cfg.Segment,
		Addr:         a.cfg.Addr,
		GRPCPort:     a.cfg.GRPCPort,
		MetricsPort:  a.cfg.MetricsPort,
		Status:       "online",
		InstallToken: a.installToken(), // 自动纳管闭环：优先读文件，fallback 到配置
		OS:           runtime.GOOS,     // 目标机操作系统（控制面据此填充 DeviceInfo.OS）
		Arch:         runtime.GOARCH,   // 目标机 CPU 架构（控制面据此填充 DeviceInfo.Arch）
	}
	ctx := logx.WithTrace(context.Background(), "register")
	for i := 0; i < 30; i++ {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resp, err := a.grpc.Register(cctx, &info)
		cancel()
		if err == nil {
			a.agentID = resp.AgentID
			// gRPC agent 身份绑定：设置 HMAC 签名密钥。
			// 后续 PullTasks/ReportResult/PollCancels/Heartbeat 请求在 metadata 中携带签名，
			// 控制面据此验证 agent 身份，不再纯信任 agent 自报的 AgentID。

			// 安全加固：优先使用预共享密钥（--grpc-signature-key），Register 响应不再下发密钥。
			//   - 配置了 cfg.GRPCSignatureKey → 使用预共享密钥签名（推荐，防注册不硬时密钥外泄）。
			//   - 未配置但 resp.Secret 非空 → 回退到响应下发密钥（向后兼容旧控制面）。
			//   - 两者都为空 → 不签名（控制面未启用签名验证，demo 模式或未配置 --grpc-require-signature）。
			signed := false
			if a.cfg.GRPCSignatureKey != "" {
				a.grpc.SetSecret(a.cfg.GRPCSignatureKey)
				signed = true
			} else if resp.Secret != "" {
				a.grpc.SetSecret(resp.Secret)
				signed = true
			}
			if !signed && a.cfg.GRPCRequireSignature {
				// 控制面要求签名但 agent 既无预共享密钥也未从响应获取密钥：
				// 后续请求将被控制面拒绝。日志警告提示运维配置 --grpc-signature-key。
				logx.Warn(ctx, "控制面启用签名验证但 agent 未配置预共享密钥（--grpc-signature-key），"+
					"后续请求将被拒绝，请配置预共享密钥", "agentID", a.agentID)
			}
			logx.Info(ctx, "注册成功", "agentID", a.agentID, "segment", a.cfg.Segment, "grpc", a.cfg.ControlAddr, "signed", signed)
			return nil
		}
		// 安全：Unauthenticated（install token 无效/过期/已被消费）属不可重试错误，
		// 立即 fail-fast 而非盲重试 30 次空耗 90s——token 一次性语义下重试注定失败。
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unauthenticated {
			return fmt.Errorf("注册被拒绝（install token 无效/过期/已消费，需重新 provision）: %w", err)
		}
		logx.Warn(ctx, "注册失败，重试", "attempt", i+1, "error", err.Error())
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("注册控制面失败，已重试 30 次")
}

// heartbeatLoop 每 10s 经 gRPC Heartbeat 发送一次心跳（ctx 取消即退出）。
// 监控指标采集频率独立：每 30s 采集一次（cpu.Percent 等需要采样间隔，频繁调用消耗 CPU）。
// OTel：每次心跳创建 span（agent.heartbeat），trace_id 经 gRPC metadata 贯穿到控制面。
func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// OTel：为每次心跳创建 span，trace_id 贯穿 agent→控制面。
			spanCtx, span := otelx.StartSpan(ctx, "agent.heartbeat")
			req := &grpcx.HeartbeatReq{AgentID: a.agentID, Status: "online", Load: 1}
			// CMDB 增量上报：每 60 秒采集一次机器属性并附带在心跳中。
			if report := a.collectCmdbReport(); report != nil {
				req.CmdbReport = report
			}
			// 监控指标上报：每 30 秒采集一次系统指标（CPU/内存/磁盘/网络/OS/服务/进程）。
			// 心跳每 10s 一次，但采集频率独立降低以减少系统开销。
			if metrics := a.collectDeviceMetrics(); metrics != nil {
				req.Metrics = metrics
			}
			cctx, cancel := context.WithTimeout(spanCtx, 5*time.Second)
			err := a.grpc.Heartbeat(cctx, req)
			cancel()
			if err != nil {
				logx.Error(spanCtx, "心跳失败", err, "agentID", a.agentID)
				span.End()
				continue
			}
			logx.Info(spanCtx, "心跳 ok", "agentID", a.agentID)
			span.End()
		}
	}
}

// collectDeviceMetrics 采集系统监控指标（每 30s 一次，节流避免高频采集消耗 CPU）。
// 返回 nil 表示本轮跳过（距上次采集不足 30s）。deviceID 用 dev-<agentID> 与控制面 DeviceInfo 对齐。
// 采集成功时同时追加到本地环形缓冲（metricsHistory），保留最近 2h 历史供扩展查询。
func (a *Agent) collectDeviceMetrics() *proto.DeviceMetrics {
	if time.Since(a.metricsLastCol) < 30*time.Second {
		return nil // 距上次采集不足 30s，跳过
	}
	a.metricsLastCol = time.Now()
	m := CollectMetrics("dev-" + a.agentID)
	// 追加到本地环形缓冲（断网恢复后可重传 / 本地查询；当前心跳仅上报最新值，控制面侧也维护环形缓冲）。
	if m != nil && a.metricsHistory != nil {
		a.metricsHistory.Add(m)
	}
	return m
}

// dispatchLoop 每 15s 触发一次 drainTasks：尽量把当前所有 pending 任务领取并投入 worker 队列，
// 避免每轮只领 1 条导致大量任务排队过久（吞吐修复）。
func (a *Agent) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.drainTasks(ctx)
		}
	}
}

// drainTasks 循环原子领取 pending 任务并投入 taskCh，直到无更多任务或上下文取消。
// 阻塞式入队：worker 池并发消费腾出 channel 空间后继续领取，故一轮即可清空队列。
func (a *Agent) drainTasks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		t, err := a.claimTask(ctx)
		if err != nil {
			logx.Error(ctx, "领取任务失败", err, "agentID", a.agentID)
			return
		}
		if t == nil {
			return
		}
		select {
		case a.taskCh <- *t:
			logx.Info(ctx, "任务入队", "taskID", t.TaskID)
		case <-ctx.Done():
			return
		}
	}
}

// addRunning 登记一个正在执行的任务的控制句柄（F3 取消信号用）。
func (a *Agent) addRunning(id string, cancel context.CancelFunc) {
	a.runMu.Lock()
	a.running[id] = &runState{cancel: cancel}
	a.runMu.Unlock()
}

// delRunning 注销一个已结束的任务，返回其是否曾被取消（决定是否需要回写 store）。
func (a *Agent) delRunning(id string) bool {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	rs, ok := a.running[id]
	if !ok {
		return false
	}
	delete(a.running, id)
	return rs.cancelled
}

// cancelLoop F3 取消信号真正下达到 agent worker：每 2s 轮询控制面
// PollCancels 拿到本 agent 当前被取消的任务 ID，对正在执行的命中项立即 cancel 其 exec。
// 已取消的任务 worker 不会回写 store（避免误触重试/死信），store 侧状态保持 cancelled。
func (a *Agent) cancelLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ids, err := a.grpc.PollCancels(ctx, a.agentID)
			if err != nil {
				logx.Error(ctx, "轮询取消信号失败", err, "agentID", a.agentID)
				continue
			}
			if len(ids) == 0 {
				continue
			}
			a.runMu.Lock()
			for _, id := range ids {
				if rs, ok := a.running[id]; ok && !rs.cancelled {
					rs.cancelled = true
					if rs.cancel != nil {
						rs.cancel()
					}
					logx.Info(ctx, "收到取消信号，中止任务", "taskID", id)
				}
			}
			a.runMu.Unlock()
		}
	}
}

// claimTask 原子领取一条 pending 任务（控制面 ClaimTask 已翻转 pending→running，不会双领）。
func (a *Agent) claimTask(ctx context.Context) (*proto.Task, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tasks, err := a.grpc.PullTasks(cctx, a.agentID)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	return &tasks[0], nil
}

// worker 从任务队列取任务并执行、上报（ctx 取消即退出）。
// F3 取消信号：执行前登记任务控制句柄，结束后注销；若执行期间被控制面取消，
// 则丢弃结果不再回写 store（避免把 cancelled 任务误翻成 done/failed/死信）。
// 熔断器：通过熔断器 Execute 包裹任务执行（按 deviceID=agentID 隔离）。
// 熔断中（Open 状态）Execute 返回 ErrCircuitOpen，跳过任务执行，构造 "circuit breaker open"
// 错误结果上报控制面；熔断恢复（HalfOpen 探测成功→Closed）后自动重新执行任务。
// 禁用模式（CBFailureThreshold<=0，cbSet=nil）下直接执行，零开销向后兼容。
func (a *Agent) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-a.taskCh:
			// 熔断器：按 deviceID（即 agentID）隔离。
			// deviceID 取 t.AgentID（任务所属设备）；空时回退 a.agentID（自身）。
			deviceID := t.AgentID
			if deviceID == "" {
				deviceID = a.agentID
			}
			var res proto.TaskResult
			var cancelled bool
			// 节点级超时：任务自带 Timeout>0 时覆盖全局 taskTimeout，0=用全局。
			taskTimeout := taskTimeoutFor(t, a.taskTimeout)
			if a.cbSet != nil {
				// 通过熔断器执行：Execute 内部处理 Open→HalfOpen→Closed 状态机。
				cbErr := a.cbSet.Execute(deviceID, func() error {
					taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
					a.addRunning(t.TaskID, cancel)
					res = a.execute(taskCtx, t)
					cancelled = a.delRunning(t.TaskID)
					cancel()
					if cancelled {
						return nil // 取消不算失败，避免误熔断
					}
					if res.ExitCode != 0 {
						return fmt.Errorf("task exit code %d", res.ExitCode)
					}
					return nil
				})
				if cbErr == circuitbreaker.ErrCircuitOpen {
					logx.Warn(ctx, "熔断器开启，跳过任务执行", "taskID", t.TaskID, "deviceID", deviceID)
					res = proto.TaskResult{
						TaskID:     t.TaskID,
						AgentID:    a.agentID,
						ClaimEpoch: t.ClaimEpoch,
						ExitCode:   -1,
						Stderr:     "circuit breaker open for device " + deviceID,
						FinishedAt: time.Now(),
					}
				}
			} else {
				// 禁用模式：直接执行，零开销向后兼容。
				taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
				a.addRunning(t.TaskID, cancel)
				res = a.execute(taskCtx, t)
				cancelled = a.delRunning(t.TaskID)
				cancel()
			}
			if cancelled {
				logx.Info(ctx, "任务已取消，丢弃执行结果", "taskID", t.TaskID)
				continue
			}
			a.reportResult(ctx, t, res)
		}
	}
}

// taskTimeoutFor 计算任务的实际超时（节点级超时）：
//   - 任务自带 Timeout>0 时按此值（秒）覆盖全局 taskTimeout；
//   - Timeout=0 时回退全局 taskTimeout（向后兼容旧任务/旧 agent）。
//
// 抽成独立函数便于单测覆盖节点级 vs 全局回退两条路径。
func taskTimeoutFor(t proto.Task, global time.Duration) time.Duration {
	if t.Timeout > 0 {
		return time.Duration(t.Timeout) * time.Second
	}
	return global
}

// execute 本地执行任务，按 Type 分派执行器（shell / service / file，见 proto.TaskType* 常量）。
// 使用传入的 ctx（worker 已绑定 taskTimeout 与取消信号，F3 取消时 ctx 被取消、命令立即中断）。
// 安全加固：stdout/stderr 用 LimitedBuffer 限制 10MB，避免 cat 大文件耗尽 agent 内存。
// 防双跑：res.ClaimEpoch = t.ClaimEpoch，上报时携带持有者令牌，store 校验持有者是否仍为当前 epoch。
// OTel：为任务执行创建 span（agent.execute），记录 task.id/type/exit_code/duration，
// trace_id 贯穿 agent→控制面（ReportResult 经 gRPC 客户端拦截器注入 trace context）。
func (a *Agent) execute(ctx context.Context, t proto.Task) proto.TaskResult {
	// OTel：为任务执行创建 span，记录任务属性。
	// spanCtx 继承 ctx 的取消信号（taskTimeout + F3 取消），执行器用 spanCtx 使取消信号直达子进程。
	ctx, span := otelx.StartSpan(ctx, "agent.execute")
	defer span.End()
	span.SetAttributes(
		attribute.String("task.id", t.TaskID),
		attribute.String("task.type", t.Type),
	)

	start := time.Now()
	res := proto.TaskResult{TaskID: t.TaskID, AgentID: a.agentID, ClaimEpoch: t.ClaimEpoch, FinishedAt: time.Now()}

	stdout := newLimitedBuffer(maxOutputBytes)
	stderr := newLimitedBuffer(maxOutputBytes)
	var runErr error

	switch t.Type {
	case "", proto.TaskTypeShell:
		// 安全提示：shell 命令来自控制面下发，内核做命令白名单过滤，
		// 白名单为空时放行所有命令（向后兼容，demo/受信内网环境）。
		// 信任边界设计：service 类型已加 verb 白名单（见 execService），shell 类型可经 --agent-shell-whitelist 收紧。
		if t.Command == "" {
			res.ExitCode = -1
			res.Stderr = "empty command"
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		// 安全加固：shell 元字符检测——纵深防御，拒绝含高危元字符的命令。
		// 白名单只校验第一个 token 的 basename，无法阻止 "ls;rm -rf /" 这类元字符拼接绕过
		// （;后内容仍由同一 sh -c 解释执行）。此处前置拦截最高危元字符，与白名单互补。
		if err := checkShellMetachars(t.Command); err != nil {
			res.ExitCode = -1
			res.Stderr = err.Error()
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		// 安全加固：命令白名单检查（白名单为空时跳过，向后兼容）。
		if err := a.checkShellWhitelist(t.Command); err != nil {
			res.ExitCode = -1
			res.Stderr = err.Error()
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		runErr = a.executeShell(ctx, t.Command, stdout, stderr)
	case proto.TaskTypeService:
		// 冻结 Linux-only（前置拒绝）：service 任务依赖 systemctl，非 Linux 平台前置拒绝。
		// 原行为是接受任务但执行时因 systemctl 不存在而失败（exit code 非 0 + 重试/死信），
		// 前置拒绝更友好——立即返回明确错误，避免无意义的重试耗尽 MaxRetries 后进入死信。
		if runtime.GOOS != "linux" {
			res.ExitCode = -1
			res.Stderr = "service tasks are only supported on Linux (Linux-only freeze, systemctl unavailable on " + runtime.GOOS + ")"
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		runErr = a.execService(ctx, stdout, stderr, t)
	case proto.TaskTypeFile:
		runErr = a.execFile(ctx, stdout, stderr, t)
	default:
		res.ExitCode = -1
		res.Stderr = "unsupported task type: " + t.Type
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}

	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
			res.Stderr += "\n" + runErr.Error()
		}
		// OTel：执行失败时记录错误到 span。
		otelx.RecordError(span, runErr)
	} else {
		res.ExitCode = 0
	}
	res.DurationMs = time.Since(start).Milliseconds()
	// OTel：记录退出码与执行耗时到 span。
	span.SetAttributes(
		attribute.Int("task.exit_code", res.ExitCode),
		attribute.Int("task.duration_ms", int(res.DurationMs)),
	)
	return res
}

// maxOutputBytes 是单个任务 stdout/stderr 的最大字节数（10MB），超过即截断并追加提示。
const maxOutputBytes = 10 * 1024 * 1024

// limitedBuffer 是带上限的 io.Writer，超过 maxBytes 后丢弃后续写入并追加截断提示。
// 用于限制单个任务 stdout/stderr 内存占用，避免 cat 大文件耗尽 agent 内存。
type limitedBuffer struct {
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

// newLimitedBuffer 构造一个 max 字节上限的 limitedBuffer。
func newLimitedBuffer(max int) *limitedBuffer {
	return &limitedBuffer{maxBytes: max}
}

// Write 实现 io.Writer。超过上限后丢弃写入并一次性追加截断提示。
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.truncated {
		return len(p), nil // 已截断，静默丢弃后续写入
	}
	remaining := b.maxBytes - b.buf.Len()
	if remaining <= 0 {
		b.markTruncated()
		return len(p), nil
	}
	if len(p) <= remaining {
		return b.buf.Write(p)
	}
	b.buf.Write(p[:remaining])
	b.markTruncated()
	return len(p), nil
}

// markTruncated 追加截断提示（仅追加一次）。
func (b *limitedBuffer) markTruncated() {
	if b.truncated {
		return
	}
	b.truncated = true
	fmt.Fprintf(&b.buf, "\n...[output truncated at %dMB]...\n", b.maxBytes/1024/1024)
}

// String 返回已收集的内容（含截断提示）。
func (b *limitedBuffer) String() string {
	return b.buf.String()
}

// checkShellMetachars 检查命令是否含危险 shell 元字符（纵深防御）。
// 含则返回错误，拒绝执行。这是对白名单的补充：白名单只校验第一个 token 的 basename，
// 无法阻止 "ls;rm -rf /" 这类元字符拼接绕过（;后内容仍由同一 sh -c 解释执行）。
//
// 设计决策：仅拦截最高危元字符，平衡安全与可用性：
//   - "\n" "\r"  换行/回车符，高危（可拼接任意命令，如 "ls\nrm -rf /"），拦截。
//     此前漏检换行符导致 "ls\nrm -rf /" 可绕过白名单首 token 校验，安全加固补齐。
//   - ";"  命令分隔符，高危（可拼接任意命令），拦截。
//   - "&"  单个 & 为后台执行符，高危（可脱离 agent 控制），拦截；但允许以下合法模式：
//     "&&"（条件拼接，合法运维常用，如 systemctl status nginx && echo ok）、
//     ">&"/"&>"（fd 重定向/合并重定向，如 echo hello 1>&2、cmd &>file），
//     通过先剔除这些合法模式再检测剩余 & 实现。
//   - "$(" 命令替换注入，高危（可执行任意子命令），拦截。
//   - "`"  反引号命令替换，高危（可执行任意子命令），拦截。
//   - "|"  管道符暂不拦截——合法运维用途过多（如 systemctl status nginx | grep Active），
//     拦截会严重损害可用性。纵深防御应配合控制面侧 SubmitTask 校验 + IAM 鉴权。
//     如需拦截管道符，可在控制面侧 validateCommand 中按部署策略决定（可配置化演进）。
//
// 这是"最佳努力防御"——无法覆盖所有绕过路径（如 base64 编码命令），但能拦截最常见的拼接注入。
func checkShellMetachars(command string) error {
	// 安全加固：拦截换行/回车符，防 "ls\nrm -rf /" 绕过白名单首 token 校验。
	// 换行符在 sh -c 中等价于命令分隔符，可拼接任意命令，与 ";" 同属高危。
	if strings.Contains(command, "\n") {
		return errors.New("shell command contains dangerous metacharacters (newline '\\n'): command rejected")
	}
	if strings.Contains(command, "\r") {
		return errors.New("shell command contains dangerous metacharacters (carriage return '\\r'): command rejected")
	}
	if strings.Contains(command, ";") {
		return errors.New("shell command contains dangerous metacharacters (';'): command rejected")
	}
	// 检测单个 &（后台执行），允许合法模式：
	//   - "&&"  条件拼接（合法运维常用）
	//   - ">&"  fd 重定向/合并（如 1>&2、2>&1）
	//   - "&>"  重定向 stdout+stderr 到文件（如 cmd &>file）
	// 实现方式：将上述合法模式替换为空串后，若仍含 & 说明存在单个 & 后台执行符。
	cmd := command
	cmd = strings.ReplaceAll(cmd, ">&", "")
	cmd = strings.ReplaceAll(cmd, "&>", "")
	cmd = strings.ReplaceAll(cmd, "&&", "")
	if strings.Contains(cmd, "&") {
		return errors.New("shell command contains dangerous metacharacters ('&'): command rejected")
	}
	if strings.Contains(command, "$(") {
		return errors.New("shell command contains dangerous metacharacters ('$()'): command rejected")
	}
	if strings.Contains(command, "`") {
		return errors.New("shell command contains dangerous metacharacters (backtick): command rejected")
	}
	return nil
}

// checkShellWhitelist 检查命令是否在白名单内（安全加固）。
// 白名单为空时放行所有命令（向后兼容，demo/受信内网环境）。
// 白名单非空时，取命令第一个 token 的 basename，检查是否匹配白名单中某个条目。
//
// 匹配规则（安全加固修正，防前缀过宽绕过）：
//   - 条目以 "*" 结尾（如 "system*"）→ 前缀匹配：base 以 "system" 开头即放行（覆盖 systemctl/systemd-analyze 等）。
//   - 条目不含 "*" → 精确匹配：base 必须完全等于条目（如 "ls" 仅匹配 "ls"，不匹配 "lsusb"）。
//   - 条目中间含 "*" 视为普通字符（仅尾部 "*" 是通配符标记）。
//
// M6 集成：网络诊断命令（ping/traceroute/tracert/nslookup/curl/nc/powershell）
// 被加入内置白名单，即使 --agent-shell-whitelist 未显式包含这些命令也放行。
// 设计理由：网络诊断是运维平台核心能力，命令经控制面侧 validateCommand 校验 +
// checkShellMetachars 元字符拦截后风险可控；若管理员需禁用，可在控制面侧禁用网络诊断 API。
//
// 此前用 HasPrefix 做前缀匹配，导致白名单 "ls" 会放行 "lsusb"/"lsof" 等非预期命令，存在过宽风险。
//
// 注意：这是最佳努力防御，无法完全阻止 "ls;rm -rf /" 这类 shell 元字符拼接绕过
// （;后内容仍由同一 sh -c 解释执行）。纵深防御应配合 checkShellMetachars 元字符拦截 +
// 控制面侧 SubmitTask 校验 + IAM 鉴权 + 限制为非交互式单命令下发（无 sh -c 元字符）。
func (a *Agent) checkShellWhitelist(command string) error {
	wl := a.cfg.AgentShellWhitelist
	if wl == "" {
		return nil // 白名单为空，不限制（向后兼容）
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil // 空命令已在调用前拦截，此处兜底放行
	}
	first := fields[0]
	base := filepath.Base(first) // 取 basename（如 /bin/ls -> ls）
	// M6 集成：网络诊断命令内置白名单（即使 --agent-shell-whitelist 未包含也放行）。
	// 覆盖 ping/traceroute/tracert/nslookup/curl/nc/powershell 等网络诊断工具，
	// 使网络拓扑探测/诊断/连通性检测在白名单启用时仍可正常工作。
	if isNetworkDiagnoseCommand(base) {
		return nil
	}
	for _, entry := range strings.Split(wl, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// 安全加固：精确匹配为主，前缀匹配仅当条目以 "*" 结尾时启用。
		// "ls" 仅匹配 "ls"，"system*" 匹配 "systemctl" 等以 "system" 开头的命令。
		if strings.HasSuffix(entry, "*") {
			prefix := strings.TrimSuffix(entry, "*")
			if prefix == "" {
				continue // "*" 单独无意义，跳过
			}
			if strings.HasPrefix(base, prefix) {
				return nil
			}
		} else {
			if base == entry {
				return nil
			}
		}
	}
	return fmt.Errorf("command %q not in shell whitelist (allowed entries: %s)", base, wl)
}

// isNetworkDiagnoseCommand 判断命令 basename 是否为网络诊断命令（M6 集成）。
//
// 网络诊断命令内置白名单，即使 --agent-shell-whitelist 未显式包含也放行：
//   - ping / ping6：ICMP 探测（Linux/Windows/macOS 通用）
//   - traceroute / tracert：路由追踪（Linux: traceroute, Windows: tracert）
//   - nslookup / dig / host：DNS 查询
//   - curl / wget：HTTP 探测
//   - nc / netcat：TCP 连通性检测
//   - powershell：Windows PowerShell（用于 Test-NetConnection 等）
//
// 这些命令由控制面侧 network API 构造并经 validateCommand + checkShellMetachars 双重校验，
// 命令参数（target/count/timeout/port）已在控制面侧范围校验，风险可控。
func isNetworkDiagnoseCommand(base string) bool {
	switch base {
	case "ping", "ping6",
		"traceroute", "tracert",
		"nslookup", "dig", "host",
		"curl", "wget",
		"nc", "netcat",
		"powershell":
		return true
	default:
		return false
	}
}

// executeShell 启动 shell 子进程并等待其结束或 ctx 取消（安全加固）。
// 与原 shellCommandContext + cmd.Run 的区别：
//   - 设置 Setpgid=true（Linux/Darwin），使子进程成为新进程组 leader，
//     ctx 取消/超时时杀整个进程组（包括子进程 fork 出的后台进程），避免孤儿后台进程继续运行。
//   - Windows 上 Setpgid 无效，取消时用 cmd.Process.Kill() 杀父进程 + taskkill /T /F /PID 杀进程树。
func (a *Agent) executeShell(ctx context.Context, command string, stdout, stderr io.Writer) error {
	cmd := shellCommand(command)
	setProcessGroup(cmd) // 平台特定：Linux/Darwin 设 Setpgid；Windows noop
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case err := <-waitCh:
		return err
	case <-ctx.Done():
		// 取消/超时：先杀进程组/树（Linux: SIGTERM 进程组；Windows: taskkill /T /F /PID 进程树），
		// 再用 cmd.Process.Kill() 兜底杀父进程（确保终止，避免 taskkill 启动慢时父进程继续运行）。
		if err := killProcessGroup(cmd.Process.Pid); err != nil {
			log.Printf("agent: killProcessGroup(pid=%d) 失败: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("agent: Process.Kill(pid=%d) 失败: %v", cmd.Process.Pid, err)
		}
		// 等待 cmd.Wait 返回，回收子进程资源避免僵尸进程
		<-waitCh
		return ctx.Err()
	}
}

// execService 运行系统服务管理命令：Command 为 start|stop|restart|status；
// 服务名优先取 Path，否则从 Command 解析（例如 "restart nginx" -> systemctl restart nginx）。
func (a *Agent) execService(ctx context.Context, out, errb io.Writer, t proto.Task) error {
	verb := strings.TrimSpace(t.Command)
	if verb == "" {
		return fmt.Errorf("service task requires command (start|stop|restart|status)")
	}
	svc := strings.TrimSpace(t.Path)
	if svc == "" {
		fields := strings.Fields(verb)
		if len(fields) >= 2 {
			verb = fields[0]
			svc = fields[1]
		} else {
			svc = verb
			verb = "status"
		}
	}
	// service verb 白名单：verb 解析完成后、执行 systemctl 前校验，
	// 拒绝任意 verb 注入（防止 "cat /etc/shadow" 等经 Command 字段拼装绕过）。
	if !serviceVerbWhitelist[verb] {
		return fmt.Errorf("service verb %q not allowed (whitelist: start|stop|restart|status|reload|enable|disable|is-active|is-enabled)", verb)
	}
	c := exec.CommandContext(ctx, "systemctl", verb, svc)
	c.Stdout = out
	c.Stderr = errb
	return c.Run()
}

// serviceVerbWhitelist 是 systemctl 允许的动词白名单。
// 仅放行只读/常规服务管理动词，拒绝任意 verb 注入 systemctl（如 "cat /etc/shadow" 经 verb 字段拼装）。
// 如需扩展（如 mask/unmask/daemon-reload）应经安全评审后显式新增，切勿放行任意字符串。
var serviceVerbWhitelist = map[string]bool{
	"start":      true,
	"stop":       true,
	"restart":    true,
	"status":     true,
	"reload":     true,
	"enable":     true,
	"disable":    true,
	"is-active":  true,
	"is-enabled": true,
}

// execFile 原子写入文件：先写同目录临时文件，再 rename 到目标路径（Path），避免半写文件。
// 安全加固：路径遍历防护——
//   - 先 filepath.Clean 规范化原始路径，Clean 后仍含 ".." 说明试图逃逸根目录，拒绝；
//   - 再 filepath.Abs 解析为绝对路径（Abs 内部会再 Clean 一次，确保路径规范）；
//   - 用 os.Lstat 检查符号链接，拒绝（避免经符号链接逃逸到任意路径）；
//   - 可选 --agent-file-root-whitelist 限制允许操作的根目录（主要路径遍历防护）。
func (a *Agent) execFile(ctx context.Context, out, errb io.Writer, t proto.Task) error {
	_ = ctx
	if t.Path == "" {
		return fmt.Errorf("file task requires path")
	}
	// 路径遍历防护：先 Clean 原始路径，检查是否残留 ".."。
	// 纯相对路径如 "../../etc/passwd" Clean 后仍含 ".."，说明试图逃逸，拒绝。
	// 绝对路径如 "/var/opsmesh/../../etc/passwd" Clean 后不含 ".."（已解析），靠根目录白名单防护。
	cleanRaw := filepath.Clean(t.Path)
	if strings.Contains(cleanRaw, "..") {
		return fmt.Errorf("path traversal detected: %q (cleaned: %q)", t.Path, cleanRaw)
	}
	// 解析绝对路径（Abs 内部会再 Clean，确保路径规范）。
	abs, err := filepath.Abs(cleanRaw)
	if err != nil {
		return fmt.Errorf("resolve absolute path %q: %w", t.Path, err)
	}
	clean := filepath.Clean(abs)
	// 拒绝符号链接（避免经符号链接逃逸到任意路径）。
	if fi, err := os.Lstat(clean); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink not allowed: %q -> %q", t.Path, clean)
	}
	// 根目录白名单检查（可选，白名单为空时跳过；主要路径遍历防护）。
	if err := a.checkFileRootWhitelist(clean); err != nil {
		return err
	}
	dir := filepath.Dir(clean)
	tmp, err := os.CreateTemp(dir, ".opsmesh-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err := os.Remove(tmpName); err != nil && !os.IsNotExist(err) {
			log.Printf("agent: 清理临时文件 %s 失败: %v", tmpName, err)
		}
	}()
	if _, err := tmp.WriteString(t.Content); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			log.Printf("agent: 写临时文件失败后关闭句柄失败: %v", cerr)
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, clean); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %d bytes to %s\n", len(t.Content), clean)
	return nil
}

// checkFileRootWhitelist 检查路径是否落在允许的根目录之下（安全加固）。
// 白名单为空时不限制（向后兼容，仍拒绝 ../ 与符号链接）。
// 白名单非空时，路径必须落在某个根目录之下（用 filepath.Rel 判断相对路径不以 ".." 开头）。
func (a *Agent) checkFileRootWhitelist(path string) error {
	wl := a.cfg.AgentFileRootWhitelist
	if wl == "" {
		return nil
	}
	for _, root := range strings.Split(wl, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootClean := filepath.Clean(rootAbs)
		rel, err := filepath.Rel(rootClean, path)
		if err != nil {
			continue
		}
		// rel 不以 ".." 开头说明 path 在 rootClean 之下（含相等）。
		if !strings.HasPrefix(rel, "..") {
			return nil
		}
	}
	return fmt.Errorf("path %q not under any allowed root (whitelist: %s)", path, wl)
}

// reportResult 把执行结果经 gRPC ReportResult 上报控制面。
func (a *Agent) reportResult(ctx context.Context, t proto.Task, res proto.TaskResult) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.grpc.ReportResult(cctx, &res); err != nil {
		logx.Error(ctx, "上报结果失败", err, "taskID", t.TaskID)
		return
	}
	logx.Info(ctx, "任务完成", "taskID", t.TaskID, "exitCode", res.ExitCode, "durationMs", res.DurationMs)
}

// shellCommand 按操作系统选择 shell 构造子进程（取消信号由 executeShell 监听 ctx 后
// 经 killProcessGroup 杀整个进程组，不再用 exec.CommandContext 直接绑定 ctx，因为后者只杀父进程
// 不杀子进程 fork 出的后台进程）。
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("sh", "-c", command)
}

// ===== 日志采集 agent 推送 =====
//
// makeLogCollectPushFn 构造 LogCollector 的上报回调：把采集到的多行记录批量经 gRPC ReportLogs
// 上报到控制面。每条 CollectedLog 按其 Content 切分（多行合并后的正文）封装为 LogLine，
// 同一文件的记录合并为单个 LogReport 批次上报（减少 gRPC 调用次数）。
//
// 上报失败返回 error（LogCollector tick 会记录告警但不中断后续采集）。
// 控制面校验 HMAC 签名后按 agent 归属租户落库（行级隔离）。
func (a *Agent) makeLogCollectPushFn() LogCollectPushFunc {
	return func(ctx context.Context, records []CollectedLog) error {
		if len(records) == 0 || a.grpc == nil {
			return nil
		}
		// 按文件分组，减少 gRPC 调用次数。
		groups := make(map[string][]CollectedLog)
		order := make([]string, 0)
		for _, r := range records {
			if _, ok := groups[r.File]; !ok {
				order = append(order, r.File)
			}
			groups[r.File] = append(groups[r.File], r)
		}
		for _, file := range order {
			recs := groups[file]
			lines := make([]proto.LogLine, 0, len(recs))
			now := time.Now()
			for _, r := range recs {
				lines = append(lines, proto.LogLine{
					Timestamp: r.Timestamp,
					Level:     parseLogLevel(r.StartLine),
					Message:   r.Content,
				})
			}
			report := &proto.LogReport{
				AgentID:     a.agentID,
				Hostname:    a.hostname,
				LogName:     filepath.Base(file),
				Lines:       lines,
				CollectedAt: now,
			}
			// 短超时（5s）避免控制面不可达时阻塞采集循环。
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			rerr := a.grpc.ReportLogs(cctx, report)
			cancel()
			if rerr != nil {
				return rerr
			}
		}
		return nil
	}
}

// logCollectLoop 定时读取指定日志文件的新增内容（基于文件 offset）并上报控制面。
// 配置来自环境变量（见 initLogCollect）：
//   - OPSMESH_LOG_COLLECT_PATHS：逗号分隔的日志文件路径。
//   - OPSMESH_LOG_COLLECT_INTERVAL：采集间隔（默认 30s）。
//
// 未配置路径时不启动（直接 return），避免空循环开销。
//
// 上报通道：经 gRPC ReportLogs 上报到控制面（见 collectAndReportLogs）。
// 控制面校验 HMAC 签名后按 agent 归属租户落库（行级隔离）。
func (a *Agent) logCollectLoop(ctx context.Context) {
	if len(a.logCollectPaths) == 0 {
		return // 未配置日志采集路径，不启动
	}
	logx.Info(ctx, "日志采集启动", "paths", a.logCollectPaths, "interval", a.logCollectInterval)
	ticker := time.NewTicker(a.logCollectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.collectAndReportLogs(ctx)
		}
	}
}

// collectAndReportLogs 遍历所有配置的日志路径，读取增量内容并经 gRPC ReportLogs 上报控制面。
// 单个文件读取失败不中断整体，仅记录告警并继续下一个文件（降级而非报错）。
// 上报失败记录日志但不中断循环（agent 不因上报失败崩溃，下次循环重试）。
// 每条日志按行切分，启发式解析级别（ERROR/WARN/INFO/DEBUG），封装为 LogReport 批次上报。
func (a *Agent) collectAndReportLogs(ctx context.Context) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	for _, path := range a.logCollectPaths {
		content, err := a.readLogIncrement(path)
		if err != nil {
			if !os.IsNotExist(err) {
				logx.Warn(ctx, "读取日志增量失败", "path", path, "error", err.Error())
			}
			continue // 文件不存在或读取失败，跳过（文件可能尚未创建）
		}
		if content == "" {
			continue // 无新增内容
		}
		// 按行切分并封装为 LogReport 批次上报。
		lines := parseLogLines(content)
		if len(lines) == 0 {
			continue
		}
		report := &proto.LogReport{
			AgentID:     a.agentID,
			Hostname:    a.hostname,
			LogName:     filepath.Base(path), // 用文件名作为日志标识（如 syslog / app.log）
			Lines:       lines,
			CollectedAt: time.Now(),
		}
		// 经 gRPC ReportLogs 上报到控制面。
		// 短超时（5s）避免控制面不可达时阻塞采集循环；失败仅记录日志，不中断后续文件采集。
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rerr := a.grpc.ReportLogs(cctx, report)
		cancel()
		if rerr != nil {
			logx.Warn(ctx, "日志上报失败", "path", path, "bytes", len(content), "lines", len(lines), "error", rerr.Error())
			continue // 上报失败不中断，下次循环重试
		}
		logx.Info(ctx, "日志采集增量已上报", "path", path, "bytes", len(content), "lines", len(lines))
	}
}

// parseLogLines 把日志增量内容按行切分为 LogLine 切片。
// 启发式解析级别：行首以 ERROR/FATAL → error，WARN/WARNING → warn，DEBUG/TRACE → debug，其余 → info。
// 空行跳过；行尾换行已由 Split 去除。Timestamp 用采集时刻（无法从行首解析时间戳的降级方案）。
func parseLogLines(content string) []proto.LogLine {
	trimmed := strings.TrimRight(content, "\r\n")
	if trimmed == "" {
		return nil
	}
	rawLines := strings.Split(trimmed, "\n")
	now := time.Now()
	out := make([]proto.LogLine, 0, len(rawLines))
	for _, raw := range rawLines {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		out = append(out, proto.LogLine{
			Timestamp: now,
			Level:     parseLogLevel(line),
			Message:   line,
		})
	}
	return out
}

// parseLogLevel 启发式从日志行首解析级别。
// 识别常见前缀：ERROR/ERR/FATAL → error，WARN/WARNING → warn，DEBUG/TRACE → debug，其余 → info。
// 大小写不敏感；仅检查行首前缀（避免误匹配行中部的关键字）。
func parseLogLevel(line string) string {
	// 取行首第一个空白分隔的 token 做级别判定（如 "ERROR 2024-01-01 ..." 的 ERROR）。
	upper := strings.ToUpper(strings.TrimSpace(line))
	// 取前缀 token（截止第一个空白）。
	end := strings.IndexAny(upper, " \t")
	token := upper
	if end > 0 {
		token = upper[:end]
	}
	switch {
	case strings.HasPrefix(token, "ERROR") || strings.HasPrefix(token, "ERR") || strings.HasPrefix(token, "FATAL"):
		return "error"
	case strings.HasPrefix(token, "WARN") || strings.HasPrefix(token, "WARNING"):
		return "warn"
	case strings.HasPrefix(token, "DEBUG") || strings.HasPrefix(token, "TRACE"):
		return "debug"
	default:
		return "info"
	}
}

// readLogIncrement 读取指定日志文件自上次 offset 之后的新增内容。
// 基于 offset 增量读取：记录上次读取到的文件大小作为 offset，下次从 offset 处读到文件末尾。
// 处理文件轮转：若当前文件大小小于已记录的 offset，说明文件被截断或轮转，重置 offset 从头读。
// 返回新增内容字符串；无新增时返回空串。
//
// 安全（OOM 防护）：单次读取上限 1MB（io.LimitReader），避免日志暴增导致 agent OOM。
// 若新增内容超过 1MB，本次仅返回前 1MB，offset 推进到 offset+1MB，剩余内容下次循环再读。
func (a *Agent) readLogIncrement(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	offset := a.logOffsets[path]
	// 文件被截断或轮转（当前大小 < 已记录 offset），重置 offset 从头读。
	if fi.Size() < offset {
		offset = 0
	}
	// 无新增内容。
	if fi.Size() == offset {
		return "", nil
	}
	// 定位到 offset 处读取到文件末尾（上限 1MB 防 OOM）。
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return "", err
	}
	// 更新 offset 为本次读取结束位置，下次从此处继续读。
	// 用 offset+len(data) 而非 fi.Size()：若本次因 1MB 上限未读完，fi.Size() 会跳过未读内容。
	a.logOffsets[path] = offset + int64(len(data))
	return string(data), nil
}
