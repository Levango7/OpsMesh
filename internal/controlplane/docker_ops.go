// Package controlplane docker_ops.go 实现 Docker 运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDockerCompose 处理 POST /api/v1/docker/compose
func (s *Server) handleDockerCompose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // up/down/ps/logs/restart/pull/build
		Project  string `json:"project"`
		File     string `json:"file"`
		Service  string `json:"service"`
		Args     string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	project := req.Project
	if project == "" {
		project = "."
	}
	file := req.File
	if file == "" {
		file = "docker-compose.yml"
	}

	var script string
	switch req.Action {
	case "up":
		svc := ""
		if req.Service != "" {
			svc = " " + req.Service
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s up -d%s %s && echo "Compose up done"`, project, file, svc, req.Args)
	case "down":
		script = fmt.Sprintf(`cd %s && docker compose -f %s down %s && echo "Compose down done"`, project, file, req.Args)
	case "ps":
		script = fmt.Sprintf(`cd %s && docker compose -f %s ps %s`, project, file, req.Args)
	case "logs":
		svc := ""
		if req.Service != "" {
			svc = " " + req.Service
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s logs --tail 50%s %s`, project, file, svc, req.Args)
	case "restart":
		svc := ""
		if req.Service != "" {
			svc = " " + req.Service
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s restart%s %s && echo "Compose restart done"`, project, file, svc, req.Args)
	case "pull":
		svc := ""
		if req.Service != "" {
			svc = " " + req.Service
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s pull%s %s && echo "Compose pull done"`, project, file, svc, req.Args)
	case "build":
		svc := ""
		if req.Service != "" {
			svc = " " + req.Service
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s build%s %s && echo "Compose build done"`, project, file, svc, req.Args)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "docker_compose_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDockerNetwork 处理 POST /api/v1/docker/network
func (s *Server) handleDockerNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // ls/inspect/create/remove/connect/disconnect/prune
		Name      string `json:"name"`
		Driver    string `json:"driver"`
		Subnet    string `json:"subnet"`
		Gateway   string `json:"gateway"`
		Container string `json:"container"`
		Alias     string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "ls":
		script = `docker network ls 2>/dev/null || echo "Docker not available"`
	case "inspect":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`docker network inspect %s 2>/dev/null || echo "Network not found"`, req.Name)
	case "create":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		driver := req.Driver
		if driver == "" {
			driver = "bridge"
		}
		opts := ""
		if req.Subnet != "" {
			opts += " --subnet " + req.Subnet
		}
		if req.Gateway != "" {
			opts += " --gateway " + req.Gateway
		}
		script = fmt.Sprintf(`docker network create --driver %s%s %s && echo "Network %s created"`, driver, opts, req.Name, req.Name)
	case "remove":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`docker network rm %s && echo "Network %s removed"`, req.Name, req.Name)
	case "connect":
		if req.Name == "" || req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and container are required"})
			return
		}
		alias := ""
		if req.Alias != "" {
			alias = " --alias " + req.Alias
		}
		script = fmt.Sprintf(`docker network connect%s %s %s && echo "Connected %s to %s"`, alias, req.Name, req.Container, req.Container, req.Name)
	case "disconnect":
		if req.Name == "" || req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and container are required"})
			return
		}
		script = fmt.Sprintf(`docker network disconnect %s %s && echo "Disconnected %s from %s"`, req.Name, req.Container, req.Container, req.Name)
	case "prune":
		script = `docker network prune -f 2>&1 && echo "Unused networks pruned"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "docker_network_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDockerVolume 处理 POST /api/v1/docker/volume
func (s *Server) handleDockerVolume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // ls/inspect/create/remove/prune
		Name     string `json:"name"`
		Driver   string `json:"driver"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "ls":
		script = `docker volume ls 2>/dev/null || echo "Docker not available"`
	case "inspect":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`docker volume inspect %s 2>/dev/null || echo "Volume not found"`, req.Name)
	case "create":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		driver := req.Driver
		if driver == "" {
			driver = "local"
		}
		script = fmt.Sprintf(`docker volume create --driver %s %s && echo "Volume %s created"`, driver, req.Name, req.Name)
	case "remove":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`docker volume rm %s && echo "Volume %s removed"`, req.Name, req.Name)
	case "prune":
		script = `docker volume prune -f 2>&1 && echo "Unused volumes pruned"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "docker_volume_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDockerSystem 处理 POST /api/v1/docker/system
func (s *Server) handleDockerSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // df/prune/info/version
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "df":
		script = `docker system df 2>/dev/null || echo "Docker not available"`
	case "prune":
		script = `docker system prune -f 2>&1 && echo "System pruned"`
	case "info":
		script = `docker info 2>/dev/null || echo "Docker not available"`
	case "version":
		script = `docker version 2>/dev/null || echo "Docker not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "docker_system_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDockerDebug 处理 POST /api/v1/docker/debug
func (s *Server) handleDockerDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // exec/top/port/diff/cp/stats
		Container string `json:"container"`
		Command   string `json:"command"`
		Source    string `json:"source"`
		Dest      string `json:"dest"`
		Port      string `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "exec":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		cmd := req.Command
		if cmd == "" {
			cmd = "sh"
		}
		script = fmt.Sprintf(`docker exec %s %s 2>&1`, req.Container, cmd)
	case "top":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker top %s 2>/dev/null || echo "Container not found"`, req.Container)
	case "port":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		if req.Port != "" {
			script = fmt.Sprintf(`docker port %s %s 2>/dev/null || echo "Port not mapped"`, req.Container, req.Port)
		} else {
			script = fmt.Sprintf(`docker port %s 2>/dev/null || echo "No port mappings"`, req.Container)
		}
	case "diff":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker diff %s 2>/dev/null || echo "Container not found"`, req.Container)
	case "cp":
		if req.Source == "" || req.Dest == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "source and dest are required"})
			return
		}
		script = fmt.Sprintf(`docker cp %s %s && echo "Copied %s -> %s"`, req.Source, req.Dest, req.Source, req.Dest)
	case "stats":
		if req.Container != "" {
			script = fmt.Sprintf(`docker stats --no-stream %s 2>/dev/null || echo "Container not found"`, req.Container)
		} else {
			script = `docker stats --no-stream 2>/dev/null || echo "Docker not available"`
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "docker_debug_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDockerRegistry 处理 POST /api/v1/docker/registry
func (s *Server) handleDockerRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // login/logout/search
		Registry  string `json:"registry"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Image     string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	registry := req.Registry
	if registry == "" {
		registry = "docker.io"
	}

	var script string
	switch req.Action {
	case "login":
		if req.Username == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
			return
		}
		script = fmt.Sprintf(`echo "%s" | docker login %s -u %s --password-stdin && echo "Login success"`, req.Password, registry, req.Username)
	case "logout":
		script = fmt.Sprintf(`docker logout %s && echo "Logout success"`, registry)
	case "search":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`docker search %s 2>/dev/null || echo "Search failed"`, req.Image)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "docker_registry_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerDockerRoutes 注册 Docker 运维路由
func (s *Server) registerDockerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/docker/compose", s.handleDockerCompose)
	mux.HandleFunc("/api/v1/docker/network", s.handleDockerNetwork)
	mux.HandleFunc("/api/v1/docker/volume", s.handleDockerVolume)
	mux.HandleFunc("/api/v1/docker/system", s.handleDockerSystem)
	mux.HandleFunc("/api/v1/docker/debug", s.handleDockerDebug)
	mux.HandleFunc("/api/v1/docker/registry", s.handleDockerRegistry)
}