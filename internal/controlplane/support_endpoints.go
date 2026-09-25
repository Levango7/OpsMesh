// support_endpoints.go — P1-6 可支撑性端点：版本、配置转储、诊断包、pprof。
//
// 背景（商用就绪评估 P1-6）：原实现没有版本端点、没有配置转储、没有诊断包，
// 客户现场排障只能 SSH + 读源码/文档，支持成本高且无法远程定位。本文件补齐
// 这四项，并把「哪些信息可以外发」用白名单写死在代码里。
//
// 安全模型（三条硬约束）：
//  1. /version 与 /healthz 同级：无鉴权，但**只**暴露版本/构建/运行时与存活时长，
//     不含配置、租户、主机名、路径等任何可被侦察的信息。
//  2. 配置转储与诊断包需 `diagnostics:dump` 权限（RBAC 派生规则下仅 admin 持有：
//     该权限不以 `:read` 结尾，故不会像 `*:read` 那样自动授予 viewer）。
//  3. **白名单取值、敏感字段只出布尔**：绝不反射整个 Config 结构（那样新增一个
//     secret 字段就默认泄漏）；未知字段的失效方向是「不可见」而非「泄漏」。
package controlplane

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	httppprof "net/http/pprof"
	"net/url"
	"runtime"
	"runtime/debug"
	runtimepprof "runtime/pprof"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/version"
)

// processStartedAt 进程启动时刻（包初始化 ≈ 进程启动），用于输出 uptime。
var processStartedAt = time.Now()

// diagPermission 诊断类端点的权限点。
//
// 刻意不以 `:read` 结尾：RBAC 派生规则把**所有** `*:read` 自动授予 viewer，
// 而配置转储/诊断包含内部拓扑与配置细节，只应给 admin（admin 自动获得全部权限点）。
const diagPermission = "diagnostics:dump"

// levelPermission 运行期日志级别开关的权限点（operator 亦可，见 sql_rbac.go 注释）。
const levelPermission = "diagnostics:execute"

// handleVersion 处理 GET /version：返回构建信息，供客户现场排障与版本核对。
//
// 无鉴权（与 /healthz 同级的最小暴露面）：只有版本号、提交、构建时间、Go 运行时
// 与存活时长——都是非机密且对排障必需的信息。
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	paginate.WriteJSON(w, http.StatusOK, s.versionInfo())
}

// versionInfo 组装版本信息（/version 与诊断包共用，保证两处口径一致）。
func (s *Server) versionInfo() map[string]any {
	info := map[string]any{
		"name":          "opsmesh",
		"version":       version.Version,
		"commit":        version.Commit,
		"date":          version.Date,
		"goVersion":     runtime.Version(),
		"goos":          runtime.GOOS,
		"goarch":        runtime.GOARCH,
		"uptimeSeconds": int(time.Since(processStartedAt).Seconds()),
	}
	// VCS 信息来自 Go 构建内嵌（-buildvcs，默认开启）：即使 -ldflags 注入失效，
	// 也能据此定位到源码版本——这对「客户跑的是哪个提交」类排障很关键。
	if bi, ok := buildInfo(); ok {
		info["vcsRevision"] = bi.revision
		info["vcsTime"] = bi.time
		info["vcsModified"] = bi.modified
	}
	return info
}

// handleAdminConfig 处理 GET /api/v1/admin/config：返回**脱敏后**的生效配置。
//
// 用途：客户现场核对「实际生效的配置是什么」（启动参数/env/默认值三者叠加后的结果），
// 这是排障中最常被问到、也最容易被误答的一项。
func (s *Server) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, diagPermission); !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusOK, s.configSnapshot())
}

// handleAdminLogLevel 处理 POST /api/v1/admin/loglevel：运行期调整日志级别（P1-6）。
//
// 为什么要有这个端点：排障常常是「先复现、再开 debug、拿到日志后立刻关回去」，
// 而改 --log-level 需要重启进程——重启本身就会改变正在被观察的状态（连接、leader、
// 计数器归零），观察窗口就废了。故提供免重启开关。
//
// 权限用 diagnostics:execute（不是 diagnostics:dump）：现场运维该能提级别，
// 但不该因此看到含内部拓扑的配置转储。
func (s *Server) handleAdminLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, levelPermission); !ok {
		return
	}
	var body struct {
		Level string `json:"level"`
	}
	// 400 携 err.Error() 是本仓既有约定（泄漏门禁只禁 5xx 携带原始错误）。
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	lv, err := logx.ParseLevel(body.Level)
	if err != nil {
		// 与启动期同一条判据：非法值明确拒绝，不静默沿用旧级别。
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	prev := logx.Level()
	logx.SetLevel(lv)
	// 级别变更本身必须落日志（否则事后无法证明"这段时间为什么日志突然变多"）。
	logx.Warn(r.Context(), "日志级别已运行期调整", "from", prev.String(), "to", lv.String(),
		"hint", "debug 日志量大且可能含敏感上下文，排障结束后请改回 info")
	paginate.WriteJSON(w, http.StatusOK, map[string]any{
		"previousLevel": strings.ToLower(prev.String()),
		"level":         strings.ToLower(lv.String()),
	})
}

