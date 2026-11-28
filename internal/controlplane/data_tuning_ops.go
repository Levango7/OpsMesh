// data_tuning_ops.go 大数据 sizing 调优 API。
//
// 路由：
//   - GET  /api/v1/data/tuning/profiles   工作负载白名单
//   - POST /api/v1/data/tuning/recommend  按 NodeSpec 生成推荐（yaml/json 渲染）
//   - POST /api/v1/data/tuning/apply      可选 dry-run；否则下发系统任务落盘配置
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/datatuning"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// dataTuningComponents 为可下发组件的白名单（同时决定 /etc/<component>/conf 路径）。
var dataTuningComponents = map[string]bool{
	"yarn":  true,
	"hdfs":  true,
	"hive":  true,
	"spark": true,
	"hbase": true,
	"kafka": true,
}

// registerDataTuningRoutes 注册大数据 sizing 调优路由。
func (s *Server) registerDataTuningRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/data/tuning/profiles", s.handleDataTuningProfiles)
	mux.HandleFunc("/api/v1/data/tuning/recommend", s.handleDataTuningRecommend)
	mux.HandleFunc("/api/v1/data/tuning/apply", s.handleDataTuningApply)
}

// handleDataTuningProfiles 处理 GET /api/v1/data/tuning/profiles。
func (s *Server) handleDataTuningProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.subsystemAuthorize(w, r, "task:read"); !ok {
		return
	}
	profiles := datatuning.Profiles()
	components := make([]string, 0, len(dataTuningComponents))
	for _, name := range []string{"yarn", "hdfs", "hive", "spark", "hbase", "kafka"} {
		if dataTuningComponents[name] {
			components = append(components, name)
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"profiles":   profiles,
		"components": components,
	})
}

// handleDataTuningRecommend 处理 POST /api/v1/data/tuning/recommend。
func (s *Server) handleDataTuningRecommend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.subsystemAuthorize(w, r, "task:read"); !ok {
		return
	}
	var req struct {
		datatuning.NodeSpec
		Format string `json:"format"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	profile, err := datatuning.Recommend(req.NodeSpec)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "yaml"
	}
	var rendered string
	switch format {
	case "yaml":
		rendered = datatuning.RenderYAML(profile)
	case "json":
		b, err := json.MarshalIndent(profile, "", "  ")
		if err != nil {
			writeInternalError(r.Context(), w, "data_tuning_recommend", err)
			return
		}
		rendered = string(b)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported format: " + req.Format})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"profile":    profile.Name,
		"workload":   profile.Workload,
		"components": profile.Components,
		"notes":      profile.Notes,
		"rendered":   rendered,
	})
}

// handleDataTuningApply 处理 POST /api/v1/data/tuning/apply。
//
// dryRun=true 返回渲染后的配置文本（200，不发任务）；否则下发系统任务，
// 将配置写入 /etc/<component>/conf/opsmesh-tuning.properties 并先做时间戳备份。
func (s *Server) handleDataTuningApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID  string              `json:"deviceID"`
		Component string              `json:"component"`
		Spec      datatuning.NodeSpec `json:"spec"`
		DryRun    bool                `json:"dryRun"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	component := strings.TrimSpace(req.Component)
	if !dataTuningComponents[component] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported component: " + req.Component})
		return
	}
	profile, err := datatuning.Recommend(req.Spec)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
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

	rendered := datatuning.RenderComponentProperties(profile, component)
	if rendered == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no tuning parameters for component: " + component})
		return
	}

	if req.DryRun {
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"dryRun":     true,
			"deviceID":   deviceID,
			"component":  component,
			"profile":    profile.Name,
			"components": profile.Components,
			"notes":      profile.Notes,
			"rendered":   rendered,
		})
		return
	}

	script := buildDataTuningApplyScript(component, rendered)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "data_tuning_apply_"+component)
	if err != nil {
		writeInternalError(r.Context(), w, "data_tuning_apply", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "data_tuning_apply",
		Target: component, Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(deviceID) + " component=" + opsSQLString(component) + " profile=" + opsSQLString(profile.Name)),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"taskID":    task.TaskID,
		"deviceID":  deviceID,
		"component": component,
		"profile":   profile.Name,
	})
}

// buildDataTuningApplyScript 生成落盘脚本：先时间戳备份，再写 heredoc 配置。
// component 已过白名单，rendered 来自确定性推荐结果，不含 shell 元字符。
func buildDataTuningApplyScript(component, rendered string) string {
	target := fmt.Sprintf("/etc/%s/conf/opsmesh-tuning.properties", component)
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "TARGET=%q\n", target)
	b.WriteString("mkdir -p \"$(dirname \"$TARGET\")\"\n")
	b.WriteString("if [ -f \"$TARGET\" ]; then cp -a \"$TARGET\" \"$TARGET.bak.$(date +%Y%m%d%H%M%S)\"; fi\n")
	b.WriteString("cat > \"$TARGET\" <<'OPSMESH_TUNING_EOF'\n")
	b.WriteString(rendered)
	b.WriteString("OPSMESH_TUNING_EOF\n")
	fmt.Fprintf(&b, "echo applied %s tuning to $TARGET\n", shellquote.Quote(component))
	return b.String()
}
