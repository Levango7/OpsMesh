package controlplane

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"opsmesh/internal/authctx"
	"opsmesh/internal/cmdb"
	"opsmesh/internal/config"
	"opsmesh/internal/discover"
	"opsmesh/internal/domain"
	"opsmesh/internal/events"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/logstore"
	"opsmesh/internal/logx"
	"opsmesh/internal/metrics"
	"opsmesh/internal/proto"
)

// grpcServerImpl 实现 grpcx.RegistrationServer 接口，把四条 gRPC 通道转发到 Registry。
type grpcServerImpl struct {
	reg         *Registry
	requireAuth bool
	cfg         *config.Config // 可为 nil（测试）；非 nil 时启用网段发现（P0-2）
	bus         events.Bus     // 可为 nil（测试）；非 nil 时发布审计/告警事件（P1-5）
	metrics     *metrics.M     // 可为 nil（测试）；非 nil 时更新观测指标（P2-1）
	cmdb        *cmdb.Handler   // CMDB 处理器（Phase 1）；nil 时不处理 CmdbReport
	logs        *logstore.Handler // M6 日志检索处理器；nil 时不落地任务日志
}

// Register 注册：调用 Registry.Register，返回分配到的 agentID 与控制面下发配置。
// 服务端按网关注入租户给 AgentInfo.TenantID 盖章（agent 不可伪造所属租户）。
// 入站 proto 经防腐层（ACL）转 domain，业务处理在 domain 上进行（P2-18 贯穿边界）。
func (g *grpcServerImpl) Register(ctx context.Context, info *proto.AgentInfo) (*grpcx.RegisterResp, error) {
	// 经防腐层：传输模型 -> 领域模型。
	dom := domain.AgentFromProto(info)
	var actx authctx.Context
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		actx = authctx.FromGRPCMetadata(md)
	}
	// B1 自动纳管闭环：install token 优先于网关租户头——token 权威。
	// agent 经 bootstrap 安装后携带一次性 install token 注册，该 token 由 Provision 签发，
	// 经 ConsumeToken 校验通过后，回填对应候选设备的 deviceID 并强制以 token 内租户为准。
	if info.InstallToken != "" {
		devID, tokTenant, tokOK := g.reg.ConsumeToken(info.InstallToken)
		if !tokOK {
			// U-04：认证失败也要留痕（B1 token 校验失败属认证事件）。
			g.reg.Audit(&proto.AuditEvent{TenantID: "", Action: "register_token_rejected", Target: dom.AgentID, Detail: "invalid or expired install token"})
			return nil, status.Error(codes.Unauthenticated, "invalid or expired install token")
		}
		dom.OnboardDeviceID = devID
		dom.TenantID = tokTenant // token 权威：纳管设备归属以 token 内租户为准
	} else {
		dom.OnboardDeviceID = "" // 安全（P0-F1）：无 token 时显式清空，agent 自报该字段一律不信任
		if actx.TenantID != "" {
			dom.TenantID = actx.TenantID // 网关注入租户盖章（agent 不可伪造）
		}
	}
	if g.requireAuth && dom.TenantID == "" {
		// 用标准 gRPC 状态码，便于 agent 侧精确判断未鉴权。
		return nil, status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
	}
	registered := g.reg.Register(domain.AgentToProto(dom))

	// 真实网段发现（P0-2）：开启时按 SegmentCIDR 扫描存活主机并纳管为真实 DeviceInfo。
	if g.cfg != nil && g.cfg.Discover && g.cfg.SegmentCIDR != "" {
		dctx := logx.WithTrace(ctx, "discover:"+registered.AgentID)
		ips, err := discover.Sweep(dctx, g.cfg.SegmentCIDR, nil, 64, 800*time.Millisecond)
		if err != nil {
			logx.Error(dctx, "网段扫描失败", err, "cidr", g.cfg.SegmentCIDR)
		} else {
			for _, ip := range ips {
				// B1 短期：网段发现的开放端口主机只是"候选"，不是已纳管设备。
				// 发现 ≠ 纳管（该主机上尚无 agent，无法注册/执行任务）。
				// 故标 State="discovered"、Managed=false、AgentID=""（待 provision 推送 agent 才真正纳管）。
				g.reg.UpsertDevice(&proto.DeviceInfo{
					DeviceID: "dev-" + ip, Segment: dom.Segment, TenantID: dom.TenantID,
					IP: ip, AgentID: "", State: "discovered", Managed: false, TaskState: "idle",
				})
			}
			logx.Info(dctx, "网段发现完成", "cidr", g.cfg.SegmentCIDR, "found", len(ips))
		}
	}

	// 注册审计与事件总线发布已统一在 store.Register 产出（U-04 等保三级 + P1-5），此处不再重复。
	// 观测：更新 agent 数与队列深度（P2-1）。
	if g.metrics != nil {
		g.metrics.SetAgents(len(g.reg.Agents("")))
	}

	return &grpcx.RegisterResp{
		AgentID: registered.AgentID,
		ControlConfig: map[string]int{
			"heartbeatInterval": 10, // 与 agent 心跳周期一致
			"taskPollInterval":  15, // 与 agent 任务轮询周期一致
		},
	}, nil
}

