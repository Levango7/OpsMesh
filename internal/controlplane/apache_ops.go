// apache_ops.go Apache httpd / Tomcat 运维支持。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleApacheHTTPD 处理 POST /api/v1/middleware/apache/httpd
func (s *Server) handleApacheHTTPD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/restart/deploy/scan/config
		Version  string `json:"version"`
		Config   string `json:"config"`
		Path     string `json:"path"`
		Content  string `json:"content"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Version == "" {
		req.Version = "2.4"
	}
	if req.Path == "" {
		req.Path = "/etc/httpd/conf/httpd.conf"
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Apache httpd Status ===" && (apachectl -S 2>/dev/null || httpd -S 2>/dev/null || echo "apachectl/httpd not available") && echo "=== Version ===" && (httpd -v 2>/dev/null || apachectl -v 2>/dev/null || echo "httpd not installed") && echo "=== Service ===" && (systemctl status httpd 2>/dev/null || systemctl status apache2 2>/dev/null || echo "systemctl not available")`
	case "start":
		script = `systemctl start httpd 2>/dev/null || apachectl start 2>/dev/null || service httpd start 2>/dev/null; echo "httpd start attempted"`
	case "stop":
		script = `systemctl stop httpd 2>/dev/null || apachectl stop 2>/dev/null || service httpd stop 2>/dev/null; echo "httpd stop attempted"`
	case "restart":
		script = `systemctl restart httpd 2>/dev/null || apachectl restart 2>/dev/null || service httpd restart 2>/dev/null; echo "httpd restart attempted"`
	case "deploy":
		vhost := ""
		if req.Config != "" {
			vhost = fmt.Sprintf("printf '%%s\\n' %s > /etc/httpd/conf.d/app.conf && echo 'vhost config written'\n", shellQuote(req.Config))
		}
		script = fmt.Sprintf(`set -e
if command -v httpd >/dev/null 2>&1 || command -v apache2 >/dev/null 2>&1; then echo 'httpd already installed'; else
  if command -v yum >/dev/null 2>&1; then yum install -y httpd httpd-tools mod_ssl; elif command -v dnf >/dev/null 2>&1; then dnf install -y httpd httpd-tools mod_ssl; elif command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y apache2; else echo 'no package manager' && exit 1; fi
fi
%s
systemctl enable --now httpd 2>/dev/null || apachectl start 2>/dev/null || true
echo "Apache httpd %s deployed"`, vhost, req.Version)
	case "scan":
		script = `echo "=== Apache Security Scan ===" && echo "--- loaded modules ---" && (apachectl -M 2>/dev/null || httpd -M 2>/dev/null) | grep -iE 'security|headers|ssl' || echo "mod_security not loaded" && echo "--- config test ---" && (apachectl -t 2>&1 || httpd -t 2>&1) && echo "--- response headers ---" && (curl -sI http://localhost/ 2>/dev/null | grep -iE 'server|x-frame|x-content-type|strict-transport|content-security' || echo "curl not available")`
	case "config":
		if req.Content != "" {
			script = fmt.Sprintf(`printf '%%s\n' %s > %s && echo "config written to %s" && (apachectl -t 2>&1 || httpd -t 2>&1)`, shellQuote(req.Content), shellQuote(req.Path), req.Path)
		} else {
			script = fmt.Sprintf(`echo "=== %s ===" && (cat %s 2>/dev/null || echo "file not found")`, req.Path, shellQuote(req.Path))
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_httpd_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheTomcat 处理 POST /api/v1/middleware/apache/tomcat
func (s *Server) handleApacheTomcat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/start/stop/restart/deploy/scan/config
		Version  string `json:"version"`
		War      string `json:"war"`  // WAR file path/URL for deploy
		Path     string `json:"path"` // config file path
		Content  string `json:"content"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Version == "" {
		req.Version = "9.0.85"
	}
	if req.Path == "" {
		req.Path = "/opt/tomcat/conf/server.xml"
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Tomcat Status ===" && (catalina.sh version 2>/dev/null || /opt/tomcat/bin/catalina.sh version 2>/dev/null || echo "catalina not available") && echo "=== Service ===" && (systemctl status tomcat 2>/dev/null || systemctl status tomcat9 2>/dev/null || echo "systemctl not available") && echo "=== Processes ===" && (ps aux | grep -i tomcat | grep -v grep | head -3)`
	case "start":
		script = `systemctl start tomcat 2>/dev/null || /opt/tomcat/bin/startup.sh 2>/dev/null || catalina.sh start 2>/dev/null; echo "tomcat start attempted"`
	case "stop":
		script = `systemctl stop tomcat 2>/dev/null || /opt/tomcat/bin/shutdown.sh 2>/dev/null || catalina.sh stop 2>/dev/null; echo "tomcat stop attempted"`
	case "restart":
		script = `(systemctl restart tomcat 2>/dev/null || (/opt/tomcat/bin/shutdown.sh 2>/dev/null; sleep 2; /opt/tomcat/bin/startup.sh 2>/dev/null)) ; echo "tomcat restart attempted"`
	case "deploy":
		war := ""
		if req.War != "" {
			war = fmt.Sprintf("if [ -f %s ]; then cp %s /opt/tomcat/webapps/ && echo 'WAR deployed'; elif echo %s | grep -q '^http'; then wget -q %s -O /opt/tomcat/webapps/app.war && echo 'WAR downloaded'; fi\n", shellQuote(req.War), shellQuote(req.War), shellQuote(req.War), shellQuote(req.War))
		}
		script = fmt.Sprintf(`set -e
if ! command -v java >/dev/null 2>&1; then
  if command -v yum >/dev/null 2>&1; then yum install -y java-11-openjdk; elif command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y openjdk-11-jdk; else echo 'no java' && exit 1; fi
fi
if [ ! -d /opt/tomcat ]; then
  VER=%s
  wget -q https://archive.apache.org/dist/tomcat/tomcat-9/v${VER}/bin/apache-tomcat-${VER}.tar.gz -O /tmp/tomcat.tgz
  mkdir -p /opt && tar -xzf /tmp/tomcat.tgz -C /opt
  ln -sfn /opt/apache-tomcat-${VER} /opt/tomcat
fi
%s
/opt/tomcat/bin/startup.sh 2>/dev/null || true
echo "Apache Tomcat %s deployed"`, req.Version, war, req.Version)
	case "scan":
		script = `echo "=== Tomcat Security Scan ===" && echo "--- manager app ---" && (ls /opt/tomcat/webapps/ 2>/dev/null | grep -i manager || echo "manager not deployed") && echo "--- default credentials check ---" && (grep -n "tomcat-users" /opt/tomcat/conf/tomcat-users.xml 2>/dev/null; grep -oE 'username="[a-z]+" password="[a-z]+"' /opt/tomcat/conf/tomcat-users.xml 2>/dev/null || echo "no default creds found") && echo "--- version ---" && /opt/tomcat/bin/catalina.sh version 2>&1 | head -5`
	case "config":
		if req.Content != "" {
			script = fmt.Sprintf(`printf '%%s\n' %s > %s && echo "config written to %s"`, shellQuote(req.Content), shellQuote(req.Path), req.Path)
		} else {
			script = fmt.Sprintf(`echo "=== %s ===" && (cat %s 2>/dev/null || echo "file not found")`, req.Path, shellQuote(req.Path))
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_tomcat_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerApacheRoutes 注册 Apache 路由
func (s *Server) registerApacheRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/apache/httpd", s.handleApacheHTTPD)
	mux.HandleFunc("/api/v1/middleware/apache/tomcat", s.handleApacheTomcat)
}

var _ = strings.TrimSpace
var _ = json.NewDecoder
