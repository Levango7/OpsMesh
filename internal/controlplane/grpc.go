package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
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
	"opsmesh/internal/otelx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// agentSignatureMaxSkew task 81：签名 timestamp 允许的最大时钟偏移（5 分钟）。
// 超过该偏移视为重放/过期，拒绝请求。5 分钟窗口兼顾合理时钟漂移与重放攻击窗口。
const agentSignatureMaxSkew = 5 * time.Minute

// agentSignatureMetadataKey task 81：gRPC metadata 中签名键名（小写按 gRPC 约定）。
const agentSignatureMetadataKey = "agent-signature"

// agentTimestampMetadataKey task 81：gRPC metadata 中 timestamp 键名（Unix 秒）。
const agentTimestampMetadataKey = "agent-timestamp"

// grpcServerImpl 实现 grpcx.RegistrationServer 接口，把四条 gRPC 通道转发到 store。
type grpcServerImpl struct {
	store       store.Store
	requireAuth bool
	cfg         *config.Config    // 可为 nil（测试）；非 nil 时启用网段发现（P0-2）
	bus         events.Bus        // 可为 nil（测试）；非 nil 时发布审计/告警事件（P1-5）
	metrics     *metrics.M        // 可为 nil（测试）；非 nil 时更新观测指标（P2-1）
	cmdb        *cmdb.Handler     // CMDB 处理器（Phase 1）；nil 时不处理 CmdbReport
	logs        *logstore.Handler // M6 日志检索处理器；nil 时不落地任务日志
	srv         *Server           // M3-2B：控制面 Server 引用，nil 时不发布 SSE 事件（测试兼容）
	// task 81 gRPC agent 身份绑定：是否强制要求 agent 请求携带 HMAC 签名。
	// false（默认，零值）=不校验签名（向后兼容 demo/测试/未启用 --grpc-require-signature 的部署）；
	// true=PullTasks/ReportResult/PollCancels/Heartbeat 入口校验 agent-signature metadata，
	// 签名不匹配或 timestamp 超过 5 分钟则拒绝（防冒领任务/伪造上报）。
	requireSignature bool
	// P0 安全加固：gRPC agent 身份绑定的预共享 HMAC 签名密钥。
	// 非空时 verifyAgentSignature 优先使用此密钥验签（而非 store.AgentSecret），
	// Register 响应不再返回签名密钥（Secret 始终为空），防注册不硬时密钥外泄。
	// 为空时回退到 store.AgentSecret（向后兼容已注册 agent 的旧机制）。
	signatureKey string
}

// Register 注册：调用 store.Register，返回分配到的 agentID 与控制面下发配置。
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
		devID, tokTenant, tokOK := g.store.ConsumeToken(info.InstallToken)
		if !tokOK {
			// U-04：认证失败也要留痕（B1 token 校验失败属认证事件）。
			// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
			g.audit(ctx, &proto.AuditEvent{TenantID: "", Action: "register_token_rejected", Target: dom.AgentID, Detail: "invalid or expired install token"})
			return nil, status.Error(codes.Unauthenticated, "invalid or expired install token")
		}
		dom.OnboardDeviceID = devID
		dom.TenantID = tokTenant // token 权威：纳管设备归属以 token 内租户为准
	} else {
		dom.OnboardDeviceID = "" // 安全（P0-F1）：无 token 时显式清空，agent 自报该字段一律不信任
		if actx.TenantID != "" {
			dom.TenantID = actx.TenantID // 网关注入租户盖章（agent 不可伪造）
		} else if g.cfg != nil && g.cfg.Demo {
			// demo 兜底：与 dashboard/SSE 一致——demo 模式下无网关租户时填 default。
			// 否则裸注册 agent 落 tenant=""，被 handleAgents("default") 过滤导致
			// 控制面看板/API 永远看不到该 agent（e2e-real firstAgentID 空列表根因）。
			dom.TenantID = sseDefaultTenant // "default"
		}
	}
	if g.requireAuth && dom.TenantID == "" {
		// 用标准 gRPC 状态码，便于 agent 侧精确判断未鉴权。
		return nil, status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
	}
	registered := g.store.Register(domain.AgentToProto(dom))

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
				g.store.UpsertDevice(&proto.DeviceInfo{
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
		g.metrics.SetAgents(len(g.store.Agents("")))
	}

	// M3-2B SSE：通知前端新 agent/设备已上线（设备表实时追加）
	// H6 租户隔离：携带 registered.TenantID，仅同租户订阅者收到。
	// M1-4：携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	if g.srv != nil {
		g.srv.publishEvent(ctx, "device_online", registered.TenantID, map[string]string{
			"agentID":  registered.AgentID,
			"hostname": registered.Hostname,
			"segment":  registered.Segment,
		})
	}

	// P0 安全加固：requireSignature 开启但未配置预共享密钥（signatureKey 为空）时，
	// 回退到 store.AgentSecret 机制——但 Register 不再下发密钥，新注册 agent 无法获取密钥，
	// 后续签名验证将全部失败。此处日志警告提示运维配置 --grpc-signature-key。
	if g.requireSignature && g.signatureKey == "" {
		logx.Warn(ctx, "gRPC 签名验证已启用但未配置预共享密钥（--grpc-signature-key），"+
			"新注册 agent 将无法签名，请配置预共享密钥或在 agent 侧配置相同密钥", "agentID", registered.AgentID)
	}

	return &grpcx.RegisterResp{
		AgentID: registered.AgentID,
		ControlConfig: map[string]int{
			"heartbeatInterval": 10, // 与 agent 心跳周期一致
			"taskPollInterval":  15, // 与 agent 任务轮询周期一致
		},
		// P0 安全加固：Register 响应不再返回签名密钥。
		// 此前在响应中下发 store.AgentSecret(agentID)，但注册不硬（任何人可注册）时
		// 攻击者可注册获取密钥后伪造签名，使签名形同虚设。改为预共享方式：
		// 控制面与 agent 两侧通过 --grpc-signature-key 手动配置同一密钥。
		// Secret 字段始终为空，agent 应从自身配置读取预共享密钥。
		Secret: "",
	}, nil
}

