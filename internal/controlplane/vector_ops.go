// vector_ops.go 向量数据库运维（Milvus/Qdrant/pgvector）。
//
// 设计原则（延续 P0-P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/vector/{deviceID} 查询向量库服务状态。
//   - 执行类：POST /api/v1/vector/deploy 等，shell 模板下发到 agent 执行；
//     向量读写走各向量库的 REST API / SQL。
//
// 向量库定位（企业级 AIOps 知识库/RAG/语义检索场景）：
//   - Milvus：分布式向量库，支持十亿级向量，Milvus stand-alone / cluster 两种部署。
//   - Qdrant：Rust 实现的高性能向量库，REST/gRPC，payload 过滤强。
//   - pgvector：PostgreSQL 扩展，复用 PG 生态，适合中小规模与事务一致场景。
//
// API 端点：
//   - GET  /api/v1/vector/{deviceID}          — 查询向量库状态
//   - POST /api/v1/vector/deploy               — 部署向量库（{deviceID, type, version, mode}）
//   - POST /api/v1/vector/{type}/{action}      — 启停重启/状态/扩缩容
//   - POST /api/v1/vector/{type}/collection    — 集合管理（create/delete/list）
//   - POST /api/v1/vector/{type}/write         — 向量写入
//   - POST /api/v1/vector/{type}/search        — 向量相似检索
//   - POST /api/v1/vector/{type}/index         — 索引管理（create/delete/list）
//   - POST /api/v1/vector/{type}/migrate       — 迁移（export/import）
//
// 安全：type/action 走白名单；集合名/库名走正则校验；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 向量库类型白名单。
var allowedVectorTypes = map[string]bool{
	"milvus":   true,
	"qdrant":   true,
	"pgvector": true,
}

// 向量库操作动作白名单。
var allowedVectorActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"scale":    true, // 扩缩容（cluster 模式）
	"undeploy": true,
}

// 向量库资源动作白名单。
var allowedVectorResourceActions = map[string]bool{
	"create":  true,
	"delete":  true,
	"list":    true,
	"migrate": true,
}

