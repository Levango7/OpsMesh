// data_governance_ops.go 数据治理平面 API。
//
// 路由：
//   - POST /api/v1/governance/rules       DQ 规则 CRUD + 评估
//   - POST /api/v1/governance/pii         PII 脱敏策略 CRUD + 扫描
//   - POST /api/v1/governance/retention   保留策略 CRUD + 应用
//   - GET  /api/v1/governance/catalog     目录检索
//   - POST /api/v1/governance/lineage     Atlas 血缘代理
package controlplane

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/governance"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// registerGovernanceRoutes 注册数据治理路由。
func (s *Server) registerGovernanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/governance/rules", s.handleGovernanceRules)
	mux.HandleFunc("/api/v1/governance/pii", s.handleGovernancePII)
	mux.HandleFunc("/api/v1/governance/retention", s.handleGovernanceRetention)
	mux.HandleFunc("/api/v1/governance/catalog", s.handleGovernanceCatalog)
	mux.HandleFunc("/api/v1/governance/lineage", s.handleGovernanceLineage)
}

// --- Rules ---

func (s *Server) handleGovernanceRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Action string              `json:"action"`
		Rule   governance.DataRule `json:"rule"`
		Rows   []map[string]any    `json:"rows"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "create":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		req.Rule.TenantID = actx.TenantID
		now := time.Now().UTC()
		req.Rule.CreatedAt = now
		req.Rule.UpdatedAt = now
		if req.Rule.Severity == "" {
			req.Rule.Severity = "warn"
		}
		if !req.Rule.Enabled {
			req.Rule.Enabled = true
		}
		if err := validateGovernanceRule(req.Rule); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		created, err := s.governanceStore.CreateRule(req.Rule)
		if err != nil {
			writeInternalError(r.Context(), w, "governance_rule_create", err)
			return
		}
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "governance_rule_create",
			Target: created.ID, Detail: sanitizeAuditDetail("dataset=" + created.Dataset + " kind=" + created.Kind),
		})
		paginate.WriteJSON(w, http.StatusCreated, created)
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Rule.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
			return
		}
		existing, err := s.governanceStore.GetRule(actx.TenantID, req.Rule.ID)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		req.Rule.TenantID = actx.TenantID
		req.Rule.CreatedAt = existing.CreatedAt
		req.Rule.UpdatedAt = time.Now().UTC()
		if req.Rule.Severity == "" {
			req.Rule.Severity = existing.Severity
		}
		if err := validateGovernanceRule(req.Rule); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		updated, err := s.governanceStore.UpdateRule(req.Rule)
		if err != nil {
			writeInternalError(r.Context(), w, "governance_rule_update", err)
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "delete":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Rule.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
			return
		}
		if err := s.governanceStore.DeleteRule(actx.TenantID, req.Rule.ID); err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"rules": s.governanceStore.ListRules(actx.TenantID),
		})
	case "evaluate":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		if req.Rule.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule id required"})
			return
		}
		rule, err := s.governanceStore.GetRule(actx.TenantID, req.Rule.ID)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		rows := make([]governance.Row, 0, len(req.Rows))
		for _, m := range req.Rows {
			rows = append(rows, governance.Row(m))
		}
		res, err := governance.EvaluateRule(rule, rows)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		res.TenantID = actx.TenantID
		saved := s.governanceStore.AppendRuleResult(res)
		paginate.WriteJSON(w, http.StatusOK, saved)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func validateGovernanceRule(r governance.DataRule) error {
	if strings.TrimSpace(r.Dataset) == "" {
		return fmt.Errorf("dataset is required")
	}
	if err := governance.ValidateIdentifier(r.Dataset); err != nil {
		return fmt.Errorf("invalid dataset %q", r.Dataset)
	}
	if r.Column != "" {
		if err := governance.ValidateIdentifier(r.Column); err != nil {
			return fmt.Errorf("invalid column %q", r.Column)
		}
	}
	switch r.Kind {
	case governance.RuleKindNull, governance.RuleKindUnique, governance.RuleKindRange,
		governance.RuleKindRegex, governance.RuleKindFreshness, governance.RuleKindRowCount:
	default:
		return fmt.Errorf("unknown rule kind %q", r.Kind)
	}
	if r.Severity == "" {
		r.Severity = "warn"
	}
	switch r.Severity {
	case "info", "warn", "critical":
	default:
		return fmt.Errorf("unknown severity %q", r.Severity)
	}
	return nil
}

// --- PII ---

func (s *Server) handleGovernancePII(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Action string               `json:"action"`
		Policy governance.PiiPolicy `json:"policy"`
		Rows   []map[string]any     `json:"rows"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "create":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		req.Policy.TenantID = actx.TenantID
		now := time.Now().UTC()
		req.Policy.CreatedAt = now
		req.Policy.UpdatedAt = now
		if req.Policy.Kind == "" {
			req.Policy.Kind = governance.PIIKindCustom
		}
		if err := validateGovernancePII(req.Policy); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		created, err := s.governanceStore.CreatePIIPolicy(req.Policy)
		if err != nil {
			writeInternalError(r.Context(), w, "governance_pii_create", err)
			return
		}
		paginate.WriteJSON(w, http.StatusCreated, created)
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Policy.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy id required"})
			return
		}
		existing, err := s.governanceStore.GetPIIPolicy(actx.TenantID, req.Policy.ID)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
			return
		}
		req.Policy.TenantID = actx.TenantID
		req.Policy.CreatedAt = existing.CreatedAt
		req.Policy.UpdatedAt = time.Now().UTC()
		if req.Policy.Kind == "" {
			req.Policy.Kind = existing.Kind
		}
		if err := validateGovernancePII(req.Policy); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		updated, err := s.governanceStore.UpdatePIIPolicy(req.Policy)
		if err != nil {
			writeInternalError(r.Context(), w, "governance_pii_update", err)
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "delete":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Policy.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy id required"})
			return
		}
		if err := s.governanceStore.DeletePIIPolicy(actx.TenantID, req.Policy.ID); err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"policies": s.governanceStore.ListPIIPolicies(actx.TenantID),
		})
	case "scan":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		masked := make([]map[string]any, 0, len(req.Rows))
		for _, row := range req.Rows {
			m := make(map[string]any, len(row))
			for k, v := range row {
				m[k] = v
			}
			if req.Policy.Column != "" {
				if v, ok := row[req.Policy.Column]; ok {
					m[req.Policy.Column+"_masked"] = governance.SuggestMask(req.Policy.Kind, fmt.Sprint(v))
				}
			}
			masked = append(masked, m)
		}
		deviceID := strings.TrimSpace(r.URL.Query().Get("deviceID"))
		if deviceID != "" {
			dev := s.store.Device(deviceID)
			if dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
				return
			}
			ag := s.store.Agent(deviceID)
			if ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
				return
			}
			script := "set -e\necho 'governance pii scan " + deviceID + " " + req.Policy.Dataset + "." + req.Policy.Column + " " + req.Policy.Kind + " done'\n"
			task, err := s.createSystemTask(actx.TenantID, deviceID, script, "governance_pii_scan")
			if err != nil {
				writeInternalError(r.Context(), w, "governance_pii_scan", err)
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
				"taskID":  task.TaskID,
				"preview": masked,
			})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"preview": masked,
		})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func validateGovernancePII(p governance.PiiPolicy) error {
	if strings.TrimSpace(p.Dataset) == "" {
		return fmt.Errorf("dataset is required")
	}
	if err := governance.ValidateIdentifier(p.Dataset); err != nil {
		return fmt.Errorf("invalid dataset %q", p.Dataset)
	}
	if strings.TrimSpace(p.Column) == "" {
		return fmt.Errorf("column is required")
	}
	if err := governance.ValidateIdentifier(p.Column); err != nil {
		return fmt.Errorf("invalid column %q", p.Column)
	}
	switch p.Kind {
	case governance.PIIKindEmail, governance.PIIKindPhone, governance.PIIKindIDCard,
		governance.PIIKindName, governance.PIIKindCustom:
	case "":
		p.Kind = governance.PIIKindCustom
	default:
		return fmt.Errorf("unknown pii kind %q", p.Kind)
	}
	return nil
}

