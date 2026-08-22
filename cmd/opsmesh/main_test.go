// cmd/opsmesh 入口逻辑单测（task 154 / P1-B3）。
//
// 覆盖三条关键路径：
//  1. --health 子命令：httptest mock 端点，直接调用同包 runHealth 验证各退出码；
//  2. --version 输出：主进程调用 main() 捕获 stdout + 直接断言 versionString() 格式；
//  3. flag 解析：TestMain 子进程模式隔离 config.Load 的全局 flag，验证默认值与覆盖。
//
// 子进程模式说明：main() 内含 os.Exit 与全局 flag.Parse，无法在主测试进程安全复用。
// 故用"测试二进制即被测程序"模式：TestMain 检测 OPSMESH_SUB 环境变量，子进程重建
// os.Args 并替换 flag.CommandLine 后调用 main()/config.Load()，父进程用 exec.Command
// 驱动并断言退出码/输出。子进程执行的代码不计入 -cover 报告，但行为被充分验证。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/version"
)

// ---------- 子进程基础设施 ----------

// TestMain 实现"测试二进制即被测程序"子进程分发。
// 当 OPSMESH_SUB=main 时：重建 os.Args 调用真实 main()（--version 走 return，其余 os.Exit）。
// 当 OPSMESH_SUB=config 时：重建 os.Args 调用 config.Load() 并把结果 JSON 打到 stdout。
// 否则：正常运行测试套件。
func TestMain(m *testing.M) {
	switch os.Getenv("OPSMESH_SUB") {
	case "main", "config":
		os.Args = reconstructArgs()
		// 替换全局 FlagSet：testing 框架在 TestMain 之前已对原 CommandLine 调用过 flag.Parse，
		// 直接再调 config.Load 的 flag.Parse 会因"已解析"而 no-op，导致用不上我们模拟的 os.Args。
		// 用全新 FlagSet（ExitOnError）替换后，config.Load 注册的 flag 会解析重建后的 os.Args。
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		switch os.Getenv("OPSMESH_SUB") {
		case "main":
			main()
			os.Exit(0) // main 中 --version 走 return 到此；其余路径已 os.Exit 不会到达
		case "config":
			cfg := config.Load()
			_ = json.NewEncoder(os.Stdout).Encode(cfg)
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// reconstructArgs 从 OPSMESH_SUB_ARGS（JSON 编码的 []string）还原 os.Args，前置 "opsmesh" 作 argv[0]。
func reconstructArgs() []string {
	raw := os.Getenv("OPSMESH_SUB_ARGS")
	if raw == "" {
		return []string{"opsmesh"}
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		fmt.Fprintf(os.Stderr, "subprocess args decode failed: %v\n", err)
		os.Exit(99)
	}
	return append([]string{"opsmesh"}, args...)
}

func encodeArgs(args []string) string {
	b, _ := json.Marshal(args)
	return string(b)
}

// cleanEnv 剔除所有 OPSMESH_ 业务环境变量，保证子进程 flag 默认值不被父进程 env 污染。
func cleanEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "OPSMESH_") {
			continue
		}
		env = append(env, e)
	}
	return env
}

// runSubmain 以子进程运行真实 main()，返回 stdout、stderr、退出码。
func runSubmain(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(cleanEnv(),
		"OPSMESH_SUB=main",
		"OPSMESH_SUB_ARGS="+encodeArgs(args),
	)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), exitCode(err)
}

// runSubconfig 以子进程调用 config.Load()，返回解析后的 Config。
func runSubconfig(t *testing.T, args ...string) *config.Config {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(cleanEnv(),
		"OPSMESH_SUB=config",
		"OPSMESH_SUB_ARGS="+encodeArgs(args),
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("subconfig run failed: %v\nstdout=%s", err, out.String())
	}
	var cfg config.Config
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config json: %v\nraw=%s", err, out.String())
	}
	return &cfg
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// ---------- stdout 捕获（主进程内调用 main() 测 --version） ----------

