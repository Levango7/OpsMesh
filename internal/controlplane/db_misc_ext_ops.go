// Package controlplane db_misc_ext_ops.go 实现补充型数据库/中间件运维支持。
//
// 覆盖既有专用文件之外的剩余品类：
//   - 向量：Weaviate（既有 vector_ops.go 只含 milvus/qdrant/pgvector）。
//   - KV：etcd（既有 servicediscovery_ops.go 只含 consul/eureka/nacos）。
//   - 缓存：Hazelcast / Ehcache（既有 cache_ops.go 只含 redis/memcached/ignite）。
//   - 搜索：Meilisearch / Typesense（既有 ELK 只含 Elasticsearch 栈）。
//   - 集成：Apache Camel（既有 middleware 面只含消息/队列品类）。
//
// 设计原则延续"查询走 API、执行走 shell"：POST 统一入口，type/action 走白名单，
// 用户输入经 shellQuote 包裹，5xx 走 writeInternalError 不回吐 err.Error()。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// 向量数据库：Weaviate
// ============================================================================

// handleWeaviate 处理 POST /api/v1/vector/weaviate
func (s *Server) handleWeaviate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/schema/data/query/nodes
		Port     int    `json:"port"`
		Class    string `json:"class"`  // schema class 名
		Module   string `json:"module"` // vectorizer 模块
		Query    string `json:"query"`  // GraphQL query
		Version  string `json:"version"`
		Replicas int    `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	port := req.Port
	if port == 0 {
		port = 8080
	}
	base := fmt.Sprintf("http://localhost:%d/v1", port)
	class := req.Class
	if class == "" {
		class = "Document"
	}
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== Weaviate Meta ===" && curl -s %s/meta | head -20 || echo "Weaviate not available"`, shellQuote(base))
	case "deploy":
		version := req.Version
		if version == "" {
			version = "1.25.5"
		}
		replicas := req.Replicas
		if replicas == 0 {
			replicas = 1
		}
		script = fmt.Sprintf(`if command -v weaviate >/dev/null 2>&1 || docker ps 2>/dev/null | grep -q weaviate; then echo 'Weaviate already deployed'; else docker run -d --name weaviate -p %d:%d -e QUERY_DEFAULTS_LIMIT=25 -e PERSISTENCE_DATA_PATH=/var/lib/weaviate -e AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true -e DEFAULT_VECTORIZER_MODULE=none cr.weaviate.io/semitechnologies/weaviate:%s && echo "Weaviate %s deployed (port %d, replicas %d)" && sleep 3 && curl -s http://localhost:%d/v1/meta | head -10; fi`, port, port, shellQuote(version), shellQuote(version), port, replicas, port)
	case "schema":
		script = fmt.Sprintf(`curl -s -X POST %s/schema -H "Content-Type: application/json" -d '{"class":"%s","vectorizer":"none"}' && echo "Schema class created: %s" || echo "Weaviate not available"`, shellQuote(base), shellQuote(class), shellQuote(class))
	case "data":
		script = fmt.Sprintf(`curl -s -X POST %s/objects -H "Content-Type: application/json" -d '{"class":"%s","id":"00000000-0000-0000-0000-000000000001","properties":{"title":"hello","content":"world"}}' && echo "Object created in %s" || echo "Weaviate not available"`, shellQuote(base), shellQuote(class), shellQuote(class))
	case "query":
		q := req.Query
		if q == "" {
			q = fmt.Sprintf("{ Get { %s { title } } }", opsSQLString(class))
		}
		script = fmt.Sprintf(`curl -s -X POST %s/graphql -H "Content-Type: application/json" -d '{"query":"%s"}' | head -30 || echo "Weaviate not available"`, shellQuote(base), shellQuote(q))
	case "nodes":
		script = fmt.Sprintf(`echo "=== Weaviate Nodes/Shards ===" && curl -s %s/nodes | head -30 || echo "Weaviate not available"`, shellQuote(base))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "weaviate_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// KV 存储：etcd
// ============================================================================

