// zookeeper_middleware_ops.go Zookeeper 中间件运维管理。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// Zookeeper 四字命令监控 + 节点状态
// ============================================================================

// zookeeperCmdReq Zookeeper 四字命令请求体。
type zookeeperCmdReq struct {
	DeviceID string `json:"deviceID"`
	Host     string `json:"host"` // Zookeeper 地址（host:port，默认 localhost:2181）
	Cmd      string `json:"cmd"`  // 四字命令：stat/ruok/mntr/cons/envi/conf/crst/dump/wchs/wchc/mon
}

// handleZookeeperCmd 处理 POST /api/v1/middleware/zookeeper/cmd。
func (s *Server) handleZookeeperCmd(w http.ResponseWriter, r *http.Request) {
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
	var req zookeeperCmdReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost:2181"
	}
	allowed := map[string]bool{"stat": true, "ruok": true, "mntr": true, "cons": true, "envi": true, "conf": true, "crst": true, "dump": true, "wchs": true, "wchc": true, "mon": true}
	if !allowed[req.Cmd] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported 4lw command (stat/ruok/mntr/cons/envi/conf/crst/dump/wchs/wchc/mon)"})
		return
	}
	script := buildZookeeperCmdScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "zookeeper_cmd_"+req.Cmd)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.zookeeper.cmd", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "zookeeper_cmd_" + req.Cmd, Target: req.Host,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildZookeeperCmdScript(req zookeeperCmdReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "HOST=%q\n", req.Host)
	fmt.Fprintf(&b, "echo %q | nc -w 2 $HOST\n", req.Cmd)
	fmt.Fprintf(&b, "echo 'Zookeeper %s 命令执行完成 (host=%s)\n", req.Cmd, req.Host)
	return b.String()
}
