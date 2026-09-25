package grpc

import (
	"context"
	"crypto/hmac"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/cmdb"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/domain"
	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/grpcx"
	"github.com/Levango7/OpsMesh/internal/logstore"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/otelx"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
	"github.com/Levango7/OpsMesh/pkg/discover"
)

// agentSignatureMaxSkew ：签名 timestamp 允许的最大时钟偏移（5 分钟）。
// 超过该偏移视为重放/过期，拒绝请求。5 分钟窗口兼顾合理时钟漂移与重放攻击窗口。
const agentSignatureMaxSkew = 5 * time.Minute

// sigWarnMaxKeys ：验签降级路径（fleet 密钥回退 / v1 旧算法）的限次告警键上限。
// 降级告警按 agentID 去重，若不封顶则海量 agentID 会让告警表无界增长（内存放大）。
// 达到上限后新键静默（宁可少告警，也不可被 agent 撑爆控制面内存）。
const sigWarnMaxKeys = 4096

// sseDefaultTenant 是 demo 模式下未携带身份头时填充的默认租户。
const sseDefaultTenant = "default"

// EventPublisher 是 gRPC handler 发布 SSE 事件的抽象接口，
// 使 grpc 包不依赖 controlplane 包的具体 Server 类型。
type EventPublisher interface {
	PublishEvent(ctx context.Context, typ string, tenantID string, data interface{})
}

// GrpcServerImpl 实现 grpcx.RegistrationServer 接口，把四条 gRPC 通道转发到 store。
//
// 依赖注入：优先使用 Svc（AgentService 适配层）；若构造时未注入 Svc，svc() 会
// 用 sync.Once 懒初始化为 NewStoreAgentService(Store)，向后兼容仅注入 Store 的
// 旧构造方（测试 / server_netsec）。TD-60 微服务切流时直接注入远程调用实现的
// AgentService 即可，无需改本结构体或任何 handler 方法。
type GrpcServerImpl struct {
	Store       store.Store  // deprecated：保留向后兼容仅注入 Store 的构造方；Svc 优先
	Svc         AgentService // Agent 适配层；nil 时由 svc() 懒初始化自 Store
	svcOnce     sync.Once    // 懒初始化 Svc 的并发安全守卫
	RequireAuth bool
	Cfg         *config.Config    // 可为 nil（测试）；非 nil 时启用网段发现
	Bus         events.Bus        // 可为 nil（测试）；非 nil 时发布审计/告警事件
	Metrics     *metrics.M        // 可为 nil（测试）；非 nil 时更新观测指标
	Cmdb        *cmdb.Handler     // CMDB 处理器（Phase 1）；nil 时不处理 CmdbReport
	Logs        *logstore.Handler // M6 日志检索处理器；nil 时不落地任务日志
	Publisher   EventPublisher    // SSE 事件发布器；nil 时不发布 SSE 事件（测试兼容）
	// RequireSignature gRPC agent 身份绑定：是否强制要求 agent 请求携带 HMAC 签名。
	// false（默认，零值）=不校验签名（向后兼容 demo/测试/未启用 --grpc-require-signature 的部署）；
	// true=PullTasks/ReportResult/PollCancels/Heartbeat 入口校验 agent-signature metadata，
	// 签名不匹配或 timestamp 超过 5 分钟则拒绝（防冒领任务/伪造上报）。
	RequireSignature bool
	// SignatureKey 安全加固：gRPC agent 身份绑定的预共享 HMAC 签名密钥。
	// 验签优先级：per-agent 密钥（store.AgentSecret，P1-2）优先，其次此预共享密钥。
	// 预共享密钥 = 全舰队同一密钥，一旦单机泄漏即全舰队可冒充，故仅作为
	//「运维显式配置 / 存量 agent 未升级」的兼容路径，命中时按限次 WARN 告警。
	SignatureKey string

	// sigWarnMu/sigWarnSeen ：验签降级告警的限次去重表（键为告警场景+agentID）。
	// 上限 sigWarnMaxKeys，防止 agentID 无界增长撑爆内存。
	sigWarnMu   sync.Mutex
	sigWarnSeen map[string]struct{}
}

// sigWarnOnce 按 key 只告警一次（键总量封顶 sigWarnMaxKeys，超出后新键静默）。
// 用于验签的「降级放行」路径：这些路径意味着安全性低于设计目标，必须让运维看见，
// 但不能每请求刷屏（否则日志被淹没、反而看不到真正的异常）。
func (g *GrpcServerImpl) sigWarnOnce(key, msg string, kv ...interface{}) {
	g.sigWarnMu.Lock()
	if g.sigWarnSeen == nil {
		g.sigWarnSeen = make(map[string]struct{})
	}
	_, seen := g.sigWarnSeen[key]
	if seen {
		g.sigWarnMu.Unlock()
		return
	}
	if len(g.sigWarnSeen) >= sigWarnMaxKeys {
		g.sigWarnMu.Unlock()
		return // 已封顶：静默（不再记录也不再打印）
	}
	g.sigWarnSeen[key] = struct{}{}
	g.sigWarnMu.Unlock()
	logx.Warn(context.Background(), msg, kv...)
}

