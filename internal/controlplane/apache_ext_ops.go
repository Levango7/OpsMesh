// apache_ext_ops.go Apache 扩展组件运维支持（Hadoop/Hive/HBase/Solr/Druid/NiFi/Impala）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleApacheHadoop 处理 POST /api/v1/middleware/apache/hadoop
func (s *Server) handleApacheHadoop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/restart/deploy/hdfs/namenode/datanode
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "3.3.6"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Hadoop Status ===" && (hadoop version 2>/dev/null || echo "hadoop not installed") && echo "=== JPS ===" && (jps 2>/dev/null || echo "jps not available")`
	case "start":
		script = `start-dfs.sh 2>/dev/null; start-yarn.sh 2>/dev/null; echo "hadoop start attempted"`
	case "stop":
		script = `stop-yarn.sh 2>/dev/null; stop-dfs.sh 2>/dev/null; echo "hadoop stop attempted"`
	case "restart":
		script = `stop-yarn.sh 2>/dev/null; stop-dfs.sh 2>/dev/null; sleep 2; start-dfs.sh 2>/dev/null; start-yarn.sh 2>/dev/null; echo "hadoop restart attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/hadoop ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/hadoop/common/hadoop-${VER}/hadoop-${VER}.tar.gz -O /tmp/hadoop.tgz
  mkdir -p /opt && tar -xzf /tmp/hadoop.tgz -C /opt
  ln -sfn /opt/hadoop-${VER} /opt/hadoop
fi
export HADOOP_HOME=/opt/hadoop
echo "Hadoop %s deployed"`, versionV, versionV)
	case "hdfs":
		script = `echo "=== HDFS ls / ===" && (hdfs dfs -ls / 2>/dev/null || echo "hdfs not available")`
	case "namenode":
		script = `echo "=== Format NameNode ===" && (hdfs namenode -format -nonInteractive 2>/dev/null || hdfs namenode -format) && echo "namenode formatted"`
	case "datanode":
		script = `echo "=== Start DataNode ===" && (hdfs --daemon start datanode 2>/dev/null || hdfs datanode) ; echo "datanode attempted"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_hadoop_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheHive 处理 POST /api/v1/middleware/apache/hive
func (s *Server) handleApacheHive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/deploy/schema/query
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "3.1.3"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Hive Status ===" && (hive --version 2>/dev/null || echo "hive not available")`
	case "start":
		script = `hive --service hiveserver2 2>/dev/null & echo "hive start attempted"`
	case "stop":
		script = `pkill -f hiveserver2 2>/dev/null; echo "hive stop attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/hive ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/hive/hive-${VER}/apache-hive-${VER}-bin.tar.gz -O /tmp/hive.tgz
  mkdir -p /opt && tar -xzf /tmp/hive.tgz -C /opt
  ln -sfn /opt/apache-hive-${VER}-bin /opt/hive
fi
export HIVE_HOME=/opt/hive
echo "Hive %s deployed"`, versionV, versionV)
	case "schema":
		script = `echo "=== Init Hive Schema ===" && (schematool -initSchema -dbType derby 2>/dev/null || schematool -initSchema) && echo "schema initialized"`
	case "query":
		script = `echo "=== Hive Query ===" && (beeline -e "SHOW DATABASES" 2>/dev/null || echo "beeline not available")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_hive_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheHBase 处理 POST /api/v1/middleware/apache/hbase
func (s *Server) handleApacheHBase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/deploy/table/scan
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "2.5.8"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== HBase Status ===" && (hbase version 2>/dev/null || echo "hbase not available")`
	case "start":
		script = `start-hbase.sh 2>/dev/null; echo "hbase start attempted"`
	case "stop":
		script = `stop-hbase.sh 2>/dev/null; echo "hbase stop attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/hbase ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/hbase/${VER}/hbase-${VER}-bin.tar.gz -O /tmp/hbase.tgz
  mkdir -p /opt && tar -xzf /tmp/hbase.tgz -C /opt
  ln -sfn /opt/hbase-${VER} /opt/hbase
fi
export HBASE_HOME=/opt/hbase
echo "HBase %s deployed"`, versionV, versionV)
	case "table":
		script = `echo "=== Create HBase Table ===" && (echo "create 'ns:table', 'cf'" | hbase shell 2>/dev/null || hbase shell -e "create 'ns:table', 'cf'")`
	case "scan":
		script = `echo "=== Scan HBase Table ===" && (echo "scan 'ns:table'" | hbase shell 2>/dev/null || hbase shell -e "scan 'ns:table'")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_hbase_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheSolr 处理 POST /api/v1/middleware/apache/solr
func (s *Server) handleApacheSolr(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/deploy/collection/query
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "9.4.1"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Solr Status ===" && (solr version 2>/dev/null || /opt/solr/bin/solr version 2>/dev/null || echo "solr not available")`
	case "start":
		script = `solr start 2>/dev/null || /opt/solr/bin/solr start 2>/dev/null; echo "solr start attempted"`
	case "stop":
		script = `solr stop -all 2>/dev/null || /opt/solr/bin/solr stop -all 2>/dev/null; echo "solr stop attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/solr ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/solr/solr/${VER}/solr-${VER}.tgz -O /tmp/solr.tgz
  mkdir -p /opt && tar -xzf /tmp/solr.tgz -C /opt
  ln -sfn /opt/solr-${VER} /opt/solr
fi
export SOLR_HOME=/opt/solr
echo "Solr %s deployed"`, versionV, versionV)
	case "collection":
		script = `echo "=== Create Solr Collection ===" && (solr create -c collection 2>/dev/null || /opt/solr/bin/solr create -c collection) ; echo "collection created"`
	case "query":
		script = `echo "=== Solr Query ===" && (curl -s "http://localhost:8983/solr/collection/select?q=*:*" 2>/dev/null || echo "solr query failed")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_solr_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheDruid 处理 POST /api/v1/middleware/apache/druid
func (s *Server) handleApacheDruid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/deploy/datasource/query
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "28.0.0"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Druid Status ===" && (druid version 2>/dev/null || echo "druid not available")`
	case "start":
		script = `druid start 2>/dev/null || /opt/druid/bin/run-druid.sh 2>/dev/null & echo "druid start attempted"`
	case "stop":
		script = `pkill -f druid 2>/dev/null; echo "druid stop attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/druid ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/druid/${VER}/apache-druid-${VER}-bin.tar.gz -O /tmp/druid.tgz
  mkdir -p /opt && tar -xzf /tmp/druid.tgz -C /opt
  ln -sfn /opt/apache-druid-${VER} /opt/druid
fi
export DRUID_HOME=/opt/druid
echo "Druid %s deployed"`, versionV, versionV)
	case "datasource":
		script = `echo "=== Druid Datasource ===" && (curl -s http://localhost:8888/druid/v2/datasources 2>/dev/null || echo "druid datasource")`
	case "query":
		script = `echo "=== Druid Query ===" && (curl -s -X POST -H 'Content-Type: application/json' -d '{"query":"SELECT 1"}' http://localhost:8888/druid/v2/sql 2>/dev/null || echo "druid query")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_druid_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheNiFi 处理 POST /api/v1/middleware/apache/nifi
func (s *Server) handleApacheNiFi(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/deploy/flow/processor
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "1.25.0"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== NiFi Status ===" && (nifi.sh status 2>/dev/null || /opt/nifi/bin/nifi.sh status 2>/dev/null || echo "nifi not available")`
	case "start":
		script = `nifi.sh start 2>/dev/null || /opt/nifi/bin/nifi.sh start 2>/dev/null; echo "nifi start attempted"`
	case "stop":
		script = `nifi.sh stop 2>/dev/null || /opt/nifi/bin/nifi.sh stop 2>/dev/null; echo "nifi stop attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/nifi ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/nifi/${VER}/nifi-${VER}-bin.tar.gz -O /tmp/nifi.tgz
  mkdir -p /opt && tar -xzf /tmp/nifi.tgz -C /opt
  ln -sfn /opt/nifi-${VER} /opt/nifi
fi
export NIFI_HOME=/opt/nifi
echo "NiFi %s deployed"`, versionV, versionV)
	case "flow":
		script = `echo "=== NiFi Flow ===" && (curl -s http://localhost:8080/nifi-api/flow/process-groups/root 2>/dev/null || echo "nifi flow")`
	case "processor":
		script = `echo "=== NiFi Processor ===" && (curl -s http://localhost:8080/nifi-api/processors 2>/dev/null || echo "nifi processor")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_nifi_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheImpala 处理 POST /api/v1/middleware/apache/impala
func (s *Server) handleApacheImpala(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/deploy/query
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "4.3.0"
	}
	// 收口（批次 15）：handler 侧已做白名单校验 ⇒ 用局部变量承接已校验值，
	// 实参不再直接引用 req.*（判据 E）；**脚本文本逐字节不变**。
	if !isVersionLike(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version: expected x.y.z, got " + req.Version})
		return
	}
	versionV := req.Version

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Impala Status ===" && (impala-shell --version 2>/dev/null || impalad --version 2>/dev/null || echo "impala not available")`
	case "start":
		script = `impalad 2>/dev/null & statestored 2>/dev/null & catalogd 2>/dev/null & echo "impala start attempted"`
	case "stop":
		script = `pkill -f impalad 2>/dev/null; pkill -f statestored 2>/dev/null; pkill -f catalogd 2>/dev/null; echo "impala stop attempted"`
	case "deploy":
		script = fmt.Sprintf(`set -e
if [ ! -d /opt/impala ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/impala/${VER}/impala-${VER}.tar.gz -O /tmp/impala.tgz
  mkdir -p /opt && tar -xzf /tmp/impala.tgz -C /opt
  ln -sfn /opt/impala-${VER} /opt/impala
fi
export IMPALA_HOME=/opt/impala
echo "Impala %s deployed"`, versionV, versionV)
	case "query":
		script = `echo "=== Impala Query ===" && (impala-shell -q "SHOW DATABASES" 2>/dev/null || echo "impala query failed")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_impala_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerApacheExtRoutes 注册 Apache 扩展路由
func (s *Server) registerApacheExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/apache/hadoop", s.handleApacheHadoop)
	mux.HandleFunc("/api/v1/middleware/apache/hive", s.handleApacheHive)
	mux.HandleFunc("/api/v1/middleware/apache/hbase", s.handleApacheHBase)
	mux.HandleFunc("/api/v1/middleware/apache/solr", s.handleApacheSolr)
	mux.HandleFunc("/api/v1/middleware/apache/druid", s.handleApacheDruid)
	mux.HandleFunc("/api/v1/middleware/apache/nifi", s.handleApacheNiFi)
	mux.HandleFunc("/api/v1/middleware/apache/impala", s.handleApacheImpala)
}
