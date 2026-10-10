// perm_catalog_test.go 规则权限点的目录守护（TD-60 A-2 阻断级缺陷的静态防线，2026-10-10）。
//
// # 为什么需要
//
// 2026-10-10 端到端双轨冒烟实测：持合法 cookie 打 `/api/v1/auth-svc/me` 恒返
// `403 permission denied: auth:read`——auth 代理规则要求 `auth:read`/`auth:write`，
// 而控制面权限目录（internal/store/model.PermSpecs）**没有 auth: 组**；
// requirePermission 的判据是「用户权限集 ∋ required」，无空串豁免 ⇒
// **含 admin 在内的任何身份都不可能通过**，双轨开关打开即认证面恒 403。
//
// 现有目录守护（internal/store/sql_rbac_catalog_test.go 的 handlerRequiredPerms）
// 只对账**手工维护的 handler 权限点清单**；代理的权限点来自**规则数据**
// （Rule.Perm / Rule.PermRules[].Perm，运行时经 ResolvePerm 解析），不在那份清单里 ⇒
// 静态对账存在盲区（守护绿、线上恒 403）。本测试把规则数据也纳入「引用点 ⊂ 目录」断言。
//
// # 为什么放在本包而不是 store 的目录守护里
//
// 依赖方向：svcproxy → internal/store/model（只读目录）是正常方向；反过来让
// internal/store 的测试 import controlplane/svcproxy 会把依赖倒过来（分层污染）。
// 所以「规则数据的权限点 ⊂ 目录」这条断言住在规则数据隔壁。
//
// # 防塌缩
//
// 与门禁第 21/24 节同哲学：断言带**下限**（真实权限点去重数 ≥ floor），
// 表被改名/清空/解析失效时判红而不是安静变绿。
package svcproxy

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

// realPermsFloor 规则表中「真实权限点」（非认证哨兵）去重后的下限，只许上调。
// 当前实测值（2026-10-10）：device/task/alert/cmdb/gpu/incident/runbook/autoscaler/
// portal/network/approval/schedule/provision 等 20+ 个，取 15 留余量。
const realPermsFloor = 15

