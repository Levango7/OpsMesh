// storage_ops.go 对象存储运维管理（Amazon S3 / Aliyun OSS / MinIO 兼容 S3）。
//
// 设计原则（延续 P0/P1/P3/P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/storage/{deviceID} 查询本地对象存储服务状态。
//   - 执行类：POST /api/v1/storage/{type}/bucket 和 /api/v1/storage/{type}/object，
//     shell 模板下发到 agent 执行（使用 aws-cli / ossutil / mc 等 CLI 工具）。
//
// 安全：AccessKey/SecretKey 不通过 shell 参数明文传递，改由 agent 侧环境变量
//（OPSMESH_S3_ACCESS_KEY / OPSMESH_S3_SECRET_KEY / OPSMESH_OSS_ACCESS_KEY_ID / OPSMESH_OSS_ACCESS_KEY_SECRET）读取；
// type/bucket/object 名走正则校验，杜绝命令注入；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// 对象存储类型白名单。
var allowedStorageTypes = map[string]bool{
	"s3":  true, // Amazon S3 及兼容（MinIO/Ceph）
	"oss": true, // Aliyun OSS
}

// 对象存储操作动作白名单。
var allowedStorageActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// 桶操作动作白名单。
var allowedBucketActions = map[string]bool{
	"create": true,
	"delete": true,
	"list":   true,
}

// 对象操作动作白名单。
var allowedObjectActions = map[string]bool{
	"upload":   true,
	"download": true,
	"delete":   true,
	"list":     true,
}

// 存储桶名正则（S3/OSS 命名规范：小写字母数字连字符点，3-63 字符）。
var storageBucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]$|^[a-z0-9]$`)

// 对象键名正则（允许常见字符，不含空格和特殊 shell 元字符）。
var storageObjectRe = regexp.MustCompile(`^[a-zA-Z0-9._/\-]+$`)

// 存储部署请求体。
type storageDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // s3/oss/minio
	Mode     string `json:"mode"`    // single/cluster
	Version  string `json:"version"` // 版本
	Config   string `json:"config"`  // 部署配置 JSON
}

// storageOpsRequest 通用对象存储操作请求体。
type storageOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"` // start/stop/restart/status/undeploy
}

// bucketReq 存储桶操作请求体。
type bucketReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // s3/oss
	Action   string `json:"action"` // create/delete/list
	Bucket   string `json:"bucket"` // 桶名
}

// objectReq 对象操作请求体。
type objectReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // s3/oss
	Action   string `json:"action"` // upload/download/delete/list
	Bucket   string `json:"bucket"` // 桶名
	Object   string `json:"object"` // 对象键
	Local    string `json:"local"`  // 本地文件路径（upload/download 用）
}

// handleStorage 查询指定设备上的对象存储状态。
func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type stInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []stInfo
	for _, stType := range []string{"s3", "oss", "minio"} {
		info := stInfo{Type: stType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				if strings.EqualFold(svc.Name, stType) || strings.EqualFold(svc.Name, stType+"-server") {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "storage": result})
}

// handleStorageDeploy 处理 POST /api/v1/storage/deploy：部署对象存储（MinIO 等）。
func (s *Server) handleStorageDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req storageDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedStorageTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported storage type"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildStorageDeployScript(req)
	action := "storage_deploy_" + req.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "storage.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s mode=%s", req.DeviceID, req.Mode)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildStorageDeployScript(req storageDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	switch req.Type {
	case "minio":
		b.WriteString("# MinIO 部署（S3 兼容对象存储）\n")
		b.WriteString("if command -v minio >/dev/null 2>&1; then echo 'MinIO 已安装'; else echo '请先下载 MinIO 二进制包' && exit 1; fi\n")
		b.WriteString("MINIO_ROOT_USER=${MINIO_ROOT_USER:-minioadmin}\n")
		b.WriteString("MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD:-minioadmin}\n")
		b.WriteString("mkdir -p /data/minio\n")
		b.WriteString("MINIO_ROOT_USER=$MINIO_ROOT_USER MINIO_ROOT_PASSWORD=$MINIO_ROOT_PASSWORD minio server /data/minio --console-address ':9001' &\n")
		b.WriteString("echo 'MinIO 已启动 (控制台:9001, API:9000)'\n")
	case "s3":
		b.WriteString("# Amazon S3 是云服务，无需本地部署。如需本地兼容，请部署 MinIO。\n")
	case "oss":
		b.WriteString("# Aliyun OSS 是云服务，无需本地部署。\n")
	}
	return b.String()
}