// handleAdminDiagnostics 处理 GET /api/v1/admin/diagnostics：打包诊断材料（zip 流）。
//
// 内容：version/config/health/metrics/goroutines + 说明文件。目标是把「客户现场要
// 提供的排障材料」一次拿全，避免反复往返索要（这是支持成本的主要来源）。
func (s *Server) handleAdminDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireProd(w, r, diagPermission); !ok {
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", diagnosticsFileName()))
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	add := func(name string, content []byte) {
		fw, err := zw.Create(name)
		if err != nil {
			logx.Warn(r.Context(), "诊断包：创建条目失败", "entry", name, "err", err)
			return
		}
		if _, err := fw.Write(content); err != nil {
			logx.Warn(r.Context(), "诊断包：写入条目失败", "entry", name, "err", err)
		}
	}

	add("README.txt", []byte(diagnosticsReadme))
	add("version.json", jsonIndent(s.versionInfo()))
	add("config.json", jsonIndent(s.configSnapshot()))
	add("health.json", jsonIndent(s.healthSnapshot(r.Context())))
	add("metrics.txt", []byte(s.renderPrometheus(r.Context())))
	// goroutine 用 debug=1（聚合后的调用栈）而非 debug=2（每 goroutine 一份完整栈）：
	// 前者是排「卡在哪/泄漏在哪」所需的形态，且体积可控（后者在万级 goroutine 下可达数百 MB）。
	add("goroutines.txt", dumpGoroutines(1, 512*1024))
	if err := zw.Close(); err != nil {
		logx.Warn(r.Context(), "诊断包：关闭 zip 失败", "err", err)
	}
}

// diagnosticsFileName 诊断包文件名（含版本与时间，便于多份材料并存与排序）。
func diagnosticsFileName() string {
	return fmt.Sprintf("opsmesh-diagnostics-%s-%s.zip", version.Version, time.Now().UTC().Format("20060102T150405Z"))
}

const diagnosticsReadme = `OpsMesh 诊断包

本包由控制面 GET /api/v1/admin/diagnostics 生成，供客户现场排障与支持团队分析。

文件说明：
  version.json   构建信息（版本/提交/构建时间/Go 版本/存活时长）
  config.json    生效配置（已脱敏：口令/密钥/令牌一律不出值，只标 configured）
  health.json    深度健康检查结果（store 等依赖）
  metrics.txt    Prometheus 文本指标（独立 metrics 端口 9091 的那一套，即监控抓取的主集；
                 注意 B/S 端口 8080 的 /metrics 输出的是另一组应用级计数，两者不同名同义）
  goroutines.txt Go goroutine 调用栈（debug=1，聚合形态）

脱敏策略（可向客户安全承诺）：
  - 所有口令、密钥、令牌、连接串中的凭证均不落盘，仅输出 configured=true|false；
  - URL 类字段剥除用户名/口令与查询参数（防着 URL 内查询串携带凭证）；
  - 不包含租户数据、用户名单、任务内容、日志正文。

注意：config.json 仍包含内部拓扑（对端地址、网段白名单、端口等）。请按贵司
敏感信息管理规定流转本文件。
`

// ── pprof（默认关闭，开启后仍受 CIDR 准入）────────────────────────────

