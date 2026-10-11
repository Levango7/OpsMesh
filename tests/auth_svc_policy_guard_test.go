package tests

// auth_svc_policy_guard_test.go — auth-svc 侧口令夹具与策略常量的跨模块守卫。
//
// 背景：peer 在 8870d69 把控制面口令策略收紧到「≥12 位 + 大小写 + 数字 + 特殊字符」
// （为与 auth-svc 同严），并建了 tests/e2e_fixture_policy_guard_test.go 守 e2e 夹具与
// 前端规则。peer 在第五十二则明确留下一句：「你们 auth-svc 侧若有同类按旧口径写死的
// 口令（E2E/冒烟/建号夹具），值得照此自查一遍」——本文件即该自查的常驻化。
//
// # 为什么只能读源文件而不能 import
//
// services/auth-svc 是**独立 Go module**（有自己的 go.mod），tests/ 属根模块，
// 无法 import 其包；且其 PasswordPolicy 字段是包内私有。故此处按源文件解析其
// 默认策略常量，与 tests/ 侧自己的口令字面量对拍。
//
// 这与 peer 那道守卫同样是「静态对拍」而非符号引用，属**已知的脆契约**：若 auth-svc
// 侧重构 defaultPasswordPolicy 的写法（如 MinLen 改成常量引用），本守卫会取不到值
// 而判红（fail-closed，并提示人工复核），不会静默放过漂移。

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
)

// authSvcPolicyFile auth-svc 的强口令策略定义文件。
const authSvcPolicyFile = "services/auth-svc/internal/http/password.go"

// authSvcMinLenRE 抓 defaultPasswordPolicy 里的 MinLen 赋值。
var authSvcMinLenRE = regexp.MustCompile(`MinLen:\s*(\d+)`)

// authSvcRequireSpecialRE 抓 RequireSpecial 赋值。
var authSvcRequireSpecialRE = regexp.MustCompile(`RequireSpecial:\s*(true|false)`)

// TestAuthSvcPolicyMatchesControlPlane auth-svc 策略不得弱于控制面。
//
// 切流放行用户的前提是「两侧同严」：任一侧放松都会让本该拒的弱口令通过。
// 这里读 auth-svc 的 defaultPasswordPolicy 与控制面 credentials 对拍。
func TestAuthSvcPolicyMatchesControlPlane(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, authSvcPolicyFile))
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", authSvcPolicyFile, err)
	}
	code := stripComments(string(b))

	svcMin := authSvcMinLenRE.FindStringSubmatch(code)
	if svcMin == nil {
		t.Fatalf("%s 未解析到 MinLen——判据失明（neutered check 比没有更坏）；"+
			"若 defaultPasswordPolicy 写法有变请同步更新本守卫的正则", authSvcPolicyFile)
	}
	svcMinLen, _ := strconv.Atoi(svcMin[1])
	if svcMinLen < credentials.ChangePasswordMinLen() {
		t.Errorf("auth-svc 口令最短长度 %d 弱于控制面 %d——切流会放行本该拒的弱口令",
			svcMinLen, credentials.ChangePasswordMinLen())
	}

	svcSpecial := authSvcRequireSpecialRE.FindStringSubmatch(code)
	if svcSpecial == nil {
		t.Fatalf("%s 未解析到 RequireSpecial——判据失明；请同步本守卫正则", authSvcPolicyFile)
	}
	if svcSpecial[1] != "true" {
		t.Errorf("auth-svc RequireSpecial=%s，控制面已要求特殊字符——两侧不同严",
			svcSpecial[1])
	}
}

// authSvcFixturePasswordRE 抓 auth-svc 测试与冒烟脚本里的口令字面量。
// 形态覆盖：Go 的 `"..."`、shell 的 `PASS='...'` / `PASS="..."`。
// 只抓长度 >=8 的，短串（如 "ok"）不可能是策略对象，抓了只会刷噪声。
var authSvcFixturePasswordRE = regexp.MustCompile(`(?:")([A-Za-z0-9@#$%^&*_+\-!.]{8,})(?:")|(?:PASS|PASSWORD)=['"]([A-Za-z0-9@#$%^&*_+\-!.]{8,})['"]`)

// authSvcFixtureSkipSubstr 这些串不是口令策略对象，跳过免噪声。
var authSvcFixtureSkipSubstr = []string{
	"SELECT ", "INSERT ", "UPDATE ", "DELETE ",
	"/api/v1/", "/opsmesh.auth.v1/",
	"BEGIN CERTIFICATE", "PRIVATE KEY",
	"tls:", "http://", "https://",
	"CERTIFICATE-----",
}

// blacklistMarker 标记 auth-svc 源文件里的「弱口令黑名单」区段起点。
// 该区段内的串是**要被策略拒掉的样本**（P@ssw0rd 之流），不是夹具口令，
// 对它们跑强口令校验纯属噪音——首版守卫就是这么刷出 13 条误报的。
const blacklistMarker = "defaultCommonPasswords"

// stripBlacklistSection 去掉黑名单声明区段，只保留其余代码做口令扫描。
func stripBlacklistSection(code string) string {
	idx := strings.Index(code, blacklistMarker)
	if idx < 0 {
		return code
	}
	// 黑名单是 var 声明块，截到下一个顶层 func/var 之前即可：
	// 简化处理——直接从标记处截到文件末或下一个 "func " 处。
	rest := code[idx:]
	if f := strings.Index(rest, "\nfunc "); f >= 0 {
		return code[:idx] + rest[f:]
	}
	return code[:idx]
}

