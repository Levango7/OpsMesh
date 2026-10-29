// cloud_account.go Wave 2-B 云账号与云资源控制面 API。
//
// API 端点：
//   - GET    /api/v1/cloud-accounts              列出云账号（租户隔离，分页）
//   - POST   /api/v1/cloud-accounts              创建云账号
//   - GET/PUT/DELETE /api/v1/cloud-accounts/{id} 云账号详情/更新/删除
//   - POST   /api/v1/cloud-accounts/{id}/sync    同步云账号（回写 sync_status/discovered_count）
//   - POST   /api/v1/cloud-accounts/{id}/test    测试云账号连通性（不持久化凭据）
//   - GET    /api/v1/cloud-accounts/{id}/regions 列出云账号可用区域（模拟/供 CloudProvider 接入）
//   - GET    /api/v1/cloud-resources             列出云资源（租户隔离，分页，可按 accountID/resourceType 过滤）
//
// 路由注册（server_lifecycle.go）：
//   - GET    /api/v1/cloud-accounts        → handleListCloudAccounts
//   - POST   /api/v1/cloud-accounts        → handleCreateCloudAccount
//   - /api/v1/cloud-accounts/              → handleCloudAccountRouting（子路径分发）
//   - GET    /api/v1/cloud-resources       → handleListCloudResources
//
// 设计要点：
//   - 所有 handler 经 s.requireTenantContext 获取 actx.TenantID，查询一律按 tenant_id 隔离；
//   - RBAC（P0：此前全表零权限闸，viewer 亦可增删改同步云账号）：读接口 cloud:read、
//     写接口 cloud:write（含同步）；2026-10-22 起由 platform:read/platform:write/task:write
//     切到专有的 cloud 族（目录此前无 cloud 族，前端 /cloud-accounts 的 requirePerm
//     'cloud:read' 对全部角色恒不可达）。platform:* 保留给真正的平台配置端点
//     （/api/v1/admin/config 等）。同步（触发 agent 侧任务下发）现由 cloud:write 把关——
//     cloud 组不在 operator 派生集内 ⇒ 该权限实际仅 admin 持有，与旧 task:write 约束等价；
//   - CredentialsRef 仅指向 SecretItem（${provider:key} 引用），明文凭据不落库：
//     创建/更新均经 model.RequireSecretReference 校验，明文 AK/SK 一律 400 拒绝；
//   - 云账号不存在或租户不匹配时按 404 拒绝（不泄露存在性）；
//   - Default status="active"，sync_status="pending"（由 store 层兜底，handler 亦显式默认）。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider"
	"github.com/Levango7/OpsMesh/internal/secrets"
	"github.com/Levango7/OpsMesh/internal/store"
	"github.com/Levango7/OpsMesh/internal/store/model"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// resolveCloudCredentials 从云账号的 CredentialsRef 解析出 Credentials。
// 返回 nil 表示无法解析（凭据未配置或解析失败）。
func (s *Server) resolveCloudCredentials(account *store.CloudAccount) *cloudprovider.Credentials {
	if account.CredentialsRef == "" {
		return nil
	}
	if s.secretProvider == nil {
		return nil
	}
	resolved, err := secrets.ResolveSecret(account.CredentialsRef, s.secretProvider)
	if err != nil {
		return nil
	}
	// 解析 JSON
	var creds struct {
		Type            string `json:"type"`
		AccessKeyID     string `json:"accessKeyId"`
		AccessKeySecret string `json:"accessKeySecret"`
		OIDCToken       string `json:"oidcToken"`
		RoleARN         string `json:"roleArn"`
		Region          string `json:"region"`
	}
	if err := json.Unmarshal([]byte(resolved), &creds); err != nil {
		// 如果不是 JSON，尝试作为 AK:SK 格式
		parts := strings.SplitN(resolved, ":", 2)
		if len(parts) == 2 {
			creds.Type = "aksk"
			creds.AccessKeyID = parts[0]
			creds.AccessKeySecret = parts[1]
		} else {
			return nil
		}
	}
	if creds.Type == "" {
		creds.Type = account.AuthType
		if creds.Type == "" {
			creds.Type = "aksk"
		}
	}
	if creds.Region == "" {
		creds.Region = account.Region
	}
	return &cloudprovider.Credentials{
		Type:            cloudprovider.AuthType(creds.Type),
		AccessKeyID:     creds.AccessKeyID,
		AccessKeySecret: creds.AccessKeySecret,
		OIDCToken:       creds.OIDCToken,
		RoleARN:         creds.RoleARN,
		Region:          creds.Region,
	}
}