// signingSecrets 返回验签候选密钥，按优先级排列：per-agent 密钥优先，预共享密钥兜底。
// 每个候选带 fleet 标记，命中 fleet 时调用方按降级路径告警（全舰队共用密钥的弱化）。
func (g *GrpcServerImpl) signingSecrets(agentID string) []signingSecret {
	var out []signingSecret
	if s := g.svc().AgentSecret(agentID); s != "" {
		out = append(out, signingSecret{secret: s, fleet: false})
	}
	if g.SignatureKey != "" {
		out = append(out, signingSecret{secret: g.SignatureKey, fleet: true})
	}
	return out
}

// signingSecret 一个验签候选密钥；fleet 表示来自全舰队预共享密钥（降级路径）。
type signingSecret struct {
	secret string
	fleet  bool
}

// transportIsTLS 判断本次连接是否经 TLS 握手（用于「密钥只走加密信道」判定）。
// peer.AuthInfo 仅在服务端配置了 grpc.Creds 且握手成功时非 nil（grpc-go 语义）；
// 另要求 Cfg 侧确实配置了证书，双条件避免误判（配置漏挂时不会误以为已加密）。
func (g *GrpcServerImpl) transportIsTLS(ctx context.Context) bool {
	if g.Cfg == nil || g.Cfg.TLSCert == "" {
		return false
	}
	p, ok := peer.FromContext(ctx)
	return ok && p.AuthInfo != nil
}

// svc 返回当前 AgentService 实现：优先返回构造时注入的 Svc；
// 若 Svc 为 nil（仅注入 Store 的旧构造方），用 sync.Once 懒初始化为
// NewStoreAgentService(Store) 并缓存。并发安全。
//
// 本方法是 GrpcServerImpl 从 store.Store 依赖迁移到 AgentService 适配层的
// 核心枢纽：所有 handler 方法通过 g.svc().* 访问 store 能力，而非 g.svc().*。
// TD-60 微服务切流时只需在构造方注入远程 AgentService 实现，handler 无需改动。
func (g *GrpcServerImpl) svc() AgentService {
	g.svcOnce.Do(func() {
		if g.Svc == nil {
			g.Svc = NewStoreAgentService(g.Store)
		}
	})
	return g.Svc
}

