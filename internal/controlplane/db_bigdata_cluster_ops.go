// db_bigdata_cluster_ops.go 大数据集群管理（Ambari + Cloudera/CDP + 通用 Hadoop HA + Kerberos）。
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

	script := buildBigDataClusterScript(bType, action, req)
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

func buildBigDataClusterScript(bType, action string, req map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	get := func(key string) string { return getStr(req, key) }
	principal := get("principal")
	if principal == "" {
		principal = "hdfs/_HOST@EXAMPLE.COM"
	}
	switch action {
	// Ambari
	case "ambari-deploy":
		b.WriteString("ambari-server start\n")
	case "ambari-status":
		b.WriteString("ambari-server status\n")
	case "ambari-services":
		b.WriteString("curl -s -u admin:admin http://localhost:8080/api/v1/clusters/default/services\n")
	case "ambari-service-start":
		b.WriteString("curl -s -u admin:admin -X PUT -H 'Content-Type: application/json' -d '{\"RequestInfo\":{\"context\":\"Start\"},\"Body\":{\"ServiceInfo\":{\"state\":\"STARTED\"}}}' http://localhost:8080/api/v1/clusters/default/services/HDFS\n")
	case "ambari-service-stop":
		b.WriteString("curl -s -u admin:admin -X PUT -H 'Content-Type: application/json' -d '{\"RequestInfo\":{\"context\":\"Stop\"},\"Body\":{\"ServiceInfo\":{\"state\":\"INSTALLED\"}}}' http://localhost:8080/api/v1/clusters/default/services/HDFS\n")
	case "ambari-service-restart":
		b.WriteString("curl -s -u admin:admin -X PUT -H 'Content-Type: application/json' -d '{\"RequestInfo\":{\"context\":\"Restart\"},\"Body\":{\"ServiceInfo\":{\"state\":\"STARTED\"}}}' http://localhost:8080/api/v1/clusters/default/services/HDFS\n")
	case "ambari-config-get":
		b.WriteString("curl -s -u admin:admin http://localhost:8080/api/v1/clusters/default/configurations\n")
	case "ambari-config-set":
		b.WriteString("curl -s -u admin:admin -X POST -H 'Content-Type: application/json' -d '{\"type\":\"core-site\",\"tag\":\"version1\",\"properties\":{}}' http://localhost:8080/api/v1/clusters/default/configurations\n")
	case "ambari-hosts":
		b.WriteString("curl -s -u admin:admin http://localhost:8080/api/v1/clusters/default/hosts\n")
	case "ambari-host-add":
		b.WriteString("curl -s -u admin:admin -X POST -H 'Content-Type: application/json' -d '{\"host_name\":\"newhost\"}' http://localhost:8080/api/v1/clusters/default/hosts\n")
	case "ambari-host-remove":
		b.WriteString("curl -s -u admin:admin -X DELETE http://localhost:8080/api/v1/clusters/default/hosts/oldhost\n")
	case "ambari-cluster-create":
		b.WriteString("curl -s -u admin:admin -X POST -H 'Content-Type: application/json' -d '{\"Clusters\":{\"cluster_name\":\"default\",\"version\":\"HDP-3.1\"}}' http://localhost:8080/api/v1/clusters\n")
	case "ambari-blueprint":
		b.WriteString("curl -s -u admin:admin http://localhost:8080/api/v1/blueprints\n")
	case "ambari-rolling-restart":
		b.WriteString("curl -s -u admin:admin -X POST -H 'Content-Type: application/json' -d '{\"rolling_restart\":\"true\"}' http://localhost:8080/api/v1/clusters/default/services/HDFS\n")
	case "ambari-kerberos-enable":
		b.WriteString("ambari-server setup-kerberos -k /etc/krb5.keytab\n")
	case "ambari-kerberos-disable":
		b.WriteString("ambari-server disable-kerberos\n")
	case "ambari-alerts":
		b.WriteString("curl -s -u admin:admin http://localhost:8080/api/v1/clusters/default/alerts\n")
	case "ambari-metrics":
		b.WriteString("curl -s -u admin:admin http://localhost:8080/api/v1/clusters/default/services/AMBARI_METRICS\n")
	// Cloudera / CM
	case "cm-deploy":
		b.WriteString("cloudera-manager --install\n")
	case "cm-status":
		b.WriteString("curl -s http://localhost:7180/api/v1/cm/version\n")
	case "cm-clusters":
		b.WriteString("curl -s http://localhost:7180/api/v1/clusters\n")
	case "cm-services":
		b.WriteString("curl -s http://localhost:7180/api/v1/clusters/default/services\n")
	case "cm-service-start":
		b.WriteString("curl -s -X POST http://localhost:7180/api/v1/clusters/default/services/hdfs/commands/start\n")
	case "cm-service-stop":
		b.WriteString("curl -s -X POST http://localhost:7180/api/v1/clusters/default/services/hdfs/commands/stop\n")
	case "cm-service-restart":
		b.WriteString("curl -s -X POST http://localhost:7180/api/v1/clusters/default/services/hdfs/commands/restart\n")
	case "cm-config":
		b.WriteString("curl -s http://localhost:7180/api/v1/clusters/default/services/hdfs/config\n")
	case "cm-hosts":
		b.WriteString("curl -s http://localhost:7180/api/v1/hosts\n")
	case "cm-kerberos":
		b.WriteString("curl -s http://localhost:7180/api/v1/clusters/default/commands/enableKerberos\n")
	case "cm-rolling-restart":
		b.WriteString("curl -s -X POST http://localhost:7180/api/v1/clusters/default/services/hdfs/commands/rollingRestart\n")
	case "cm-upgrade":
		b.WriteString("cloudera-manager --upgrade\n")
	case "cm-parcel-list":
		b.WriteString("curl -s http://localhost:7180/api/v1/clusters/default/parcels\n")
	case "cm-parcel-install":
		b.WriteString("curl -s -X POST http://localhost:7180/api/v1/clusters/default/parcels/products/CDH/versions/latest/commands/install\n")
	case "cm-parcel-activate":
		b.WriteString("curl -s -X POST http://localhost:7180/api/v1/clusters/default/parcels/products/CDH/versions/latest/commands/activate\n")
	case "cm-alerts":
		b.WriteString("curl -s http://localhost:7180/api/v1/events\n")
	// Hadoop HA
	case "ha-namenode-status":
		b.WriteString("hdfs haadmin -getServiceState nn1\n")
	case "ha-namenode-failover":
		b.WriteString("hdfs haadmin -failover nn1 nn2\n")
	case "ha-namenode-checkpoint":
		b.WriteString("hdfs dfsadmin -rollEdits\n")
	case "ha-zkfc-status":
		b.WriteString("jps | grep -i DFSZKFailoverController\n")
	case "ha-journalnode-status":
		b.WriteString("hdfs --daemon status journalnode\n")
	case "ha-yarn-rm-status":
		b.WriteString("yarn rmadmin -getServiceState rm1\n")
	case "ha-yarn-rm-failover":
		b.WriteString("yarn rmadmin -failover rm1 rm2\n")
	case "ha-hdfs-fsck":
		b.WriteString("hdfs fsck / -files -blocks -locations\n")
	case "ha-hdfs-balancer":
		b.WriteString("hdfs balancer\n")
	case "ha-hdfs-quota-set":
		b.WriteString("hdfs dfsadmin -setQuota 1000 /user\n")
	case "ha-hdfs-quota-get":
		b.WriteString("hdfs dfsadmin -count -q /\n")
	case "ha-yarn-queue-list":
		b.WriteString("yarn queue -list\n")
	case "ha-yarn-queue-create":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "ha-yarn-queue-delete":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "ha-yarn-queue-update":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "ha-yarn-app-kill":
		appID := get("appID")
		if appID == "" {
			appID = get("id")
		}
		if appID == "" {
			appID = "application_0"
		}
		fmt.Fprintf(&b, "yarn application -kill %s\n", shellquote.Quote(appID))
	case "ha-yarn-app-list":
		b.WriteString("yarn application -list\n")
	// Kerberos
	case "kerberos-status":
		b.WriteString("systemctl status krb5kdc kadmin\n")
	case "kerberos-principal-list":
		b.WriteString("kadmin.local -q 'listprincs'\n")
	case "kerberos-principal-create":
		fmt.Fprintf(&b, "kadmin.local -q 'addprinc -randkey %s'\n", shellquote.Quote(principal))
	case "kerberos-principal-delete":
		fmt.Fprintf(&b, "kadmin.local -q 'delprinc -force %s'\n", shellquote.Quote(principal))
	case "kerberos-keytab-generate":
		fmt.Fprintf(&b, "kadmin.local -q 'ktadd -k /tmp/%s.keytab %s'\n", "service", shellquote.Quote(principal))
	case "kerberos-kinit-test":
		fmt.Fprintf(&b, "kinit -kt /tmp/service.keytab %s && klist\n", shellquote.Quote(principal))
	default:
		fmt.Fprintf(&b, "echo 'bigdata cluster %s %s done'\n", bType, action)
	}
	fmt.Fprintf(&b, "echo 'bigdata cluster %s %s done'\n", bType, action)
	return b.String()
}
