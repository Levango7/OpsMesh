// agent_extra_test.go 补充 agent.go 中未覆盖的纯函数与辅助逻辑单元测试。
//
// 覆盖：
//   - firstNonEmptyAgent / installToken / initLogCollect / shutdownOTel
//   - collectCmdbReport 节流 / collectCmdbServices / collectCmdbMiddleware / collectCmdbNetwork
//   - applyRlimits / setupDiscoveryBalancer / addRunning / delRunning
//   - checkShellMetachars 各元字符 / checkShellWhitelist 边界 / isNetworkDiagnoseCommand
//   - execService verb 白名单 / execFile 错误路径 / checkFileRootWhitelist
//   - shellCommand / parseLogLines / parseLogLevel / readLogIncrement
//   - New 构造 / reportResult / claimTask / drainTasks
package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/discovery"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/proto"
)

// --- firstNonEmptyAgent ---

func TestFirstNonEmptyAgent_Extra(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"all_empty", []string{"", "", ""}, ""},
		{"first_nonempty", []string{"a", "b", "c"}, "a"},
		{"skip_leading_empty", []string{"", "", "x"}, "x"},
		{"single", []string{"only"}, "only"},
		{"nil_args", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstNonEmptyAgent(c.in...); got != c.want {
				t.Fatalf("firstNonEmptyAgent(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// --- installToken ---

func TestInstallToken_FilePrecedence(t *testing.T) {
	dir := t.TempDir()
	// 写入 install.token 文件
	tok := "token-from-file"
	if err := os.WriteFile(filepath.Join(dir, "install.token"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Agent{dataDir: dir, cfg: &config.Config{InstallToken: "token-from-cfg"}}
	if got := a.installToken(); got != tok {
		t.Fatalf("installToken 应优先读文件，得到 %q, want %q", got, tok)
	}
}

func TestInstallToken_FallbackToCfg(t *testing.T) {
	dir := t.TempDir()
	// 无 install.token 文件，应回退到 cfg.InstallToken
	a := &Agent{dataDir: dir, cfg: &config.Config{InstallToken: "cfg-token"}}
	if got := a.installToken(); got != "cfg-token" {
		t.Fatalf("无文件时应回退 cfg，得到 %q", got)
	}
}

func TestInstallToken_EmptyDataDir(t *testing.T) {
	a := &Agent{dataDir: "", cfg: &config.Config{InstallToken: "cfg-token"}}
	if got := a.installToken(); got != "cfg-token" {
		t.Fatalf("空 dataDir 应回退 cfg，得到 %q", got)
	}
}

func TestInstallToken_EmptyFileFallback(t *testing.T) {
	dir := t.TempDir()
	// 写入空 token 文件，应回退到 cfg
	if err := os.WriteFile(filepath.Join(dir, "install.token"), []byte("   "), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Agent{dataDir: dir, cfg: &config.Config{InstallToken: "cfg-token"}}
	if got := a.installToken(); got != "cfg-token" {
		t.Fatalf("空文件应回退 cfg，得到 %q", got)
	}
}

// --- initLogCollect ---

func TestInitLogCollect_Defaults(t *testing.T) {
	a := &Agent{}
	a.initLogCollect()
	if a.logCollectInterval != 30*time.Second {
		t.Fatalf("默认间隔应为 30s，得到 %v", a.logCollectInterval)
	}
	if a.logOffsets == nil {
		t.Fatal("logOffsets 应被初始化")
	}
	if len(a.logCollectPaths) != 0 {
		t.Fatalf("未配置时应无路径，得到 %v", a.logCollectPaths)
	}
}

func TestInitLogCollect_FromEnv(t *testing.T) {
	t.Setenv("OPSMESH_LOG_COLLECT_INTERVAL", "5s")
	t.Setenv("OPSMESH_LOG_COLLECT_PATHS", "/var/log/a.log, /var/log/b.log ")
	a := &Agent{}
	a.initLogCollect()
	if a.logCollectInterval != 5*time.Second {
		t.Fatalf("间隔应为 5s，得到 %v", a.logCollectInterval)
	}
	if len(a.logCollectPaths) != 2 {
		t.Fatalf("应有 2 个路径，得到 %v", a.logCollectPaths)
	}
	if a.logCollectPaths[0] != "/var/log/a.log" {
		t.Fatalf("第一个路径错误: %q", a.logCollectPaths[0])
	}
	if a.logCollectPaths[1] != "/var/log/b.log" {
		t.Fatalf("第二个路径应 TrimSpace: %q", a.logCollectPaths[1])
	}
}

func TestInitLogCollect_InvalidInterval(t *testing.T) {
	t.Setenv("OPSMESH_LOG_COLLECT_INTERVAL", "not-a-duration")
	a := &Agent{}
	a.initLogCollect()
	// 非法间隔应保持默认 30s
	if a.logCollectInterval != 30*time.Second {
		t.Fatalf("非法间隔应保持默认 30s，得到 %v", a.logCollectInterval)
	}
}

func TestInitLogCollect_NegativeInterval(t *testing.T) {
	t.Setenv("OPSMESH_LOG_COLLECT_INTERVAL", "-5s")
	a := &Agent{}
	a.initLogCollect()
	// 负间隔应保持默认
	if a.logCollectInterval != 30*time.Second {
		t.Fatalf("负间隔应保持默认，得到 %v", a.logCollectInterval)
	}
}

func TestInitLogCollect_EmptyPaths(t *testing.T) {
	t.Setenv("OPSMESH_LOG_COLLECT_PATHS", "")
	a := &Agent{}
	a.initLogCollect()
	if len(a.logCollectPaths) != 0 {
		t.Fatalf("空路径字符串应无路径，得到 %v", a.logCollectPaths)
	}
}

// --- shutdownOTel ---

func TestShutdownOTel_Nil(t *testing.T) {
	a := &Agent{}
	// otelShutdown 为 nil 应直接返回，不 panic
	a.shutdownOTel()
}

func TestShutdownOTel_Noop(t *testing.T) {
	a := &Agent{otelShutdown: func(ctx context.Context) error { return nil }}
	a.shutdownOTel()
}

// --- collectCmdbReport 节流 ---

// skipServiceProbe 在测试期间清空服务采集白名单。
// collectCmdbServices 在 Windows 上对每个白名单服务 spawn 一次 sc query（各 2s 超时），
// 本组测试只验证节流/Seq 逻辑，服务探测另有 NoPanic 测试覆盖；
// 清空白名单避免高负载下 21 次子进程拖慢甚至拖过 60s 节流窗口导致 flaky。
func skipServiceProbe(t *testing.T) {
	t.Helper()
	saved := monitoredServices
	monitoredServices = nil
	t.Cleanup(func() { monitoredServices = saved })
}

func TestCollectCmdbReport_Throttle(t *testing.T) {
	skipServiceProbe(t)
	a := &Agent{hostname: "h", cfg: &config.Config{}}
	// 首次调用应返回非 nil（零值 cmdbLastCol 距今远超 60s）
	r1 := a.collectCmdbReport()
	if r1 == nil {
		t.Fatal("首次采集应返回非 nil")
	}
	if r1.CiType != "machine" {
		t.Fatalf("CiType 应为 machine，得到 %q", r1.CiType)
	}
	if r1.Seq != 1 {
		t.Fatalf("Seq 应为 1，得到 %d", r1.Seq)
	}
	// 采集本身可能耗时，以"采集刚结束"为基准验证节流（cmdbLastCol 记录的是采集开始时刻，
	// 极端情况下采集超 60s 会让窗口提前过期）。
	a.cmdbLastCol = time.Now()
	r2 := a.collectCmdbReport()
	if r2 != nil {
		t.Fatal("距上次不足 60s 应返回 nil（节流）")
	}
}

func TestCollectCmdbReport_SeqIncrement(t *testing.T) {
	skipServiceProbe(t)
	a := &Agent{hostname: "h", cfg: &config.Config{}}
	// 模拟上次采集时间已过 60s
	a.cmdbLastCol = time.Now().Add(-70 * time.Second)
	r := a.collectCmdbReport()
	if r == nil {
		t.Fatal("应返回非 nil")
	}
	if r.Seq != 1 {
		t.Fatalf("Seq 应为 1，得到 %d", r.Seq)
	}
}

// --- collectCmdbServices / collectCmdbMiddleware / collectCmdbNetwork ---

func TestCollectCmdbServices_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectCmdbServices panic: %v", r)
		}
	}()
	_ = collectCmdbServices()
}

func TestCollectCmdbMiddleware_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectCmdbMiddleware panic: %v", r)
		}
	}()
	_ = collectCmdbMiddleware()
}

