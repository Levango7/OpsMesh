package controlplane

// shellquote_security_test.go — shellQuote 全量修正（17 文件 / 269 调用点）与
// 三处新收编点（db_cloud_managed_ops / elasticsearch_ops_ext / db_nosql_ext_ops HBase）
// 的注入守卫（2026-11-05，取证见 docs/opsmesh-authz-audit-2026-11-04.md）。
//
// 为什么值得单独立守卫：旧 shellQuote 只转义 `"`——双引号段内 `$`、反引号、`\`
// 仍有特殊语义，`shellQuote("$(id)")` 产出 "$(id)" 会被 shell **真实执行**；
// 结尾反斜杠还会吞掉收尾引号破坏配对。这是一个**静默**失效：脚本照常下发、
// 只在执行时见分晓。本文件把「转义后无可展开结构」钉成构建期红。

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

func newShellQuoteServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

// sqPost 以 admin（网关注入路径）发一条 JSON POST——被收编的 handler 多数要求
// system:write，故测试走 admin 角色，专注注入面而非鉴权面（鉴权面另有用例覆盖）。
func sqPost(t *testing.T, s *Server, h http.HandlerFunc, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/sq-audit", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "sq-audit")
	req.Header.Set("X-User-Roles", "admin")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// hasUnescapedDoubleQuoteSpecial 是 QuoteDouble 语义的判据：去掉外壳两端双引号后，
// 正文里任何 `$`、反引号或 `"` 都必须是转义态（前一字符为 `\`）。
// 旧实现（只转义 "）在这组载荷上全部判红。
func hasUnescapedDoubleQuoteSpecial(s string) bool {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return true // 形状不对同样视为不安全
	}
	body := s[1 : len(s)-1]
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++ // 被转义的下一个字符，跳过
		case '$', '`', '"':
			return true
		}
	}
	return false
}

// TestShellQuoteNeutralizesExpansion 直接钉 shellQuote 语义。
// 窄变异：把实现改回 `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` ⇒ 本用例判红。
func TestShellQuoteNeutralizesExpansion(t *testing.T) {
	payloads := []string{
		`$(id)`,
		"`id`",
		`$HOME`,
		`${IFS}`,
		`a"; touch /tmp/j; echo "`,
		`back\`,
		`\\`,
		`$`,
		"`",
	}
	for _, p := range payloads {
		q := shellQuote(p)
		if hasUnescapedDoubleQuoteSpecial(q) {
			t.Errorf("shellQuote(%q) = %q：仍含未转义展开结构（可注入）", p, q)
		}
	}
}

// TestShellQuoteEmptyAndPlain 形状回归：常规值与空串的产出不得改变——
// 一批调用点把结果嵌在既有双引号段内（如 `".backup %s"`），形状是兼容面。
func TestShellQuoteEmptyAndPlain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{"plain", `"plain"`},
		{"ns:table", `"ns:table"`},
		{"a-b.c_d-1", `"a-b.c_d-1"`},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestShellQuoteTrailingBackslashDoesNotBreakPairing 结尾反斜杠回归：
// 旧实现产出 "abc\" —— 反斜杠吞掉收尾引号、引号配对破坏，后续任意文本可被拼接。
func TestShellQuoteTrailingBackslashDoesNotBreakPairing(t *testing.T) {
	got := shellQuote("abc\\")
	if got != `"abc\\"` {
		t.Errorf("shellQuote(abc\\) = %q, want %q", got, `"abc\\"`)
	}
}

// ---------------------------------------------------------------- 新收编点（handler 级）

// TestESNodeDrainRejectsInjectionPayloads：node 会嵌进 `-d '{"...":"%s"}'`
// ——shell 单引号内的 JSON 文本，没有任何合法转义空间（加引号破坏外层，
// JSON 语法还要求转义 " 与 \），因此必须白名单拒绝。
func TestESNodeDrainRejectsInjectionPayloads(t *testing.T) {
	s := newShellQuoteServer()
	for _, bad := range []string{
		`$(id)`,
		"`id`",
		`n'; touch /tmp/x #`,
		`n"x`,
		"n x",
		`ns:n`,
		`a/b`,
	} {
		rec := sqPost(t, s, s.handleElasticsearchNodeDrain,
			map[string]interface{}{"deviceID": "sq-dev", "action": "drain", "node": bad})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("node=%q 应 400，实得 %d：%s", bad, rec.Code, rec.Body.String())
		}
	}
	// 合法名放行（下游 202/任务结果不在本断言射程）
	rec := sqPost(t, s, s.handleElasticsearchNodeDrain,
		map[string]interface{}{"deviceID": "sq-dev", "action": "drain", "node": "es-node-01"})
	if rec.Code == http.StatusBadRequest {
		t.Errorf("合法节点名被误拒：%s", rec.Body.String())
	}
}

