// Package agent 实现网段 agent：经真实 gRPC(9090) 注册、周期心跳、拉取任务、本地执行(os/exec)、上报结果。
// U-05: 通过 --mode=agent 启动；与控制面共用同一份二进制/镜像。
// 四条通道（注册/心跳/拉任务/上报结果）全部走 gRPC 9090（JSON codec，见 grpcclient.go），
// 不依赖 protobuf 代码生成。
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opsmesh/internal/config"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/logx"
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
	dataDir        string // agent.id 落盘目录（P0-2 身份持久化）
	taskTimeout    time.Duration
	workers        int
	taskCh         chan proto.Task      // 待执行任务队列（worker 池消费，P1-3）
	runMu          sync.Mutex           // 保护 running 的并发读写
	running        map[string]*runState // 当前正在执行的任务 ID -> 控制句柄（F3）
	cmdbSeq        int64                // CMDB 上报序列号（agent 侧递增）
	cmdbLastCol    time.Time            // 上次 CMDB 采集时间（每 60s 采集一次）
	metricsLastCol time.Time            // 上次监控指标采集时间（每 30s 采集一次）
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
	return &Agent{
		cfg:         cfg,
		hostname:    h,
		dataDir:     cfg.DataDir,
		agentID:     loadOrCreateAgentID(cfg.DataDir, h), // P0-2 身份持久化：重启沿用稳定 ID
		taskTimeout: cfg.TaskTimeout,
		workers:     w,
		taskCh:      make(chan proto.Task, w*2),
		running:     make(map[string]*runState),
	}
}

// loadOrCreateAgentID 持久化 agent 身份（P0-2）：优先从 <dir>/agent.id 读取稳定 ID，
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

// installToken 读取 install token（B1 自动纳管闭环）。
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
// 包含：hostname / os / kernel / cpu_cores / mem_total_gb。
// Heartbeat 每 10 秒一次，但 CMDB 属性变化慢，降低采集频率减少系统开销。
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

	return &proto.CmdbReport{
		CiType: "machine",
		Seq:    a.cmdbSeq,
		Attrs: []proto.CmdbAttr{
			{Key: "hostname", Value: a.hostname, Type: "string"},
			{Key: "os.version", Value: osRelease, Type: "string"},
			{Key: "kernel.version", Value: kernel, Type: "string"},
		},
	}
}

// Run 启动 agent：应用资源限额 -> 建 gRPC 通道 -> 注册 -> 启动 worker 池 + 心跳 + 调度循环，
// 直到收到终止信号优雅退出。
func (a *Agent) Run() error {
	a.applyRlimits() // P0-3 进程级资源限额（fork 炸弹 / 内存爆炸防护）

	// B4：显式打印目标机能力矩阵，避免“能注册但半数任务类型必挂”的错觉。
	// U-02 冻结 Linux-only：非 Linux 能跑 shell，但 service(systemctl)/rlimit 不可用。
	logx.Info(context.Background(), "agent 能力矩阵", "os", runtime.GOOS, "note", capabilityNote(runtime.GOOS))

	// A3 多控制面 failover：优先用逗号分隔的 --control-addrs，
	// 为空则回退单地址 --control-addr（向后兼容）。
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

	if err := a.register(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logx.WithTrace(ctx, a.agentID)

	// 启动 worker 池（P1-3）：并发执行被领取的任务。
	for i := 0; i < a.workers; i++ {
		go a.worker(ctx)
	}

	go a.heartbeatLoop(ctx)
	go a.dispatchLoop(ctx)
	go a.cancelLoop(ctx) // F3 取消信号：轮询控制面，中止已下发取消的正在执行任务

	logx.Info(ctx, "agent 运行中", "agentID", a.agentID, "workers", a.workers, "grpc", a.cfg.ControlAddr)
	<-ctx.Done()
	logx.Info(ctx, "agent 收到终止信号，退出", "agentID", a.agentID)
	return nil
}

// applyRlimits 应用进程级资源限额（P0-3）。所有选项为 0 时不限制。
// 平台相关实现见 rlimit_unix.go（linux/darwin）与 rlimit_other.go（windows 等无 POSIX rlimit 的平台）。
func (a *Agent) applyRlimits() {
	setRlimits(a)
}

// capabilityNote 返回目标机能力矩阵提示（B4：显式能力边界，避免“能注册但半数任务类型必挂”错觉）。
// U-02 冻结 Linux-only：非 Linux 能跑 shell，但 service(systemctl)/rlimit 不可用，需在文档与启动日志明示。
func capabilityNote(goos string) string {
	if goos == "linux" {
		return "target=linux: 全能力（shell/service/systemctl + rlimit）"
	}
	return fmt.Sprintf("target=%s: 仅 shell 可用；service(systemctl)/rlimit 不可用（U-02 冻结 Linux-only，详见产品文档）", goos)
}

// register 经 gRPC Register 注册自身，拿到 agentID。带有限重试，避免控制面未就绪即退出。
func (a *Agent) register() error {
	info := proto.AgentInfo{
		AgentID:      a.agentID, // P0-2 上传持久化身份，服务端幂等复用（不重新分配，避免孤儿）
		Hostname:     a.hostname,
		Segment:      a.cfg.Segment,
		Addr:         a.cfg.Addr,
		GRPCPort:     a.cfg.GRPCPort,
		MetricsPort:  a.cfg.MetricsPort,
		Status:       "online",
		InstallToken: a.installToken(), // B1 自动纳管闭环：优先读文件，fallback 到配置
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
			// task 81 gRPC agent 身份绑定：保存控制面下发的 HMAC 签名密钥。
			// 后续 PullTasks/ReportResult/PollCancels/Heartbeat 请求在 metadata 中携带签名，
			// 控制面据此验证 agent 身份，不再纯信任 agent 自报的 AgentID。
			// resp.Secret 为空表示控制面未启用签名验证（demo 模式或未配置 --grpc-require-signature），agent 不签名。
			if resp.Secret != "" {
				a.grpc.SetSecret(resp.Secret)
			}
			logx.Info(ctx, "注册成功", "agentID", a.agentID, "segment", a.cfg.Segment, "grpc", a.cfg.ControlAddr, "signed", resp.Secret != "")
			return nil
		}
		// 安全（P1-F5）：Unauthenticated（install token 无效/过期/已被消费）属不可重试错误，
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
func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := a.grpc.Heartbeat(cctx, req)
			cancel()
			if err != nil {
				logx.Error(ctx, "心跳失败", err, "agentID", a.agentID)
				continue
			}
			logx.Info(ctx, "心跳 ok", "agentID", a.agentID)
		}
	}
}

