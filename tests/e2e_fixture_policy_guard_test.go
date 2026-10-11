package tests

// e2e_fixture_policy_guard_test.go — 跨语言静态守卫：E2E 口令夹具 + 前端口令校验规则
// 必须与**后端真策略**（internal/controlplane/credentials.ValidateStrongPassword）一致。
//
// 背景（2026-10-12 实测，run 38157979508 的 e2e-real job）：口令策略收紧到
// 「≥12 位 + 大小写 + 数字 + 特殊字符」（8870d69，为与 auth-svc 同严）之后，
// E2E 夹具仍是 `E2eRealPass2026`（16 位、三类字符、**无特殊字符**）⇒
// 首登改密被后端 400 拒掉 ⇒ 5 个用例全红（其中一例还被 loginGuard 429 连带）。
// 同一改动还留下前端校验比后端弱（8 位、不查特殊字符）的分叉——用户按前端规则
// 填的口径提交后仍会被后端拒。两类缺口此前都无任何本地守卫能发现：
// Go 单测跑不到 web/ 的 JS，前端单测也不读后端常量。
//
// 因此本守卫直接读三处真实来源做交叉核对，任一处漂移即判红：
//   ① 后端策略本体（import credentials 包，不复制字面量——复制就会各改各的）
//   ② web/enterprise/e2e-real/*.spec.js 的夹具口令默认值
//   ③ ChangePasswordView.vue / PasswordStrength.vue 的前端规则 + i18n 文案
//
// 判据同样只查非注释行：注释里写「至少 12 位」不能替代码里的 `length < 12`。

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
)

// e2eSpecDir E2E 真实后端用例目录（与 ci.yml 的 e2e-real job 同源）。
const e2eSpecDir = "web/enterprise/e2e-real"

// fixturePasswordRE 抓 spec 里的口令默认值：`process.env.X || 'literal'` 与
// `const X = process.env.X || 'literal'` 两种形态（security.spec.js 用前者，
// core/agent_lifecycle 用后者）。只抓右边那个 literal —— 环境变量可覆盖，
// 但 CI 不覆盖，默认值就是实际生效值。
var fixturePasswordRE = regexp.MustCompile(`process\.env\.E2E_NEW_PASS\s*\|\|\s*'([^']+)'`)

// TestE2EFixturePasswordsSatisfyBackendPolicy E2E 夹具口令必须过得过后端策略。
//
// 为什么值得一道守卫：夹具口令是"测试替真实用户走的一条路"，策略一收紧它就静默失效，
// 而失效点在 CI 的 e2e-real job（本地 go test 完全看不到），红的时候离改动已经隔了几笔。
// 这里按后端策略本体逐条校验，不另写一套阈值。
func TestE2EFixturePasswordsSatisfyBackendPolicy(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, e2eSpecDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", e2eSpecDir, err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".spec.js") {
			continue
		}
		rel := filepath.Join(e2eSpecDir, e.Name())
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", rel, err)
		}
		code := stripComments(string(b))
		for _, m := range fixturePasswordRE.FindAllStringSubmatch(code, -1) {
			checked++
			if msg := credentials.ValidateStrongPassword(m[1]); msg != "" {
				t.Errorf("%s 的夹具口令 %q 不满足后端口令策略（%s）——"+
					"首登改密会被 400 拒掉，e2e-real 整片红；请换成满足「≥12 位 + 大小写 + 数字 + 特殊字符」的口令",
					rel, m[1], msg)
			}
		}
	}
	if checked < 4 {
		t.Errorf("只从 e2e-real 抓到 %d 处夹具口令默认值——预期 >=4（三个 spec 共 4 处）；"+
			"扫描面塌缩会让本守卫空转", checked)
	}
}

// frontendMinLenRE 抓前端长度阈值：`.length < N` 与 `.length >= N` 两种形态
// （ChangePasswordView 用「小于即拒」`< 12`，PasswordStrength 用「达标即勾」`>= 12`）。
// 两种写法表达同一个"要求至少 N 位"，都要抓。
var frontendMinLenRE = regexp.MustCompile(`\.length\s*(<|>=)\s*(\d+)`)