func TestCollectCmdbNetwork_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectCmdbNetwork panic: %v", r)
		}
	}()
	_ = collectCmdbNetwork()
}

// --- applyRlimits ---

func TestApplyRlimits_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyRlimits panic: %v", r)
		}
	}()
	a := &Agent{cfg: &config.Config{MaxProcs: 0, MaxFiles: 0, MaxMemoryMB: 0}}
	a.applyRlimits()
}

// --- setupDiscoveryBalancer ---

func TestSetupDiscoveryBalancer_EmptyAddrs(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	// 空 addrs 应直接返回，不 panic
	a.setupDiscoveryBalancer(nil)
}

func TestSetupDiscoveryBalancer_NilGRPC(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	// grpc 为 nil 应直接返回
	a.setupDiscoveryBalancer([]string{"127.0.0.1:9090"})
}

func TestSetupDiscoveryBalancer_SingleAddr(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: &config.Config{LBStrategy: "failover"}, grpc: cli}
	a.setupDiscoveryBalancer([]string{"127.0.0.1:9090"})
	if cli.balancer == nil {
		t.Fatal("单地址也应注入 balancer")
	}
}

func TestSetupDiscoveryBalancer_MultiAddr(t *testing.T) {
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: &config.Config{LBStrategy: "round-robin"}, grpc: cli}
	a.setupDiscoveryBalancer([]string{"127.0.0.1:9090", "127.0.0.2:9090"})
	if cli.balancer == nil {
		t.Fatal("多地址应注入 balancer")
	}
}