// handleCloudAccounts 统一处理 /api/v1/cloud-accounts：
//   - GET  列表（分页）
//   - POST 创建
func (s *Server) handleCloudAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListCloudAccounts(w, r)
	case http.MethodPost:
		s.handleCreateCloudAccount(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleCloudAccountRouting 分派 /api/v1/cloud-accounts/{id} 子路径：
//   - GET    /{id}             → handleGetCloudAccount
//   - PUT    /{id}             → handleUpdateCloudAccount
//   - DELETE /{id}             → handleDeleteCloudAccount
//   - POST   /{id}/sync        → handleSyncCloudAccount
//   - POST   /{id}/test        → handleTestCloudAccount
//   - GET    /{id}/regions     → handleListCloudAccountRegions
func (s *Server) handleCloudAccountRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/cloud-accounts/")
	if rest == "" {
		paginate.JSONError(w, http.StatusBadRequest, "account id required")
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "account id required")
		return
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch r.Method {
	case http.MethodGet:
		switch action {
		case "":
			s.handleGetCloudAccount(w, r, id)
		case "regions":
			s.handleListCloudAccountRegions(w, r, id)
		default:
			paginate.JSONError(w, http.StatusNotFound, "not found")
		}
	case http.MethodPut:
		if action == "" {
			s.handleUpdateCloudAccount(w, r, id)
		} else {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case http.MethodDelete:
		if action == "" {
			s.handleDeleteCloudAccount(w, r, id)
		} else {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case http.MethodPost:
		switch action {
		case "sync":
			s.handleSyncCloudAccount(w, r, id)
		case "test":
			s.handleTestCloudAccount(w, r, id)
		default:
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// 云账号 CRUD
// ============================================================================

// handleListCloudAccounts 处理 GET /api/v1/cloud-accounts：列出当前租户的云账号（分页）。
// 查询参数：offset（默认 0）、limit（默认 20，上限 200）。
func (s *Server) handleListCloudAccounts(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:read"); !ok {
		return
	}
	offset, limit := parseOffsetLimit(r)
	accounts, total, err := s.store.ListCloudAccounts(r.Context(), actx.TenantID, offset, limit)
	if err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "list cloud accounts failed")
		return
	}
	if accounts == nil {
		accounts = []*store.CloudAccount{}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"accounts": accounts,
		"total":    total,
		"offset":   offset,
		"limit":    limit,
	})
}

// handleCreateCloudAccount 处理 POST /api/v1/cloud-accounts：创建云账号。
// 请求体：{name, provider, region, auth_type, credentials_ref, workspace_id, created_by}。
// name/provider/auth_type 必填；credentials_ref 引用 SecretItem（不存明文）。
func (s *Server) handleCreateCloudAccount(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}
	var body struct {
		Name           string `json:"name"`
		WorkspaceID    string `json:"workspaceId"`
		Provider       string `json:"provider"`
		Region         string `json:"region"`
		AuthType       string `json:"authType"`
		CredentialsRef string `json:"credentialsRef"`
		CreatedBy      string `json:"createdBy"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Name == "" || body.Provider == "" || body.AuthType == "" {
		paginate.JSONError(w, http.StatusBadRequest, "name, provider and authType are required")
		return
	}
	// 凭据引用形状校验：明文 AK/SK 一律拒绝（凭据本体只进 secret provider，库里只留引用）。
	if err := validateCloudCredentialsRef(body.CredentialsRef); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	acct := &store.CloudAccount{
		TenantID:       actx.TenantID,
		WorkspaceID:    body.WorkspaceID,
		Name:           body.Name,
		Provider:       body.Provider,
		Region:         body.Region,
		AuthType:       body.AuthType,
		CredentialsRef: body.CredentialsRef,
		Status:         "active",
		SyncStatus:     "pending",
		CreatedBy:      body.CreatedBy,
	}
	if err := s.store.CreateCloudAccount(r.Context(), acct); err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "create cloud account failed")
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "cloud_account_create",
		Target: acct.ID, Detail: "name=" + acct.Name,
	})
	paginate.WriteJSON(w, http.StatusCreated, acct)
}

// handleGetCloudAccount 处理 GET /api/v1/cloud-accounts/{id}：返回单个云账号。
func (s *Server) handleGetCloudAccount(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:read"); !ok {
		return
	}
	acct, err := s.store.GetCloudAccount(r.Context(), id)
	if err != nil || acct == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	// 租户隔离：归属其他租户时按 not found 拒绝（不泄露存在性）。
	if acct.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	paginate.WriteJSON(w, http.StatusOK, acct)
}

// handleUpdateCloudAccount 处理 PUT /api/v1/cloud-accounts/{id}：部分更新云账号。
// 请求体：{workspace_id, name, provider, region, auth_type, credentials_ref, status, ...}。
// id/tenant_id/created_at 不可改。
func (s *Server) handleUpdateCloudAccount(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}
	// 先校验存在 + 租户归属。
	existing, err := s.store.GetCloudAccount(r.Context(), id)
	if err != nil || existing == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	if existing.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	var updates map[string]interface{}
	if err := decodeJSONBody(w, r, &updates); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// 禁止越权改写归属与创建时间。
	delete(updates, "id")
	delete(updates, "tenantId")
	delete(updates, "tenant_id")
	delete(updates, "createdAt")
	delete(updates, "created_by")
	// 凭据引用形状校验：明文 AK/SK 一律拒绝（store 认 snake_case 的 credentials_ref，
	// 驼峰 credentialsRef 虽被 store 忽略，同样先拒以免调用方误以为已写入）。
	for _, key := range []string{"credentials_ref", "credentialsRef"} {
		ref, ok := updates[key].(string)
		if !ok {
			continue
		}
		if err := validateCloudCredentialsRef(ref); err != nil {
			paginate.JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.store.UpdateCloudAccount(r.Context(), id, updates); err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "update cloud account failed")
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "cloud_account_update",
		Target: id, Detail: "updates=" + fmt.Sprint(updates),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleDeleteCloudAccount 处理 DELETE /api/v1/cloud-accounts/{id}：删除云账号。
// 同时删除该账号下全部云资源（级联）。
func (s *Server) handleDeleteCloudAccount(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}
	existing, err := s.store.GetCloudAccount(r.Context(), id)
	if err != nil || existing == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	if existing.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	// 级联删除该账号下全部云资源。级联失败不阻断账号删除，但需留痕。
	if err := s.store.DeleteCloudResourcesByAccount(r.Context(), id); err != nil {
		logx.Warn(r.Context(), "级联删除云账号下云资源失败（继续删除云账号）", "accountID", id, "error", err)
	}
	if err := s.store.DeleteCloudAccount(r.Context(), id); err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "delete cloud account failed")
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "cloud_account_delete",
		Target: id, Detail: "name=" + existing.Name,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleSyncCloudAccount 处理 POST /api/v1/cloud-accounts/{id}/sync：同步云账号。
// 请求体：{sync_status, discovered_count}。回写同步状态到 store。
// 注意：真正的厂商 API 调用由 CloudProvider（Wave 2-A）实现，此处仅做状态回写入口。
func (s *Server) handleSyncCloudAccount(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	// 同步触发 agent 侧的任务下发（云资源发现）：按云账号写权限点闸（cloud:write，admin 独占组）。
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}
	existing, err := s.store.GetCloudAccount(r.Context(), id)
	if err != nil || existing == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	if existing.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	var body struct {
		SyncStatus      string `json:"syncStatus"`
		DiscoveredCount int    `json:"discoveredCount"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.SyncStatus == "" {
		body.SyncStatus = "syncing"
	}

	// 尝试调用 CloudProvider 进行真实资源发现
	creds := s.resolveCloudCredentials(existing)
	discoveredCount := body.DiscoveredCount
	if creds != nil {
		provider, ok := s.cloudProviderFactory.Get(cloudprovider.ProviderType(existing.Provider))
		if ok {
			region := existing.Region
			if region == "" {
				region = creds.Region
			}
			if region == "" {
				region = "cn-hangzhou"
			}
			// 发现实例（各资源类型独立尽力发现：单项失败记 Warn 留痕，按 0 计入总数，不阻断同步）
			instances, err := provider.DiscoverInstances(r.Context(), *creds, region)
			if err != nil {
				logx.Warn(r.Context(), "发现云实例失败", "provider", existing.Provider, "region", region, "error", err)
			}
			vpcs, err := provider.DiscoverVPCs(r.Context(), *creds, region)
			if err != nil {
				logx.Warn(r.Context(), "发现 VPC 失败", "provider", existing.Provider, "region", region, "error", err)
			}
			subnets, err := provider.DiscoverSubnets(r.Context(), *creds, region, "")
			if err != nil {
				logx.Warn(r.Context(), "发现子网失败", "provider", existing.Provider, "region", region, "error", err)
			}
			clusters, err := provider.DiscoverClusters(r.Context(), *creds, region)
			if err != nil {
				logx.Warn(r.Context(), "发现集群失败", "provider", existing.Provider, "region", region, "error", err)
			}
			discoveredCount = len(instances) + len(vpcs) + len(subnets) + len(clusters)
			body.SyncStatus = "synced"
		}
	}

	if err := s.store.SyncCloudAccount(r.Context(), id, body.SyncStatus, discoveredCount); err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "sync cloud account failed")
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "cloud_account_sync",
		Target: id, Detail: "sync_status=" + body.SyncStatus + " discovered=" + strconv.Itoa(discoveredCount),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{
		"status":          "synced",
		"syncStatus":      body.SyncStatus,
		"discoveredCount": strconv.Itoa(discoveredCount),
	})
}