// checkAgentTenant 校验 req.AgentID 归属 ctx 租户（H2 gRPC 租户归属校验）。
// requireAuth 关闭（空租户）时放行，保持向后兼容（开发/内网友好网络降级）。
// agent 不存在时不拒绝（让后续业务逻辑处理 NotFound/未注册）；
// 仅在 agent 存在且其 TenantID 非空且与 ctx 租户不一致时返回 PermissionDenied。
// Register 不调用本函数（install token 权威，已在 Register 内单独处理）。
func (g *grpcServerImpl) checkAgentTenant(ctx context.Context, agentID string) error {
	if !g.requireAuth {
		return nil // 关闭鉴权时放行
	}
	var actx authctx.Context
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		actx = authctx.FromGRPCMetadata(md)
	}
	if actx.TenantID == "" {
		return status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
	}
	if agentID == "" {
		return nil // 空 AgentID 由后续业务逻辑校验 InvalidArgument
	}
	a := g.store.Agent(agentID)
	if a == nil {
		return nil // agent 不存在，交由后续业务逻辑处理（未注册/已退役）
	}
	// agent 已绑定租户且与 ctx 租户不一致 → 跨租户越权访问。
	if a.TenantID != "" && a.TenantID != actx.TenantID {
		return status.Error(codes.PermissionDenied, fmt.Sprintf("agent %q belongs to tenant %q, not %q (cross-tenant access denied)", agentID, a.TenantID, actx.TenantID))
	}
	return nil
}

