package controlplane

// container_injection_test.go — 批次 8（容器/存储运维面「req.* 未转义直插」收口）的注入守卫。
//
// 覆盖范围：internal/controlplane/docker_ops.go（24）+ container_ops.go（13）+ distfs_ops.go（14）。
//
// 为什么是真缺陷面：这些 handler 把用户可控字段裸拼进要下发到 agent 的 shell 脚本，
// agent 以 `sh -c` 执行 ⇒ `$(id)` / 反引号 / 分号被**真实执行**。鉴权面无缺口
// （均经 dispatchKafkaTask / dispatchFastDFSTask → subsystemAuthorize 的 task:write 闸）。
//
// 判据：
//
//	① 载荷只以单引号字面量形态出现——脚本中「单引号之外」的文本不得含载荷、
//	  不得含 `$(`、不得含反引号、不得含注入的副作用命令；
//	② **多词形态**字段（compose args、debug exec command、gluster mode/bricks）
//	  含 shell 元字符 ⇒ 400 拒收（不能裸拼，也不能用 Quote——Quote 会把多个词压成一个词、
//	  改掉命令语义）；合法值仍受理（能力不误伤）；
//	③ 若本机有 sh：把脚本交给**真 sh** 执行一遍，注入载荷不得产生任何副作用；
//	④ 源码级兜底：三个文件里不得再有「script = fmt.Sprintf(..., req.*, ...)」形态
//	  （未经 Quote、也没有对应 ValidateArgList 校验的）。
//
// 载荷集与「引号外文本」判据沿用批次 7（probe_injection_test.go 的 unquotedText /
// assertNoInjection），语义判据的载荷形态也照批次 7 的教训选**语法合法**的那一种
// （带未闭合单引号的载荷会让整段脚本变成语法错误 ⇒ fail-closed，不构成可执行注入的证明）。

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// b8UnquotedText 返回脚本中**不在单引号字面量内**的文本。
//
// POSIX 单引号语义：单引号内一切为字面量；唯一的转义法是关闭-转义-重开
// （' → '\”），故外层看到 `'` 切换状态、外层看到 `\'` 时该引号是字面量
// （不切换状态）。据此抽出「引号外文本」——注入载荷若出现在这里，就是可执行结构。
//
// 本函数与批次 7 的 unquotedText 同语义但独立命名：批次 8 的 PR 会以「main 已含
// 批次 7」为基线，同名同文件会撞符号；独立命名让两批的测试各自自包含。
func b8UnquotedText(script string) string {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(script); i++ {
		c := script[i]
		if inQuote {
			if c == '\'' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '\'':
			inQuote = true
		case '\\':
			b.WriteByte(c)
			if i+1 < len(script) {
				i++
				b.WriteByte(script[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// b8AssertNoInjection 断言脚本里载荷只以字面量形态存在（判据①）。
func b8AssertNoInjection(t *testing.T, name, script string, payload string) {
	t.Helper()
	out := b8UnquotedText(script)
	for _, bad := range []string{payload, "$(", "`", "touch /tmp/opsmesh-b8-pwned"} {
		if bad == "" {
			continue
		}
		if strings.Contains(out, bad) {
			t.Errorf("%s：引号外文本含可执行结构 %q：\n脚本=%s\n引号外=%s", name, bad, script, out)
		}
	}
	// 旧形态：双引号直插（双引号内 $ / 反引号仍会展开，故不是防护）。
	if strings.Contains(script, `"`+payload+`"`) {
		t.Errorf("%s：仍是双引号直插形态：\n%s", name, script)
	}
}

// b8Payloads 是批次 8 的注入载荷集。
var b8Payloads = []string{
	`$(id)`,
	"`id`",
	`x; touch /tmp/opsmesh-b8-pwned`,
	`x'; touch /tmp/opsmesh-b8-pwned; echo '`,
}

// b8Cases 覆盖三个文件里所有把用户字段拼进脚本的脚本行。
// field = 注入字段（其余字段给合法值，满足各 handler 的必填校验）。
func b8Cases(s *Server) []struct {
	name    string
	handler http.HandlerFunc
	body    map[string]interface{}
	field   string
} {
	return []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		field   string
	}{
		// ---- docker compose ----
		{"compose/up-project", s.handleDockerCompose, map[string]interface{}{"action": "up", "project": "/srv/app", "file": "docker-compose.yml", "service": "web"}, "project"},
		{"compose/down-file", s.handleDockerCompose, map[string]interface{}{"action": "down", "project": "/srv/app", "file": "docker-compose.yml"}, "file"},
		{"compose/ps-project", s.handleDockerCompose, map[string]interface{}{"action": "ps", "project": "/srv/app", "file": "docker-compose.yml"}, "project"},
		{"compose/logs-service", s.handleDockerCompose, map[string]interface{}{"action": "logs", "project": "/srv/app", "file": "docker-compose.yml", "service": "web"}, "service"},
		{"compose/restart-project", s.handleDockerCompose, map[string]interface{}{"action": "restart", "project": "/srv/app", "file": "docker-compose.yml"}, "project"},
		{"compose/pull-project", s.handleDockerCompose, map[string]interface{}{"action": "pull", "project": "/srv/app", "file": "docker-compose.yml"}, "project"},
		{"compose/build-service", s.handleDockerCompose, map[string]interface{}{"action": "build", "project": "/srv/app", "file": "docker-compose.yml", "service": "web"}, "service"},
		// ---- docker network ----
		{"network/inspect-name", s.handleDockerNetwork, map[string]interface{}{"action": "inspect", "name": "net1"}, "name"},
		{"network/create-name", s.handleDockerNetwork, map[string]interface{}{"action": "create", "name": "net1", "driver": "bridge", "subnet": "10.0.0.0/24", "gateway": "10.0.0.1"}, "name"},
		{"network/create-driver", s.handleDockerNetwork, map[string]interface{}{"action": "create", "name": "net1", "driver": "bridge"}, "driver"},
		{"network/create-subnet", s.handleDockerNetwork, map[string]interface{}{"action": "create", "name": "net1", "driver": "bridge", "subnet": "10.0.0.0/24"}, "subnet"},
		{"network/create-gateway", s.handleDockerNetwork, map[string]interface{}{"action": "create", "name": "net1", "driver": "bridge", "gateway": "10.0.0.1"}, "gateway"},
		{"network/remove-name", s.handleDockerNetwork, map[string]interface{}{"action": "remove", "name": "net1"}, "name"},
		{"network/connect-container", s.handleDockerNetwork, map[string]interface{}{"action": "connect", "name": "net1", "container": "c1"}, "container"},
		{"network/connect-alias", s.handleDockerNetwork, map[string]interface{}{"action": "connect", "name": "net1", "container": "c1", "alias": "a1"}, "alias"},
		{"network/disconnect-container", s.handleDockerNetwork, map[string]interface{}{"action": "disconnect", "name": "net1", "container": "c1"}, "container"},
		// ---- docker volume ----
		{"volume/inspect-name", s.handleDockerVolume, map[string]interface{}{"action": "inspect", "name": "vol1"}, "name"},
		{"volume/create-name", s.handleDockerVolume, map[string]interface{}{"action": "create", "name": "vol1", "driver": "local"}, "name"},
		{"volume/create-driver", s.handleDockerVolume, map[string]interface{}{"action": "create", "name": "vol1", "driver": "local"}, "driver"},
		{"volume/remove-name", s.handleDockerVolume, map[string]interface{}{"action": "remove", "name": "vol1"}, "name"},
		// ---- docker debug ----
		{"debug/exec-container", s.handleDockerDebug, map[string]interface{}{"action": "exec", "container": "c1"}, "container"},
		{"debug/top-container", s.handleDockerDebug, map[string]interface{}{"action": "top", "container": "c1"}, "container"},
		{"debug/port-port", s.handleDockerDebug, map[string]interface{}{"action": "port", "container": "c1", "port": "80/tcp"}, "port"},
		{"debug/diff-container", s.handleDockerDebug, map[string]interface{}{"action": "diff", "container": "c1"}, "container"},
		{"debug/cp-source", s.handleDockerDebug, map[string]interface{}{"action": "cp", "source": "/tmp/a", "dest": "/tmp/b"}, "source"},
		{"debug/cp-dest", s.handleDockerDebug, map[string]interface{}{"action": "cp", "source": "/tmp/a", "dest": "/tmp/b"}, "dest"},
		{"debug/stats-container", s.handleDockerDebug, map[string]interface{}{"action": "stats", "container": "c1"}, "container"},
		// ---- docker registry ----
		{"registry/login-password", s.handleDockerRegistry, map[string]interface{}{"action": "login", "username": "u", "password": "p"}, "password"},
		{"registry/login-username", s.handleDockerRegistry, map[string]interface{}{"action": "login", "username": "u", "password": "p"}, "username"},
		{"registry/logout-registry", s.handleDockerRegistry, map[string]interface{}{"action": "logout"}, "registry"},
		{"registry/search-image", s.handleDockerRegistry, map[string]interface{}{"action": "search", "image": "nginx"}, "image"},
		// ---- container / image ----
		{"container/inspect", s.handleContainer, map[string]interface{}{"action": "inspect", "container": "c1"}, "container"},
		{"container/start", s.handleContainer, map[string]interface{}{"action": "start", "container": "c1"}, "container"},
		{"container/stop", s.handleContainer, map[string]interface{}{"action": "stop", "container": "c1"}, "container"},
		{"container/restart", s.handleContainer, map[string]interface{}{"action": "restart", "container": "c1"}, "container"},
		{"container/remove", s.handleContainer, map[string]interface{}{"action": "remove", "container": "c1"}, "container"},
		{"container/logs", s.handleContainer, map[string]interface{}{"action": "logs", "container": "c1"}, "container"},
		{"image/pull", s.handleImage, map[string]interface{}{"action": "pull", "image": "nginx:1.21"}, "image"},
		{"image/push", s.handleImage, map[string]interface{}{"action": "push", "image": "nginx:1.21"}, "image"},
		{"image/build-dockerfile", s.handleImage, map[string]interface{}{"action": "build", "image": "app:1", "dockerfile": "Dockerfile", "context": "."}, "dockerfile"},
		{"image/build-context", s.handleImage, map[string]interface{}{"action": "build", "image": "app:1", "dockerfile": "Dockerfile", "context": "."}, "context"},
		{"image/scan", s.handleImage, map[string]interface{}{"action": "scan", "image": "nginx:1.21"}, "image"},
		{"image/sign", s.handleImage, map[string]interface{}{"action": "sign", "image": "nginx:1.21"}, "image"},
		{"image/tag", s.handleImage, map[string]interface{}{"action": "tag", "image": "nginx:1.21", "tag": "v1"}, "tag"},
		{"image/remove", s.handleImage, map[string]interface{}{"action": "remove", "image": "nginx:1.21"}, "image"},
		// ---- distfs ----
		{"distfs/ceph-deploy-version", s.handleDistFS, map[string]interface{}{"type": "ceph", "action": "deploy", "version": "reef"}, "version"},
		{"distfs/ceph-create-pool", s.handleDistFS, map[string]interface{}{"type": "ceph", "action": "create-pool", "pool": "p1"}, "pool"},
		{"distfs/ceph-delete-pool", s.handleDistFS, map[string]interface{}{"type": "ceph", "action": "delete-pool", "pool": "p1"}, "pool"},
		{"distfs/ceph-cephfs-create", s.handleDistFS, map[string]interface{}{"type": "ceph", "action": "cephfs-create", "volume": "v1"}, "volume"},
		{"distfs/ceph-rbd-create-pool", s.handleDistFS, map[string]interface{}{"type": "ceph", "action": "rbd-create", "pool": "p1", "volume": "v1"}, "pool"},
		{"distfs/ceph-rbd-create-volume", s.handleDistFS, map[string]interface{}{"type": "ceph", "action": "rbd-create", "pool": "p1", "volume": "v1"}, "volume"},
		{"distfs/gluster-deploy-version", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "deploy", "version": "11.0"}, "version"},
		{"distfs/gluster-create-volume", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "create-volume", "volume": "v1", "bricks": "srv1:/v1"}, "volume"},
		{"distfs/gluster-delete-volume", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "delete-volume", "volume": "v1"}, "volume"},
		{"distfs/gluster-peer-probe", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "peer-probe", "host": "h1"}, "host"},
		{"distfs/gluster-mount-path", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "mount", "volume": "v1", "path": "/mnt/x"}, "path"},
		{"distfs/hdfs-deploy-version", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "deploy", "version": "3.3.6"}, "version"},
		{"distfs/hdfs-mkdir-path", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "mkdir", "path": "/user/x"}, "path"},
		{"distfs/hdfs-ls-path", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "ls", "path": "/user/x"}, "path"},
		{"distfs/hdfs-put-file", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "put", "file": "/tmp/a", "path": "/user/x"}, "file"},
		{"distfs/hdfs-put-path", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "put", "file": "/tmp/a", "path": "/user/x"}, "path"},
		{"distfs/hdfs-get-remote", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "get", "remote": "/user/x", "file": "/tmp/a"}, "remote"},
		{"distfs/hdfs-rm-path", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "rm", "path": "/user/x"}, "path"},
		{"distfs/hdfs-du-path", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "du", "path": "/user/x"}, "path"},
		{"distfs/seaweedfs-deploy-version", s.handleDistFS, map[string]interface{}{"type": "seaweedfs", "action": "deploy", "version": "3.64"}, "version"},
		{"distfs/seaweedfs-upload-file", s.handleDistFS, map[string]interface{}{"type": "seaweedfs", "action": "upload", "file": "/tmp/a"}, "file"},
		{"distfs/seaweedfs-download-remote", s.handleDistFS, map[string]interface{}{"type": "seaweedfs", "action": "download", "file": "/tmp/a", "remote": "/user/x"}, "remote"},
	}
}

