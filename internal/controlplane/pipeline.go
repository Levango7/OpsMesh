package controlplane

// pipeline.go 实现 Phase 2 CI/CD 流水线 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/pipeline/templates           列出模板
//   - POST   /api/v1/pipeline/templates           创建模板
//   - GET    /api/v1/pipeline/templates/{id}      获取模板详情
//   - PUT    /api/v1/pipeline/templates/{id}      更新模板
//   - DELETE /api/v1/pipeline/templates/{id}      删除模板
//   - POST   /api/v1/pipeline/templates/{id}/run  触发运行
//   - GET    /api/v1/pipeline/runs                列出运行记录
//   - GET    /api/v1/pipeline/runs/{id}           获取运行详情
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 pipeline:read/pipeline:write 权限。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// handlePipelineTemplates 统一处理 /api/v1/pipeline/templates：
//   - GET：列出模板
//   - POST：创建模板
func (s *Server) handlePipelineTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPipelineTemplates(w, r)
	case http.MethodPost:
		s.handleCreatePipelineTemplate(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListPipelineTemplates 处理 GET /api/v1/pipeline/templates：列出模板。
func (s *Server) handleListPipelineTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	templates := s.store.ListTemplates(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"templates": templates})
}

// handleCreatePipelineTemplate 处理 POST /api/v1/pipeline/templates：创建模板。
func (s *Server) handleCreatePipelineTemplate(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.PipelineTemplate
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	// 租户隔离（P0-6）：模板的 AgentID 决定后续 pipeline 运行在哪个 agent 上执行
	// （processPendingPipelineRuns 按 tpl.AgentID 建任务，任务队列按 agent_id 寻址）。
	// 在模板写入侧即校验归属，避免跨租户模板成为长期潜伏的下发通道。
	if body.AgentID != "" {
		if _, ok := s.requireTenantAgent(w, body.AgentID, actx.TenantID); !ok {
			return
		}
	}
	created := s.store.CreateTemplate(actx.TenantID, &body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create template failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "pipeline_template_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handlePipelineTemplate 分派 /api/v1/pipeline/templates/{id} 子路径：
//   - GET    /api/v1/pipeline/templates/{id}        获取模板详情
//   - PUT    /api/v1/pipeline/templates/{id}        更新模板
//   - DELETE /api/v1/pipeline/templates/{id}        删除模板
//   - POST   /api/v1/pipeline/templates/{id}/run    触发运行
func (s *Server) handlePipelineTemplate(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/pipeline/templates/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetPipelineTemplate(w, r, id)
		case http.MethodPut:
			s.handleUpdatePipelineTemplate(w, r, id)
		case http.MethodDelete:
			s.handleDeletePipelineTemplate(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "run":
		s.handleRunPipelineTemplate(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetPipelineTemplate 处理 GET /api/v1/pipeline/templates/{id}：获取详情。
func (s *Server) handleGetPipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	t, ok := s.store.GetTemplate(actx.TenantID, id)
	if !ok || t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, t)
}

// handleUpdatePipelineTemplate 处理 PUT /api/v1/pipeline/templates/{id}：更新模板。
// PipelineStore 无 UpdateTemplate 方法，用 Delete + Create 模拟（保留原 ID）。
func (s *Server) handleUpdatePipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	existing, ok := s.store.GetTemplate(actx.TenantID, id)
	if !ok || existing == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body store.PipelineTemplate
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// 租户隔离（P0-6）：同创建路径，模板 AgentID 必须属于本租户。
	// 空 AgentID 不在校验范围（保持 PUT 全量替换语义：清空执行目标由运行期分支处理）。
	if body.AgentID != "" {
		if _, ok := s.requireTenantAgent(w, body.AgentID, actx.TenantID); !ok {
			return
		}
	}
	body.ID = id
	body.TenantID = actx.TenantID
	body.CreatedAt = existing.CreatedAt
	if !s.store.DeleteTemplate(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "update template failed (delete step)"})
		return
	}
	updated := s.store.CreateTemplate(actx.TenantID, &body)
	if updated == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "update template failed (create step)"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "pipeline_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeletePipelineTemplate 处理 DELETE /api/v1/pipeline/templates/{id}：删除模板。
func (s *Server) handleDeletePipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteTemplate(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "pipeline_template_delete", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleRunPipelineTemplate 处理 POST /api/v1/pipeline/templates/{id}/run：触发运行。
// 真实执行：创建 pending 记录后由后台 pipelineExecutor 推进状态。
func (s *Server) handleRunPipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	tpl, ok := s.store.GetTemplate(actx.TenantID, id)
	if !ok || tpl == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		Parameters map[string]string `json:"parameters"`
	}
	// 请求体可选（可无参数触发）：解析失败不 4xx 拒绝，body 保持零值继续，
	// 但畸形请求体必须留痕（非静默吞错）。
	if err := decodeJSONBody(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		logx.Warn(r.Context(), "controlplane: trigger run: 可选请求体解析失败（按无参数继续）", err)
	}
	now := time.Now()
	run := &store.PipelineRun{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		Status:       "pending",
		Parameters:   body.Parameters,
		StartedAt:    &now,
	}
	created := s.store.CreateRun(actx.TenantID, run)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create run failed"})
		return
	}
	// 审计：记录触发人。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "pipeline_run", Target: created.ID, Detail: sanitizeAuditDetail("template=" + tpl.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// startPipelineExecutor 启动后台 pipeline 执行器，周期推进 pending→running→succeeded。