// Register 注册：调用 store.Register，返回分配到的 agentID 与控制面下发配置。
// 服务端按网关注入租户给 AgentInfo.TenantID 盖章（agent 不可伪造所属租户）。
// 入站 proto 经防腐层（ACL）转 domain，业务处理在 domain 上进行（贯穿边界）。
func (g *GrpcServerImpl) Register(ctx context.Context, info *proto.AgentInfo) (*grpcx.RegisterResp, error) {
	// 经防腐层：传输模型 -> 领域模型。
	dom := domain.AgentFromProto(info)
	var actx authctx.Context
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		actx = authctx.FromGRPCMetadata(md)
	}
	// 自动纳管闭环：install token 优先于网关租户头——token 权威。
	// agent 经 bootstrap 安装后携带一次性 install token 注册，该 token 由 Provision 签发，
	// 经 ConsumeToken 校验通过后，回填对应候选设备的 deviceID 并强制以 token 内租户为准。
	// tokenAuthenticated ：本次注册是否经一次性 install token 认证。
	// 只有认证过的注册才可能获得 per-agent 签名密钥（P1-2 密钥下发门槛）。
	tokenAuthenticated := false
	tokenStale := false
	if info.InstallToken != "" {
		devID, tokTenant, tokOK := g.svc().ConsumeToken(info.InstallToken)
		if tokOK {
			tokenAuthenticated = true
			dom.OnboardDeviceID = devID
			dom.TenantID = tokTenant // token 权威：纳管设备归属以 token 内租户为准
		} else {
			tokenStale = true
		}
	}

	// 重启容错（一次性 token 的既有语义缺口）：install token 在首轮注册即被消费，
	// 而 agent 本机 <dataDir>/install.token 文件不会自动消失（bootstrap 写入、agent 只读），
	// systemd 重启/机器重启后 agent 会再次携带同一 token 注册——若一律按「已消费」拒绝，
	// agent 侧 fail-fast 退出，纳管 agent 永远无法重启（多租户下即整机失联）。
	//
	// 判别：agentID 已在库 = 本机已持有 per-agent 密钥的「已知 agent 重注册」，而非首次纳管。
	// 该路径不以 token 认证、不下发密钥（密钥已在 agent 本机 agent.key），
	// 且租户一律沿用库内既有值——token 与 agent 自报值都不能改写归属。
	// agentID 不在库（或为空）= 拿失效 token 做首次纳管，仍然拒绝。
	if tokenStale {
		existing := g.svc().Agent(info.AgentID)
		if info.AgentID == "" || existing == nil {
			// 认证失败也要留痕（B1 token 校验失败属认证事件）。
			// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
			g.Audit(ctx, &proto.AuditEvent{TenantID: "", Action: "register_token_rejected", Target: dom.AgentID, Detail: "invalid, expired or already-consumed install token"})
			return nil, status.Error(codes.Unauthenticated, "invalid or expired install token")
		}
		dom.TenantID = existing.TenantID
		dom.OnboardDeviceID = existing.OnboardDeviceID
		g.sigWarnOnce("token-consumed:"+info.AgentID,
			"install token 已消费或失效，按已知 agent 重注册处理（沿用库内租户、不下发密钥）；"+
				"如需为该 agent 换发密钥，请重新 provision 签发新 token",
			"agentID", info.AgentID)
	}

	if !tokenAuthenticated && !tokenStale {
		dom.OnboardDeviceID = "" // 安全：无 token 时显式清空，agent 自报该字段一律不信任
		if actx.TenantID != "" {
			dom.TenantID = actx.TenantID // 网关注入租户盖章（agent 不可伪造）
		} else if existing := g.svc().Agent(dom.AgentID); existing != nil && existing.TenantID != "" {
			// 已知 agent 且调用方未声明任何租户（无 token、无网关注入）：沿用库内租户。
			// 只做「填空」不改归属——否则既有租户会被写成空，既造成数据损坏（agent 从原租户
			// 视图消失），也会被 store 的跨租户保护拒绝导致 agent 重启后 fail-fast 失联。
			dom.TenantID = existing.TenantID
		} else if g.Cfg != nil && g.Cfg.Demo {
			// demo 兜底：与 dashboard/SSE 一致——demo 模式下无网关租户时填 default。
			// 否则裸注册 agent 落 tenant=""，被 handleAgents("default") 过滤导致
			// 控制面看板/API 永远看不到该 agent（e2e-real firstAgentID 空列表根因）。
			dom.TenantID = sseDefaultTenant // "default"
		}
	}
	if g.RequireAuth && dom.TenantID == "" {
		// 用标准 gRPC 状态码，便于 agent 侧精确判断未鉴权。
		return nil, status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
	}
	registered := g.svc().Register(domain.AgentToProto(dom))
	if registered == nil {
		// store 拒绝注册（跨租户重绑定已有 agentID，见 store.Register 租户锁定）。
		// 注册不硬时任何人可注册，若允许改租户则等于把他人 agent 连设备/任务一起偷走，
		// 故事件必须留痕并拒绝（authentication 已过但 authorization 不通过）。
		g.Audit(ctx, &proto.AuditEvent{TenantID: actx.TenantID, Action: "register_tenant_conflict", Target: info.AgentID, Detail: "agentID already bound to another tenant; re-registration with a different tenant refused"})
		return nil, status.Error(codes.PermissionDenied, "agentID already belongs to another tenant (cross-tenant re-registration refused)")
	}

	// 真实网段发现：开启时按 SegmentCIDR 扫描存活主机并纳管为真实 DeviceInfo。
	if g.Cfg != nil && g.Cfg.Discover && g.Cfg.SegmentCIDR != "" {
		dctx := logx.WithTrace(ctx, "discover:"+registered.AgentID)
		ips, err := discover.Sweep(dctx, g.Cfg.SegmentCIDR, nil, 64, 800*time.Millisecond)
		if err != nil {
			logx.Error(dctx, "网段扫描失败", err, "cidr", g.Cfg.SegmentCIDR)
		} else {
			for _, ip := range ips {
				// 短期：网段发现的开放端口主机只是"候选"，不是已纳管设备。
				// 发现 ≠ 纳管（该主机上尚无 agent，无法注册/执行任务）。
				// 故标 State="discovered"、Managed=false、AgentID=""（待 provision 推送 agent 才真正纳管）。
				g.svc().UpsertDevice(&proto.DeviceInfo{
					DeviceID: "dev-" + ip, Segment: dom.Segment, TenantID: dom.TenantID,
					IP: ip, AgentID: "", State: "discovered", Managed: false, TaskState: "idle",
				})
			}
			logx.Info(dctx, "网段发现完成", "cidr", g.Cfg.SegmentCIDR, "found", len(ips))
		}
	}

	// 注册审计与事件总线发布已统一在 store.Register 产出（等保三级 +），此处不再重复。
	// 观测：更新 agent 数与队列深度。
	if g.Metrics != nil {
		g.Metrics.SetAgents(len(g.svc().Agents("")))
	}

	// SSE：通知前端新 agent/设备已上线（设备表实时追加）
	// 租户隔离：携带 registered.TenantID，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	if g.Publisher != nil {
		g.Publisher.PublishEvent(ctx, "device_online", registered.TenantID, map[string]string{
			// 契约字段（sse-protocol.md / 前端 EVENT_CONTRACT）：deviceID + segment。
			// 缺 deviceID 时前端 validateEventData 会丢弃事件，设备列表失去实时刷新。
			"deviceID": registered.AgentID,
			"segment":  registered.Segment,
			// 扩展字段：向后兼容保留。
			"agentID":  registered.AgentID,
			"hostname": registered.Hostname,
		})
	}

	// P1-2 per-agent 密钥下发：仅当「经一次性 install token 认证」且「连接为 TLS」时下发。
	//
	// 为何不是无条件下发：上一版 Register 曾无条件返回 store.AgentSecret(agentID)，
	// 而注册不硬（任何人可注册任意 agentID）时攻击者可直接注册同名 agentID 拿走密钥，
	// 签名形同虚设，故一度改为完全不下发（强制全舰队预共享密钥）。
	// 但全舰队同密钥 = 单点泄漏即全舰队可冒充，且轮换需全体停机改配置。
	//
	// 现在收口为「token 认证 + 加密信道」双门槛：
	//   - install token 由 Provision 签发（HMAC 签名 + 一次性 + 限时），只有拿到该设备
	//     一次性 token 的调用方能通过；重放被 ConsumeToken 的原子抢占挡住。
	//   - TLS 门槛保证密钥不过明文信道（无 TLS 时明文下发等于向同网段泄漏）。
	// 两条都不满足时不下发，agent 侧回退到本机 agent.key 或运维预共享密钥。
	deliveredSecret := ""
	if tokenAuthenticated {
		if g.transportIsTLS(ctx) {
			deliveredSecret = g.svc().AgentSecret(registered.AgentID)
		} else {
			g.sigWarnOnce("secret-plaintext:"+registered.AgentID,
				"install token 认证通过但 gRPC 连接非 TLS，拒绝下发 per-agent 签名密钥（防明文信道泄漏）；"+
					"请启用 --tls-cert/--tls-key，或在 agent 侧配置 --grpc-signature-key 预共享密钥",
				"agentID", registered.AgentID)
		}
	}

	// requireSignature 开启但既无 per-agent 密钥可下发、又未配置预共享密钥：
	// 该 agent 后续所有请求都会被验签拒绝，此处提前告警（否则现场只见 agent 反复失败）。
	if g.RequireSignature && deliveredSecret == "" && g.SignatureKey == "" {
		g.sigWarnOnce("no-key:"+registered.AgentID,
			"gRPC 签名验证已启用，但本次注册既未下发 per-agent 密钥（需 install token + TLS）"+
				"也未配置预共享密钥（--grpc-signature-key）：该 agent 只能用本机已落盘的 agent.key 签名，"+
				"否则后续请求将被拒绝",
			"agentID", registered.AgentID)
	}

	return &grpcx.RegisterResp{
		AgentID: registered.AgentID,
		ControlConfig: map[string]int{
			"heartbeatInterval": 10, // 与 agent 心跳周期一致
			"taskPollInterval":  15, // 与 agent 任务轮询周期一致
		},
		// P1-2：仅 token 认证 + TLS 时下发 per-agent 密钥（见上方 deliveredSecret 说明）。
		// agent 收到后落盘 <dataDir>/agent.key（0600）并优先用它签名（v2，覆盖载荷）。
		Secret: deliveredSecret,
	}, nil
}