// handleTestCloudAccount 处理 POST /api/v1/cloud-accounts/{id}/test：测试云账号连通性。
// 返回 {status, message}；不持久化凭据，CredentialsRef 仅供 CloudProvider 引用 SecretItem。
func (s *Server) handleTestCloudAccount(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:read"); !ok {
		return
	}
	existing, err := s.store.GetCloudAccount(r.Context(), id)
	if err != nil || existing == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	if existing.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	// 凭据明文不落库：CredentialsRef 指向 SecretItem，此处不解密、不外传明文。
	// 调用 CloudProvider.ValidateCredentials 进行真实连通性校验。
	if existing.CredentialsRef == "" {
		paginate.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "failed",
			"message": "no credentials configured",
		})
		return
	}
	creds := s.resolveCloudCredentials(existing)
	if creds == nil {
		paginate.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "failed",
			"message": "failed to resolve credentials",
		})
		return
	}
	provider, ok := s.cloudProviderFactory.Get(cloudprovider.ProviderType(existing.Provider))
	if !ok {
		paginate.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "failed",
			"message": "provider not supported: " + existing.Provider,
		})
		return
	}
	if err := provider.ValidateCredentials(r.Context(), *creds); err != nil {
		paginate.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "failed",
			"message": err.Error(),
		})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "credentials validated successfully",
	})
}