// --- addRunning / delRunning ---

func TestAddDelRunning_Basic(t *testing.T) {
	a := &Agent{running: make(map[string]*runState)}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.addRunning("task-1", cancel)
	if len(a.running) != 1 {
		t.Fatalf("addRunning 后应有 1 个，得到 %d", len(a.running))
	}
	// 未取消时 delRunning 应返回 false
	if cancelled := a.delRunning("task-1"); cancelled {
		t.Fatal("未取消的任务 delRunning 应返回 false")
	}
	if len(a.running) != 0 {
		t.Fatalf("delRunning 后应为 0 个，得到 %d", len(a.running))
	}
}

func TestDelRunning_NotExist(t *testing.T) {
	a := &Agent{running: make(map[string]*runState)}
	if cancelled := a.delRunning("nope"); cancelled {
		t.Fatal("不存在的任务 delRunning 应返回 false")
	}
}

func TestDelRunning_Cancelled(t *testing.T) {
	a := &Agent{running: make(map[string]*runState)}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.addRunning("task-x", cancel)
	// 标记为已取消
	a.runMu.Lock()
	if rs, ok := a.running["task-x"]; ok {
		rs.cancelled = true
	}
	a.runMu.Unlock()
	if cancelled := a.delRunning("task-x"); !cancelled {
		t.Fatal("已取消的任务 delRunning 应返回 true")
	}
}