// registerPprof 按 --debug-pprof 注册 net/http/pprof 处理器（P1-6 可支撑性）。
//
// 默认关闭：pprof 能读到进程内存、调用栈与阻塞剖面，属内部诊断面。开启时仍复用
// --metrics-allow-cidr 做来源准入（与 /metrics 同一白名单）：生产模式下该白名单
// fail-closed（空即拒），故「生产要能用 pprof」必须显式放开来源——两层门槛叠加。
//
// 另注：net/http/pprof 在 init 时会向 http.DefaultServeMux 注册 /debug/pprof/*。
// 本二进制（控制面/agent）的每个 http.Server 都显式设置 Handler、无人服务
// DefaultServeMux（2026-09-26 全仓核对 internal/ 与 cmd/），故不会因此意外暴露。
func (s *Server) registerPprof(mux *http.ServeMux) {
	if s.cfg == nil || !s.cfg.DebugPprof {
		return
	}
	logx.Warn(context.Background(), "已启用 /debug/pprof/（调试面）",
		"hint", "受 --metrics-allow-cidr 白名单准入；仅应在排障期间开启，用完请关闭并重启")
	allow := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.metricsAllowed(r.RemoteAddr) {
				logx.Warn(r.Context(), "pprof 访问被拒（不在 CIDR 白名单）", "remote", r.RemoteAddr)
				paginate.JSONError(w, http.StatusForbidden, "pprof access denied")
				return
			}
			h(w, r)
		}
	}
	for _, p := range []string{
		"cmdline", "profile", "symbol", "trace",
		"goroutine", "heap", "allocs", "block", "mutex", "threadcreate",
	} {
		mux.HandleFunc("/debug/pprof/"+p, allow(httppprof.Handler(p).ServeHTTP))
	}
	mux.HandleFunc("/debug/pprof/", allow(httppprof.Index))
}

// ── 配置快照（脱敏）────────────────────────────────────────────────

