// Package controlplane cicd_ops.go 实现 CI/CD 流水线运维
// 支持：Jenkins、GitLab CI/CD、GitHub Actions、Tekton、Argo Workflows
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// Jenkins 管理
// ============================================================================

// handleJenkins 处理 POST /api/v1/cicd/jenkins
func (s *Server) handleJenkins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/status/job-list/job-build/job-disable/job-enable/plugin-list
		Version  string `json:"version"`
		Job      string `json:"job"`
		Params   string `json:"params"` // JSON params
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "lts-jdk17"
		}
		script = fmt.Sprintf(`docker run -d --name jenkins -p 8080:8080 -p 50000:50000 -v jenkins-data:/var/jenkins_home jenkins/jenkins:%s 2>/dev/null && echo "Jenkins %s deployed"`, version, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script = `curl -s http://localhost:8080/api/json 2>/dev/null | head -5 || echo "Jenkins not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "job-list":
		script = `curl -s "http://localhost:8080/api/json?tree=jobs[name,color]" 2>/dev/null || echo "Jenkins not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_job-list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "job-list"})

	case "job-build":
		if req.Job == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST "http://localhost:8080/job/%s/build" 2>/dev/null && echo "Build triggered for %s" || echo "Jenkins not available"`, req.Job, req.Job)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_job-build")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "job-build"})

	case "job-disable":
		if req.Job == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST "http://localhost:8080/job/%s/disable" 2>/dev/null && echo "Job %s disabled" || echo "Jenkins not available"`, req.Job, req.Job)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_job-disable")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "job-disable"})

	case "job-enable":
		if req.Job == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST "http://localhost:8080/job/%s/enable" 2>/dev/null && echo "Job %s enabled" || echo "Jenkins not available"`, req.Job, req.Job)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_job-enable")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "job-enable"})

	case "plugin-list":
		script = `curl -s "http://localhost:8080/pluginManager/api/json?depth=1" 2>/dev/null | head -20 || echo "Jenkins not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "jenkins_plugin-list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "plugin-list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// GitLab 管理
// ============================================================================

// handleGitLab 处理 POST /api/v1/cicd/gitlab
func (s *Server) handleGitLab(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/projects/pipelines/jobs
		Project  string `json:"project"`
		Branch   string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `curl -s http://localhost/api/v4/version 2>/dev/null || echo "GitLab not available"`
	case "projects":
		script = `curl -s http://localhost/api/v4/projects 2>/dev/null | head -20 || echo "GitLab not available"`
	case "pipelines":
		if req.Project == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "project is required"})
			return
		}
		script = fmt.Sprintf(`curl -s "http://localhost/api/v4/projects/%s/pipelines" 2>/dev/null | head -20 || echo "GitLab not available"`, req.Project)
	case "jobs":
		if req.Project == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "project is required"})
			return
		}
		script = fmt.Sprintf(`curl -s "http://localhost/api/v4/projects/%s/jobs" 2>/dev/null | head -20 || echo "GitLab not available"`, req.Project)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gitlab_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// GitHub Actions 管理
// ============================================================================

// handleGitHubActions 处理 POST /api/v1/cicd/github-actions
func (s *Server) handleGitHubActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/workflows/runs
		Repo     string `json:"repo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `gh auth status 2>/dev/null && echo "GitHub CLI authenticated" || echo "GitHub CLI not authenticated"`
	case "workflows":
		if req.Repo == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is required"})
			return
		}
		script = fmt.Sprintf(`gh workflow list -R %s 2>/dev/null || echo "GitHub CLI not available"`, req.Repo)
	case "runs":
		if req.Repo == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is required"})
			return
		}
		script = fmt.Sprintf(`gh run list -R %s 2>/dev/null | head -20 || echo "GitHub CLI not available"`, req.Repo)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "github-actions_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Tekton 管理
// ============================================================================

// handleTekton 处理 POST /api/v1/cicd/tekton
func (s *Server) handleTekton(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/pipelines/pipelineruns/tasks
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `kubectl get pods -n tekton-pipelines 2>/dev/null | head -10 || echo "Tekton not installed"`
	case "pipelines":
		script = `kubectl get pipelines -A 2>/dev/null || echo "Tekton not installed"`
	case "pipelineruns":
		script = `kubectl get pipelineruns -A 2>/dev/null | head -20 || echo "Tekton not installed"`
	case "tasks":
		script = `kubectl get tasks -A 2>/dev/null | head -20 || echo "Tekton not installed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "tekton_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerCICDRoutes 注册 CI/CD 路由
func (s *Server) registerCICDRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/cicd/jenkins", s.handleJenkins)
	mux.HandleFunc("/api/v1/cicd/gitlab", s.handleGitLab)
	mux.HandleFunc("/api/v1/cicd/github-actions", s.handleGitHubActions)
	mux.HandleFunc("/api/v1/cicd/tekton", s.handleTekton)
}
