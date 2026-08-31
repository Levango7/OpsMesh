package controlplane

// backup_api.go 实现 Phase 3 灾备恢复 HTTP handler（真实备份/恢复）。
//
// 说明：backup.go（CLI 子命令）实现 ExportBackup/ImportBackup 的 JSON 快照读写逻辑；
// 本文件实现 HTTP API 端点，归档内容与 CLI 同源——导出 store 领域数据 JSON 快照
// + metadata.json 打包为 tar.gz 写入 data/backups/（Server.backupDir）。
//
// API 端点：
//   - POST   /api/v1/backup/create   创建备份（请求体：{type: "full"|"config"|"devices"|"tasks"}）
//   - GET    /api/v1/backup/list     列出备份记录
//   - POST   /api/v1/backup/restore  恢复备份（请求体：{id}）
//   - DELETE /api/v1/backup/         删除备份（子路径：{id}）
//
// 备份流程（真实实现，非模拟）：
//   - 创建：落库 status=creating → 后台 goroutine 归档（defer recover 兜底）→
//     更新 status=completed + 真实 Size/Path；失败置 status=failed。
//   - 恢复：读归档 snapshot.json 走现有 store 接口写回（SetConfig/UpsertDevice/Register/
//     CreateTask/CreateAlertRule/CreateTemplate/CreateAutomationRule），返回真实结果；
//     备份记录不存在或归档文件缺失返回 404。

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// backupSnapshotMeta 备份归档元信息（metadata.json 内容）。
type backupSnapshotMeta struct {
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Format    string    `json:"format"` // json
	Type      string    `json:"type"`   // full/config/devices/tasks
	TenantID  string    `json:"tenantID"`
	BackupID  string    `json:"backupID"`
}

// backupSnapshot 备份归档 JSON 快照（snapshot.json 内容）。
// 字段与 store 领域数据对应，restore 时按字段写回对应 store 接口。
type backupSnapshot struct {
	Meta            backupSnapshotMeta        `json:"meta"`
	Configs         []*store.ConfigItem       `json:"configs,omitempty"`
	Devices         []proto.DeviceInfo        `json:"devices,omitempty"`
	Agents          []*proto.AgentInfo        `json:"agents,omitempty"`
	Tasks           []*proto.Task             `json:"tasks,omitempty"`
	AlertRules      []*store.AlertRule        `json:"alertRules,omitempty"`
	Templates       []*store.PipelineTemplate `json:"templates,omitempty"`
	AutomationRules []*store.AutomationRule   `json:"automationRules,omitempty"`
}

