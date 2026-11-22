// db_hdfs_advanced_ops.go HDFS/Hadoop HA/YARN 高级运维（配额/快照/安全模式/均衡器/fsck/块报告/下线/NameNode HA/YARN 队列与应用/RM HA）。
package controlplane

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// hdfsAdvancedActions HDFS 高级运维 action 白名单。
var hdfsAdvancedActions = map[string]bool{
	"quota-set": true, "quota-get": true, "quota-clr": true,
	"snapshot-create": true, "snapshot-delete": true, "snapshot-list": true,
	"safemode-enter": true, "safemode-leave": true, "safemode-status": true,
	"balancer": true, "fsck": true, "block-report": true, "decommission": true,
	"namenode-status": true, "namenode-failover": true, "namenode-transition-active": true, "namenode-transition-standby": true, "namenode-checkhealth": true,
	"zkfc-status": true, "journalnode-status": true,
	"yarn-queue-list": true, "yarn-queue-create": true, "yarn-queue-delete": true, "yarn-queue-update": true,
	"yarn-app-list": true, "yarn-app-kill": true,
	"yarn-rm-status": true, "yarn-rm-failover": true,
}

func (s *Server) handleHDFSAdvanced(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID     string      `json:"deviceID"`
		Action       string      `json:"action"`
		Path         string      `json:"path"`
		Quota        interface{} `json:"quota"`
		SpaceQuota   interface{} `json:"spaceQuota"`
		Snapshot     string      `json:"snapshot"`
		SnapshotName string      `json:"snapshotName"`
		Active       string      `json:"active"`
		Standby      string      `json:"standby"`
		Node         string      `json:"node"`
		Host         string      `json:"host"`
		Queue        string      `json:"queue"`
		QueueName    string      `json:"queueName"`
		AppID        string      `json:"appID"`
		ID           string      `json:"id"`
		Exclude      string      `json:"exclude"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !hdfsAdvancedActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	if req.Path != "" {
		if err := shellquote.ValidatePath(req.Path); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
			return
		}
	}
	path := req.Path
	if path == "" {
		path = "/"
	}

	snapshot := req.Snapshot
	if snapshot == "" {
		snapshot = req.SnapshotName
	}
	if snapshot != "" {
		if err := shellquote.ValidateName(snapshot, 128); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid snapshot"})
			return
		}
	}
	if snapshot == "" {
		snapshot = "snapshot"
	}

	queue := req.Queue
	if queue == "" {
		queue = req.QueueName
	}
	if queue != "" {
		if err := shellquote.ValidateIdent(queue); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid queue"})
			return
		}
	}
	if queue == "" {
		queue = "default"
	}

	appID := req.AppID
	if appID == "" {
		appID = req.ID
	}
	if appID != "" {
		if err := shellquote.ValidateIdent(appID); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid appID"})
			return
		}
	}

	for _, h := range []string{req.Active, req.Standby, req.Node, req.Host} {
		if h != "" {
			if err := shellquote.ValidateHost(h); err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host"})
				return
			}
		}
	}
	active := req.Active
	if active == "" {
		active = "namenode"
	}
	standby := req.Standby
	if standby == "" {
		standby = "namenode2"
	}
	node := req.Node
	if node == "" {
		node = req.Host
	}
	if node == "" {
		node = "localhost"
	}

	quotaStr := toStringIface(req.Quota)
	if quotaStr != "" {
		if _, err := strconv.Atoi(quotaStr); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid quota"})
			return
		}
	}
	spaceQuotaStr := toStringIface(req.SpaceQuota)
	if spaceQuotaStr != "" {
		if !isSafeSize(spaceQuotaStr) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid spaceQuota"})
			return
		}
	}

	if req.Exclude != "" {
		if err := shellquote.ValidatePath(req.Exclude); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid exclude"})
			return
		}
	}
	exclude := req.Exclude
	if exclude == "" {
		exclude = "/tmp/exclude.txt"
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(req.DeviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(req.DeviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := buildHDFSAdvancedScript(req.Action, path, quotaStr, spaceQuotaStr, snapshot, active, standby, node, queue, appID, exclude)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "hdfs_advanced_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.hdfs.advanced", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_hdfs_advanced_" + req.Action,
		Target: "hdfs", Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " action=" + req.Action + " path=" + path),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func toStringIface(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.Itoa(int(t))
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}

func isSafeSize(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c == 'k' || c == 'K' || c == 'm' || c == 'M' || c == 'g' || c == 'G' || c == 't' || c == 'T':
		case c == 'b' || c == 'B':
		default:
			return false
		}
	}
	return true
}

func buildHDFSAdvancedScript(action, path, quota, spaceQuota, snapshot, active, standby, node, queue, appID, exclude string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	qp := shellquote.Quote(path)
	switch action {
	case "quota-set":
		if quota != "" {
			fmt.Fprintf(&b, "hdfs dfsadmin -setQuota %s %s\n", shellquote.Quote(quota), qp)
		}
		if spaceQuota != "" {
			fmt.Fprintf(&b, "hdfs dfsadmin -setSpaceQuota %s %s\n", shellquote.Quote(spaceQuota), qp)
		}
		if quota == "" && spaceQuota == "" {
			fmt.Fprintf(&b, "hdfs dfsadmin -setQuota 1000 %s\n", qp)
		}
	case "quota-get":
		fmt.Fprintf(&b, "hdfs dfsadmin -count -q %s\n", qp)
	case "quota-clr":
		fmt.Fprintf(&b, "hdfs dfsadmin -clrQuota %s\n", qp)
		fmt.Fprintf(&b, "hdfs dfsadmin -clrSpaceQuota %s\n", qp)
	case "snapshot-create":
		fmt.Fprintf(&b, "hdfs dfsadmin -allowSnapshot %s\n", qp)
		fmt.Fprintf(&b, "hdfs dfs -createSnapshot %s %s\n", qp, shellquote.Quote(snapshot))
	case "snapshot-delete":
		fmt.Fprintf(&b, "hdfs dfs -deleteSnapshot %s %s\n", qp, shellquote.Quote(snapshot))
	case "snapshot-list":
		fmt.Fprintf(&b, "hdfs dfs -lsSnapshot %s\n", qp)
	case "safemode-enter":
		b.WriteString("hdfs dfsadmin -safemode enter\n")
	case "safemode-leave":
		b.WriteString("hdfs dfsadmin -safemode leave\n")
	case "safemode-status":
		b.WriteString("hdfs dfsadmin -safemode get\n")
	case "balancer":
		b.WriteString("hdfs balancer\n")
	case "fsck":
		fmt.Fprintf(&b, "hdfs fsck %s -files -blocks -locations\n", qp)
	case "block-report":
		b.WriteString("hdfs dfsadmin -report\n")
	case "decommission":
		fmt.Fprintf(&b, "echo %s > /tmp/hdfs-exclude.txt 2>/dev/null; hdfs dfsadmin -refreshNodes\n", shellquote.Quote(exclude))
	case "namenode-status":
		fmt.Fprintf(&b, "hdfs haadmin -getServiceState %s\n", shellquote.Quote(active))
	case "namenode-failover":
		fmt.Fprintf(&b, "hdfs haadmin -failover %s %s\n", shellquote.Quote(active), shellquote.Quote(standby))
	case "namenode-transition-active":
		fmt.Fprintf(&b, "hdfs haadmin -transitionToActive %s\n", shellquote.Quote(active))
	case "namenode-transition-standby":
		fmt.Fprintf(&b, "hdfs haadmin -transitionToStandby %s\n", shellquote.Quote(standby))
	case "namenode-checkhealth":
		fmt.Fprintf(&b, "hdfs haadmin -checkHealth %s\n", shellquote.Quote(active))
	case "zkfc-status":
		b.WriteString("hdfs zkfc -formatZK 2>/dev/null; jps 2>/dev/null | grep -i zkfc || echo 'zkfc not running'\n")
	case "journalnode-status":
		b.WriteString("jps 2>/dev/null | grep -i journalnode || echo 'journalnode not running'\n")
	case "yarn-queue-list":
		fmt.Fprintf(&b, "yarn queue -status %s\n", shellquote.Quote(queue))
	case "yarn-queue-create":
		fmt.Fprintf(&b, "yarn rmadmin -addToClusterNodeLabels %s\n", shellquote.Quote(queue))
	case "yarn-queue-delete":
		fmt.Fprintf(&b, "yarn rmadmin -removeFromClusterNodeLabels %s\n", shellquote.Quote(queue))
	case "yarn-queue-update":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "yarn-app-list":
		b.WriteString("yarn application -list\n")
	case "yarn-app-kill":
		fmt.Fprintf(&b, "yarn application -kill %s\n", shellquote.Quote(appID))
	case "yarn-rm-status":
		fmt.Fprintf(&b, "yarn rmadmin -getServiceState %s\n", shellquote.Quote(active))
	case "yarn-rm-failover":
		fmt.Fprintf(&b, "yarn rmadmin -transitionToActive %s\n", shellquote.Quote(active))
		fmt.Fprintf(&b, "yarn rmadmin -transitionToStandby %s\n", shellquote.Quote(standby))
	}
	fmt.Fprintf(&b, "echo 'hdfs advanced %s done'\n", action)
	return b.String()
}