// TestRulePermsExistInCatalog 断言三张规则表引用的每个权限点都在控制面权限目录里，
// 或为显式的「仅认证」哨兵。
func TestRulePermsExistInCatalog(t *testing.T) {
	catalog := map[string]bool{}
	for _, spec := range model.PermSpecs {
		if spec.Name != "" {
			catalog[spec.Name] = true
		}
	}
	if len(catalog) < 40 {
		t.Fatalf("权限目录只读到 %d 个权限点（< 40）——目录解析失效或权限点被批量删除，先核 PermSpecs", len(catalog))
	}

	// 哨兵不得与真实权限点撞名（否则「仅认证」语义会被目录里的真权限点悄悄顶替）。
	if catalog[PermAuthenticated] {
		t.Fatalf("哨兵 %q 出现在权限目录里——哨兵必须与真实权限点命名空间隔离", PermAuthenticated)
	}

	tables := map[string][]Rule{
		"Rules":        Rules,
		"DeviceExtras": DeviceExtras,
		"TaskExtras":   TaskExtras,
	}
	checked := 0
	real := map[string]bool{}
	for table, rules := range tables {
		for i := range rules {
			refs := []struct {
				where string
				perm  string
			}{{"Perm", rules[i].Perm}}
			for j := range rules[i].PermRules {
				refs = append(refs, struct {
					where string
					perm  string
				}{"PermRules[" + rules[i].PermRules[j].Method + " " + rules[i].PermRules[j].PathPrefix + "]", rules[i].PermRules[j].Perm})
			}
			for _, ref := range refs {
				if ref.perm == PermAuthenticated {
					checked++
					continue
				}
				checked++
				real[ref.perm] = true
				if !catalog[ref.perm] {
					t.Errorf("%s 的规则 %q 引用了目录外权限点 %q——"+
						"requirePermission 判据是「用户权限集 ∋ required」，目录里没有的点**任何角色（含 admin）都过不了**，该端点将恒 403。"+
						"修法：补进 model.PermSpecs 与 role 派生，或改用 PermAuthenticated（仅认证，不查权限）",
						table, rules[i].PublicPrefix, ref.perm)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("规则表里没有扫到任何权限点引用——扫描面塌缩（表被改名/清空？）")
	}
	if len(real) < realPermsFloor {
		t.Fatalf("规则表中真实权限点去重后只有 %d 个（< 下限 %d）——扫描面疑似塌缩，先核三张表", len(real), realPermsFloor)
	}
	t.Logf("规则权限点对账通过：%d 处引用（真实权限点 %d 个，哨兵放行），全部 ⊆ 目录", checked, len(real))
}

// TestCredentialForwardingRestrictedToAuthDomain 会话 Cookie 投递的静态守卫（TD-60 §9.3 方向 A）。
//
// 语义：聚合代理默认剥除客户端会话 Cookie（防凭证落地到内部服务日志），**只有 auth 域**
// （凭证签发方，且 auth-svc 是自验 token 模型、不读注入身份头）可置 ForwardCookie=true。
// 若把别的域置 true：该域上游会收到用户会话凭证——既有日志落地风险，也在被入侵时
// 多一处可重放的凭证面。这是「凭证只投递给签发方」的最小信任原则，用静态断言钉住。
func TestCredentialForwardingRestrictedToAuthDomain(t *testing.T) {
	tables := map[string][]Rule{
		"Rules":        Rules,
		"DeviceExtras": DeviceExtras,
		"TaskExtras":   TaskExtras,
	}
	forwarded, authRules := 0, 0
	for table, rules := range tables {
		for i := range rules {
			if rules[i].Domain == "auth" {
				authRules++
			}
			if !rules[i].ForwardCookie {
				continue
			}
			forwarded++
			if rules[i].Domain != "auth" {
				t.Errorf("%s 的规则 %q（Domain=%s）设了 ForwardCookie——"+
					"会话凭证只允许投递给凭证签发方（auth 域）：其余域身份走注入头、剥 Cookie 是既有安全语义"+
					"（TD-60 §9.3 方向 A，2026-10-10 裁决）",
					table, rules[i].PublicPrefix, rules[i].Domain)
			}
		}
	}
	// 双下限防塌缩：auth 域规则存在、且确有规则放行凭证。
	// 若无规则置位 ⇒ 双轨开关打开后 auth-svc 恒 401（§9.2 缺陷复发）；若 auth 域消失 ⇒ 守卫失去锚点。
	if authRules == 0 {
		t.Fatal("规则表里找不到 auth 域——双轨机制载体消失，守卫失去锚点")
	}
	if forwarded == 0 {
		t.Fatal("没有任何规则设置 ForwardCookie——auth 域 Cookie 透传被关掉，" +
			"双轨开关打开后 auth-svc 将对每个请求返 401（td60 提案 §9.2 缺陷形态复发）")
	}
}

// TestAuthRuleIsAuthenticatedOnly 钉住阻断级缺陷的修复形态：
// auth 域自服务端点在代理层只认证、不查权限（与控制面本地 handler 语义一致）。
// 若有人把它们改回某个具体权限点，本用例会失败——那正是缺陷复发形态。
func TestAuthRuleIsAuthenticatedOnly(t *testing.T) {
	var auth *Rule
	for i := range Rules {
		if Rules[i].Domain == "auth" {
			auth = &Rules[i]
			break
		}
	}
	if auth == nil {
		t.Fatal("规则表里找不到 auth 域（双轨机制的载体）——表被改名？")
	}
	// 自服务端点清单：与 auth-svc 网关暴露 + 本地控制面提供的一一对应。
	paths := []string{
		"/api/v1/auth/me",
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/auth/logout",
		"/api/v1/auth/refresh",
		"/api/v1/auth/change-password",
	}
	for _, p := range paths {
		if got := auth.ResolvePerm("GET", p); got != PermAuthenticated {
			t.Errorf("auth 规则 GET %s 解析出权限点 %q，want %q（本地该端点只做 token 校验，不查权限）", p, got, PermAuthenticated)
		}
		if got := auth.ResolvePerm("POST", p); got != PermAuthenticated {
			t.Errorf("auth 规则 POST %s 解析出权限点 %q，want %q", p, got, PermAuthenticated)
		}
	}
	// 兜底（未列出的路径）也必须是哨兵：auth 域不存在「需要具体权限点」的端点，
	// 若未来出现，应先补目录权限点并在此显式登记。
	if auth.Perm != PermAuthenticated {
		t.Errorf("auth 规则兜底 Perm = %q, want %q", auth.Perm, PermAuthenticated)
	}
	// 非 auth 域规则**不得**使用哨兵（含 DeviceExtras/TaskExtras）：那会把该域权限闸整个关掉，
	// 是静默提权面；裁判定为「只有 auth 域的自服务端点可以仅认证」。
	for _, table := range [][]Rule{Rules, DeviceExtras, TaskExtras} {
		for i := range table {
			if table[i].Domain == "auth" {
				continue
			}
			if table[i].Perm == PermAuthenticated {
				t.Errorf("非 auth 域规则 %q 的兜底 Perm 使用了仅认证哨兵——该域会失去权限闸", table[i].PublicPrefix)
			}
			for j := range table[i].PermRules {
				if table[i].PermRules[j].Perm == PermAuthenticated {
					t.Errorf("非 auth 域规则 %q 的路径规则（%s %s）使用了仅认证哨兵——该路径会失去权限闸",
						table[i].PublicPrefix, table[i].PermRules[j].Method, table[i].PermRules[j].PathPrefix)
				}
			}
		}
	}
}