// configSnapshot 返回可安全外发的配置视图。
//
// 白名单：只列非机密且排障必需的原值；敏感字段只出 configured 布尔。
// 若后续给 Config 新增字段，默认不会出现在本视图中（失效方向是「不可见」）。
func (s *Server) configSnapshot() map[string]any {
	c := s.cfg
	if c == nil {
		return map[string]any{"available": false, "reason": "server without config"}
	}
	return map[string]any{
		"available": true,
		"runtime": map[string]any{
			"mode":              c.Mode,
			"addr":              c.Addr,
			"segment":           c.Segment,
			"httpPort":          c.HTTPPort,
			"grpcPort":          c.GRPCPort,
			"metricsPort":       c.MetricsPort,
			"production":        c.Production,
			"demo":              c.Demo,
			"replicas":          c.Replicas,
			"maxProcs":          c.MaxProcs,
			"maxFiles":          c.MaxFiles,
			"maxMemoryMB":       c.MaxMemoryMB,
			"dataDir":           c.DataDir,
			"logLevel":          logxLevelName(),
			"workerConcurrency": c.WorkerConcurrency,
		},
		"store": map[string]any{
			"backend":      c.Store,
			"redisAddr":    c.RedisAddr,
			"multiSchema":  c.MultiSchema,
			"schemaPrefix": c.SchemaPrefix,
			"eventBus":     c.EventBus,
			"kafkaBrokers": c.KafkaBrokers,
			"kafkaTopic":   c.KafkaTopic,
			// MySQLDSN 含口令：整串不出，只标是否配置。
			"mysqlDsnConfigured": c.MySQLDSN != "",
		},
		"auth": map[string]any{
			"requireAuth":                c.RequireAuth,
			"publicRegister":             c.PublicRegister,
			"allowPublicRegister":        c.AllowPublicRegister,
			"cookieSecure":               c.CookieSecure,
			"trustProxy":                 c.TrustProxy,
			"trustGatewayHeaders":        c.TrustGatewayHeaders,
			"jwtIssuer":                  c.JWTIssuer,
			"sessionStore":               c.SessionStore,
			"allowedOrigins":             c.AllowedOrigins,
			"jwtSecretConfigured":        c.JWTSecret != "",
			"jwtPublicKeyConfigured":     c.JWTPublicKey != "",
			"encryptionKeyConfigured":    c.EncryptionKey != "",
			"adminPasswordConfigured":    c.AdminPassword != "" || c.AdminPasswordFile != "",
			"installTokenConfigured":     c.InstallToken != "",
			"provisionSecretConfigured":  c.ProvisionSecret != "",
			"federationSecretConfigured": c.FederationSecret != "",
		},
		"tls": map[string]any{
			"httpTls":                 c.HTTPTLS,
			"certFile":                c.TLSCert, // 路径非机密；值为空表示未配置
			"clientCaFile":            c.ClientCA,
			"tlsWatch":                c.TLSWatch,
			"keyFileConfigured":       c.TLSKey != "",
			"federationPort":          c.FederationPort,
			"federationCertFile":      c.FederationTLSCert,
			"federationCaFile":        c.FederationCA,
			"federationKeyConfigured": c.FederationTLSKey != "",
		},
		"secrets": map[string]any{
			"provider": c.SecretProvider,
			"file":     c.SecretFile,
			"vault": map[string]any{
				"addr":            c.VaultAddr,
				"mount":           c.VaultMount,
				"tokenConfigured": c.VaultToken != "",
			},
			"kms": map[string]any{
				"endpoint": c.KmsEndpoint,
				// KmsKeyID 是 KMS 侧定位符，不属密钥本体，但排障收益有限，故按敏感处理。
				"keyIdConfigured": c.KmsKeyID != "",
				"tokenConfigured": c.KmsToken != "",
			},
		},
		"discovery": map[string]any{
			"discover":                   c.Discover,
			"segmentCidr":                c.SegmentCIDR,
			"autoProvision":              c.AutoProvision,
			"provisionCidrWhitelist":     c.ProvisionCIDRWhitelist,
			"provisionSshUser":           c.ProvisionSSHUser,
			"provisionSshKeyConfigured":  c.ProvisionSSHKey != "",
			"provisionSshPassConfigured": c.ProvisionSSHKP != "",
			"provisionSshKnownHosts":     c.ProvisionSSHKnownHosts,
			"deviceFpDeadline":           rfc3339OrEmpty(c.DeviceFPDeadline),
		},
		"agent": map[string]any{
			"shellWhitelist":             c.AgentShellWhitelist,
			"shellWhitelistDefault":      c.AgentShellWhitelistDefault,
			"fileRootWhitelist":          c.AgentFileRootWhitelist,
			"grpcRequireSignature":       c.GRPCRequireSignature,
			"grpcSignatureKeyConfigured": c.GRPCSignatureKey != "",
			"controlAddr":                c.ControlAddr,
			"controlAddrs":               c.ControlAddrs,
			"controlplaneEndpoints":      c.ControlplaneEndpoints,
			"lbStrategy":                 c.LBStrategy,
		},
		"observability": map[string]any{
			"metricsAllowCidr":         c.MetricsAllowCIDR,
			"logStore":                 c.LogStore,
			"logBackend":               c.LogBackend,
			"lokiEndpoint":             redactURL(c.LokiEndpoint),
			"esEndpoint":               redactURL(c.ESEndpoint),
			"esIndex":                  c.ESIndex,
			"otelEndpoint":             redactURL(c.OTELEndpoint),
			"otelServiceName":          c.OTELServiceName,
			"otelStdout":               c.OTELStdout,
			"logPushEnabled":           c.LogPushEnabled,
			"logPushFileCount":         len(c.LogPushFiles),
			"logPushPattern":           c.LogPushPattern,
			"logPushBackend":           c.LogPushBackend,
			"logPushEndpoint":          redactURL(c.LogPushEndpoint),
			"alertNotifierType":        c.AlertNotifierType,
			"alertWebhookUrl":          redactURL(c.AlertWebhookURL),
			"alertEmailHost":           c.AlertEmailHost,
			"alertEmailPort":           c.AlertEmailPort,
			"alertEmailUser":           c.AlertEmailUser,
			"alertEmailFrom":           c.AlertEmailFrom,
			"alertEmailTo":             c.AlertEmailTo,
			"alertEmailPassConfigured": c.AlertEmailPass != "",
			"notifyChannels":           c.NotifyChannels,
		},
		"limits": map[string]any{
			"taskTimeout":            c.TaskTimeout.String(),
			"shutdownTimeout":        c.ShutdownTimeout.String(),
			"taskLeaseSec":           c.TaskLeaseSec,
			"taskMaxRetries":         c.TaskMaxRetries,
			"leaderTtlSec":           c.LeaderTTLSec,
			"leaderTickSec":          c.LeaderTickSec,
			"archiveAgeMin":          c.ArchiveAgeMin,
			"auditRetentionDays":     c.AuditRetentionDays,
			"cbFailureThreshold":     c.CBFailureThreshold,
			"cbRecoveryTimeout":      c.CBRecoveryTimeout.String(),
			"cbHalfOpenMaxCalls":     c.CBHalfOpenMaxCalls,
			"cbRateLimitPerSec":      c.CBRateLimitPerSec,
			"webhookAllowPrivate":    c.WebhookAllowPrivate,
			"quotaEnabled":           c.QuotaEnabled,
			"quotaMaxDevices":        c.QuotaMaxDevices,
			"quotaMaxTasks":          c.QuotaMaxTasks,
			"quotaMaxAlerts":         c.QuotaMaxAlerts,
			"allowStubStores":        c.AllowStubStores,
			"anomalyDetection":       c.AnomalyDetection,
			"anomalyWindowSize":      c.AnomalyWindowSize,
			"anomalyThreshold":       c.AnomalyThreshold,
			"automationEvalInterval": c.AutomationEvalInterval.String(),
			"inhibitRulesFile":       c.InhibitRulesFile,
		},
	}
}