// handleBackupCreate 处理 POST /api/v1/backup/create：创建备份。
//
// 请求体：{"type": "full"|"config"|"devices"|"tasks"}
// 落库 creating 后由后台 goroutine 异步归档（不阻塞 HTTP 响应）。
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "backup:write"); !ok {
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
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Type string `json:"type"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	validTypes := map[string]bool{"full": true, "config": true, "devices": true, "tasks": true}
	if !validTypes[body.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid type (must be full/config/devices/tasks)"})
		return
	}
	rec := &store.BackupRecord{
		Type:   body.Type,
		Status: "creating",
	}
	created := s.store.CreateBackup(actx.TenantID, rec)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create backup failed"})
		return
	}
	// 后台 goroutine 归档：导出 JSON 快照打包 tar.gz 写入 backupDir，完成后更新 status=completed。
	// DATA RACE 修复（CI -race 实测捕获）：此前把同一 created 指针既交给 goroutine 写
	// （runBackupAsync 更新 Status/Path）又在下方同步 JSON 序列化——响应写一半后台
	// 已并发改同一结构体。goroutine 持独立副本（值拷贝结构体仅含值字段，浅拷贝即安全），
	// created 保持只读直至序列化完成。
	asyncRec := *created
	go s.runBackupAsync(actx.TenantID, &asyncRec)
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// runBackupAsync 后台归档备份（defer recover 兜底，panic 时置 failed）。
func (s *Server) runBackupAsync(tenantID string, rec *store.BackupRecord) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("controlplane: backup async panic recovered (id=%s): %v", rec.ID, p)
			rec.Status = "failed"
			rec.Path = ""
			s.store.UpdateBackup(tenantID, rec)
		}
	}()
	snap := s.buildBackupSnapshot(tenantID, rec)
	path, size, err := writeBackupArchive(snap, s.effectiveBackupDir(), rec.ID)
	if err != nil {
		log.Printf("controlplane: backup archive failed (id=%s): %v", rec.ID, err)
		rec.Status = "failed"
		s.store.UpdateBackup(tenantID, rec)
		return
	}
	rec.Status = "completed"
	rec.Path = path
	rec.Size = size
	s.store.UpdateBackup(tenantID, rec)
}

// buildBackupSnapshot 导出 store 领域数据为 JSON 快照（跨租户，tenantID 记为元信息）。
func (s *Server) buildBackupSnapshot(tenantID string, rec *store.BackupRecord) *backupSnapshot {
	snap := &backupSnapshot{
		Meta: backupSnapshotMeta{
			Version:   "1",
			CreatedAt: time.Now(),
			Format:    "json",
			Type:      rec.Type,
			TenantID:  tenantID,
			BackupID:  rec.ID,
		},
	}
	// 配置中心：default 租户配置（平台配置/配额等 key 均存于 default）。
	snap.Configs = s.store.ListConfigs("default")
	// 设备：Snapshot("") 返回 segment -> []DeviceInfo，展平。
	for _, list := range s.store.Snapshot("") {
		snap.Devices = append(snap.Devices, list...)
	}
	// 领域数据：agents/tasks/alertRules/templates/automationRules 全量（跨租户）。
	snap.Agents = s.store.Agents("")
	snap.Tasks = s.store.AllTasks("")
	snap.AlertRules = s.store.ListAlertRules("")
	snap.Templates = s.store.ListTemplates("")
	snap.AutomationRules = s.store.ListAutomationRules("")
	return snap
}

// effectiveBackupDir 返回备份归档目录：优先 Server.backupDir（NewServer 注入 data/backups），
// 空值时回退 os.TempDir()/opsmesh-backups（测试/直构 Server 兜底）。
func (s *Server) effectiveBackupDir() string {
	if s.backupDir != "" {
		return s.backupDir
	}
	return filepath.Join(os.TempDir(), "opsmesh-backups")
}

// writeBackupArchive 把快照打包为 backup-<ts>-<id>.tar.gz 写入 dir，返回文件路径与字节大小。
// 归档内含 metadata.json（元信息）+ snapshot.json（业务数据快照）。
func writeBackupArchive(snap *backupSnapshot, dir, id string) (string, int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create backup dir: %w", err)
	}
	name := fmt.Sprintf("backup-%s-%s.tar.gz", time.Now().Format("20060102-150405"), id)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", 0, fmt.Errorf("create archive file: %w", err)
	}
	defer f.Close()

	counter := &byteCounter{w: f}
	gz := gzip.NewWriter(counter)
	tw := tar.NewWriter(gz)

	// metadata.json
	metaJSON, err := json.MarshalIndent(snap.Meta, "", "  ")
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return "", 0, err
	}
	if err := writeTarEntry(tw, "metadata.json", metaJSON); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return "", 0, err
	}
	// snapshot.json
	snapJSON, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return "", 0, err
	}
	if err := writeTarEntry(tw, "snapshot.json", snapJSON); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return "", 0, err
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return "", 0, err
	}
	if err := gz.Close(); err != nil {
		return "", 0, err
	}
	return path, counter.n, nil
}

// writeTarEntry 向 tar.Writer 写入一个内容为 data 的文本文件条目。
func writeTarEntry(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     int64(len(data)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// byteCounter 计数写入总字节数（用于备份文件真实 Size）。
type byteCounter struct {
	w io.Writer
	n int64
}

// Write 实现 io.Writer，累计字节数后透传。
func (c *byteCounter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// readBackupArchive 读取归档，解析 snapshot.json 为 *backupSnapshot。
func readBackupArchive(path string) (*backupSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var snap backupSnapshot
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if strings.TrimSuffix(filepath.Base(hdr.Name), "/") != "snapshot.json" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read snapshot.json: %w", err)
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, fmt.Errorf("parse snapshot.json: %w", err)
		}
		break
	}
	if len(snap.Meta.BackupID) == 0 && snap.Meta.CreatedAt.IsZero() {
		return nil, fmt.Errorf("archive missing snapshot.json metadata (corrupt backup)")
	}
	return &snap, nil
}

// restoreSnapshot 把快照数据写回 store（走现有 store 接口），返回各类写回计数。
func (s *Server) restoreSnapshot(snap *backupSnapshot) map[string]int {
	counts := map[string]int{}
	if snap == nil {
		return counts
	}
	// configs：SetConfig 幂等。
	for _, item := range snap.Configs {
		if item == nil || item.Key == "" {
			continue
		}
		s.store.SetConfig(item)
		counts["configs"]++
	}
	// devices：UpsertDevice 幂等。
	for i := range snap.Devices {
		d := snap.Devices[i]
		s.store.UpsertDevice(&d)
		counts["devices"]++
	}
	// agents：Register 幂等（按 agentID）。
	for _, a := range snap.Agents {
		if a == nil || a.AgentID == "" {
			continue
		}
		ac := *a
		s.store.Register(&ac)
		counts["agents"]++
	}
	// tasks：CreateTask（AgentID 空的任务不可入队，跳过）。
	for _, t := range snap.Tasks {
		if t == nil || t.AgentID == "" {
			continue
		}
		tc := *t
		s.store.CreateTask(&tc)
		counts["tasks"]++
	}
	// alertRules：CreateAlertRule 幂等（保留原 ID）。
	for _, r := range snap.AlertRules {
		if r == nil {
			continue
		}
		rc := *r
		s.store.CreateAlertRule(&rc)
		counts["alertRules"]++
	}
	// templates：CreateTemplate。
	for _, t := range snap.Templates {
		if t == nil {
			continue
		}
		tc := *t
		s.store.CreateTemplate(tc.TenantID, &tc)
		counts["templates"]++
	}
	// automationRules：CreateAutomationRule。
	for _, r := range snap.AutomationRules {
		if r == nil {
			continue
		}
		rc := *r
		s.store.CreateAutomationRule(rc.TenantID, &rc)
		counts["automationRules"]++
	}
	return counts
}

// handleBackupList 处理 GET /api/v1/backup/list：列出备份记录。
func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "backup:read"); !ok {
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
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	backups := s.store.ListBackups(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"backups": backups})
}

// handleBackupRestore 处理 POST /api/v1/backup/restore：恢复备份。
//
// 请求体：{"id": "..."}
// 真实恢复：读归档 snapshot.json 写回 store，返回各类恢复计数；
// 备份记录不存在或归档文件缺失返回 404。
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "backup:write"); !ok {
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
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.ID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	rec, ok := s.store.GetBackup(actx.TenantID, body.ID)
	if !ok || rec == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "backup not found"})
		return
	}
	if rec.Path == "" {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "backup archive missing (path empty)"})
		return
	}
	if _, err := os.Stat(rec.Path); err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "backup archive file not found: " + rec.Path})
		return
	}
	snap, err := readBackupArchive(rec.Path)
	if err != nil {
		writeInternalError(r.Context(), w, "backup.readArchive", err)
		return
	}
	counts := s.restoreSnapshot(snap)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "restored",
		"backup":      rec,
		"restored":    counts,
		"completedAt": time.Now(),
	})
}

// handleBackupDeleteRouting 分派 DELETE /api/v1/backup/{id}：删除备份。
func (s *Server) handleBackupDeleteRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/backup/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "backup id required"})
		return
	}
	id := rest
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "backup id required"})
		return
	}
	if r.Method != http.MethodDelete {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "backup:write"); !ok {
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
	if !s.store.DeleteBackup(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "backup not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