// CheckAgentTenant 校验 req.AgentID 归属 ctx 租户（H2 gRPC 租户归属校验）。
// requireAuth 关闭（空租户）时放行，保持向后兼容（开发/内网友好网络降级）。
// agent 不存在时不拒绝（让后续业务逻辑处理 NotFound/未注册）；
// 仅在 agent 存在且其 TenantID 非空且与 ctx 租户不一致时返回 PermissionDenied。
// Register 不调用本函数（install token 权威，已在 Register 内单独处理）。
//
// 无网关注入租户时的归属来源（P1-2 可用性修复，实测缺陷）：
// agent 是**拉模型**（直连 9090，不经 HTTP 网关），无任何途径提供 X-Tenant-ID——
// 控制面也没有向 agent 下发租户的通道（RegisterResp 无租户字段，agent 无租户配置项）。
// 而 --require-auth 的语义是「要求**网关**注入租户」（见 flag 帮助与 docs/api-reference.md），
// 对 agent 通道本不适用。此前该检查对 agent 通道一并生效，后果是：生产形态
// （REQUIRE_AUTH=true + GRPC_REQUIRE_SIGNATURE=true）下 agent 能注册成功，但
// Heartbeat/PullTasks/PollCancels/ReportResult/ReportLogs 全部 401 → 任务下发与结果上报完全不可用
// （真机实测：注册 1 次成功后，心跳/领任务/取消轮询连续失败）。
// 故此处改为：ctx 无租户时，取「注册时盖章的归属租户」（由 install token/库内记录确定，
// 不是 agent 自报），仅当 agent 未知或其归属租户为空时才维持原拒绝。
//
// 安全边界说明：被取代的「必须自带租户元数据」从来不是安全边界——该元数据不带签名，
// 能伪造身份的调用方本可直接填上正确租户通过旧检查；真正的身份边界是
// verifyAgentSignature（v2 覆盖载荷）与 mTLS。此处不放松任何既有拒绝路径：
// 声明了租户且与归属不一致 → 仍 PermissionDenied；未知 agent + 无租户 → 仍 Unauthenticated。
func (g *GrpcServerImpl) CheckAgentTenant(ctx context.Context, agentID string) error {
	if !g.RequireAuth {
		return nil // 关闭鉴权时放行
	}
	var actx authctx.Context
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		actx = authctx.FromGRPCMetadata(md)
	}
	var a *proto.AgentInfo
	if agentID != "" {
		a = g.svc().Agent(agentID)
	}
	if actx.TenantID == "" {
		if a == nil || a.TenantID == "" {
			return status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
		}
		actx.TenantID = a.TenantID
	}
	if agentID == "" {
		return nil // 空 AgentID 由后续业务逻辑校验 InvalidArgument
	}
	if a == nil {
		return nil // agent 不存在，交由后续业务逻辑处理（未注册/已退役）
	}
	// agent 已绑定租户且与 ctx 租户不一致 → 跨租户越权访问。
	if a.TenantID != "" && a.TenantID != actx.TenantID {
		return status.Error(codes.PermissionDenied, fmt.Sprintf("agent %q belongs to tenant %q, not %q (cross-tenant access denied)", agentID, a.TenantID, actx.TenantID))
	}
	return nil
}