// handleEtcd 处理 POST /api/v1/kv/etcd
func (s *Server) handleEtcd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/put/get/delete/snapshot/health/defrag
		Endpoint string `json:"endpoint"`
		Key      string `json:"key"`
		Value    string `json:"value"`
		Prefix   string `json:"prefix"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	endpoint := req.Endpoint
	if endpoint == "" {
		endpoint = "localhost:2379"
	}
	key := req.Key
	if key == "" {
		key = "/opsmesh/example"
	}
	prefix := req.Prefix
	if prefix == "" {
		prefix = "/opsmesh"
	}
	snapPath := req.Path
	if snapPath == "" {
		snapPath = "/var/backups/etcd/etcd-snapshot.db"
	}
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== etcd Endpoint Status ===" && etcdctl --endpoints=%s endpoint status -w table 2>&1 || echo "etcd not available"`, shellQuote(endpoint))
	case "deploy":
		script = `if command -v etcdctl >/dev/null 2>&1; then echo 'etcd already installed'; elif [ -f /opt/etcd/etcd ] && [ -f /opt/etcd/etcdctl ]; then echo 'etcd binary already present'; else apt-get install -y etcd-server etcd-client 2>/dev/null || { mkdir -p /opt/etcd && wget -q https://github.com/etcd-io/etcd/releases/download/v3.5.15/etcd-v3.5.15-linux-amd64.tar.gz -O /tmp/etcd.tgz && tar -xzf /tmp/etcd.tgz -C /tmp/ && cp /tmp/etcd-v3.5.15-linux-amd64/etcd* /opt/etcd/ && echo "etcd 3.5.15 staged to /opt/etcd"; }; fi`
	case "put":
		if req.Value == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "value is required"})
			return
		}
		script = fmt.Sprintf(`etcdctl --endpoints=%s put %s %s && echo "Key set: %s"`, shellQuote(endpoint), shellQuote(key), shellQuote(req.Value), shellQuote(key))
	case "get":
		script = fmt.Sprintf(`etcdctl --endpoints=%s get %s -w fields 2>&1 || echo "Key not found: %s"`, shellQuote(endpoint), shellQuote(key), shellQuote(key))
	case "delete":
		script = fmt.Sprintf(`etcdctl --endpoints=%s del %s && echo "Key deleted: %s"`, shellQuote(endpoint), shellQuote(key), shellQuote(key))
	case "snapshot":
		script = fmt.Sprintf(`mkdir -p $(dirname %s) && etcdctl --endpoints=%s snapshot save %s && echo "Snapshot saved: %s"`, shellQuote(snapPath), shellQuote(endpoint), shellQuote(snapPath), shellQuote(snapPath))
	case "health":
		script = fmt.Sprintf(`echo "=== etcd Cluster Health ===" && etcdctl --endpoints=%s endpoint health -w table 2>&1 && echo "=== Members ===" && etcdctl --endpoints=%s member list -w table 2>&1`, shellQuote(endpoint), shellQuote(endpoint))
	case "defrag":
		script = fmt.Sprintf(`echo "Defragmenting etcd..." && etcdctl --endpoints=%s defrag && echo "Defrag done"`, shellQuote(endpoint))
	case "list-prefix":
		script = fmt.Sprintf(`echo "=== Keys under %s ===" && etcdctl --endpoints=%s get %s --prefix -w simple 2>&1 | head -50`, shellQuote(prefix), shellQuote(endpoint), shellQuote(prefix))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "etcd_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 缓存扩展：Hazelcast
// ============================================================================

// handleHazelcast 处理 POST /api/v1/cache/hazelcast
func (s *Server) handleHazelcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/cluster/map/put/get
		Port     int    `json:"port"`
		Map      string `json:"map"`
		Key      string `json:"key"`
		Value    string `json:"value"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	port := req.Port
	if port == 0 {
		port = 5701
	}
	mapName := req.Map
	if mapName == "" {
		mapName = "opsmesh-cache"
	}
	base := fmt.Sprintf("http://localhost:%d/hazelcast/rest", port)
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== Hazelcast Cluster ===" && curl -s %s/cluster | head -20 || echo "Hazelcast not available"`, shellQuote(base))
	case "deploy":
		version := req.Version
		if version == "" {
			version = "5.5.0"
		}
		script = fmt.Sprintf(`if docker ps 2>/dev/null | grep -q hazelcast; then echo 'Hazelcast already running'; else docker run -d --name hazelcast -p %d:%d hazelcast/hazelcast:%s && echo "Hazelcast %s deployed (port %d)" && sleep 5 && curl -s http://localhost:%d/hazelcast/rest/cluster | head -10; fi`, port, port, shellQuote(version), shellQuote(version), port, port)
	case "cluster":
		script = fmt.Sprintf(`echo "=== Cluster State ===" && curl -s %s/cluster-state 2>&1 && echo "=== Members ===" && curl -s %s/cluster 2>&1 || echo "Hazelcast not available"`, shellQuote(base), shellQuote(base))
	case "map":
		script = fmt.Sprintf(`echo "=== Map Sizes ===" && curl -s %s/maps 2>&1 || echo "Hazelcast not available"`, shellQuote(base))
	case "put":
		if req.Value == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "value is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST -d %s %s/maps/%s/%s && echo "Key set" || echo "Hazelcast not available"`, shellQuote(req.Value), shellQuote(base), shellQuote(mapName), shellQuote(req.Key))
	case "get":
		script = fmt.Sprintf(`curl -s %s/maps/%s/%s 2>&1 && echo "" || echo "Key not found or Hazelcast not available"`, shellQuote(base), shellQuote(mapName), shellQuote(req.Key))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "hazelcast_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 缓存扩展：Ehcache
