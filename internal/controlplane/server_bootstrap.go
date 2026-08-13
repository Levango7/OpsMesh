// server_bootstrap.go — B1 纳管 bootstrap：install.sh 分发 + agent 二进制分发 + 自动纳管
package controlplane

import (
	"context"
	"crypto/hmac"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"opsmesh/internal/logx"
	"opsmesh/internal/proto"
	"opsmesh/internal/provision"
	"opsmesh/internal/version"
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
		jsonError(w, http.StatusUnauthorized, "bootstrap token required (Authorization: Bearer <secret> or ?token=<secret>)")
		return false
	}
	return true
}

// handleInstallSh 处理 GET /install.sh：下发 agent 自举安装脚本（B1 bootstrap）。
// 脚本由 provision.InstallScript 按 --advertise-addr 动态生成（内嵌下载地址），
// 配合 bootstrap 命令 `curl -sSL <addr>/install.sh | sh -s -- --token=<tok>` 完成 agent 安装与注册。
//
// P0-G1 安全加固：原端点完全开放，现加 token 校验（demo 模式放宽）。
func (s *Server) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.verifyBootstrapToken(w, r) {
		return
	}
	// P1-5 访问审计：install.sh 是 bootstrap 端点，保持开放但审计访问来源供溯源。
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "bootstrap_install_sh", Target: "/install.sh",
		Detail: "remote=" + r.RemoteAddr,
	})
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(provision.InstallScript(s.cfg.AdvertiseAddr, version.Version))); err != nil {
		log.Printf("controlplane: handleInstallSh 写安装脚本失败: %v", err)
	}
}

// handleServeAgent 处理 GET /bin/opsmesh-agent：分发 agent 二进制本体（双模式同体），
// 供 install.sh 脚本下载安装。
//
// P0-G1 安全加固：原端点完全开放，现加 token 校验（demo 模式放宽）。
func (s *Server) handleServeAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.verifyBootstrapToken(w, r) {
		return
	}
	// P1-5 访问审计：agent 二进制分发端点，保持开放但审计下载来源供溯源。
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: clientIP(r, s.cfg.TrustProxy), Action: "bootstrap_serve_agent", Target: "/bin/opsmesh-agent",
		Detail: "remote=" + r.RemoteAddr,
	})
	path, err := os.Executable()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot resolve agent binary")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot open agent binary")
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
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("controlplane: handleServeAgent 写 agent 二进制失败: %v", err)
	}
}

// handleAutoProvision 处理 POST /api/v1/provision/auto：手动触发 B1 自动纳管编排。
// body: {"cidrs":["10.30.0.0/24"], "tenantID":"t1"}；cidrs 缺省时回退 --segment-cidr。
func (s *Server) handleAutoProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}
	cidrs := body.CIDRs
	if len(cidrs) == 0 && s.cfg.SegmentCIDR != "" {
		cidrs = []string{s.cfg.SegmentCIDR}
	}
	// H6 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	tenant := actx.TenantID
	if len(cidrs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no cidrs provided (body.cidrs or --segment-cidr)"})
		return
	}
	// B1 修复 7：SSRF 校验 advertise URL（仅警告不阻止，控制面常部署内网）。
	// advertise URL 是控制面自身地址（运维配置），非用户可控，SSRF 校验仅做安全审计告警。
	if s.cfg.AdvertiseAddr != "" {
		if err := validateURLSSRF(s.cfg.AdvertiseAddr); err != nil {
			logx.Warn(r.Context(), "AdvertiseAddr SSRF 校验失败（仅警告，控制面常部署内网）", "url", s.cfg.AdvertiseAddr, "err", err)
		}
	}
	// task 248 SSRF 防护：autoProvision CIDR 白名单校验。
	// 白名单非空时，每个目标 CIDR 必须完全落在白名单内，防止运维误配置或攻击者构造请求
	// 扫描任意网段（如 169.254.169.254 元数据网段获取云凭据，或扫描内网其他网段做内网探测）。
	// 白名单为空时不校验（向后兼容）。校验失败返回 403 Forbidden。
	if s.cfg.ProvisionCIDRWhitelist != "" {
		allowedCIDRs := strings.Split(s.cfg.ProvisionCIDRWhitelist, ",")
		for _, cidr := range cidrs {
			if err := ValidateCIDR(cidr, allowedCIDRs); err != nil {
				logx.Warn(r.Context(), "autoProvision CIDR 白名单校验失败", "cidr", cidr, "err", err)
				writeJSON(w, http.StatusForbidden, map[string]string{"error": fmt.Sprintf("CIDR %q not allowed by provision-cidr-whitelist: %v", cidr, err)})
				return
			}
		}
	}
	sum, err := provision.AutoProvision(r.Context(), provision.Deps{
		UpsertDevice: s.store.UpsertDevice,
		Provision:    s.store.Provision,
	}, s.cfg, cidrs, tenant)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// M1-4：携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
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
		if s.store.IsLeader() && s.cfg.SegmentCIDR != "" {
			// task 248 SSRF 防护：后台循环同样校验 CIDR 白名单。
			// 白名单非空时 --segment-cidr 必须在白名单内，否则跳过本轮扫描并告警
			// （不 fail-fast，避免后台循环崩溃；配置错误应由启动期 Validate 兜底）。
			if s.cfg.ProvisionCIDRWhitelist != "" {
				allowedCIDRs := strings.Split(s.cfg.ProvisionCIDRWhitelist, ",")
				if err := ValidateCIDR(s.cfg.SegmentCIDR, allowedCIDRs); err != nil {
					log.Printf("controlplane: autoProvisionLoop CIDR 白名单校验失败，跳过本轮: %v", err)
				} else {
					if _, err := provision.AutoProvision(ctx, provision.Deps{
						UpsertDevice: s.store.UpsertDevice,
						Provision:    s.store.Provision,
					}, s.cfg, []string{s.cfg.SegmentCIDR}, ""); err != nil {
						log.Printf("controlplane: autoProvisionLoop 自动纳管失败: %v", err)
					}
				}
			} else {
				if _, err := provision.AutoProvision(ctx, provision.Deps{
					UpsertDevice: s.store.UpsertDevice,
					Provision:    s.store.Provision,
				}, s.cfg, []string{s.cfg.SegmentCIDR}, ""); err != nil {
					log.Printf("controlplane: autoProvisionLoop 自动纳管失败: %v", err)
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