func (s *Server) startPipelineExecutor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.processPendingPipelineRuns()
			}
		}
	}()
}

// processPendingPipelineRuns 扫描并推进所有 pending 状态的 pipeline 运行记录。
func (s *Server) processPendingPipelineRuns() {
	// 扫描所有租户的 pending 运行记录。
	allRuns := s.store.ListRuns("", "")
	for _, run := range allRuns {
		if run.Status != "pending" {
			continue
		}
		tenantID := run.TenantID
		if tenantID == "" {
			tenantID = "default"
		}
		// 推进到 running。
		now := time.Now()
		run.Status = "running"
		run.StartedAt = &now
		if _, ok := s.store.UpdateRun(tenantID, run); !ok {
			continue
		}

		// 读取模板，创建执行任务。
		tpl, ok := s.store.GetTemplate(tenantID, run.TemplateID)
		if !ok || tpl == nil {
			run.Status = "failed"
			finished := time.Now()
			run.FinishedAt = &finished
			run.Logs = "template not found"
			s.store.UpdateRun(tenantID, run)
			continue
		}
		// 模板须指定执行 agent（AgentID 非空），否则任务无法认领下发。
		if tpl.AgentID == "" {
			run.Status = "failed"
			finished := time.Now()
			run.FinishedAt = &finished
			run.Logs = "template agentID not set"
			s.store.UpdateRun(tenantID, run)
			continue
		}
		// 租户隔离（P0-6）：模板 AgentID 必须属于运行所属租户。
		// 写入侧（handleCreatePipelineTemplate/handleUpdatePipelineTemplate）已校验，
		// 此处对「本修复上线前创建的存量模板」兜底：命中即判失败并留可诊断日志，
		// 绝不下发跨租户任务（任务队列按 agent_id 寻址，下发即等于把命令交给对方 agent）。
		if _, err := tenantAgentIn(s.store, tpl.AgentID, tenantID); err != nil {
			run.Status = "failed"
			finished := time.Now()
			run.FinishedAt = &finished
			run.Logs = "template agentID rejected: " + err.Error()
			s.store.UpdateRun(tenantID, run)
			continue
		}

		// 创建 shell 任务执行 pipeline（从模板 YAML 提取命令）。
		// 任务 AgentID 认领到模板指定 agent；ParentID 关联到 run.ID 供状态对账（reconcileRunStatus）。
		task := &proto.Task{
			Type:     "shell",
			Command:  extractPipelineCommand(tpl),
			AgentID:  tpl.AgentID,
			TenantID: tenantID,
			Status:   "pending",
			ParentID: run.ID,
		}
		created, err := s.store.CreateTask(task)
		if err != nil {
			// 缺口 3：执行任务没落库 = 这次 pipeline 运行实际不存在，标 failed 并把
			// store 的错误写进 logs（此前 created==nil 分支够不着真实失败：失败时
			// store 返回的是入参而非 nil，会被当成成功）。
			run.Status = "failed"
			finished := time.Now()
			run.FinishedAt = &finished
			run.Logs = "failed to create execution task: " + err.Error()
			s.store.UpdateRun(tenantID, run)
			continue
		}
		if created == nil || created.TaskID == "" {
			run.Status = "failed"
			finished := time.Now()
			run.FinishedAt = &finished
			run.Logs = "failed to create execution task"
			s.store.UpdateRun(tenantID, run)
			continue
		}

		// 任务已下发，run 保持 running（终态由 reconcileRunStatus 按子任务状态推导，不再立即 succeeded）。
		run.Logs = "execution task created: " + opsSQLString(created.TaskID)
		s.store.UpdateRun(tenantID, run)
	}
}