// verifyAgentSignature gRPC agent 身份绑定：校验请求 metadata 中的 HMAC 签名。
// 开启 requireSignature 时，agent 必须在 gRPC metadata 中携带：
//   - agent-timestamp：签名生成时刻（Unix 秒，十进制字符串）
//   - agent-signature：HMAC 签名（hex），算法见 agent-signature-alg
//   - agent-signature-alg：v2（推荐）= HMAC(secret, "v2\n"+ts+"\n"+身份+"\n"+载荷摘要)，覆盖载荷；
//     v1（缺失时的默认，仅兼容存量 agent）= HMAC(secret, ts+身份)，不覆盖载荷。
//
// 密钥选择（P1-2）：per-agent 密钥（store.AgentSecret）优先，预共享密钥兜底；
// 命中预共享密钥或 v1 算法时按限次 WARN 告警（二者均为低于设计目标的降级路径）。
// 身份：除 CancelTask 以租户 ID 为身份（agent 侧无 agentID 字段）外，均为 agentID。
//
// 校验项：签名/timestamp 缺失 / timestamp 非法或超 5 分钟偏移 / 算法未知 /
// 载荷摘要不可计算 / 所有候选密钥均不匹配 → Unauthenticated；
// 无任何可用密钥 → Unauthenticated（未注册 agent 不应有签名）。
// requireSignature 关闭时直接放行（向后兼容 demo/未启用 --grpc-require-signature 的部署）。
func (g *GrpcServerImpl) verifyAgentSignature(ctx context.Context, agentID string, payload any) error {
	if !g.RequireSignature {
		return nil // 未启用签名验证，放行（向后兼容）
	}
	if agentID == "" {
		return status.Error(codes.InvalidArgument, "agentID required (signature verification enabled)")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing gRPC metadata: agent-signature required")
	}
	sigVals := md.Get(grpcx.AgentSignatureMetadataKey)
	tsVals := md.Get(grpcx.AgentTimestampMetadataKey)
	if len(sigVals) == 0 || len(tsVals) == 0 {
		g.incSig("none", "rejected")
		return status.Error(codes.Unauthenticated, "missing agent-signature or agent-timestamp metadata")
	}
	providedSig := sigVals[0]
	tsStr := tsVals[0]

	// 算法版本：缺失按 v1（兼容未升级 agent），未知值直接拒绝（不猜测）。
	// 先于时间戳校验解析，使被拒流量也归入正确的 alg 标签（可观测性）。
	alg := grpcx.AgentSignatureAlgV1
	if av := md.Get(grpcx.AgentSignatureAlgMetadataKey); len(av) > 0 && av[0] != "" {
		alg = av[0]
	}
	digest := ""
	switch alg {
	case grpcx.AgentSignatureAlgV1:
		// 降级路径：旧算法不覆盖载荷，同秒内任意报文可被改写后重放。限次告警促升级。
		g.sigWarnOnce("alg-v1:"+agentID,
			"agent 使用 v1 签名算法（不覆盖载荷，存在篡改/重放风险），建议升级 agent 到 v2", "agentID", agentID)
	case grpcx.AgentSignatureAlgV2:
		digest = grpcx.PayloadDigest(payload)
		if digest == "" {
			// 服务端自身无法计算摘要 = 编程错误（已解码的报文必然可再序列化），不可当成验签通过。
			g.incSig(alg, "rejected")
			return status.Error(codes.Internal, "payload digest unavailable: signature cannot be verified")
		}
	default:
		g.incSig(alg, "rejected")
		return status.Error(codes.Unauthenticated, fmt.Sprintf("unknown agent-signature-alg %q (supported: %s, %s)", alg, grpcx.AgentSignatureAlgV1, grpcx.AgentSignatureAlgV2))
	}

	// 解析 timestamp（Unix 秒）
	tsSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		g.incSig(alg, "rejected")
		return status.Error(codes.Unauthenticated, "invalid agent-timestamp: not a valid Unix second")
	}
	tsTime := time.Unix(tsSec, 0)
	now := time.Now()
	if skew := now.Sub(tsTime); skew > agentSignatureMaxSkew || skew < -agentSignatureMaxSkew {
		g.incSig(alg, "rejected")
		return status.Error(codes.Unauthenticated, fmt.Sprintf("agent-timestamp out of skew (max %v): got %v, now %v", agentSignatureMaxSkew, tsTime, now))
	}

	// 密钥选择（P1-2）：per-agent 密钥优先，预共享密钥兜底（命中即告警）。
	candidates := g.signingSecrets(agentID)
	if len(candidates) == 0 {
		g.incSig(alg, "rejected")
		return status.Error(codes.Unauthenticated, fmt.Sprintf("agent %q has no signing secret (not registered, secret not generated, and no pre-shared --grpc-signature-key configured)", agentID))
	}
	for _, cand := range candidates {
		var wantSig string
		if alg == grpcx.AgentSignatureAlgV2 {
			wantSig = grpcx.ComputeAgentSignatureV2(cand.secret, tsStr, agentID, digest)
		} else {
			wantSig = grpcx.ComputeAgentSignatureV1(cand.secret, tsStr, agentID)
		}
		if hmac.Equal([]byte(providedSig), []byte(wantSig)) {
			g.incSig(alg, "ok")
			if cand.fleet {
				// 降级路径：预共享密钥为全舰队共用，单机泄漏即全舰队可冒充。限次告警促迁移。
				g.incSigKeySource("fleet")
				g.sigWarnOnce("fleet-key:"+agentID,
					"agent 使用全舰队预共享密钥验签通过（单机泄漏即全舰队可冒充）；"+
						"建议改用 per-agent 密钥（install token + TLS 自动下发）", "agentID", agentID)
			} else {
				g.incSigKeySource("per_agent")
			}
			return nil
		}
	}
	g.incSig(alg, "rejected")
	return status.Error(codes.Unauthenticated, "agent-signature mismatch: HMAC verification failed")
}

