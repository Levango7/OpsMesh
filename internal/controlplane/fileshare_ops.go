// fileshare_ops.go 文件共享运维（FTP/vsftpd、Samba、NFS）。
//
// 设计原则（延续 P0-P6"查询走 API、执行走 shell"架构）：
//   - NFS 为 Linux 集群共享刚需（K8s PV / 数据库备份目录 / 应用日志汇总）；
//     Samba 负责 Windows 互通；vsftpd 承接存量文件交换。
//   - 共享管理采用"托管配置段"策略：写独立配置文件并 reload，避免解析用户既有配置。
//
// API 端点（POST /api/v1/fileshare/{action}）：
//   - status  {deviceID}                                  — FTP/Samba/NFS 服务状态
//   - deploy  {deviceID, type}                            — 部署服务（vsftpd/samba/nfs）
//   - share   {deviceID, type, op, name, path, options}   — 共享 add/delete/list
//   - mount   {deviceID, server, exportPath, mountPoint}  — NFS 客户端挂载/卸载（op=mount/umount/list）
//   - service {deviceID, type, serviceAction}             — 服务启停
//
// 安全：路径走绝对路径校验（禁 .. / NUL / 换行）；共享名/主机走正则；
// 挂载参数白名单；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 文件共享动作白名单。
var allowedFSShareActions = map[string]bool{
	"status":  true,
	"deploy":  true,
	"share":   true,
	"mount":   true,
	"service": true,
}

// 共享服务类型白名单。
var allowedFSShareTypes = map[string]bool{
	"vsftpd": true,
	"samba":  true,
	"nfs":    true,
}

// 共享操作白名单。
var allowedFSShareOps = map[string]bool{
	"add":    true,
	"delete": true,
	"list":   true,
}

// 挂载操作白名单。
var allowedFSMountOps = map[string]bool{
	"mount":  true,
	"umount": true,
	"list":   true,
}

// 绝对路径正则（禁 ..、NUL 之外的常见可打印字符；不含空格与通配符）。
var fsPathRe = regexp.MustCompile(`^/[a-zA-Z0-9_./\-]{0,255}$`)