// collectDeviceMetrics 采集系统监控指标（每 30s 一次，节流避免高频采集消耗 CPU）。
// 返回 nil 表示本轮跳过（距上次采集不足 30s）。deviceID 用 dev-<agentID> 与控制面 DeviceInfo 对齐。
func (a *Agent) collectDeviceMetrics() *proto.DeviceMetrics {
	if time.Since(a.metricsLastCol) < 30*time.Second {
		return nil // 距上次采集不足 30s，跳过
	}
	a.metricsLastCol = time.Now()
	return CollectMetrics("dev-" + a.agentID)
}

// dispatchLoop 每 15s 触发一次 drainTasks：尽量把当前所有 pending 任务领取并投入 worker 队列，
// 避免每轮只领 1 条导致大量任务排队过久（P1-6 吞吐修复）。
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
// 阻塞式入队：worker 池并发消费腾出 channel 空间后继续领取，故一轮即可清空队列（P1-6）。
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
func (a *Agent) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-a.taskCh:
			taskCtx, cancel := context.WithTimeout(ctx, a.taskTimeout)
			a.addRunning(t.TaskID, cancel)
			res := a.execute(taskCtx, t)
			cancelled := a.delRunning(t.TaskID)
			cancel() // 释放 timer（cancelLoop 已可能触发过，幂等）
			if cancelled {
				logx.Info(ctx, "任务已取消，丢弃执行结果", "taskID", t.TaskID)
				continue
			}
			a.reportResult(ctx, t, res)
		}
	}
}

