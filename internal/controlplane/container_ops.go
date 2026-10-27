// Package controlplane container_ops.go 实现容器与镜像管理
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleContainer 处理 POST /api/v1/container
func (s *Server) handleContainer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/inspect/start/stop/restart/remove/logs
		Container string `json:"container"`
		Image    string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `docker ps -a --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null || echo "Docker not available"`
	case "inspect":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker inspect %s 2>/dev/null | head -40 || echo "Container not found"`, req.Container)
	case "start":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker start %s && echo "Container %s started"`, req.Container, req.Container)
	case "stop":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker stop %s && echo "Container %s stopped"`, req.Container, req.Container)
	case "restart":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker restart %s && echo "Container %s restarted"`, req.Container, req.Container)
	case "remove":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker rm -f %s && echo "Container %s removed"`, req.Container, req.Container)
	case "logs":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker logs --tail 50 %s 2>&1`, req.Container)
	case "stats":
		script = `docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}" 2>/dev/null || echo "Docker not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "container_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleImage 处理 POST /api/v1/container/image
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/pull/push/build/scan/sign/tag
		Image    string `json:"image"`
		Tag      string `json:"tag"`
		Dockerfile string `json:"dockerfile"`
		Context  string `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `docker images --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}" 2>/dev/null || echo "Docker not available"`
	case "pull":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`docker pull %s && echo "Pulled %s"`, req.Image, req.Image)
	case "push":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`docker push %s && echo "Pushed %s"`, req.Image, req.Image)
	case "build":
		if req.Image == "" || req.Dockerfile == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image and dockerfile are required"})
			return
		}
		ctx := req.Context
		if ctx == "" {
			ctx = "."
		}
		script = fmt.Sprintf(`docker build -t %s -f %s %s 2>&1 | tail -20 && echo "Built %s"`, req.Image, req.Dockerfile, ctx, req.Image)
	case "scan":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`trivy image %s 2>/dev/null || echo "Trivy not available"`, req.Image)
	case "sign":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`cosign sign %s 2>/dev/null && echo "Signed %s" || echo "Cosign not available"`, req.Image, req.Image)
	case "tag":
		if req.Image == "" || req.Tag == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image and tag are required"})
			return
		}
		script = fmt.Sprintf(`docker tag %s %s && echo "Tagged %s as %s"`, req.Image, req.Tag, req.Image, req.Tag)
	case "remove":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`docker rmi %s && echo "Removed %s"`, req.Image, req.Image)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "image_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerContainerRoutes 注册容器与镜像路由
func (s *Server) registerContainerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/container", s.handleContainer)
	mux.HandleFunc("/api/v1/container/image", s.handleImage)
}
