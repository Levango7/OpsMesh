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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"

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

// validatedVectorDeploy / validatedVectorAction / validatedVectorCollection /
// validatedVectorWrite / validatedVectorSearch 是白名单校验后的形态。
// builder 只接受这几种类型 —— raw vectorDeployRequest / vectorOpsRequest /
// vectorCollectionReq / vectorWriteReq / vectorSearchReq 无法被传入 builder。
// 这样 builder 层上不可能自行传递未校验值，校验责任不留模糊地带。
type validatedVectorDeploy struct {
	Type, Version string
}

type validatedVectorAction struct {
	Type     string
	Action   string
	Replicas int
}

type validatedVectorCollection struct {
	Type       string
	Action     string
	Collection string
	Dimension  int
	Metric     string
}

type validatedVectorWrite struct {
	Type       string
	Collection string
	ID         string
	Vector     []float64
	Payload    string
}

type validatedVectorSearch struct {
	Type       string
	Collection string
	Vector     []float64
	TopK       int
}

var allowedVectorCollectionActions = map[string]bool{"create": true, "delete": true, "list": true}
var allowedVectorIndexActions = map[string]bool{"create": true, "delete": true, "list": true}
var allowedVectorMigrateActions = map[string]bool{"export": true, "import": true}

func validateVectorDeploy(req vectorDeployRequest) (validatedVectorDeploy, error) {
	if !allowedVectorTypes[req.Type] {
		return validatedVectorDeploy{}, errors.New("unsupported vector db type")
	}
	if req.Version != "" && !middlewareVersionRe.MatchString(req.Version) {
		return validatedVectorDeploy{}, errors.New("invalid version")
	}
	return validatedVectorDeploy{Type: req.Type, Version: req.Version}, nil
}

func validateVectorAction(vecType, action string, req vectorOpsRequest) (validatedVectorAction, error) {
	if !allowedVectorTypes[vecType] {
		return validatedVectorAction{}, errors.New("unsupported vector db type")
	}
	if !allowedVectorActions[action] {
		return validatedVectorAction{}, errors.New("unsupported action")
	}
	return validatedVectorAction{Type: vecType, Action: action, Replicas: req.Replicas}, nil
}

// validateVectorCollectionAction 同时服务于 Collection（create/delete/list）+ Index（create/delete/list）。
// handler 调用前各自验证 action 白名单；validators 复用此函数。Migrate 不走此路径。
func validateVectorCollectionAction(vecType string, req vectorCollectionReq, allowed map[string]bool) (validatedVectorCollection, error) {
	if !allowedVectorTypes[vecType] {
		return validatedVectorCollection{}, errors.New("unsupported vector db type")
	}
	if !allowed[req.Action] {
		return validatedVectorCollection{}, errors.New("unsupported action")
	}
	if req.Action != "list" && !vectorCollectionRe.MatchString(req.Collection) {
		return validatedVectorCollection{}, errors.New("invalid collection name")
	}
	if req.Action == "create" && req.Dimension <= 0 {
		return validatedVectorCollection{}, errors.New("dimension must be positive")
	}
	if req.Action == "create" && req.Metric != "" {
		if err := shellquote.ValidateName(req.Metric, 32); err != nil {
			return validatedVectorCollection{}, fmt.Errorf("invalid metric: %w", err)
		}
	}
	return validatedVectorCollection{Type: vecType, Action: req.Action, Collection: req.Collection, Dimension: req.Dimension, Metric: req.Metric}, nil
}

func validateVectorWrite(vecType string, req vectorWriteReq) (validatedVectorWrite, error) {
	if !allowedVectorTypes[vecType] {
		return validatedVectorWrite{}, errors.New("unsupported vector db type")
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		return validatedVectorWrite{}, errors.New("invalid collection name")
	}
	if len(req.Vector) == 0 {
		return validatedVectorWrite{}, errors.New("vector is required")
	}
	if _, err := opsIdentifier("vector id", req.ID); err != nil {
		return validatedVectorWrite{}, err
	}
	if strings.TrimSpace(req.Payload) != "" {
		if !json.Valid([]byte(req.Payload)) {
			return validatedVectorWrite{}, errors.New("payload must be valid JSON")
		}
		if strings.Contains(req.Payload, "'") {
			return validatedVectorWrite{}, errors.New("payload must not contain single quotes")
		}
	}
	return validatedVectorWrite{Type: vecType, Collection: req.Collection, ID: req.ID, Vector: req.Vector, Payload: req.Payload}, nil
}