// reconcileRunStatus 按 run 的子任务（TasksByParent）推导运行终态。
//
// 规则：任一子任务 failed/cancelled → failed；全部 done → succeeded；否则 running
// （含无子任务的异常态——任务尚未创建或已全部清理，视为进行中）。
// 供 run 详情/列表 handler 查询时映射，不持久化（run 存储状态由 executor 维护为 running）。
func (s *Server) reconcileRunStatus(runID string) string {
	if s.store == nil || runID == "" {
		return "running"
	}
	tasks := s.store.TasksByParent(runID)
	if len(tasks) == 0 {
		return "running"
	}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if t.Status == "failed" || t.Status == "cancelled" {
			return "failed"
		}
	}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if t.Status != "done" {
			return "running"
		}
	}
	return "succeeded"
}

const (
	// maxPipelineScriptBytes / maxPipelineScriptLines 限制从模板提取出的脚本体积，
	// 防止超长 YAML 把 shell 任务的 Command 撑爆（下发/存储/日志均按此上限假设）。
	maxPipelineScriptBytes = 64 * 1024
	maxPipelineScriptLines = 1000
)

// pipelineScriptKeyRe 匹配可承载脚本的 YAML 键：script/sh/command/run。
// 允许出现在块序列项中（如 "- script: |"）。
var pipelineScriptKeyRe = regexp.MustCompile(`^\s*(?:-\s+)?(?:script|sh|command|run)\s*:\s*(.*)$`)

// pipelineListItemRe 匹配块序列项 "- value"。
var pipelineListItemRe = regexp.MustCompile(`^\s*-\s*(.*)$`)

// extractPipelineCommand 从 PipelineTemplate 中提取执行命令。
// 支持 Tekton/Jenkins YAML 格式，提取 script/sh/command/run 字段：
//   - 单行值（script: echo hi）原样返回；
//   - 块标量（| / >，含 |- / |+ / >- / >+ 等 chomping 变体）按缩进收集后续行，
//     去掉公共缩进后用换行（|）或空格（>）连接；
//   - 列表形式（script:\n  - line1\n  - line2）用换行连接。
//
// 若无法解析，回退到第一行非注释内容。
func extractPipelineCommand(tpl *store.PipelineTemplate) string {
	if tpl.YAML == "" {
		return fmt.Sprintf(`echo %s`, opsEcho("pipeline %s started", tpl.Name))
	}
	lines := strings.Split(tpl.YAML, "\n")
	if cmd, ok := extractYAMLScript(lines); ok {
		return truncatePipelineScript(cmd)
	}
	// 回退：第一行非空非注释内容
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return trimmed
		}
	}
	return fmt.Sprintf(`echo %s`, opsEcho("pipeline %s started", tpl.Name))
}

// extractYAMLScript 按行扫描 YAML，返回首个可识别的脚本内容。
// 识别顺序与旧实现一致：靠前的 script/sh/command/run 键优先。
func extractYAMLScript(lines []string) (string, bool) {
	for i, line := range lines {
		m := pipelineScriptKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		value := strings.TrimSpace(m[1])
		keyIndent := pipelineIndent(line)
		if value == "" {
			// 列表形式：script:\n  - line one\n  - line two
			if items := collectPipelineListItems(lines, i+1, keyIndent); len(items) > 0 {
				return strings.Join(items, "\n"), true
			}
			continue
		}
		if value[0] != '|' && value[0] != '>' {
			return value, true
		}
		// 块标量：|（literal）或 >（folded）。chomping 指示符（+/-）按基样式处理，
		// 统一裁掉块末尾的空行（等价于 clip 语义）。
		block := deindentPipelineBlock(collectPipelineBlockLines(lines, i+1, keyIndent))
		if len(block) == 0 {
			continue
		}
		if value[0] == '>' {
			return foldPipelineBlock(block), true
		}
		return strings.Join(block, "\n"), true
	}
	return "", false
}

