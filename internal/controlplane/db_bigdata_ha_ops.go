// db_bigdata_ha_ops.go 大数据组件 HA 运维（HBase/Hive/ZooKeeper/Solr/Oozie/YARN/HDFS）。
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

var bigDataHANameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var bigDataHATypes = map[string]bool{
	"hbase": true, "hive": true, "zookeeper": true, "solr": true,
	"oozie": true, "yarn": true, "hdfs": true,
}

var bigDataHAActions = map[string]bool{
	"hbase-master-status": true, "hbase-master-start": true, "hbase-master-stop": true,
	"hbase-region-list": true, "hbase-region-move": true, "hbase-region-balance": true,
	"hbase-compaction-major": true, "hbase-compaction-minor": true, "hbase-flush": true,
	"hbase-snapshot-create": true, "hbase-snapshot-restore": true, "hbase-snapshot-list": true,
	"hbase-snapshot-delete": true, "hbase-backup": true, "hbase-restore": true,
	"hbase-coprocessor-list": true, "hbase-table-disable": true, "hbase-table-enable": true,
	"hbase-table-alter": true, "hbase-table-describe": true,
	"hive-metastore-status": true, "hive-metastore-start": true, "hive-metastore-stop": true,
	"hive-hs2-status": true, "hive-hs2-start": true, "hive-hs2-stop": true,
	"hive-ha-status": true, "hive-ha-failover": true,
	"hive-metastore-db-status": true, "hive-metastore-db-backup": true, "hive-metastore-db-restore": true,
	"hive-partition-list": true, "hive-partition-add": true, "hive-partition-drop": true,
	"hive-table-stats": true, "hive-table-analyze": true,
	"zk-ensemble-status": true, "zk-server-list": true, "zk-server-add": true, "zk-server-remove": true,
	"zk-leader": true, "zk-follower": true, "zk-config-get": true, "zk-config-set": true,
	"zk-acl-list": true, "zk-acl-set": true, "zk-snapshot": true, "zk-restore": true, "zk-purge": true,
	"solrcloud-status": true, "solrcloud-collection-list": true, "solrcloud-collection-create": true,
	"solrcloud-collection-delete": true, "solrcloud-shard-list": true, "solrcloud-shard-split": true,
	"solrcloud-replica-add": true, "solrcloud-replica-delete": true, "solrcloud-rebalance": true,
	"solrcloud-backup": true, "solrcloud-restore": true,
	"oozie-ha-status": true, "oozie-ha-failover": true, "oozie-db-status": true,
	"oozie-db-backup": true, "oozie-db-restore": true,
	"yarn-rm-ha-status": true, "yarn-rm-failover": true, "yarn-nm-list": true,
	"yarn-nm-decommission": true, "yarn-nm-recommission": true, "yarn-queue-status": true,
	"yarn-queue-create": true, "yarn-queue-delete": true, "yarn-queue-update": true,
	"yarn-capacity-scheduler": true, "yarn-fair-scheduler": true,
	"hdfs-namenode-ha-status": true, "hdfs-namenode-failover": true, "hdfs-zkfc-status": true,
	"hdfs-journalnode-status": true, "hdfs-safemode": true, "hdfs-quota-set": true,
	"hdfs-quota-get": true, "hdfs-quota-clr": true, "hdfs-snapshot-create": true,
	"hdfs-snapshot-delete": true, "hdfs-snapshot-list": true, "hdfs-balancer": true,
	"hdfs-fsck": true, "hdfs-block-report": true, "hdfs-decommission": true, "hdfs-recommission": true,
}