// execute 本地执行任务，按 Type 分派执行器（shell / service / file，见 proto.TaskType* 常量）。
// 使用传入的 ctx（worker 已绑定 taskTimeout 与取消信号，F3 取消时 ctx 被取消、命令立即中断）。
// 安全加固（task 78）：stdout/stderr 用 LimitedBuffer 限制 10MB，避免 cat 大文件耗尽 agent 内存。
func (a *Agent) execute(ctx context.Context, t proto.Task) proto.TaskResult {
	start := time.Now()
	res := proto.TaskResult{TaskID: t.TaskID, AgentID: a.agentID, FinishedAt: time.Now()}

	_ = ctx // ctx 由 shell/service/file 执行器经 exec.CommandContext 消费（取消信号直达子进程）

	stdout := newLimitedBuffer(maxOutputBytes)
	stderr := newLimitedBuffer(maxOutputBytes)
	var runErr error

	switch t.Type {
	case "", proto.TaskTypeShell:
		// 安全提示（P1-10 / H4-M8）：shell 命令来自控制面下发，内核做命令白名单过滤（task 78），
		// 白名单为空时放行所有命令（向后兼容，demo/受信内网环境）。
		// 信任边界设计：service 类型已加 verb 白名单（见 execService），shell 类型可经 --agent-shell-whitelist 收紧。
		if t.Command == "" {
			res.ExitCode = -1
			res.Stderr = "empty command"
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		// 安全加固（task 78）：命令白名单检查（白名单为空时跳过，向后兼容）。
		if err := a.checkShellWhitelist(t.Command); err != nil {
			res.ExitCode = -1
			res.Stderr = err.Error()
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		runErr = a.executeShell(ctx, t.Command, stdout, stderr)
	case proto.TaskTypeService:
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
	} else {
		res.ExitCode = 0
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

// maxOutputBytes 是单个任务 stdout/stderr 的最大字节数（10MB），超过即截断并追加提示（task 78）。
const maxOutputBytes = 10 * 1024 * 1024

// limitedBuffer 是带上限的 io.Writer，超过 maxBytes 后丢弃后续写入并追加截断提示。
// 用于限制单个任务 stdout/stderr 内存占用，避免 cat 大文件耗尽 agent 内存（task 78）。
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

// checkShellWhitelist 检查命令是否在白名单内（task 78 安全加固）。
// 白名单为空时放行所有命令（向后兼容，demo/受信内网环境）。
// 白名单非空时，取命令第一个 token 的 basename，检查是否匹配白名单中某个前缀。
// 注意：这是最佳努力防御，无法完全阻止 "ls;rm -rf /" 这类 shell 元字符拼接绕过
// （;后内容仍由同一 sh -c 解释执行）。纵深防御应配合控制面侧 SubmitTask 校验 + IAM 鉴权 +
// 限制为非交互式单命令下发（无 sh -c 元字符）。本检查覆盖"第一个程序名不在白名单"的常见误用与
// 控制面被攻陷后下发任意命令的场景。
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
	for _, prefix := range strings.Split(wl, ",") {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		// 精确匹配或前缀匹配（如白名单 "system" 允许 "systemctl"）
		if base == prefix || strings.HasPrefix(base, prefix) {
			return nil
		}
	}
	return fmt.Errorf("command %q not in shell whitelist (allowed prefixes: %s)", base, wl)
}

// executeShell 启动 shell 子进程并等待其结束或 ctx 取消（task 78 安全加固）。
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
	// H4/M8 service verb 白名单：verb 解析完成后、执行 systemctl 前校验，
	// 拒绝任意 verb 注入（防止 "cat /etc/shadow" 等经 Command 字段拼装绕过）。
	if !serviceVerbWhitelist[verb] {
		return fmt.Errorf("service verb %q not allowed (whitelist: start|stop|restart|status|reload|enable|disable|is-active|is-enabled)", verb)
	}
	c := exec.CommandContext(ctx, "systemctl", verb, svc)
	c.Stdout = out
	c.Stderr = errb
	return c.Run()
}

// serviceVerbWhitelist 是 systemctl 允许的动词白名单（H4/M8 安全加固）。
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
// 安全加固（task 78）：路径遍历防护——
//   - 先 filepath.Clean 规范化原始路径，Clean 后仍含 ".." 说明试图逃逸根目录，拒绝；
//   - 再 filepath.Abs 解析为绝对路径（Abs 内部会再 Clean 一次，确保路径规范）；
//   - 用 os.Lstat 检查符号链接，拒绝（避免经符号链接逃逸到任意路径）；
//   - 可选 --agent-file-root-whitelist 限制允许操作的根目录（主要路径遍历防护）。
func (a *Agent) execFile(ctx context.Context, out, errb io.Writer, t proto.Task) error {
	_ = ctx
	if t.Path == "" {
		return fmt.Errorf("file task requires path")
	}
	// 路径遍历防护（task 78）：先 Clean 原始路径，检查是否残留 ".."。
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
	// 根目录白名单检查（task 78：可选，白名单为空时跳过；主要路径遍历防护）。
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

// checkFileRootWhitelist 检查路径是否落在允许的根目录之下（task 78 安全加固）。
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

// shellCommand 按操作系统选择 shell 构造子进程（task 78：取消信号由 executeShell 监听 ctx 后
// 经 killProcessGroup 杀整个进程组，不再用 exec.CommandContext 直接绑定 ctx，因为后者只杀父进程
// 不杀子进程 fork 出的后台进程）。
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("sh", "-c", command)
}