// ============================================================================

// handleEhcache 处理 POST /api/v1/cache/ehcache
func (s *Server) handleEhcache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/config/check
		App      string `json:"app"`    // JVM 应用进程名
		Path     string `json:"path"`   // ehcache 配置目录
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	app := req.App
	if app == "" {
		app = "java"
	}
	cfgPath := req.Path
	if cfgPath == "" {
		cfgPath = "/opt/app/ehcache.xml"
	}
	if err := shellquote.ValidatePath(cfgPath); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== Ehcache JVM Processes ===" && (ps aux | grep -i %s | grep -v grep | head -10 || echo "No JVM process") && echo "=== Heap ===" && (jstat -gc $(pgrep -f %s | head -1) 500 1 2>/dev/null | tail -1 || echo "jstat not available")`, shellQuote(app), shellQuote(app))
	case "deploy":
		script = fmt.Sprintf(`mkdir -p $(dirname %s) && if [ -f %s ]; then echo "ehcache.xml already present"; else cat > %s <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<ehcache xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:noNamespaceSchemaLocation="http://www.ehcache.org/ehcache.xsd">
  <diskStore path="java.io.tmpdir"/>
  <defaultCache maxEntriesLocalHeap="10000" eternal="false"
                timeToIdleSeconds="120" timeToLiveSeconds="1200"
                maxEntriesLocalDisk="10000000" diskExpiryThreadIntervalSeconds="120"
                memoryStoreEvictionPolicy="LRU">
    <persistence strategy="localTempSwap"/>
  </defaultCache>
  <cache name="opsmeshCache" maxEntriesLocalHeap="10000" maxEntriesLocalDisk="1000"
         eternal="false" diskSpoolBufferSizeMB="20" timeToIdleSeconds="300"
         timeToLiveSeconds="600" memoryStoreEvictionPolicy="LFU"
         transactionalMode="off">
    <persistence strategy="localTempSwap"/>
  </cache>
</ehcache>
XML
echo "ehcache.xml written to %s"; fi`, shellQuote(cfgPath), shellQuote(cfgPath), shellQuote(cfgPath), shellQuote(cfgPath))
	case "config":
		script = fmt.Sprintf(`if [ -f %s ]; then echo "=== ehcache.xml ===" && head -40 %s; else echo "ehcache.xml not found at %s"; fi`, shellQuote(cfgPath), shellQuote(cfgPath), shellQuote(cfgPath))
	case "check":
		script = fmt.Sprintf(`echo "=== XML Dry Parse ===" && (python3 -c "import xml.dom.minidom; xml.dom.minidom.parse('%s'); print('XML OK')" 2>&1 || xmllint --noout %s 2>&1 || echo "no XML parser available") && echo "=== Cache Metrics (JMX) ===" && (jcmd $(pgrep -f %s | head -1) VM.version 2>/dev/null | head -3 || echo "jcmd not available")`, shellQuote(cfgPath), shellQuote(cfgPath), shellQuote(app))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ehcache_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 搜索引擎：Meilisearch
// ============================================================================