// captureStdout 临时重定向 os.Stdout 捕获 f 的输出。用于安全调用 main() 的 --version 短路分支。
func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	f()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// ---------- runHealth 直接测试 ----------

// newHealthServer 启动一个返回指定状态码的 mock HTTP server，返回 server 与其监听端口。
func newHealthServer(t *testing.T, status int) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	port := portOf(t, srv)
	return srv, port
}

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// withArgs 临时设置 os.Args 并在返回时恢复，供 runHealth 直接测试使用。
func withArgs(args ...string) func() {
	old := os.Args
	os.Args = append([]string{"opsmesh"}, args...)
	return func() { os.Args = old }
}

func TestRunHealth_OK(t *testing.T) {
	srv, port := newHealthServer(t, http.StatusOK)
	defer srv.Close()
	defer withArgs("--health", "--http-port="+strconv.Itoa(port))()
	if code := runHealth(); code != 0 {
		t.Fatalf("runHealth=%d, want 0 (HTTP 200)", code)
	}
}

func TestRunHealth_Non200(t *testing.T) {
	srv, port := newHealthServer(t, http.StatusServiceUnavailable)
	defer srv.Close()
	defer withArgs("--health", "--http-port="+strconv.Itoa(port))()
	if code := runHealth(); code != 1 {
		t.Fatalf("runHealth=%d, want 1 (非 200)", code)
	}
}

func TestRunHealth_Unreachable(t *testing.T) {
	// 启动后立即关闭，得到一个"几乎必然未监听"的端口。
	srv, port := newHealthServer(t, http.StatusOK)
	srv.Close()
	defer withArgs("--health", "--http-port="+strconv.Itoa(port))()
	if code := runHealth(); code != 1 {
		t.Fatalf("runHealth=%d, want 1 (连接不可达)", code)
	}
}

func TestRunHealth_ShortFlag(t *testing.T) {
	// -health 单横线变体：覆盖 runHealth 内 -health 过滤分支。
	srv, port := newHealthServer(t, http.StatusOK)
	defer srv.Close()
	defer withArgs("-health", "--http-port="+strconv.Itoa(port))()
	if code := runHealth(); code != 0 {
		t.Fatalf("runHealth=%d, want 0 (-health 变体)", code)
	}
}

func TestRunHealth_BadFlag(t *testing.T) {
	// 未知 flag：FlagSet(ContinueOnError).Parse 返回 err → runHealth 返回 2。
	defer withArgs("--health", "--no-such-flag")()
	if code := runHealth(); code != 2 {
		t.Fatalf("runHealth=%d, want 2 (未知 flag)", code)
	}
}

func TestRunHealth_BadPort(t *testing.T) {
	// --http-port 非数字：fs.Int 解析失败 → Parse 返回 err → runHealth 返回 2。
	defer withArgs("--health", "--http-port=not-an-int")()
	if code := runHealth(); code != 2 {
		t.Fatalf("runHealth=%d, want 2 (非法端口号)", code)
	}
}

func TestRunHealth_DefaultPort(t *testing.T) {
	// 不传 --http-port 时 runHealth 默认连 8080。
	// 此处不绑定 8080（避免与真实环境冲突），仅验证默认值会触发连接失败 → 1，
	// 间接证明默认端口 8080 被使用（若默认值改变，断言仍成立但语义需复核）。
	// 环境加固（P2）：若本机 8080 已被其它服务占用（如开发机上的容器），
	// 连接会成功而非失败，断言失去意义——此时跳过而非误报。
	if conn, err := net.DialTimeout("tcp", "localhost:8080", 500*time.Millisecond); err == nil {
		conn.Close()
		t.Skip("本机 8080 端口已被占用（非 OpsMesh），跳过默认端口不可达断言")
	}
	defer withArgs("--health")()
	if code := runHealth(); code != 1 {
		t.Fatalf("runHealth=%d, want 1 (默认端口 8080 不可达)", code)
	}
}

