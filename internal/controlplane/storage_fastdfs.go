// Package controlplane storage_fastdfs.go 实现 FastDFS 分布式存储运维
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleFastDFS 处理 POST /api/v1/storage/fastdfs
func (s *Server) handleFastDFS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy-tracker/deploy-storage/status/upload/download/list/delete
		Tracker  string `json:"tracker"`
		Group    string `json:"group"`
		File     string `json:"file"`
		Remote   string `json:"remote"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy-tracker":
		script := `docker run -d --name fastdfs-tracker -p 22122:22122 -v /data/fastdfs/tracker:/var/fdfs delron/fastdfs:latest tracker 2>/dev/null && echo "FastDFS tracker deployed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_deploy-tracker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-tracker"})

	case "deploy-storage":
		tracker := req.Tracker
		if tracker == "" {
			tracker = "localhost"
		}
		script := fmt.Sprintf(`docker run -d --name fastdfs-storage -p 23000:23000 -p 8888:8888 -e TRACKER_SERVER=%s:22122 -v /data/fastdfs/storage:/var/fdfs delron/fastdfs:latest storage 2>/dev/null && echo "FastDFS storage deployed"`, tracker)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_deploy-storage")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-storage"})

	case "status":
		script := `docker ps --filter name=fastdfs --format "{{.Names}}: {{.Status}}" 2>/dev/null || echo "FastDFS not running"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "upload":
		if req.File == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
			return
		}
		script := fmt.Sprintf(`fdfs_upload_file /etc/fdfs/client.conf %s 2>/dev/null`, req.File)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_upload")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upload"})

	case "download":
		if req.Remote == "" || req.File == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "remote and file are required"})
			return
		}
		script := fmt.Sprintf(`fdfs_download_file /etc/fdfs/client.conf %s %s 2>/dev/null`, req.Remote, req.File)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_download")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "download"})

	case "list":
		script := `fdfs_monitor /etc/fdfs/client.conf 2>/dev/null | head -50 || echo "FastDFS not available"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "delete":
		if req.Remote == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "remote file id is required"})
			return
		}
		script := fmt.Sprintf(`fdfs_delete_file /etc/fdfs/client.conf %s 2>/dev/null`, req.Remote)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "fastdfs_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// dispatchFastDFSTask 封装租户上下文获取 + 任务创建
func (s *Server) dispatchFastDFSTask(w http.ResponseWriter, r *http.Request, deviceID, script, action string) (string, bool) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return "", false
	}
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "fastdfs.dispatch", err)
		return "", false
	}
	return task.TaskID, true
}

// RegisterFastDFSRoutes 注册 FastDFS 路由
func (s *Server) RegisterFastDFSRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/storage/fastdfs", s.handleFastDFS)
}
