// server_bootstrap.go — 纳管 bootstrap：install.sh 分发 + agent 二进制分发 + 自动纳管
package controlplane

import (
	"context"
	"crypto/hmac"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/version"
	"github.com/Levango7/OpsMesh/pkg/provision"
)

func (s *Server) verifyBootstrapToken(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg != nil && s.cfg.Demo {
		return true // demo 模式放宽：本地体验不要求 token
	}
	// 期望 token：ProvisionSecret。空配置时拒绝所有非 demo 访问（防误配开放）。
	expected := ""
	if s.cfg != nil {
		expected = s.cfg.ProvisionSecret
	}
	// 提取请求 token：优先 Authorization: Bearer，回退 ?token= 查询参数。
	got := ""
	if tok, err := extractBearer(r); err == nil && tok != "" {
		got = tok
	} else if q := r.URL.Query().Get("token"); q != "" {
		got = q
	}
	if expected == "" || got == "" || !hmac.Equal([]byte(got), []byte(expected)) {
		paginate.JSONError(w, http.StatusUnauthorized, "bootstrap token required (Authorization: Bearer <secret> or ?token=<secret>)")
		return false
	}
	return true
}

// handleInstallSh 处理 GET /install.sh：下发 agent 自举安装脚本（B1 bootstrap）。
// 脚本由 provision.InstallScript 按 --advertise-addr 动态生成（内嵌下载地址），
// 配合 bootstrap 命令 `curl -sSL <addr>/install.sh | sh -s -- --token=<tok>` 完成 agent 安装与注册。
//
// 安全加固：原端点完全开放，现加 token 校验（demo 模式放宽）。
func (s *Server) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.verifyBootstrapToken(w, r) {
		return
	}
	// 访问审计：install.sh 是 bootstrap 端点，保持开放但审计访问来源供溯源。
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "bootstrap_install_sh", Target: "/install.sh",
		Detail: "remote=" + r.RemoteAddr,
	})
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(provision.InstallScript(s.cfg.AdvertiseAddr, version.Version))); err != nil {
		logx.Error(r.Context(), "controlplane: handleInstallSh 写安装脚本失败", err)
	}
}

// handleServeAgent 处理 GET /bin/opsmesh-agent：按 ?os=&arch= 分发 agent 二进制（A1 多架构），
// 供 install.sh 脚本下载安装。
//
// 查找顺序（先到先得）：
//  1. --agent-bin-dir / OPSMESH_AGENT_BIN_DIR 目录下按平台/架构命名产物
//     opsmesh-agent-{os}-{arch}（如 opsmesh-agent-linux-arm64）；
//  2. 同目录 generic 产物 opsmesh-agent；
//  3. 请求未带 ?arch=（旧客户端/未探测）时回退当前进程二进制；
//  4. 请求带 ?arch= 且与本进程平台不一致、目录又无对应产物 → 404 如实报错，
//     绝不静默下发错架构二进制。
//
// os/arch 仅接受 [a-z0-9] 词元（防路径穿越）；缺省 linux/amd64，
// 语义对齐 device-svc 的 detectPlatform/resolveAgentBinary。
func (s *Server) handleServeAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.verifyBootstrapToken(w, r) {
		return
	}
	// 访问审计：agent 二进制分发端点，保持开放但审计下载来源供溯源。
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "bootstrap_serve_agent", Target: "/bin/opsmesh-agent",
		Detail: "remote=" + r.RemoteAddr,
	})
	q := r.URL.Query()
	archSpecified := q.Get("arch") != ""
	osName, arch := detectBootstrapPlatform(q.Get("os"), q.Get("arch"))
	path, actualOS, actualArch := s.resolveAgentBinary(osName, arch, !archSpecified)
	if path == "" {
		paginate.JSONError(w, http.StatusNotFound, fmt.Sprintf("agent binary not found for %s/%s (set --agent-bin-dir and place opsmesh-agent-%s-%s there)", osName, arch, osName, arch))
		return
	}
	// G703 取证：path 源自 query，但经 bootstrapSafeToken [a-z0-9] 白名单约束 +
	// filepath.Join 于 AgentBinDir 之下，无穿越面（独立 gosec 步不认 //nolint 形态，
	// 故用 gosec 原生 #nosec 行尾注释；golangci-lint 的 gosec 引擎同认）
	f, err := os.Open(path) // #nosec G703
	if err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "cannot open agent binary")
		return
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "cannot stat agent binary")
		return
	}
	w.Header().Set("X-OpsMesh-Agent-OS", actualOS)
	w.Header().Set("X-OpsMesh-Agent-Arch", actualArch)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=opsmesh-agent")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		logx.Error(r.Context(), "controlplane: handleServeAgent 写 agent 二进制失败", err)
	}
}