// ---------- --version 输出测试 ----------

var versionLineRE = regexp.MustCompile(`^opsmesh \S+ \(commit=\S+ date=\S+\)\n$`)

func TestVersionString(t *testing.T) {
	got := versionString()
	want := fmt.Sprintf("opsmesh %s (commit=%s date=%s)\n", version.Version, version.Commit, version.Date)
	if got != want {
		t.Fatalf("versionString=%q, want %q", got, want)
	}
	if !versionLineRE.MatchString(got) {
		t.Fatalf("versionString %q 不匹配预期格式 ^opsmesh <v> (commit=<c> date=<d>)\\n$", got)
	}
}

// TestVersionOutputMain 在主进程调用 runMain() 捕获 stdout，覆盖 runMain 的 --version 短路分支。
// 安全性：os.Args=["opsmesh","--version"] 时 runMain 在 config.Load 之前 return 0，不触 flag/os.Exit。
func TestVersionOutputMain(t *testing.T) {
	defer withArgs("--version")()
	out := captureStdout(func() { runMain() })
	if out != versionString() {
		t.Fatalf("runMain --version stdout=%q, want %q", out, versionString())
	}
	if !versionLineRE.MatchString(out) {
		t.Fatalf("runMain --version 输出 %q 格式不符", out)
	}
}

func TestVersionShortFlagMain(t *testing.T) {
	defer withArgs("-version")()
	out := captureStdout(func() { runMain() })
	if out != versionString() {
		t.Fatalf("runMain -version stdout=%q, want %q", out, versionString())
	}
}

// TestRunMainHealthOK 在主进程调用 runMain() 走 --health 分支，覆盖 runMain 的 --health 短路。
// runMain 返回 runHealth() 的退出码而非 os.Exit，故可在主进程安全断言返回值。
func TestRunMainHealthOK(t *testing.T) {
	srv, port := newHealthServer(t, http.StatusOK)
	defer srv.Close()
	defer withArgs("--health", "--http-port="+strconv.Itoa(port))()
	if code := runMain(); code != 0 {
		t.Fatalf("runMain --health=%d, want 0 (HTTP 200)", code)
	}
}

func TestRunMainHealthFail(t *testing.T) {
	srv, port := newHealthServer(t, http.StatusServiceUnavailable)
	defer srv.Close()
	defer withArgs("--health", "--http-port="+strconv.Itoa(port))()
	if code := runMain(); code != 1 {
		t.Fatalf("runMain --health=%d, want 1 (非 200)", code)
	}
}

// TestVersionSubprocess 子进程端到端验证 --version 退出码 0 + 输出格式。
func TestVersionSubprocess(t *testing.T) {
	out, _, code := runSubmain(t, "--version")
	if code != 0 {
		t.Fatalf("--version exit=%d, want 0", code)
	}
	if out != versionString() {
		t.Fatalf("--version subprocess stdout=%q, want %q", out, versionString())
	}
}

// ---------- flag 解析测试（子进程隔离全局 flag） ----------

func TestFlagDefaults(t *testing.T) {
	cfg := runSubconfig(t) // 无任何参数
	if cfg.Mode != "controlplane" {
		t.Errorf("默认 Mode=%q, want controlplane", cfg.Mode)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("默认 HTTPPort=%d, want 8080", cfg.HTTPPort)
	}
	if cfg.GRPCPort != 9090 {
		t.Errorf("默认 GRPCPort=%d, want 9090", cfg.GRPCPort)
	}
	if cfg.MetricsPort != 9091 {
		t.Errorf("默认 MetricsPort=%d, want 9091", cfg.MetricsPort)
	}
}