// TestESShardRelocateRejectsInjectionPayloads：index/from/to 三值同 drain 的处境。
func TestESShardRelocateRejectsInjectionPayloads(t *testing.T) {
	s := newShellQuoteServer()
	cases := []map[string]interface{}{
		{"deviceID": "sq-dev", "action": "relocate", "index": "$(id)", "shard": 1, "from": "n1", "to": "n2"},
		{"deviceID": "sq-dev", "action": "relocate", "index": "idx", "shard": 1, "from": "`id`", "to": "n2"},
		{"deviceID": "sq-dev", "action": "relocate", "index": "idx", "shard": 1, "from": "n1", "to": `n"; x`},
	}
	for i, body := range cases {
		rec := sqPost(t, s, s.handleElasticsearchShardRelocate, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("relocate case#%d 应 400，实得 %d：%s", i, rec.Code, rec.Body.String())
		}
	}
	rec := sqPost(t, s, s.handleElasticsearchShardRelocate,
		map[string]interface{}{"deviceID": "sq-dev", "action": "relocate", "index": "idx-1", "shard": 0, "from": "node-1", "to": "node-2"})
	if rec.Code == http.StatusBadRequest {
		t.Errorf("合法参数被误拒：%s", rec.Body.String())
	}
}

// TestCloudManagedParamsQuotedInScript：云 CLI 参数走引号层（不是白名单）——
// 注入载荷必须作为**转义后的字面量**进入下发脚本，不得原样拼接。
func TestCloudManagedParamsQuotedInScript(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`
	rec := sqPost(t, s, s.handleDBAurora,
		map[string]interface{}{"deviceID": "sq-cli-dev", "action": "status", "id": payload})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status 载荷应被受理（引号层保护）：%d %s", rec.Code, rec.Body.String())
	}
	var cmd string
	for _, tk := range s.store.GetTasks("sq-cli-dev") {
		if tk != nil {
			cmd = tk.Command
			break
		}
	}
	if cmd == "" {
		t.Fatal("未找到派发任务")
	}
	if !strings.Contains(cmd, `"\$(id)"`) {
		t.Errorf("脚本未含转义字面量 %q：%s", `"\$(id)"`, cmd)
	}
	if strings.Contains(cmd, `"$(id)"`) {
		t.Errorf("脚本含未转义形态（可注入）：%s", cmd)
	}
}

// TestCloudManagedAllProvidersQuote 六家云厂商逐一验证转义路径生效：
// 每家取一个 action，断言注入载荷以转义形态进入脚本。
func TestCloudManagedAllProvidersQuote(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`
	cases := []struct {
		name string
		h    http.HandlerFunc
		body map[string]interface{}
	}{
		{"aurora", s.handleDBAurora, map[string]interface{}{"deviceID": "d1", "action": "delete", "id": payload}},
		{"cloudsql", s.handleDBCloudSQL, map[string]interface{}{"deviceID": "d2", "action": "status", "name": payload}},
		{"azuresql", s.handleDBAzureSQL, map[string]interface{}{"deviceID": "d3", "action": "status", "server": payload}},
		{"polardb", s.handleDBPolarDB, map[string]interface{}{"deviceID": "d4", "action": "delete", "id": payload}},
		{"gaussdb", s.handleDBGaussDBDWS, map[string]interface{}{"deviceID": "d5", "action": "delete", "id": payload}},
		{"tdsqlc", s.handleDBTDSQLC, map[string]interface{}{"deviceID": "d6", "action": "delete", "id": payload}},
	}
	for _, c := range cases {
		rec := sqPost(t, s, c.h, c.body)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s 应受理：%d %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		dev, _ := c.body["deviceID"].(string)
		var cmd string
		for _, tk := range s.store.GetTasks(dev) {
			if tk != nil {
				cmd = tk.Command
				break
			}
		}
		if cmd == "" {
			t.Errorf("%s 未找到派发任务", c.name)
			continue
		}
		if !strings.Contains(cmd, `"\$(id)"`) {
			t.Errorf("%s 脚本未含转义字面量：%s", c.name, cmd)
		}
	}
}

// ---------------------------------------------------------------- 第三批：git_unix_ops.go（28 处）