// collectPipelineBlockLines 收集 keyIndent 之后所有缩进更深的行（含块内空行）。
// 遇到非空且缩进不深于键的行即视为块结束；块末尾空行被裁掉。
func collectPipelineBlockLines(lines []string, start, keyIndent int) []string {
	var block []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			block = append(block, "")
			continue
		}
		if pipelineIndent(line) <= keyIndent {
			break
		}
		block = append(block, line)
	}
	for len(block) > 0 && block[len(block)-1] == "" {
		block = block[:len(block)-1]
	}
	return block
}

// deindentPipelineBlock 按非空行的最小公共缩进整体左移。
func deindentPipelineBlock(lines []string) []string {
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := pipelineIndent(line)
		if minIndent < 0 || n < minIndent {
			minIndent = n
		}
	}
	if minIndent < 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		if len(line) >= minIndent {
			out = append(out, line[minIndent:])
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	return out
}

// foldPipelineBlock 按 folded 语义连接块内行：相邻非空行之间折成单个空格；
// 空行保留为换行。这是典型 CI 对 ">" 的简化语义（不做段落级换行折叠）。
func foldPipelineBlock(lines []string) string {
	var b strings.Builder
	prevText := false
	for i, line := range lines {
		if line == "" {
			if i > 0 && prevText {
				b.WriteByte('\n')
			}
			prevText = false
			continue
		}
		if prevText {
			b.WriteByte(' ')
		}
		b.WriteString(line)
		prevText = true
	}
	return b.String()
}

// collectPipelineListItems 收集 keyIndent 之后更深缩进的 "- item" 行。
func collectPipelineListItems(lines []string, start, keyIndent int) []string {
	var items []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if pipelineIndent(line) <= keyIndent {
			break
		}
		m := pipelineListItemRe.FindStringSubmatch(line)
		if m == nil {
			break
		}
		if item := strings.TrimSpace(m[1]); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// pipelineIndent 返回行首空白宽度（空格/制表符各计 1）。
func pipelineIndent(line string) int {
	n := 0
	for _, r := range line {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

// truncatePipelineScript 按 64 KiB / 1000 行上限截断脚本；字节截断点回退到
// 合法 UTF-8 边界，避免下发半个字符。
func truncatePipelineScript(s string) string {
	if lines := strings.Split(s, "\n"); len(lines) > maxPipelineScriptLines {
		s = strings.Join(lines[:maxPipelineScriptLines], "\n")
	}
	if len(s) > maxPipelineScriptBytes {
		s = s[:maxPipelineScriptBytes]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// handlePipelineRuns 统一处理 /api/v1/pipeline/runs：
//   - GET：列出运行记录（支持 ?templateID= 过滤）
func (s *Server) handlePipelineRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPipelineRuns(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListPipelineRuns 处理 GET /api/v1/pipeline/runs：列出运行记录。
func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	templateID := r.URL.Query().Get("templateID")
	runs := s.store.ListRuns(actx.TenantID, templateID)
	// 查询时按子任务状态对账：派生 run.Status（failed/succeeded/running）。
	for _, run := range runs {
		if run == nil {
			continue
		}
		run.Status = s.reconcileRunStatus(run.ID)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// handlePipelineRun 处理 /api/v1/pipeline/runs/{id}：获取运行详情。
func (s *Server) handlePipelineRun(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/pipeline/runs/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "run id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "run id required"})
		return
	}
	if len(parts) > 1 {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + parts[1]})
		return
	}
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	run, ok := s.store.GetRun(actx.TenantID, id)
	if !ok || run == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}
	// 查询时按子任务状态对账：派生 run.Status（failed/succeeded/running）。
	run.Status = s.reconcileRunStatus(run.ID)
	paginate.WriteJSON(w, http.StatusOK, run)
}