// verifyAgentSignature task 81 gRPC agent 身份绑定：校验请求 metadata 中的 HMAC 签名。
// 开启 requireSignature 时，agent 必须在 gRPC metadata 中携带：
//   - agent-timestamp：签名生成时刻（Unix 秒，十进制字符串）
//   - agent-signature：HMAC-SHA256(secret, timestamp + agentID) 的 hex 编码
//
// 控制面用预共享密钥（signatureKey，优先）或 store.AgentSecret(agentID)（回退）重新计算 HMAC
// 并与 metadata 中的签名比对。
// 校验项：签名缺失 / timestamp 缺失或非法 / timestamp 超过 5 分钟偏移 / 签名不匹配 → Unauthenticated。
// requireSignature 关闭时直接放行（向后兼容 demo/未启用 --grpc-require-signature 的部署）。
// agent 不存在或未生成 secret 时：requireSignature 开启则拒绝（未注册 agent 不应有签名），
// 关闭则放行（保持原有行为）。
func (g *grpcServerImpl) verifyAgentSignature(ctx context.Context, agentID string) error {
	if !g.requireSignature {
		return nil // 未启用签名验证，放行（向后兼容）
	}
	if agentID == "" {
		return status.Error(codes.InvalidArgument, "agentID required (signature verification enabled)")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing gRPC metadata: agent-signature required")
	}
	sigVals := md.Get(agentSignatureMetadataKey)
	tsVals := md.Get(agentTimestampMetadataKey)
	if len(sigVals) == 0 || len(tsVals) == 0 {
		return status.Error(codes.Unauthenticated, "missing agent-signature or agent-timestamp metadata")
	}
	providedSig := sigVals[0]
	tsStr := tsVals[0]

	// 解析 timestamp（Unix 秒）
	tsSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return status.Error(codes.Unauthenticated, "invalid agent-timestamp: not a valid Unix second")
	}
	tsTime := time.Unix(tsSec, 0)
	now := time.Now()
	if skew := now.Sub(tsTime); skew > agentSignatureMaxSkew || skew < -agentSignatureMaxSkew {
		return status.Error(codes.Unauthenticated, fmt.Sprintf("agent-timestamp out of skew (max %v): got %v, now %v", agentSignatureMaxSkew, tsTime, now))
	}

	// P0 安全加固：优先使用预共享密钥（signatureKey），为空时回退到 store.AgentSecret（向后兼容）。
	// 预共享密钥模式下所有 agent 共用同一密钥，密钥不随 Register 响应下发，防注册不硬时密钥外泄。
	secret := g.signatureKey
	if secret == "" {
		secret = g.store.AgentSecret(agentID)
	}
	if secret == "" {
		// agent 未注册或未生成 secret 且未配置预共享密钥：requireSignature 开启时拒绝
		return status.Error(codes.Unauthenticated, fmt.Sprintf("agent %q has no signing secret (not registered, secret not generated, and no pre-shared --grpc-signature-key configured)", agentID))
	}

	// 重新计算 HMAC-SHA256(secret, timestamp + agentID) 并比对
	wantSig := computeAgentSignature(secret, tsStr, agentID)
	if !hmac.Equal([]byte(providedSig), []byte(wantSig)) {
		return status.Error(codes.Unauthenticated, "agent-signature mismatch: HMAC verification failed")
	}
	return nil
}

// computeAgentSignature task 81：计算 HMAC-SHA256(secret, timestamp + agentID) 的 hex 编码。
// 消息 = timestamp（Unix 秒字符串）+ agentID，密钥 = agent 的 secret。
// 控制面与 agent 端共用此函数（agent 端在 grpcclient.go 内联实现，保持一致）。
func computeAgentSignature(secret, timestamp, agentID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + agentID))
	return hex.EncodeToString(mac.Sum(nil))
}

// audit M1-4 分布式可观测性：gRPC handler 的审计日志 helper，
// 从 ctx 提取 OTel trace_id 注入 AuditEvent.TraceID，然后转发到 store.Audit。
// 与 Server.audit 对齐，使 gRPC 路径产出的审计日志也关联 trace_id。
// e 为 nil 时直接返回（容错）。
func (g *grpcServerImpl) audit(ctx context.Context, e *proto.AuditEvent) {
	if e == nil {
		return
	}
	if e.TraceID == "" {
		e.TraceID = otelx.TraceIDFromContext(ctx)
	}
	g.store.Audit(e)
}

// Heartbeat 心跳：转发到 store.Heartbeat；若携带监控指标则缓存到 store。
func (g *grpcServerImpl) Heartbeat(ctx context.Context, req *grpcx.HeartbeatReq) (*grpcx.Empty, error) {
	if err := g.checkAgentTenant(ctx, req.AgentID); err != nil {
		return nil, err
	}
	if err := g.verifyAgentSignature(ctx, req.AgentID); err != nil {
		return nil, err
	}
	g.store.Heartbeat(req.AgentID, req.Status, req.Load)
	// 监控指标上报：agent 每 30s 采集一次系统指标随心跳上报，控制面缓存最新值供 API 查询。
	if req.Metrics != nil {
		// 若 metrics.DeviceID 为空，用 dev-<agentID> 兜底（与 Register 创建占位设备的 ID 对齐）。
		deviceID := req.Metrics.DeviceID
		if deviceID == "" {
			deviceID = "dev-" + req.AgentID
			req.Metrics.DeviceID = deviceID
		}
		g.store.StoreDeviceMetrics(deviceID, req.Metrics)
	}
	return &grpcx.Empty{}, nil
}