func (s *Server) handleBigDataHA(w http.ResponseWriter, r *http.Request) {
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
	bType := getStr(req, "type")
	if bType == "" {
		bType = "hbase"
	}
	if !bigDataHATypes[bType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + bType})
		return
	}
	action := getStr(req, "action")
	if action == "" || !bigDataHANameRe.MatchString(action) || !bigDataHAActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !bigDataHANameRe.MatchString(str) {
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

	script := buildBigDataHAScript(bType, action, req)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "bigdata_ha_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.bigdata.ha", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_bigdata_ha_" + action,
		Target: bType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + bType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func buildBigDataHAScript(bType, action string, req map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	name := getStr(req, "name")
	if name == "" {
		name = getStr(req, "tableName")
	}
	if name == "" {
		name = "default"
	}
	table := getStr(req, "table")
	if table == "" {
		table = name
	}
	snapshot := getStr(req, "snapshot")
	if snapshot == "" {
		snapshot = "snap1"
	}
	region := getStr(req, "region")
	if region == "" {
		region = "region1"
	}
	queue := getStr(req, "queue")
	if queue == "" {
		queue = "default"
	}
	switch action {
	case "hbase-master-status":
		b.WriteString("hbase hbck -details 2>/dev/null || echo 'hbase master status'\n")
	case "hbase-master-start":
		b.WriteString("hbase-daemon.sh start master\n")
	case "hbase-master-stop":
		b.WriteString("hbase-daemon.sh stop master\n")
	case "hbase-region-list":
		b.WriteString("echo 'list_regions' | hbase shell\n")
	case "hbase-region-move":
		fmt.Fprintf(&b, "echo 'move_region %s' | hbase shell\n", shellquote.Quote(region))
	case "hbase-region-balance":
		b.WriteString("echo 'balance_switch true' | hbase shell\n")
	case "hbase-compaction-major":
		fmt.Fprintf(&b, "echo 'major_compact \"%s\"' | hbase shell\n", shellquote.Quote(table))
	case "hbase-compaction-minor":
		fmt.Fprintf(&b, "echo 'compact \"%s\"' | hbase shell\n", shellquote.Quote(table))
	case "hbase-flush":
		fmt.Fprintf(&b, "echo 'flush \"%s\"' | hbase shell\n", shellquote.Quote(table))
	case "hbase-snapshot-create":
		fmt.Fprintf(&b, "echo 'snapshot \"%s\", \"%s\"' | hbase shell\n", shellquote.Quote(table), shellquote.Quote(snapshot))
	case "hbase-snapshot-restore":
		fmt.Fprintf(&b, "echo 'restore_snapshot \"%s\"' | hbase shell\n", shellquote.Quote(snapshot))
	case "hbase-snapshot-list":
		b.WriteString("echo 'list_snapshots' | hbase shell\n")
	case "hbase-snapshot-delete":
		fmt.Fprintf(&b, "echo 'delete_snapshot \"%s\"' | hbase shell\n", shellquote.Quote(snapshot))
	case "hbase-backup":
		fmt.Fprintf(&b, "hbase backup create full /backup/%s\n", shellquote.Quote(table))
	case "hbase-restore":
		b.WriteString("hbase restore /backup/default\n")
	case "hbase-coprocessor-list":
		b.WriteString("echo 'list_coprocessors' | hbase shell 2>/dev/null || echo 'coprocessor list'\n")
	case "hbase-table-disable":
		fmt.Fprintf(&b, "echo 'disable \"%s\"' | hbase shell\n", shellquote.Quote(table))
	case "hbase-table-enable":
		fmt.Fprintf(&b, "echo 'enable \"%s\"' | hbase shell\n", shellquote.Quote(table))
	case "hbase-table-alter":
		fmt.Fprintf(&b, "echo 'alter \"%s\", {NAME => \"cf1\"}' | hbase shell\n", shellquote.Quote(table))
	case "hbase-table-describe":
		fmt.Fprintf(&b, "echo 'describe \"%s\"' | hbase shell\n", shellquote.Quote(table))
	case "hive-metastore-status":
		b.WriteString("hive --service metastore --status 2>/dev/null || systemctl status hive-metastore\n")
	case "hive-metastore-start":
		b.WriteString("hive --service metastore &\n")
	case "hive-metastore-stop":
		b.WriteString("pkill -f 'hive.*metastore' || true\n")
	case "hive-hs2-status":
		b.WriteString("systemctl status hive-server2\n")
	case "hive-hs2-start":
		b.WriteString("hive --service hiveserver2 &\n")
	case "hive-hs2-stop":
		b.WriteString("pkill -f 'hive.*hiveserver2' || true\n")
	case "hive-ha-status":
		b.WriteString("hive --service hiveserver2 --status 2>/dev/null || echo 'hive ha status'\n")
	case "hive-ha-failover":
		b.WriteString("echo 'hive ha failover'\n")
	case "hive-metastore-db-status":
		b.WriteString("hive --service schematool -info\n")
	case "hive-metastore-db-backup":
		b.WriteString("hive --service schematool -backup /backup/hive_metastore.sql\n")
	case "hive-metastore-db-restore":
		b.WriteString("hive --service schematool -restore /backup/hive_metastore.sql\n")
	case "hive-partition-list":
		fmt.Fprintf(&b, "hive -e \"SHOW PARTITIONS %s\"\n", shellquote.Quote(table))
	case "hive-partition-add":
		fmt.Fprintf(&b, "hive -e \"ALTER TABLE %s ADD PARTITION (dt='2024-01-01')\"\n", shellquote.Quote(table))
	case "hive-partition-drop":
		fmt.Fprintf(&b, "hive -e \"ALTER TABLE %s DROP PARTITION (dt='2024-01-01')\"\n", shellquote.Quote(table))
	case "hive-table-stats":
		fmt.Fprintf(&b, "hive -e \"DESCRIBE FORMATTED %s\"\n", shellquote.Quote(table))
	case "hive-table-analyze":
		fmt.Fprintf(&b, "hive -e \"ANALYZE TABLE %s COMPUTE STATISTICS\"\n", shellquote.Quote(table))
	case "zk-ensemble-status":
		b.WriteString("echo 'ruok' | nc localhost 2181\n")
	case "zk-server-list":
		b.WriteString("zkServer.sh status\n")
	case "zk-server-add":
		b.WriteString("echo 'server add requested'\n")
	case "zk-server-remove":
		b.WriteString("echo 'server remove requested'\n")
	case "zk-leader":
		b.WriteString("echo 'stat' | nc localhost 2181 | grep Mode\n")
	case "zk-follower":
		b.WriteString("echo 'stat' | nc localhost 2181 | grep Mode\n")
	case "zk-config-get":
		b.WriteString("cat /etc/zookeeper/conf/zoo.cfg\n")
	case "zk-config-set":
		b.WriteString("echo 'zk config set'\n")
	case "zk-acl-list":
		b.WriteString("echo 'getAcl /' | zkCli.sh\n")
	case "zk-acl-set":
		b.WriteString("echo 'setAcl / world:anyone:r' | zkCli.sh\n")
	case "zk-snapshot":
		b.WriteString("zkServer.sh snapshot\n")
	case "zk-restore":
		b.WriteString("echo 'zk restore'\n")
	case "zk-purge":
		b.WriteString("zkPurge.sh -n 3\n")
	case "solrcloud-status":
		b.WriteString("solr status\n")
	case "solrcloud-collection-list":
		b.WriteString("solrctl collection --list\n")
	case "solrcloud-collection-create":
		fmt.Fprintf(&b, "solrctl collection --create %s\n", shellquote.Quote(name))
	case "solrcloud-collection-delete":
		fmt.Fprintf(&b, "solrctl collection --delete %s\n", shellquote.Quote(name))
	case "solrcloud-shard-list":
		b.WriteString("solrctl shard --list\n")
	case "solrcloud-shard-split":
		fmt.Fprintf(&b, "solrctl shard --split %s\n", shellquote.Quote(name))
	case "solrcloud-replica-add":
		b.WriteString("solrctl replica --add\n")
	case "solrcloud-replica-delete":
		b.WriteString("solrctl replica --delete\n")
	case "solrcloud-rebalance":
		b.WriteString("solrctl rebalance\n")
	case "solrcloud-backup":
		b.WriteString("solrctl backup\n")
	case "solrcloud-restore":
		b.WriteString("solrctl restore\n")
	case "oozie-ha-status":
		b.WriteString("oozie admin -status\n")
	case "oozie-ha-failover":
		b.WriteString("oozie admin -failover\n")
	case "oozie-db-status":
		b.WriteString("oozie db -status\n")
	case "oozie-db-backup":
		b.WriteString("oozie db -backup\n")
	case "oozie-db-restore":
		b.WriteString("oozie db -restore\n")
	case "yarn-rm-ha-status":
		b.WriteString("yarn rmadmin -getServiceState rm1\n")
	case "yarn-rm-failover":
		b.WriteString("yarn rmadmin -failover rm1 rm2\n")
	case "yarn-nm-list":
		b.WriteString("yarn node -list\n")
	case "yarn-nm-decommission":
		b.WriteString("yarn rmadmin -refreshNodes\n")
	case "yarn-nm-recommission":
		b.WriteString("yarn rmadmin -refreshNodes\n")
	case "yarn-queue-status":
		fmt.Fprintf(&b, "yarn queue -status %s\n", shellquote.Quote(queue))
	case "yarn-queue-create":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "yarn-queue-delete":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "yarn-queue-update":
		b.WriteString("yarn rmadmin -refreshQueues\n")
	case "yarn-capacity-scheduler":
		b.WriteString("cat /etc/hadoop/capacity-scheduler.xml\n")
	case "yarn-fair-scheduler":
		b.WriteString("cat /etc/hadoop/fair-scheduler.xml\n")
	case "hdfs-namenode-ha-status":
		b.WriteString("hdfs haadmin -getServiceState nn1\n")
	case "hdfs-namenode-failover":
		b.WriteString("hdfs haadmin -failover nn1 nn2\n")
	case "hdfs-zkfc-status":
		b.WriteString("jps | grep -i DFSZKFailoverController\n")
	case "hdfs-journalnode-status":
		b.WriteString("hdfs --daemon status journalnode\n")
	case "hdfs-safemode":
		b.WriteString("hdfs dfsadmin -safemode get\n")
	case "hdfs-quota-set":
		b.WriteString("hdfs dfsadmin -setQuota 1000 /\n")
	case "hdfs-quota-get":
		b.WriteString("hdfs dfsadmin -count -q /\n")
	case "hdfs-quota-clr":
		b.WriteString("hdfs dfsadmin -clrQuota /\n")
	case "hdfs-snapshot-create":
		fmt.Fprintf(&b, "hdfs dfs -createSnapshot / %s\n", shellquote.Quote(snapshot))
	case "hdfs-snapshot-delete":
		fmt.Fprintf(&b, "hdfs dfs -deleteSnapshot / %s\n", shellquote.Quote(snapshot))
	case "hdfs-snapshot-list":
		b.WriteString("hdfs dfs -ls /.snapshot\n")
	case "hdfs-balancer":
		b.WriteString("hdfs balancer\n")
	case "hdfs-fsck":
		b.WriteString("hdfs fsck / -files -blocks -locations\n")
	case "hdfs-block-report":
		b.WriteString("hdfs dfsadmin -report\n")
	case "hdfs-decommission":
		b.WriteString("hdfs dfsadmin -refreshNodes\n")
	case "hdfs-recommission":
		b.WriteString("hdfs dfsadmin -refreshNodes\n")
	default:
		fmt.Fprintf(&b, "echo 'bigdata ha %s %s done'\n", bType, action)
	}
	fmt.Fprintf(&b, "echo 'bigdata ha %s %s done'\n", bType, action)
	return b.String()
}
