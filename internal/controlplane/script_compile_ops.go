// Package controlplane script_compile_ops.go 实现脚本编译与构建工具运维
// 支持：Shell/Bash, Python, PowerShell, CMD/BAT, Ruby, Perl, Node.js, Go, Rust, Java/JAR
// 构建工具：Maven, Gradle, npm, yarn, cargo, make, cmake, go build
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// 脚本执行 (多语言)
// ============================================================================

// handleScriptExec 处理 POST /api/v1/script/exec
func (s *Server) handleScriptExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Language string `json:"language"` // shell/python/powershell/cmd/bat/ruby/perl/node/go/rust/java
		Content  string `json:"content"`
		Args     string `json:"args"`    // 命令行参数
		WorkDir  string `json:"workDir"` // 工作目录
		Timeout  int    `json:"timeout"` // 超时 (秒)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Content == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	if req.Language == "" {
		req.Language = "shell"
	}

	// 根据语言选择解释器
	interpreter := getInterpreter(req.Language)
	if interpreter == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported language: " + req.Language})
		return
	}

	// 构建执行脚本
	var script string
	ext := getScriptExtension(req.Language)
	filename := fmt.Sprintf("/tmp/script_%d.%s", nowUnix(), ext)

	// 写入脚本文件
	script = fmt.Sprintf(`cat > %s << 'SCRIPT_EOF'
%s
SCRIPT_EOF
chmod +x %s
`, filename, req.Content, filename)

	// 执行
	workDir := req.WorkDir
	if workDir == "" {
		workDir = "/tmp"
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 120
	}

	script += fmt.Sprintf(`cd %s && timeout %d %s %s %s 2>&1; echo "EXIT_CODE=$?"`, workDir, timeout, interpreter, filename, req.Args)

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "script_exec_"+req.Language)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "exec", "language": req.Language})
}

func getInterpreter(lang string) string {
	switch lang {
	case "shell", "bash", "sh":
		return "bash"
	case "python", "python3", "py":
		return "python3"
	case "powershell", "ps1":
		return "powershell"
	case "cmd", "bat":
		return "cmd /C"
	case "ruby", "rb":
		return "ruby"
	case "perl", "pl":
		return "perl"
	case "node", "nodejs", "js":
		return "node"
	case "go":
		return "go run"
	case "rust", "rs":
		return "rustc -o /tmp/rust_out && /tmp/rust_out"
	case "java":
		return "java"
	case "groovy":
		return "groovy"
	case "php":
		return "php"
	case "lua":
		return "lua"
	default:
		return ""
	}
}

func getScriptExtension(lang string) string {
	switch lang {
	case "shell", "bash", "sh":
		return "sh"
	case "python", "python3", "py":
		return "py"
	case "powershell", "ps1":
		return "ps1"
	case "cmd", "bat":
		return "bat"
	case "ruby", "rb":
		return "rb"
	case "perl", "pl":
		return "pl"
	case "node", "nodejs", "js":
		return "js"
	case "go":
		return "go"
	case "rust", "rs":
		return "rs"
	case "java":
		return "java"
	case "groovy":
		return "groovy"
	case "php":
		return "php"
	case "lua":
		return "lua"
	default:
		return "txt"
	}
}

func nowUnix() int64 {
	return time.Now().Unix()
}

// ============================================================================
// 脚本编译
// ============================================================================

// handleScriptCompile 处理 POST /api/v1/script/compile
func (s *Server) handleScriptCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Language string `json:"language"` // go/rust/java/c/cpp/csharp
		Source   string `json:"source"`
		Output   string `json:"output"` // 输出文件名
		Args     string `json:"args"`   // 编译参数
		WorkDir  string `json:"workDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Source == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "source is required"})
		return
	}

	if req.Language == "" {
		req.Language = "go"
	}

	output := req.Output
	if output == "" {
		output = "/tmp/compiled_out"
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = "/tmp"
	}

	var script string
	switch req.Language {
	case "go":
		script = fmt.Sprintf(`cat > %s/main.go << 'SCRIPT_EOF'
%s
SCRIPT_EOF
cd %s && go build -o %s %s main.go && echo "Go compilation successful: %s"`, workDir, req.Source, workDir, output, req.Args, output)
	case "rust", "rs":
		ext := "rs"
		script = fmt.Sprintf(`cat > %s/main.%s << 'SCRIPT_EOF'
%s
SCRIPT_EOF
cd %s && rustc %s -o %s main.%s && echo "Rust compilation successful: %s"`, workDir, ext, req.Source, workDir, req.Args, output, ext, output)
	case "java":
		script = fmt.Sprintf(`cat > %s/Main.java << 'SCRIPT_EOF'
%s
SCRIPT_EOF
cd %s && javac %s Main.java && jar cvf %s.jar Main.class && echo "Java compilation successful: %s.jar"`, workDir, req.Source, workDir, req.Args, output, output)
	case "c":
		script = fmt.Sprintf(`cat > %s/main.c << 'SCRIPT_EOF'
%s
SCRIPT_EOF
cd %s && gcc %s -o %s main.c && echo "C compilation successful: %s"`, workDir, req.Source, workDir, req.Args, output, output)
	case "cpp", "c++":
		script = fmt.Sprintf(`cat > %s/main.cpp << 'SCRIPT_EOF'
%s
SCRIPT_EOF
cd %s && g++ %s -o %s main.cpp && echo "C++ compilation successful: %s"`, workDir, req.Source, workDir, req.Args, output, output)
	case "csharp", "cs":
		script = fmt.Sprintf(`cat > %s/main.cs << 'SCRIPT_EOF'