// PullTasks 拉任务：原子领取该 agent 的下一条 pending 任务（pending→running），
// 多副本控制面并发调用时同一任务只会被一个副本领取（HA 协调，P1-1）。
func (g *grpcServerImpl) PullTasks(ctx context.Context, req *grpcx.PullTasksReq) (*grpcx.PullTasksResp, error) {
	if err := g.checkAgentTenant(ctx, req.AgentID); err != nil {
		return nil, err
	}
	if err := g.verifyAgentSignature(ctx, req.AgentID); err != nil {
		return nil, err
	}
	t := g.store.ClaimTask(req.AgentID)
	if g.metrics != nil {
		g.metrics.SetQueueDepth(g.store.PendingDepth())
	}
	if t == nil {
		return &grpcx.PullTasksResp{}, nil
	}
	// 经防腐层出站：领域模型 -> 传输模型（P2-18 贯穿边界）。
	return &grpcx.PullTasksResp{Tasks: []proto.Task{*domain.TaskToProto(domain.TaskFromProto(t))}}, nil
}

// ReportResult 上报结果：转发到 store.SubmitResult，并更新观测指标 / 事件总线。
func (g *grpcServerImpl) ReportResult(ctx context.Context, res *proto.TaskResult) (*grpcx.Empty, error) {
	if err := g.checkAgentTenant(ctx, res.AgentID); err != nil {
		return nil, err
	}
	if err := g.verifyAgentSignature(ctx, res.AgentID); err != nil {
		return nil, err
	}
	g.store.SubmitResult(res)

	// M6 日志检索：任务执行结果（stdout/stderr）自动落地为可检索日志。
	// 租户取自 agent 归属（g.store.Agent 返回 *proto.AgentInfo.TenantID），强制隔离不可伪造。
	if g.logs != nil && res.AgentID != "" {
		if a := g.store.Agent(res.AgentID); a != nil {
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
		g.metrics.SetQueueDepth(g.store.PendingDepth())
	}
	if g.bus != nil {
		lvl := events.LevelInfo
		if dr.ExitCode != 0 {
			lvl = events.LevelWarn
		}
		g.bus.Publish(ctx, events.Event{
			TenantID: "", UserID: "", Action: "report_result",
			Target: dr.TaskID,
			Detail: fmt.Sprintf("exitCode=%d", dr.ExitCode),
			Level:  lvl,
		})
	}
	// M3-2B SSE：通知前端任务状态已变更（结果上报 → done/failed，前端任务表刷新）。
	// 失败时同时发 alert_new：任务失败可能触发死信 → critical 告警（store 层在 SubmitResult
	// 内部判定），前端收到 alert_new 即刷新告警面板。冗余刷新可接受（前端刷新幂等）。
	// H6 租户隔离：事件归属租户取自 agent 注册时的 TenantID（agent 不可伪造，由 Register 盖章），
	// 仅同租户订阅者收到；agent 不存在时 tenant 留空（兼容旧数据/无网关降级）。
	// M1-4：携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	if g.srv != nil {
		agentTenant := ""
		if a := g.store.Agent(res.AgentID); a != nil {
			agentTenant = a.TenantID
		}
		status := "done"
		if dr.ExitCode != 0 {
			status = "failed"
		}
		g.srv.publishEvent(ctx, "task_status", agentTenant, map[string]interface{}{
			"taskID":   dr.TaskID,
			"status":   status,
			"agentID":  res.AgentID,
			"exitCode": dr.ExitCode,
		})
		if dr.ExitCode != 0 {
			g.srv.publishEvent(ctx, "alert_new", agentTenant, map[string]string{
				"taskID": dr.TaskID,
				"action": "dead_letter_check",
			})
		}
	}
	return &grpcx.Empty{}, nil
}

// CancelTask 取消任务（F3）：转发到 store.CancelTask（pending/running -> cancelled）。
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
	ok := g.store.CancelTask(req.TaskID, tenant)
	if !ok {
		return nil, status.Error(codes.NotFound, "task not cancellable (not found / not pending|running / tenant mismatch)")
	}
	// M3-2B SSE：通知前端任务已取消（gRPC 通道，agent 侧或编排系统触发）
	// H6 租户隔离：携带 tenant，仅同租户订阅者收到。
	// M1-4：携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	if g.srv != nil {
		g.srv.publishEvent(ctx, "task_status", tenant, map[string]string{
			"taskID": req.TaskID,
			"status": "cancelled",
		})
	}
	return &grpcx.Empty{}, nil
}