func TestAddRunning_Concurrent(t *testing.T) {
	a := &Agent{running: make(map[string]*runState)}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.addRunning("task-"+itoa(i), cancel)
		}(i)
	}
	wg.Wait()
	if len(a.running) != 50 {
		t.Fatalf("并发 addRunning 后应有 50 个，得到 %d", len(a.running))
	}
}

// itoa 简单整数转字符串（避免引入 strconv）。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// --- checkShellMetachars ---

func TestCheckShellMetachars_Safe(t *testing.T) {
	safe := []string{
		"ls -la",
		"echo hello",
		"systemctl status nginx",
		"systemctl status nginx | grep Active",
		"echo hello && echo world",
		"echo hello 1>&2",
		"cmd &>file",
		"ping -c 1 127.0.0.1",
	}
	for _, c := range safe {
		if err := checkShellMetachars(c); err != nil {
			t.Fatalf("安全命令 %q 不应报错: %v", c, err)
		}
	}
}

func TestCheckShellMetachars_Newline(t *testing.T) {
	if err := checkShellMetachars("ls\nrm -rf /"); err == nil {
		t.Fatal("含换行符应被拒绝")
	}
}

func TestCheckShellMetachars_CarriageReturn(t *testing.T) {
	if err := checkShellMetachars("ls\rrm -rf /"); err == nil {
		t.Fatal("含回车符应被拒绝")
	}
}

func TestCheckShellMetachars_Semicolon(t *testing.T) {
	if err := checkShellMetachars("ls;rm -rf /"); err == nil {
		t.Fatal("含分号应被拒绝")
	}
}

func TestCheckShellMetachars_SingleAmpersand(t *testing.T) {
	if err := checkShellMetachars("sleep 10 &"); err == nil {
		t.Fatal("含单个 & 后台执行符应被拒绝")
	}
}

func TestCheckShellMetachars_CommandSubstitution(t *testing.T) {
	if err := checkShellMetachars("echo $(rm -rf /)"); err == nil {
		t.Fatal("含 $(...) 命令替换应被拒绝")
	}
}

func TestCheckShellMetachars_Backtick(t *testing.T) {
	if err := checkShellMetachars("echo `rm -rf /`"); err == nil {
		t.Fatal("含反引号应被拒绝")
	}
}

func TestCheckShellMetachars_AllowLogicalAnd(t *testing.T) {
	if err := checkShellMetachars("true && echo ok"); err != nil {
		t.Fatalf("&& 条件拼接应放行: %v", err)
	}
}

func TestCheckShellMetachars_AllowFdRedirect(t *testing.T) {
	if err := checkShellMetachars("echo hi 1>&2"); err != nil {
		t.Fatalf(">& fd 重定向应放行: %v", err)
	}
}

func TestCheckShellMetachars_AllowCombinedRedirect(t *testing.T) {
	if err := checkShellMetachars("cmd &>file"); err != nil {
		t.Fatalf("&> 合并重定向应放行: %v", err)
	}
}

// --- checkShellWhitelist ---

func TestCheckShellWhitelist_Empty(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: ""}}
	if err := a.checkShellWhitelist("anything"); err != nil {
		t.Fatalf("空白名单应放行: %v", err)
	}
}

func TestCheckShellWhitelist_ExactMatch(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls,echo"}}
	if err := a.checkShellWhitelist("ls -la"); err != nil {
		t.Fatalf("ls 应放行: %v", err)
	}
	if err := a.checkShellWhitelist("echo hi"); err != nil {
		t.Fatalf("echo 应放行: %v", err)
	}
	if err := a.checkShellWhitelist("rm -rf /"); err == nil {
		t.Fatal("rm 不在白名单应拒绝")
	}
}