// detectBootstrapPlatform 归一化 os/arch 查询参数，缺省 linux/amd64。
func detectBootstrapPlatform(osName, arch string) (string, string) {
	if osName == "" {
		osName = "linux"
	}
	if arch == "" {
		arch = "amd64"
	}
	return osName, arch
}

// resolveAgentBinary 按平台/架构解析 agent 二进制路径；找不到返回 ""。
// 同时返回实际下发产物的 os/arch（走本进程二进制时为 runtime 值，供响应头如实描述）。
func (s *Server) resolveAgentBinary(osName, arch string, selfAllowed bool) (string, string, string) {
	if !bootstrapSafeToken(osName) || !bootstrapSafeToken(arch) {
		return "", osName, arch
	}
	if s.cfg != nil && s.cfg.AgentBinDir != "" {
		for _, name := range []string{
			fmt.Sprintf("opsmesh-agent-%s-%s", osName, arch),
			"opsmesh-agent",
		} {
			if p := filepath.Join(s.cfg.AgentBinDir, name); bootstrapFileExists(p) {
				return p, osName, arch
			}
		}
	}
	if selfAllowed || (osName == runtime.GOOS && arch == runtime.GOARCH) {
		if p, err := os.Executable(); err == nil {
			return p, runtime.GOOS, runtime.GOARCH
		}
	}
	return "", osName, arch
}

