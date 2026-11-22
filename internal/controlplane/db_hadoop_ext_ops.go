// db_hadoop_ext_ops.go Hadoop 生态高级运维（Hadoop/Hive/HBase/Solr/Druid/NiFi/Impala/Pinot：HDFS/YARN/Hive/HBase/Solr/Druid/NiFi/Impala/Pinot）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var hadoopExtTypes = map[string]bool{
	"hadoop": true, "hive": true, "hbase": true, "solr": true,
	"druid": true, "nifi": true, "impala": true, "pinot": true,
}

var hadoopExtActions = map[string]bool{
	"hdfs-status": true, "hdfs-fsck": true, "hdfs-balancer": true, "hdfs-report": true,
	"yarn-status": true, "yarn-apps": true, "yarn-kill": true,
	"hive-metastore": true, "hive-tables": true,
	"hbase-status": true, "hbase-flush": true, "hbase-compact": true, "hbase-snapshot": true, "hbase-restore": true,
	"solr-cores": true, "solr-collections": true, "solr-reload": true,
	"druid-supervisor": true, "druid-segment": true, "druid-task": true,
	"nifi-flow": true, "nifi-process-group": true, "nifi-processor": true,
	"impala-status": true, "impala-query": true,
	"pinot-status": true, "pinot-table": true, "pinot-segment": true,
}

func (s *Server) handleHadoopExt(w http.ResponseWriter, r *http.Request) {
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
	hType, _ := req["type"].(string)
	if hType == "" {
		hType = "hadoop"
	}
	if !hadoopExtTypes[hType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + hType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !extSafeRe.MatchString(action) || !hadoopExtActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !extSafeRe.MatchString(str) {
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

	script := fmt.Sprintf("set -e\necho 'hadoop %s ext %s done'\n", hType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, hType+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.hadoop.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_hadoop_ext_" + action,
		Target: hType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + hType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