// TestB8InjectionQuoted 判据①：载荷只以单引号字面量形态出现。
func TestB8InjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	for _, c := range b8Cases(s) {
		for pi, payload := range b8Payloads {
			body := map[string]interface{}{"deviceID": "b8-" + c.name + "-" + string(rune('a'+pi))}
			for k, v := range c.body {
				body[k] = v
			}
			body[c.field] = payload
			dev := body["deviceID"].(string)
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
			}
			sc := sqDispatchScript(t, s, dev)
			b8AssertNoInjection(t, c.name+"/"+payload, sc, payload)
		}
	}
}

// TestB8FreeFormFieldsRejected 判据②：多词形态字段含 shell 元字符 ⇒ 400 拒收；
// 合法值仍受理（能力不误伤）。
func TestB8FreeFormFieldsRejected(t *testing.T) {
	s := newShellQuoteServer()
	bad := []string{
		`--foo; rm -rf /`,
		`--foo && touch /tmp/x`,
		`--foo | tee /tmp/x`,
		"$(id)",
		"`id`",
		`--foo > /tmp/x`,
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
	}{
		{"compose-args", s.handleDockerCompose, map[string]interface{}{"action": "up", "project": "/srv/app", "file": "docker-compose.yml", "args": "X"}},
		{"debug-command", s.handleDockerDebug, map[string]interface{}{"action": "exec", "container": "c1", "command": "X"}},
		{"gluster-mode", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "create-volume", "volume": "v1", "bricks": "srv1:/v1", "mode": "X"}},
		{"gluster-bricks", s.handleDistFS, map[string]interface{}{"type": "glusterfs", "action": "create-volume", "volume": "v1", "bricks": "X", "mode": "replica 3"}},
	}
	for _, c := range cases {
		for i, payload := range bad {
			body := map[string]interface{}{"deviceID": "b8-free-" + c.name + "-" + string(rune('a'+i))}
			for k, v := range c.body {
				body[k] = v
			}
			// 注入点：compose→args；debug→command；gluster→mode/bricks
			switch c.name {
			case "compose-args":
				body["args"] = payload
			case "debug-command":
				body["command"] = payload
			case "gluster-mode":
				body["mode"] = payload
			case "gluster-bricks":
				body["bricks"] = payload
			}
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s：含元字符的参数串应 400 拒收，实得 %d：%s", c.name, rec.Code, rec.Body.String())
			}
		}
		// 合法值仍受理
		body := map[string]interface{}{"deviceID": "b8-free-ok-" + c.name}
		for k, v := range c.body {
			body[k] = v
		}
		switch c.name {
		case "compose-args":
			body["args"] = "--scale web=3 --build"
		case "debug-command":
			body["command"] = "sh -c df"
		case "gluster-mode":
			body["mode"] = "replica 3"
		case "gluster-bricks":
			body["bricks"] = "srv1:/v1 srv2:/v2"
		}
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s：合法参数串应受理，实得 %d：%s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// TestB8SemanticNoSideEffect 判据③：把脚本交给**真 sh** 执行，注入不得产生副作用。
// 只取快失败的动作（inspect/ls 一类），避免 docker 拉镜像之类的长任务。
func TestB8SemanticNoSideEffect(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本机无 sh（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	payload := "x; touch " + filepath.ToSlash(marker) + "; echo y"

	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
	}{
		{"docker-volume-inspect", s.handleDockerVolume, map[string]interface{}{"action": "inspect", "name": "v1"}},
		{"docker-network-inspect", s.handleDockerNetwork, map[string]interface{}{"action": "inspect", "name": "net1"}},
		{"container-inspect", s.handleContainer, map[string]interface{}{"action": "inspect", "container": "c1"}},
		{"hdfs-ls", s.handleDistFS, map[string]interface{}{"type": "hdfs", "action": "ls", "path": "/user/x"}},
	}
	for _, c := range plan {
		body := map[string]interface{}{"deviceID": "b8-sem-" + c.name}
		for k, v := range c.body {
			body[k] = v
		}
		switch c.name {
		case "docker-volume-inspect":
			body["name"] = payload
		case "docker-network-inspect":
			body["name"] = payload
		case "container-inspect":
			body["container"] = payload
		case "hdfs-ls":
			body["path"] = payload
		}
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
		}
		sc := sqDispatchScript(t, s, body["deviceID"].(string))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		exec.CommandContext(ctx, sh, "-c", sc).Run() // 退出码无关紧要（工具多半不存在）
		cancel()
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s：注入载荷被执行（标记文件已落地）：\n%s", c.name, sc)
		}
	}
}

// TestB8NoRawReqInterpolation 判据④：三个文件里不得再有未经 Quote（且无对应
// ValidateArgList 校验）的「script = fmt.Sprintf(..., req.*, ...)」形态。
func TestB8NoRawReqInterpolation(t *testing.T) {
	for _, f := range []string{"docker_ops.go", "container_ops.go", "distfs_ops.go"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", f, err)
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "script") || !strings.Contains(line, "fmt.Sprintf") {
				continue
			}
			if !strings.Contains(line, "req.") {
				continue
			}
			if strings.Contains(line, "shellquote.Quote") {
				continue
			}
			// 多词形态的字段走 ValidateArgList 校验：回看 12 行内是否有该校验。
			validated := false
			for j := i - 1; j >= 0 && j >= i-12; j-- {
				if strings.Contains(lines[j], "ValidateArgList") {
					validated = true
					break
				}
			}
			if validated {
				continue
			}
			t.Errorf("%s:%d 用户字段未经 Quote/ValidateArgList 直插脚本：%s", f, i+1, strings.TrimSpace(line))
		}
	}
}