// PollCancels F3 取消信号下发：agent 侧 cancelLoop 轮询，返回本 agent 当前
// 处于 cancelled 状态的任务 ID 列表；agent 命中正在执行的任务即中止本地执行。
func (g *grpcServerImpl) PollCancels(ctx context.Context, req *grpcx.PollCancelsReq) (*grpcx.PollCancelsResp, error) {
	if req.AgentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agentID required")
	}
	// task 81：PollCancels 原完全无租户校验，知道 AgentID 即可拉取消列表。
	// 现添加签名验证，确保只有持有该 agent secret 的调用方能拉取（防冒充）。
	if err := g.verifyAgentSignature(ctx, req.AgentID); err != nil {
		return nil, err
	}
	// 同时补做租户归属校验（与 PullTasks/ReportResult 一致），防跨租户拉取消列表。
	if err := g.checkAgentTenant(ctx, req.AgentID); err != nil {
		return nil, err
	}
	ids := g.store.CancelledTaskIDs(req.AgentID)
	return &grpcx.PollCancelsResp{CancelledTaskIDs: ids}, nil
}

// ReportLogs task 247 agent 日志上报：接收 agent 采集的日志批次并落库。
// 校验 agent 身份（HMAC 签名）与租户归属后，按 agent 注册时盖章的 TenantID 回填 report.TenantID
// （agent 不可伪造租户），再经 store.SaveLogs 落库（行级隔离）。
// 同时把每行日志经 logstore.Handler.Append 转发到 M6 日志检索后端（若注入），供统一检索。
// 上报失败不中断 agent 循环（agent 侧仅记录日志），此处返回错误供 agent 决策重试/跳过。
func (g *grpcServerImpl) ReportLogs(ctx context.Context, req *grpcx.ReportLogsReq) (*grpcx.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	agentID := req.Report.AgentID
	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agentID required")
	}
	// task 81：校验 HMAC 签名，确保请求来自授权 agent（防冒充上报日志）。
	if err := g.verifyAgentSignature(ctx, agentID); err != nil {
		return nil, err
	}
	// H2：校验租户归属，防跨租户上报。
	if err := g.checkAgentTenant(ctx, agentID); err != nil {
		return nil, err
	}
	// 按 agent 归属回填 TenantID（agent 自报不信任，行级隔离由控制面盖章）。
	// agent 不存在时 tenant 留空（兼容无网关降级 / 旧数据）。
	tenantID := req.Report.TenantID
	if a := g.store.Agent(agentID); a != nil {
		tenantID = a.TenantID
	}
	// 落库到 store（MemoryStore/SQLStore 内存暂存，供 GET /api/v1/agent-logs 检索）。
	report := req.Report
	report.TenantID = tenantID
	if err := g.store.SaveLogs(tenantID, &report); err != nil {
		logx.Error(ctx, "agent 日志落库失败", err, "agentID", agentID, "logName", req.Report.LogName)
		return nil, status.Error(codes.Internal, fmt.Sprintf("save logs failed: %v", err))
	}

	// M6 日志检索桥接：把每行日志转发到 logstore 后端（若注入），供 /api/v1/logs 统一检索。
	// 与 ReportResult 的 RecordTaskResult 模式一致：source=agent，level 取自 LogLine.Level。
	if g.logs != nil && len(req.Report.Lines) > 0 {
		ls := g.logs.Store()
		deviceID := "dev-" + agentID
		for _, line := range req.Report.Lines {
			lvl := strings.ToLower(line.Level)
			if lvl == "" {
				lvl = "info"
			}
			_ = ls.Append(ctx, &logstore.Entry{
				TenantID:  tenantID,
				DeviceID:  deviceID,
				AgentID:   agentID,
				Timestamp: line.Timestamp,
				Level:     lvl,
				Source:    "agent",
				Message:   line.Message,
			})
		}
	}

	// U-04 等保三级留痕：agent 日志上报记审计事件。
	g.audit(ctx, &proto.AuditEvent{
		TenantID: tenantID,
		Action:   "report_logs",
		Target:   agentID,
		Detail:   fmt.Sprintf("logName=%s lines=%d", req.Report.LogName, len(req.Report.Lines)),
	})

	// M3-2B SSE：通知前端有新日志到达（前端日志面板可刷新）。
	if g.srv != nil {
		g.srv.publishEvent(ctx, "agent_logs", tenantID, map[string]interface{}{
			"agentID": agentID,
			"logName": req.Report.LogName,
			"lines":   len(req.Report.Lines),
		})
	}

	return &grpcx.Empty{}, nil
}
