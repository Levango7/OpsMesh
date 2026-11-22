// db_hadoop_ext_ops.go Hadoop 生态高级运维（Hadoop/Hive/HBase/Solr/Druid/NiFi/Impala/Pinot：HDFS/YARN/Hive/HBase/Solr/Druid/NiFi/Impala/Pinot）。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
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

	script := buildHadoopExtScript(action, req)
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
func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func buildHadoopExtScript(action string, req map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	id := getStr(req, "id")
	if id == "" {
		id = getStr(req, "appID")
	}
	if id == "" {
		id = getStr(req, "core")
	}
	if id == "" {
		id = getStr(req, "collection")
	}
	table := getStr(req, "table")
	if table == "" {
		table = getStr(req, "tableName")
	}
	dbType := getStr(req, "dbType")
	if dbType == "" {
		dbType = "mysql"
	}
	snapshot := getStr(req, "snapshot")
	if snapshot == "" {
		snapshot = getStr(req, "snapshotName")
	}
	core := getStr(req, "core")
	if core == "" {
		core = id
	}
	if core == "" {
		core = "core"
	}
	collection := getStr(req, "collection")
	if collection == "" {
		collection = id
	}
	if collection == "" {
		collection = "collection"
	}
	query := getStr(req, "query")
	if query == "" {
		query = "SHOW DATABASES"
	}
	switch action {
	case "hdfs-status":
		b.WriteString("hdfs dfsadmin -report\n")
	case "hdfs-fsck":
		b.WriteString("hdfs fsck / -files -blocks -locations\n")
	case "hdfs-balancer":
		b.WriteString("hdfs balancer\n")
	case "hdfs-report":
		b.WriteString("hdfs dfsadmin -report\n")
	case "yarn-status":
		b.WriteString("yarn node -list\n")
	case "yarn-apps":
		b.WriteString("yarn application -list\n")
	case "yarn-kill":
		appID := id
		if appID == "" {
			appID = "application_0"
		}
		fmt.Fprintf(&b, "yarn application -kill %s\n", shellquote.Quote(appID))
	case "hive-metastore":
		fmt.Fprintf(&b, "schematool -dbType %s -info\n", shellquote.Quote(dbType))
	case "hive-tables":
		b.WriteString("beeline -u jdbc:hive2://localhost:10000 -e \"SHOW TABLES\"\n")
	case "hbase-status":
		b.WriteString("hbase shell \"status\"\n")
	case "hbase-flush":
		if table != "" {
			fmt.Fprintf(&b, "hbase shell \"flush '%s'\"\n", table)
		} else {
			b.WriteString("hbase shell \"flush\"\n")
		}
	case "hbase-compact":
		if table != "" {
			fmt.Fprintf(&b, "hbase shell \"compact '%s'\"\n", table)
		} else {
			b.WriteString("hbase shell \"compact\"\n")
		}
	case "hbase-snapshot":
		if table != "" && snapshot != "" {
			fmt.Fprintf(&b, "hbase shell \"snapshot '%s','%s'\"\n", table, snapshot)
		} else {
			b.WriteString("hbase shell \"snapshot\"\n")
		}
	case "hbase-restore":
		if snapshot != "" {
			fmt.Fprintf(&b, "hbase shell \"restore_snapshot '%s'\"\n", snapshot)
		} else {
			b.WriteString("hbase shell \"restore_snapshot\"\n")
		}
	case "solr-cores":
		b.WriteString("solr status\n")
	case "solr-collections":
		b.WriteString("curl -s http://localhost:8983/solr/admin/collections?action=LIST\n")
	case "solr-reload":
		fmt.Fprintf(&b, "curl -s \"http://localhost:8983/solr/admin/cores?action=RELOAD&core=%s\"\n", core)
	case "druid-supervisor":
		b.WriteString("curl -s http://localhost:8081/druid/indexer/v1/supervisor\n")
	case "druid-segment":
		b.WriteString("curl -s http://localhost:8082/druid/v2/datasources\n")
	case "druid-task":
		b.WriteString("curl -s http://localhost:8081/druid/indexer/v1/tasks\n")
	case "nifi-flow":
		b.WriteString("curl -s http://localhost:8080/nifi-api/flow/process-groups/root\n")
	case "nifi-process-group":
		b.WriteString("curl -s http://localhost:8080/nifi-api/process-groups/root/process-groups\n")
	case "nifi-processor":
		b.WriteString("curl -s http://localhost:8080/nifi-api/flow/search-results?q=processor\n")
	case "impala-status":
		b.WriteString("impala-shell -q \"SHOW DATABASES\" 2>/dev/null || impalad --version 2>/dev/null || echo 'impala not available'\n")
	case "impala-query":
		fmt.Fprintf(&b, "impala-shell -q %s\n", shellquote.Quote(query))
	case "pinot-status":
		b.WriteString("curl -s http://localhost:9000/health\n")
	case "pinot-table":
		b.WriteString("curl -s http://localhost:9000/tables\n")
	case "pinot-segment":
		b.WriteString("curl -s http://localhost:9000/segments\n")
	default:
		fmt.Fprintf(&b, "echo 'hadoop %s ext %s done'\n", getStr(req, "type"), action)
	}
	fmt.Fprintf(&b, "echo 'hadoop ext %s done'\n", action)
	return b.String()
}