// handleListCloudAccountRegions 处理 GET /api/v1/cloud-accounts/{id}/regions：列出可用区域。
// 供前端地域下拉；由 CloudProvider（Wave 2-A）按 provider 返回真实区域列表，本端点返回常见占位。
func (s *Server) handleListCloudAccountRegions(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:read"); !ok {
		return
	}
	existing, err := s.store.GetCloudAccount(r.Context(), id)
	if err != nil || existing == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	if existing.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	regions := cloudProviderRegions(existing.Provider)
	// 尝试调用 CloudProvider.ListRegions 获取真实区域列表
	creds := s.resolveCloudCredentials(existing)
	if creds != nil {
		provider, ok := s.cloudProviderFactory.Get(cloudprovider.ProviderType(existing.Provider))
		if ok {
			if realRegions, err := provider.ListRegions(r.Context(), *creds); err == nil && len(realRegions) > 0 {
				regions = make([]string, 0, len(realRegions))
				for _, r := range realRegions {
					regions = append(regions, r.ID)
				}
			}
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"provider": existing.Provider,
		"regions":  regions,
	})
}

// cloudProviderRegions 按 provider 返回常见区域占位（Wave 2-A CloudProvider 接入后替换为真实调用）。
func cloudProviderRegions(provider string) []string {
	switch provider {
	case "aliyun":
		return []string{"cn-hangzhou", "cn-beijing", "cn-shanghai", "cn-shenzhen"}
	case "aws":
		return []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}
	case "azure":
		return []string{"chinaeast2", "chinanorth2", "eastasia", "southeastasia"}
	case "tencent":
		return []string{"ap-guangzhou", "ap-shanghai", "ap-beijing", "ap-chengdu"}
	case "huawei":
		return []string{"cn-north-1", "cn-east-2", "cn-south-1"}
	default:
		return []string{}
	}
}