// incSig 记录一次验签结果（P1-2 可观测性）；Metrics 未注入时为空操作。
// 标签基数由 metrics 包收敛为固定集合（算法声明来自 agent，不可直接入标签）。
func (g *GrpcServerImpl) incSig(alg, result string) {
	if g.Metrics != nil {
		g.Metrics.IncAgentSignature(alg, result)
	}
}

// incSigKeySource 记录验签通过的密钥来源（per_agent=该 agent 独立密钥；fleet=全舰队预共享兜底）。
func (g *GrpcServerImpl) incSigKeySource(source string) {
	if g.Metrics != nil {
		g.Metrics.IncAgentSignatureKeySource(source)
	}
}

// Audit 分布式可观测性：gRPC handler 的审计日志 helper，
// 从 ctx 提取 OTel trace_id 注入 AuditEvent.TraceID，然后转发到 store.Audit。
// 与 Server.audit 对齐，使 gRPC 路径产出的审计日志也关联 trace_id。
// e 为 nil 时直接返回（容错）。
func (g *GrpcServerImpl) Audit(ctx context.Context, e *proto.AuditEvent) {
	if e == nil {
		return
	}
	if e.TraceID == "" {
		e.TraceID = otelx.TraceIDFromContext(ctx)
	}
	g.svc().Audit(e)
}

// Heartbeat 心跳：转发到 store.Heartbeat；若携带监控指标则缓存到 store。
func (g *GrpcServerImpl) Heartbeat(ctx context.Context, req *grpcx.HeartbeatReq) (*grpcx.Empty, error) {
	if err := g.CheckAgentTenant(ctx, req.AgentID); err != nil {
		return nil, err
	}
	if err := g.verifyAgentSignature(ctx, req.AgentID, req); err != nil {
		return nil, err
	}
	g.svc().Heartbeat(req.AgentID, req.Status, req.Load)
	// 监控指标上报：agent 每 30s 采集一次系统指标随心跳上报，控制面缓存最新值供 API 查询。
	if req.Metrics != nil {
		// 若 metrics.DeviceID 为空，用 dev-<agentID> 兜底（与 Register 创建占位设备的 ID 对齐）。
		deviceID := req.Metrics.DeviceID
		if deviceID == "" {
			deviceID = "dev-" + req.AgentID
			req.Metrics.DeviceID = deviceID
		}
		g.svc().StoreDeviceMetrics(deviceID, req.Metrics)
	}
	return &grpcx.Empty{}, nil
}

// PullTasks 拉任务：原子领取该 agent 的下一条 pending 任务（pending→running），
// 多副本控制面并发调用时同一任务只会被一个副本领取（HA 协调）。
func (g *GrpcServerImpl) PullTasks(ctx context.Context, req *grpcx.PullTasksReq) (*grpcx.PullTasksResp, error) {
	if err := g.CheckAgentTenant(ctx, req.AgentID); err != nil {
		return nil, err
	}
	if err := g.verifyAgentSignature(ctx, req.AgentID, req); err != nil {
		return nil, err
	}
	t := g.svc().ClaimTask(req.AgentID)
	if g.Metrics != nil {
		g.Metrics.SetQueueDepth(g.svc().PendingDepth())
	}
	if t == nil {
		return &grpcx.PullTasksResp{}, nil
	}
	// 经防腐层出站：领域模型 -> 传输模型（贯穿边界）。
	return &grpcx.PullTasksResp{Tasks: []proto.Task{*domain.TaskToProto(domain.TaskFromProto(t))}}, nil
}