// sqDispatchScript 取某设备第一条已派发任务的脚本体（本批 handler 均经 dispatchKafkaTask）。
func sqDispatchScript(t *testing.T, s *Server, dev string) string {
	t.Helper()
	for _, tk := range s.store.GetTasks(dev) {
		if tk != nil && tk.Command != "" {
			return tk.Command
		}
	}
	t.Fatalf("设备 %s 未找到派发任务", dev)
	return ""
}

// TestGitOpsInjectionQuoted 钉 Git 段：token 与回显消息都必须以转义形态进入脚本。
// 修前实测：git clone 直接裸拼（`git clone %s %s`）、commit 消息嵌在 `-m "%s"` 的
// 双引号段里且未转义 `$`/反引号——两者都可用 `$(id)` 类载荷注入。
func TestGitOpsInjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`

	// clone：URL / path / 回显消息三处
	rec := sqPost(t, s, s.handleOSGit, map[string]interface{}{
		"deviceID": "sq-git-1", "action": "clone",
		"repoUrl": "https://ex/" + payload, "path": payload + "-path",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("clone 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-git-1")
	for _, want := range []string{
		`git clone "https://ex/\$(id)" "\$(id)-path"`,
		`echo "Cloned https://ex/\$(id) to \$(id)-path"`,
	} {
		if !strings.Contains(sc, want) {
			t.Errorf("clone 脚本缺转义形态 %q：\n%s", want, sc)
		}
	}
	if strings.Contains(sc, `"$(id)`) {
		t.Errorf("clone 脚本含未转义 $() 形态：\n%s", sc)
	}

	// commit：消息含引号逃逸尝试——必须整体转义且不保留裸引号
	payload2 := `x"; touch /tmp/sqp; echo "`
	rec = sqPost(t, s, s.handleOSGit, map[string]interface{}{
		"deviceID": "sq-git-2", "action": "commit", "message": payload2,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("commit 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-git-2")
	if !strings.Contains(sc, `git commit -m "x\"; touch /tmp/sqp; echo \""`) {
		t.Errorf("commit 消息未转义：\n%s", sc)
	}
	if strings.Contains(sc, `-m "x"; touch`) {
		t.Errorf("commit 脚本含未转义引号（可逃逸出 -m 段）：\n%s", sc)
	}

	// checkout / tag
	rec = sqPost(t, s, s.handleOSGit, map[string]interface{}{
		"deviceID": "sq-git-3", "action": "checkout", "branch": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("checkout 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-git-3")
	if !strings.Contains(sc, `git checkout "\$(id)" 2>&1 && echo "Checked out \$(id)"`) {
		t.Errorf("checkout 脚本未转义：\n%s", sc)
	}

	rec = sqPost(t, s, s.handleOSGit, map[string]interface{}{
		"deviceID": "sq-git-4", "action": "tag", "tag": "v1" + payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("tag 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-git-4")
	if !strings.Contains(sc, `git tag "v1\$(id)" 2>&1 && echo "Tag v1\$(id) created"`) {
		t.Errorf("tag 脚本未转义：\n%s", sc)
	}
}

// TestUnixMacOSDiskRaidInjectionQuoted 钉 Unix/macOS/磁盘/RAID 四段。
// 全部为 sh 语境（svcadm/brew/smartctl 等 Unix 工具），QuoteDouble 语义可证。
func TestUnixMacOSDiskRaidInjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`

	// unix pkg-install：三个包管理器路径 + 回显
	rec := sqPost(t, s, s.handleUnixOps, map[string]interface{}{
		"deviceID": "sq-unix-1", "action": "pkg-install", "package": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("pkg-install 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-unix-1")
	for _, want := range []string{
		`pkgadd -d "\$(id)"`, `swinstall "\$(id)"`, `pkgutil -i "\$(id)"`,
		`echo "Installed \$(id)"`,
	} {
		if !strings.Contains(sc, want) {
			t.Errorf("pkg-install 脚本缺 %q：\n%s", want, sc)
		}
	}

	// unix service-start：分号载荷——引号内必须是字面量，不得作为分隔符
	svc := `x; reboot`
	rec = sqPost(t, s, s.handleUnixOps, map[string]interface{}{
		"deviceID": "sq-unix-2", "action": "service-start", "service": svc,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("service-start 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-unix-2")
	for _, want := range []string{
		`svcadm enable "x; reboot"`,
		`/etc/init.d/"x; reboot" start`,
		`echo "Service x; reboot started"`,
	} {
		if !strings.Contains(sc, want) {
			t.Errorf("service-start 脚本缺 %q：\n%s", want, sc)
		}
	}
	if strings.Contains(sc, `enable x; reboot`) {
		t.Errorf("service-start 出现裸分号分隔（注入形态）：\n%s", sc)
	}

	// macOS brew-install
	rec = sqPost(t, s, s.handleMacOSExt, map[string]interface{}{
		"deviceID": "sq-mac-1", "action": "brew-install", "package": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("brew-install 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-mac-1")
	if !strings.Contains(sc, `brew install "\$(id)"`) || !strings.Contains(sc, `echo "Installed \$(id)"`) {
		t.Errorf("brew-install 脚本未转义：\n%s", sc)
	}

	// 磁盘 smart：token + 中文消息
	rec = sqPost(t, s, s.handleDiskHealth, map[string]interface{}{
		"deviceID": "sq-disk-1", "action": "smart", "disk": "/dev/sda" + payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("smart 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-disk-1")
	if !strings.Contains(sc, `smartctl -a "/dev/sda\$(id)"`) ||
		!strings.Contains(sc, `echo "smartctl 不可用或磁盘 /dev/sda\$(id) 不支持 SMART"`) {
		t.Errorf("smart 脚本未转义：\n%s", sc)
	}

	// RAID create：三处回显合并为一个完整转义字面量
	rec = sqPost(t, s, s.handleOSRAID, map[string]interface{}{
		"deviceID": "sq-raid-1", "action": "create",
		"name": "n" + payload, "disks": "d" + payload, "level": "1",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("raid create 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-raid-1")
	if !strings.Contains(sc, `echo "创建 RAID n\$(id): d\$(id) 上的磁盘 n\$(id)"`) {
		t.Errorf("raid create 脚本未转义：\n%s", sc)
	}
}

// TestWindowsRegistryCmdInjectionRejected：registry 脚本在 Windows agent 上由
// cmd /C 执行（见 internal/agent/agent.go 的 shellCommand），值嵌在 cmd 双引号段里——
// cmd 无引号内转义语法（`\"` 照样闭合引号，实测），只能力度=拒绝：
// `"` / `%` / `!` / CR/LF 五类。类型字段走白名单。
func TestWindowsRegistryCmdInjectionRejected(t *testing.T) {
	s := newShellQuoteServer()

	rejected := []map[string]interface{}{
		{"deviceID": "sq-reg-1", "action": "query", "path": `HKLM\Software" & echo MARKER & "`},
		{"deviceID": "sq-reg-1", "action": "query", "path": `HKLM\%PATH%\x`},
		{"deviceID": "sq-reg-1", "action": "query", "path": "HKLM\\x\r\necho MARKER"},
		{"deviceID": "sq-reg-2", "action": "set", "path": `HKLM\Software\K`, "name": `v"al & x`, "value": "1"},
		{"deviceID": "sq-reg-2", "action": "set", "path": `HKLM\Software\K`, "name": "v", "value": `a%PATH%b`},
		{"deviceID": "sq-reg-2", "action": "set", "path": `HKLM\Software\K`, "name": "v", "value": "1", "type": "REG_SZ; echo MARKER"},
		{"deviceID": "sq-reg-3", "action": "delete", "path": `HKLM\Software\K`, "name": `v"x`},
	}
	for i, body := range rejected {
		rec := sqPost(t, s, s.handleWindowsRegistry, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case#%d 应 400，实得 %d：%s", i, rec.Code, rec.Body.String())
		}
	}

	// 合法值放行且脚本形状不变（保留原 cmd 引号段）
	rec := sqPost(t, s, s.handleWindowsRegistry, map[string]interface{}{
		"deviceID": "sq-reg-ok", "action": "query", "path": `HKLM\SOFTWARE\My App`,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("合法路径应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-reg-ok")
	if !strings.Contains(sc, `reg query "HKLM\SOFTWARE\My App"`) {
		t.Errorf("合法路径脚本形状被改：\n%s", sc)
	}

	// set：合法含空格/反斜杠/中文的值
	rec = sqPost(t, s, s.handleWindowsRegistry, map[string]interface{}{
		"deviceID": "sq-reg-ok2", "action": "set",
		"path": `HKLM\SOFTWARE\My App`, "name": `Some Value`, "value": `C:\Program Files (x86)\工具`,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("合法 set 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-reg-ok2")
	if !strings.Contains(sc, `reg add "HKLM\SOFTWARE\My App" /v "Some Value" /t REG_SZ /d "C:\Program Files (x86)\工具" /f`) {
		t.Errorf("合法 set 脚本形状被改：\n%s", sc)
	}
}

// TestWindowsPackageMgrWhitelistAndQuoting：mgr 窄域走白名单（防 cmd 与 sh 双语境），
// pkg 走引号层（该脚本本身是 POSIX 写法：case/command -v）。
func TestWindowsPackageMgrWhitelistAndQuoting(t *testing.T) {
	s := newShellQuoteServer()

	for _, bad := range []string{`winget; echo MARKER`, `powershell`, `winget & x`, `../winget`} {
		rec := sqPost(t, s, s.handleWindowsPackage, map[string]interface{}{
			"deviceID": "sq-wp-1", "action": "install", "package": "p", "manager": bad,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("mgr=%q 应 400，实得 %d：%s", bad, rec.Code, rec.Body.String())
		}
	}

	rec := sqPost(t, s, s.handleWindowsPackage, map[string]interface{}{
		"deviceID": "sq-wp-2", "action": "install", "package": `$(id)`, "manager": "winget",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("合法 manager 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-wp-2")
	for _, want := range []string{
		`PKG_MGR="winget"`,
		`winget install --id "\$(id)" --silent`,
		`choco install "\$(id)" -y`,
		`scoop install "\$(id)"`,
	} {
		if !strings.Contains(sc, want) {
			t.Errorf("windows-package 脚本缺 %q：\n%s", want, sc)
		}
	}
}

// ---------------------------------------------------------------- 第四批：Windows/PS 家族（EncodedCommand）

// decodePSCommand 拆 `powershell -EncodedCommand <b64> [cmd 尾巴]` 并解回 PS 脚本。
func decodePSCommand(t *testing.T, script string) string {
	t.Helper()
	const prefix = "powershell -EncodedCommand "
	if !strings.HasPrefix(script, prefix) {
		t.Fatalf("脚本非编码形态：%s", script)
	}
	b64 := strings.Fields(strings.TrimPrefix(script, prefix))[0]
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 解码: %v", err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(u))
}

// assertPSWrapped 断言 wire 形态并返回解码后的 PS 脚本：
// ① 必须走 -EncodedCommand（cmd 层无引号内转义语法，旧式 -Command "..." 不可用）；
// ② 不得残留 2>/dev/null（cmd 下整行报废）、必须是 2>nul；
// ③ 原始载荷不得出现在 wire 形态（只允许在 base64 内）。
func assertPSWrapped(t *testing.T, script, rawPayload string) string {
	t.Helper()
	if !strings.HasPrefix(script, "powershell -EncodedCommand ") {
		t.Fatalf("未走编码传输：%s", script)
	}
	if strings.Contains(script, "2>/dev/null") {
		t.Fatalf("仍含 cmd 下会整行报废的 2>/dev/null：%s", script)
	}
	if !strings.Contains(script, "2>nul") {
		t.Fatalf("缺 2>nul：%s", script)
	}
	if strings.Contains(script, "powershell -Command ") {
		t.Fatalf("仍含旧式 -Command：%s", script)
	}
	if rawPayload != "" && strings.Contains(script, rawPayload) {
		t.Fatalf("wire 形态泄漏原始载荷（应只存在于 base64 内）：%s", script)
	}
	return decodePSCommand(t, script)
}

func TestWindowsFirewallUserPSEncoding(t *testing.T) {
	s := newShellQuoteServer()
	payload := `x'; Write-Output MARKER; '`
	pq := shellquote.QuotePowerShellSingle

	// firewall add：四个动态值全转义，模板逐字相等
	rec := sqPost(t, s, s.handleWindowsFirewall, map[string]interface{}{
		"deviceID": "sq-psfw-1", "action": "add", "port": "80", "protocol": "tcp", "name": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("firewall add 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-psfw-1")
	ps := assertPSWrapped(t, sc, payload)
	want := `New-NetFirewallRule -DisplayName ` + pq(payload) + ` -Direction 'in' -Protocol 'tcp' -LocalPort '80' -Action Allow -ErrorAction Stop; Write-Output 'Firewall rule added'`
	if ps != want {
		t.Errorf("firewall add 脚本不等：\n got=%s\nwant=%s", ps, want)
	}
	if !strings.HasSuffix(sc, " 2>nul || echo Failed") {
		t.Errorf("firewall add 尾巴错误：%s", sc)
	}

	// firewall delete
	rec = sqPost(t, s, s.handleWindowsFirewall, map[string]interface{}{
		"deviceID": "sq-psfw-2", "action": "delete", "name": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("firewall delete 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps = assertPSWrapped(t, sqDispatchScript(t, s, "sq-psfw-2"), payload)
	want = `Remove-NetFirewallRule -DisplayName ` + pq(payload) + ` -ErrorAction Stop; Write-Output 'Firewall rule deleted'`
	if ps != want {
		t.Errorf("firewall delete 脚本不等：\n got=%s\nwant=%s", ps, want)
	}

	// user create：用户名与密码（含单引号）都转义；消息在 PS 内
	rec = sqPost(t, s, s.handleWindowsUser, map[string]interface{}{
		"deviceID": "sq-psuser-1", "action": "create", "user": payload, "password": `p'a%ss!`,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("user create 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps = assertPSWrapped(t, sqDispatchScript(t, s, "sq-psuser-1"), payload)
	want = `New-LocalUser -Name ` + pq(payload) + ` -Password (ConvertTo-SecureString ` + pq(`p'a%ss!`) + ` -AsPlainText -Force) -ErrorAction Stop; Write-Output ` + pq("User "+payload+" created")
	if ps != want {
		t.Errorf("user create 脚本不等：\n got=%s\nwant=%s", ps, want)
	}

	// user delete
	rec = sqPost(t, s, s.handleWindowsUser, map[string]interface{}{
		"deviceID": "sq-psuser-2", "action": "delete", "user": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("user delete 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps = assertPSWrapped(t, sqDispatchScript(t, s, "sq-psuser-2"), payload)
	want = `Remove-LocalUser -Name ` + pq(payload) + ` -ErrorAction Stop; Write-Output ` + pq("User "+payload+" deleted")
	if ps != want {
		t.Errorf("user delete 脚本不等：\n got=%s\nwant=%s", ps, want)
	}

	// user add-to-group
	rec = sqPost(t, s, s.handleWindowsUser, map[string]interface{}{
		"deviceID": "sq-psuser-3", "action": "add-to-group", "user": payload, "group": `grp"x`,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("add-to-group 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps = assertPSWrapped(t, sqDispatchScript(t, s, "sq-psuser-3"), payload)
	want = `Add-LocalGroupMember -Group ` + pq(`grp"x`) + ` -Member ` + pq(payload) + ` -ErrorAction Stop; Write-Output ` + pq("User "+payload+` added to group grp"x`)
	if ps != want {
		t.Errorf("add-to-group 脚本不等：\n got=%s\nwant=%s", ps, want)
	}
}

func TestWindowsEventlogPSEncodingAndLevelWhitelist(t *testing.T) {
	s := newShellQuoteServer()
	pq := shellquote.QuotePowerShellSingle
	logName := `Sq'Log`

	// query：logName 转义；level 默认 Error,Warning 为裸词；count 默认 50
	rec := sqPost(t, s, s.handleWindowsEventLog, map[string]interface{}{
		"deviceID": "sq-pslog-1", "action": "query", "logName": logName,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("eventlog query 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps := assertPSWrapped(t, sqDispatchScript(t, s, "sq-pslog-1"), logName)
	want := `Get-EventLog -LogName ` + pq(logName) + ` -EntryType Error,Warning -Newest 50 | Select-Object TimeGenerated, EntryType, Source, Message | Format-Table -AutoSize`
	if ps != want {
		t.Errorf("eventlog query 脚本不等：\n got=%s\nwant=%s", ps, want)
	}

	// level 注入载荷必须 400（裸词位只允许字母与逗号）
	for _, bad := range []string{`Error; Write-Output MARKER`, `Error|Write-Output x`, `$x`, "Error\nWarning", `Err'or`} {
		rec = sqPost(t, s, s.handleWindowsEventLog, map[string]interface{}{
			"deviceID": "sq-pslog-2", "action": "query", "level": bad,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("level=%q 应 400，实得 %d：%s", bad, rec.Code, rec.Body.String())
		}
	}
	// 合法级别放行
	rec = sqPost(t, s, s.handleWindowsEventLog, map[string]interface{}{
		"deviceID": "sq-pslog-3", "action": "query", "level": "Error,Warning,Critical",
	})
	if rec.Code != http.StatusAccepted {
		t.Errorf("合法 level 被误拒：%d", rec.Code)
	}

	// clear
	rec = sqPost(t, s, s.handleWindowsEventLog, map[string]interface{}{
		"deviceID": "sq-pslog-4", "action": "clear", "logName": logName,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("eventlog clear 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps = assertPSWrapped(t, sqDispatchScript(t, s, "sq-pslog-4"), logName)
	want = `Clear-EventLog -LogName ` + pq(logName) + ` -ErrorAction Stop; Write-Output ` + pq("Event log "+logName+" cleared")
	if ps != want {
		t.Errorf("eventlog clear 脚本不等：\n got=%s\nwant=%s", ps, want)
	}

	// export：路径在 PS 侧拼接并整体转义
	rec = sqPost(t, s, s.handleWindowsEventLog, map[string]interface{}{
		"deviceID": "sq-pslog-5", "action": "export", "logName": logName,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("eventlog export 应受理：%d %s", rec.Code, rec.Body.String())
	}
	ps = assertPSWrapped(t, sqDispatchScript(t, s, "sq-pslog-5"), logName)
	exportPath := `C:\temp\` + logName + `.csv`
	want = `Get-EventLog -LogName ` + pq(logName) + ` -ErrorAction Stop | Export-Csv -Path ` + pq(exportPath) + ` -NoTypeInformation -ErrorAction Stop; Write-Output ` + pq("Exported to "+exportPath)
	if ps != want {
		t.Errorf("eventlog export 脚本不等：\n got=%s\nwant=%s", ps, want)
	}
}

func TestWindowsServicePSEncoding(t *testing.T) {
	s := newShellQuoteServer()
	payload := `svc'; Write-Output MARKER; '`
	pq := shellquote.QuotePowerShellSingle

	cases := []struct {
		action string
		body   map[string]interface{}
		want   string
	}{
		{"start", nil, `Start-Service -Name ` + pq(payload) + ` -ErrorAction Stop; Write-Output ` + pq("Service "+payload+" started")},
		{"stop", nil, `Stop-Service -Name ` + pq(payload) + ` -Force -ErrorAction Stop; Write-Output ` + pq("Service "+payload+" stopped")},
		{"restart", nil, `Restart-Service -Name ` + pq(payload) + ` -Force -ErrorAction Stop; Write-Output ` + pq("Service "+payload+" restarted")},
		{"create", nil, `New-Service -Name ` + pq(payload) + ` -BinaryPathName ` + pq(payload) + ` -ErrorAction Stop; Write-Output ` + pq("Service "+payload+" created")},
		{"delete", nil, `sc.exe delete ` + pq(payload) + `; if ($LASTEXITCODE -ne 0) { exit 1 }; Write-Output ` + pq("Service "+payload+" deleted")},
		{"query", nil, `Get-Service -Name ` + pq(payload) + ` | Select-Object Name, Status, StartType, DisplayName`},
	}
	for i, c := range cases {
		body := map[string]interface{}{
			"deviceID": "sq-pssvc-" + string(rune('a'+i)), "action": c.action, "service": payload,
		}
		rec := sqPost(t, s, s.handleWindowsService, body)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s 应受理：%d %s", c.action, rec.Code, rec.Body.String())
			continue
		}
		sc := sqDispatchScript(t, s, "sq-pssvc-"+string(rune('a'+i)))
		ps := assertPSWrapped(t, sc, payload)
		if ps != c.want {
			t.Errorf("%s 脚本不等：\n got=%s\nwant=%s", c.action, ps, c.want)
		}
		// 原实现 start/stop/restart/create/delete 无 || Failed 尾巴，query 无尾巴——此处只允许 2>nul
		if strings.Contains(sc, "echo Failed") {
			t.Errorf("%s 不应有 Failed 尾巴：%s", c.action, sc)
		}
	}
}

// ---------------------------------------------------------------- 第五批：os_ext_ops.go sh 清扫

// TestOSProcessLogInjectionQuoted 钉 handleOSProcess/handleOSLog：
// list 的 grep filter、kill 的信号名、journalctl 的 unit/since。
func TestOSProcessLogInjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`

	// process list
	rec := sqPost(t, s, s.handleOSProcess, map[string]interface{}{
		"deviceID": "sq-os-1", "action": "list", "filter": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("process list 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-os-1")
	if !strings.Contains(sc, `grep -i "\$(id)"`) {
		t.Errorf("filter 未转义：\n%s", sc)
	}
	if strings.Contains(sc, `grep -i $(id)`) {
		t.Errorf("filter 含未转义形态：\n%s", sc)
	}

	// kill：恶意信号名 400；合法信号名放行
	for _, bad := range []string{`TERM; reboot`, `$(id)`, `SIG-9`, `TERM|x`} {
		rec = sqPost(t, s, s.handleOSProcess, map[string]interface{}{
			"deviceID": "sq-os-2", "action": "kill", "pid": 1, "signal": bad,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("signal=%q 应 400，实得 %d", bad, rec.Code)
		}
	}
	rec = sqPost(t, s, s.handleOSProcess, map[string]interface{}{
		"deviceID": "sq-os-3", "action": "kill", "pid": 1, "signal": "TERM",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("合法 signal 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-os-3")
	if !strings.Contains(sc, `kill -TERM 1`) {
		t.Errorf("kill 脚本形态被改：\n%s", sc)
	}

	// journal-query：unit 与 since 都转义
	rec = sqPost(t, s, s.handleOSLog, map[string]interface{}{
		"deviceID": "sq-os-4", "action": "journal-query", "unit": payload, "since": `y"$(id)`,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("journal-query 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-os-4")
	if !strings.Contains(sc, `-u "\$(id)"`) {
		t.Errorf("unit 未转义：\n%s", sc)
	}
	if !strings.Contains(sc, `--since "y\"\$(id)"`) {
		t.Errorf("since 未转义（含引号载荷）：\n%s", sc)
	}
}

// TestMacOSLaunchctlInjectionQuoted 钉 handleMacOSService 的 5 个动作。
func TestMacOSLaunchctlInjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`

	rec := sqPost(t, s, s.handleMacOSService, map[string]interface{}{
		"deviceID": "sq-mac-svc-1", "action": "load", "plist": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("load 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-mac-svc-1")
	if !strings.Contains(sc, `launchctl load "\$(id)" 2>&1 && echo "Loaded \$(id)"`) {
		t.Errorf("load 未转义：\n%s", sc)
	}

	svc := `com.x; reboot`
	rec = sqPost(t, s, s.handleMacOSService, map[string]interface{}{
		"deviceID": "sq-mac-svc-2", "action": "status", "service": svc,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-mac-svc-2")
	if !strings.Contains(sc, `launchctl list "com.x; reboot" 2>/dev/null || echo "Service com.x; reboot not found"`) {
		t.Errorf("status 未转义：\n%s", sc)
	}
	if strings.Contains(sc, `list com.x; reboot`) {
		t.Errorf("status 含裸分号分隔：\n%s", sc)
	}

	// stop：另一个动作抽验
	rec = sqPost(t, s, s.handleMacOSService, map[string]interface{}{
		"deviceID": "sq-mac-svc-3", "action": "stop", "service": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("stop 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-mac-svc-3")
	if !strings.Contains(sc, `launchctl stop "\$(id)" 2>&1 && echo "Stopped \$(id)"`) {
		t.Errorf("stop 未转义：\n%s", sc)
	}
}

// TestAgentDistributeHarmonyInjectionQuoted 钉 handleAgentDistribute 消息与 hdc 三动作。
func TestAgentDistributeHarmonyInjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	payload := `$(id)`

	rec := sqPost(t, s, s.handleAgentDistribute, map[string]interface{}{
		"deviceID": "sq-ad-1", "action": "download", "version": payload, "os": "linux", "arch": "amd64",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("download 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc := sqDispatchScript(t, s, "sq-ad-1")
	if !strings.Contains(sc, `echo "Downloading agent \$(id) for linux/amd64"`) {
		t.Errorf("download 消息未转义：\n%s", sc)
	}

	rec = sqPost(t, s, s.handleHarmonyOSExt, map[string]interface{}{
		"deviceID": "sq-hm-1", "action": "install-app", "appPath": payload,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("hdc install 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-hm-1")
	if !strings.Contains(sc, `hdc install "\$(id)" 2>&1 && echo "Installed \$(id)"`) {
		t.Errorf("hdc install 未转义：\n%s", sc)
	}

	rec = sqPost(t, s, s.handleHarmonyOSExt, map[string]interface{}{
		"deviceID": "sq-hm-2", "action": "transfer-file", "localPath": payload, "remotePath": `r"; x`,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("hdc transfer 应受理：%d %s", rec.Code, rec.Body.String())
	}
	sc = sqDispatchScript(t, s, "sq-hm-2")
	if !strings.Contains(sc, `hdc file send "\$(id)" "r\"; x" 2>&1 && echo "Transferred \$(id) -> r\"; x"`) {
		t.Errorf("hdc transfer 未转义：\n%s", sc)
	}
}