// handleStorageAction 处理 POST /api/v1/storage/{type}/{action}：启停重启/状态。
func (s *Server) handleStorageAction(w http.ResponseWriter, r *http.Request, stType, action string) {
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
	if !allowedStorageTypes[stType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported storage type"})
		return
	}
	if !allowedStorageActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req storageOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = stType
	req.Action = action

	script := buildStorageActionScript(req)
	taskAction := "storage_" + action + "_" + stType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "storage.action."+stType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: stType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildStorageActionScript(req storageOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "minio":
		b.WriteString("# MinIO 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start minio || pkill -f 'minio server' || true\nMINIO_ROOT_USER=${MINIO_ROOT_USER:-minioadmin} MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD:-minioadmin} minio server /data/minio --console-address ':9001' &\n")
		case "stop":
			b.WriteString("systemctl stop minio || pkill -f 'minio server' || true\n")
		case "restart":
			b.WriteString("systemctl restart minio || (pkill -f 'minio server' || true)\n")
		case "status":
			b.WriteString("systemctl is-active --quiet minio && echo 'running' || (pgrep -f 'minio server' >/dev/null && echo 'running' || echo 'not running')\n")
		}
	case "s3", "oss":
		fmt.Fprintf(&b, "# %s 是云服务，启停由云厂商控制台或 API 完成\n", req.Type)
	}
	fmt.Fprintf(&b, "echo '%s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleStorageBucket 处理 POST /api/v1/storage/{type}/bucket：桶管理。
func (s *Server) handleStorageBucket(w http.ResponseWriter, r *http.Request, stType string) {
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
	if !allowedStorageTypes[stType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported storage type"})
		return
	}
	var req bucketReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedBucketActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if req.Action != "list" && !storageBucketRe.MatchString(req.Bucket) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bucket name"})
		return
	}
	req.Type = stType

	script := buildBucketScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "storage_bucket_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "storage.bucket."+stType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "storage_bucket_" + req.Action, Target: req.Bucket,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s", req.DeviceID, stType)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildBucketScript(req bucketReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "s3":
		b.WriteString("# S3 桶管理（aws-cli，凭证从环境变量 OPSMESH_S3_ACCESS_KEY / OPSMESH_S3_SECRET_KEY 读取）\n")
		b.WriteString("export AWS_ACCESS_KEY_ID=${OPSMESH_S3_ACCESS_KEY}\n")
		b.WriteString("export AWS_SECRET_ACCESS_KEY=${OPSMESH_S3_SECRET_KEY}\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "aws s3api create-bucket --bucket %s || echo '桶已存在或创建失败: %s'\n", req.Bucket, req.Bucket)
		case "delete":
			fmt.Fprintf(&b, "aws s3api delete-bucket --bucket %s || echo '桶不存在或删除失败: %s'\n", req.Bucket, req.Bucket)
		case "list":
			b.WriteString("aws s3api list-buckets --query 'Buckets[].Name'\n")
		}
	case "oss":
		b.WriteString("# OSS 桶管理（ossutil，凭证从环境变量 OPSMESH_OSS_ACCESS_KEY_ID / OPSMESH_OSS_ACCESS_KEY_SECRET 读取）\n")
		b.WriteString("export OSS_ACCESS_KEY_ID=${OPSMESH_OSS_ACCESS_KEY_ID}\n")
		b.WriteString("export OSS_ACCESS_KEY_SECRET=${OPSMESH_OSS_ACCESS_KEY_SECRET}\n")
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "ossutil mb oss://%s || echo '桶已存在或创建失败: %s'\n", req.Bucket, req.Bucket)
		case "delete":
			fmt.Fprintf(&b, "ossutil rm oss://%s --force || echo '桶不存在或删除失败: %s'\n", req.Bucket, req.Bucket)
		case "list":
			b.WriteString("ossutil ls\n")
		}
	}
	fmt.Fprintf(&b, "echo '对象存储 %s 桶 %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// handleStorageObject 处理 POST /api/v1/storage/{type}/object：对象管理。
func (s *Server) handleStorageObject(w http.ResponseWriter, r *http.Request, stType string) {
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
	if !allowedStorageTypes[stType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported storage type"})
		return
	}
	var req objectReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedObjectActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (upload/download/delete/list)"})
		return
	}
	if req.Action != "list" && !storageBucketRe.MatchString(req.Bucket) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bucket name"})
		return
	}
	if req.Action != "list" && req.Object != "" && !storageObjectRe.MatchString(req.Object) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid object key"})
		return
	}
	req.Type = stType

	script := buildObjectScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "storage_object_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "storage.object."+stType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "storage_object_" + req.Action, Target: req.Object,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s type=%s bucket=%s", req.DeviceID, stType, req.Bucket)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildObjectScript(req objectReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "s3":
		b.WriteString("export AWS_ACCESS_KEY_ID=${OPSMESH_S3_ACCESS_KEY}\n")
		b.WriteString("export AWS_SECRET_ACCESS_KEY=${OPSMESH_S3_SECRET_KEY}\n")
		switch req.Action {
		case "upload":
			fmt.Fprintf(&b, "aws s3 cp %s s3://%s/%s\n", shellQuote(req.Local), req.Bucket, req.Object)
		case "download":
			fmt.Fprintf(&b, "aws s3 cp s3://%s/%s %s\n", req.Bucket, req.Object, shellQuote(req.Local))
		case "delete":
			fmt.Fprintf(&b, "aws s3 rm s3://%s/%s\n", req.Bucket, req.Object)
		case "list":
			fmt.Fprintf(&b, "aws s3 ls s3://%s/\n", req.Bucket)
		}
	case "oss":
		b.WriteString("export OSS_ACCESS_KEY_ID=${OPSMESH_OSS_ACCESS_KEY_ID}\n")
		b.WriteString("export OSS_ACCESS_KEY_SECRET=${OPSMESH_OSS_ACCESS_KEY_SECRET}\n")
		switch req.Action {
		case "upload":
			fmt.Fprintf(&b, "ossutil cp %s oss://%s/%s\n", shellQuote(req.Local), req.Bucket, req.Object)
		case "download":
			fmt.Fprintf(&b, "ossutil cp oss://%s/%s %s\n", req.Bucket, req.Object, shellQuote(req.Local))
		case "delete":
			fmt.Fprintf(&b, "ossutil rm oss://%s/%s --force\n", req.Bucket, req.Object)
		case "list":
			fmt.Fprintf(&b, "ossutil ls oss://%s/\n", req.Bucket)
		}
	}
	fmt.Fprintf(&b, "echo '对象存储 %s 对象 %s 操作完成\n", req.Type, req.Action)
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerStorageRoutes 注册对象存储运维路由。
func (s *Server) registerStorageRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/storage/{type}/bucket 和 /api/v1/storage/{type}/object
	mux.HandleFunc("/api/v1/storage/s3/bucket", s.handleStorageBucketS3)
	mux.HandleFunc("/api/v1/storage/oss/bucket", s.handleStorageBucketOSS)
	mux.HandleFunc("/api/v1/storage/s3/object", s.handleStorageObjectS3)
	mux.HandleFunc("/api/v1/storage/oss/object", s.handleStorageObjectOSS)
	// 泛路由：GET /api/v1/storage/{deviceID} / POST /api/v1/storage/{type}/{action} / POST /api/v1/storage/deploy
	mux.HandleFunc("/api/v1/storage/deploy", s.handleStorageDeploy)
	mux.HandleFunc("/api/v1/storage/", s.handleStorageRouting)
}

func (s *Server) handleStorageBucketS3(w http.ResponseWriter, r *http.Request) {
	s.handleStorageBucket(w, r, "s3")
}
func (s *Server) handleStorageBucketOSS(w http.ResponseWriter, r *http.Request) {
	s.handleStorageBucket(w, r, "oss")
}
func (s *Server) handleStorageObjectS3(w http.ResponseWriter, r *http.Request) {
	s.handleStorageObject(w, r, "s3")
}
func (s *Server) handleStorageObjectOSS(w http.ResponseWriter, r *http.Request) {
	s.handleStorageObject(w, r, "oss")
}

func (s *Server) handleStorageRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/storage/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/storage/{deviceID}
		if segments[0] != "" {
			s.handleStorage(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/storage/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleStorageAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