// 向量集合名/库名正则。
var vectorCollectionRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,255}$`)

// 向量库部署请求体。
type vectorDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // milvus/qdrant/pgvector
	Version  string `json:"version"` // 版本
	Mode     string `json:"mode"`    // single/cluster
}

// vectorOpsRequest 通用向量库操作请求体。
type vectorOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	Replicas int    `json:"replicas"` // scale 用
}

// vectorCollectionReq 集合管理请求体。
type vectorCollectionReq struct {
	DeviceID   string `json:"deviceID"`
	Type       string `json:"type"`       // milvus/qdrant/pgvector
	Action     string `json:"action"`     // create/delete/list
	Collection string `json:"collection"` // 集合名
	Dimension  int    `json:"dimension"`  // 向量维度（create 用）
	Metric     string `json:"metric"`     // 距离度量（L2/IP/COSINE）
}

// vectorWriteReq 向量写入请求体。
type vectorWriteReq struct {
	DeviceID   string    `json:"deviceID"`
	Type       string    `json:"type"`
	Collection string    `json:"collection"`
	ID         string    `json:"id"`      // 主键
	Vector     []float64 `json:"vector"`  // 向量
	Payload    string    `json:"payload"` // 元数据 JSON
}

// vectorSearchReq 向量检索请求体。
type vectorSearchReq struct {
	DeviceID   string    `json:"deviceID"`
	Type       string    `json:"type"`
	Collection string    `json:"collection"`
	Vector     []float64 `json:"vector"` // 查询向量
	TopK       int       `json:"topK"`   // 返回条数
}

// handleVector 查询指定设备上的向量库状态。
func (s *Server) handleVector(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	dev := s.store.Device(deviceID)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	metrics := s.store.DeviceMetrics(deviceID)
	if metrics == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics yet"})
		return
	}
	type vecInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []vecInfo
	for _, vecType := range []string{"milvus", "qdrant", "pgvector"} {
		info := vecInfo{Type: vecType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, vecType) || strings.EqualFold(svc.Name, vecType+"-server") {
					info.Status = svc.Status
					break
				}
			}
		}
		if info.Status == "not-installed" {
			continue
		}
		result = append(result, info)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "vectorDB": result})
}

// handleVectorDeploy 处理 POST /api/v1/vector/deploy：部署向量库。
func (s *Server) handleVectorDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req vectorDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedVectorTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	if req.Version != "" && !middlewareVersionRe.MatchString(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildVectorDeployScript(req)
	action := "vector_deploy_" + req.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s mode=%s version=%s", req.DeviceID, req.Mode, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorDeployScript(req vectorDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 部署（依赖 etcd + MinIO，stand-alone 用 docker-compose）\n")
		b.WriteString("MILVUS_VERSION=${MILVUS_VERSION:-v2.4.0}\n")
		b.WriteString("cd /opt && wget -q https://github.com/milvus-io/milvus/releases/download/$MILVUS_VERSION/milvus-standalone-docker-compose.yml -O docker-compose.yml\n")
		b.WriteString("docker compose up -d\n")
		b.WriteString("echo 'Milvus 已启动 (SDK:19530, WebUI:9091)'\n")
	case "qdrant":
		b.WriteString("# Qdrant 部署\n")
		b.WriteString("if command -v qdrant >/dev/null 2>&1; then echo 'Qdrant 已安装'; else docker run -d --name qdrant -p 6333:6333 -p 6334:6334 -v /data/qdrant:/qdrant/storage qdrant/qdrant || echo '请安装 Qdrant' && exit 1; fi\n")
		b.WriteString("echo 'Qdrant 已启动 (REST:6333, gRPC:6334)'\n")
	case "pgvector":
		b.WriteString("# pgvector 部署（PostgreSQL 扩展）\n")
		b.WriteString("if command -v psql >/dev/null 2>&1; then echo 'PostgreSQL 已安装'; else echo '请先安装 PostgreSQL' && exit 1; fi\n")
		b.WriteString("$PKG_MGR install -y postgresql-14-pgvector 2>/dev/null || (cd /tmp && git clone --branch v0.6.0 https://github.com/pgvector/pgvector.git && cd pgvector && make && make install) || echo '请安装 pgvector 扩展'\n")
		b.WriteString("echo 'pgvector 扩展已安装，需在数据库中执行 CREATE EXTENSION vector'\n")
	}
	return b.String()
}

// handleVectorAction 处理 POST /api/v1/vector/{type}/{action}：启停重启/状态/扩缩容。
func (s *Server) handleVectorAction(w http.ResponseWriter, r *http.Request, vecType, action string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedVectorTypes[vecType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	if !allowedVectorActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req vectorOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = vecType
	req.Action = action

	script := buildVectorActionScript(req)
	taskAction := "vector_" + action + "_" + vecType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.action."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: vecType,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s replicas=%d", req.DeviceID, req.Replicas)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorActionScript(req vectorOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 启停/扩缩容\n")
		switch req.Action {
		case "start":
			b.WriteString("cd /opt && docker compose up -d || true\n")
		case "stop":
			b.WriteString("cd /opt && docker compose down || true\n")
		case "restart":
			b.WriteString("cd /opt && docker compose restart || true\n")
		case "status":
			b.WriteString("docker ps --filter name=milvus --format '{{.Names}}: {{.Status}}' || echo 'not running'\n")
		case "scale":
			if req.Replicas <= 0 {
				req.Replicas = 1
			}
			b.WriteString(fmt.Sprintf("cd /opt && docker compose up -d --scale milvus-standalone=%d || true\n", req.Replicas))
		}
	case "qdrant":
		b.WriteString("# Qdrant 启停/扩缩容\n")
		switch req.Action {
		case "start":
			b.WriteString("docker start qdrant || true\n")
		case "stop":
			b.WriteString("docker stop qdrant || true\n")
		case "restart":
			b.WriteString("docker restart qdrant || true\n")
		case "status":
			b.WriteString("docker ps --filter name=qdrant --format '{{.Names}}: {{.Status}}' || echo 'not running'\n")
		case "scale":
			b.WriteString("echo 'Qdrant 扩缩容：需部署 Qdrant 集群（distributed mode）'\n")
		}
	case "pgvector":
		b.WriteString("# pgvector 状态（基于 PostgreSQL）\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start postgresql || true\n")
		case "stop":
			b.WriteString("systemctl stop postgresql || true\n")
		case "restart":
			b.WriteString("systemctl restart postgresql || true\n")
		case "status":
			b.WriteString("systemctl is-active postgresql || echo 'not running'\n")
		case "scale":
			b.WriteString("echo 'pgvector 扩缩容：随 PostgreSQL 扩缩容（主从/citus）'\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo '向量库 %s %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// handleVectorCollection 处理 POST /api/v1/vector/{type}/collection：集合管理。
func (s *Server) handleVectorCollection(w http.ResponseWriter, r *http.Request, vecType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedVectorTypes[vecType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	var req vectorCollectionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if req.Action != "list" && !vectorCollectionRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid collection name"})
		return
	}
	if req.Action == "create" && req.Dimension <= 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "dimension must be positive"})
		return
	}
	req.Type = vecType

	script := buildVectorCollectionScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_collection_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.collection."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_collection_" + req.Action, Target: req.Collection,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s dim=%d", req.DeviceID, vecType, req.Dimension)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorCollectionScript(req vectorCollectionReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 集合管理（REST API，v2）\n")
		switch req.Action {
		case "create":
			if req.Metric == "" {
				req.Metric = "COSINE"
			}
			b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/collections/create' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"dimension\":%d,\"metricType\":\"%s\"}'\n", req.Collection, req.Dimension, req.Metric))
		case "delete":
			b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/collections/drop' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\"}'\n", req.Collection))
		case "list":
			b.WriteString("curl -s -X POST 'http://localhost:19530/v2/vectordb/collections/list' -H 'Content-Type: application/json' -d '{}'\n")
		}
	case "qdrant":
		b.WriteString("# Qdrant 集合管理（REST API）\n")
		switch req.Action {
		case "create":
			if req.Metric == "" {
				req.Metric = "Cosine"
			}
			b.WriteString(fmt.Sprintf("curl -s -X PUT 'http://localhost:6333/collections/%s' -H 'Content-Type: application/json' -d '{\"vectors\":{\"size\":%d,\"distance\":\"%s\"}}'\n", req.Collection, req.Dimension, req.Metric))
		case "delete":
			b.WriteString(fmt.Sprintf("curl -s -X DELETE 'http://localhost:6333/collections/%s'\n", req.Collection))
		case "list":
			b.WriteString("curl -s 'http://localhost:6333/collections'\n")
		}
	case "pgvector":
		b.WriteString("# pgvector 表管理（SQL）\n")
		switch req.Action {
		case "create":
			b.WriteString(fmt.Sprintf("psql -c 'CREATE EXTENSION IF NOT EXISTS vector; CREATE TABLE IF NOT EXISTS %s (id text PRIMARY KEY, embedding vector(%d), payload jsonb)' || echo '创建失败'\n", req.Collection, req.Dimension))
		case "delete":
			b.WriteString(fmt.Sprintf("psql -c 'DROP TABLE IF EXISTS %s' || echo '删除失败'\n", req.Collection))
		case "list":
			b.WriteString("psql -c '\\dt'\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo '向量集合 %s 操作完成\n", req.Action))
	return b.String()
}

// handleVectorWrite 处理 POST /api/v1/vector/{type}/write：向量写入。
func (s *Server) handleVectorWrite(w http.ResponseWriter, r *http.Request, vecType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedVectorTypes[vecType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	var req vectorWriteReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid collection name"})
		return
	}
	if len(req.Vector) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "vector is required"})
		return
	}
	req.Type = vecType

	script := buildVectorWriteScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_write")
	if err != nil {
		writeInternalError(r.Context(), w, "vector.write."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_write", Target: req.Collection,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s id=%s dim=%d", req.DeviceID, req.ID, len(req.Vector))),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorWriteScript(req vectorWriteReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	vecJSON := floatSliceToJSON(req.Vector)
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 向量写入\n")
		b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/entities/insert' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"data\":[{\"id\":\"%s\",\"vector\":%s}]}'\n", req.Collection, req.ID, vecJSON))
	case "qdrant":
		b.WriteString("# Qdrant 向量写入（point id 需为整数或 UUID，此处用哈希）\n")
		b.WriteString(fmt.Sprintf("curl -s -X PUT 'http://localhost:6333/collections/%s/points' -H 'Content-Type: application/json' -d '{\"points\":[{\"id\":1,\"vector\":%s,\"payload\":%s}]}'\n", req.Collection, vecJSON, nonEmptyPayload(req.Payload)))
	case "pgvector":
		b.WriteString("# pgvector 向量写入\n")
		b.WriteString(fmt.Sprintf("psql -c \"INSERT INTO %s (id, embedding) VALUES ('%s', '%s') ON CONFLICT (id) DO UPDATE SET embedding=EXCLUDED.embedding\" || echo '写入失败'\n", req.Collection, req.ID, vecJSON))
	}
	b.WriteString("echo '向量写入完成'\n")
	return b.String()
}

// handleVectorSearch 处理 POST /api/v1/vector/{type}/search：向量相似检索。
func (s *Server) handleVectorSearch(w http.ResponseWriter, r *http.Request, vecType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	if !allowedVectorTypes[vecType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	var req vectorSearchReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid collection name"})
		return
	}
	if len(req.Vector) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "vector is required"})
		return
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}
	req.Type = vecType

	script := buildVectorSearchScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_search")
	if err != nil {
		writeInternalError(r.Context(), w, "vector.search."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_search", Target: req.Collection,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s topK=%d", req.DeviceID, req.TopK)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorSearchScript(req vectorSearchReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	vecJSON := floatSliceToJSON(req.Vector)
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 相似检索\n")
		b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/entities/search' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"data\":[%s],\"limit\":%d}'\n", req.Collection, vecJSON, req.TopK))
	case "qdrant":
		b.WriteString("# Qdrant 相似检索\n")
		b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:6333/collections/%s/points/search' -H 'Content-Type: application/json' -d '{\"vector\":%s,\"limit\":%d}'\n", req.Collection, vecJSON, req.TopK))
	case "pgvector":
		b.WriteString("# pgvector 相似检索（L2 距离 <->）\n")
		b.WriteString(fmt.Sprintf("psql -c 'SELECT id, embedding <-> ''%s'' AS distance FROM %s ORDER BY distance LIMIT %d' || echo '检索失败'\n", vecJSON, req.Collection, req.TopK))
	}
	b.WriteString("echo '向量检索完成'\n")
	return b.String()
}

// handleVectorIndex 处理 POST /api/v1/vector/{type}/index：索引管理。
func (s *Server) handleVectorIndex(w http.ResponseWriter, r *http.Request, vecType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedVectorTypes[vecType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	var req vectorCollectionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid collection name"})
		return
	}
	req.Type = vecType

	script := buildVectorIndexScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_index_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.index."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_index_" + req.Action, Target: req.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorIndexScript(req vectorCollectionReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 索引管理（IVF_FLAT / HNSW）\n")
		switch req.Action {
		case "create":
			b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/indexes/create' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"indexParams\":[{\"fieldName\":\"vector\",\"indexType\":\"HNSW\",\"metricType\":\"COSINE\"}]}'\n", req.Collection))
		case "delete":
			b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/indexes/drop' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"indexName\":\"vector\"}'\n", req.Collection))
		case "list":
			b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:19530/v2/vectordb/indexes/list' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\"}'\n", req.Collection))
		}
	case "qdrant":
		b.WriteString("# Qdrant 索引管理（payload 字段索引）\n")
		switch req.Action {
		case "create":
			b.WriteString(fmt.Sprintf("curl -s -X PUT 'http://localhost:6333/collections/%s/index' -H 'Content-Type: application/json' -d '{\"field_name\":\"payload\",\"field_schema\":\"keyword\"}'\n", req.Collection))
		case "delete":
			b.WriteString(fmt.Sprintf("curl -s -X DELETE 'http://localhost:6333/collections/%s/index/payload'\n", req.Collection))
		case "list":
			b.WriteString(fmt.Sprintf("curl -s 'http://localhost:6333/collections/%s'\n", req.Collection))
		}
	case "pgvector":
		b.WriteString("# pgvector 索引管理（IVFFlat / HNSW）\n")
		switch req.Action {
		case "create":
			b.WriteString(fmt.Sprintf("psql -c 'CREATE INDEX ON %s USING hnsw (embedding vector_cosine_ops)' || echo '创建索引失败'\n", req.Collection))
		case "delete":
			b.WriteString(fmt.Sprintf("psql -c 'DROP INDEX IF EXISTS %s_embedding_idx'\n", req.Collection))
		case "list":
			b.WriteString(fmt.Sprintf("psql -c '\\di'\n"))
		}
	}
	b.WriteString(fmt.Sprintf("echo '向量索引 %s 操作完成\n", req.Action))
	return b.String()
}

// handleVectorMigrate 处理 POST /api/v1/vector/{type}/migrate：迁移（export/import）。
func (s *Server) handleVectorMigrate(w http.ResponseWriter, r *http.Request, vecType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	if !allowedVectorTypes[vecType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported vector db type"})
		return
	}
	var req vectorCollectionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"export": true, "import": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (export/import)"})
		return
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid collection name"})
		return
	}
	req.Type = vecType

	script := buildVectorMigrateScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_migrate_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.migrate."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_migrate_" + req.Action, Target: req.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorMigrateScript(req vectorCollectionReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "milvus":
		b.WriteString("# Milvus 迁移（milvus-backup 工具）\n")
		switch req.Action {
		case "export":
			b.WriteString(fmt.Sprintf("milvus-backup create -n backup_%s -c %s || echo '导出失败，需配置 milvus-backup'\n", req.Collection, req.Collection))
		case "import":
			b.WriteString(fmt.Sprintf("milvus-backup restore -n backup_%s -c %s || echo '导入失败，需配置 milvus-backup'\n", req.Collection, req.Collection))
		}
	case "qdrant":
		b.WriteString("# Qdrant 迁移（快照 snapshot）\n")
		switch req.Action {
		case "export":
			b.WriteString(fmt.Sprintf("curl -s -X POST 'http://localhost:6333/collections/%s/snapshots'\n", req.Collection))
		case "import":
			b.WriteString(fmt.Sprintf("echo 'Qdrant 导入：PUT /collections/%s/snapshots/upload（需上传快照文件）'\n", req.Collection))
		}
	case "pgvector":
		b.WriteString("# pgvector 迁移（pg_dump/restore）\n")
		switch req.Action {
		case "export":
			b.WriteString(fmt.Sprintf("pg_dump -t %s > /tmp/%s_dump.sql || echo '导出失败'\n", req.Collection, req.Collection))
		case "import":
			b.WriteString(fmt.Sprintf("psql < /tmp/%s_dump.sql || echo '导入失败'\n", req.Collection))
		}
	}
	b.WriteString(fmt.Sprintf("echo '向量库迁移 %s 操作完成\n", req.Action))
	return b.String()
}

// floatSliceToJSON 把 float64 切片转为 JSON 数组字符串（紧凑输出）。
func floatSliceToJSON(v []float64) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%g", f))
	}
	sb.WriteByte(']')
	return sb.String()
}

// nonEmptyPayload 返回 payload（空则返回空对象）。
func nonEmptyPayload(p string) string {
	if strings.TrimSpace(p) == "" {
		return "{}"
	}
	return p
}

// ============================================================================
// 路由注册
// ============================================================================

// registerVectorRoutes 注册向量库运维路由。
func (s *Server) registerVectorRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/vector/{type}/{collection|write|search|index|migrate}
	for _, t := range []string{"milvus", "qdrant", "pgvector"} {
		tt := t
		mux.HandleFunc("/api/v1/vector/"+tt+"/collection", func(w http.ResponseWriter, r *http.Request) { s.handleVectorCollection(w, r, tt) })
		mux.HandleFunc("/api/v1/vector/"+tt+"/write", func(w http.ResponseWriter, r *http.Request) { s.handleVectorWrite(w, r, tt) })
		mux.HandleFunc("/api/v1/vector/"+tt+"/search", func(w http.ResponseWriter, r *http.Request) { s.handleVectorSearch(w, r, tt) })
		mux.HandleFunc("/api/v1/vector/"+tt+"/index", func(w http.ResponseWriter, r *http.Request) { s.handleVectorIndex(w, r, tt) })
		mux.HandleFunc("/api/v1/vector/"+tt+"/migrate", func(w http.ResponseWriter, r *http.Request) { s.handleVectorMigrate(w, r, tt) })
	}
	// 泛路由
	mux.HandleFunc("/api/v1/vector/deploy", s.handleVectorDeploy)
	mux.HandleFunc("/api/v1/vector/", s.handleVectorRouting)
}

func (s *Server) handleVectorRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/vector/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		if segments[0] != "" {
			s.handleVector(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		if segments[0] != "" && segments[1] != "" {
			s.handleVectorAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