// --- Retention ---

func (s *Server) handleGovernanceRetention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Action   string                     `json:"action"`
		Policy   governance.RetentionPolicy `json:"policy"`
		DryRun   bool                       `json:"dryRun"`
		DeviceID string                     `json:"deviceID"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "create":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		req.Policy.TenantID = actx.TenantID
		now := time.Now().UTC()
		req.Policy.CreatedAt = now
		req.Policy.UpdatedAt = now
		if req.Policy.Action == "" {
			req.Policy.Action = governance.RetentionActionDelete
		}
		if err := validateGovernanceRetention(req.Policy); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		created, err := s.governanceStore.CreateRetentionPolicy(req.Policy)
		if err != nil {
			writeInternalError(r.Context(), w, "governance_retention_create", err)
			return
		}
		paginate.WriteJSON(w, http.StatusCreated, created)
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Policy.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy id required"})
			return
		}
		existing, err := s.governanceStore.GetRetentionPolicy(actx.TenantID, req.Policy.ID)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
			return
		}
		req.Policy.TenantID = actx.TenantID
		req.Policy.CreatedAt = existing.CreatedAt
		req.Policy.UpdatedAt = time.Now().UTC()
		if req.Policy.Action == "" {
			req.Policy.Action = existing.Action
		}
		if err := validateGovernanceRetention(req.Policy); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		updated, err := s.governanceStore.UpdateRetentionPolicy(req.Policy)
		if err != nil {
			writeInternalError(r.Context(), w, "governance_retention_update", err)
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "delete":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Policy.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy id required"})
			return
		}
		if err := s.governanceStore.DeleteRetentionPolicy(actx.TenantID, req.Policy.ID); err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"policies": s.governanceStore.ListRetentionPolicies(actx.TenantID),
		})
	case "apply":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Policy.ID != "" {
			stored, err := s.governanceStore.GetRetentionPolicy(actx.TenantID, req.Policy.ID)
			if err != nil {
				paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
				return
			}
			req.Policy = stored
		}
		req.Policy.TenantID = actx.TenantID
		sql, err := governance.BuildRetentionSQL(req.Policy)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.DryRun {
			paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"dryRun": true,
				"sql":    sql,
			})
			return
		}
		deviceID := strings.TrimSpace(req.DeviceID)
		if deviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "apply requires deviceID"})
			return
		}
		dev := s.store.Device(deviceID)
		if dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
		ag := s.store.Agent(deviceID)
		if ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
		script := "set -e\ncat > /tmp/retention_apply.sql <<'SQL'\n" + sql + "\nSQL\n" +
			"echo 'retention apply " + req.Policy.Dataset + " " + req.Policy.Action + " done'\n"
		task, err := s.createSystemTask(actx.TenantID, deviceID, script, "governance_retention_apply")
		if err != nil {
			writeInternalError(r.Context(), w, "governance_retention_apply", err)
			return
		}
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "governance_retention_apply",
			Target: req.Policy.Dataset, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " action=" + req.Policy.Action),
		})
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
			"taskID":   task.TaskID,
			"deviceID": deviceID,
			"sql":      sql,
		})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func validateGovernanceRetention(p governance.RetentionPolicy) error {
	if strings.TrimSpace(p.Dataset) == "" {
		return fmt.Errorf("dataset is required")
	}
	if err := governance.ValidateIdentifier(p.Dataset); err != nil {
		return fmt.Errorf("invalid dataset %q", p.Dataset)
	}
	if p.RetentionDays <= 0 {
		return fmt.Errorf("retentionDays must be > 0")
	}
	switch p.Action {
	case governance.RetentionActionDelete, governance.RetentionActionArchive, governance.RetentionActionAnonymize:
	case "":
		p.Action = governance.RetentionActionDelete
	default:
		return fmt.Errorf("unknown action %q", p.Action)
	}
	return nil
}

// --- Catalog ---

func (s *Server) handleGovernanceCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.subsystemAuthorize(w, r, "task:read")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	assets := s.governanceStore.ListCatalogAssets(actx.TenantID)
	out := make([]governance.CatalogAsset, 0, len(assets))
	for _, a := range assets {
		if q != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(q)) &&
			!strings.Contains(strings.ToLower(a.Owner), strings.ToLower(q)) {
			continue
		}
		if tag != "" {
			found := false
			for _, t := range a.Tags {
				if strings.EqualFold(t, tag) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, a)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"assets": out,
		"total":  len(out),
	})
}

// --- Lineage (Atlas proxy) ---

func (s *Server) handleGovernanceLineage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.subsystemAuthorize(w, r, "task:read"); !ok {
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	atlasURL := os.Getenv("OPSMESH_ATLAS_URL")
	if atlasURL == "" {
		paginate.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "atlas not configured"})
		return
	}
	u := strings.TrimRight(atlasURL, "/") + "/api/atlas/v2/search/basic?query=" + url.QueryEscape(q)
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeInternalError(r.Context(), w, "governance_lineage", err)
		return
	}
	resp, err := http.DefaultClient.Do(outReq)
	if err != nil {
		writeInternalError(r.Context(), w, "governance_lineage", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeInternalError(r.Context(), w, "governance_lineage", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