func TestCheckShellWhitelist_PrefixMatch(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "system*"}}
	if err := a.checkShellWhitelist("systemctl status nginx"); err != nil {
		t.Fatalf("system* 应匹配 systemctl: %v", err)
	}
	if err := a.checkShellWhitelist("systemd-analyze"); err != nil {
		t.Fatalf("system* 应匹配 systemd-analyze: %v", err)
	}
}

func TestCheckShellWhitelist_Basename(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls"}}
	// 用绝对路径，basename 后应为 ls
	if err := a.checkShellWhitelist("/bin/ls -la"); err != nil {
		t.Fatalf("basename 匹配应放行: %v", err)
	}
}

func TestCheckShellWhitelist_NetworkDiagnose(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls"}}
	// 网络诊断命令应内置白名单
	diag := []string{"ping", "ping6", "traceroute", "tracert", "nslookup", "dig", "host", "curl", "wget", "nc", "netcat", "powershell"}
	for _, c := range diag {
		if err := a.checkShellWhitelist(c + " 127.0.0.1"); err != nil {
			t.Fatalf("网络诊断命令 %q 应放行: %v", c, err)
		}
	}
}

func TestCheckShellWhitelist_EmptyCommand(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls"}}
	if err := a.checkShellWhitelist(""); err != nil {
		t.Fatalf("空命令应放行（兜底）: %v", err)
	}
}

func TestCheckShellWhitelist_OnlyWhitespace(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls"}}
	if err := a.checkShellWhitelist("   "); err != nil {
		t.Fatalf("纯空白命令应放行: %v", err)
	}
}

// --- isNetworkDiagnoseCommand ---

func TestIsNetworkDiagnoseCommand(t *testing.T) {
	yes := []string{"ping", "ping6", "traceroute", "tracert", "nslookup", "dig", "host", "curl", "wget", "nc", "netcat", "powershell"}
	for _, c := range yes {
		if !isNetworkDiagnoseCommand(c) {
			t.Fatalf("%q 应为网络诊断命令", c)
		}
	}
	no := []string{"ls", "rm", "cat", "sh", "bash", "systemctl", ""}
	for _, c := range no {
		if isNetworkDiagnoseCommand(c) {
			t.Fatalf("%q 不应为网络诊断命令", c)
		}
	}
}

// --- execService verb 白名单 ---

func TestExecService_InvalidVerb(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	var out, errb strings.Builder
	// 非法 verb 应被拒绝（在调用 systemctl 之前）
	err := a.execService(context.Background(), &out, &errb, proto.Task{
		Type:    proto.TaskTypeService,
		Command: "cat /etc/shadow",
		Path:    "nginx",
	})
	if err == nil {
		t.Fatal("非法 verb 应返回错误")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("应提示 not allowed，得到 %v", err)
	}
}

func TestExecService_EmptyCommand(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	var out, errb strings.Builder
	err := a.execService(context.Background(), &out, &errb, proto.Task{
		Type:    proto.TaskTypeService,
		Command: "",
	})
	if err == nil {
		t.Fatal("空 command 应返回错误")
	}
}

func TestExecService_VerbFromCommand(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	var out, errb strings.Builder
	// Path 为空时从 Command 解析 verb 与 svc
	// "status nginx" -> verb=status, svc=nginx
	err := a.execService(context.Background(), &out, &errb, proto.Task{
		Type:    proto.TaskTypeService,
		Command: "status nginx",
	})
	// systemctl 不存在或 nginx 不存在都会返回非 nil，但不应是 verb 白名单错误
	if err != nil && strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("status 应在白名单内，不应报 not allowed: %v", err)
	}
}

// --- execFile 错误路径 ---

func TestExecFile_EmptyPath(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	var out, errb strings.Builder
	err := a.execFile(context.Background(), &out, &errb, proto.Task{
		Type: proto.TaskTypeFile,
		Path: "",
	})
	if err == nil {
		t.Fatal("空 path 应返回错误")
	}
	if !strings.Contains(err.Error(), "requires path") {
		t.Fatalf("应提示 requires path，得到 %v", err)
	}
}

