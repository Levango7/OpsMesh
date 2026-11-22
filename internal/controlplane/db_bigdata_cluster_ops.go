// db_bigdata_cluster_ops.go 大数据集群管理（Ambari + Cloudera/CDP + 通用 Hadoop HA + Kerberos）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var bigDataClusterNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var bigDataClusterTypes = map[string]bool{
	"ambari": true, "cloudera": true, "hadoop": true, "hdp": true, "cdh": true, "cdp": true,
}

var bigDataClusterActions = map[string]bool{
	"ambari-deploy": true, "ambari-status": true, "ambari-services": true,
	"ambari-service-start": true, "ambari-service-stop": true, "ambari-service-restart": true,
	"ambari-config-get": true, "ambari-config-set": true, "ambari-hosts": true,
	"ambari-host-add": true, "ambari-host-remove": true, "ambari-cluster-create": true,
	"ambari-blueprint": true, "ambari-rolling-restart": true, "ambari-kerberos-enable": true,
	"ambari-kerberos-disable": true, "ambari-alerts": true, "ambari-metrics": true,
	"cm-deploy": true, "cm-status": true, "cm-clusters": true, "cm-services": true,
	"cm-service-start": true, "cm-service-stop": true, "cm-service-restart": true,
	"cm-config": true, "cm-hosts": true, "cm-kerberos": true, "cm-rolling-restart": true,
	"cm-upgrade": true, "cm-parcel-list": true, "cm-parcel-install": true,
	"cm-parcel-activate": true, "cm-alerts": true,
	"ha-namenode-status": true, "ha-namenode-failover": true, "ha-namenode-checkpoint": true,
	"ha-zkfc-status": true, "ha-journalnode-status": true, "ha-yarn-rm-status": true,
	"ha-yarn-rm-failover": true, "ha-hdfs-fsck": true, "ha-hdfs-balancer": true,
	"ha-hdfs-quota-set": true, "ha-hdfs-quota-get": true, "ha-yarn-queue-list": true,
	"ha-yarn-queue-create": true, "ha-yarn-queue-delete": true, "ha-yarn-queue-update": true,
	"ha-yarn-app-kill": true, "ha-yarn-app-list": true,
	"kerberos-status": true, "kerberos-principal-list": true, "kerberos-principal-create": true,
	"kerberos-principal-delete": true, "kerberos-keytab-generate": true, "kerberos-kinit-test": true,
}

func (s *Server) handleBigDataCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID, _ := req["deviceID"].(string)
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	bType, _ := req["type"].(string)
	if bType == "" {
		bType = "hadoop"
	}
	if !bigDataClusterTypes[bType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + bType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !bigDataClusterNameRe.MatchString(action) || !bigDataClusterActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !bigDataClusterNameRe.MatchString(str) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + k})
			return
		}
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(deviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(deviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := fmt.Sprintf("set -e\necho 'bigdata cluster %s %s done'\n", bType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "bigdata_cluster_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.bigdata.cluster", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_bigdata_cluster_" + action,
		Target: bType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + bType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