// ReportResult 上报结果：转发到 store.SubmitResult，并更新观测指标 / 事件总线。
func (g *GrpcServerImpl) ReportResult(ctx context.Context, res *proto.TaskResult) (*grpcx.Empty, error) {
	if err := g.CheckAgentTenant(ctx, res.AgentID); err != nil {
		return nil, err
	}
	if err := g.verifyAgentSignature(ctx, res.AgentID, res); err != nil {
		return nil, err
	}
	g.svc().SubmitResult(res)

	// M6 日志检索：任务执行结果（stdout/stderr）自动落地为可检索日志。
	// 租户取自 agent 归属（g.svc().Agent 返回 *proto.AgentInfo.TenantID），强制隔离不可伪造。
	if g.Logs != nil && res.AgentID != "" {
		if a := g.svc().Agent(res.AgentID); a != nil {
			g.Logs.RecordTaskResult(ctx, a.TenantID, res.AgentID, res.TaskID, res.ExitCode, res.Stdout, res.Stderr)
		}
	}

	// 经防腐层入站：传输模型 -> 领域模型承载业务语义（贯穿边界）。
	dr := domain.TaskResultFromProto(res)
	if g.Metrics != nil {
		status := "done"
		if dr.ExitCode != 0 {
			status = "failed"
		}
		g.Metrics.IncTask(status)
		g.Metrics.ObserveDuration(float64(dr.DurationMs) / 1000.0)
		g.Metrics.SetQueueDepth(g.svc().PendingDepth())
	}
	if g.Bus != nil {
		lvl := events.LevelInfo
		if dr.ExitCode != 0 {
			lvl = events.LevelWarn
		}
		g.Bus.Publish(ctx, events.Event{
			TenantID: "", UserID: "", Action: "report_result",
			Target: dr.TaskID,
			Detail: fmt.Sprintf("exitCode=%d", dr.ExitCode),
			Level:  lvl,
		})
	}
	// SSE：通知前端任务状态已变更（结果上报 → done/failed，前端任务表刷新）。
	// 失败时同时发 alert_new：任务失败可能触发死信 → critical 告警（store 层在 SubmitResult
	// 内部判定），前端收到 alert_new 即刷新告警面板。冗余刷新可接受（前端刷新幂等）。
	// 租户隔离：事件归属租户取自 agent 注册时的 TenantID（agent 不可伪造，由 Register 盖章），
	// 仅同租户订阅者收到；agent 不存在时 tenant 留空（兼容旧数据/无网关降级）。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	if g.Publisher != nil {
		agentTenant := ""
		if a := g.svc().Agent(res.AgentID); a != nil {
			agentTenant = a.TenantID
		}
		status := "done"
		if dr.ExitCode != 0 {
			status = "failed"
		}
		g.Publisher.PublishEvent(ctx, "task_status", agentTenant, map[string]interface{}{
			"taskID":   dr.TaskID,
			"status":   status,
			"agentID":  res.AgentID,
			"exitCode": dr.ExitCode,
		})
		if dr.ExitCode != 0 {
			g.Publisher.PublishEvent(ctx, "alert_new", agentTenant, map[string]string{
				"taskID": dr.TaskID,
				"action": "dead_letter_check",
			})
		}
	}
	return &grpcx.Empty{}, nil
}

// CancelTask 取消任务（F3）：转发到 store.CancelTask（pending/running -> cancelled）。
// 服务端用网关注入租户强制覆盖 req.TenantID，防止越权取消他租户任务。
//
// G1 鉴权修复：CancelTask 原无 agent 验签（对比 PollCancels 已验签）。CancelTaskReq
// 无 AgentID 字段，agent 侧（grpcclient.CancelTask）以 tenantID 为签名身份附加 HMAC
// （signContext(ctx, tenantID)），故此处以 req.TenantID 调 verifyAgentSignature 对齐：
// requireSignature 开启时，只有持有该租户签名密钥的调用方能取消任务（防冒充取消）。
// 同时补租户交叉校验：metadata 与请求体均声明租户且不一致时拒绝（防伪装他租户）。
func (g *GrpcServerImpl) CancelTask(ctx context.Context, req *grpcx.CancelTaskReq) (*grpcx.Empty, error) {
	var actx authctx.Context
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		actx = authctx.FromGRPCMetadata(md)
	}
	if g.RequireAuth && actx.TenantID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing tenant context: gateway auth required (--require-auth)")
	}
	// 补 agent 验签：以 req.TenantID 为签名身份（与 agent 侧 signContext(ctx, tenantID) 对齐）。
	// RequireSignature 关闭时 verifyAgentSignature 直接放行（向后兼容）。
	if err := g.verifyAgentSignature(ctx, req.TenantID, req); err != nil {
		return nil, err
	}
	tenant := req.TenantID
	if g.RequireAuth {
		tenant = actx.TenantID // 强制租户隔离
	}
	// 租户交叉校验：metadata 与请求体均声明租户且不一致 → 拒绝。
	if actx.TenantID != "" && req.TenantID != "" && actx.TenantID != req.TenantID {
		return nil, status.Error(codes.PermissionDenied, "tenant mismatch between metadata and request body")
	}
	if req.TaskID == "" {
		return nil, status.Error(codes.InvalidArgument, "taskID required")
	}
	ok := g.svc().CancelTask(req.TaskID, tenant)
	if !ok {
		return nil, status.Error(codes.NotFound, "task not cancellable (not found / not pending|running / tenant mismatch)")
	}
	// SSE：通知前端任务已取消（gRPC 通道，agent 侧或编排系统触发）
	// 租户隔离：携带 tenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	if g.Publisher != nil {
		g.Publisher.PublishEvent(ctx, "task_status", tenant, map[string]string{
			"taskID": req.TaskID,
			"status": "cancelled",
		})
	}
	return &grpcx.Empty{}, nil
}