%s
SCRIPT_EOF
cd %s && mcs %s -out:%s main.cs && echo "C# compilation successful: %s"`, workDir, req.Source, workDir, req.Args, output, output)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported language: " + req.Language})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "script_compile_"+req.Language)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "compile", "language": req.Language, "output": output})
}

// ============================================================================
// 构建工具
// ============================================================================

// handleBuildTool 处理 POST /api/v1/build/tool
func (s *Server) handleBuildTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Tool     string `json:"tool"`    // maven/gradle/npm/yarn/cargo/make/cmake/go
		Action   string `json:"action"`  // build/test/clean/install/package
		Project  string `json:"project"` // 项目路径
		Args     string `json:"args"`    // 额外参数
		Profile  string `json:"profile"` // Maven profile / Gradle task
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Tool == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tool is required"})
		return
	}

	project := req.Project
	if project == "" {
		project = "/tmp/project"
	}

	action := req.Action
	if action == "" {
		action = "build"
	}

	var script string
	switch req.Tool {
	case "maven", "mvn":
		goal := action
		if goal == "build" {
			goal = "package"
		}
		if goal == "test" {
			goal = "test"
		}
		if goal == "clean" {
			goal = "clean"
		}
		if goal == "install" {
			goal = "install"
		}
		profile := ""
		if req.Profile != "" {
			profile = "-P " + req.Profile
		}
		script = fmt.Sprintf(`cd %s && mvn %s %s %s 2>&1 | tail -30`, project, goal, profile, req.Args)

	case "gradle", "gradlew":
		task := action
		if task == "build" {
			task = "build"
		}
		if task == "test" {
			task = "test"
		}
		if task == "clean" {
			task = "clean"
		}
		script = fmt.Sprintf(`cd %s && ./gradlew %s %s 2>&1 | tail -30`, project, task, req.Args)

	case "npm":
		cmd := "npm run " + action
		if action == "build" {
			cmd = "npm run build"
		}
		if action == "test" {
			cmd = "npm test"
		}
		if action == "install" {
			cmd = "npm install"
		}
		if action == "clean" {
			cmd = "npm run clean 2>/dev/null || rm -rf node_modules dist"
		}
		script = fmt.Sprintf(`cd %s && %s %s 2>&1 | tail -30`, project, cmd, req.Args)

	case "yarn":
		cmd := "yarn " + action
		if action == "build" {
			cmd = "yarn build"
		}
		if action == "test" {
			cmd = "yarn test"
		}
		if action == "install" {
			cmd = "yarn install"
		}
		script = fmt.Sprintf(`cd %s && %s %s 2>&1 | tail -30`, project, cmd, req.Args)

	case "cargo":
		cmd := "cargo build"
		if action == "test" {
			cmd = "cargo test"
		}
		if action == "clean" {
			cmd = "cargo clean"
		}
		if action == "install" {
			cmd = "cargo install --path ."
		}
		if action == "package" {
			cmd = "cargo package"
		}
		script = fmt.Sprintf(`cd %s && %s %s 2>&1 | tail -30`, project, cmd, req.Args)

	case "make":
		target := action
		if action == "build" {
			target = "all"
		}
		script = fmt.Sprintf(`cd %s && make %s %s 2>&1 | tail -30`, project, target, req.Args)

	case "cmake":
		cmd := "cmake --build ."
		if action == "configure" {
			cmd = "cmake -B build -S ."
		}
		if action == "clean" {
			cmd = "rm -rf build && cmake -B build -S ."
		}
		script = fmt.Sprintf(`cd %s && %s %s 2>&1 | tail -30`, project, cmd, req.Args)

	case "go":
		cmd := "go build ./..."
		if action == "test" {
			cmd = "go test ./..."
		}
		if action == "clean" {
			cmd = "go clean ./..."
		}
		if action == "install" {
			cmd = "go install ./..."
		}
		if action == "mod" {
			cmd = "go mod tidy"
		}
		script = fmt.Sprintf(`cd %s && %s %s 2>&1 | tail -30`, project, cmd, req.Args)

	case "python":
		cmd := "python3 setup.py build"
		if action == "test" {
			cmd = "python3 -m pytest"
		}
		if action == "install" {
			cmd = "pip install -e ."
		}
		if action == "clean" {
			cmd = "rm -rf build dist *.egg-info"
		}
		script = fmt.Sprintf(`cd %s && %s %s 2>&1 | tail -30`, project, cmd, req.Args)

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown tool: " + req.Tool})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "build_"+req.Tool+"_"+action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": action, "tool": req.Tool})
}

// ============================================================================
// 脚本模板管理
// ============================================================================

// handleScriptTemplate 处理 POST /api/v1/script/template
func (s *Server) handleScriptTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/get/create/delete
		Name     string `json:"name"`
		Language string `json:"language"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `ls /opt/opsmesh/scripts/ 2>/dev/null || echo "No script templates"`
	case "get":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`cat /opt/opsmesh/scripts/%s 2>/dev/null || echo "Script not found"`, req.Name)
	case "create":
		if req.Name == "" || req.Content == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and content are required"})
			return
		}
		script = fmt.Sprintf(`mkdir -p /opt/opsmesh/scripts && cat > /opt/opsmesh/scripts/%s << 'SCRIPT_EOF'
%s
SCRIPT_EOF
echo "Script template %s created"`, req.Name, req.Content, req.Name)
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`rm -f /opt/opsmesh/scripts/%s && echo "Script template %s deleted"`, req.Name, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "script_template_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerScriptCompileRoutes 注册脚本编译与构建工具路由
func (s *Server) registerScriptCompileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/script/exec", s.handleScriptExec)
	mux.HandleFunc("/api/v1/script/compile", s.handleScriptCompile)
	mux.HandleFunc("/api/v1/build/tool", s.handleBuildTool)
	mux.HandleFunc("/api/v1/script/template", s.handleScriptTemplate)
}