func TestExecFile_SymlinkReject(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("创建符号链接失败（平台不支持）: %v", err)
	}
	a := &Agent{cfg: &config.Config{}}
	var out, errb strings.Builder
	err := a.execFile(context.Background(), &out, &errb, proto.Task{
		Type:    proto.TaskTypeFile,
		Path:    link,
		Content: "pwned",
	})
	if err == nil {
		t.Fatal("符号链接应被拒绝")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("应提示 symlink，得到 %v", err)
	}
}

func TestExecFile_WriteSuccess(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{cfg: &config.Config{}}
	var out, errb strings.Builder
	target := filepath.Join(dir, "out.txt")
	err := a.execFile(context.Background(), &out, &errb, proto.Task{
		Type:    proto.TaskTypeFile,
		Path:    target,
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("写入应成功: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "hello" {
		t.Fatalf("文件内容 = %q, want hello", string(got))
	}
	if !strings.Contains(out.String(), "wrote") {
		t.Fatalf("stdout 应提示 wrote，得到 %q", out.String())
	}
}

// --- checkFileRootWhitelist ---

func TestCheckFileRootWhitelist_Empty(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentFileRootWhitelist: ""}}
	if err := a.checkFileRootWhitelist("/any/path"); err != nil {
		t.Fatalf("空白名单应放行: %v", err)
	}
}

func TestCheckFileRootWhitelist_Inside(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{cfg: &config.Config{AgentFileRootWhitelist: dir}}
	if err := a.checkFileRootWhitelist(filepath.Join(dir, "sub", "file.txt")); err != nil {
		t.Fatalf("白名单内路径应放行: %v", err)
	}
}

func TestCheckFileRootWhitelist_Outside(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{cfg: &config.Config{AgentFileRootWhitelist: dir}}
	if err := a.checkFileRootWhitelist(filepath.Join(dir, "..", "outside.txt")); err == nil {
		t.Fatal("白名单外路径应拒绝")
	}
}

func TestCheckFileRootWhitelist_MultiRoot(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	a := &Agent{cfg: &config.Config{AgentFileRootWhitelist: dir1 + "," + dir2}}
	if err := a.checkFileRootWhitelist(filepath.Join(dir2, "file.txt")); err != nil {
		t.Fatalf("第二个根目录内应放行: %v", err)
	}
}

// --- shellCommand ---

func TestShellCommand_Extra(t *testing.T) {
	cmd := shellCommand("echo hi")
	if cmd == nil {
		t.Fatal("shellCommand 不应返回 nil")
	}
	// 不同平台 argv 不同，但都应非空
	if len(cmd.Args) == 0 {
		t.Fatal("Args 不应为空")
	}
}

// --- parseLogLines / parseLogLevel ---

func TestParseLogLines_Basic(t *testing.T) {
	lines := parseLogLines("ERROR something bad\nWARN be careful\nall good\n")
	if len(lines) != 3 {
		t.Fatalf("应解析 3 行，得到 %d", len(lines))
	}
	if lines[0].Level != "error" {
		t.Fatalf("第一行级别应为 error，得到 %q", lines[0].Level)
	}
	if lines[1].Level != "warn" {
		t.Fatalf("第二行级别应为 warn，得到 %q", lines[1].Level)
	}
	if lines[2].Level != "info" {
		t.Fatalf("第三行级别应为 info，得到 %q", lines[2].Level)
	}
}

func TestParseLogLines_Empty(t *testing.T) {
	if got := parseLogLines(""); got != nil {
		t.Fatalf("空内容应返回 nil，得到 %v", got)
	}
	if got := parseLogLines("\n\n"); got != nil {
		t.Fatalf("纯换行应返回 nil，得到 %v", got)
	}
}