// PollCancels F3 取消信号下发：agent 侧 cancelLoop 轮询，返回本 agent 当前
// 处于 cancelled 状态的任务 ID 列表；agent 命中正在执行的任务即中止本地执行。
func (g *GrpcServerImpl) PollCancels(ctx context.Context, req *grpcx.PollCancelsReq) (*grpcx.PollCancelsResp, error) {
	if req.AgentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agentID required")
	}
	// ：PollCancels 原完全无租户校验，知道 AgentID 即可拉取消列表。
	// 现添加签名验证，确保只有持有该 agent secret 的调用方能拉取（防冒充）。
	if err := g.verifyAgentSignature(ctx, req.AgentID, req); err != nil {
		return nil, err
	}
	// 同时补做租户归属校验（与 PullTasks/ReportResult 一致），防跨租户拉取消列表。
	if err := g.CheckAgentTenant(ctx, req.AgentID); err != nil {
		return nil, err
	}
	ids := g.svc().CancelledTaskIDs(req.AgentID)
	return &grpcx.PollCancelsResp{CancelledTaskIDs: ids}, nil
}

// ReportLogs agent 日志上报：接收 agent 采集的日志批次并落库。
// 校验 agent 身份（HMAC 签名）与租户归属后，按 agent 注册时盖章的 TenantID 回填 report.TenantID
// （agent 不可伪造租户），再经 store.SaveLogs 落库（行级隔离）。
// 同时把每行日志经 logstore.Handler.Append 转发到 M6 日志检索后端（若注入），供统一检索。
// 上报失败不中断 agent 循环（agent 侧仅记录日志），此处返回错误供 agent 决策重试/跳过。
func (g *GrpcServerImpl) ReportLogs(ctx context.Context, req *grpcx.ReportLogsReq) (*grpcx.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	agentID := req.Report.AgentID
	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agentID required")
	}
	// 校验 HMAC 签名，确保请求来自授权 agent（防冒充上报日志）。
	if err := g.verifyAgentSignature(ctx, agentID, req); err != nil {
		return nil, err
	}
	// 校验租户归属，防跨租户上报。
	if err := g.CheckAgentTenant(ctx, agentID); err != nil {
		return nil, err
	}
	// 按 agent 归属回填 TenantID（agent 自报不信任，行级隔离由控制面盖章）。
	// agent 不存在时 tenant 留空（兼容无网关降级 / 旧数据）。
	tenantID := req.Report.TenantID
	if a := g.svc().Agent(agentID); a != nil {
		tenantID = a.TenantID
	}
	// 落库到 store（MemoryStore/SQLStore 内存暂存，供 GET /api/v1/agent-logs 检索）。
	report := req.Report
	report.TenantID = tenantID
	if err := g.svc().SaveLogs(tenantID, &report); err != nil {
		logx.Error(ctx, "agent 日志落库失败", err, "agentID", agentID, "logName", req.Report.LogName)
		return nil, status.Error(codes.Internal, fmt.Sprintf("save logs failed: %v", err))
	}

	// M6 日志检索桥接：把每行日志转发到 logstore 后端（若注入），供 /api/v1/logs 统一检索。
	// 与 ReportResult 的 RecordTaskResult 模式一致：source=agent，level 取自 LogLine.Level。
	if g.Logs != nil && len(req.Report.Lines) > 0 {
		ls := g.Logs.Store()
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

	// 等保三级留痕：agent 日志上报记审计事件。
	g.Audit(ctx, &proto.AuditEvent{
		TenantID: tenantID,
		Action:   "report_logs",
		Target:   agentID,
		Detail:   fmt.Sprintf("logName=%s lines=%d", req.Report.LogName, len(req.Report.Lines)),
	})

	// SSE：通知前端有新日志到达（前端日志面板可刷新）。
	if g.Publisher != nil {
		g.Publisher.PublishEvent(ctx, "agent_logs", tenantID, map[string]interface{}{
			"agentID": agentID,
			"logName": req.Report.LogName,
			"lines":   len(req.Report.Lines),
		})
	}

	return &grpcx.Empty{}, nil
}