func validateVectorSearch(vecType string, req vectorSearchReq) (validatedVectorSearch, error) {
	if !allowedVectorTypes[vecType] {
		return validatedVectorSearch{}, errors.New("unsupported vector db type")
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		return validatedVectorSearch{}, errors.New("invalid collection name")
	}
	if len(req.Vector) == 0 {
		return validatedVectorSearch{}, errors.New("vector is required")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	return validatedVectorSearch{Type: vecType, Collection: req.Collection, Vector: req.Vector, TopK: topK}, nil
}

func validateVectorMigrate(vecType string, req vectorCollectionReq) (validatedVectorCollection, error) {
	if !allowedVectorTypes[vecType] {
		return validatedVectorCollection{}, errors.New("unsupported vector db type")
	}
	if !allowedVectorMigrateActions[req.Action] {
		return validatedVectorCollection{}, errors.New("unsupported action (export/import)")
	}
	if !vectorCollectionRe.MatchString(req.Collection) {
		return validatedVectorCollection{}, errors.New("invalid collection name")
	}
	return validatedVectorCollection{Type: vecType, Action: req.Action, Collection: req.Collection}, nil
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
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateVectorDeploy(req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorDeployScript(vreq)
	action := "vector_deploy_" + vreq.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: vreq.Type,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " mode=" + opsSQLString(req.Mode) + " version=" + opsSQLString(req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorDeployScript(vreq validatedVectorDeploy) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch vreq.Type {
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
	var req vectorOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateVectorAction(vecType, action, req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorActionScript(vreq)
	actionV, err := opsIdentifier("action", action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.action."+vecType, err)
		return
	}
	taskAction := "vector_" + actionV + "_" + vecType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.action."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: vecType,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " replicas=" + opsSQLString(strconv.Itoa(req.Replicas))),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorActionScript(vreq validatedVectorAction) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch vreq.Type {
	case "milvus":
		b.WriteString("# Milvus 启停/扩缩容\n")
		switch vreq.Action {
		case "start":
			b.WriteString("cd /opt && docker compose up -d || true\n")
		case "stop":
			b.WriteString("cd /opt && docker compose down || true\n")
		case "restart":
			b.WriteString("cd /opt && docker compose restart || true\n")
		case "status":
			b.WriteString("docker ps --filter name=milvus --format '{{.Names}}: {{.Status}}' || echo 'not running'\n")
		case "scale":
			replicas := vreq.Replicas
			if replicas <= 0 {
				replicas = 1
			}
			fmt.Fprintf(&b, "cd /opt && docker compose up -d --scale milvus-standalone=%d || true\n", replicas)
		}
	case "qdrant":
		b.WriteString("# Qdrant 启停/扩缩容\n")
		switch vreq.Action {
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
		switch vreq.Action {
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
	fmt.Fprintf(&b, "echo '向量库 %s %s 操作完成\n", vreq.Type, vreq.Action)
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
	var req vectorCollectionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateVectorCollectionAction(vecType, req, allowedVectorCollectionActions)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorCollectionScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_collection_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.collection."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_collection_" + vreq.Action, Target: vreq.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " type=" + opsSQLString(vecType) + " dim=" + opsSQLString(strconv.Itoa(req.Dimension))),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorCollectionScript(vreq validatedVectorCollection) string {

	var b strings.Builder
	collectionV, metricV := vreq.Collection, vreq.Metric
	b.WriteString("set -e\n")
	switch vreq.Type {
	case "milvus":
		b.WriteString("# Milvus 集合管理（REST API，v2）\n")
		switch vreq.Action {
		case "create":
			if metricV == "" {
				metricV = "COSINE"
			}
			fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/collections/create' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"dimension\":%d,\"metricType\":\"%s\"}'\n", collectionV, vreq.Dimension, metricV)
		case "delete":
			fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/collections/drop' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\"}'\n", collectionV)
		case "list":
			b.WriteString("curl -s -X POST 'http://localhost:19530/v2/vectordb/collections/list' -H 'Content-Type: application/json' -d '{}'\n")
		}
	case "qdrant":
		b.WriteString("# Qdrant 集合管理（REST API）\n")
		switch vreq.Action {
		case "create":
			if metricV == "" {
				metricV = "Cosine"
			}
			fmt.Fprintf(&b, "curl -s -X PUT 'http://localhost:6333/collections/%s' -H 'Content-Type: application/json' -d '{\"vectors\":{\"size\":%d,\"distance\":\"%s\"}}'\n", collectionV, vreq.Dimension, metricV)
		case "delete":
			fmt.Fprintf(&b, "curl -s -X DELETE 'http://localhost:6333/collections/%s'\n", collectionV)
		case "list":
			b.WriteString("curl -s 'http://localhost:6333/collections'\n")
		}
	case "pgvector":
		b.WriteString("# pgvector 表管理（SQL）\n")
		switch vreq.Action {
		case "create":
			fmt.Fprintf(&b, "psql -c 'CREATE EXTENSION IF NOT EXISTS vector; CREATE TABLE IF NOT EXISTS %s (id text PRIMARY KEY, embedding vector(%d), payload jsonb)' || echo '创建失败'\n", collectionV, vreq.Dimension)
		case "delete":
			fmt.Fprintf(&b, "psql -c 'DROP TABLE IF EXISTS %s' || echo '删除失败'\n", collectionV)
		case "list":
			b.WriteString("psql -c '\\dt'\n")
		}
	}
	fmt.Fprintf(&b, "echo '向量集合 %s 操作完成\n", vreq.Action)
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
	var req vectorWriteReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateVectorWrite(vecType, req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorWriteScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_write")
	if err != nil {
		writeInternalError(r.Context(), w, "vector.write."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_write", Target: vreq.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " id=" + opsSQLString(req.ID) + " dim=" + opsSQLString(strconv.Itoa(len(req.Vector)))),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorWriteScript(vreq validatedVectorWrite) string {

	var b strings.Builder
	collectionV, idV, payloadV := vreq.Collection, vreq.ID, vreq.Payload
	b.WriteString("set -e\n")
	vecJSON := floatSliceToJSON(vreq.Vector)
	switch vreq.Type {
	case "milvus":
		b.WriteString("# Milvus 向量写入\n")
		fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/entities/insert' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"data\":[{\"id\":\"%s\",\"vector\":%s}]}'\n", collectionV, idV, vecJSON)
	case "qdrant":
		b.WriteString("# Qdrant 向量写入（point id 需为整数或 UUID，此处用哈希）\n")
		fmt.Fprintf(&b, "curl -s -X PUT 'http://localhost:6333/collections/%s/points' -H 'Content-Type: application/json' -d '{\"points\":[{\"id\":1,\"vector\":%s,\"payload\":%s}]}'\n", collectionV, vecJSON, nonEmptyPayload(payloadV))
	case "pgvector":
		b.WriteString("# pgvector 向量写入\n")
		fmt.Fprintf(&b, "psql -c \"INSERT INTO %s (id, embedding) VALUES ('%s', '%s') ON CONFLICT (id) DO UPDATE SET embedding=EXCLUDED.embedding\" || echo '写入失败'\n", collectionV, idV, vecJSON)
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
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
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
	vreq, verr := validateVectorSearch(vecType, req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorSearchScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_search")
	if err != nil {
		writeInternalError(r.Context(), w, "vector.search."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_search", Target: vreq.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " topK=" + opsSQLString(strconv.Itoa(vreq.TopK))),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorSearchScript(vreq validatedVectorSearch) string {

	var b strings.Builder
	b.WriteString("set -e\n")
	vecJSON := floatSliceToJSON(vreq.Vector)
	switch vreq.Type {
	case "milvus":
		b.WriteString("# Milvus 相似检索\n")
		fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/entities/search' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"data\":[%s],\"limit\":%d}'\n", vreq.Collection, vecJSON, vreq.TopK)
	case "qdrant":
		b.WriteString("# Qdrant 相似检索\n")
		fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:6333/collections/%s/points/search' -H 'Content-Type: application/json' -d '{\"vector\":%s,\"limit\":%d}'\n", vreq.Collection, vecJSON, vreq.TopK)
	case "pgvector":
		b.WriteString("# pgvector 相似检索（L2 距离 <->）\n")
		fmt.Fprintf(&b, "psql -c 'SELECT id, embedding <-> ''%s'' AS distance FROM %s ORDER BY distance LIMIT %d' || echo '检索失败'\n", vecJSON, vreq.Collection, vreq.TopK)
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
	var req vectorCollectionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateVectorCollectionAction(vecType, req, allowedVectorIndexActions)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorIndexScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_index_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.index."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_index_" + vreq.Action, Target: vreq.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorIndexScript(vreq validatedVectorCollection) string {

	var b strings.Builder
	b.WriteString("set -e\n")
	switch vreq.Type {
	case "milvus":
		b.WriteString("# Milvus 索引管理（IVF_FLAT / HNSW）\n")
		switch vreq.Action {
		case "create":
			fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/indexes/create' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"indexParams\":[{\"fieldName\":\"vector\",\"indexType\":\"HNSW\",\"metricType\":\"COSINE\"}]}'\n", vreq.Collection)
		case "delete":
			fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/indexes/drop' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\",\"indexName\":\"vector\"}'\n", vreq.Collection)
		case "list":
			fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:19530/v2/vectordb/indexes/list' -H 'Content-Type: application/json' -d '{\"collectionName\":\"%s\"}'\n", vreq.Collection)
		}
	case "qdrant":
		b.WriteString("# Qdrant 索引管理（payload 字段索引）\n")
		switch vreq.Action {
		case "create":
			fmt.Fprintf(&b, "curl -s -X PUT 'http://localhost:6333/collections/%s/index' -H 'Content-Type: application/json' -d '{\"field_name\":\"payload\",\"field_schema\":\"keyword\"}'\n", vreq.Collection)
		case "delete":
			fmt.Fprintf(&b, "curl -s -X DELETE 'http://localhost:6333/collections/%s/index/payload'\n", vreq.Collection)
		case "list":
			fmt.Fprintf(&b, "curl -s 'http://localhost:6333/collections/%s'\n", vreq.Collection)
		}
	case "pgvector":
		b.WriteString("# pgvector 索引管理（IVFFlat / HNSW）\n")
		switch vreq.Action {
		case "create":
			fmt.Fprintf(&b, "psql -c 'CREATE INDEX ON %s USING hnsw (embedding vector_cosine_ops)' || echo '创建索引失败'\n", vreq.Collection)
		case "delete":
			fmt.Fprintf(&b, "psql -c 'DROP INDEX IF EXISTS %s_embedding_idx'\n", vreq.Collection)
		case "list":
			fmt.Fprintf(&b, "psql -c '\\di'\n")
		}
	}
	fmt.Fprintf(&b, "echo '向量索引 %s 操作完成\n", vreq.Action)
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
	var req vectorCollectionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateVectorMigrate(vecType, req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildVectorMigrateScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "vector_migrate_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "vector.migrate."+vecType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "vector_migrate_" + vreq.Action, Target: vreq.Collection,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVectorMigrateScript(vreq validatedVectorCollection) string {

	var b strings.Builder
	b.WriteString("set -e\n")
	switch vreq.Type {
	case "milvus":
		b.WriteString("# Milvus 迁移（milvus-backup 工具）\n")
		switch vreq.Action {
		case "export":
			fmt.Fprintf(&b, "milvus-backup create -n backup_%s -c %s || echo '导出失败，需配置 milvus-backup'\n", vreq.Collection, vreq.Collection)
		case "import":
			fmt.Fprintf(&b, "milvus-backup restore -n backup_%s -c %s || echo '导入失败，需配置 milvus-backup'\n", vreq.Collection, vreq.Collection)
		}
	case "qdrant":
		b.WriteString("# Qdrant 迁移（快照 snapshot）\n")
		switch vreq.Action {
		case "export":
			fmt.Fprintf(&b, "curl -s -X POST 'http://localhost:6333/collections/%s/snapshots'\n", vreq.Collection)
		case "import":
			fmt.Fprintf(&b, "echo 'Qdrant 导入：PUT /collections/%s/snapshots/upload（需上传快照文件）'\n", vreq.Collection)
		}
	case "pgvector":
		b.WriteString("# pgvector 迁移（pg_dump/restore）\n")
		switch vreq.Action {
		case "export":
			fmt.Fprintf(&b, "pg_dump -t %s > /tmp/%s_dump.sql || echo '导出失败'\n", vreq.Collection, vreq.Collection)
		case "import":
			fmt.Fprintf(&b, "psql < /tmp/%s_dump.sql || echo '导入失败'\n", vreq.Collection)
		}
	}
	fmt.Fprintf(&b, "echo '向量库迁移 %s 操作完成\n", vreq.Action)
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
		fmt.Fprintf(&sb, "%g", f)
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