// frontendRequiredMinLen 从源码里取出"要求至少多少位"。
// `< N` 与 `>= N` 语义等价；取最小是因为同文件出现多个阈值时，
// 最宽松那个才是用户实际能过的口径。
func frontendRequiredMinLen(code string) int {
	minLen := -1
	for _, m := range frontendMinLenRE.FindAllStringSubmatch(code, -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if minLen == -1 || n < minLen {
			minLen = n
		}
	}
	return minLen
}

// TestFrontendPasswordRulesMatchBackend 前端校验规则不得弱于后端。
//
// 两类弱都会被判红：① 长度阈值小于后端；② 没有特殊字符判定。
// 只查非注释行——注释里写「至少 12 位」不替代码里的阈值。
func TestFrontendPasswordRulesMatchBackend(t *testing.T) {
	root := repoRoot(t)

	type target struct {
		rel       string
		wantRules bool // 是否需要看到特殊字符判定
	}
	targets := []target{
		{"web/enterprise/src/views/ChangePasswordView.vue", true},
		{"web/enterprise/src/components/PasswordStrength.vue", true},
	}
	for _, tg := range targets {
		b, err := os.ReadFile(filepath.Join(root, tg.rel))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", tg.rel, err)
		}
		code := stripComments(string(b))

		minLen := frontendRequiredMinLen(code)
		if minLen == -1 {
			t.Errorf("%s 未找到 `.length < N` 或 `.length >= N` 形式的口令长度阈值——判据失明（neutered check 比没有更坏）", tg.rel)
		} else if minLen < credentials.ChangePasswordMinLen() {
			t.Errorf("%s 的口令长度阈值为 %d，弱于后端策略的 %d——用户按前端口径填的口令会被后端拒；"+
				"两侧必须同严（与后端 credentials.changePasswordMinLen 对齐）",
				tg.rel, minLen, credentials.ChangePasswordMinLen())
		}

		if tg.wantRules && !strings.Contains(code, "/[^A-Za-z0-9]/") {
			t.Errorf("%s 没有特殊字符判定（缺 /[^A-Za-z0-9]/ 之类的负字符类）——"+
				"后端要求特殊字符，前端不查就会让用户提交必然被拒的口令", tg.rel)
		}
	}
}

// passwordI18nMinLenRE 抓 i18n 文案里的位数（"至少 12 位" / "at least 12 characters"）。
var passwordI18nMinLenRE = regexp.MustCompile(`(?:至少|at least)\s*(\d+)`)

// TestPasswordI18nTextMatchesPolicy i18n 文案里写的位数必须与后端一致。
//
// 为什么不重要还要查：文案是用户看到的口径，写「至少 8 位」而服务端要 12 位，
// 等于前端替后端撒谎。这是上一轮收紧策略时漏改的一处（四份语料同步改）。
func TestPasswordI18nTextMatchesPolicy(t *testing.T) {
	root := repoRoot(t)
	rels := []string{
		"web/enterprise/src/i18n/zh.json",
		"web/enterprise/src/i18n/en.json",
		"web/enterprise/src/i18n/locales/zh/common.json",
		"web/enterprise/src/i18n/locales/en/common.json",
	}
	// 每条语料的 change_password 段都必须给出特殊字符提示与 12 位口径。
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", rel, err)
		}
		src := string(b)
		if !strings.Contains(src, `"need_special"`) {
			t.Errorf("%s 缺 change_password.need_special 键——前端特殊字符提示会渲染成 key 名", rel)
		}
		if !strings.Contains(src, `"password_strength"`) || !strings.Contains(src, `"special"`) {
			t.Errorf("%s 的 password_strength 段缺 special 键——强度条需求列表少一行且显示 key 名", rel)
		}
		for _, m := range passwordI18nMinLenRE.FindAllStringSubmatch(src, -1) {
			n, _ := strconv.Atoi(m[1])
			if n == 8 {
				t.Errorf("%s 仍写着「8 位」口径（第 %d 处）——与后端策略（%d）不一致",
					rel, strings.Index(src, m[0])/80+1, credentials.ChangePasswordMinLen())
			}
		}
	}
}
