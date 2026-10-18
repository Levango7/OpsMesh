// ssh_trust_ops.go SSH 互信运维（密钥生成/公钥分发/撤销/免密测试/清单查询）。
//
// 设计原则（延续 P0-P6"查询走 API、执行走 shell"架构）：
//   - 全部动作以 shell 模板下发到 agent 执行（无状态、可审计）。
//   - 典型闭环：源设备 keygen 拿到公钥 → 目标设备 authorize 写入 → 源设备 test 验证免密。
//     该闭环是 Ansible 免密、集群多节点互信（Kafka/Redis/ES）、rsync/scp 跨机分发的前置。
//
// API 端点（POST /api/v1/ssh-trust/{action}）：
//   - keygen    {deviceID, user, keyType}             — 生成密钥对（不存在时），输出公钥
//   - authorize {deviceID, user, publicKey}           — 把公钥写入目标用户 authorized_keys（幂等）
//   - revoke    {deviceID, user, publicKey}           — 从 authorized_keys 移除指定公钥
//   - list      {deviceID, user}                      — 列出 authorized_keys 与授权指纹
//   - test      {deviceID, user, host, port}          — 从设备 ssh 到目标主机测试免密
//
// 安全：user 走 safeNameRe；公钥走严格格式正则（拒绝换行/NUL/heredoc 终止符注入）；
// host 走主机正则；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// SSH 互信动作白名单。
var allowedSSHTrustActions = map[string]bool{
	"keygen":    true,
	"authorize": true,
	"revoke":    true,
	"list":      true,
	"test":      true,
}

// 密钥类型白名单。
var allowedSSHKeyTypes = map[string]bool{
	"ed25519": true,
	"rsa":     true,
	"ecdsa":   true,
}

