// db_spark_flink_ops.go Spark / Flink 作业管理。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

var sparkFlinkNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var sparkFlinkTypes = map[string]bool{
	"spark": true, "flink": true,
}

var sparkFlinkActions = map[string]bool{
	// Spark
	"status": true, "submit": true, "kill": true, "logs": true, "app-list": true,
	"app-status": true, "history-server": true, "spark-shell": true, "sql": true,
	"thrift-server": true, "streaming": true, "structured-streaming": true,
	"checkpoint-status": true, "executor-list": true, "executor-kill": true,
	"config": true, "monitoring": true,
	// Flink
	"job-list": true, "job-status": true, "savepoint": true, "restore": true,
	"checkpoint-list": true, "taskmanager-list": true, "taskmanager-kill": true,
	"web-ui": true, "sql-client": true,
}

func (s *Server) handleSparkFlink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID := getStr(req, "deviceID")
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	sfType := getStr(req, "type")
	if sfType == "" {
		sfType = "spark"
	}
	if !sparkFlinkTypes[sfType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + sfType})
		return
	}
	action := getStr(req, "action")
	if action == "" || !sparkFlinkNameRe.MatchString(action) || !sparkFlinkActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !sparkFlinkNameRe.MatchString(str) {
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

	script := buildSparkFlinkScript(sfType, action, req)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "sparkflink_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.sparkflink", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_sparkflink_" + action,
		Target: sfType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + sfType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func buildSparkFlinkScript(sfType, action string, req map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	id := getStr(req, "id")
	if id == "" {
		id = getStr(req, "appID")
	}
	if id == "" {
		id = getStr(req, "jobID")
	}
	if id == "" {
		id = "application_0"
	}
	jar := getStr(req, "jar")
	if jar == "" {
		jar = "/opt/spark/app.jar"
	}
	query := getStr(req, "query")
	if query == "" {
		query = "SHOW DATABASES"
	}
	switch action {
	// Spark
	case "status":
		b.WriteString("spark-submit --version\n")
	case "submit":
		fmt.Fprintf(&b, "spark-submit --master yarn --deploy-mode cluster %s\n", shellquote.Quote(jar))
	case "kill":
		fmt.Fprintf(&b, "spark-submit --kill %s\n", shellquote.Quote(id))
	case "logs":
		fmt.Fprintf(&b, "yarn logs -applicationId %s\n", shellquote.Quote(id))
	case "app-list":
		b.WriteString("yarn application -list | grep -i spark\n")
	case "app-status":
		fmt.Fprintf(&b, "yarn application -status %s\n", shellquote.Quote(id))
	case "history-server":
		b.WriteString("curl -s http://localhost:18080\n")
	case "spark-shell":
		b.WriteString("spark-shell --version\n")
	case "sql":
		fmt.Fprintf(&b, "spark-sql -e %s\n", shellquote.Quote(query))
	case "thrift-server":
		b.WriteString("/opt/spark/sbin/start-thriftserver.sh\n")
	case "streaming":
		fmt.Fprintf(&b, "spark-submit --master yarn --deploy-mode cluster %s\n", shellquote.Quote(jar))
	case "structured-streaming":
		fmt.Fprintf(&b, "spark-submit --master yarn --deploy-mode cluster %s\n", shellquote.Quote(jar))
	case "checkpoint-status":
		b.WriteString("hdfs dfs -ls /spark-checkpoint 2>/dev/null || echo 'no checkpoint dir'\n")
	case "executor-list":
		b.WriteString("curl -s http://localhost:4040/api/v1/applications\n")
	case "executor-kill":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:4040/api/v1/applications/%s/executors/%s\n", shellquote.Quote(id), "0")
	case "config":
		b.WriteString("cat /opt/spark/conf/spark-defaults.conf\n")
	case "monitoring":
		b.WriteString("curl -s http://localhost:4040/metrics/json\n")
	// Flink
	case "job-list":
		b.WriteString("flink list\n")
	case "job-status":
		b.WriteString("flink list -r\n")
	case "savepoint":
		fmt.Fprintf(&b, "flink savepoint %s hdfs:///flink/savepoints\n", shellquote.Quote(id))
	case "restore":
		fmt.Fprintf(&b, "flink run -s hdfs:///flink/savepoints/%s %s\n", shellquote.Quote(id), shellquote.Quote(jar))
	case "checkpoint-list":
		fmt.Fprintf(&b, "curl -s http://localhost:8081/jobs/%s/checkpoints\n", shellquote.Quote(id))
	case "taskmanager-list":
		b.WriteString("curl -s http://localhost:8081/taskmanagers\n")
	case "taskmanager-kill":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:8081/taskmanagers/%s\n", shellquote.Quote(id))
	case "web-ui":
		b.WriteString("curl -s http://localhost:8081/overview\n")
	case "sql-client":
		b.WriteString("sql-client.sh embedded -e \"SHOW CATALOGS\"\n")
	default:
		fmt.Fprintf(&b, "echo 'sparkflink %s %s done'\n", sfType, action)
	}
	fmt.Fprintf(&b, "echo 'sparkflink %s %s done'\n", sfType, action)
	return b.String()
}
