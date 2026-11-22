// db_hadoop_ecosystem_ops.go Hadoop 生态管理（Ranger/Atlas/Knox/Sqoop/Flume/Hue/Oozie/Zeppelin）。
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

var hadoopEcoNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var hadoopEcoTypes = map[string]bool{
	"ranger": true, "atlas": true, "knox": true, "sqoop": true,
	"flume": true, "hue": true, "oozie": true, "zeppelin": true,
}

var hadoopEcoActions = map[string]bool{
	// Ranger
	"status": true, "policy-list": true, "policy-create": true, "policy-delete": true,
	"audit": true, "user-sync": true,
	// Atlas
	"entity-search": true, "lineage": true, "type-def": true, "classification": true,
	// Knox
	"topology-list": true, "topology-create": true, "service-list": true, "ldap-config": true,
	// Sqoop
	"import": true, "export": true, "job-list": true, "job-create": true,
	"job-delete": true, "job-exec": true,
	// Flume
	"agent-status": true, "flow-list": true, "flow-deploy": true, "flow-stop": true,
	// Hue
	"user-list": true, "query-history": true,
	// Oozie
	"workflow-list": true, "workflow-submit": true, "workflow-kill": true,
	"coordinator-list": true, "bundle-list": true,
	// Zeppelin
	"notebook-list": true, "notebook-run": true, "interpreter-list": true, "interpreter-restart": true,
}

func (s *Server) handleHadoopEcosystem(w http.ResponseWriter, r *http.Request) {
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
	ecoType := getStr(req, "type")
	if ecoType == "" {
		ecoType = "ranger"
	}
	if !hadoopEcoTypes[ecoType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + ecoType})
		return
	}
	action := getStr(req, "action")
	if action == "" || !hadoopEcoNameRe.MatchString(action) || !hadoopEcoActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !hadoopEcoNameRe.MatchString(str) {
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

	script := buildHadoopEcosystemScript(ecoType, action, req)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "hadoop_ecosystem_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.hadoop.ecosystem", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_hadoop_ecosystem_" + action,
		Target: ecoType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + ecoType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func buildHadoopEcosystemScript(ecoType, action string, req map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	name := getStr(req, "name")
	if name == "" {
		name = getStr(req, "jobName")
	}
	if name == "" {
		name = "default"
	}
	table := getStr(req, "table")
	if table == "" {
		table = getStr(req, "tableName")
	}
	if table == "" {
		table = "default_table"
	}
	id := getStr(req, "id")
	if id == "" {
		id = getStr(req, "notebookID")
	}
	if id == "" {
		id = "notebook"
	}
	switch action {
	// Ranger
	case "status":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/servicedefs\n")
	case "policy-list":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/policy\n")
	case "policy-create":
		b.WriteString("curl -s -X POST -H 'Content-Type: application/json' -d '{\"service\":\"hdfs\",\"name\":\"policy1\",\"policyType\":0}' http://localhost:6080/service/public/v2/api/policy\n")
	case "policy-delete":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:6080/service/public/v2/api/policy/%s\n", shellquote.Quote(name))
	case "audit":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/audit\n")
	case "user-sync":
		b.WriteString("curl -s -X POST http://localhost:6080/service/xusers/users\n")
	// Atlas
	case "entity-search":
		b.WriteString("curl -s 'http://localhost:21000/api/atlas/v2/search/basic?query=*'\n")
	case "lineage":
		b.WriteString("curl -s http://localhost:21000/api/atlas/v2/lineage\n")
	case "type-def":
		b.WriteString("curl -s http://localhost:21000/api/atlas/v2/types/typedefs\n")
	case "classification":
		b.WriteString("curl -s http://localhost:21000/api/atlas/v2/classification\n")
	// Knox
	case "topology-list":
		b.WriteString("curl -s http://localhost:8443/gateway/admin/api/v1/topologies\n")
	case "topology-create":
		b.WriteString("curl -s -X POST -H 'Content-Type: application/xml' -d '<topology></topology>' http://localhost:8443/gateway/admin/api/v1/topologies\n")
	case "service-list":
		b.WriteString("curl -s http://localhost:8443/gateway/admin/api/v1/services\n")
	case "ldap-config":
		b.WriteString("curl -s http://localhost:8443/gateway/admin/api/v1/ldap\n")
	// Sqoop
	case "import":
		fmt.Fprintf(&b, "sqoop import --connect jdbc:mysql://localhost/%s --table %s --target-dir /user/sqoop/%s\n", "db", shellquote.Quote(table), shellquote.Quote(table))
	case "export":
		fmt.Fprintf(&b, "sqoop export --connect jdbc:mysql://localhost/db --table %s --export-dir /user/sqoop/%s\n", shellquote.Quote(table), shellquote.Quote(table))
	case "job-list":
		b.WriteString("sqoop job --list\n")
	case "job-create":
		fmt.Fprintf(&b, "sqoop job --create %s -- import --connect jdbc:mysql://localhost/db --table %s\n", shellquote.Quote(name), shellquote.Quote(table))
	case "job-delete":
		fmt.Fprintf(&b, "sqoop job --delete %s\n", shellquote.Quote(name))
	case "job-exec":
		fmt.Fprintf(&b, "sqoop job --exec %s\n", shellquote.Quote(name))
	// Flume
	case "agent-status":
		b.WriteString("flume-ng version\n")
	case "flow-list":
		b.WriteString("find /etc/flume-ng/conf -name '*.conf' 2>/dev/null || echo 'no flume conf'\n")
	case "flow-deploy":
		b.WriteString("flume-ng agent -n agent -f /etc/flume-ng/conf/flume.conf -Dflume.root.logger=INFO,console\n")
	case "flow-stop":
		b.WriteString("pkill -f flume-ng || true\n")
	// Hue
	case "user-list":
		b.WriteString("curl -s http://localhost:8888/api/users\n")
	case "query-history":
		b.WriteString("curl -s http://localhost:8888/api/query/history\n")
	// Oozie
	case "workflow-list":
		b.WriteString("oozie jobs -jobtype wf\n")
	case "workflow-submit":
		b.WriteString("oozie job -submit oozie-workflow.xml\n")
	case "workflow-kill":
		fmt.Fprintf(&b, "oozie job -kill %s\n", shellquote.Quote(id))
	case "coordinator-list":
		b.WriteString("oozie jobs -jobtype coordinator\n")
	case "bundle-list":
		b.WriteString("oozie jobs -jobtype bundle\n")
	// Zeppelin
	case "notebook-list":
		b.WriteString("curl -s http://localhost:8080/api/notebook\n")
	case "notebook-run":
		fmt.Fprintf(&b, "curl -s -X PUT http://localhost:8080/api/notebook/%s/run\n", shellquote.Quote(id))
	case "interpreter-list":
		b.WriteString("curl -s http://localhost:8080/api/interpreter\n")
	case "interpreter-restart":
		b.WriteString("curl -s -X PUT http://localhost:8080/api/interpreter/restart/spark\n")
	default:
		fmt.Fprintf(&b, "echo 'hadoop ecosystem %s %s done'\n", ecoType, action)
	}
	fmt.Fprintf(&b, "echo 'hadoop ecosystem %s %s done'\n", ecoType, action)
	return b.String()
}