// SSH 公钥格式正则：算法 + base64 + 可选注释。
// 算法涵盖 ssh-ed25519 / ssh-rsa / ecdsa-sha2-nistp* / sk-*（FIDO）。
var sshPublicKeyRe = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|ecdsa-sha2-nistp521|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/]+={0,3}( [^\r\n]*)?$`)

// heredoc 终止符（公钥内容不得包含，防注入）。
const sshTrustHeredocEOF = "OPSMESH_PUBKEY_EOF"

// SSH 端口上下界。
const sshPortMin, sshPortMax = 1, 65535

// sshTrustReq SSH 互信请求体。
type sshTrustReq struct {
	DeviceID  string `json:"deviceID"`
	Action    string `json:"action"`    // keygen/authorize/revoke/list/test
	User      string `json:"user"`      // SSH 用户名（默认 root）
	KeyType   string `json:"keyType"`   // 密钥类型（keygen 用；默认 ed25519）
	PublicKey string `json:"publicKey"` // 公钥内容（authorize/revoke 用）
	Host      string `json:"host"`      // 目标主机（test 用）
	Port      int    `json:"port"`      // SSH 端口（test 用；默认 22）
}

// handleSSHTrust 处理 POST /api/v1/ssh-trust/{action}。
func (s *Server) handleSSHTrust(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedSSHTrustActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (keygen/authorize/revoke/list/test)"})
		return
	}
	var req sshTrustReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Action = action
	// 默认用户名与密钥类型。
	if req.User == "" {
		req.User = "root"
	}
	if !safeNameRe.MatchString(req.User) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user name"})
		return
	}

	switch action {
	case "keygen":
		if req.KeyType == "" {
			req.KeyType = "ed25519"
		}
		if !allowedSSHKeyTypes[req.KeyType] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid key type (ed25519/rsa/ecdsa)"})
			return
		}
	case "authorize", "revoke":
		if req.PublicKey == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "publicKey is required"})
			return
		}
		if !validSSHPublicKey(req.PublicKey) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ssh public key format"})
			return
		}
	case "test":
		if req.Host == "" || !monitorHostRe.MatchString(req.Host) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host"})
			return
		}
		if req.Port == 0 {
			req.Port = 22
		}
		if req.Port < sshPortMin || req.Port > sshPortMax {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid port"})
			return
		}
	}

	script := buildSSHTrustScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "ssh_trust_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "ssh_trust."+action, err)
		return
	}
	target := req.User
	if action == "test" {
		target = req.User + "@" + req.Host
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "ssh_trust_" + action, Target: target,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// validSSHPublicKey 校验公钥：格式匹配 + 无 NUL/换行 + 不含 heredoc 终止符。
func validSSHPublicKey(pk string) bool {
	if strings.ContainsAny(pk, "\x00\r\n") {
		return false
	}
	if strings.Contains(pk, sshTrustHeredocEOF) {
		return false
	}
	return sshPublicKeyRe.MatchString(pk)
}

func buildSSHTrustScript(req sshTrustReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("TARGET_USER=" + shellQuote(req.User) + "\n")
	// 解析目标用户 home（getent 优先，回退 /home 与 root）。
	b.WriteString("USER_HOME=$(getent passwd \"$TARGET_USER\" 2>/dev/null | cut -d: -f6)\n")
	b.WriteString("[ -z \"$USER_HOME\" ] && { if [ \"$TARGET_USER\" = \"root\" ]; then USER_HOME=/root; else USER_HOME=/home/$TARGET_USER; fi; }\n")

	switch req.Action {
	case "keygen":
		keyPath := "$USER_HOME/.ssh/id_ed25519"
		switch req.KeyType {
		case "rsa":
			keyPath = "$USER_HOME/.ssh/id_rsa"
		case "ecdsa":
			keyPath = "$USER_HOME/.ssh/id_ecdsa"
		}
		fmt.Fprintf(&b, "KEY_PATH=%q\n", keyPath)
		b.WriteString("if [ ! -f \"$KEY_PATH\" ]; then\n")
		b.WriteString("  mkdir -p \"$USER_HOME/.ssh\" && chmod 700 \"$USER_HOME/.ssh\"\n")
		fmt.Fprintf(&b, "  ssh-keygen -t %s -N '' -f \"$KEY_PATH\" -C \"opsmesh@$(hostname)\"\n", req.KeyType)
		b.WriteString("  chown -R \"$TARGET_USER\" \"$USER_HOME/.ssh\" 2>/dev/null || true\n")
		b.WriteString("  echo '密钥对已生成'\n")
		b.WriteString("else echo '密钥对已存在（幂等跳过）'\nfi\n")
		b.WriteString("echo '=== PUBLIC KEY ==='\n")
		b.WriteString("cat \"$KEY_PATH.pub\"\n")

	case "authorize":
		// 公钥经校验后以 quoted heredoc 写入（'EOF' 防变量展开；内容无换行/终止符）。
		b.WriteString("mkdir -p \"$USER_HOME/.ssh\" && chmod 700 \"$USER_HOME/.ssh\"\n")
		b.WriteString("[ -f \"$USER_HOME/.ssh/authorized_keys\" ] && chmod 600 \"$USER_HOME/.ssh/authorized_keys\"\n")
		b.WriteString("touch \"$USER_HOME/.ssh/authorized_keys\"\n")
		pk := req.PublicKey
		fmt.Fprintf(&b, "if grep -qF %s \"$USER_HOME/.ssh/authorized_keys\" 2>/dev/null; then\n", shellQuote(pk))
		b.WriteString("  echo '公钥已存在（幂等跳过）'\n")
		b.WriteString("else\n")
		fmt.Fprintf(&b, "  printf '%%s\\n' %s >> \"$USER_HOME/.ssh/authorized_keys\"\n", shellQuote(pk))
		b.WriteString("  echo '公钥已授权'\nfi\n")
		b.WriteString("chown -R \"$TARGET_USER\" \"$USER_HOME/.ssh\" 2>/dev/null || true\n")
		b.WriteString("chmod 600 \"$USER_HOME/.ssh/authorized_keys\"\n")

	case "revoke":
		b.WriteString("if [ ! -f \"$USER_HOME/.ssh/authorized_keys\" ]; then echo 'authorized_keys 不存在，无需撤销'; exit 0; fi\n")
		pk := req.PublicKey
		b.WriteString("BEFORE=$(wc -l < \"$USER_HOME/.ssh/authorized_keys\")\n")
		fmt.Fprintf(&b, "grep -vF %s \"$USER_HOME/.ssh/authorized_keys\" > \"$USER_HOME/.ssh/authorized_keys.tmp\" || true\n", shellQuote(pk))
		b.WriteString("mv \"$USER_HOME/.ssh/authorized_keys.tmp\" \"$USER_HOME/.ssh/authorized_keys\"\n")
		b.WriteString("chmod 600 \"$USER_HOME/.ssh/authorized_keys\"\n")
		b.WriteString("AFTER=$(wc -l < \"$USER_HOME/.ssh/authorized_keys\")\n")
		b.WriteString("echo \"撤销完成：行数 $BEFORE -> $AFTER\"\n")

	case "list":
		b.WriteString("echo '=== authorized_keys（授权指纹）==='\n")
		b.WriteString("if [ -f \"$USER_HOME/.ssh/authorized_keys\" ]; then\n")
		b.WriteString("  ssh-keygen -lf \"$USER_HOME/.ssh/authorized_keys\" 2>/dev/null || cat \"$USER_HOME/.ssh/authorized_keys\"\n")
		b.WriteString("else echo '(空)'; fi\n")
		b.WriteString("echo '=== 本机公钥 ==='\n")
		b.WriteString("ls \"$USER_HOME/.ssh\"/*.pub 2>/dev/null | while read f; do echo \"--- $f ---\"; cat \"$f\"; done || echo '(无)'\n")
		b.WriteString("echo '=== known_hosts 条数 ==='\n")
		b.WriteString("[ -f \"$USER_HOME/.ssh/known_hosts\" ] && wc -l < \"$USER_HOME/.ssh/known_hosts\" || echo 0\n")

	case "test":
		host := req.Host
		port := req.Port
		b.WriteString("# 免密连通测试（BatchMode 禁止交互密码；accept-new 自动记录已知主机）\n")
		fmt.Fprintf(&b, "if ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 -p %d %s@%s 'echo OPSMESH_SSH_OK' 2>&1; then\n",
			port, req.User, host)
		b.WriteString("  echo '免密连通测试通过'\n")
		b.WriteString("else\n")
		b.WriteString("  echo '免密连通测试失败（检查 authorized_keys / sshd 配置 / 网络可达）'\n")
		b.WriteString("  exit 1\n")
		b.WriteString("fi\n")
	}
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerSSHTrustRoutes 注册 SSH 互信路由。
func (s *Server) registerSSHTrustRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/ssh-trust/keygen", s.handleSSHTrustKeygen)
	mux.HandleFunc("/api/v1/ssh-trust/authorize", s.handleSSHTrustAuthorize)
	mux.HandleFunc("/api/v1/ssh-trust/revoke", s.handleSSHTrustRevoke)
	mux.HandleFunc("/api/v1/ssh-trust/list", s.handleSSHTrustList)
	mux.HandleFunc("/api/v1/ssh-trust/test", s.handleSSHTrustTest)
}

func (s *Server) handleSSHTrustKeygen(w http.ResponseWriter, r *http.Request) {
	s.handleSSHTrust(w, r, "keygen")
}
func (s *Server) handleSSHTrustAuthorize(w http.ResponseWriter, r *http.Request) {
	s.handleSSHTrust(w, r, "authorize")
}
func (s *Server) handleSSHTrustRevoke(w http.ResponseWriter, r *http.Request) {
	s.handleSSHTrust(w, r, "revoke")
}
func (s *Server) handleSSHTrustList(w http.ResponseWriter, r *http.Request) {
	s.handleSSHTrust(w, r, "list")
}
func (s *Server) handleSSHTrustTest(w http.ResponseWriter, r *http.Request) {
	s.handleSSHTrust(w, r, "test")
}