// 共享名正则（Samba share 名 / NFS 无）。
var fsShareNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,64}$`)

// NFS 客户端选项白名单（挂载参数）。
var allowedNFSMountOpts = map[string]bool{
	"rw": true, "ro": true, "hard": true, "soft": true, "intr": true,
	"rsize=65536": true, "wsize=65536": true, "nfsvers=3": true, "nfsvers=4": true,
	"noatime": true, "timeo=600": true, "retrans=2": true,
}

// fsShareReq 文件共享请求体。
type fsShareReq struct {
	DeviceID    string `json:"deviceID"`
	Action      string `json:"action"`      // status/deploy/share/mount/service
	Type        string `json:"type"`        // vsftpd/samba/nfs
	Op          string `json:"op"`          // add/delete/list（share）；mount/umount/list（mount）
	Name        string `json:"name"`        // 共享名（Samba）
	Path        string `json:"path"`        // 共享目录（绝对路径）
	Options     string `json:"options"`     // 共享选项（NFS: rw,sync,no_root_squash / Samba: browseable 等）
	Server      string `json:"server"`      // NFS 服务端（mount 用）
	ExportPath  string `json:"exportPath"`  // NFS 导出路径（mount 用）
	MountPoint  string `json:"mountPoint"`  // 挂载点（mount 用）
	MountOpts   string `json:"mountOpts"`   // 挂载参数（逗号分隔白名单）
	Service     string `json:"service"`     // 服务动作（service 用）
	AnonymousOK bool   `json:"anonymousOK"` // FTP 匿名访问（deploy 用，默认 false）
}

// handleFSShare 处理 POST /api/v1/fileshare/{action}。
func (s *Server) handleFSShare(w http.ResponseWriter, r *http.Request, action string) {
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
	if !allowedFSShareActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (status/deploy/share/mount/service)"})
		return
	}
	var req fsShareReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Action = action
	if req.Type == "" {
		req.Type = "nfs"
	}
	if !allowedFSShareTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported type (vsftpd/samba/nfs)"})
		return
	}

	switch action {
	case "share":
		if !allowedFSShareOps[req.Op] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported op (add/delete/list)"})
			return
		}
		if req.Op != "list" {
			if !validFSPath(req.Path) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path (must be absolute, no .. or spaces)"})
				return
			}
			if req.Type == "samba" && !fsShareNameRe.MatchString(req.Name) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid share name"})
				return
			}
			if req.Type == "nfs" {
				req.Name = req.Path // NFS 以导出路径为标识。
			}
		}
		if req.Options != "" && !validFSOptions(req.Type, req.Options) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid share options"})
			return
		}
	case "mount":
		if !allowedFSMountOps[req.Op] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported op (mount/umount/list)"})
			return
		}
		if req.Op != "list" {
			if !monitorHostRe.MatchString(req.Server) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server"})
				return
			}
			if !validFSPath(req.ExportPath) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid exportPath"})
				return
			}
			if !validFSPath(req.MountPoint) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mountPoint"})
				return
			}
			if !validMountOpts(req.MountOpts) {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mountOpts (whitelist enforced)"})
				return
			}
		}
	case "service":
		if req.Service == "" {
			req.Service = "status"
		}
		if !allowedNTPServiceActions[req.Service] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid service action (start/stop/restart/status)"})
			return
		}
	}

	script := buildFSShareScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "fileshare_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "fileshare."+action, err)
		return
	}
	target := req.Type
	if action == "share" {
		target = req.Name
	}
	if action == "mount" {
		target = req.Server + ":" + req.ExportPath
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "fileshare_" + action, Target: target,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// validFSPath 校验绝对路径：以 / 开头、无 ..、无空格/NUL/换行/引号。
func validFSPath(p string) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	if strings.ContainsAny(p, " \t\x00\r\n'\"`$") {
		return false
	}
	return fsPathRe.MatchString(p)
}

// validFSOptions 按类型校验共享选项。
func validFSOptions(shareType, opts string) bool {
	if strings.ContainsAny(opts, "\x00\r\n'\"`") {
		return false
	}
	parts := strings.Split(opts, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch shareType {
		case "nfs":
			// 导出选项白名单（rw/ro/sync/async/no_root_squash/root_squash/all_squash/anongid/anongid=*/anonuid=*/no_subtree_check/subtree_check/fsid=*）
			if !isAllowedNFSOption(p) {
				return false
			}
		case "samba":
			// Samba 共享参数（key=value 或 flag），限制字符集。
			if !regexp.MustCompile(`^[a-zA-Z0-9 _=.\-/]{1,64}$`).MatchString(p) {
				return false
			}
		}
	}
	return true
}

func isAllowedNFSOption(opt string) bool {
	base := opt
	if i := strings.Index(opt, "="); i > 0 {
		base = opt[:i]
	}
	switch base {
	case "rw", "ro", "sync", "async", "no_root_squash", "root_squash", "all_squash",
		"no_subtree_check", "subtree_check", "insecure", "secure", "crossmnt",
		"anonuid", "anongid", "fsid":
		return true
	}
	return false
}

// validMountOpts 校验挂载参数（白名单 + 校验集字符）。
func validMountOpts(opts string) bool {
	if opts == "" {
		return true
	}
	if strings.ContainsAny(opts, "\x00\r\n'\"`;|&$") {
		return false
	}
	for _, p := range strings.Split(opts, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !allowedNFSMountOpts[p] {
			return false
		}
	}
	return true
}

