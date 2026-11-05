// Package controlplane db_cloud_managed_ops.go implements cloud-managed database ops support.
package controlplane

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// getStringField extracts a non-empty string from a decoded JSON object using
// the provided key aliases (first match wins).
func getStringField(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				return strings.TrimSpace(t)
			}
		case float64:
			return fmt.Sprintf("%g", t)
		case bool:
			if t {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func decodeCloudManagedBody(r *http.Request) (map[string]interface{}, error) {
	var raw map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&raw)
	if err == io.EOF {
		return map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]interface{}{}
	}
	return raw, nil
}

// handleDBAurora handles POST /api/v1/db/aurora
func (s *Server) handleDBAurora(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	raw, err := decodeCloudManagedBody(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := getStringField(raw, "deviceID", "deviceId", "agentID", "agentId")
	action := getStringField(raw, "action")
	id := getStringField(raw, "id", "clusterId", "clusterID", "dbClusterIdentifier", "dbClusterId", "dbClusterID", "identifier", "name")
	if id == "" {
		id = "<id>"
	}

	var script string
	switch action {
	case "status":
		script = fmt.Sprintf("aws rds describe-db-clusters --db-cluster-identifier %s", shellQuote(id))
	case "create":
		script = "aws rds create-db-cluster --engine aurora-mysql"
	case "delete":
		script = fmt.Sprintf("aws rds delete-db-cluster --db-cluster-identifier %s --skip-final-snapshot", shellQuote(id))
	case "backup":
		script = "aws rds create-db-cluster-snapshot"
	case "restore":
		script = "aws rds restore-db-cluster-from-snapshot"
	case "replica":
		script = fmt.Sprintf("aws rds create-db-instance --db-cluster-identifier %s --db-instance-class db.r6g.large", shellQuote(id))
	case "failover":
		script = "aws rds failover-db-cluster"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, deviceID, script, "db_aurora_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action})
}

// handleDBCloudSQL handles POST /api/v1/db/cloudsql
func (s *Server) handleDBCloudSQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	raw, err := decodeCloudManagedBody(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := getStringField(raw, "deviceID", "deviceId", "agentID", "agentId")
	action := getStringField(raw, "action")
	name := getStringField(raw, "name", "instance", "instanceName", "id")
	if name == "" {
		name = "<name>"
	}
	backupID := getStringField(raw, "backupId", "backupID", "backup_id", "backupId", "backup")
	if backupID == "" {
		backupID = "<backup-id>"
	}

	var script string
	switch action {
	case "status":
		script = fmt.Sprintf("gcloud sql instances describe %s", shellQuote(name))
	case "create":
		script = fmt.Sprintf("gcloud sql instances create %s --database-version=MYSQL_8_0 --tier=db-f1-micro", shellQuote(name))
	case "delete":
		script = fmt.Sprintf("gcloud sql instances delete %s", shellQuote(name))
	case "backup":
		script = fmt.Sprintf("gcloud sql backups create --instance=%s", shellQuote(name))
	case "restore":
		script = fmt.Sprintf("gcloud sql backups restore %s --backup-instance=%s", shellQuote(backupID), shellQuote(name))
	case "replica":
		script = fmt.Sprintf("gcloud sql instances create %s-replica --master-instance-name=%s", shellQuote(name), shellQuote(name))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, deviceID, script, "db_cloudsql_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action})
}

// handleDBAzureSQL handles POST /api/v1/db/azuresql
func (s *Server) handleDBAzureSQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	raw, err := decodeCloudManagedBody(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := getStringField(raw, "deviceID", "deviceId", "agentID", "agentId")
	action := getStringField(raw, "action")
	db := getStringField(raw, "db", "database", "databaseName", "name")
	if db == "" {
		db = "<db>"
	}
	server := getStringField(raw, "server", "serverName")
	if server == "" {
		server = "<server>"
	}
	rg := getStringField(raw, "rg", "resourceGroup", "resourceGroupName")
	if rg == "" {
		rg = "<rg>"
	}
	key := getStringField(raw, "key", "storageKey", "storage_key")
	if key == "" {
		key = "<key>"
	}
	uri := getStringField(raw, "uri", "storageUri", "storage_uri")
	if uri == "" {
		uri = "<uri>"
	}
	rule := getStringField(raw, "rule", "ruleName", "firewallRule", "name")
	if rule == "" {
		rule = "<rule>"
	}
	ip := getStringField(raw, "ip", "startIp", "startIPAddress", "ipAddress", "address")
	if ip == "" {
		ip = "<ip>"
	}

	var script string
	switch action {
	case "status":
		script = fmt.Sprintf("az sql db show --name %s --server %s --resource-group %s", shellQuote(db), shellQuote(server), shellQuote(rg))
	case "create":
		script = fmt.Sprintf("az sql db create --name %s --server %s --resource-group %s --edition Standard", shellQuote(db), shellQuote(server), shellQuote(rg))
	case "delete":
		script = fmt.Sprintf("az sql db delete --name %s --server %s --resource-group %s --yes", shellQuote(db), shellQuote(server), shellQuote(rg))
	case "backup":
		script = fmt.Sprintf("az sql db export --name %s --server %s --storage-key %s --storage-key-type StorageAccessKey --storage-uri %s", shellQuote(db), shellQuote(server), shellQuote(key), shellQuote(uri))
	case "restore":
		script = fmt.Sprintf("az sql db import --name %s --server %s --storage-key %s --storage-key-type StorageAccessKey --storage-uri %s", shellQuote(db), shellQuote(server), shellQuote(key), shellQuote(uri))
	case "firewall":
		script = fmt.Sprintf("az sql server firewall-rule create --name %s --server %s --resource-group %s --start-ip-address %s --end-ip-address %s", shellQuote(rule), shellQuote(server), shellQuote(rg), shellQuote(ip), shellQuote(ip))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, deviceID, script, "db_azuresql_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action})
}

// handleDBPolarDB handles POST /api/v1/db/polardb
func (s *Server) handleDBPolarDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	raw, err := decodeCloudManagedBody(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := getStringField(raw, "deviceID", "deviceId", "agentID", "agentId")
	action := getStringField(raw, "action")
	id := getStringField(raw, "id", "dbClusterId", "dbClusterID", "dbClusterIdentifier", "clusterId", "clusterID", "identifier", "name")
	if id == "" {
		id = "<id>"
	}
	vpc := getStringField(raw, "vpc", "vpcId", "vpcID", "VPCId")
	if vpc == "" {
		vpc = "<vpc>"
	}
	vswitch := getStringField(raw, "vswitch", "vswitchId", "vswitchID", "VSwitchId", "vSwitchId")
	if vswitch == "" {
		vswitch = "<vswitch>"
	}

	var script string
	switch action {
	case "status":
		script = "aliyun polardb DescribeDBClusters"
	case "create":
		script = fmt.Sprintf("aliyun polardb CreateDBCluster --DBType MySQL --DBVersion 8.0 --PayType Postpaid --DBNodeClass polar.mysql.x4.medium --ClusterNetworkType VPC --VPCId %s --VSwitchId %s", shellQuote(vpc), shellQuote(vswitch))
	case "delete":
		script = fmt.Sprintf("aliyun polardb DeleteDBCluster --DBClusterId %s", shellQuote(id))
	case "backup":
		script = fmt.Sprintf("aliyun polardb CreateBackup --DBClusterId %s", shellQuote(id))
	case "restore":
		script = "aliyun polardb RestoreTable"
	case "replica":
		script = "aliyun polardb CreateDBNodes"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, deviceID, script, "db_polardb_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action})
}

// handleDBGaussDBDWS handles POST /api/v1/db/gaussdbdws
func (s *Server) handleDBGaussDBDWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	raw, err := decodeCloudManagedBody(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := getStringField(raw, "deviceID", "deviceId", "agentID", "agentId")
	action := getStringField(raw, "action")
	name := getStringField(raw, "name", "instanceName", "instance", "id")
	if name == "" {
		name = "<name>"
	}
	id := getStringField(raw, "id", "instanceId", "instanceID", "instance_id", "InstanceId")
	if id == "" {
		id = "<id>"
	}
	backupID := getStringField(raw, "backupId", "backupID", "backup_id", "BackupId", "backup")
	if backupID == "" {
		backupID = "<backup-id>"
	}

	var script string
	switch action {
	case "status":
		script = "huaweicloud gaussdb list-instances"
	case "create":
		script = fmt.Sprintf("huaweicloud gaussdb create-instance --name %s --engine-version 9.1.0.128 --node-type dws.m1.xlarge.ultrahigh", shellQuote(name))
	case "delete":
		script = fmt.Sprintf("huaweicloud gaussdb delete-instance --instance-id %s", shellQuote(id))
	case "backup":
		script = fmt.Sprintf("huaweicloud gaussdb create-backup --instance-id %s", shellQuote(id))
	case "restore":
		script = fmt.Sprintf("huaweicloud gaussdb restore-instance --instance-id %s --backup-id %s", shellQuote(id), shellQuote(backupID))
	case "node":
		script = fmt.Sprintf("huaweicloud gaussdb create-node --instance-id %s --node-type dws.m1.xlarge.ultrahigh", shellQuote(id))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, deviceID, script, "db_gaussdbdws_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action})
}

// handleDBTDSQLC handles POST /api/v1/db/tdsqlc
func (s *Server) handleDBTDSQLC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	raw, err := decodeCloudManagedBody(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := getStringField(raw, "deviceID", "deviceId", "agentID", "agentId")
	action := getStringField(raw, "action")
	id := getStringField(raw, "id", "instanceId", "instanceID", "instance_id", "InstanceId")
	if id == "" {
		id = "<id>"
	}
	backupID := getStringField(raw, "backupId", "backupID", "backup_id", "BackupId", "backup")
	if backupID == "" {
		backupID = "<backup-id>"
	}

	var script string
	switch action {
	case "status":
		script = "tccli cdb DescribeDBInstances"
	case "create":
		script = "tccli cdb CreateDBInstance --GoodsNum 1 --Memory 1000 --Volume 10 --Period 1 --Zone ap-guangzhou-2 --InstanceRole master"
	case "delete":
		script = fmt.Sprintf("tccli cdb DeleteDBInstance --InstanceId %s", shellQuote(id))
	case "backup":
		script = fmt.Sprintf("tccli cdb CreateBackup --InstanceId %s", shellQuote(id))
	case "restore":
		script = fmt.Sprintf("tccli cdb RestoreInstance --InstanceId %s --BackupId %s", shellQuote(id), shellQuote(backupID))
	case "replica":
		script = fmt.Sprintf("tccli cdb CreateRoInstance --MasterInstanceId %s", shellQuote(id))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, deviceID, script, "db_tdsqlc_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action})
}

// registerCloudManagedDBRoutes registers cloud-managed database ops routes.
func (s *Server) registerCloudManagedDBRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/aurora", s.handleDBAurora)
	mux.HandleFunc("/api/v1/db/cloudsql", s.handleDBCloudSQL)
	mux.HandleFunc("/api/v1/db/azuresql", s.handleDBAzureSQL)
	mux.HandleFunc("/api/v1/db/polardb", s.handleDBPolarDB)
	mux.HandleFunc("/api/v1/db/gaussdbdws", s.handleDBGaussDBDWS)
	mux.HandleFunc("/api/v1/db/tdsqlc", s.handleDBTDSQLC)
}