// handleMeilisearch 处理 POST /api/v1/search/meilisearch
func (s *Server) handleMeilisearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // status/deploy/index/document/search/keys/stats
		Port      int    `json:"port"`
		MasterKey string `json:"masterKey"`
		Index     string `json:"index"`
		Query     string `json:"query"`
		DocID     string `json:"docId"`
		Version   string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	port := req.Port
	if port == 0 {
		port = 7700
	}
	masterKey := req.MasterKey
	if masterKey == "" {
		masterKey = "ChangeMe123!"
	}
	index := req.Index
	if index == "" {
		index = "docs"
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== Meilisearch Health ===" && curl -s %s/health && echo && curl -s %s/version && echo || echo "Meilisearch not available"`, shellQuote(base), shellQuote(base))
	case "deploy":
		version := req.Version
		if version == "" {
			version = "1.10"
		}
		script = fmt.Sprintf(`if command -v meilisearch >/dev/null 2>&1 || docker ps 2>/dev/null | grep -q meilisearch; then echo 'Meilisearch already deployed'; else docker run -d --name meilisearch -p %d:%d -e MEILI_MASTER_KEY=%s getmeili/meilisearch:v%s && echo "Meilisearch v%s deployed (port %d)" && sleep 3 && curl -s %s/health; fi`, port, port, shellQuote(masterKey), shellQuote(version), shellQuote(version), port, shellQuote(base))
	case "index":
		script = fmt.Sprintf(`curl -s -X POST %s/indexes -H "Authorization: Bearer %s" -H "Content-Type: application/json" -d '{"uid":"%s","primaryKey":"id"}' && echo "Index created: %s" || echo "Meilisearch not available"`, shellQuote(base), shellQuote(masterKey), shellQuote(index), shellQuote(index))
	case "document":
		docID := req.DocID
		if docID == "" {
			docID = "1"
		}
		script = fmt.Sprintf(`curl -s -X POST %s/indexes/%s/documents -H "Authorization: Bearer %s" -H "Content-Type: application/json" -d '{"id":%s,"title":"hello","content":"world"}' && echo "Document added: %s" || echo "Meilisearch not available"`, shellQuote(base), shellQuote(index), shellQuote(masterKey), shellQuote(docID), shellQuote(docID))
	case "search":
		q := req.Query
		if q == "" {
			q = "hello"
		}
		script = fmt.Sprintf(`curl -s -X POST %s/indexes/%s/search -H "Authorization: Bearer %s" -H "Content-Type: application/json" -d '{"q":"%s","limit":10}' | head -30 || echo "Meilisearch not available"`, shellQuote(base), shellQuote(index), shellQuote(masterKey), shellQuote(q))
	case "keys":
		script = fmt.Sprintf(`curl -s %s/keys -H "Authorization: Bearer %s" | head -20 || echo "Meilisearch not available"`, shellQuote(base), shellQuote(masterKey))
	case "stats":
		script = fmt.Sprintf(`echo "=== Stats ===" && curl -s %s/stats -H "Authorization: Bearer %s" | head -30 || echo "Meilisearch not available"`, shellQuote(base), shellQuote(masterKey))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "meilisearch_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 搜索引擎：Typesense
// ============================================================================

// handleTypesense 处理 POST /api/v1/search/typesense
func (s *Server) handleTypesense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"` // status/deploy/collection/document/search/keys/health
		Port       int    `json:"port"`
		APIKey     string `json:"apiKey"`
		Collection string `json:"collection"`
		Query      string `json:"query"`
		DocID      string `json:"docId"`
		Version    string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	port := req.Port
	if port == 0 {
		port = 8108
	}
	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = "ChangeMe123!"
	}
	collection := req.Collection
	if collection == "" {
		collection = "docs"
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	var script string
	switch req.Action {
	case "status", "health":
		script = fmt.Sprintf(`echo "=== Typesense Health ===" && curl -s %s/health && echo && curl -s %s/debug 2>&1 | head -3 || echo "Typesense not available"`, shellQuote(base), shellQuote(base))
	case "deploy":
		version := req.Version
		if version == "" {
			version = "27.1"
		}
		script = fmt.Sprintf(`if command -v typesense-server >/dev/null 2>&1 || docker ps 2>/dev/null | grep -q typesense; then echo 'Typesense already deployed'; else docker run -d --name typesense -p %d:%d -v /var/lib/typesense:/data typesense/typesense:%s --data-dir /data --api-key=%s --listen-port %d && echo "Typesense %s deployed (port %d)" && sleep 3 && curl -s %s/health; fi`, port, port, shellQuote(version), shellQuote(apiKey), port, shellQuote(version), port, shellQuote(base))
	case "collection":
		script = fmt.Sprintf(`curl -s -X POST %s/collections -H "X-TYPESENSE-API-KEY: %s" -H "Content-Type: application/json" -d '{"name":"%s","fields":[{"name":"id","type":"string"},{"name":"title","type":"string"},{"name":"content","type":"string"}],"default_sorting_field":"id"}' && echo "Collection created: %s" || echo "Typesense not available"`, shellQuote(base), shellQuote(apiKey), shellQuote(collection), shellQuote(collection))
	case "document":
		docID := req.DocID
		if docID == "" {
			docID = "1"
		}
		script = fmt.Sprintf(`curl -s -X POST %s/collections/%s/documents -H "X-TYPESENSE-API-KEY: %s" -H "Content-Type: application/json" -d '{"id":"%s","title":"hello","content":"world"}' && echo "Document added: %s" || echo "Typesense not available"`, shellQuote(base), shellQuote(collection), shellQuote(apiKey), shellQuote(docID), shellQuote(docID))
	case "search":
		q := req.Query
		if q == "" {
			q = "*"
		}
		script = fmt.Sprintf(`curl -s -X GET %s/collections/%s/documents?q=%s\&query_by=title,content\&limit=10 -H "X-TYPESENSE-API-KEY: %s" | head -30 || echo "Typesense not available"`, shellQuote(base), shellQuote(collection), shellQuote(q), shellQuote(apiKey))
	case "keys":
		script = fmt.Sprintf(`curl -s %s/keys -H "X-TYPESENSE-API-KEY: %s" | head -20 || echo "Typesense not available"`, shellQuote(base), shellQuote(apiKey))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "typesense_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 集成框架：Apache Camel
// ============================================================================

// handleApacheCamel 处理 POST /api/v1/middleware/apache/camel
func (s *Server) handleApacheCamel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/route/list-routes/start-route/stop-route/trace
		Home     string `json:"home"`
		Route    string `json:"route"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	home := req.Home
	if home == "" {
		home = "/opt/camel"
	}
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== Camel Version ===" && (du -f %s/bin 2>/dev/null | head -1 || echo "Camel not found at %s") && echo "=== Camel Processes ===" && (ps aux | grep -i camel | grep -v grep | head -5 || echo "No Camel process")`, shellQuote(home), shellQuote(home))
	case "deploy":
		version := req.Version
		if version == "" {
			version = "4.6.0"
		}
		script = fmt.Sprintf(`if [ -f %s/bin/camel ]; then echo "Apache Camel already installed"; else mkdir -p %s && wget -q https://archive.apache.org/dist/camel/apache-camel/4.6.0/apache-camel-%s/apache-camel-%s.tar.gz -O /tmp/camel.tgz && tar -xzf /tmp/camel.tgz -C %s/ --strip-components=1 2>/dev/null || { echo "下载失败，可改从镜像站 apache.dataguru.cn 或 CDN 获取"; exit 1; }; export CAMEL_HOME=%s && chmod +x %s/bin/camel && echo "Apache Camel %s installed at %s" && %s/bin/camel --version; fi`, shellQuote(home), shellQuote(home), shellQuote(version), shellQuote(version), shellQuote(home), shellQuote(home), shellQuote(home), shellQuote(version), shellQuote(home), shellQuote(home))
	case "route":
		if req.Route == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "route is required"})
			return
		}
		script = fmt.Sprintf(`export CAMEL_HOME=%s && echo "=== Route: %s ===" && %s/bin/camel route show %s 2>/dev/null || echo "route show 需要 JBang/应用上下文"`, shellQuote(home), shellQuote(req.Route), shellQuote(home), shellQuote(req.Route))
	case "list-routes":
		script = fmt.Sprintf(`export CAMEL_HOME=%s && %s/bin/camel route list 2>&1 | head -30 || echo "route list 需要运行中的 Camel 上下文"`, shellQuote(home), shellQuote(home))
	case "start-route", "stop-route":
		if req.Route == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "route is required"})
			return
		}
		verb := "start"
		if req.Action == "stop-route" {
			verb = "stop"
		}
		script = fmt.Sprintf(`export CAMEL_HOME=%s && %s/bin/camel route %s %s 2>&1 || echo "route %s 需要运行中的 Camel 上下文"`, shellQuote(home), shellQuote(home), shellQuote(verb), shellQuote(req.Route), shellQuote(verb))
	case "trace":
		script = fmt.Sprintf(`export CAMEL_HOME=%s && echo "=== JBang Camel Trace ===" && (curl -s http://localhost:8080/q/metric 2>/dev/null | head -10 || echo "Camel actuator/metrics 未暴露（需 -Dcamel.metrics.enabled=true）")`, shellQuote(home))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_camel_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerMiscExtRoutes 注册补充型数据库/中间件运维路由。
func (s *Server) registerMiscExtRoutes(mux *http.ServeMux) {
	// 向量补充：Weaviate
	mux.HandleFunc("/api/v1/vector/weaviate", s.handleWeaviate)
	// KV：etcd
	mux.HandleFunc("/api/v1/kv/etcd", s.handleEtcd)
	// 缓存补充：Hazelcast / Ehcache
	mux.HandleFunc("/api/v1/cache/hazelcast", s.handleHazelcast)
	mux.HandleFunc("/api/v1/cache/ehcache", s.handleEhcache)
	// 搜索引擎：Meilisearch / Typesense
	mux.HandleFunc("/api/v1/search/meilisearch", s.handleMeilisearch)
	mux.HandleFunc("/api/v1/search/typesense", s.handleTypesense)
	// 集成框架：Apache Camel
	mux.HandleFunc("/api/v1/middleware/apache/camel", s.handleApacheCamel)
}