// validateCloudCredentialsRef 校验云账号凭据引用的形状：只接受 ${provider:key} 引用，
// 明文凭据（如 "AKIDxxxx:SECRETyyyy"）一律拒绝。
//
// 判据复用 secrets 落库的同一处实现 model.RequireSecretReference（TD-88 方向裁定：
// 库里只留引用，凭据本体进 --secret-provider 指向的来源），避免「secrets 表拒明文、
// cloud_accounts 表收明文」两套形状判断各自漂移。
//
// 空值是合法状态（未配置凭据）：test 端点据此返回 "no credentials configured"。
func validateCloudCredentialsRef(ref string) error {
	if ref == "" {
		return nil
	}
	return model.RequireSecretReference(ref)
}

// parseOffsetLimit 从 query 解析 offset/limit。
// offset 默认 0，limit 默认 20、上限 200。
func parseOffsetLimit(r *http.Request) (int, int) {
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	return offset, limit
}

// ============================================================================
// 云资源
// ============================================================================

// handleListCloudResources 处理 GET /api/v1/cloud-resources：列出当前租户的云资源（分页）。
// 查询参数：offset、limit、accountID（可选）、resourceType（可选）。
func (s *Server) handleListCloudResources(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:read"); !ok {
		return
	}
	offset, limit := parseOffsetLimit(r)
	resources, total, err := s.store.ListCloudResources(r.Context(), actx.TenantID,
		r.URL.Query().Get("accountID"), r.URL.Query().Get("resourceType"), offset, limit)
	if err != nil {
		paginate.JSONError(w, http.StatusInternalServerError, "list cloud resources failed")
		return
	}
	if resources == nil {
		resources = []*store.CloudResource{}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"resources": resources,
		"total":     total,
		"offset":    offset,
		"limit":     limit,
	})
}