// weakSampleMarkers 这些标识符附近的串是**要被策略拒掉的弱口令样本**，
// 不是「期望被接受的夹具口令」。对它们跑强口令校验是噪音——策略测试
// 本来就该用不达标的口令当输入（否则测不出「能不能拒」）。
//
// 首版守卫没排除这类，刷出 5 条误报（admin123/short1!A/Abcdefgh123x/
// Welcome1!23 之流），已按此修正。
var weakSampleMarkers = []string{
	"weak", "noSpecial", "common", "blacklist", "Blacklist",
	"invalid", "reject", "拒绝", "弱口令",
}

// lineHasWeakSampleMarker 判断该口令所在行是否带弱口令样本标识。
func lineHasWeakSampleMarker(line string) bool {
	for _, mk := range weakSampleMarkers {
		if strings.Contains(line, mk) {
			return true
		}
	}
	return false
}

func isAuthSvcFixturePassword(s string) bool {
	for _, sub := range authSvcFixtureSkipSubstr {
		if strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestAuthSvcFixturePasswordsSatisfyPolicy auth-svc 侧口令夹具必须过得严策略。
//
// 覆盖三类（peer 第五十二则点名的自查范围）：
//   - services/auth-svc 下 .go 测试里的口令字面量
//   - deploy/scripts/smoke-cutover.sh（切流真进程冒烟）
//   - deploy/scripts/smoke-dual.sh（auth 双轨冒烟）
//
// 注意：服务层测试（internal/service）不经 HTTP 策略校验，所以弱口令在那里
// 不会红——但它仍是「按旧口径写死的口令」，一旦被搬进 HTTP 用例就会炸。
// 这道守卫把爆炸点前移到本地 go test。
func TestAuthSvcFixturePasswordsSatisfyPolicy(t *testing.T) {
	root := repoRoot(t)
	rels := []string{
		"services/auth-svc",
		"deploy/scripts/smoke-cutover.sh",
		"deploy/scripts/smoke-dual.sh",
	}
	checked := 0
	bad := 0
	for _, rel := range rels {
		base := filepath.Join(root, rel)
		info, err := os.Stat(base)
		if err != nil {
			continue // 文件/目录不存在（如某冒烟脚本未入库）则跳过
		}
		var files []string
		if info.IsDir() {
			_ = filepath.Walk(base, func(p string, fi os.FileInfo, err error) error {
				if err != nil || fi.IsDir() {
					return nil
				}
				if strings.HasSuffix(p, ".go") {
					files = append(files, p)
				}
				return nil
			})
		} else {
			files = append(files, base)
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			showRel, _ := filepath.Rel(root, f)
			// 剥掉黑名单区段：那里的串是「要被拒的弱口令样本」，不是夹具。
			code := stripBlacklistSection(stripComments(string(b)))
			// 行级画像：用于排除弱口令样本（策略测试的合法输入）。
			lines := strings.Split(code, "\n")
			for _, m := range authSvcFixturePasswordRE.FindAllStringSubmatch(code, -1) {
				pw := m[1]
				if pw == "" {
					pw = m[2]
				}
				if pw == "" || !isAuthSvcFixturePassword(pw) {
					continue
				}
				// 只对「形似口令」的串做校验：含大小写+数字+字母混合且无 SQL/路径痕迹。
				if !looksLikeFixturePassword(pw) {
					continue
				}
				// 所在行若带弱口令样本标识，属策略测试的合法输入，跳过。
				if lineHasWeakSampleMarker(lineOf(lines, pw)) {
					continue
				}
				checked++
				if msg := weakPasswordReason(pw); msg != "" {
					bad++
					t.Errorf("%s 的口令字面量 %q 不达策略（%s）——换个满足「≥12 位 + 大小写 + "+
						"数字 + 特殊字符」的值，免得它日被搬进 HTTP 用例时才炸",
						showRel, pw, msg)
				}
			}
		}
	}
	if checked == 0 {
		t.Errorf("auth-svc 侧一个口令字面量都没扫到——扫描面塌缩，本守卫在空转；" +
			"若 fixtures 结构有变请更新 authSvcFixturePasswordRE")
	}
}

// lineOf 返回包含指定口令的首行（用于行级画像）。找不到时回空串。
func lineOf(lines []string, pw string) string {
	for _, l := range lines {
		if strings.Contains(l, pw) {
			return l
		}
	}
	return ""
}

// looksLikeFixturePassword 判断是否「形似用于登录/改密的口令」而非其他串。
// 条件：>=8 位、同时含大写/小写/数字、且不是纯符号串。
func looksLikeFixturePassword(s string) bool {
	if len(s) < 8 {
		return false
	}
	var up, lo, di bool
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			up = true
		case c >= 'a' && c <= 'z':
			lo = true
		case c >= '0' && c <= '9':
			di = true
		}
	}
	return up && lo && di
}

// weakPasswordReason 返回空串表示合格，否则返回不达标的原因。
// 与 auth-svc 的 defaultPasswordPolicy 同口径：>=12 位 + 三类字符 + 特殊字符。
func weakPasswordReason(pw string) string {
	var up, lo, di, sp bool
	for _, c := range pw {
		switch {
		case c >= 'A' && c <= 'Z':
			up = true
		case c >= 'a' && c <= 'z':
			lo = true
		case c >= '0' && c <= '9':
			di = true
		default:
			sp = true
		}
	}
	if len(pw) < 12 {
		return "不足 12 位"
	}
	if !up {
		return "缺大写"
	}
	if !lo {
		return "缺小写"
	}
	if !di {
		return "缺数字"
	}
	if !sp {
		return "缺特殊字符"
	}
	return ""
}