func TestFlagOverride(t *testing.T) {
	cfg := runSubconfig(t, "--mode=agent", "--http-port=7000", "--grpc-port=8000", "--metrics-port=7001")
	if cfg.Mode != "agent" {
		t.Errorf("覆盖后 Mode=%q, want agent", cfg.Mode)
	}
	if cfg.HTTPPort != 7000 {
		t.Errorf("覆盖后 HTTPPort=%d, want 7000", cfg.HTTPPort)
	}
	if cfg.GRPCPort != 8000 {
		t.Errorf("覆盖后 GRPCPort=%d, want 8000", cfg.GRPCPort)
	}
	if cfg.MetricsPort != 7001 {
		t.Errorf("覆盖后 MetricsPort=%d, want 7001", cfg.MetricsPort)
	}
}

func TestFlagOverrideSingle(t *testing.T) {
	// 仅覆盖 --grpc-port，其余保持默认，验证 flag 间互不干扰。
	cfg := runSubconfig(t, "--grpc-port=12345")
	if cfg.GRPCPort != 12345 {
		t.Errorf("GRPCPort=%d, want 12345", cfg.GRPCPort)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("未覆盖的 HTTPPort=%d, want 8080", cfg.HTTPPort)
	}
	if cfg.Mode != "controlplane" {
		t.Errorf("未覆盖的 Mode=%q, want controlplane", cfg.Mode)
	}
}

// TestMainUnknownMode 验证 --mode=invalid 经 config.Load → Validate 失败 → main exit 1。
func TestMainUnknownMode(t *testing.T) {
	_, stderr, code := runSubmain(t, "--mode=invalid")
	if code != 1 {
		t.Fatalf("--mode=invalid exit=%d, want 1; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "非法 --mode") && !strings.Contains(stderr, "校验失败") {
		t.Fatalf("--mode=invalid stderr 未含预期错误: %s", stderr)
	}
}

// TestRunMainUnknownMode 在主进程覆盖 runMain 的 config.Load → Validate 失败 → return 1 路径。
// 难点：config.Load 用全局 flag.CommandLine，而 testing 框架在 TestMain 之前已对其调过 flag.Parse，
// 直接调 config.Load 的 flag.Parse 会 no-op 而用上 testing 的 -test.* 参数。故此处临时替换
// flag.CommandLine 为全新 FlagSet 并重建 os.Args，使 config.Load 解析我们模拟的 --mode=invalid，
// 测试结束后恢复原状。--mode 为显式 flag，优先于 env，故无需清理 OPSMESH_* 环境变量。
func TestRunMainUnknownMode(t *testing.T) {
	oldFS := flag.CommandLine
	oldArgs := os.Args
	defer func() { flag.CommandLine = oldFS; os.Args = oldArgs }()
	flag.CommandLine = flag.NewFlagSet("opsmesh", flag.ExitOnError)
	os.Args = []string{"opsmesh", "--mode=invalid"}

	if code := runMain(); code != 1 {
		t.Fatalf("runMain --mode=invalid=%d, want 1 (Validate 失败)", code)
	}
}

// ---------- --health 端到端子进程 ----------

// TestHealthSubprocessExitOK 子进程运行 main + --health，验证 main 的 --health 短路 + os.Exit(0)。
func TestHealthSubprocessExitOK(t *testing.T) {
	srv, port := newHealthServer(t, http.StatusOK)
	defer srv.Close()
	_, _, code := runSubmain(t, "--health", "--http-port="+strconv.Itoa(port))
	if code != 0 {
		t.Fatalf("subprocess --health exit=%d, want 0 (HTTP 200)", code)
	}
}

func TestHealthSubprocessExitFail(t *testing.T) {
	srv, port := newHealthServer(t, http.StatusInternalServerError)
	defer srv.Close()
	_, _, code := runSubmain(t, "--health", "--http-port="+strconv.Itoa(port))
	if code != 1 {
		t.Fatalf("subprocess --health exit=%d, want 1 (非 200)", code)
	}
}