// Heartbeat 心跳：转发到 Registry.Heartbeat。
func (g *grpcServerImpl) Heartbeat(ctx context.Context, req *grpcx.HeartbeatReq) (*grpcx.Empty, error) {
	g.reg.Heartbeat(req.AgentID, req.Status, req.Load)
	return &grpcx.Empty{}, nil
}

// PullTasks 拉任务：原子领取该 agent 的下一条 pending 任务（pending→running），
// 多副本控制面并发调用时同一任务只会被一个副本领取（HA 协调，P1-1）。
func (g *grpcServerImpl) PullTasks(ctx context.Context, req *grpcx.PullTasksReq) (*grpcx.PullTasksResp, error) {
	t := g.reg.ClaimTask(req.AgentID)
	if g.metrics != nil {
		g.metrics.SetQueueDepth(g.reg.PendingDepth())
	}
	if t == nil {
		return &grpcx.PullTasksResp{}, nil
	}
	// 经防腐层出站：领域模型 -> 传输模型（P2-18 贯穿边界）。
	return &grpcx.PullTasksResp{Tasks: []proto.Task{*domain.TaskToProto(domain.TaskFromProto(t))}}, nil
}

// ReportResult 上报结果：转发到 Registry.SubmitResult，并更新观测指标 / 事件总线。
func (g *grpcServerImpl) ReportResult(ctx context.Context, res *proto.TaskResult) (*grpcx.Empty, error) {
	g.reg.SubmitResult(res)

	// M6 日志检索：任务执行结果（stdout/stderr）自动落地为可检索日志。
	// 租户取自 agent 归属（g.reg.Agent 返回 *proto.AgentInfo.TenantID），强制隔离不可伪造。
	if g.logs != nil && res.AgentID != "" {
		if a := g.reg.Agent(res.AgentID); a != nil {
			g.logs.RecordTaskResult(ctx, a.TenantID, res.AgentID, res.TaskID, res.ExitCode, res.Stdout, res.Stderr)
		}
	}

	// 经防腐层入站：传输模型 -> 领域模型承载业务语义（P2-18 贯穿边界）。
	dr := domain.TaskResultFromProto(res)
	if g.metrics != nil {
		status := "done"
		if dr.ExitCode != 0 {
			status = "failed"
		}
		g.metrics.IncTask(status)
		g.metrics.ObserveDuration(float64(dr.DurationMs) / 1000.0)
		g.metrics.SetQueueDepth(g.reg.PendingDepth())
	}
	if g.bus != nil {
		lvl := events.LevelInfo
		if dr.ExitCode != 0 {
			lvl = events.LevelWarn
		}
		g.bus.Publish(ctx, events.Event{
			TenantID: "", UserID: "", Action: "report_result",
			Target:   dr.TaskID,
			Detail:   fmt.Sprintf("exitCode=%d", dr.ExitCode),
			Level:    lvl,
		})
	}
	return &grpcx.Empty{}, nil
}

// CancelTask 取消任务（F3）：转发到 Registry.CancelTask（pending/running -> cancelled）。
// 服务端用网关注入租户强制覆盖 req.TenantID，防止越权取消他租户任务。
func (g *grpcServerImpl) CancelTask(ctx context.Context, req *grpcx.CancelTaskReq) (*grpcx.Empty, error) {
	var actx authctx.Context
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		actx = authctx.FromGRPCMetadata(md)
	}
	if g.requireAuth && actx.TenantID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
	}
	tenant := req.TenantID
	if g.requireAuth {
		tenant = actx.TenantID // 强制租户隔离
	}
	if req.TaskID == "" {
		return nil, status.Error(codes.InvalidArgument, "taskID required")
	}
	ok := g.reg.CancelTask(req.TaskID, tenant)
	if !ok {
		return nil, status.Error(codes.NotFound, "task not cancellable (not found / not pending|running / tenant mismatch)")
	}
	return &grpcx.Empty{}, nil
}

// PollCancels F3 取消信号下发：agent 侧 cancelLoop 轮询，返回本 agent 当前
// 处于 cancelled 状态的任务 ID 列表；agent 命中正在执行的任务即中止本地执行。
func (g *grpcServerImpl) PollCancels(ctx context.Context, req *grpcx.PollCancelsReq) (*grpcx.PollCancelsResp, error) {
	if req.AgentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agentID required")
	}
	ids := g.reg.CancelledTaskIDs(req.AgentID)
	return &grpcx.PollCancelsResp{CancelledTaskIDs: ids}, nil
}
