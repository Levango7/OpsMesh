// Package controlplane docker_ops.go 实现 Docker 运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// handleDockerCompose 处理 POST /api/v1/docker/compose
//
// 注入面（批次 8）：
//   - project / file / service 是单 token 字段 → shellquote.Quote；
//   - args 是**自由参数串**（`--scale web=3` 本就是多个词，不能用 Quote 压成一个词）
//     → shellquote.ValidateArgList 白名单校验，任何 shell 元字符一律 400 拒收。
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

	// args 是自由参数串：白名单校验（批次 8）。裸拼可被执行（`--foo; rm -rf /`）。
	if err := shellquote.ValidateArgList(req.Args, 512); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid args: " + err.Error()})
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
			svc = " " + shellquote.Quote(req.Service)
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s up -d%s %s && echo "Compose up done"`, shellquote.Quote(project), shellquote.Quote(file), svc, req.Args)
	case "down":
		script = fmt.Sprintf(`cd %s && docker compose -f %s down %s && echo "Compose down done"`, shellquote.Quote(project), shellquote.Quote(file), req.Args)
	case "ps":
		script = fmt.Sprintf(`cd %s && docker compose -f %s ps %s`, shellquote.Quote(project), shellquote.Quote(file), req.Args)
	case "logs":
		svc := ""
		if req.Service != "" {
			svc = " " + shellquote.Quote(req.Service)
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s logs --tail 50%s %s`, shellquote.Quote(project), shellquote.Quote(file), svc, req.Args)
	case "restart":
		svc := ""
		if req.Service != "" {
			svc = " " + shellquote.Quote(req.Service)
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s restart%s %s && echo "Compose restart done"`, shellquote.Quote(project), shellquote.Quote(file), svc, req.Args)
	case "pull":
		svc := ""
		if req.Service != "" {
			svc = " " + shellquote.Quote(req.Service)
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s pull%s %s && echo "Compose pull done"`, shellquote.Quote(project), shellquote.Quote(file), svc, req.Args)
	case "build":
		svc := ""
		if req.Service != "" {
			svc = " " + shellquote.Quote(req.Service)
		}
		script = fmt.Sprintf(`cd %s && docker compose -f %s build%s %s && echo "Compose build done"`, shellquote.Quote(project), shellquote.Quote(file), svc, req.Args)
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
		script = fmt.Sprintf(`docker network inspect %s 2>/dev/null || echo "Network not found"`, shellquote.Quote(req.Name))
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
			opts += " --subnet " + shellquote.Quote(req.Subnet)
		}
		if req.Gateway != "" {
			opts += " --gateway " + shellquote.Quote(req.Gateway)
		}
		script = fmt.Sprintf(`docker network create --driver %s%s %s && echo "Network %s created"`, shellquote.Quote(driver), opts, shellquote.Quote(req.Name), shellquote.Quote(req.Name))
	case "remove":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`docker network rm %s && echo "Network %s removed"`, shellquote.Quote(req.Name), shellquote.Quote(req.Name))
	case "connect":
		if req.Name == "" || req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and container are required"})
			return
		}
		alias := ""
		if req.Alias != "" {
			alias = " --alias " + shellquote.Quote(req.Alias)
		}
		script = fmt.Sprintf(`docker network connect%s %s %s && echo "Connected %s to %s"`, alias, shellquote.Quote(req.Name), shellquote.Quote(req.Container), shellquote.Quote(req.Container), shellquote.Quote(req.Name))
	case "disconnect":
		if req.Name == "" || req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and container are required"})
			return
		}
		script = fmt.Sprintf(`docker network disconnect %s %s && echo "Disconnected %s from %s"`, shellquote.Quote(req.Name), shellquote.Quote(req.Container), shellquote.Quote(req.Container), shellquote.Quote(req.Name))
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
		script = fmt.Sprintf(`docker volume inspect %s 2>/dev/null || echo "Volume not found"`, shellquote.Quote(req.Name))
	case "create":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		driver := req.Driver
		if driver == "" {
			driver = "local"
		}
		script = fmt.Sprintf(`docker volume create --driver %s %s && echo "Volume %s created"`, shellquote.Quote(driver), shellquote.Quote(req.Name), shellquote.Quote(req.Name))
	case "remove":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`docker volume rm %s && echo "Volume %s removed"`, shellquote.Quote(req.Name), shellquote.Quote(req.Name))
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
		// 命令串是自由参数（容器内执行）：白名单校验（批次 8）。
		// 收窄的代价（含引号/管道的命令串会被拒）是有意的——命令串若原样裸拼，
		// 注入会落在 **agent 宿主**上（越出容器边界）；复杂命令应改走
		// 「先落脚本文件、再执行文件」。
		if err := shellquote.ValidateArgList(cmd, 512); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid command: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`docker exec %s %s 2>&1`, shellquote.Quote(req.Container), cmd)
	case "top":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker top %s 2>/dev/null || echo "Container not found"`, shellquote.Quote(req.Container))
	case "port":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		if req.Port != "" {
			script = fmt.Sprintf(`docker port %s %s 2>/dev/null || echo "Port not mapped"`, shellquote.Quote(req.Container), shellquote.Quote(req.Port))
		} else {
			script = fmt.Sprintf(`docker port %s 2>/dev/null || echo "No port mappings"`, shellquote.Quote(req.Container))
		}
	case "diff":
		if req.Container == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
			return
		}
		script = fmt.Sprintf(`docker diff %s 2>/dev/null || echo "Container not found"`, shellquote.Quote(req.Container))
	case "cp":
		if req.Source == "" || req.Dest == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "source and dest are required"})
			return
		}
		script = fmt.Sprintf(`docker cp %s %s && echo "Copied %s -> %s"`, shellquote.Quote(req.Source), shellquote.Quote(req.Dest), shellquote.Quote(req.Source), shellquote.Quote(req.Dest))
	case "stats":
		if req.Container != "" {
			script = fmt.Sprintf(`docker stats --no-stream %s 2>/dev/null || echo "Container not found"`, shellquote.Quote(req.Container))
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
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // login/logout/search
		Registry string `json:"registry"`
		Username string `json:"username"`
		Password string `json:"password"`
		Image    string `json:"image"`
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
		script = fmt.Sprintf(`echo %s | docker login %s -u %s --password-stdin && echo "Login success"`, shellquote.Quote(req.Password), shellquote.Quote(registry), shellquote.Quote(req.Username))
	case "logout":
		script = fmt.Sprintf(`docker logout %s && echo "Logout success"`, shellquote.Quote(registry))
	case "search":
		if req.Image == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		script = fmt.Sprintf(`docker search %s 2>/dev/null || echo "Search failed"`, shellquote.Quote(req.Image))
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
