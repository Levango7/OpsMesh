// Package controlplane db_nosql_ext_ops.go 实现 NoSQL/半结构化数据库运维支持。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// HBase 表名（可带 namespace）与列族名走 pkg/shellquote 的统一白名单
// （2026-11-05 迁移，opencode 按 workbuddy 2026-11-04T22:58Z NOTE 收编）。
//
// 为什么这两个值必须白名单而不是引号转义：它们进的是
// `hbase shell <<< "create '<table>', '<cf>'"` 的**单引号段**，一个 `'` 即可
// 跳出引号，外层双引号还会继续展开 `$()` 与反引号——那里没有合法转义空间。
// 语义与旧内联正则逐字等价：每段 [A-Za-z0-9_.-]、1..128 字符、至多一层 `ns:`
// 前缀（ValidateQualifiedName 拒绝三段及以上、拒绝空段），错误文案保持不变。
const hbaseNameMaxSegLen = 128

func firstNonEmptyNoSQL(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// handleDBHBase 处理 POST /api/v1/db/hbase
func (s *Server) handleDBHBase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	var req struct {
		DeviceID     string `json:"deviceID"`
		Action       string `json:"action"`
		Table        string `json:"table"`
		DB           string `json:"db"`
		Name         string `json:"name"`
		Namespace    string `json:"namespace"`
		CF           string `json:"cf"`
		ColumnFamily string `json:"columnFamily"`
		Family       string `json:"family"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	table := firstNonEmptyNoSQL(req.Table, req.DB, req.Name)
	if table == "" {
		table = "ns:table"
	}
	cf := firstNonEmptyNoSQL(req.CF, req.ColumnFamily, req.Family)
	if cf == "" {
		cf = "cf"
	}
	if shellquote.ValidateQualifiedName(table, hbaseNameMaxSegLen) != nil ||
		shellquote.ValidateName(cf, hbaseNameMaxSegLen) != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported table or columnFamily name"})
		return
	}
	var script string
	switch req.Action {
	case "status":
		script = `hbase version && hbase-daemon.sh status`
	case "deploy":
		script = `if command -v hbase >/dev/null 2>&1; then echo 'HBase already installed'; else wget https://archive.apache.org/dist/hbase/stable/hbase.tar.gz -O /tmp/hbase.tar.gz || apt install hbase; fi`
	case "table", "create-table":
		script = fmt.Sprintf(`hbase shell <<< "create '%s', '%s'"`, table, cf)
	case "drop-table":
		script = fmt.Sprintf(`hbase shell <<< "disable '%s' && drop '%s'"`, table, table)
	case "scan":
		script = fmt.Sprintf(`hbase shell <<< "scan '%s'"`, table)
	case "compact":
		script = fmt.Sprintf(`hbase shell <<< "compact '%s'"`, table)
	case "major-compact":
		script = fmt.Sprintf(`hbase shell <<< "major_compact '%s'"`, table)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_hbase_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBCassandra 处理 POST /api/v1/db/cassandra
func (s *Server) handleDBCassandra(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Command  string `json:"command"`
		Keyspace string `json:"keyspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "status":
		script = `nodetool status && cassandra -v`
	case "deploy":
		script = `if command -v cassandra >/dev/null 2>&1; then echo 'Cassandra already installed'; else apt install cassandra; fi`
	case "node-tool":
		cmd := req.Command
		if cmd == "" {
			cmd = "status"
		}
		script = fmt.Sprintf(`nodetool %s`, shellQuote(cmd))
	case "repair":
		script = `nodetool repair`
	case "compact":
		script = `nodetool compact`
	case "flush":
		script = `nodetool flush`
	case "snapshot":
		script = `nodetool snapshot`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_cassandra_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBCouchbase 处理 POST /api/v1/db/couchbase
func (s *Server) handleDBCouchbase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Bucket   string `json:"bucket"`
		Name     string `json:"name"`
		DB       string `json:"db"`
		Query    string `json:"query"`
		Index    string `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	bucket := firstNonEmptyNoSQL(req.Bucket, req.Name, req.DB)
	if bucket == "" {
		bucket = "default"
	}
	var script string
	switch req.Action {
	case "status":
		script = `curl -s http://localhost:8091/pools`
	case "deploy":
		script = `if command -v couchbase-server >/dev/null 2>&1; then echo 'Couchbase already installed'; else wget couchbase-server || docker run couchbase; fi`
	case "bucket":
		script = `couchbase-cli bucket-create`
	case "create-bucket":
		script = fmt.Sprintf(`couchbase-cli bucket-create --bucket %s --bucket-type couchbase`, shellQuote(bucket))
	case "drop-bucket":
		script = fmt.Sprintf(`couchbase-cli bucket-delete --bucket %s`, shellQuote(bucket))
	case "index":
		idx := req.Index
		if idx == "" {
			idx = "CREATE INDEX ..."
		}
		script = fmt.Sprintf(`cbq -e %s`, shellQuote(idx))
	case "query":
		q := req.Query
		if q == "" {
			q = "SELECT ..."
		}
		script = fmt.Sprintf(`cbq -e %s`, shellQuote(q))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_couchbase_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBScyllaDB 处理 POST /api/v1/db/scylladb
func (s *Server) handleDBScyllaDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Command  string `json:"command"`
		Keyspace string `json:"keyspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "status":
		script = `nodetool status && scylla --version`
	case "deploy":
		script = `if command -v scylla >/dev/null 2>&1; then echo 'Scylla already installed'; else apt install scylla; fi`
	case "node-tool":
		cmd := req.Command
		if cmd == "" {
			cmd = "status"
		}
		script = fmt.Sprintf(`nodetool %s`, shellQuote(cmd))
	case "repair":
		script = `nodetool repair`
	case "compact":
		script = `nodetool compact`
	case "flush":
		script = `nodetool flush`
	case "snapshot":
		script = `nodetool snapshot`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_scylladb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBCouchDB 处理 POST /api/v1/db/couchdb
func (s *Server) handleDBCouchDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Name     string `json:"name"`
		Bucket   string `json:"bucket"`
		Query    string `json:"query"`
		Design   string `json:"design"`
		View     string `json:"view"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	db := firstNonEmptyNoSQL(req.DB, req.Name, req.Bucket)
	if db == "" {
		db = "appdb"
	}
	design := firstNonEmptyNoSQL(req.Design, req.View)
	if design == "" {
		design = "default"
	}
	var script string
	switch req.Action {
	case "status":
		script = `curl -s http://localhost:5984`
	case "deploy":
		script = `if command -v couchdb >/dev/null 2>&1; then echo 'CouchDB already installed'; else apt install couchdb; fi`
	case "bucket", "create-db":
		script = fmt.Sprintf(`curl -s -X PUT http://localhost:5984/%s`, shellQuote(db))
	case "drop-db":
		script = fmt.Sprintf(`curl -s -X DELETE http://localhost:5984/%s`, shellQuote(db))
	case "view":
		script = fmt.Sprintf(`curl -s -X PUT http://localhost:5984/%s/_design/%s`, shellQuote(db), shellQuote(design))
	case "query":
		q := req.Query
		if q == "" {
			q = `{"selector":{}}`
		}
		script = fmt.Sprintf(`curl -s -X POST http://localhost:5984/%s/_find -H 'Content-Type: application/json' -d %s`, shellQuote(db), shellQuote(q))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_couchdb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerNoSQLExtRoutes 注册 NoSQL/半结构化数据库运维路由。
func (s *Server) registerNoSQLExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/hbase", s.handleDBHBase)
	mux.HandleFunc("/api/v1/db/cassandra", s.handleDBCassandra)
	mux.HandleFunc("/api/v1/db/couchbase", s.handleDBCouchbase)
	mux.HandleFunc("/api/v1/db/scylladb", s.handleDBScyllaDB)
	mux.HandleFunc("/api/v1/db/couchdb", s.handleDBCouchDB)
}