// handleCreateCloudResource 处理 POST /api/v1/cloud-resources：创建云资源。
// 请求体：{device_id, resource_type, region, name, properties, tags}
func (s *Server) handleCreateCloudResource(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}

	var req struct {
		DeviceID     string            `json:"device_id"`
		ResourceType string            `json:"resource_type"`
		Region       string            `json:"region"`
		Name         string            `json:"name"`
		Properties   map[string]string `json:"properties"`
		Tags         map[string]string `json:"tags"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.DeviceID == "" || req.ResourceType == "" || req.Region == "" || req.Name == "" {
		paginate.JSONError(w, http.StatusBadRequest, "device_id, resource_type, region, name are required")
		return
	}

	// 获取云账号
	account, err := s.store.GetCloudAccount(r.Context(), req.DeviceID)
	if err != nil || account == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud account not found")
		return
	}
	if account.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusForbidden, "access denied")
		return
	}

	// 获取对应的 CloudProvider
	provider, ok := s.cloudProviderFactory.Get(cloudprovider.ProviderType(account.Provider))
	if !ok {
		paginate.JSONError(w, http.StatusInternalServerError, "cloud provider not found")
		return
	}

	// 调用 Provider 创建资源
	creds := s.resolveCloudCredentials(account)
	if creds == nil {
		paginate.JSONError(w, http.StatusInternalServerError, "resolve credentials failed")
		return
	}
	cloudReq := cloudprovider.CreateResourceRequest{
		ResourceType: req.ResourceType,
		Region:       req.Region,
		Name:         req.Name,
		Properties:   req.Properties,
		Tags:         req.Tags,
	}
	resource, err := provider.CreateResource(r.Context(), *creds, cloudReq)
	if err != nil {
		logx.Error(r.Context(), "create cloud resource failed", err, "region", req.Region)
		paginate.JSONError(w, http.StatusInternalServerError, "create cloud resource failed")
		return
	}

	// 保存到本地存储（Tags 需要序列化为 JSON）
	tagsJSON, err := json.Marshal(resource.Tags)
	if err != nil {
		logx.Error(r.Context(), "marshal tags failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "marshal tags failed")
		return
	}

	cloudRes := &store.CloudResource{
		ID:             resource.ResourceID,
		TenantID:       actx.TenantID,
		CloudAccountID: req.DeviceID,
		ResourceType:   resource.ResourceType,
		ResourceID:     resource.ResourceID,
		ResourceName:   resource.ResourceName,
		Region:         resource.Region,
		Provider:       string(provider.ProviderType()),
		Status:         resource.Status,
		PrivateIP:      resource.PrivateIP,
		PublicIP:       resource.PublicIP,
		Tags:           string(tagsJSON),
	}
	if err := s.store.CreateCloudResource(r.Context(), cloudRes); err != nil {
		logx.Error(r.Context(), "save cloud resource failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "save cloud resource failed")
		return
	}

	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"resource": resource,
	})
}

// handleDeleteCloudResource 处理 DELETE /api/v1/cloud-resources/{id}：删除云资源。
func (s *Server) handleDeleteCloudResource(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}

	resourceID := strings.TrimPrefix(r.URL.Path, "/api/v1/cloud-resources/")
	if resourceID == "" {
		paginate.JSONError(w, http.StatusBadRequest, "resource id required")
		return
	}

	// 先查找本地资源，获取 cloud_account_id 和 provider
	resource, err := s.store.GetCloudResource(r.Context(), resourceID)
	if err != nil || resource == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud resource not found")
		return
	}
	if resource.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusForbidden, "access denied")
		return
	}

	provider, ok := s.cloudProviderFactory.Get(cloudprovider.ProviderType(resource.Provider))
	if !ok {
		paginate.JSONError(w, http.StatusInternalServerError, "cloud provider not found")
		return
	}

	// 调用 Provider 删除资源
	account, err := s.store.GetCloudAccount(r.Context(), resource.CloudAccountID)
	if err != nil || account == nil {
		logx.Error(r.Context(), "get cloud account failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "get cloud account failed")
		return
	}
	creds := s.resolveCloudCredentials(account)
	if creds == nil {
		paginate.JSONError(w, http.StatusInternalServerError, "resolve credentials failed")
		return
	}
	if err := provider.DeleteResource(r.Context(), *creds, resource.ResourceID); err != nil {
		logx.Error(r.Context(), "delete cloud resource failed", err, "resource_id", resource.ResourceID)
		paginate.JSONError(w, http.StatusInternalServerError, "delete cloud resource failed")
		return
	}

	// 从本地存储删除
	if err := s.store.DeleteCloudResource(r.Context(), resourceID); err != nil {
		logx.Error(r.Context(), "delete local resource failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "delete local resource failed")
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleSyncCloudResourceTags 处理 PUT /api/v1/cloud-resources/{id}/tags：同步云资源标签。
func (s *Server) handleSyncCloudResourceTags(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cloud:write"); !ok {
		return
	}

	resourceID := strings.TrimPrefix(r.URL.Path, "/api/v1/cloud-resources/")
	resourceID = strings.TrimSuffix(resourceID, "/tags")
	if resourceID == "" {
		paginate.JSONError(w, http.StatusBadRequest, "resource id required")
		return
	}

	var req struct {
		Tags map[string]string `json:"tags"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.JSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Tags) == 0 {
		paginate.JSONError(w, http.StatusBadRequest, "tags required")
		return
	}

	// 先查找本地资源
	resource, err := s.store.GetCloudResource(r.Context(), resourceID)
	if err != nil || resource == nil {
		paginate.JSONError(w, http.StatusNotFound, "cloud resource not found")
		return
	}
	if resource.TenantID != actx.TenantID {
		paginate.JSONError(w, http.StatusForbidden, "access denied")
		return
	}

	// 获取完整云账号信息（包含 CredentialsRef）
	account, err := s.store.GetCloudAccount(r.Context(), resource.CloudAccountID)
	if err != nil || account == nil {
		logx.Error(r.Context(), "get cloud account failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "get cloud account failed")
		return
	}
	creds := s.resolveCloudCredentials(account)
	if creds == nil {
		paginate.JSONError(w, http.StatusInternalServerError, "resolve credentials failed")
		return
	}

	provider, ok := s.cloudProviderFactory.Get(cloudprovider.ProviderType(resource.Provider))
	if !ok {
		paginate.JSONError(w, http.StatusInternalServerError, "cloud provider not found")
		return
	}

	// 解析本地存储的标签（JSON 字符串 -> map）
	existingTags := make(map[string]string)
	if resource.Tags != "" {
		if err := json.Unmarshal([]byte(resource.Tags), &existingTags); err != nil {
			logx.Error(r.Context(), "parse existing tags failed", err)
			paginate.JSONError(w, http.StatusInternalServerError, "parse existing tags failed")
			return
		}
	}

	// 调用 Provider 同步标签
	if err := provider.SyncTags(r.Context(), *creds, resource.ResourceID, req.Tags); err != nil {
		logx.Error(r.Context(), "sync tags failed", err, "resource_id", resource.ResourceID)
		paginate.JSONError(w, http.StatusInternalServerError, "sync tags failed")
		return
	}

	// 更新本地标签（合并新标签）
	mergedTags := make(map[string]string)
	for k, v := range existingTags {
		mergedTags[k] = v
	}
	for k, v := range req.Tags {
		mergedTags[k] = v
	}
	tagsJSON, err := json.Marshal(mergedTags)
	if err != nil {
		logx.Error(r.Context(), "marshal tags failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "marshal tags failed")
		return
	}
	if err := s.store.UpdateCloudResource(r.Context(), resourceID, map[string]interface{}{"tags": string(tagsJSON)}); err != nil {
		logx.Error(r.Context(), "update local tags failed", err)
		paginate.JSONError(w, http.StatusInternalServerError, "update local tags failed")
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "tags synced"})
}