// bootstrapSafeToken 限定 os/arch 词元为小写数字字母，防路径穿越。
func bootstrapSafeToken(v string) bool {
	if v == "" {
		return false
	}
	for _, c := range v {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// bootstrapFileExists 判存在且为常规文件（拒绝目录等）。
//
// bootstrapFileExists 判存在且为常规文件（拒绝目录等）。
func bootstrapFileExists(p string) bool {
	// G703 取证同 os.Open：p 由 bootstrapSafeToken 白名单词元拼成
	info, err := os.Stat(p) // #nosec G703
	return err == nil && !info.IsDir()
}

// handleAutoProvision 处理 POST /api/v1/provision/auto：手动触发 自动纳管编排。
// body: {"cidrs":["10.30.0.0/24"], "tenantID":"t1"}；cidrs 缺省时回退 --segment-cidr。
func (s *Server) handleAutoProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "provision:execute"); !ok {
		return
	}
	var body struct {
		CIDRs    []string `json:"cidrs"`
		TenantID string   `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil && r.ContentLength != 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}
	cidrs := body.CIDRs
	if len(cidrs) == 0 && s.cfg.SegmentCIDR != "" {
		cidrs = []string{s.cfg.SegmentCIDR}
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	tenant := actx.TenantID
	if len(cidrs) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no cidrs provided (body.cidrs or --segment-cidr)"})
		return
	}
	// 修复 7：SSRF 校验 advertise URL（仅警告不阻止，控制面常部署内网）。
	// advertise URL 是控制面自身地址（运维配置），非用户可控，SSRF 校验仅做安全审计告警。
	if s.cfg.AdvertiseAddr != "" {
		if err := validateURLSSRF(s.cfg.AdvertiseAddr); err != nil {
			logx.Warn(r.Context(), "AdvertiseAddr SSRF 校验失败（仅警告，控制面常部署内网）", "url", s.cfg.AdvertiseAddr, "err", err)
		}
	}
	// SSRF 防护：autoProvision CIDR 白名单校验。
	// 白名单非空时，每个目标 CIDR 必须完全落在白名单内，防止运维误配置或攻击者构造请求
	// 扫描任意网段（如 169.254.169.254 元数据网段获取云凭据，或扫描内网其他网段做内网探测）。
	// 白名单为空时不校验（向后兼容）。校验失败返回 403 Forbidden。
	if s.cfg.ProvisionCIDRWhitelist != "" {
		allowedCIDRs := strings.Split(s.cfg.ProvisionCIDRWhitelist, ",")
		for _, cidr := range cidrs {
			if err := ValidateCIDR(cidr, allowedCIDRs); err != nil {
				logx.Warn(r.Context(), "autoProvision CIDR 白名单校验失败", "cidr", cidr, "err", err)
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": fmt.Sprintf("CIDR %q not allowed by provision-cidr-whitelist: %v", cidr, err)})
				return
			}
		}
	}
	sum, err := provision.AutoProvision(r.Context(), provision.DeviceDeps{
		UpsertDevice: func(deviceID, ip, cidr, tenantID string) {
			s.store.UpsertDevice(&proto.DeviceInfo{
				DeviceID: deviceID,
				IP:       ip,
				Segment:  cidr,
				TenantID: tenantID,
				State:    "discovered",
				Managed:  false,
			})
		},
		Provision: s.store.Provision,
	}, provision.Config{
		AdvertiseAddr:          s.cfg.AdvertiseAddr,
		FallbackAdvertise:      fmt.Sprintf("http://127.0.0.1:%d", s.cfg.HTTPPort),
		Production:             s.cfg.Production,
		ProvisionSSHUser:       s.cfg.ProvisionSSHUser,
		ProvisionSSHKey:        s.cfg.ProvisionSSHKey,
		ProvisionSSHKP:         s.cfg.ProvisionSSHKP,
		ProvisionSSHKnownHosts: s.cfg.ProvisionSSHKnownHosts,
	}, cidrs, tenant)
	if err != nil {
		writeInternalError(r.Context(), w, "bootstrap.autoProvision", err)
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: actx.UserID, Action: "auto_provision", Target: strings.Join(cidrs, ","),
		Detail: fmt.Sprintf("scanned=%d registered=%d provisioned=%d sshPushed=%d", sum.Scanned, sum.Registered, sum.Provisioned, sum.SSHPushed),
	})
	paginate.WriteJSON(w, http.StatusOK, sum)
}

// autoProvisionLoop 后台周期执行 自动纳管：仅当 --discover && --auto-provision 开启时，
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
		if s.store.IsLeader() && s.cfg.SegmentCIDR != "" {
			// SSRF 防护：后台循环同样校验 CIDR 白名单。
			// 白名单非空时 --segment-cidr 必须在白名单内，否则跳过本轮扫描并告警
			// （不 fail-fast，避免后台循环崩溃；配置错误应由启动期 Validate 兜底）。
			if s.cfg.ProvisionCIDRWhitelist != "" {
				allowedCIDRs := strings.Split(s.cfg.ProvisionCIDRWhitelist, ",")
				if err := ValidateCIDR(s.cfg.SegmentCIDR, allowedCIDRs); err != nil {
					logx.Warn(ctx, "controlplane: autoProvisionLoop CIDR 白名单校验失败，跳过本轮", err)
				} else {
					if _, err := provision.AutoProvision(ctx, provision.DeviceDeps{
						UpsertDevice: func(deviceID, ip, cidr, tenantID string) {
							s.store.UpsertDevice(&proto.DeviceInfo{
								DeviceID: deviceID,
								IP:       ip,
								Segment:  cidr,
								TenantID: tenantID,
								State:    "discovered",
								Managed:  false,
							})
						},
						Provision: s.store.Provision,
					}, provision.Config{
						AdvertiseAddr:          s.cfg.AdvertiseAddr,
						FallbackAdvertise:      fmt.Sprintf("http://127.0.0.1:%d", s.cfg.HTTPPort),
						Production:             s.cfg.Production,
						ProvisionSSHUser:       s.cfg.ProvisionSSHUser,
						ProvisionSSHKey:        s.cfg.ProvisionSSHKey,
						ProvisionSSHKP:         s.cfg.ProvisionSSHKP,
						ProvisionSSHKnownHosts: s.cfg.ProvisionSSHKnownHosts,
					}, []string{s.cfg.SegmentCIDR}, ""); err != nil {
						logx.Error(ctx, "controlplane: autoProvisionLoop 自动纳管失败", err)
					}
				}
			} else {
				if _, err := provision.AutoProvision(ctx, provision.DeviceDeps{
					UpsertDevice: func(deviceID, ip, cidr, tenantID string) {
						s.store.UpsertDevice(&proto.DeviceInfo{
							DeviceID: deviceID,
							IP:       ip,
							Segment:  cidr,
							TenantID: tenantID,
							State:    "discovered",
							Managed:  false,
						})
					},
					Provision: s.store.Provision,
				}, provision.Config{
					AdvertiseAddr:          s.cfg.AdvertiseAddr,
					FallbackAdvertise:      fmt.Sprintf("http://127.0.0.1:%d", s.cfg.HTTPPort),
					Production:             s.cfg.Production,
					ProvisionSSHUser:       s.cfg.ProvisionSSHUser,
					ProvisionSSHKey:        s.cfg.ProvisionSSHKey,
					ProvisionSSHKP:         s.cfg.ProvisionSSHKP,
					ProvisionSSHKnownHosts: s.cfg.ProvisionSSHKnownHosts,
				}, []string{s.cfg.SegmentCIDR}, ""); err != nil {
					logx.Error(ctx, "controlplane: autoProvisionLoop 自动纳管失败", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// writeJSON 统一写出 JSON 响应。