func buildFSShareScript(req fsShareReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "status":
		b.WriteString("# 文件共享服务状态\n")
		b.WriteString("echo '=== vsftpd ==='\nsystemctl is-active vsftpd 2>/dev/null || echo 'not running'\n")
		b.WriteString("echo '=== smbd ==='\nsystemctl is-active smbd 2>/dev/null || systemctl is-active smb 2>/dev/null || echo 'not running'\n")
		b.WriteString("echo '=== nfs-server ==='\nsystemctl is-active nfs-server 2>/dev/null || systemctl is-active nfs-kernel-server 2>/dev/null || echo 'not running'\n")
		b.WriteString("echo '=== 托管导出（NFS）==='\n")
		b.WriteString("exportfs -v 2>/dev/null | head -30 || cat /etc/exports 2>/dev/null || echo '(无)'\n")
		b.WriteString("echo '=== 托管共享（Samba）==='\n")
		b.WriteString("cat /etc/samba/opsmesh-shares.conf 2>/dev/null || echo '(无)'\n")

	case "deploy":
		b.WriteString("# 部署文件共享服务\n")
		switch req.Type {
		case "vsftpd":
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum install -y vsftpd >/dev/null 2>&1 || true; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y vsftpd >/dev/null 2>&1 || true; fi\n")
			b.WriteString("mkdir -p /etc/vsftpd.d\n")
			b.WriteString("cat > /etc/vsftpd.d/opsmesh.conf <<'OPSMESH_FS_EOF'\n")
			b.WriteString("# OpsMesh 托管：vsftpd 配置\n")
			b.WriteString("local_enable=YES\nwrite_enable=YES\nchroot_local_user=YES\n")
			if req.AnonymousOK {
				b.WriteString("anonymous_enable=YES\nanon_upload_enable=YES\n")
			} else {
				b.WriteString("anonymous_enable=NO\n")
			}
			b.WriteString("OPSMESH_FS_EOF\n")
			b.WriteString("grep -qs 'vsftpd.d' /etc/vsftpd.conf 2>/dev/null || echo 'include_dir=/etc/vsftpd.d' >> /etc/vsftpd.conf 2>/dev/null || true\n")
			b.WriteString("systemctl enable --now vsftpd 2>/dev/null || systemctl enable --now vsftpd.service || true\n")
			b.WriteString("echo 'vsftpd 已部署'\n")
		case "samba":
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum install -y samba >/dev/null 2>&1 || true; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y samba >/dev/null 2>&1 || true; fi\n")
			b.WriteString("mkdir -p /etc/samba\n")
			b.WriteString("touch /etc/samba/opsmesh-shares.conf\n")
			b.WriteString("MAIN=/etc/samba/smb.conf\n")
			b.WriteString("if ! grep -qs 'opsmesh-shares.conf' \"$MAIN\" 2>/dev/null; then echo 'include = /etc/samba/opsmesh-shares.conf' >> \"$MAIN\"; fi\n")
			b.WriteString("testparm -s >/dev/null 2>&1 && systemctl enable --now smbd 2>/dev/null || systemctl enable --now smb 2>/dev/null || true\n")
			b.WriteString("systemctl restart smbd 2>/dev/null || systemctl restart smb 2>/dev/null || true\n")
			b.WriteString("echo 'samba 已部署'\n")
		case "nfs":
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum install -y nfs-utils >/dev/null 2>&1 || true; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y nfs-kernel-server >/dev/null 2>&1 || true; fi\n")
			b.WriteString("mkdir -p /etc/exports.d\n")
			b.WriteString("touch /etc/exports.d/opsmesh.exports\n")
			b.WriteString("systemctl enable --now nfs-server 2>/dev/null || systemctl enable --now nfs-kernel-server 2>/dev/null || true\n")
			b.WriteString("exportfs -ra 2>/dev/null || true\n")
			b.WriteString("echo 'NFS 服务端已部署'\n")
		}

	case "share":
		switch req.Type {
		case "nfs":
			b.WriteString("# NFS 导出管理（/etc/exports.d/opsmesh.exports，托管文件）\n")
			b.WriteString("EXPORTS=/etc/exports.d/opsmesh.exports\nmkdir -p /etc/exports.d && touch \"$EXPORTS\"\n")
			switch req.Op {
			case "list":
				b.WriteString("cat \"$EXPORTS\"; echo '---'; exportfs -v 2>/dev/null | head -30 || true\n")
			case "add":
				opts := req.Options
				if opts == "" {
					opts = "rw,sync,no_root_squash,no_subtree_check"
				}
				b.WriteString(fmt.Sprintf("mkdir -p %q\n", req.Path))
				b.WriteString(fmt.Sprintf("sed -i -E '\\#^%s#d' \"$EXPORTS\" 2>/dev/null || true\n", req.Path))
				b.WriteString(fmt.Sprintf("echo '%s %s(%s)' >> \"$EXPORTS\"\n", req.Path, "*", opts))
				b.WriteString("exportfs -ra && echo '导出已添加并生效'\n")
			case "delete":
				b.WriteString(fmt.Sprintf("sed -i -E '\\#^%s#d' \"$EXPORTS\"\n", req.Path))
				b.WriteString("exportfs -ra && echo '导出已删除并生效'\n")
			}
		case "samba":
			b.WriteString("# Samba 共享管理（/etc/samba/opsmesh-shares.conf，托管文件）\n")
			b.WriteString("CONF=/etc/samba/opsmesh-shares.conf\nmkdir -p /etc/samba && touch \"$CONF\"\n")
			switch req.Op {
			case "list":
				b.WriteString("cat \"$CONF\" 2>/dev/null || echo '(无托管共享)'\n")
			case "add":
				b.WriteString("BACKUP=\"$CONF.bak.$(date +%s)\"\ncp \"$CONF\" \"$BACKUP\"\n")
				// 幂等：删除同名 section（[name] 到下一个 [ 之间）。
				b.WriteString(fmt.Sprintf("awk -v sec='[%s]' '$0==sec{skip=1;next} /^\\[/{skip=0} !skip' \"$CONF\" > \"$CONF.tmp\" && mv \"$CONF.tmp\" \"$CONF\"\n", req.Name))
				b.WriteString(fmt.Sprintf("mkdir -p %q\n", req.Path))
				extra := req.Options
				if extra == "" {
					extra = "browseable = yes\n  writable = yes\n  guest ok = no"
				}
				b.WriteString(fmt.Sprintf("{\n  echo '[%s]'\n  echo '  path = %s'\n  echo '  %s'\n} >> \"$CONF\"\n", req.Name, req.Path, strings.ReplaceAll(extra, "\n", "\n  ")))
				b.WriteString("if testparm -s >/dev/null 2>&1; then systemctl reload smbd 2>/dev/null || systemctl reload smb 2>/dev/null || true; echo '共享已添加并 reload'; else cp \"$BACKUP\" \"$CONF\"; echo 'smb.conf 校验失败，已回滚' && exit 1; fi\n")
			case "delete":
				b.WriteString("BACKUP=\"$CONF.bak.$(date +%s)\"\ncp \"$CONF\" \"$BACKUP\"\n")
				b.WriteString(fmt.Sprintf("awk -v sec='[%s]' '$0==sec{skip=1;next} /^\\[/{skip=0} !skip' \"$CONF\" > \"$CONF.tmp\" && mv \"$CONF.tmp\" \"$CONF\"\n", req.Name))
				b.WriteString("if testparm -s >/dev/null 2>&1; then systemctl reload smbd 2>/dev/null || true; echo '共享已删除并 reload'; else cp \"$BACKUP\" \"$CONF\"; echo '校验失败，已回滚' && exit 1; fi\n")
			}
		case "vsftpd":
			b.WriteString("# vsftpd 用户目录共享（创建目录并授权）\n")
			switch req.Op {
			case "list":
				b.WriteString("grep -E '^(local_root|user_config_dir)' /etc/vsftpd.conf /etc/vsftpd.d/*.conf 2>/dev/null || echo '(无托管共享)'\n")
			case "add":
				b.WriteString(fmt.Sprintf("mkdir -p %q && chmod 755 %q\n", req.Path, req.Path))
				b.WriteString(fmt.Sprintf("echo '共享目录已创建: %s（用户目录由系统用户管理）'\n", req.Path))
			case "delete":
				b.WriteString(fmt.Sprintf("echo 'vsftpd 共享删除：目录 %s 保留（防误删数据），如需删除请手动处理'\n", req.Path))
			}
		}

	case "mount":
		b.WriteString("# NFS 客户端挂载管理\n")
		switch req.Op {
		case "list":
			b.WriteString("mount -t nfs,nfs4 2>/dev/null || mount | grep -E 'type nfs' || echo '(无 NFS 挂载)'\n")
		case "mount":
			opts := req.MountOpts
			if opts == "" {
				opts = "rw,hard,intr,noatime,nfsvers=4"
			}
			b.WriteString(fmt.Sprintf("mkdir -p %q\n", req.MountPoint))
			b.WriteString(fmt.Sprintf("mount -t nfs -o %s %s:%s %s && echo '已挂载: %s'\n", opts, req.Server, req.ExportPath, req.MountPoint, req.MountPoint))
			b.WriteString("# 幂等写入 fstab（重启保持；先删同名行）\n")
			b.WriteString(fmt.Sprintf("sed -i -E '\\#%s#d' /etc/fstab 2>/dev/null || true\n", req.MountPoint))
			b.WriteString(fmt.Sprintf("echo '%s:%s %s nfs %s 0 0' >> /etc/fstab\n", req.Server, req.ExportPath, req.MountPoint, opts))
			b.WriteString("echo 'fstab 已更新（重启保持）'\n")
		case "umount":
			b.WriteString(fmt.Sprintf("umount %q 2>/dev/null || umount -l %q 2>/dev/null || echo '卸载失败（可能未挂载或占用）'\n", req.MountPoint, req.MountPoint))
			b.WriteString(fmt.Sprintf("sed -i -E '\\#%s#d' /etc/fstab 2>/dev/null || true\n", req.MountPoint))
			b.WriteString("echo '已卸载并从 fstab 移除'\n")
		}

	case "service":
		b.WriteString("# 文件共享服务管理\n")
		var svcNames string
		switch req.Type {
		case "vsftpd":
			svcNames = "vsftpd"
		case "samba":
			svcNames = "smbd smb"
		case "nfs":
			svcNames = "nfs-server nfs-kernel-server"
		}
		switch req.Service {
		case "start":
			b.WriteString(fmt.Sprintf("for s in %s; do systemctl start $s 2>/dev/null && { echo \"$s started\"; exit 0; }; done; echo 'service not found' && exit 1\n", svcNames))
		case "stop":
			b.WriteString(fmt.Sprintf("for s in %s; do systemctl stop $s 2>/dev/null && { echo \"$s stopped\"; exit 0; }; done; true\n", svcNames))
		case "restart":
			b.WriteString(fmt.Sprintf("for s in %s; do systemctl restart $s 2>/dev/null && { echo \"$s restarted\"; exit 0; }; done; true\n", svcNames))
		case "status":
			b.WriteString(fmt.Sprintf("for s in %s; do systemctl is-active $s 2>/dev/null && echo \"$s: active\"; done; true\n", svcNames))
		}
	}
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerFSShareRoutes 注册文件共享运维路由。
func (s *Server) registerFSShareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/fileshare/status", s.handleFSShareStatus)
	mux.HandleFunc("/api/v1/fileshare/deploy", s.handleFSShareDeploy)
	mux.HandleFunc("/api/v1/fileshare/share", s.handleFSShareShare)
	mux.HandleFunc("/api/v1/fileshare/mount", s.handleFSShareMount)
	mux.HandleFunc("/api/v1/fileshare/service", s.handleFSShareService)
}

func (s *Server) handleFSShareStatus(w http.ResponseWriter, r *http.Request) {
	s.handleFSShare(w, r, "status")
}
func (s *Server) handleFSShareDeploy(w http.ResponseWriter, r *http.Request) {
	s.handleFSShare(w, r, "deploy")
}
func (s *Server) handleFSShareShare(w http.ResponseWriter, r *http.Request) {
	s.handleFSShare(w, r, "share")
}
func (s *Server) handleFSShareMount(w http.ResponseWriter, r *http.Request) {
	s.handleFSShare(w, r, "mount")
}
func (s *Server) handleFSShareService(w http.ResponseWriter, r *http.Request) {
	s.handleFSShare(w, r, "service")
}
