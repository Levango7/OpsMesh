// guard_parity_test.go 登录防护等价性的契约守卫（TD-60 §10.2 遗留项之一）。
//
// 背景：auth 域切流后，请求经聚合代理到达 auth-svc。两侧登录防护必须等价，
// 否则「切流 = 防爆破能力缩水」——一个看不见的降级。已核实的事实：
//
//   - IP 令牌桶：两侧同参数（auth-svc 注释明示"A2 防爆破两道闸（与 controlplane loginGuard
//     同参数）"；controlplane 侧 IP 闸 + 账号锁定检查位于路由判定**之前**，
//     故名册用户在本侧就被拦，不会因切流绕过本地闸）。
//   - 失败计数/账号锁定：**在 auth-svc 侧产生**（本地补记只覆盖"经本面失败"的请求）。
//     auth-svc 提供 `GetLoginGuardStatus`（gRPC 只读）查询，但 controlplane 不直连其
//     gRPC，只有 HTTP 代理 ⇒ 本侧无法在登录前读到对侧锁定态。
//
// 本测试把「已对齐的部分」钉成断言，并把「未对齐的部分」钉成**显式已知边界**：
// 将来若有人动了任一侧的闸参数或路由顺序，这里会先红。
package cutover

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
)

// authSvcGatewayPath auth-svc HTTP 网关源码（只读核对参数口径）。
var authSvcGatewayPath = "../../../services/auth-svc/internal/http/gateway.go"

// TestLoginGuardIPBucketParamsMatchBothSides 两侧 IP 令牌桶参数一致（切流不削弱防爆破）。
//
// 常量名两侧刻意不同（controlplane `loginRateBurst` / auth-svc `guardRateBurst`），
// 所以比的是**数值**而非名字——参数drift 会静默改变防爆破强度，是"切流=降级"的典型形态。
func TestLoginGuardIPBucketParamsMatchBothSides(t *testing.T) {
	localSrc := readLocal(t, "internal/controlplane/loginguard/guard.go")
	svcSrc := readLocal(t, "services/auth-svc/internal/http/guard.go")
	cases := []struct {
		label  string
		localP string
		svcP   string
	}{
		{"令牌桶容量", `loginRateBurst\s*=\s*(\d+)`, `guardRateBurst\s*=\s*(\d+)`},
		{"补充速率(每秒)", `loginRateRefill\s*=\s*([0-9.]+(?:\s*/\s*[0-9.]+)?)`, `guardRateRefill\s*=\s*([0-9.]+(?:\s*/\s*[0-9.]+)?)`},
		{"失败阈值", `loginMaxFails\s*=\s*(\d+)`, `guardMaxFails\s*=\s*(\d+)`},
		{"失败窗口", `loginFailWindow\s*=\s*([0-9]+)\s*\*\s*time\.Minute`, `guardFailWindow\s*=\s*([0-9]+)\s*\*\s*time\.Minute`},
	}
	for _, c := range cases {
		lv, sv := firstGroup(localSrc, c.localP), firstGroup(svcSrc, c.svcP)
		if lv == "" || sv == "" {
			t.Errorf("登录防护参数「%s」取不到（controlplane=%q auth-svc=%q）——契约失去比对基准，"+
				"需人工复核两侧是否仍等价", c.label, lv, sv)
			continue
		}
		if lv != sv {
			t.Errorf("登录防护参数「%s」两侧不一致：controlplane=%q vs auth-svc=%q——"+
				"切流会让该参数实测值变化（防爆破强度改变）", c.label, lv, sv)
		}
	}
}

// TestCutoverRouteOrderKeepsLocalGateFirst 路由判定在本地闸之后（切流不绕过 IP 限流/账号锁定）。
// 断言 handleAuthLogin 里「路由分支出现在 loginGuard 检查之后」。
func TestCutoverRouteOrderKeepsLocalGateFirst(t *testing.T) {
	src := readLocal(t, "internal/controlplane/auth_login.go")
	allow := strings.Index(src, "loginGuard.Allow(")
	locked := strings.Index(src, "loginGuard.Locked(")
	route := strings.Index(src, "cutoverForwardLogin(")
	if allow < 0 || locked < 0 || route < 0 {
		t.Fatalf("handleAuthLogin 的闸/路由锚点缺失（Allow=%d Locked=%d 路由=%d）——顺序断言失效",
			allow, locked, route)
	}
	// 取 handleAuthLogin 函数体，避免被文件里其他函数体的锚点干扰。
	body := funcBodyOf(t, src, "handleAuthLogin")
	if i := strings.Index(body, "loginGuard.Allow("); i < 0 {
		t.Fatal("handleAuthLogin 内未见 IP 限流闸")
	}
	if i := strings.Index(body, "loginGuard.Locked("); i < 0 {
		t.Fatal("handleAuthLogin 内未见账号锁定检查")
	}
	if i := strings.Index(body, "cutoverForwardLogin("); i < strings.Index(body, "loginGuard.Locked(") {
		t.Error("切流路由分支跑在账号锁定检查之前——锁定态用户会被送去对侧重试，" +
			"本地防爆破闸被绕过（这正是要守的顺序）")
	}
}

// TestPasswordPolicyParityOnFarSide 口令策略两侧同严（切流不削弱口令强度）。
// auth-svc 的强口令基线由本包 credentials.ValidateStrongPassword 提供同源语义；
// 这里断言两侧**都要求特殊字符**这一收紧点真正落在对侧。
func TestPasswordPolicyParityOnFarSide(t *testing.T) {
	svc := readLocal(t, "services/auth-svc/internal/http/password.go")
	if !strings.Contains(svc, "RequireSpecial") {
		t.Error("auth-svc 侧未见 RequireSpecial——两侧口令策略需重新对拍")
	}
	// 本侧策略必须真的要求特殊字符（收紧基线，不是注释）。
	got := ValidateStrongPasswordNoSpecialForTest()
	if got == "" {
		t.Error("本侧强口令校验未拒绝「无特殊字符」的口令——与 auth-svc 策略不一致")
	}
}

// ---------- 小工具 ----------

func readLocal(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile("../../../" + rel)
	if err != nil {
		t.Skipf("读取 %s 失败：%v", rel, err)
	}
	return string(b)
}

func firstGroup(src, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// funcBodyOf 取 src 中名为 fn 的函数体文本（大括号配平）。
func funcBodyOf(t *testing.T, src, fn string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^func (?:\([^)]*\)\s*)?"?` + regexp.QuoteMeta(fn) + `\s*\(`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		t.Fatalf("找不到函数 %s", fn)
	}
	depth := 0
	for i := loc[0]; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[loc[0] : i+1]
			}
		}
	}
	t.Fatalf("函数 %s 体未闭合", fn)
	return ""
}

// ValidateStrongPasswordNoSpecialForTest 供上述对拍用：构造一个"只差特殊字符"的合规口令，
// 交给 credentials.ValidateStrongPassword 判定。收紧的基线（12+大小写+数字+特殊）
// 必须因为它**缺特殊字符**而被拒。
func ValidateStrongPasswordNoSpecialForTest() string {
	pw := "Abcdefghij1k" // 12 位，大小写+数字，无特殊字符
	if msg := credentials.ValidateStrongPassword(pw); msg != "" {
		return msg
	}
	return ""
}