// redactURL 剥除 URL 中的用户信息与查询参数，只保留 scheme://host[:port]/path。
//
// 为什么必须脱敏：`--log-push-endpoint` / `--alert-webhook-url` 这类 URL 常被写成
// `https://user:pass@host/path` 或 `?token=...`，直接回显等于把凭证写进诊断包。
// 非 URL 或解析失败：返回是否非空的布尔描述，绝不回显原文（原文可能含凭证）。
func redactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<已配置（非标准 URL，内容不展示）>"
	}
	// Query 与 User 一律丢弃；Fragment 亦不含所需信息。
	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
}

// rfc3339OrEmpty 便于把 time.Time 字段以稳定格式输出（零值输出空串）。
func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// logxLevelName 返回当前生效的日志级别名（排障第一问：日志级别是不是被调高了）。
func logxLevelName() string {
	return strings.ToLower(logx.Level().String())
}

// ── 健康与指标复用（诊断包与 /healthz、/metrics 同源，避免两处口径漂移）──

// healthSnapshot 复用 pingStore 做与 /healthz 相同的深度检查。
func (s *Server) healthSnapshot(ctx context.Context) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := map[string]any{"status": "ok", "checks": map[string]string{"store": "ok"}}
	if err := s.pingStore(ctx); err != nil {
		out["status"] = "unhealthy"
		out["checks"] = map[string]string{"store": "unavailable"}
	}
	if s.store != nil {
		out["isLeader"] = s.store.IsLeader()
	}
	return out
}

// renderPrometheus 复用与 /metrics 相同的渲染路径。
func (s *Server) renderPrometheus(ctx context.Context) string {
	if s.metrics == nil {
		return "# metrics 未启用\n"
	}
	s.metrics.SetAgents(len(s.store.Agents("")))
	return s.metrics.Render()
}

// ── 小工具 ─────────────────────────────────────────────────────────

// jsonIndent 把 map 序列化为缩进 JSON（诊断包内为人类可读；失败时返回错误说明而非空文件）。
func jsonIndent(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte(fmt.Sprintf("序列化失败: %v\n", err))
	}
	return append(b, '\n')
}

// vcsBuildInfo 是 Go 构建内嵌的 VCS 元信息（-buildvcs 默认开启）。
type vcsBuildInfo struct {
	revision string
	time     string
	modified bool
}

// buildInfo 读取构建内嵌的 VCS 信息；未内嵌（如 -buildvcs=false）时 ok=false。
// 它不依赖 -ldflags，故即使版本注入失效也能定位源码版本。
func buildInfo() (vcsBuildInfo, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return vcsBuildInfo{}, false
	}
	var out vcsBuildInfo
	found := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			out.revision, found = s.Value, true
		case "vcs.time":
			out.time = s.Value
		case "vcs.modified":
			out.modified = s.Value == "true"
		}
	}
	return out, found
}

// dumpGoroutines 抓取 goroutine 调用栈（debug=1 聚合形态），最多 limit 字节。
// 上限的意义：即使进程有数万 goroutine，也不能让诊断包把内存/带宽打满。
func dumpGoroutines(debug int, limit int) []byte {
	var buf bytes.Buffer
	cw := &capWriter{w: &buf, limit: limit}
	// Lookup 对未知 profile 名返回 nil（此处 "goroutine" 恒定存在，但缺它就不能盲目解引用）。
	gp := runtimepprof.Lookup("goroutine")
	if gp == nil {
		return []byte("无 goroutine profile 可用\n")
	}
	if err := gp.WriteTo(cw, debug); err != nil {
		_, _ = fmt.Fprintf(&buf, "\n... [调用栈抓取中断: %v]\n", err)
	}
	if cw.truncated {
		buf.WriteString("\n... [已截断：调用栈超过上限]\n")
	}
	return buf.Bytes()
}

// capWriter 达到上限后静默丢弃（并记录被截断）。
type capWriter struct {
	w         *bytes.Buffer
	limit     int
	truncated bool
}

func (c *capWriter) Write(p []byte) (int, error) {
	remain := c.limit - c.w.Len()
	if remain <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		c.truncated = true
		c.w.Write(p[:remain])
		return len(p), nil
	}
	return c.w.Write(p)
}