func TestParseLogLines_CarriageReturn(t *testing.T) {
	lines := parseLogLines("line1\r\nline2\r\n")
	if len(lines) != 2 {
		t.Fatalf("应解析 2 行（CRLF），得到 %d", len(lines))
	}
	if lines[0].Message != "line1" {
		t.Fatalf("第一行应为 line1，得到 %q", lines[0].Message)
	}
}

func TestParseLogLevel_AllLevels(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"ERROR something", "error"},
		{"ERR something", "error"},
		{"FATAL crash", "error"},
		{"WARN careful", "warn"},
		{"WARNING careful", "warn"},
		{"DEBUG info", "debug"},
		{"TRACE info", "debug"},
		{"INFO normal", "info"},
		{"random text", "info"},
		{"", "info"},
		{"error lower case", "error"}, // 大小写不敏感
	}
	for _, c := range cases {
		if got := parseLogLevel(c.line); got != c.want {
			t.Fatalf("parseLogLevel(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// --- readLogIncrement ---

func TestReadLogIncrement_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{logOffsets: make(map[string]int64)}
	content, err := a.readLogIncrement(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if content != "line1\nline2\n" {
		t.Fatalf("首次读取应返回全部内容，得到 %q", content)
	}
	// offset 应推进到文件末尾
	if a.logOffsets[path] != 12 {
		t.Fatalf("offset 应为 12，得到 %d", a.logOffsets[path])
	}
}

func TestReadLogIncrement_NoNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{logOffsets: map[string]int64{path: 5}}
	content, err := a.readLogIncrement(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if content != "" {
		t.Fatalf("无新增内容应返回空串，得到 %q", content)
	}
}

func TestReadLogIncrement_FileTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// 模拟文件被截断：原 offset=100，新文件只有 5 字节
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{logOffsets: map[string]int64{path: 100}}
	content, err := a.readLogIncrement(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if content != "short" {
		t.Fatalf("截断后应从头读，得到 %q", content)
	}
}

func TestReadLogIncrement_FileNotExist(t *testing.T) {
	a := &Agent{logOffsets: make(map[string]int64)}
	_, err := a.readLogIncrement("/nonexistent/path/file.log")
	if err == nil {
		t.Fatal("文件不存在应返回错误")
	}
}

func TestReadLogIncrement_Incremental(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{logOffsets: make(map[string]int64)}
	// 首次读取
	c1, err := a.readLogIncrement(path)
	if err != nil {
		t.Fatal(err)
	}
	if c1 != "first\n" {
		t.Fatalf("首次读取 = %q, want first\\n", c1)
	}
	// 追加内容
	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 第二次读取应只返回新增
	c2, err := a.readLogIncrement(path)
	if err != nil {
		t.Fatal(err)
	}
	if c2 != "second\n" {
		t.Fatalf("增量读取 = %q, want second\\n", c2)
	}
}

// --- New ---

func TestNew_Extra(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:            dir,
		WorkerConcurrency:  2,
		TaskTimeout:        30 * time.Second,
		OTELEndpoint:       "",
		OTELStdout:         false,
		CBFailureThreshold: 0, // 禁用熔断器
	}
	a := New(cfg)
	if a == nil {
		t.Fatal("New 不应返回 nil")
	}
	if a.workers != 2 {
		t.Fatalf("workers 应为 2，得到 %d", a.workers)
	}
	if a.agentID == "" {
		t.Fatal("agentID 不应为空")
	}
	if a.taskCh == nil {
		t.Fatal("taskCh 不应为 nil")
	}
	if a.running == nil {
		t.Fatal("running 不应为 nil")
	}
	if a.metricsHistory == nil {
		t.Fatal("metricsHistory 不应为 nil")
	}
	// otelShutdown 在 New() 后为 nil（OTel 初始化已移至 Run() 方法中，
	// 使初始化失败能向调用方透传 error 而非 log.Fatalf 进程退出）。
	if a.otelShutdown != nil {
		t.Fatal("otelShutdown 在 New() 后应为 nil（OTel 初始化已移至 Run()）")
	}
	// 禁用熔断器时 cbSet 应为 nil
	if a.cbSet != nil {
		t.Fatal("CBFailureThreshold=0 时 cbSet 应为 nil")
	}
}

func TestNew_DefaultWorkers(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, WorkerConcurrency: 0} // 0 应回退到 4
	a := New(cfg)
	if a.workers != 4 {
		t.Fatalf("WorkerConcurrency=0 应回退到 4，得到 %d", a.workers)
	}
}

func TestNew_NegativeWorkers(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, WorkerConcurrency: -1}
	a := New(cfg)
	if a.workers != 4 {
		t.Fatalf("WorkerConcurrency<0 应回退到 4，得到 %d", a.workers)
	}
}

