package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
)

// TestSaveLoadAgentKey ：per-agent 密钥落盘 0600、可读回；内容变化才重写。
func TestSaveLoadAgentKey(t *testing.T) {
	dir := t.TempDir()
	if got := loadAgentKey(dir); got != "" {
		t.Fatalf("未落盘时应为空，得到 %q", got)
	}
	if err := saveAgentKey(dir, "key-abc"); err != nil {
		t.Fatalf("saveAgentKey: %v", err)
	}
	if got := loadAgentKey(dir); got != "key-abc" {
		t.Fatalf("读回 = %q, want key-abc", got)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, "agent.key"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("agent.key 权限 = %o, want 600（密钥不得组/其他可读）", perm)
		}
	}
	// 内容不同 → 覆盖写。
	if err := saveAgentKey(dir, "key-def"); err != nil {
		t.Fatalf("saveAgentKey(覆盖): %v", err)
	}
	if got := loadAgentKey(dir); got != "key-def" {
		t.Fatalf("覆盖后读回 = %q, want key-def", got)
	}
}

// TestSaveAgentKey_EmptyDir ：dataDir 为空时明确报错（不静默丢密钥）。
func TestSaveAgentKey_EmptyDir(t *testing.T) {
	if err := saveAgentKey("", "k"); err == nil {
		t.Fatal("空 dataDir 应返回错误")
	}
}

// TestResolveAgentKey_Priority ：密钥优先级 = 预共享 > 响应下发（并落盘）> 本机 agent.key > 空。
func TestResolveAgentKey_Priority(t *testing.T) {
	ctx := context.Background()

	// 1) 预共享密钥优先，即使响应也下发了密钥。
	a := &Agent{dataDir: t.TempDir(), cfg: &config.Config{GRPCSignatureKey: "pre-shared"}}
	if key, src := a.resolveAgentKey(ctx, "from-response"); key != "pre-shared" || src != "pre-shared" {
		t.Fatalf("优先级 1 失败：key=%q src=%q", key, src)
	}

	// 2) 无预共享 → 用响应密钥，并落盘供重启复用。
	dir := t.TempDir()
	a = &Agent{dataDir: dir, cfg: &config.Config{}}
	key, src := a.resolveAgentKey(ctx, "from-response")
	if key != "from-response" || src != "register-response" {
		t.Fatalf("优先级 2 失败：key=%q src=%q", key, src)
	}
	if got := loadAgentKey(dir); got != "from-response" {
		t.Fatalf("响应密钥未落盘：%q", got)
	}

	// 3) 控制面未下发（token 已消费 / 非 TLS）→ 回退本机 agent.key，身份连续。
	a = &Agent{dataDir: dir, cfg: &config.Config{}}
	if key, src = a.resolveAgentKey(ctx, ""); key != "from-response" || src != "agent.key" {
		t.Fatalf("优先级 3 失败：key=%q src=%q", key, src)
	}

	// 4) 三者皆无 → 空串（不签名）。
	a = &Agent{dataDir: t.TempDir(), cfg: &config.Config{}}
	if key, src = a.resolveAgentKey(ctx, ""); key != "" || src != "" {
		t.Fatalf("优先级 4 失败：key=%q src=%q", key, src)
	}
}

// TestSandboxedEnv_StripsSecrets ：任务子进程环境不得携带 agent 自身凭据（P1-2 核心断言）。
func TestSandboxedEnv_StripsSecrets(t *testing.T) {
	t.Setenv("OPSMESH_GRPC_SIGNATURE_KEY", "fleet-secret")
	t.Setenv("OPSMESH_PROVISION_SECRET", "prov-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("PATH", "/usr/bin:/bin")

	env := sandboxedEnv()
	joined := strings.Join(env, "\n")
	for _, leak := range []string{"OPSMESH_GRPC_SIGNATURE_KEY", "OPSMESH_PROVISION_SECRET", "AWS_SECRET_ACCESS_KEY", "fleet-secret", "prov-secret", "aws-secret"} {
		if strings.Contains(joined, leak) {
			t.Fatalf("敏感变量 %q 泄漏给任务子进程:\n%s", leak, joined)
		}
	}
	// PATH 必须保留（否则 sh -c 找不到任何命令）。
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			found = true
		}
	}
	if !found {
		t.Fatal("PATH 应保留在任务子进程环境（否则命令不可执行）")
	}
}

// TestSandboxEnvAllowlist_NoCredentialishNames ：白名单本身不得包含任何凭据类变量名
// （防后续「顺手加一个方便变量」把密钥重新引回任务子进程）。
func TestSandboxEnvAllowlist_NoCredentialishNames(t *testing.T) {
	suspicious := []string{"SECRET", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL", "_KEY", "APIKEY", "OPIKEY"}
	for name := range sandboxEnvAllowlist {
		up := strings.ToUpper(name)
		if strings.HasPrefix(up, "OPSMESH_") {
			t.Fatalf("白名单含 OpsMesh 自身变量 %q：可能承载凭据/配置，不得透传任务子进程", name)
		}
		for _, s := range suspicious {
			if strings.Contains(up, s) {
				t.Fatalf("白名单含疑似凭据变量 %q（命中 %q）：透传前须安全评审", name, s)
			}
		}
	}
}

// TestExecuteShell_DoesNotLeakAgentEnv ：端到端断言——真起子进程读环境，
// agent 的签名密钥不得出现在任务输出里（P1-2 的直接证据）。
func TestExecuteShell_DoesNotLeakAgentEnv(t *testing.T) {
	t.Setenv("OPSMESH_GRPC_SIGNATURE_KEY", "fleet-secret-must-not-leak")
	a := newTestAgent(5 * time.Second)
	cmd := "env"
	if runtime.GOOS == "windows" {
		cmd = "set"
	}
	var out, errb strings.Builder
	if err := a.executeShell(context.Background(), cmd, &out, &errb); err != nil {
		t.Fatalf("executeShell: %v (stderr=%s)", err, errb.String())
	}
	if strings.Contains(out.String(), "fleet-secret-must-not-leak") {
		t.Fatalf("任务子进程读到了 agent 的签名密钥：\n%s", out.String())
	}
}

// TestSandboxedEnv_AllowsLocale ：LC_* 例外放行（本地化不影响安全）。
func TestSandboxedEnv_AllowsLocale(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	env := sandboxedEnv()
	found := false
	for _, kv := range env {
		if kv == "LC_ALL=zh_CN.UTF-8" {
			found = true
		}
	}
	if !found {
		t.Fatal("LC_ALL 应在放行白名单内")
	}
}