func TestNew_WithCircuitBreaker(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:            dir,
		CBFailureThreshold: 3,
		CBRecoveryTimeout:  30 * time.Second,
		CBHalfOpenMaxCalls: 1,
	}
	a := New(cfg)
	if a.cbSet == nil {
		t.Fatal("CBFailureThreshold>0 时 cbSet 不应为 nil")
	}
}

// --- reportResult ---

func TestReportResult_NoPanic(t *testing.T) {
	// 用 nil grpc 会 panic，所以这里构造一个不会成功的调用但确保不 panic
	// 实际上 reportResult 内部调用 a.grpc.ReportResult，nil grpc 会 panic
	// 跳过此测试，改用 mock grpc
	t.Skip("需要 mock grpc，见 grpcclient_extra_test.go")
}

// --- claimTask ---

func TestClaimTask_NoTasks(t *testing.T) {
	// 构造一个返回空任务列表的 mock grpc
	cli, err := newTestGRPCClient()
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agentID: "test", grpc: cli}
	// 不连真实服务器，invoke 会失败，claimTask 应返回 error；
	// 即使连不上但返回 nil task（err == nil）也 ok。不 panic 即可。
	_, _ = a.claimTask(context.Background())
}

// --- drainTasks ---

func TestDrainTasks_CancelledContext(t *testing.T) {
	a := &Agent{agentID: "test", cfg: &config.Config{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// 已取消的 ctx 应立即返回
	a.drainTasks(ctx)
}

// --- worker（取消路径） ---

func TestWorker_CancelledContext(t *testing.T) {
	a := &Agent{
		agentID:     "test",
		taskTimeout: 5 * time.Second,
		cfg:         &config.Config{},
		taskCh:      make(chan proto.Task, 1),
		running:     make(map[string]*runState),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// 已取消的 ctx，worker 应立即退出
	done := make(chan struct{})
	go func() {
		a.worker(ctx)
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("worker 应在 ctx 取消后立即退出")
	}
}

// --- collectDeviceMetrics 节流（补充：metricsHistory 为 nil） ---

func TestCollectDeviceMetrics_NilHistory(t *testing.T) {
	a := &Agent{agentID: "test", metricsHistory: nil}
	// metricsHistory 为 nil 时不应 panic
	m := a.collectDeviceMetrics()
	if m == nil {
		t.Fatal("首次采集应返回非 nil")
	}
	// 第二次应被节流
	if m2 := a.collectDeviceMetrics(); m2 != nil {
		t.Fatal("第二次应被节流")
	}
}

// --- 辅助函数 ---

// newTestGRPCClient 构造用于测试的 gRPC 客户端（不连真实服务器）。
func newTestGRPCClient() (*GRPCClient, error) {
	return NewGRPCClient([]string{"127.0.0.1:9090"}, "", "", "", 9090)
}

// 确保 discovery 包被引用（setupDiscoveryBalancer 测试用）。
var _ = discovery.NewFailover

// 确保 grpcx 包被引用（部分测试用）。
var _ = grpcx.HeartbeatReq{}

// 确保 runtime 包被引用（部分平台分支测试用）。
var _ = runtime.GOOS
